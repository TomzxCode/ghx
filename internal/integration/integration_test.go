package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

func sampleScenario() *mockserver.Scenario {
	return mockserver.NewScenarioBuilder("acme", "myproject").
		WithSeed(42).
		AddIssue("Bug: crash on start", "App crashes on startup", 10*24*time.Hour,
			mockserver.WithIssueState("CLOSED"),
			mockserver.WithIssueAssignee("bob"),
			mockserver.WithIssueLabels("bug", "p0"),
			mockserver.WithIssueComment("alice", "Reproduced.", 9*24*time.Hour),
			mockserver.WithIssueComment("bob", "Fixed.", 8*24*time.Hour),
		).
		AddIssue("Feature: dark mode", "Add dark mode", 5*24*time.Hour,
			mockserver.WithIssueLabels("enhancement"),
			mockserver.WithIssueComment("carol", "Working on it.", 4*24*time.Hour),
		).
		AddIssue("Docs: update README", "README is outdated", 2*24*time.Hour,
			mockserver.WithIssueLabels("documentation"),
		).
		AddIssue("Fix login bug", "Login fails on Safari", 3*24*time.Hour,
			mockserver.WithIssueLabels("bug"),
			mockserver.WithIssueComment("alice", "Can reproduce.", 3*time.Hour),
		).
		AddIssue("Add CI pipeline", "Need automated tests", 1*24*time.Hour,
			mockserver.WithIssueLabels("enhancement", "ci"),
		).
		AddPR("Fix crash on start", "Fixes the crash.", "fix/crash", 8*24*time.Hour,
			mockserver.WithPRState("MERGED"),
			mockserver.WithPRReview("APPROVED"),
			mockserver.WithPRComment("alice", "LGTM.", 8*24*time.Hour),
		).
		AddPR("Add dark mode", "Adds dark theme.", "feat/dark-mode", 3*24*time.Hour,
			mockserver.WithPRLabels("enhancement"),
			mockserver.WithPRComment("bob", "Looking good!", 2*24*time.Hour),
		).
		AddPR("WIP: refactor auth", "Refactoring.", "refactor/auth", 1*24*time.Hour,
			mockserver.WithPRDraft(true),
		).
		Build()
}

