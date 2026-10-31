package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

func TestParseSinceDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "date only is UTC midnight",
			input: "2026-09-01",
			want:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "RFC3339 UTC",
			input: "2026-09-01T15:04:05Z",
			want:  time.Date(2026, 9, 1, 15, 4, 5, 0, time.UTC),
		},
		{
			name:  "RFC3339 with offset",
			input: "2026-09-01T10:00:00-05:00",
			want:  time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC),
		},
		{
			name:  "naive datetime local",
			input: "2026-09-01 15:04",
			want:  time.Date(2026, 9, 1, 15, 4, 0, 0, time.Local),
		},
		{
			name:  "naive datetime with seconds",
			input: "2026-09-01 15:04:05",
			want:  time.Date(2026, 9, 1, 15, 4, 5, 0, time.Local),
		},
		{
			name:    "empty is invalid",
			input:   "",
			wantErr: true,
		},
		{
			name:    "garbage is invalid",
			input:   "yesterday",
			wantErr: true,
		},
		{
			name:    "partial date is invalid",
			input:   "2026-09",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSinceDate(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSinceDate(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSinceDate(%q): %v", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseSinceDate(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSinceDate_TrimsWhitespace(t *testing.T) {
	got, err := parseSinceDate(" 2026-09-01 ")
	if err != nil {
		t.Fatalf("parseSinceDate: %v", err)
	}
	if want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// setCacheTestEnv points the package-level flags used by runCache at the mock
// server and a temp cache dir, restoring the previous values on cleanup.
func setCacheTestEnv(t *testing.T, apiURL, dir string) {
	t.Helper()
	oldRepo, oldAPI, oldDir := repoFlag, apiURLFlag, cacheDir
	oldStorage := storageFlag
	oldDur, oldForce, oldIfStale, oldSince, oldType, oldRetries := cacheDuration, cacheForce, cacheIfStale, cacheSince, cacheType, cacheRetries
	oldWorkers := cacheWorkers
	t.Cleanup(func() {
		repoFlag, apiURLFlag, cacheDir = oldRepo, oldAPI, oldDir
		storageFlag = oldStorage
		cacheDuration, cacheForce, cacheIfStale, cacheSince, cacheType, cacheRetries = oldDur, oldForce, oldIfStale, oldSince, oldType, oldRetries
		cacheWorkers = oldWorkers
	})
	repoFlag = "acme/myproject"
	apiURLFlag = apiURL
	cacheDir = dir
	// These tests inspect the cache through a file store, so pin the file
	// backend regardless of the (SQLite) default.
	storageFlag = "file"
	cacheDuration = 60
	cacheForce = false
	cacheIfStale = false
	cacheSince = ""
	cacheType = "both"
	cacheRetries = 2
	cacheWorkers = 2
}

// captureStdout redirects os.Stdout while fn runs and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	r.Close()
	return buf.String()
}

func runCacheCapture(t *testing.T) string {
	t.Helper()
	var out string
	func() {
		defer func() {
			if err := recover(); err != nil {
				t.Fatalf("runCache panicked: %v", err)
			}
		}()
		out = captureStdout(t, func() {
			if err := runCache(nil, nil); err != nil {
				t.Fatalf("runCache: %v", err)
			}
		})
	}()
	return out
}

var (
	cachedIssuesRe = regexp.MustCompile(`Cached (\d+) issue\(s\)`)
	cachedPRsRe    = regexp.MustCompile(`Cached (\d+) pull request\(s\)`)
)

func parseCachedCount(t *testing.T, out string, re *regexp.Regexp) int {
	t.Helper()
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("output %q does not contain %s", out, re)
	}
	var n int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
		t.Fatalf("parsing count from %q: %v", m[1], err)
	}
	return n
}

