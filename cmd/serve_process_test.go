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
		// The interactive window form preserves the requested repositories.
		`action="/process"`,
		`<input type="hidden" name="repo" value="github.com/acme/repo1">`,
		`name="days" min="1" max="730" value="30"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report missing %q", want)
		}
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
