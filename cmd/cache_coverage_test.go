package cmd

import (
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

// TestCache_WorkersProcessCoverageSegments verifies a multi-worker segmented
// run fetches every item once and records merged coverage for both kinds.
func TestCache_WorkersProcessCoverageSegments(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	scenario := sinceScenario(now)
	srv := mockserver.NewServer(scenario)
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())
	cacheWorkers = 3

	out := runCacheCapture(t)
	if got := parseCachedCount(t, out, cachedIssuesRe); got != len(scenario.Issues) {
		t.Errorf("cached %d issue(s), want %d", got, len(scenario.Issues))
	}
	if got := parseCachedCount(t, out, cachedPRsRe); got != len(scenario.PRs) {
		t.Errorf("cached %d pull request(s), want %d", got, len(scenario.PRs))
	}

	oldestIssue := time.Now().UTC()
	for _, si := range scenario.Issues {
		if si.Issue.UpdatedAt.Before(oldestIssue) {
			oldestIssue = si.Issue.UpdatedAt
		}
	}
	oldestPR := time.Now().UTC()
	for _, sp := range scenario.PRs {
		if sp.PullRequest.UpdatedAt.Before(oldestPR) {
			oldestPR = sp.PullRequest.UpdatedAt
		}
	}

	store := cache.NewStoreWithPath(cacheDir)
	for kind, oldest := range map[string]time.Time{"issues": oldestIssue, "prs": oldestPR} {
		segs, err := store.LoadSegments("github.com", "acme", "myproject", kind)
		if err != nil {
			t.Fatalf("LoadSegments(%s): %v", kind, err)
		}
		if !cache.Covers(segs, oldest, time.Now().UTC()) {
			t.Errorf("%s coverage does not include [%v, now]: %+v", kind, oldest, segs)
		}
		if len(segs) != 1 {
			t.Errorf("%s coverage has %d segments, want 1 merged segment: %+v", kind, len(segs), segs)
		}
	}
}
