package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
)

// overlapGate releases a request only once both portions have a request in
// flight, so it detects whether issues and pull requests are fetched
// concurrently. A sequential fetch makes the first request time out.
type overlapGate struct {
	wait     time.Duration
	mu       sync.Mutex
	arrived  map[string]bool
	released chan struct{}
	once     sync.Once
	timedOut bool
}

func newOverlapGate(wait time.Duration) *overlapGate {
	return &overlapGate{
		wait:     wait,
		arrived:  map[string]bool{},
		released: make(chan struct{}),
	}
}

// enter records a request for portion and waits for the other portion's request
// to arrive too. It reports whether the two requests overlapped.
func (g *overlapGate) enter(portion string) bool {
	g.mu.Lock()
	g.arrived[portion] = true
	if g.arrived["issues"] && g.arrived["prs"] {
		g.once.Do(func() { close(g.released) })
	}
	g.mu.Unlock()

	select {
	case <-g.released:
		return true
	case <-time.After(g.wait):
		g.mu.Lock()
		g.timedOut = true
		g.mu.Unlock()
		return false
	}
}

func (g *overlapGate) overlapped() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.timedOut
}

// parallelFetchServer serves one issue page and one PR page, holding each
// response until the other portion's request is also in flight.
func parallelFetchServer(t *testing.T, gate *overlapGate) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.Query, "issues(first:"):
			gate.enter("issues")
			_, _ = w.Write([]byte(`{"data":{"repository":{"issues":{
				"totalCount":1,
				"pageInfo":{"hasNextPage":false},
				"nodes":[{
					"number":1,"title":"Cached issue","state":"OPEN",
					"author":{"login":"alice"},
					"assignees":{"nodes":[]},"labels":{"nodes":[]},
					"createdAt":"2026-08-01T00:00:00Z","updatedAt":"2026-08-02T00:00:00Z",
					"url":"https://example.com/issues/1","body":"",
					"comments":{"totalCount":0,"nodes":[]}
				}]
			}}}}`))
		case strings.Contains(req.Query, "pullRequests(first:"):
			gate.enter("prs")
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequests":{
				"totalCount":1,
				"pageInfo":{"hasNextPage":false},
				"nodes":[{
					"number":1,"title":"Cached PR","state":"OPEN","isDraft":false,
					"author":{"login":"bob"},
					"assignees":{"nodes":[]},"labels":{"nodes":[]},
					"baseRefName":"main","headRefName":"feature",
					"createdAt":"2026-08-01T00:00:00Z","updatedAt":"2026-08-02T00:00:00Z",
					"url":"https://example.com/pull/1","body":"",
					"comments":{"totalCount":0,"nodes":[]},
					"reviews":{"nodes":[]},"timelineItems":{"nodes":[]}
				}]
			}}}}`))
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCache_FetchesIssuesAndPRsConcurrently verifies a full run keeps both
// requests in flight at the same time and caches both portions.
func TestCache_FetchesIssuesAndPRsConcurrently(t *testing.T) {
	gate := newOverlapGate(5 * time.Second)
	srv := parallelFetchServer(t, gate)
	setCacheTestEnv(t, srv.URL, t.TempDir())

	out := runCacheCapture(t)

	if !gate.overlapped() {
		t.Error("issues and pull requests were not fetched concurrently")
	}
	if !strings.Contains(out, "Cached 1 issue(s).") {
		t.Errorf("expected the issue count in %q", out)
	}
	if !strings.Contains(out, "Cached 1 pull request(s).") {
		t.Errorf("expected the pull request count in %q", out)
	}

	store := cache.NewStoreWithPath(cacheDir)
	if _, _, err := store.LoadIssue("github.com", "acme", "myproject", 1); err != nil {
		t.Errorf("issue #1 was not cached: %v", err)
	}
	if _, _, err := store.LoadPR("github.com", "acme", "myproject", 1); err != nil {
		t.Errorf("PR #1 was not cached: %v", err)
	}
	info, err := store.LoadCacheInfo("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadCacheInfo: %v", err)
	}
	if info == nil || !info.Complete {
		t.Error("a full concurrent run should mark the cache complete")
	}
}

// TestCache_FailingPortionKeepsOtherPortion verifies the portions are
// independent: a pull request failure does not throw away the issues the
// concurrent run already cached.
func TestCache_FailingPortionKeepsOtherPortion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.Query, "issues(first:"):
			_, _ = w.Write([]byte(`{"data":{"repository":{"issues":{
				"totalCount":1,
				"pageInfo":{"hasNextPage":false},
				"nodes":[{
					"number":1,"title":"Cached issue","state":"OPEN",
					"author":{"login":"alice"},
					"assignees":{"nodes":[]},"labels":{"nodes":[]},
					"createdAt":"2026-08-01T00:00:00Z","updatedAt":"2026-08-02T00:00:00Z",
					"url":"https://example.com/issues/1","body":"",
					"comments":{"totalCount":0,"nodes":[]}
				}]
			}}}}`))
		case strings.Contains(req.Query, "pullRequests(first:"):
			_, _ = w.Write([]byte(`{"errors":[{"message":"pull requests unavailable"}]}`))
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL, t.TempDir())
	cacheRetries = 0 // a GraphQL error is not retried; keep the test fast

	var err error
	captureStdout(t, func() { err = runCache(nil, nil) })
	if err == nil || !strings.Contains(err.Error(), "fetching pull requests") {
		t.Fatalf("expected the pull request failure to surface, got %v", err)
	}

	store := cache.NewStoreWithPath(cacheDir)
	if _, _, err := store.LoadIssue("github.com", "acme", "myproject", 1); err != nil {
		t.Errorf("issue #1 should stay cached despite the PR failure: %v", err)
	}
}

// TestCombinedProgress_TracksBothPortions verifies the shared bar counts both
// portions, adopts the sum of their totals, and finishes after the last one.
func TestCombinedProgress_TracksBothPortions(t *testing.T) {
	p := &combinedBar{
		status: func() string { return "" },
		totals: map[string]int{},
		done:   map[string]int{},
		writer: io.Discard,
	}
	p.handle(FetchEvent{Phase: "issues", Kind: "start", Message: "Caching issues..."})
	p.handle(FetchEvent{Phase: "prs", Kind: "start", Message: "Caching pull requests..."})
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10})
	p.handle(FetchEvent{Phase: "prs", Kind: "batch", Done: 2, Total: 4})

	if p.max != 14 {
		t.Errorf("bar maximum = %d, want the sum of both totals (14)", p.max)
	}
	desc := p.describe()
	for _, want := range []string{"issues 5/10", "pull requests 2/4"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description %q does not contain %q", desc, want)
		}
	}

	p.handle(FetchEvent{Phase: "issues", Kind: "done", Done: 5})
	if p.active != 1 {
		t.Errorf("active portions = %d, want 1 while pull requests still run", p.active)
	}
	p.handle(FetchEvent{Phase: "prs", Kind: "done", Done: 2})
	if p.active != 0 {
		t.Errorf("active portions = %d, want 0 after both finished", p.active)
	}
	if !p.bar.IsFinished() {
		t.Error("the shared bar should be finished after the last portion")
	}
}
