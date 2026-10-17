package cmd

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/logging"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

func readCacheScenario() *mockserver.Scenario {
	return mockserver.NewScenarioBuilder("acme", "myproject").
		WithNow(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)).
		AddIssue("Bug: crash on start", "Crashes on startup", 5*24*time.Hour,
			mockserver.WithIssueComment("alice", "Reproduced.", 4*24*time.Hour),
			mockserver.WithIssueComment("bob", "Fixed.", 3*24*time.Hour),
		).
		AddIssue("Docs: update README", "README is outdated", 2*24*time.Hour).
		AddPR("Fix crash on start", "Fixes the crash.", "fix/crash", 3*24*time.Hour,
			mockserver.WithPRReview("APPROVED"),
			mockserver.WithPRComment("bob", "LGTM.", 3*24*time.Hour),
		).
		Build()
}

// withReadFlags isolates the read-command globals and points them at a mock
// server and a temp cache dir for the duration of a test.
func withReadFlags(t *testing.T, apiURL, dir string) {
	t.Helper()
	oldRepo, oldAPI, oldDir, oldStorage := repoFlag, apiURLFlag, cacheDir, storageFlag
	oldIssueListJSON, oldIssueListNoTruncate := issueListJSON, issueListNoTruncate
	oldIssueListState, oldIssueListLimit := issueListState, issueListLimit
	oldIssueViewComments, oldIssueViewJSON := issueViewComments, issueViewJSON
	oldIssueViewRefresh, oldIssueViewIDs := issueViewRefresh, issueViewIDs
	oldPRViewComments, oldPRViewJSON, oldPRViewRefresh := prViewComments, prViewJSON, prViewRefresh
	t.Cleanup(func() {
		repoFlag, apiURLFlag, cacheDir, storageFlag = oldRepo, oldAPI, oldDir, oldStorage
		issueListJSON, issueListNoTruncate = oldIssueListJSON, oldIssueListNoTruncate
		issueListState, issueListLimit = oldIssueListState, oldIssueListLimit
		issueViewComments, issueViewJSON = oldIssueViewComments, oldIssueViewJSON
		issueViewRefresh, issueViewIDs = oldIssueViewRefresh, oldIssueViewIDs
		prViewComments, prViewJSON, prViewRefresh = oldPRViewComments, oldPRViewJSON, oldPRViewRefresh
	})
	repoFlag = "acme/myproject"
	apiURLFlag = apiURL
	cacheDir = dir
	storageFlag = "file"
	issueListJSON, issueListNoTruncate = false, false
	issueListState, issueListLimit = "open", 1000
	issueViewComments, issueViewJSON = true, false
	issueViewRefresh, issueViewIDs = false, false
	prViewComments, prViewJSON, prViewRefresh = true, false, false
}

// TestIssueList_CachesFetchedIssues verifies that a list served from the API
// backfills the cache with the full issue records (comments included), matching
// what `ghx cache` writes.
func TestIssueList_CachesFetchedIssues(t *testing.T) {
	srv := mockserver.NewServer(readCacheScenario())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)

	captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})

	store := cache.NewStoreWithPath(dir)
	defer store.Close()

	issue, _, err := store.LoadIssue("github.com", "acme", "myproject", 1)
	if err != nil {
		t.Fatalf("issue #1 was not cached: %v", err)
	}
	if len(issue.Comments) != 2 {
		t.Errorf("cached issue #1 has %d comments, want 2 (detail was not fetched)", len(issue.Comments))
	}

	if _, _, err := store.LoadIssue("github.com", "acme", "myproject", 2); err != nil {
		t.Errorf("issue #2 (no comments) was not cached: %v", err)
	}
}

