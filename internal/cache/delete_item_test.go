package cache

import (
	"testing"
)

// TestDeleteItem exercises per-item deletion on both backends: the named kind's
// item disappears, an item of the other kind with the same number stays, and a
// missing item reports false instead of an error.
func TestDeleteItem(t *testing.T) {
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

			if err := s.SaveIssue(host, owner, repo, sampleIssue(5)); err != nil {
				t.Fatalf("SaveIssue: %v", err)
			}
			if err := s.SavePR(host, owner, repo, samplePR(5)); err != nil {
				t.Fatalf("SavePR: %v", err)
			}
			if err := s.SavePR(host, owner, repo, samplePR(6)); err != nil {
				t.Fatalf("SavePR: %v", err)
			}

			deleted, err := s.DeleteItem(host, owner, repo, KindIssues, 5)
			if err != nil {
				t.Fatalf("DeleteItem(issue 5): %v", err)
			}
			if !deleted {
				t.Error("DeleteItem(issue 5) reported false, want true")
			}
			if _, _, err := s.LoadIssue(host, owner, repo, 5); err == nil {
				t.Error("issue 5 is still cached after deletion")
			}
			if _, _, err := s.LoadPR(host, owner, repo, 5); err != nil {
				t.Errorf("pull request 5 was deleted with the issue: %v", err)
			}

			deleted, err = s.DeleteItem(host, owner, repo, KindPRs, 6)
			if err != nil {
				t.Fatalf("DeleteItem(PR 6): %v", err)
			}
			if !deleted {
				t.Error("DeleteItem(PR 6) reported false, want true")
			}
			if _, _, err := s.LoadPR(host, owner, repo, 6); err == nil {
				t.Error("pull request 6 is still cached after deletion")
			}

			deleted, err = s.DeleteItem(host, owner, repo, KindPRs, 99)
			if err != nil {
				t.Fatalf("DeleteItem(missing PR): %v", err)
			}
			if deleted {
				t.Error("DeleteItem(missing PR) reported true, want false")
			}
		})
	}
}
