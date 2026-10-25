package webui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/processflow"
)

// ProcessMaxDays bounds the reporting window, matching the process command.
const processMaxDays = 730

// handleProcess serves the process report at /process. The report is rendered
// by the injected ProcessFunc; the query string mirrors the ghx process flags
// (repo, days) plus the stats page's period bounds (from, to).
func (s *Server) handleProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method %s not allowed", r.Method)
		return
	}
	if s.process == nil {
		httpError(w, http.StatusNotImplemented, "process report is not available")
		return
	}
	days, err := parseDaysParam(r.URL.Query().Get("days"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "%s", err.Error())
		return
	}
	if err := validateRangeParams(r.URL.Query().Get("from"), r.URL.Query().Get("to")); err != nil {
		httpError(w, http.StatusBadRequest, "%s", err.Error())
		return
	}
	q := ProcessQuery{
		Repos: r.URL.Query()["repo"],
		Days:  days,
		From:  r.URL.Query().Get("from"),
		To:    r.URL.Query().Get("to"),
	}
	html, err := s.process(q)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// handleAPIProcess serves the process report as JSON at /api/process, computed
// from the cache only (the web API never calls GitHub). Query params: repo
// (repeatable, "host/owner/repo", omitted means every cached repository),
// days, and the optional from/to bounds; when either bound is set the report
// covers that explicit range instead of the trailing days window.
func (s *Server) handleAPIProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method %s not allowed", r.Method)
		return
	}
	days, err := parseDaysParam(r.URL.Query().Get("days"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "%s", err.Error())
		return
	}
	coords, err := s.resolveProcessRepos(r.URL.Query()["repo"])
	if err != nil {
		httpError(w, http.StatusBadRequest, "%s", err.Error())
		return
	}
	start, end, err := parseRangeParams(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "%s", err.Error())
		return
	}
	var items []processflow.Item
	for _, c := range coords {
		issues, err := s.store.LoadAllIssues(c[0], c[1], c[2])
		if err != nil {
			httpError(w, http.StatusInternalServerError, "%s", err.Error())
			return
		}
		prs, err := s.store.LoadAllPRs(c[0], c[1], c[2])
		if err != nil {
			httpError(w, http.StatusInternalServerError, "%s", err.Error())
			return
		}
		items = append(items, processflow.FromIssues(issues)...)
		items = append(items, processflow.FromPRs(prs)...)
	}
	var report processflow.Report
	switch {
	case !start.IsZero() || !end.IsZero():
		if end.IsZero() {
			end = time.Now().UTC()
		}
		report = processflow.BuildRange(items, start, end)
	case days > 0:
		report = processflow.Build(items, time.Now().UTC(), days)
	default:
		// No bounds and no window: the entire available history.
		report = processflow.BuildRange(items, time.Time{}, time.Now().UTC())
	}
	writeJSON(w, http.StatusOK, report)
}

// dateParamLayouts are the accepted bounds formats, matching the stats
// page's period parameters: date-only values and RFC3339 timestamps.
var dateParamLayouts = []string{"2006-01-02", time.RFC3339}

// parseBoundParam parses one optional bound. A date-only value is midnight,
// or the last nanosecond of the day when endOfDay is set, so a `to` bound is
// inclusive. Empty means absent (zero time).
func parseBoundParam(raw string, endOfDay bool) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	for _, layout := range dateParamLayouts {
		t, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		if endOfDay && layout == "2006-01-02" {
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), t.Location())
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid bound %q: expected YYYY-MM-DD or RFC3339", raw)
}

// parseRangeParams parses both optional bounds, failing when `to` precedes
// `from`.
func parseRangeParams(fromRaw, toRaw string) (time.Time, time.Time, error) {
	from, err := parseBoundParam(fromRaw, false)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parseBoundParam(toRaw, true)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("to (%s) is before from (%s)", toRaw, fromRaw)
	}
	return from, to, nil
}

// validateRangeParams checks the raw bounds without resolving them; the
// injected renderer parses them itself.
func validateRangeParams(fromRaw, toRaw string) error {
	_, _, err := parseRangeParams(fromRaw, toRaw)
	return err
}

// parseDaysParam reads the optional window length: empty means unset (the
// report covers all available history unless bounds are given), and a value
// is clamped to the range the command accepts.
func parseDaysParam(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > processMaxDays {
		return 0, fmt.Errorf("invalid days %q: expected 1-%d", raw, processMaxDays)
	}
	return days, nil
}

// resolveProcessRepos maps requested "host/owner/repo" names onto cached
// coordinates; an empty list selects every cached repository. An explicitly
// named repository that is not cached is an error, like the refresh endpoint.
func (s *Server) resolveProcessRepos(names []string) ([][3]string, error) {
	cached, err := s.store.ListCachedRepos()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		coords := make([][3]string, 0, len(cached))
		for _, cr := range cached {
			coords = append(coords, [3]string{cr.Host, cr.Owner, cr.Repo})
		}
		return coords, nil
	}
	coords := make([][3]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		parts := strings.Split(name, "/")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid repository %q: expected host/owner/repo", name)
		}
		host, owner, repo := parts[0], parts[1], parts[2]
		found := false
		for _, cr := range cached {
			if strings.EqualFold(cr.Host, host) && strings.EqualFold(cr.Owner, owner) && strings.EqualFold(cr.Repo, repo) {
				coords = append(coords, [3]string{cr.Host, cr.Owner, cr.Repo})
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("repository %s is not cached", name)
		}
		key := strings.ToLower(host + "/" + owner + "/" + repo)
		if seen[key] {
			continue
		}
		seen[key] = true
	}
	return coords, nil
}
