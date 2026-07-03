package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

func newTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return &Store{baseDir: dir}
}

func sampleIssue(n int) *github.Issue {
	return &github.Issue{
		Number:    n,
		Title:     "Sample issue",
		State:     "OPEN",
		Author:    github.Actor{Login: "alice"},
		Labels:    []github.Label{{Name: "bug", Color: "d73a4a"}},
		CreatedAt: time.Now().Add(-24 * time.Hour),
		UpdatedAt: time.Now(),
		URL:       "https://github.com/owner/repo/issues/1",
		Body:      "Issue body text.",
		Comments: []github.Comment{
			{ID: "c1", Author: github.Actor{Login: "bob"}, Body: "A comment.", CreatedAt: time.Now()},
		},
	}
}

func samplePR(n int) *github.PullRequest {
	return &github.PullRequest{
		Number:      n,
		Title:       "Sample PR",
		State:       "OPEN",
		Author:      github.Actor{Login: "carol"},
		BaseRefName: "main",
		HeadRefName: "feature-x",
		CreatedAt:   time.Now().Add(-2 * time.Hour),
		UpdatedAt:   time.Now(),
		URL:         "https://github.com/owner/repo/pull/1",
		Body:        "PR body text.",
	}
}

func TestSaveLoadIssue(t *testing.T) {
	s := newTempStore(t)
	issue := sampleIssue(42)

	if err := s.SaveIssue("github.com", "owner", "repo", issue); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	path := filepath.Join(s.baseDir, "github.com", "owner", "repo", "issues", "42.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	loaded, mtime, err := s.LoadIssue("github.com", "owner", "repo", 42)
	if err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	if loaded.Number != 42 {
		t.Errorf("Number: got %d, want 42", loaded.Number)
	}
	if loaded.Title != issue.Title {
		t.Errorf("Title: got %q, want %q", loaded.Title, issue.Title)
	}
	if len(loaded.Comments) != 1 {
		t.Errorf("Comments: got %d, want 1", len(loaded.Comments))
	}
	if mtime.IsZero() {
		t.Error("mtime should not be zero")
	}
}

func TestSaveLoadPR(t *testing.T) {
	s := newTempStore(t)
	pr := samplePR(7)

	if err := s.SavePR("github.com", "owner", "repo", pr); err != nil {
		t.Fatalf("SavePR: %v", err)
	}

	loaded, _, err := s.LoadPR("github.com", "owner", "repo", 7)
	if err != nil {
		t.Fatalf("LoadPR: %v", err)
	}
	if loaded.Number != 7 {
		t.Errorf("Number: got %d, want 7", loaded.Number)
	}
	if loaded.HeadRefName != "feature-x" {
		t.Errorf("HeadRefName: got %q, want feature-x", loaded.HeadRefName)
	}
}

func TestLoadAllIssues(t *testing.T) {
	s := newTempStore(t)

	for _, n := range []int{1, 2, 3} {
		issue := sampleIssue(n)
		issue.Number = n
		if err := s.SaveIssue("github.com", "owner", "repo", issue); err != nil {
			t.Fatalf("SaveIssue(%d): %v", n, err)
		}
	}

	issues, err := s.LoadAllIssues("github.com", "owner", "repo")
	if err != nil {
		t.Fatalf("LoadAllIssues: %v", err)
	}
	if len(issues) != 3 {
		t.Errorf("got %d issues, want 3", len(issues))
	}
}

func TestLoadAllIssues_Empty(t *testing.T) {
	s := newTempStore(t)
	issues, err := s.LoadAllIssues("github.com", "owner", "repo")
	if err != nil {
		t.Fatalf("LoadAllIssues on empty dir: %v", err)
	}
	if issues != nil {
		t.Errorf("expected nil, got %v", issues)
	}
}

func TestCacheInfo(t *testing.T) {
	s := newTempStore(t)

	if err := s.SaveCacheInfo("github.com", "owner", "repo", 120); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}

	info, err := s.LoadCacheInfo("github.com", "owner", "repo")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info.Duration != 120 {
		t.Errorf("Duration: got %d, want 120", info.Duration)
	}
	if time.Since(info.CachedAt) > 5*time.Second {
		t.Errorf("CachedAt too old: %v", info.CachedAt)
	}
}

func TestIsCacheFresh(t *testing.T) {
	s := newTempStore(t)

	// No cache_info yet → not fresh.
	if fresh, _ := s.IsCacheFresh("github.com", "owner", "repo"); fresh {
		t.Error("expected not fresh when no cache_info exists")
	}

	// Write a fresh cache_info with 60-minute duration.
	if err := s.SaveCacheInfo("github.com", "owner", "repo", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	if fresh, err := s.IsCacheFresh("github.com", "owner", "repo"); !fresh || err != nil {
		t.Errorf("expected fresh, got fresh=%v err=%v", fresh, err)
	}
}

func TestIsCacheFreshWithDuration_Stale(t *testing.T) {
	s := newTempStore(t)

	// Save a cache_info with duration=60, then check with duration=0.
	if err := s.SaveCacheInfo("github.com", "owner", "repo", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	// duration=0 means "always consider stale".
	if fresh, _ := s.IsCacheFreshWithDuration("github.com", "owner", "repo", 0); fresh {
		t.Error("expected not fresh with duration=0")
	}
}

func TestSaveCacheInfoFull_PersistsCursorsAndComplete(t *testing.T) {
	s := newTempStore(t)

	cursor := time.Date(2024, 1, 15, 12, 30, 0, 0, time.UTC)
	if err := s.SaveCacheInfoFull("github.com", "owner", "repo", &CacheInfo{
		CachedAt:    time.Now(),
		Duration:    60,
		Complete:    false,
		IssueCursor: &cursor,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}

	info, err := s.LoadCacheInfo("github.com", "owner", "repo")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info.Complete {
		t.Error("Complete should be false for a partial fetch")
	}
	if info.IssueCursor == nil || !info.IssueCursor.Equal(cursor) {
		t.Errorf("IssueCursor = %v, want %v", info.IssueCursor, cursor)
	}
	// An incomplete cache must not be considered fresh even within duration.
	if fresh, _ := s.IsCacheFresh("github.com", "owner", "repo"); fresh {
		t.Error("incomplete cache should not be fresh")
	}
}

func TestIsCacheFresh_RequiresComplete(t *testing.T) {
	s := newTempStore(t)

	// Persist an incomplete cache (e.g. an interrupted fetch advanced the cursor
	// but never marked completion). It must not be served as fresh.
	cursor := time.Now()
	if err := s.SaveCacheInfoFull("github.com", "owner", "repo", &CacheInfo{
		CachedAt:    time.Now(),
		Duration:    60,
		Complete:    false,
		IssueCursor: &cursor,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}
	if fresh, _ := s.IsCacheFresh("github.com", "owner", "repo"); fresh {
		t.Error("incomplete cache should not be fresh")
	}
}
