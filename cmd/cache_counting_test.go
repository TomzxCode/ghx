package cmd

import (
	"sync"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/mockserver"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// operationLog records the operation name of every instrumented GitHub call.
type operationLog struct {
	mu  sync.Mutex
	ops map[string]int
}

func newOperationLog() *operationLog {
	return &operationLog{ops: map[string]int{}}
}

func (l *operationLog) Record(e telemetry.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops[e.Operation]++
}

func (l *operationLog) Enabled() bool { return true }

func (l *operationLog) Flush(time.Duration) {}

func (l *operationLog) count(operation string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ops[operation]
}

// TestFetchRepoDataCountsOnlyForProgress verifies that a refresh with no
// progress sink skips the window and total counts, which only fill progress
// events. The counts are separate GraphQL round trips, so a caller that never
// renders progress (the JSON-RPC server) should not pay for them.
func TestFetchRepoDataCountsOnlyForProgress(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)

	run := func(onProgress bool) *operationLog {
		t.Helper()
		client, err := github.NewClientWithURL(srv.URL(), "token", "github.com")
		if err != nil {
			t.Fatalf("new client: %v", err)
		}
		log := newOperationLog()
		client.SetTelemetry(log)
		store := cache.NewStoreWithPath(t.TempDir())
		t.Cleanup(func() { _ = store.Close() })
		repo := &gitremote.Repo{Host: "github.com", Owner: "acme", Name: "myproject"}
		opts := FetchOptions{FetchIssues: true, FetchPRs: true, Duration: 60}
		if onProgress {
			opts.OnProgress = func(FetchEvent) {}
		}
		if _, _, err := fetchRepoData(store, client, repo, &cache.CacheInfo{}, opts); err != nil {
			t.Fatalf("fetchRepoData: %v", err)
		}
		return log
	}

	quiet := run(false)
	noisy := run(true)

	if quiet.count("count_issues") != 0 || quiet.count("count_prs") != 0 {
		t.Errorf("no progress sink still counted items: issues=%d prs=%d",
			quiet.count("count_issues"), quiet.count("count_prs"))
	}
	if noisy.count("count_issues") != 1 || noisy.count("count_prs") != 1 {
		t.Errorf("progress sink did not count items: issues=%d prs=%d",
			noisy.count("count_issues"), noisy.count("count_prs"))
	}
	if quiet.count("search_count") >= noisy.count("search_count") {
		t.Errorf("window counts: quiet=%d noisy=%d", quiet.count("search_count"), noisy.count("search_count"))
	}
}