func sinceScenario(now time.Time) *mockserver.Scenario {
	return mockserver.NewScenarioBuilder("acme", "myproject").
		WithSeed(42).
		WithNow(now).
		AddIssue("Issue: 30 days old", "old", 30*24*time.Hour).
		AddIssue("Issue: 10 days old", "mid", 10*24*time.Hour).
		AddIssue("Issue: 1 day old", "new", 1*24*time.Hour).
		AddPR("PR: 20 days old", "old", "old", 20*24*time.Hour).
		AddPR("PR: 1 day old", "new", "new", 1*24*time.Hour).
		Build()
}

// TestCache_SinceRefreshesFreshCache verifies that --since on an already
// covered cache bypasses --if-stale but has nothing left to fetch.
func TestCache_SinceRefreshesFreshCache(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	// First run: full cold fetch.
	out := runCacheCapture(t)
	if !strings.Contains(out, "Caching issues for acme/myproject") {
		t.Errorf("expected cold fetch output, got %q", out)
	}

	// The cache is now fresh; with --if-stale a plain run does nothing.
	cacheIfStale = true
	out = runCacheCapture(t)
	if !strings.Contains(out, "Cache is still fresh") {
		t.Errorf("expected --if-stale short-circuit, got %q", out)
	}
	cacheIfStale = false

	cutoff := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

	// --since run on the fresh cache: the range is already covered.
	cacheSince = "2026-08-25"
	out = runCacheCapture(t)
	if strings.Contains(out, "Cache is still fresh") {
		t.Error("--since should bypass the --if-stale short-circuit")
	}
	if !strings.Contains(out, "Fetching issues created or updated since 2026-08-25 00:00") {
		t.Errorf("expected --since issue message, got %q", out)
	}
	if !strings.Contains(out, "Fetching pull requests updated since 2026-08-25 00:00") {
		t.Errorf("expected --since PR message, got %q", out)
	}
	if got := parseCachedCount(t, out, cachedIssuesRe); got != 0 {
		t.Errorf("cached %d issue(s) with --since, want 0 (already covered)", got)
	}
	if got := parseCachedCount(t, out, cachedPRsRe); got != 0 {
		t.Errorf("cached %d pull request(s) with --since, want 0 (already covered)", got)
	}

	store := cache.NewStoreWithPath(cacheDir)
	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("cache should remain complete after a --since run")
	}
	if info.PRFloor == nil || info.PRFloor.After(cutoff) {
		t.Errorf("PR floor = %v, want coverage starting at or before %v", info.PRFloor, cutoff)
	}
	segs, err := store.LoadSegments("github.com", "acme", "myproject", "prs")
	if err != nil {
		t.Fatalf("LoadSegments: %v", err)
	}
	if !cache.Covers(segs, cutoff, time.Now().UTC()) {
		t.Errorf("coverage does not include [%v, now]: %+v", cutoff, segs)
	}
}

// TestCache_SinceOnIncompleteCache verifies --since still applies when the
// legacy cursor only covered part of history: the uncovered range is filled.
func TestCache_SinceOnIncompleteCache(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	// Simulate an interrupted cold fetch: the legacy issue cursor is mid-history.
	store := cache.NewStoreWithPath(cacheDir)
	cur := now.Add(-48 * time.Hour)
	if err := store.SaveCacheInfoFull("github.com", "acme", "myproject", &cache.CacheInfo{
		Complete:    false,
		IssueCursor: &cur,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}

	cacheSince = "2026-08-25"
	out := runCacheCapture(t)
	if !strings.Contains(out, "Fetching issues created or updated since 2026-08-25 00:00") {
		t.Errorf("expected the issue --since message, got %q", out)
	}
	if !strings.Contains(out, "Fetching pull requests updated since 2026-08-25 00:00") {
		t.Errorf("expected the PR --since message, got %q", out)
	}
	// The issue cursor covered through Aug 29; only the newer issue is fetched.
	if got := parseCachedCount(t, out, cachedIssuesRe); got != 1 {
		t.Errorf("cached %d issue(s), want 1", got)
	}
	if got := parseCachedCount(t, out, cachedPRsRe); got != 1 {
		t.Errorf("cached %d pull request(s), want 1", got)
	}

	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("the run should mark the cache complete")
	}
	if info.PRFloor == nil || !info.PRFloor.Equal(time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("PR floor = %v, want the --since cutoff", info.PRFloor)
	}
}

