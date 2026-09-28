package cache

import (
	"testing"
	"time"
)

// TestPerTypeCacheFreshness exercises the per-type freshness API on both
// backends, including persistence of the per-type timestamps and the legacy
// fallback to CachedAt when they are unset.
func TestPerTypeCacheFreshness(t *testing.T) {
	factories := map[string]func(t *testing.T) Store{
		"file": func(t *testing.T) Store { return NewStoreWithPath(t.TempDir()) },
		"sqlite": func(t *testing.T) Store {
			s, err := NewSQLiteStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			return s
		},
	}
	const host, owner, repo = "github.com", "acme", "big"

	for name, mk := range factories {
		t.Run(name, func(t *testing.T) {
			s := mk(t)
			defer s.Close()

			now := time.Now()
			old := now.Add(-2 * time.Hour)
			// Issues refreshed 2h ago, PRs just now; duration 60m.
			if err := s.SaveCacheInfoFull(host, owner, repo, &CacheInfo{
				CachedAt:       now,
				Duration:       60,
				Complete:       true,
				IssuesCachedAt: old,
				PRsCachedAt:    now,
			}); err != nil {
				t.Fatalf("SaveCacheInfoFull: %v", err)
			}

			if fresh, _ := s.IsIssuesCacheFresh(host, owner, repo); fresh {
				t.Error("issues should be stale (2h old > 60m duration)")
			}
			if fresh, _ := s.IsPRsCacheFresh(host, owner, repo); !fresh {
				t.Error("PRs should be fresh")
			}
			if fresh, _ := s.IsCacheFresh(host, owner, repo); fresh {
				t.Error("IsCacheFresh should be false when issues are stale")
			}

			info, err := s.LoadCacheInfo(host, owner, repo)
			if err != nil {
				t.Fatalf("LoadCacheInfo: %v", err)
			}
			if !info.IssuesCachedAt.Equal(old) || !info.PRsCachedAt.Equal(now) {
				t.Errorf("per-type timestamps not preserved: issues=%v prs=%v", info.IssuesCachedAt, info.PRsCachedAt)
			}

			// Legacy format: zero per-type timestamps fall back to CachedAt.
			if err := s.SaveCacheInfoFull(host, owner, repo, &CacheInfo{
				CachedAt: now,
				Duration: 60,
				Complete: true,
			}); err != nil {
				t.Fatalf("SaveCacheInfoFull (legacy): %v", err)
			}
			if fresh, _ := s.IsIssuesCacheFresh(host, owner, repo); !fresh {
				t.Error("legacy issues should fall back to CachedAt (fresh)")
			}
			if fresh, _ := s.IsPRsCacheFresh(host, owner, repo); !fresh {
				t.Error("legacy PRs should fall back to CachedAt (fresh)")
			}
		})
	}
}
