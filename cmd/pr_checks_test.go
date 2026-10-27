package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

// checksScenario is a repository with one pull request whose head commit
// reports a failing check run and a passing commit status.
func checksScenario() *mockserver.Scenario {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return mockserver.NewScenario(
		mockserver.WithScenarioPR("acme", "myproject", github.PullRequest{
			Number:        1,
			Title:         "Fix crash on start",
			State:         "OPEN",
			HeadRefOid:    "0123456789abcdef0123456789abcdef01234567",
			CreatedAt:     now,
			UpdatedAt:     now,
			Checks:        &github.PRChecks{State: "FAILURE"},
			ReviewThreads: &github.PRThreadCounts{Total: 3, Open: 1},
		}),
		mockserver.WithScenarioPRChecks("acme", "myproject", 1, []github.PRCheck{
			{Type: "CheckRun", Name: "build", Status: "COMPLETED", Conclusion: "FAILURE", URL: "https://ci.example/build"},
			{Type: "StatusContext", Name: "ci/unit", State: "SUCCESS", Description: "All tests passed"},
		}),
	)
}

func TestPRChecks_PrintsEachCheck(t *testing.T) {
	srv := mockserver.NewServer(checksScenario())
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)
	oldJSON := prChecksJSON
	t.Cleanup(func() { prChecksJSON = oldJSON })
	prChecksJSON = false

	out := captureStdout(t, func() {
		if err := runPRChecks(nil, []string{"1"}); err != nil {
			t.Fatalf("runPRChecks: %v", err)
		}
	})
	for _, want := range []string{"failing", "2 check(s)", "build", "ci/unit"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q; output:\n%s", want, out)
		}
	}
}

func TestPRChecks_EmitsJSON(t *testing.T) {
	srv := mockserver.NewServer(checksScenario())
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)
	oldJSON := prChecksJSON
	t.Cleanup(func() { prChecksJSON = oldJSON })
	prChecksJSON = true

	out := captureStdout(t, func() {
		if err := runPRChecks(nil, []string{"1"}); err != nil {
			t.Fatalf("runPRChecks: %v", err)
		}
	})
	var rollup github.PRCheckRollup
	if err := json.Unmarshal([]byte(out), &rollup); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if rollup.State != "FAILURE" || rollup.Total != 2 || len(rollup.Checks) != 2 {
		t.Errorf("rollup = %+v, want FAILURE with 2 checks", rollup)
	}
	if rollup.Checks[0].URL != "https://ci.example/build" {
		t.Errorf("check url = %q, want the details URL", rollup.Checks[0].URL)
	}
}

// TestPRList_EmitsCachedChecksAndThreads covers the list JSON: the rollup and
// thread counts ride along with the cached record, so a caller reads them from
// a fresh cache without a second request.
func TestPRList_EmitsCachedChecksAndThreads(t *testing.T) {
	dir := t.TempDir()
	withPRListFlags(t, "http://127.0.0.1:1", dir)
	prListJSON = "number,checks,reviewThreads"

	now := time.Now().UTC()
	store := cache.NewStoreWithPath(dir)
	if err := store.SavePR("github.com", "acme", "myproject", &github.PullRequest{
		Number:        1,
		Title:         "Fix crash on start",
		State:         "OPEN",
		CreatedAt:     now,
		UpdatedAt:     now,
		Checks:        &github.PRChecks{State: "FAILURE", Total: 2},
		ReviewThreads: &github.PRThreadCounts{Total: 3, Open: 1},
	}); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
	if err := store.SaveCacheInfoFull("github.com", "acme", "myproject", &cache.CacheInfo{
		CachedAt: now,
		Duration: 60,
		Complete: true,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}
	store.Close()

	out := captureStdout(t, func() {
		if err := runPRList(nil, nil); err != nil {
			t.Fatalf("runPRList: %v", err)
		}
	})
	var rows []struct {
		Number        int                    `json:"number"`
		Checks        *github.PRChecks       `json:"checks"`
		ReviewThreads *github.PRThreadCounts `json:"reviewThreads"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Checks == nil || *rows[0].Checks != (github.PRChecks{State: "FAILURE", Total: 2}) {
		t.Errorf("checks = %+v, want FAILURE with 2 contexts", rows[0].Checks)
	}
	if rows[0].ReviewThreads == nil || *rows[0].ReviewThreads != (github.PRThreadCounts{Total: 3, Open: 1}) {
		t.Errorf("review threads = %+v, want 1 open of 3", rows[0].ReviewThreads)
	}
}