// TestIssueList_KeepsExistingCache verifies that an entry already in the cache
// is left untouched, so a list never overwrites richer cached data.
func TestIssueList_KeepsExistingCache(t *testing.T) {
	srv := mockserver.NewServer(readCacheScenario())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)

	store := cache.NewStoreWithPath(dir)
	defer store.Close()
	if err := store.SaveIssue("github.com", "acme", "myproject", &github.Issue{Number: 1, Title: "sentinel"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})

	issue, _, err := store.LoadIssue("github.com", "acme", "myproject", 1)
	if err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	if issue.Title != "sentinel" {
		t.Errorf("cached issue #1 was overwritten by list; title = %q", issue.Title)
	}
}

// TestPRList_CachesFetchedPRs verifies that a list served from the API backfills
// the cache with the full PR records (comments included), matching `ghx cache`.
func TestPRList_CachesFetchedPRs(t *testing.T) {
	scenario := readCacheScenario()
	prNumber := scenario.PRs[0].Number
	srv := mockserver.NewServer(scenario)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)

	captureStdout(t, func() {
		if err := runPRList(nil, nil); err != nil {
			t.Fatalf("runPRList: %v", err)
		}
	})

	store := cache.NewStoreWithPath(dir)
	defer store.Close()

	pr, _, err := store.LoadPR("github.com", "acme", "myproject", prNumber)
	if err != nil {
		t.Fatalf("PR #%d was not cached: %v", prNumber, err)
	}
	if len(pr.Comments) == 0 {
		t.Error("cached PR has no comments (detail was not fetched)")
	}
}

// TestIssueList_EmitsDebugTimings verifies that with debug logging enabled a
// list logs where its time went: the API list call and the cache backfill
// stages.
func TestIssueList_EmitsDebugTimings(t *testing.T) {
	srv := mockserver.NewServer(readCacheScenario())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)

	if err := logging.SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug): %v", err)
	}
	var buf bytes.Buffer
	logging.SetOutput(&buf)
	t.Cleanup(func() {
		logging.SetOutput(os.Stderr)
		_ = logging.SetLevel("info")
	})

	captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})

	out := buf.String()
	for _, want := range []string{
		"issue list: fetched from API",
		"cache backfill: scanned",
		"cache backfill: done",
		"duration=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug log missing %q; got:\n%s", want, out)
		}
	}
}

// TestIssueView_FreshCacheMissFetches verifies that viewing an item missing from
// an otherwise-fresh cache fetches it and caches it instead of erroring.
func TestIssueView_FreshCacheMissFetches(t *testing.T) {
	srv := mockserver.NewServer(readCacheScenario())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)

	// Mark the repository's issues fresh without caching any item.
	store := cache.NewStoreWithPath(dir)
	if err := store.SaveCacheInfo("github.com", "acme", "myproject", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	store.Close()

	out := captureStdout(t, func() {
		if err := runIssueView(nil, []string{"1"}); err != nil {
			t.Fatalf("runIssueView: %v", err)
		}
	})
	if out == "" {
		t.Error("runIssueView printed nothing")
	}

	check := cache.NewStoreWithPath(dir)
	defer check.Close()
	if _, _, err := check.LoadIssue("github.com", "acme", "myproject", 1); err != nil {
		t.Errorf("viewed issue was not cached: %v", err)
	}
}

// TestPRView_FreshCacheMissFetches is the pull-request counterpart of
// TestIssueView_FreshCacheMissFetches.
func TestPRView_FreshCacheMissFetches(t *testing.T) {
	scenario := readCacheScenario()
	prNumber := scenario.PRs[0].Number
	srv := mockserver.NewServer(scenario)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)

	store := cache.NewStoreWithPath(dir)
	if err := store.SaveCacheInfo("github.com", "acme", "myproject", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	store.Close()

	out := captureStdout(t, func() {
		if err := runPRView(nil, []string{strconv.Itoa(prNumber)}); err != nil {
			t.Fatalf("runPRView: %v", err)
		}
	})
	if out == "" {
		t.Error("runPRView printed nothing")
	}

	check := cache.NewStoreWithPath(dir)
	defer check.Close()
	if _, _, err := check.LoadPR("github.com", "acme", "myproject", prNumber); err != nil {
		t.Errorf("viewed PR was not cached: %v", err)
	}
}
