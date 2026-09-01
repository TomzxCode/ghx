package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

// TestCache_TypeInvalid verifies --type rejects unknown values.
func TestCache_TypeInvalid(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	cacheType = "bogus"
	err := runCache(nil, nil)
	if err == nil || !strings.Contains(err.Error(), `invalid --type "bogus"`) {
		t.Errorf("expected invalid --type error, got %v", err)
	}
}

// TestCache_TypeIssuesOnly verifies --type issues refreshes only the issue
// portion, bypasses the freshness short-circuit, and keeps the cache complete.
func TestCache_TypeIssuesOnly(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	// Full cache first; a second full run would short-circuit as fresh.
	_ = runCacheCapture(t)
	out := runCacheCapture(t)
	if !strings.Contains(out, "Cache is still fresh") {
		t.Fatalf("expected fresh-cache short-circuit, got %q", out)
	}

	cacheType = "issues"
	out = runCacheCapture(t)
	if strings.Contains(out, "Cache is still fresh") {
		t.Error("--type issues should bypass the freshness short-circuit")
	}
	if !strings.Contains(out, "Cached ") && !strings.Contains(out, "issue(s)") {
		t.Errorf("expected issue fetch output, got %q", out)
	}
	if !strings.Contains(out, "Skipping pull requests (--type issues)") {
		t.Errorf("expected PR skip notice, got %q", out)
	}
	if strings.Contains(out, "pull request(s)\n") || strings.Contains(out, "Caching pull requests") || strings.Contains(out, "Fetching pull requests") {
		t.Errorf("PRs should not be fetched, got %q", out)
	}

	store := cache.NewStoreWithPath(cacheDir)
	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("cache should remain complete after --type issues on a complete cache")
	}
}

// TestCache_TypePrsOnly verifies --type prs refreshes only the PR portion.
func TestCache_TypePrsOnly(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	_ = runCacheCapture(t) // full cache

	cacheType = "prs"
	out := runCacheCapture(t)
	if !strings.Contains(out, "Skipping issues (--type prs)") {
		t.Errorf("expected issue skip notice, got %q", out)
	}
	if strings.Contains(out, "Caching issues") || strings.Contains(out, "Fetching issues") || strings.Contains(out, "issue(s)") {
		t.Errorf("issues should not be fetched, got %q", out)
	}
	if !strings.Contains(out, "Cached ") || !strings.Contains(out, "pull request(s)") {
		t.Errorf("expected PR fetch output, got %q", out)
	}

	store := cache.NewStoreWithPath(cacheDir)
	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("cache should remain complete after --type prs on a complete cache")
	}
	if info.PRCursor == nil {
		t.Error("PR cursor should be set after a --type prs run")
	}
}

// TestCache_TypePrsDoesNotCompleteIncompleteCache verifies a partial run never
// marks an incomplete cache complete; a later full run finishes the job.
func TestCache_TypePrsDoesNotCompleteIncompleteCache(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	// Simulate an interrupted cold fetch: issue cursor mid-history, no PRs.
	store := cache.NewStoreWithPath(cacheDir)
	cur := now.Add(-48 * time.Hour)
	if err := store.SaveCacheInfoFull("github.com", "acme", "myproject", &cache.CacheInfo{
		Complete:    false,
		IssueCursor: &cur,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}

	cacheType = "prs"
	out := runCacheCapture(t)
	if !strings.Contains(out, "Caching pull requests") {
		t.Errorf("expected cold PR fetch, got %q", out)
	}

	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info.Complete {
		t.Error("--type prs must not mark an incomplete cache complete")
	}
	if info.PRCursor == nil {
		t.Error("PR cursor should be persisted after a --type prs run")
	}

	// A subsequent full run completes both portions.
	cacheType = "both"
	out = runCacheCapture(t)
	if strings.Contains(out, "Skipping") {
		t.Errorf("full run should not skip anything, got %q", out)
	}
	info, err = store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("full run should mark the cache complete")
	}
}