func startServer(t *testing.T, scenario *mockserver.Scenario) *mockserver.Server {
	t.Helper()
	srv := mockserver.NewServer(scenario)
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_FetchAllIssues(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	issues, err := client.FetchAllIssues("acme", "myproject", nil, nil)
	if err != nil {
		t.Fatalf("FetchAllIssues: %v", err)
	}
	if len(issues) != 5 {
		t.Errorf("got %d issues, want 5", len(issues))
	}

	for _, issue := range issues {
		if issue.Number <= 0 {
			t.Errorf("issue number = %d, want > 0", issue.Number)
		}
		if issue.CreatedAt.IsZero() {
			t.Errorf("issue #%d has zero CreatedAt", issue.Number)
		}
	}
}

func TestClient_FetchAllPRs(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	prs, err := client.FetchAllPRs("acme", "myproject", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	if len(prs) != 3 {
		t.Errorf("got %d PRs, want 3", len(prs))
	}
}

// TestClient_FetchPRsAsc_ResumesFromPageCursor verifies the cache walk resumes
// from a stored page cursor instead of restarting the history.
func TestClient_FetchPRsAsc_ResumesFromPageCursor(t *testing.T) {
	cfg := mockserver.SimulationConfig{
		NumUsers:      3,
		Repos:         []string{"acme/big"},
		History:       60 * 24 * time.Hour,
		IssuesPerRepo: 5,
		PRsPerRepo:    250, // 5 pages of 50
		CloseRate:     0.3,
		MergeRate:     0.3,
		DraftRate:     0.1,
		ReviewRate:    0.5,
		Seed:          17,
	}
	srv := mockserver.NewServer(mockserver.Generate(cfg))
	t.Cleanup(srv.Close)

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	var firstPageCursor string
	all, err := client.FetchPRsAsc("acme", "big", "", func(_ []*github.PullRequest, _ int, after string, more bool) error {
		if more && firstPageCursor == "" {
			firstPageCursor = after
		}
		return nil
	})
	if err != nil {
		t.Fatalf("FetchPRsAsc: %v", err)
	}
	if len(all) != 250 {
		t.Fatalf("first walk returned %d PRs, want 250", len(all))
	}
	if firstPageCursor == "" {
		t.Fatal("first page reported no cursor to resume from")
	}

	resumed, err := client.FetchPRsAsc("acme", "big", firstPageCursor, nil)
	if err != nil {
		t.Fatalf("FetchPRsAsc resume: %v", err)
	}
	if len(resumed) != 200 {
		t.Errorf("resumed walk returned %d PRs, want 200", len(resumed))
	}
	if len(resumed) > 0 && len(all) > 0 && resumed[0].Number == all[0].Number {
		t.Errorf("resumed walk started at the first PR (#%d); the cursor was ignored", resumed[0].Number)
	}
}

// TestClient_FetchPRsAsc_RestartsWhenCursorRejected verifies a stored cursor
// that GitHub rejects is dropped and the walk restarts from the head.
func TestClient_FetchPRsAsc_RestartsWhenCursorRejected(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	prs, err := client.FetchPRsAsc("acme", "myproject", "not-a-cursor", nil)
	if err != nil {
		t.Fatalf("FetchPRsAsc: %v", err)
	}
	if len(prs) != 3 {
		t.Errorf("restarted walk returned %d PRs, want 3", len(prs))
	}
}

func TestClient_FetchAllIssues_ProgressCallback(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	var lastTotal, count int
	calls := 0
	cb := func(batch []*github.Issue, total int) error {
		lastTotal = total
		count += len(batch)
		calls++
		return nil
	}

	issues, err := client.FetchAllIssues("acme", "myproject", nil, cb)
	if err != nil {
		t.Fatalf("FetchAllIssues: %v", err)
	}
	if len(issues) != 5 {
		t.Fatalf("got %d issues, want 5", len(issues))
	}
	if calls == 0 {
		t.Fatal("batch callback was never invoked")
	}
	if lastTotal != 5 {
		t.Errorf("last total = %d, want 5", lastTotal)
	}
	if count != 5 {
		t.Errorf("batched count = %d, want 5", count)
	}
}

func TestClient_FetchAllPRs_ProgressCallback(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	var lastTotal, count int
	calls := 0
	cb := func(batch []*github.PullRequest, total int) error {
		lastTotal = total
		count += len(batch)
		calls++
		return nil
	}

	prs, err := client.FetchAllPRs("acme", "myproject", cb)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("got %d PRs, want 3", len(prs))
	}
	if calls == 0 {
		t.Fatal("batch callback was never invoked")
	}
	if lastTotal != 3 {
		t.Errorf("last total = %d, want 3", lastTotal)
	}
	if count != 3 {
		t.Errorf("batched count = %d, want 3", count)
	}
}

func TestClient_GetIssue(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	issue, err := client.GetIssue("acme", "myproject", 1)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Number != 1 {
		t.Errorf("Number = %d, want 1", issue.Number)
	}
	if issue.Title != "Bug: crash on start" {
		t.Errorf("Title = %q", issue.Title)
	}
	if issue.State != "CLOSED" {
		t.Errorf("State = %q, want CLOSED", issue.State)
	}
	if len(issue.Comments) != 2 {
		t.Errorf("Comments = %d, want 2", len(issue.Comments))
	}
}

func TestClient_GetPR(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	pr, err := client.GetPR("acme", "myproject", 6)
	if err != nil {
		t.Fatalf("GetPR: %v", err)
	}
	if pr.Number != 6 {
		t.Errorf("Number = %d, want 6", pr.Number)
	}
	if pr.State != "MERGED" {
		t.Errorf("State = %q, want MERGED", pr.State)
	}
	if pr.BaseRefName != "main" {
		t.Errorf("BaseRefName = %q, want main", pr.BaseRefName)
	}
}

func TestClient_ListIssues(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	issues, err := client.ListIssues("acme", "myproject", github.IssueListOptions{
		State: "open",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 4 {
		t.Errorf("got %d open issues, want 4", len(issues))
	}
}

func TestClient_ListPRs(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	prs, err := client.ListPRs("acme", "myproject", github.PRListOptions{
		State: "open",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 2 {
		t.Errorf("got %d open PRs, want 2", len(prs))
	}
}

func TestClient_GetIssue_NotFound(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	_, err = client.GetIssue("acme", "myproject", 999)
	if err == nil {
		t.Error("expected error for missing issue")
	}
}

func TestClient_DeltaFetch(t *testing.T) {
	cfg := mockserver.SmallConfig()
	scenario := mockserver.Generate(cfg)
	srv := startServer(t, scenario)

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	since := time.Now().Add(-1 * time.Hour)
	issues, err := client.FetchAllIssues("acme", "testrepo", &since, nil)
	if err != nil {
		t.Fatalf("FetchAllIssues with since: %v", err)
	}

	for _, issue := range issues {
		if issue.UpdatedAt.Before(since) {
			t.Errorf("issue #%d updatedAt=%v is before since=%v", issue.Number, issue.UpdatedAt, since)
		}
	}
}

func TestClient_FetchAndCache(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	store := cache.NewStoreWithPath(t.TempDir())

	issues, err := client.FetchAllIssues("acme", "myproject", nil, nil)
	if err != nil {
		t.Fatalf("FetchAllIssues: %v", err)
	}
	for _, issue := range issues {
		if err := store.SaveIssue("mock", "acme", "myproject", issue); err != nil {
			t.Fatalf("SaveIssue #%d: %v", issue.Number, err)
		}
	}

	prs, err := client.FetchAllPRs("acme", "myproject", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	for _, pr := range prs {
		if err := store.SavePR("mock", "acme", "myproject", pr); err != nil {
			t.Fatalf("SavePR #%d: %v", pr.Number, err)
		}
	}

	if err := store.SaveCacheInfo("mock", "acme", "myproject", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}

	loaded, _, err := store.LoadIssue("mock", "acme", "myproject", 1)
	if err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	if loaded.Number != 1 {
		t.Errorf("loaded issue number = %d, want 1", loaded.Number)
	}
	if loaded.Title != "Bug: crash on start" {
		t.Errorf("loaded title = %q", loaded.Title)
	}

	allIssues, err := store.LoadAllIssues("mock", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadAllIssues: %v", err)
	}
	if len(allIssues) != 5 {
		t.Errorf("cached %d issues, want 5", len(allIssues))
	}

	allPRs, err := store.LoadAllPRs("mock", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadAllPRs: %v", err)
	}
	if len(allPRs) != 3 {
		t.Errorf("cached %d PRs, want 3", len(allPRs))
	}

	fresh, err := store.IsCacheFresh("mock", "acme", "myproject")
	if err != nil {
		t.Fatalf("IsCacheFresh: %v", err)
	}
	if !fresh {
		t.Error("cache should be fresh")
	}
}

func TestClient_GenerateLargeAndFetch(t *testing.T) {
	cfg := mockserver.SimulationConfig{
		NumUsers:          5,
		Repos:             []string{"acme/testrepo"},
		History:           30 * 24 * time.Hour,
		IssuesPerRepo:     20,
		PRsPerRepo:        15,
		CommentsPerIssue:  3,
		CommentsPerPR:     2,
		CloseRate:         0.5,
		MergeRate:         0.6,
		DraftRate:         0.1,
		ReviewRate:        0.8,
		Seed:              1,
		AssigneesPerIssue: 1,
		AssigneesPerPR:    1,
		LabelsPerItem:     2,
		MilestonesPerRepo: 2,
	}
	scenario := mockserver.Generate(cfg)
	srv := startServer(t, scenario)

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	issues, err := client.FetchAllIssues("acme", "testrepo", nil, nil)
	if err != nil {
		t.Fatalf("FetchAllIssues: %v", err)
	}
	if len(issues) != 20 {
		t.Errorf("got %d issues, want 20", len(issues))
	}

	prs, err := client.FetchAllPRs("acme", "testrepo", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	if len(prs) != 15 {
		t.Errorf("got %d PRs, want 15", len(prs))
	}

	totalComments := 0
	for _, issue := range issues {
		totalComments += len(issue.Comments)
	}
	for _, pr := range prs {
		totalComments += len(pr.Comments)
	}
	if totalComments == 0 {
		t.Error("expected some comments")
	}
}

// advanceCursor mirrors cmd.advanceCursor so tests can reproduce the cache
// command's resume-cursor bookkeeping without depending on the cmd package.
func advanceCursor(cur **time.Time, t time.Time) {
	if t.IsZero() {
		return
	}
	if *cur == nil || t.After(**cur) {
		x := t
		*cur = &x
	}
}

// TestClient_FetchAllIssues_OldestFirst verifies the cold fetch returns issues
// ordered by updatedAt ascending, which is what makes resume-by-cursor correct.
func TestClient_FetchAllIssues_OldestFirst(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	issues, err := client.FetchAllIssues("acme", "myproject", nil, nil)
	if err != nil {
		t.Fatalf("FetchAllIssues: %v", err)
	}
	for i := 1; i < len(issues); i++ {
		if issues[i].UpdatedAt.Before(issues[i-1].UpdatedAt) {
			t.Errorf("issues not ascending by updatedAt: #%d (%v) < #%d (%v)",
				issues[i].Number, issues[i].UpdatedAt, issues[i-1].Number, issues[i-1].UpdatedAt)
		}
	}
}

// TestCache_ResumeAfterInterruption simulates an interrupted cold fetch (e.g. a
// rate limit after the first page) and verifies that:
//   - issues fetched so far are already on disk (incremental writes);
//   - the resume cursor was persisted;
//   - re-running resumes from the cursor and reaches a complete 150-issue cache.
func TestCache_ResumeAfterInterruption(t *testing.T) {
	cfg := mockserver.SimulationConfig{
		NumUsers:          3,
		Repos:             []string{"acme/big"},
		History:           60 * 24 * time.Hour,
		IssuesPerRepo:     150,
		PRsPerRepo:        0,
		CommentsPerIssue:  0,
		CloseRate:         0.3,
		Seed:              7,
		AssigneesPerIssue: 1,
		LabelsPerItem:     1,
		MilestonesPerRepo: 1,
	}
	scenario := mockserver.Generate(cfg)
	srv := startServer(t, scenario)

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	store := cache.NewStoreWithPath(t.TempDir())
	info := &cache.CacheInfo{}

	// First pass: write + persist cursor after each page, but abort after the
	// first page to simulate an interruption.
	page := 0
	interruptHandler := func(batch []*github.Issue, total int) error {
		page++
		for _, issue := range batch {
			if err := store.SaveIssue("mock", "acme", "big", issue); err != nil {
				return err
			}
			advanceCursor(&info.IssueCursor, issue.UpdatedAt)
		}
		if err := store.SaveCacheInfoFull("mock", "acme", "big", info); err != nil {
			return err
		}
		if page == 1 {
			return errors.New("simulated interruption after first page")
		}
		return nil
	}
	if _, err := client.FetchAllIssues("acme", "big", nil, interruptHandler); err == nil {
		t.Fatal("expected interruption error on first pass")
	}

	// Incremental writes: the first page is already on disk.
	onDisk, err := store.LoadAllIssues("mock", "acme", "big")
	if err != nil {
		t.Fatalf("LoadAllIssues after interruption: %v", err)
	}
	if len(onDisk) == 0 {
		t.Fatal("no issues persisted after first page; incremental writes are broken")
	}

	// Cursor persisted; cache not marked complete.
	saved, err := store.LoadCacheInfo("mock", "acme", "big")
	if err != nil {
		t.Fatalf("LoadCacheInfo after interruption: %v", err)
	}
	if saved.IssueCursor == nil {
		t.Fatal("resume cursor was not persisted")
	}
	if saved.Complete {
		t.Error("cache should not be marked complete after an interruption")
	}

	// Resume: fetch from the cursor with a handler that completes.
	resumeHandler := func(batch []*github.Issue, total int) error {
		for _, issue := range batch {
			if err := store.SaveIssue("mock", "acme", "big", issue); err != nil {
				return err
			}
			advanceCursor(&info.IssueCursor, issue.UpdatedAt)
		}
		return store.SaveCacheInfoFull("mock", "acme", "big", info)
	}
	if _, err := client.FetchAllIssues("acme", "big", info.IssueCursor, resumeHandler); err != nil {
		t.Fatalf("resume FetchAllIssues: %v", err)
	}
	info.Complete = true
	info.CachedAt = time.Now()
	info.Duration = 60
	if err := store.SaveCacheInfoFull("mock", "acme", "big", info); err != nil {
		t.Fatalf("final SaveCacheInfoFull: %v", err)
	}

	onDisk2, err := store.LoadAllIssues("mock", "acme", "big")
	if err != nil {
		t.Fatalf("LoadAllIssues after resume: %v", err)
	}
	if len(onDisk2) != 150 {
		t.Errorf("after resume got %d issues, want 150 (resume lost or duplicated data)", len(onDisk2))
	}

	// Freshness now reflects a complete cache.
	if fresh, _ := store.IsCacheFresh("mock", "acme", "big"); !fresh {
		t.Error("cache should be fresh after a complete resume")
	}
}

// TestClient_FetchPRsUpdated verifies the connection-walk PR delta path
// returns exactly the PRs updated at or after the cutoff (exact timestamps,
// not day-granular like the search API it replaced).
func TestClient_FetchPRsUpdated(t *testing.T) {
	srv := startServer(t, sampleScenario())

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	// Pick a cutoff that leaves some PRs after it.
	all, err := client.FetchAllPRs("acme", "myproject", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	var cutoff time.Time
	want := map[int]bool{}
	for _, pr := range all {
		if pr.Number == 7 { // "WIP: refactor auth", most recently updated in the sample
			cutoff = pr.UpdatedAt.Add(-1 * time.Second)
		}
	}
	for _, pr := range all {
		if !pr.UpdatedAt.Before(cutoff) {
			want[pr.Number] = true
		}
	}

	got, err := client.FetchPRsUpdated("acme", "myproject", cutoff, nil)
	if err != nil {
		t.Fatalf("FetchPRsUpdated: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one PR updated since the cutoff")
	}
	gotNums := map[int]bool{}
	for _, pr := range got {
		if pr.UpdatedAt.Before(cutoff) {
			t.Errorf("PR #%d updatedAt=%v is before cutoff %v", pr.Number, pr.UpdatedAt, cutoff)
		}
		gotNums[pr.Number] = true
	}
	if len(gotNums) != len(want) {
		t.Errorf("got %d distinct PRs, want %d", len(gotNums), len(want))
	}
	for n := range want {
		if !gotNums[n] {
			t.Errorf("PR #%d updated at or after the cutoff is missing from the delta result", n)
		}
	}
}

// TestClient_FetchPRsUpdated_StopsEarly verifies the newest-first delta walk
// stops paginating as soon as a page reaches the cutoff, so a delta over a
// large repo costs only a couple of requests instead of walking all pages.
func TestClient_FetchPRsUpdated_StopsEarly(t *testing.T) {
	cfg := mockserver.SimulationConfig{
		NumUsers:          3,
		Repos:             []string{"acme/big"},
		History:           60 * 24 * time.Hour,
		IssuesPerRepo:     5,   // PR bodies reference issue numbers, so not zero
		PRsPerRepo:        250, // 5 pages of 50
		CommentsPerIssue:  0,
		CommentsPerPR:     0,
		CloseRate:         0.3,
		MergeRate:         0.3,
		DraftRate:         0.1,
		ReviewRate:        0.5,
		Seed:              11,
		AssigneesPerIssue: 0,
		AssigneesPerPR:    1,
		LabelsPerItem:     1,
		MilestonesPerRepo: 1,
	}
	mock := mockserver.NewServer(mockserver.Generate(cfg))
	t.Cleanup(mock.Close)

	// Count GraphQL requests reaching the mock via a reverse proxy.
	var count int32
	target, err := url.Parse(mock.URL())
	if err != nil {
		t.Fatalf("parsing mock URL: %v", err)
	}
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
	}))
	t.Cleanup(counting.Close)

	client, err := github.NewClientWithURL(counting.URL, "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	all, err := client.FetchAllPRs("acme", "big", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	if len(all) != 250 {
		t.Fatalf("got %d PRs, want 250", len(all))
	}

	// Cutoff leaves only the 5 newest PRs on the delta side.
	desc := append([]*github.PullRequest(nil), all...)
	sort.Slice(desc, func(i, j int) bool { return desc[i].UpdatedAt.After(desc[j].UpdatedAt) })
	cutoff := desc[4].UpdatedAt
	want := 0
	for _, pr := range all {
		if !pr.UpdatedAt.Before(cutoff) {
			want++
		}
	}

	atomic.StoreInt32(&count, 0)
	got, err := client.FetchPRsUpdated("acme", "big", cutoff, nil)
	if err != nil {
		t.Fatalf("FetchPRsUpdated: %v", err)
	}

	if len(got) != want {
		t.Errorf("delta returned %d PRs, want %d", len(got), want)
	}
	for _, pr := range got {
		if pr.UpdatedAt.Before(cutoff) {
			t.Errorf("PR #%d updatedAt=%v is before cutoff %v", pr.Number, pr.UpdatedAt, cutoff)
		}
	}
	// Phase 1 (light walk) stops after the first page because the cutoff is
	// within the first page (page size 25); phase 2 covers the window with a
	// single full page. Allow slack for boundary ties.
	if got := atomic.LoadInt32(&count); got > 3 {
		t.Errorf("delta made %d requests, want at most 3 (walks did not stop early)", got)
	}
}

// TestClient_FetchPRsUpdated_SizesPagesToWindow verifies the full-payload page
// size follows the number of in-window PRs, so an idle delta (one boundary PR)
// does not fetch a whole page of unchanged PRs.
func TestClient_FetchPRsUpdated_SizesPagesToWindow(t *testing.T) {
	cfg := mockserver.SimulationConfig{
		NumUsers:          3,
		Repos:             []string{"acme/big"},
		History:           60 * 24 * time.Hour,
		IssuesPerRepo:     5,
		PRsPerRepo:        250,
		CommentsPerIssue:  0,
		CommentsPerPR:     0,
		CloseRate:         0.3,
		MergeRate:         0.3,
		DraftRate:         0.1,
		ReviewRate:        0.5,
		Seed:              11,
		AssigneesPerIssue: 0,
		AssigneesPerPR:    1,
		LabelsPerItem:     1,
		MilestonesPerRepo: 1,
	}
	mock := mockserver.NewServer(mockserver.Generate(cfg))
	t.Cleanup(mock.Close)

	// Record the `first` variable of every full-payload page.
	var mu sync.Mutex
	var pageSizes []int
	target, err := url.Parse(mock.URL())
	if err != nil {
		t.Fatalf("parsing mock URL: %v", err)
	}
	sizing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			var req struct {
				Query     string                 `json:"query"`
				Variables map[string]interface{} `json:"variables"`
			}
			// The light scan selects only number and updatedAt; the full page
			// is the one carrying additions/deletions.
			if json.Unmarshal(body, &req) == nil && strings.Contains(req.Query, "additions") {
				if f, ok := req.Variables["first"].(float64); ok {
					mu.Lock()
					pageSizes = append(pageSizes, int(f))
					mu.Unlock()
				}
			}
		}
		httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
	}))
	t.Cleanup(sizing.Close)

	client, err := github.NewClientWithURL(sizing.URL, "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	all, err := client.FetchAllPRs("acme", "big", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}

	// Cutoff at the newest PR: the window is just the boundary item(s).
	desc := append([]*github.PullRequest(nil), all...)
	sort.Slice(desc, func(i, j int) bool { return desc[i].UpdatedAt.After(desc[j].UpdatedAt) })
	cutoff := desc[0].UpdatedAt
	want := 0
	for _, pr := range all {
		if !pr.UpdatedAt.Before(cutoff) {
			want++
		}
	}

	mu.Lock()
	pageSizes = nil
	mu.Unlock()

	got, err := client.FetchPRsUpdated("acme", "big", cutoff, nil)
	if err != nil {
		t.Fatalf("FetchPRsUpdated: %v", err)
	}
	if len(got) != want {
		t.Errorf("delta returned %d PRs, want %d", len(got), want)
	}

	mu.Lock()
	sizes := append([]int(nil), pageSizes...)
	mu.Unlock()
	if len(sizes) == 0 {
		t.Fatal("no full-payload page was requested")
	}
	maxPage := want + 1
	if maxPage > 25 {
		maxPage = 25
	}
	for _, size := range sizes {
		if size < 1 || size > maxPage {
			t.Errorf("full-payload page requested %d PR(s) for a %d-PR window; want between 1 and %d", size, want, maxPage)
		}
	}
	if want < 25 && sizes[0] != maxPage {
		t.Errorf("first full-payload page requested %d PR(s), want %d", sizes[0], maxPage)
	}
}

// TestClient_FetchPRsUpdated_ParallelPages verifies a window spanning several
// pages is assembled completely when pages are fetched concurrently.
func TestClient_FetchPRsUpdated_ParallelPages(t *testing.T) {
	cfg := mockserver.SimulationConfig{
		NumUsers:          3,
		Repos:             []string{"acme/big"},
		History:           60 * 24 * time.Hour,
		IssuesPerRepo:     5,
		PRsPerRepo:        250, // 10 pages of 25
		CommentsPerIssue:  0,
		CommentsPerPR:     0,
		CloseRate:         0.3,
		MergeRate:         0.3,
		DraftRate:         0.1,
		ReviewRate:        0.5,
		Seed:              11,
		AssigneesPerIssue: 0,
		AssigneesPerPR:    1,
		LabelsPerItem:     1,
		MilestonesPerRepo: 1,
	}
	srv := startServer(t, mockserver.Generate(cfg))

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	all, err := client.FetchAllPRs("acme", "big", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}

	desc := append([]*github.PullRequest(nil), all...)
	sort.Slice(desc, func(i, j int) bool { return desc[i].UpdatedAt.After(desc[j].UpdatedAt) })
	cutoff := desc[99].UpdatedAt // 100th newest → 4 pages of 25
	want := map[int]bool{}
	for _, pr := range all {
		if !pr.UpdatedAt.Before(cutoff) {
			want[pr.Number] = true
		}
	}

	got, err := client.FetchPRsUpdated("acme", "big", cutoff, nil)
	if err != nil {
		t.Fatalf("FetchPRsUpdated: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("delta returned %d PRs, want %d", len(got), len(want))
	}
	seen := map[int]bool{}
	for _, pr := range got {
		if pr.UpdatedAt.Before(cutoff) {
			t.Errorf("PR #%d updatedAt=%v is before cutoff %v", pr.Number, pr.UpdatedAt, cutoff)
		}
		seen[pr.Number] = true
	}
	for n := range want {
		if !seen[n] {
			t.Errorf("PR #%d updated at or after the cutoff is missing from the delta result", n)
		}
	}
}

// TestClient_CachesChecksAndReviewThreads covers the head-commit rollup and
// thread counts riding along with a cached pull request, and the on-demand
// read of the individual checks behind the rollup.
func TestClient_CachesChecksAndReviewThreads(t *testing.T) {
	now := time.Now().UTC()
	pr := github.PullRequest{
		Number:        1,
		Title:         "Ship it",
		State:         "OPEN",
		Author:        github.Actor{Login: "alice"},
		BaseRefName:   "main",
		HeadRefName:   "feat/ship",
		HeadRefOid:    "0123456789abcdef0123456789abcdef01234567",
		CreatedAt:     now.Add(-24 * time.Hour),
		UpdatedAt:     now,
		URL:           "https://github.com/acme/myproject/pull/1",
		Checks:        &github.PRChecks{State: "FAILURE"},
		ReviewThreads: &github.PRThreadCounts{Total: 3, Open: 1},
	}
	scenario := mockserver.NewScenario(
		mockserver.WithScenarioPR("acme", "myproject", pr),
		mockserver.WithScenarioPRChecks("acme", "myproject", 1, []github.PRCheck{
			{Type: "CheckRun", Name: "build", Status: "COMPLETED", Conclusion: "FAILURE", URL: "https://ci.example/build"},
			{Type: "StatusContext", Name: "ci/unit", State: "SUCCESS", Description: "All tests passed", URL: "https://ci.example/unit"},
		}),
	)
	srv := startServer(t, scenario)

	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	prs, err := client.FetchAllPRs("acme", "myproject", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	if prs[0].Checks == nil || *prs[0].Checks != (github.PRChecks{State: "FAILURE", Total: 2}) {
		t.Errorf("checks = %+v, want FAILURE with 2 contexts", prs[0].Checks)
	}
	if prs[0].ReviewThreads == nil || *prs[0].ReviewThreads != (github.PRThreadCounts{Total: 3, Open: 1}) {
		t.Errorf("review threads = %+v, want 1 open of 3", prs[0].ReviewThreads)
	}

	rollup, err := client.GetPRChecks("acme", "myproject", 1)
	if err != nil {
		t.Fatalf("GetPRChecks: %v", err)
	}
	if rollup.State != "FAILURE" || rollup.Total != 2 {
		t.Errorf("rollup = %+v, want FAILURE with 2 checks", rollup)
	}
	if len(rollup.Checks) != 2 {
		t.Fatalf("checks = %+v, want 2", rollup.Checks)
	}
	if got := rollup.Checks[0]; got.Name != "build" || got.Conclusion != "FAILURE" || got.URL != "https://ci.example/build" {
		t.Errorf("check run = %+v, want the failing build", got)
	}
	if got := rollup.Checks[1]; got.Type != "StatusContext" || got.Name != "ci/unit" || got.State != "SUCCESS" || got.Description != "All tests passed" {
		t.Errorf("status context = %+v, want the passing unit status", got)
	}
}

// TestClient_WalkPRsAscUntil verifies the ascending walk stops at an exclusive
// updatedAt bound and returns only items before it.
func TestClient_WalkPRsAscUntil(t *testing.T) {
	cfg := mockserver.SimulationConfig{
		NumUsers:      3,
		Repos:         []string{"acme/big"},
		History:       60 * 24 * time.Hour,
		IssuesPerRepo: 5,
		PRsPerRepo:    250,
		CloseRate:     0.3,
		MergeRate:     0.3,
		DraftRate:     0.1,
		ReviewRate:    0.5,
		Seed:          31,
	}
	srv := mockserver.NewServer(mockserver.Generate(cfg))
	t.Cleanup(srv.Close)
	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	all, err := client.FetchPRsAsc("acme", "big", "", nil)
	if err != nil {
		t.Fatalf("FetchPRsAsc: %v", err)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt.Before(all[j].UpdatedAt) })
	cutoff := all[100].UpdatedAt

	var got []*github.PullRequest
	if err := client.WalkPRsAscUntil("acme", "big", "", cutoff, func(batch []*github.PullRequest, _ int, _ string, _ bool) error {
		got = append(got, batch...)
		return nil
	}); err != nil {
		t.Fatalf("WalkPRsAscUntil: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("bounded walk returned %d PRs, want 100", len(got))
	}
	for _, pr := range got {
		if !pr.UpdatedAt.Before(cutoff) {
			t.Errorf("PR #%d updatedAt=%v is at or after the cutoff %v", pr.Number, pr.UpdatedAt, cutoff)
		}
	}
}

// TestClient_FetchIssuesUntil verifies the issue walk drops items at or after
// the exclusive bound.
func TestClient_FetchIssuesUntil(t *testing.T) {
	srv := startServer(t, sampleScenario())
	client, err := github.NewClientWithURL(srv.URL(), "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	all, err := client.FetchAllIssues("acme", "myproject", nil, nil)
	if err != nil {
		t.Fatalf("FetchAllIssues: %v", err)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt.Before(all[j].UpdatedAt) })
	cutoff := all[2].UpdatedAt

	got, err := client.FetchIssuesUntil("acme", "myproject", nil, &cutoff, nil)
	if err != nil {
		t.Fatalf("FetchIssuesUntil: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("bounded issue walk returned %d issues, want 2", len(got))
	}
	for _, issue := range got {
		if !issue.UpdatedAt.Before(cutoff) {
			t.Errorf("issue #%d updatedAt=%v is at or after the cutoff %v", issue.Number, issue.UpdatedAt, cutoff)
		}
	}
}
