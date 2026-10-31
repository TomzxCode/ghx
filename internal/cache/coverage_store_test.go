package cache

import (
	"testing"
	"time"
)

func TestCoverageSegmentsRoundTrip(t *testing.T) {
	stores := map[string]Store{
		"file": func() Store { return NewStoreWithPath(t.TempDir()) }(),
		"sqlite": func() Store {
			s, err := NewSQLiteStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			return s
		}(),
	}
	const host, owner, repo = "github.com", "acme", "project"
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			defer store.Close()
			day := func(n int) time.Time { return time.Date(2026, 8, n, 0, 0, 0, 0, time.UTC) }

			if err := store.SaveSegments(host, owner, repo, "prs", []Segment{{Start: day(1), End: day(3)}}); err != nil {
				t.Fatalf("SaveSegments: %v", err)
			}
			if err := store.SaveSegments(host, owner, repo, "prs", []Segment{{Start: day(3), End: day(4)}}); err != nil {
				t.Fatalf("SaveSegments merge: %v", err)
			}
			segs, err := store.LoadSegments(host, owner, repo, "prs")
			if err != nil {
				t.Fatalf("LoadSegments: %v", err)
			}
			if len(segs) != 1 || !segs[0].Start.Equal(day(1)) || !segs[0].End.Equal(day(4)) {
				t.Fatalf("merged segments = %+v, want [%v, %v)", segs, day(1), day(4))
			}

			if err := store.SaveSegments(host, owner, repo, "issues", []Segment{{Start: day(5), End: day(6)}}); err != nil {
				t.Fatalf("SaveSegments issues: %v", err)
			}
			if segs, err := store.LoadSegments(host, owner, repo, "issues"); err != nil || len(segs) != 1 {
				t.Fatalf("issue segments = %+v, %v", segs, err)
			}
			if segs, err := store.LoadSegments(host, owner, repo, "prs"); err != nil || len(segs) != 1 {
				t.Fatalf("PR segments changed with the issue write: %+v, %v", segs, err)
			}

			if err := store.DeleteSegments(host, owner, repo, "prs"); err != nil {
				t.Fatalf("DeleteSegments: %v", err)
			}
			if segs, err := store.LoadSegments(host, owner, repo, "prs"); err != nil || len(segs) != 0 {
				t.Fatalf("PR segments after delete = %+v, %v", segs, err)
			}
			if segs, err := store.LoadSegments(host, owner, repo, "issues"); err != nil || len(segs) != 1 {
				t.Fatalf("issue segments after PR delete = %+v, %v", segs, err)
			}
		})
	}
}

func TestDeleteTypeRemovesItemsAndCoverage(t *testing.T) {
	stores := map[string]Store{
		"file": func() Store { return NewStoreWithPath(t.TempDir()) }(),
		"sqlite": func() Store {
			s, err := NewSQLiteStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			return s
		}(),
	}
	const host, owner, repo = "github.com", "acme", "project"
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			defer store.Close()
			if err := store.SaveIssue(host, owner, repo, richIssue(1)); err != nil {
				t.Fatalf("SaveIssue: %v", err)
			}
			if err := store.SavePR(host, owner, repo, richPR(2)); err != nil {
				t.Fatalf("SavePR: %v", err)
			}
			day := func(n int) time.Time { return time.Date(2026, 8, n, 0, 0, 0, 0, time.UTC) }
			if err := store.SaveSegments(host, owner, repo, "issues", []Segment{{Start: day(1), End: day(2)}}); err != nil {
				t.Fatalf("SaveSegments issues: %v", err)
			}
			if err := store.SaveSegments(host, owner, repo, "prs", []Segment{{Start: day(1), End: day(2)}}); err != nil {
				t.Fatalf("SaveSegments prs: %v", err)
			}

			n, err := store.DeleteType(host, owner, repo, "prs")
			if err != nil {
				t.Fatalf("DeleteType: %v", err)
			}
			if n != 1 {
				t.Errorf("DeleteType deleted %d PRs, want 1", n)
			}
			if issues, err := store.LoadAllIssues(host, owner, repo); err != nil || len(issues) != 1 {
				t.Errorf("issues after PR delete = %d, %v", len(issues), err)
			}
			if prs, err := store.LoadAllPRs(host, owner, repo); err != nil || len(prs) != 0 {
				t.Errorf("PRs after delete = %d, %v", len(prs), err)
			}
			if segs, err := store.LoadSegments(host, owner, repo, "prs"); err != nil || len(segs) != 0 {
				t.Errorf("PR coverage after delete = %+v, %v", segs, err)
			}
			if segs, err := store.LoadSegments(host, owner, repo, "issues"); err != nil || len(segs) != 1 {
				t.Errorf("issue coverage after PR delete = %+v, %v", segs, err)
			}
		})
	}
}
