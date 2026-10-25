package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/webui"
)

// seedStatsRepo writes the given PRs into a store under host/owner/name and
// returns the opened store.
func seedStatsRepo(t *testing.T, host, owner, name string, prs ...*github.PullRequest) cache.Store {
	t.Helper()
	store := cache.NewStoreWithPath(t.TempDir())
	for _, pr := range prs {
		if err := store.SavePR(host, owner, name, pr); err != nil {
			t.Fatalf("SavePR: %v", err)
		}
	}
	if err := store.SaveCacheInfo(host, owner, name, 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestServeStatsFunc_RendersReport verifies the injected stats renderer builds
// the report from the cached PRs of the requested repositories.
func TestServeStatsFunc_RendersReport(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	pr := statsTestPR(1, "alice", base, "MERGED", []github.Comment{
		{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
	})
	store := seedStatsRepo(t, "github.com", "acme", "repo1", pr)

	stats := serveStatsFunc(store)
	html, err := stats(webui.StatsQuery{Repos: []string{"github.com/acme/repo1"}, State: "all"})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	for _, want := range []string{
		"Repository activity report",
		"Repos: acme/repo1",
		"alice",
		"bob",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report missing %q", want)
		}
	}
}

// TestServeStatsFunc_AllCachedRepos verifies the empty repo list falls back to
// every cached repository, and period filters apply.
func TestServeStatsFunc_AllCachedRepos(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	inPeriod := statsTestPR(1, "alice", base, "OPEN", nil)
	outOfPeriod := statsTestPR(2, "alice", base.Add(-720*time.Hour), "OPEN", nil)
	store := seedStatsRepo(t, "github.com", "acme", "repo1", inPeriod, outOfPeriod)

	stats := serveStatsFunc(store)
	html, err := stats(webui.StatsQuery{From: "2026-03-01", To: "2026-03-31", ListPRs: true})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(html, "#1</a>") {
		t.Error("report is missing the in-period PR")
	}
	if strings.Contains(html, "#2</a>") {
		t.Error("report contains the out-of-period PR")
	}
}

// TestServeStatsFunc_Filters verifies the query filters map onto the report.
func TestServeStatsFunc_Filters(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	alice := statsTestPR(1, "alice", base, "OPEN", nil)
	bob := statsTestPR(2, "bob", base, "OPEN", nil)
	store := seedStatsRepo(t, "github.com", "acme", "repo1", alice, bob)

	stats := serveStatsFunc(store)
	html, err := stats(webui.StatsQuery{Authors: []string{"alice"}})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if strings.Contains(html, "bob") {
		t.Error("author filter did not exclude bob from the report")
	}
}

// TestServeStatsFunc_PeriodForm verifies interactive reports carry the period
// form that lets users refilter the report from the page.
func TestServeStatsFunc_PeriodForm(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	store := seedStatsRepo(t, "github.com", "acme", "repo1", statsTestPR(1, "alice", base, "OPEN", nil))

	stats := serveStatsFunc(store)
	html, err := stats(webui.StatsQuery{From: "2026-03-01", To: "2026-03-31"})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	for _, want := range []string{
		`action="/stats"`,
		`id="report-from"`,
		`name="from" value="2026-03-01"`,
		`name="to" value="2026-03-31"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("period form missing %q", want)
		}
	}
	// All-cached-repo reports resubmit without pinning the repository set.
	if strings.Contains(html, `name="repo"`) {
		t.Error("all-repos report pins repositories in the period form")
	}

	html, err = stats(webui.StatsQuery{Repos: []string{"github.com/acme/repo1"}})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(html, `name="repo" value="github.com/acme/repo1"`) {
		t.Error("single-repo report lost its repo in the period form")
	}
}

// TestRenderReport_PeriodFormIsInteractiveOnly verifies buildReport fills the
// form inputs and that the CLI-written file stays static.
func TestRenderReport_PeriodFormIsInteractiveOnly(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: statsTestPR(1, "alice", base, "OPEN", nil)}},
		statsFilters{From: base.Add(-24 * time.Hour), To: base.Add(24 * time.Hour), State: "all"},
	)
	if len(report.RepoIDs) != 1 || report.RepoIDs[0] != "github.com/org/repo1" {
		t.Fatalf("RepoIDs: got %v", report.RepoIDs)
	}
	if report.FromValue != "2026-03-09" || report.ToValue != "2026-03-11" {
		t.Errorf("date values: got %q/%q", report.FromValue, report.ToValue)
	}

	html, err := renderReport(report)
	if err != nil {
		t.Fatalf("renderReport: %v", err)
	}
	if strings.Contains(html, `action="/stats"`) {
		t.Error("CLI report must not render the period form")
	}

	report.Interactive = true
	html, err = renderReport(report)
	if err != nil {
		t.Fatalf("renderReport interactive: %v", err)
	}
	for _, want := range []string{
		`action="/stats"`,
		`name="repo" value="github.com/org/repo1"`,
		`name="from" value="2026-03-09"`,
		`name="to" value="2026-03-11"`,
		`name="top" value="5"`,
		`id="report-state"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("interactive report missing %q", want)
		}
	}

	// All-repos reports omit the hidden repository inputs.
	report.AllRepos = true
	html, err = renderReport(report)
	if err != nil {
		t.Fatalf("renderReport all repos: %v", err)
	}
	if strings.Contains(html, `name="repo"`) {
		t.Error("AllRepos report pins repositories in the period form")
	}
}

// TestServeStatsFunc_Errors verifies the error paths the UI surfaces.
func TestServeStatsFunc_Errors(t *testing.T) {
	empty := serveStatsFunc(cache.NewStoreWithPath(t.TempDir()))
	if _, err := empty(webui.StatsQuery{}); err == nil {
		t.Error("empty cache: want error, got nil")
	}

	store := seedStatsRepo(t, "github.com", "acme", "repo1", statsTestPR(1, "alice", time.Now(), "OPEN", nil))
	stats := serveStatsFunc(store)
	if _, err := stats(webui.StatsQuery{Repos: []string{"acme/repo1"}}); err == nil {
		t.Error("two-part repo name: want error, got nil")
	}
	if _, err := stats(webui.StatsQuery{Repos: []string{"github.com/acme/other"}}); err == nil {
		t.Error("uncached repo: want error, got nil")
	}
	if _, err := stats(webui.StatsQuery{From: "not-a-date"}); err == nil {
		t.Error("invalid from: want error, got nil")
	}
	if _, err := stats(webui.StatsQuery{From: "2026-03-10", To: "2026-03-01"}); err == nil {
		t.Error("to before from: want error, got nil")
	}
}
