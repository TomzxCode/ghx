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
// (repo, days).
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
	q := ProcessQuery{Repos: r.URL.Query()["repo"], Days: days}
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
// (repeatable, "host/owner/repo", omitted means every cached repository) and
// days.
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
	report := processflow.Build(items, time.Now().UTC(), days)
	writeJSON(w, http.StatusOK, report)
}

// parseDaysParam reads the window length, defaulting to the process default
// and clamped to the same range the command accepts.
func parseDaysParam(raw string) (int, error) {
	if raw == "" {
		return processflow.DefaultWindowDays, nil
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
