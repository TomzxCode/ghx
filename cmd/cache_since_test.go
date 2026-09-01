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
	"github.com/tomzxcode/ghx/internal/github"
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
	oldDur, oldForce, oldSince, oldType, oldRetries := cacheDuration, cacheForce, cacheSince, cacheType, cacheRetries
	t.Cleanup(func() {
		repoFlag, apiURLFlag, cacheDir = oldRepo, oldAPI, oldDir
		cacheDuration, cacheForce, cacheSince, cacheType, cacheRetries = oldDur, oldForce, oldSince, oldType, oldRetries
	})
	repoFlag = "acme/myproject"
	apiURLFlag = apiURL
	cacheDir = dir
	cacheDuration = 60
	cacheForce = false
	cacheSince = ""
	cacheType = "both"
	cacheRetries = 2
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

// TestCache_SinceRefreshesFreshCache verifies that --since performs a targeted
// refresh on an otherwise fresh cache: it bypasses the freshness short-circuit,
// fetches only entries updated on/after the date, and keeps the cache complete.
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

	// The cache is now fresh; a plain run must short-circuit.
	out = runCacheCapture(t)
	if !strings.Contains(out, "Cache is still fresh") {
		t.Errorf("expected fresh-cache short-circuit, got %q", out)
	}

	// Count what a --since 2026-08-25 refresh should return, according to the
	// scenario itself.
	client, err := github.NewClientWithURL(srv.URL(), "test-token", "github.com")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	cutoff := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	allIssues, err := client.FetchAllIssues("acme", "myproject", nil, nil)
	if err != nil {
		t.Fatalf("FetchAllIssues: %v", err)
	}
	wantIssues := 0
	for _, is := range allIssues {
		if !is.UpdatedAt.Before(cutoff) {
			wantIssues++
		}
	}
	allPRs, err := client.FetchAllPRs("acme", "myproject", nil)
	if err != nil {
		t.Fatalf("FetchAllPRs: %v", err)
	}
	wantPRs := 0
	for _, pr := range allPRs {
		if !pr.UpdatedAt.Before(cutoff) {
			wantPRs++
		}
	}

	// --since run on the fresh cache.
	cacheSince = "2026-08-25"
	out = runCacheCapture(t)
	if strings.Contains(out, "Cache is still fresh") {
		t.Error("--since should bypass the freshness short-circuit")
	}
	if !strings.Contains(out, "Fetching issues created or updated since 2026-08-25 00:00") {
		t.Errorf("expected --since issue fetch message, got %q", out)
	}
	if !strings.Contains(out, "Fetching pull requests created or updated since 2026-08-25 00:00") {
		t.Errorf("expected --since PR fetch message, got %q", out)
	}
	if got := parseCachedCount(t, out, cachedIssuesRe); got != wantIssues {
		t.Errorf("cached %d issue(s) with --since, want %d", got, wantIssues)
	}
	if got := parseCachedCount(t, out, cachedPRsRe); got != wantPRs {
		t.Errorf("cached %d pull request(s) with --since, want %d", got, wantPRs)
	}

	info, err := (cache.NewStoreWithPath(cacheDir)).LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("cache should remain complete after a --since refresh")
	}
}

// TestCache_SinceIgnoredWhenIncomplete verifies --since does not mark an
// interrupted cache complete; the full fetch is resumed instead.
func TestCache_SinceIgnoredWhenIncomplete(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	// Simulate an interrupted cold fetch: cursor mid-history, not complete.
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
	if !strings.Contains(out, "resuming the full fetch (--since ignored)") {
		t.Errorf("expected --since to be ignored, got %q", out)
	}
	if strings.Contains(out, "created or updated since") {
		t.Errorf("--since should not drive the fetch, got %q", out)
	}
	if !strings.Contains(out, "Resuming issues from") {
		t.Errorf("expected resume message, got %q", out)
	}

	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("resumed fetch should mark the cache complete")
	}
}

// TestCache_SinceInvalidInputAndForceConflict covers argument validation.
func TestCache_SinceInvalidInputAndForceConflict(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	cacheSince = "not-a-date"
	err := runCache(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid --since") {
		t.Errorf("expected invalid --since error, got %v", err)
	}

	cacheSince = "2026-08-25"
	cacheForce = true
	err = runCache(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "--force and --since cannot be combined") {
		t.Errorf("expected force/since conflict error, got %v", err)
	}
}