// TestCache_SinceSeeksPRWalkAtFloor verifies --since starts the pull request
// coverage at the cutoff instead of walking the repository's history.
func TestCache_SinceSeeksPRWalkAtFloor(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	cacheType = "prs"
	cacheSince = "2026-08-25"
	out := runCacheCapture(t)
	if !strings.Contains(out, "Fetching pull requests updated since 2026-08-25 00:00") {
		t.Errorf("expected the PR date seek message, got %q", out)
	}
	if got := parseCachedCount(t, out, cachedPRsRe); got != 1 {
		t.Errorf("cached %d pull request(s), want 1", got)
	}

	store := cache.NewStoreWithPath(cacheDir)
	prs, err := store.LoadAllPRs("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadAllPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("cached %d PRs, want 1", len(prs))
	}
	cutoff := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	if prs[0].UpdatedAt.Before(cutoff) {
		t.Errorf("PR #%d updatedAt=%v is before the floor %v", prs[0].Number, prs[0].UpdatedAt, cutoff)
	}

	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info.PRFloor == nil || !info.PRFloor.Equal(cutoff) {
		t.Errorf("PR floor = %v, want %v", info.PRFloor, cutoff)
	}
	segs, err := store.LoadSegments("github.com", "acme", "myproject", "prs")
	if err != nil {
		t.Fatalf("LoadSegments: %v", err)
	}
	if len(segs) == 0 || !segs[0].Start.Equal(cutoff) || !segs[0].End.After(cutoff) {
		t.Errorf("coverage = %+v, want a segment starting at %v", segs, cutoff)
	}
}

// TestCache_RejectedDateCursorLeavesGap verifies a rejected synthetic cursor
// surfaces as an error and leaves the range uncovered for the next run.
func TestCache_RejectedDateCursorLeavesGap(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	scenario := sinceScenario(now)
	scenario.RejectSyntheticCursors = true
	srv := mockserver.NewServer(scenario)
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	cacheType = "prs"
	cacheSince = "2026-08-25"
	var err error
	captureStdout(t, func() { err = runCache(nil, nil) })
	if err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("expected the rejected cursor error, got %v", err)
	}

	store := cache.NewStoreWithPath(cacheDir)
	segs, err := store.LoadSegments("github.com", "acme", "myproject", "prs")
	if err != nil {
		t.Fatalf("LoadSegments: %v", err)
	}
	if len(segs) != 0 {
		t.Errorf("rejected segment should not be marked covered: %+v", segs)
	}
}

