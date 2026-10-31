package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

// TestCache_PlainRunDeltaRefreshesFreshCache verifies that a plain run after
// coverage is current does not re-fetch and does not short-circuit.
func TestCache_PlainRunDeltaRefreshesFreshCache(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	// First run: full cold fetch.
	out := runCacheCapture(t)
	if !strings.Contains(out, "Caching issues for acme/myproject") {
		t.Fatalf("expected cold fetch output, got %q", out)
	}

	store := cache.NewStoreWithPath(cacheDir)
	before, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if before == nil || !before.Complete {
		t.Fatal("cache should be complete after the cold fetch")
	}

	// The cache is fresh, but a plain run still performs the delta fetch.
	out = runCacheCapture(t)
	if strings.Contains(out, "Cache is still fresh") {
		t.Errorf("plain run must not short-circuit, got %q", out)
	}
	if strings.Contains(out, "Fetching issues updated since") || strings.Contains(out, "Fetching pull requests updated since") {
		t.Errorf("a covered cache should have no update window, got %q", out)
	}
	// The coverage already reaches now, so the run has nothing to fetch.
	if got := parseCachedCount(t, out, cachedIssuesRe); got != 0 {
		t.Errorf("run cached %d issue(s), want 0", got)
	}
	if got := parseCachedCount(t, out, cachedPRsRe); got != 0 {
		t.Errorf("run cached %d pull request(s), want 0", got)
	}

	after, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if after == nil || !after.Complete {
		t.Error("cache should stay complete after a delta run")
	}
	if after.CachedAt.Before(before.CachedAt) {
		t.Errorf("CachedAt went backwards: %v then %v", before.CachedAt, after.CachedAt)
	}
}

// TestCache_IfStaleSkipsFreshCache verifies --if-stale restores the no-op
// behavior on a fresh cache.
func TestCache_IfStaleSkipsFreshCache(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	_ = runCacheCapture(t) // full cache

	cacheIfStale = true
	out := runCacheCapture(t)
	if !strings.Contains(out, "Cache is still fresh") {
		t.Errorf("expected --if-stale short-circuit, got %q", out)
	}
	if strings.Contains(out, "Fetching issues") || strings.Contains(out, "Fetching pull requests") {
		t.Errorf("--if-stale on a fresh cache must not fetch, got %q", out)
	}
}

// TestCache_IfStaleFetchesWhenStale verifies --if-stale does not block a
// refresh once the cache is older than --cache-duration.
func TestCache_IfStaleFetchesWhenStale(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	_ = runCacheCapture(t) // full cache

	store := cache.NewStoreWithPath(cacheDir)
	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil {
		t.Fatal("cache info missing after cold fetch")
	}
	info.CachedAt = time.Now().Add(-2 * time.Hour)
	if err := store.SaveCacheInfoFull("github.com", "acme", "myproject", info); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}

	cacheIfStale = true
	out := runCacheCapture(t)
	if strings.Contains(out, "Cache is still fresh") {
		t.Errorf("stale cache must not short-circuit, got %q", out)
	}
	if got := parseCachedCount(t, out, cachedIssuesRe); got != 0 {
		t.Errorf("run cached %d issue(s), want 0 (coverage already current)", got)
	}
}
