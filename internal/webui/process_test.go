package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/processflow"
)

// TestProcessEndpoint verifies the /process handler parses its query into a
// ProcessQuery and serves the injected renderer's HTML.
func TestProcessEndpoint(t *testing.T) {
	var got ProcessQuery
	srv := newTestServer(newTestStore(t), nil)
	srv.SetProcessFunc(func(q ProcessQuery) (string, error) {
		got = q
		return "<html>process</html>", nil
	})
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/process?repo=github.com/acme/one&days=30", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	if len(got.Repos) != 1 || got.Repos[0] != "github.com/acme/one" || got.Days != 30 {
		t.Errorf("query = %+v, want one repo and 30 days", got)
	}
}

// TestProcessEndpoint_ValidationAndAvailability mirrors the stats endpoint's
// guard rails: 501 without a renderer, 400 on a bad window, 405 on non-GET.
func TestProcessEndpoint_ValidationAndAvailability(t *testing.T) {
	srv := newTestServer(newTestStore(t), nil)
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/process", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("no renderer: status = %d, want 501", rec.Code)
	}

	srv.SetProcessFunc(func(ProcessQuery) (string, error) { return "<html></html>", nil })
	h = srv.Handler()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/process?days=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad days: status = %d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/process?days=0", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("zero days: status = %d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/process", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("post: status = %d, want 405", rec.Code)
	}
}

// TestAPIProcess verifies the JSON endpoint computes the report from the
// cache: station counts, samples, and the aging list.
func TestAPIProcess(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	mergedAt := now.Add(-48 * time.Hour)
	seedRepo(t, store, "github.com", "acme", "repo1",
		[]*github.Issue{makeIssue(1, "Open issue", "open", "body", now.Add(-24*time.Hour))},
		[]*github.PullRequest{
			makePR(10, "Merged PR", "closed", false, &mergedAt, now.Add(-24*time.Hour)),
			makePR(11, "Draft PR", "open", true, nil, now.Add(-12*time.Hour)),
		},
	)
	srv := newTestServer(store, nil)
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/process?repo=github.com/acme/repo1&days=30", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var report processflow.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.WindowDays != 30 {
		t.Errorf("window = %d, want 30", report.WindowDays)
	}
	if len(report.Issues.Stations) != 2 || report.Issues.Stations[0].Count != 1 {
		t.Errorf("issue stations = %+v, want two stations with one open issue", report.Issues.Stations)
	}
	counts := map[processflow.Station]int{}
	for _, s := range report.PRs.Stations {
		counts[s.Station] = s.Count
	}
	if counts[processflow.Merged] != 1 || counts[processflow.Draft] != 1 {
		t.Errorf("PR station counts = %v, want one merged and one draft", counts)
	}
	if len(report.Samples) == 0 {
		t.Error("expected flow samples")
	}
	if len(report.WIP) != 2 {
		t.Errorf("WIP list = %d items, want 2 (the open issue and the draft PR)", len(report.WIP))
	}
}

// TestAPIProcess_DefaultsToAllCachedRepos verifies the empty repo list selects
// every cached repository, and an uncached one is rejected.
func TestAPIProcess_DefaultsToAllCachedRepos(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	seedRepo(t, store, "github.com", "acme", "one",
		[]*github.Issue{makeIssue(1, "One", "open", "", now)}, nil)
	seedRepo(t, store, "github.com", "acme", "two", nil,
		[]*github.PullRequest{makePR(2, "Two", "open", false, nil, now)})
	srv := newTestServer(store, nil)
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/process", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var report processflow.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	openIssues := report.Issues.Stations[0]
	readyPRs := report.PRs.Stations[1]
	if openIssues.Count != 1 || readyPRs.Count != 1 {
		t.Errorf("report = one open issue / ready PR expected, got %+v / %+v", openIssues, readyPRs)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/process?repo=github.com/acme/missing", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("uncached repo: status = %d, want 400", rec.Code)
	}
}
