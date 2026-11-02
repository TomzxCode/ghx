package cmd

import (
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

// TestCache_RecordsViewerTeams asserts a cache run stores the authenticated
// user's team memberships for the host, which team-aware search then reads.
func TestCache_RecordsViewerTeams(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	want := []github.ViewerTeam{
		{Organization: "acme", Slug: "platform", Name: "Platform"},
		{Organization: "acme", Slug: "infra", Name: "Infra"},
	}
	srv := mockserver.NewServer(mockserver.NewScenario(
		mockserver.WithScenarioIssue("acme", "myproject", github.Issue{
			Number: 1, Title: "Fix login", State: "OPEN",
			Author: github.Actor{Login: "alice"}, CreatedAt: now, UpdatedAt: now,
		}),
		mockserver.WithViewerTeams(want),
	))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())

	runCacheCapture(t)

	store := cache.NewStoreWithPath(cacheDir)
	defer store.Close()
	got, err := store.LoadViewerTeams("github.com")
	if err != nil {
		t.Fatalf("LoadViewerTeams: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("teams = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("team %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
