package cache

import (
	"testing"
	"time"
)

// TestIsItemFresh exercises the per-item freshness window on both backends. The
// window follows the repository's configured cache duration, so an item
// refreshed within it is a hit even when the repository's cache as a whole is
// much older. A repository with no cache metadata falls back to the default.
func TestIsItemFresh(t *testing.T) {
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

	cases := []struct {
		name      string
		info      *CacheInfo // nil leaves the repository without cache metadata
		itemAge   time.Duration
		wantFresh bool
	}{
		{
			name:      "no metadata uses the default window",
			info:      nil,
			itemAge:   DefaultItemFreshness - time.Minute,
			wantFresh: true,
		},
		{
			name:      "no metadata rejects an item past the default window",
			info:      nil,
			itemAge:   DefaultItemFreshness + time.Minute,
			wantFresh: false,
		},
		{
			name:      "configured duration extends the window",
			info:      &CacheInfo{Duration: 1440},
			itemAge:   12 * time.Hour,
			wantFresh: true,
		},
		{
			name:      "configured duration still expires",
			info:      &CacheInfo{Duration: 1440},
			itemAge:   25 * time.Hour,
			wantFresh: false,
		},
		{
			name:      "default duration rejects an item older than an hour",
			info:      &CacheInfo{Duration: 60},
			itemAge:   90 * time.Minute,
			wantFresh: false,
		},
		{
			name:      "zero duration never serves an item",
			info:      &CacheInfo{Duration: 0},
			itemAge:   time.Minute,
			wantFresh: false,
		},
	}

	for name, mk := range factories {
		t.Run(name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					s := mk(t)
					defer s.Close()

					if tc.info != nil {
						info := *tc.info
						info.CachedAt = time.Now().Add(-30 * 24 * time.Hour)
						info.Complete = true
						if err := s.SaveCacheInfoFull(host, owner, repo, &info); err != nil {
							t.Fatalf("SaveCacheInfoFull: %v", err)
						}
					}

					got := IsItemFresh(s, host, owner, repo, time.Now().Add(-tc.itemAge))
					if got != tc.wantFresh {
						t.Errorf("IsItemFresh(item age %v, duration %+v) = %v, want %v",
							tc.itemAge, tc.info, got, tc.wantFresh)
					}
				})
			}
		})
	}
}
