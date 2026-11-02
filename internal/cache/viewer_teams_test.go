package cache

import (
	"sort"
	"testing"

	"github.com/tomzxcode/ghx/internal/github"
)

func TestViewerTeamsRoundTrip(t *testing.T) {
	stores := map[string]func(*testing.T) Store{
		"file": func(t *testing.T) Store { return NewStoreWithPath(t.TempDir()) },
		"sqlite": func(t *testing.T) Store {
			s, err := NewSQLiteStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			return s
		},
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			store := open(t)
			defer store.Close()

			if got, err := store.LoadViewerTeams("github.com"); err != nil || got != nil {
				t.Fatalf("teams before any save = %+v, %v; want nil, nil", got, err)
			}

			want := []github.ViewerTeam{
				{Organization: "acme", Slug: "platform", Name: "Platform"},
				{Organization: "acme", Slug: "infra", Name: "Infra"},
			}
			if err := store.SaveViewerTeams("github.com", want); err != nil {
				t.Fatalf("SaveViewerTeams: %v", err)
			}
			got, err := store.LoadViewerTeams("github.com")
			if err != nil {
				t.Fatalf("LoadViewerTeams: %v", err)
			}
			sortTeams(want)
			sortTeams(got)
			if len(got) != len(want) {
				t.Fatalf("teams = %+v, want %+v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("team %d = %+v, want %+v", i, got[i], want[i])
				}
			}

			// A second save replaces the host's set rather than appending to it.
			replacement := []github.ViewerTeam{{Organization: "globex", Slug: "reviewers", Name: "Reviewers"}}
			if err := store.SaveViewerTeams("github.com", replacement); err != nil {
				t.Fatalf("SaveViewerTeams replacement: %v", err)
			}
			got, err = store.LoadViewerTeams("github.com")
			if err != nil {
				t.Fatalf("LoadViewerTeams after replacement: %v", err)
			}
			if len(got) != 1 || got[0] != replacement[0] {
				t.Fatalf("teams after replacement = %+v, want %+v", got, replacement)
			}

			// Teams are scoped per host.
			if got, err := store.LoadViewerTeams("ghe.example.com"); err != nil || got != nil {
				t.Fatalf("teams on another host = %+v, %v; want nil, nil", got, err)
			}
		})
	}
}

// sortTeams orders teams the way the SQLite backend returns them, so a
// round-trip assertion does not depend on the backend's row order.
func sortTeams(teams []github.ViewerTeam) {
	sort.Slice(teams, func(i, j int) bool {
		if teams[i].Organization != teams[j].Organization {
			return teams[i].Organization < teams[j].Organization
		}
		return teams[i].Slug < teams[j].Slug
	})
}
