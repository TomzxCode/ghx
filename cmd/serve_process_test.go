package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/webui"
)

// seedProcessRepo writes the given issues and PRs into a store under
// host/owner/name and returns the opened store.
func seedProcessRepo(t *testing.T, host, owner, name string, issues []*github.Issue, prs []*github.PullRequest) cache.Store {
	t.Helper()
	store := cache.NewStoreWithPath(t.TempDir())
	for _, issue := range issues {
		if err := store.SaveIssue(host, owner, name, issue); err != nil {
			t.Fatalf("SaveIssue: %v", err)
		}
	}
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

// TestServeProcessFunc_RendersReport verifies the injected process renderer
// builds the report from the cached issues and PRs of the requested
// repositories. Active items are named in the aging list, so the fixtures are
// open work.
func TestServeProcessFunc_RendersReport(t *testing.T) {
	now := time.Now().UTC()
	issue := &github.Issue{Number: 1, Title: "Open issue", State: "open", CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now}
	pr := &github.PullRequest{Number: 2, Title: "Draft PR", State: "open", IsDraft: true, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now}
	store := seedProcessRepo(t, "github.com", "acme", "repo1", []*github.Issue{issue}, []*github.PullRequest{pr})

	process := serveProcessFunc(store)
	html, err := process(webui.ProcessQuery{Repos: []string{"github.com/acme/repo1"}, Days: 30})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	for _, want := range []string{
		"Process report",
		"acme/repo1",
		"Open issue",
		"Draft PR",
		// The interactive period form preserves the requested repositories
		// and pre-fills the covered bounds.
		`action="/process"`,
		`<input type="hidden" name="repo" value="github.com/acme/repo1">`,
		`name="days" min="1" max="730" value="30"`,
		`name="from"`,
		`name="to"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report missing %q", want)
		}
	}
}

// TestServeProcessFunc_Range verifies an explicit from/to query renders the
// report over that range and names it in the meta line.
func TestServeProcessFunc_Range(t *testing.T) {
	now := time.Now().UTC()
	created := now.AddDate(0, -2, 0)
	closedAt := created.Add(48 * time.Hour)
	issue := &github.Issue{Number: 1, Title: "Ranged issue", State: "closed", CreatedAt: created, ClosedAt: &closedAt}
	store := seedProcessRepo(t, "github.com", "acme", "repo1", []*github.Issue{issue}, nil)

	process := serveProcessFunc(store)
	from := created.AddDate(0, 0, -1).Format("2006-01-02")
	to := created.AddDate(0, 0, 5).Format("2006-01-02")
	html, err := process(webui.ProcessQuery{Repos: []string{"github.com/acme/repo1"}, From: from, To: to})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if !strings.Contains(html, `name="from" value="`+from+`"`) || !strings.Contains(html, `name="to" value="`+to+`"`) {
		t.Error("date inputs should carry the requested bounds")
	}
	if strings.Contains(html, "last 90 days") {
		t.Error("a ranged report should not describe itself as the trailing window")
	}
	if strings.Contains(html, "No issues closed in the window") {
		t.Error("the range should include the closed issue's completion")
	}

	if _, err = process(webui.ProcessQuery{Repos: []string{"github.com/acme/repo1"}, From: to, To: from}); err == nil {
		t.Fatal("to before from should be an error")
	}
	if _, err = process(webui.ProcessQuery{Repos: []string{"github.com/acme/repo1"}, From: "yesterday"}); err == nil {
		t.Fatal("an unparseable bound should be an error")
	}
}

// TestServeProcessFunc_DefaultsToAllHistory verifies that with no bounds and
// no window the rendered report covers the entire available history and says
// so in the meta line.
func TestServeProcessFunc_DefaultsToAllHistory(t *testing.T) {
	created := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	issue := &github.Issue{Number: 1, Title: "Old issue", State: "open", CreatedAt: created, UpdatedAt: created}
	store := seedProcessRepo(t, "github.com", "acme", "repo1", []*github.Issue{issue}, nil)

	process := serveProcessFunc(store)
	html, err := process(webui.ProcessQuery{Repos: []string{"github.com/acme/repo1"}})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if !strings.Contains(html, "Jan 1, 2025") {
		t.Error("the meta line should name the covered span starting at the first item")
	}
	if strings.Contains(html, "last 90 days") {
		t.Error("the all-history default should not describe itself as a trailing window")
	}
	if !strings.Contains(html, "Old issue") {
		t.Error("the aging list should include the old item")
	}
	// The date inputs stay empty: submitting again keeps covering everything.
	if !strings.Contains(html, `name="from" value=""`) || !strings.Contains(html, `name="to" value=""`) {
		t.Error("the date inputs should be unset by default")
	}
	if strings.Contains(html, `name="days" min="1" max="730" value=""`) == false {
		t.Error("the days input should be unset by default")
	}
}

// TestServeProcessFunc_RejectsUncachedRepo mirrors the stats renderer: an
// explicitly named repository that is not cached is an error, never an API
// call, and an empty cache is an error rather than an empty report.
func TestServeProcessFunc_RejectsUncachedRepo(t *testing.T) {
	empty := cache.NewStoreWithPath(t.TempDir())
	t.Cleanup(func() { empty.Close() })
	process := serveProcessFunc(empty)
	if _, err := process(webui.ProcessQuery{Repos: nil, Days: 30}); err == nil {
		t.Fatal("an empty cache should be an error, not an empty report")
	}

	seedProcessRepo(t, "github.com", "acme", "repo1", nil, nil)
	if _, err := process(webui.ProcessQuery{Repos: []string{"github.com/acme/other"}, Days: 30}); err == nil {
		t.Fatal("uncached repository should be an error")
	}
}