// TestCache_MigratesLegacyCursorToCoverage verifies the old ascending page
// cursor becomes a covered prefix, so only the range after it is fetched.
func TestCache_MigratesLegacyCursorToCoverage(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	store := cache.NewStoreWithPath(cacheDir)
	cur := now.Add(-15 * 24 * time.Hour)
	if err := store.SaveCacheInfoFull("github.com", "acme", "myproject", &cache.CacheInfo{
		Complete:     false,
		PRCursor:     &cur,
		PRPageCursor: "1",
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}

	out := runCacheCapture(t)
	if !strings.Contains(out, "Fetching pull requests updated since") {
		t.Errorf("expected the post-migration PR message, got %q", out)
	}
	// The old PR is below the legacy cursor; only the newer one is fetched.
	if got := parseCachedCount(t, out, cachedPRsRe); got != 1 {
		t.Errorf("migrated run cached %d pull request(s), want 1", got)
	}

	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info.PRPageCursor != "" {
		t.Errorf("page cursor = %q, want empty after migration", info.PRPageCursor)
	}
	segs, err := store.LoadSegments("github.com", "acme", "myproject", "prs")
	if err != nil {
		t.Fatalf("LoadSegments: %v", err)
	}
	if !cache.Covers(segs, cur, time.Now().UTC()) {
		t.Errorf("coverage does not include [%v, now]: %+v", cur, segs)
	}
}

// TestCache_SinceFillsGap verifies a lower --since fills only the uncovered
// gap and does not re-fetch the already covered window.
func TestCache_SinceFillsGap(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	scenario := sinceScenario(now)
	srv := mockserver.NewServer(scenario)
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	_ = runCacheCapture(t) // full cache

	cutoff := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	coveredStart := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	store := cache.NewStoreWithPath(cacheDir)
	for _, kind := range []string{"issues", "prs"} {
		if err := store.DeleteSegments("github.com", "acme", "myproject", kind); err != nil {
			t.Fatalf("DeleteSegments: %v", err)
		}
		if err := store.SaveSegments("github.com", "acme", "myproject", kind, []cache.Segment{{
			Start: coveredStart, End: time.Now().UTC(),
		}}); err != nil {
			t.Fatalf("SaveSegments: %v", err)
		}
	}

	wantIssues, wantPRs := 0, 0
	for _, si := range scenario.Issues {
		if !si.Issue.UpdatedAt.Before(cutoff) && si.Issue.UpdatedAt.Before(coveredStart) {
			wantIssues++
		}
	}
	for _, sp := range scenario.PRs {
		if !sp.PullRequest.UpdatedAt.Before(cutoff) && sp.PullRequest.UpdatedAt.Before(coveredStart) {
			wantPRs++
		}
	}

	cacheSince = "2026-08-01"
	out := runCacheCapture(t)
	if got := parseCachedCount(t, out, cachedIssuesRe); got != wantIssues {
		t.Errorf("cached %d issue(s) in the gap, want %d", got, wantIssues)
	}
	if got := parseCachedCount(t, out, cachedPRsRe); got != wantPRs {
		t.Errorf("cached %d pull request(s) in the gap, want %d", got, wantPRs)
	}
	for _, kind := range []string{"issues", "prs"} {
		segs, err := store.LoadSegments("github.com", "acme", "myproject", kind)
		if err != nil {
			t.Fatalf("LoadSegments: %v", err)
		}
		if !cache.Covers(segs, cutoff, time.Now().UTC()) {
			t.Errorf("%s coverage does not include [%v, now]: %+v", kind, cutoff, segs)
		}
	}
}

// TestCache_SinceInvalidInput covers --since argument validation.
func TestCache_SinceInvalidInput(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	cacheSince = "not-a-date"
	err := runCache(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid --since") {
		t.Errorf("expected invalid --since error, got %v", err)
	}
}

// TestCache_ForceSinceRebuildsWindow verifies --force with --since deletes the
// selected type's cached items and coverage, then fetches only the window.
func TestCache_ForceSinceRebuildsWindow(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	_ = runCacheCapture(t) // both types cached

	cacheForce = true
	cacheSince = "2026-08-25"
	cacheType = "prs"
	out := runCacheCapture(t)
	if got := parseCachedCount(t, out, cachedPRsRe); got != 1 {
		t.Errorf("rebuilt window cached %d pull request(s), want 1", got)
	}

	store := cache.NewStoreWithPath(cacheDir)
	prs, err := store.LoadAllPRs("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadAllPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("cached %d PRs after the rebuild, want 1", len(prs))
	}
	cutoff := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	if prs[0].UpdatedAt.Before(cutoff) {
		t.Errorf("PR #%d updatedAt=%v is before the rebuild cutoff %v", prs[0].Number, prs[0].UpdatedAt, cutoff)
	}
	issues, err := store.LoadAllIssues("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadAllIssues: %v", err)
	}
	if len(issues) != 3 {
		t.Errorf("issue cache should be untouched, got %d issues", len(issues))
	}
	segs, err := store.LoadSegments("github.com", "acme", "myproject", "prs")
	if err != nil {
		t.Fatalf("LoadSegments: %v", err)
	}
	if len(segs) == 0 || segs[0].Start.Before(cutoff) {
		t.Errorf("PR coverage = %+v, want a segment starting at or after %v", segs, cutoff)
	}
}
