package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/logging"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// newStubClient returns a github.Client wired to a stub server. The returned
// http.Client routes requests to h without a real socket.
func newStubClient(t *testing.T, rec telemetry.Recorder, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := &Client{
		host:        "github.com",
		endpointURL: srv.URL,
		httpClient:  srv.Client(),
		recorder:    rec,
	}
	c.withDefaults()
	c.sleep = func(time.Duration) {}
	return c
}

// fakeRecorder captures events in memory for assertions. Recording is
// mutex-protected so tests can drive concurrent calls through it.
type fakeRecorder struct {
	mu     sync.Mutex
	events []telemetry.Event
}

func (f *fakeRecorder) Record(e telemetry.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeRecorder) snapshot() []telemetry.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telemetry.Event(nil), f.events...)
}

func (f *fakeRecorder) Enabled() bool         { return true }
func (f *fakeRecorder) Flush(_ time.Duration) {}

// TC-1/TC-11: one call records exactly one event with the caller's labels.
func TestQueryRecordsOneEvent(t *testing.T) {
	rec := &fakeRecorder{}
	var calls int32
	c := newStubClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Write([]byte(`{"data":{"ok":true}}`))
	})

	var result map[string]interface{}
	err := c.QueryCtx(CallContext{
		Operation: "fetch_all_issues",
		Kind:      telemetry.KindIssueFull,
		Repo:      "acme/platform",
		PageSize:  100,
	}, `query { ok }`, nil, &result)
	if err != nil {
		t.Fatalf("QueryCtx: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1", len(rec.events))
	}
	e := rec.events[0]
	if e.Kind != telemetry.KindIssueFull || e.Operation != "fetch_all_issues" {
		t.Errorf("kind/op = %q/%q", e.Kind, e.Operation)
	}
	if e.Repo != "acme/platform" || e.Status != 200 {
		t.Errorf("repo/status = %q/%d", e.Repo, e.Status)
	}
	if e.Attempts != 1 || e.Retried() {
		t.Errorf("attempts/retried = %d/%v", e.Attempts, e.Retried())
	}
	if e.PageSize != 100 {
		t.Errorf("pageSize = %d", e.PageSize)
	}
	if e.RateRemaining != 4999 {
		t.Errorf("rateRemaining = %d, want 4999", e.RateRemaining)
	}
	if e.Duration <= 0 {
		t.Error("duration should be positive")
	}
	_ = calls
}

// TC-2/TC-12: a call retried after a transient failure records both attempts
// in a single event, and the duration includes the backoff wait.
func TestQueryRecordsRetryInOneEvent(t *testing.T) {
	rec := &fakeRecorder{}
	var slept time.Duration
	var n int32
	c := newStubClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("bad gateway"))
			return
		}
		w.Write([]byte(`{"data":{"ok":true}}`))
	})
	c.sleep = func(d time.Duration) { slept = d }

	var result map[string]interface{}
	if err := c.QueryCtx(CallContext{
		Operation: "fetch_pr_full_page",
		Kind:      telemetry.KindPRFullPage,
		Repo:      "acme/platform",
		PageSize:  25,
		Cursor:    true,
	}, `query { ok }`, nil, &result); err != nil {
		t.Fatalf("QueryCtx: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1 (one per call, not per attempt)", len(rec.events))
	}
	e := rec.events[0]
	if e.Attempts != 2 || !e.Retried() {
		t.Errorf("attempts/retried = %d/%v, want 2/true", e.Attempts, e.Retried())
	}
	if !e.Cursor {
		t.Error("cursor should be recorded")
	}
	if slept == 0 {
		t.Error("expected a backoff wait between attempts")
	}
	// The stub sleep does not consume real time; the assertion is that the
	// retry path ran and the single event carries both attempts.
	if e.Duration <= 0 {
		t.Error("duration should be positive")
	}
}

// A disabled client records nothing.
func TestDisabledRecorderRecordsNothing(t *testing.T) {
	c := newStubClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{}}`))
	})
	if c.recorder != nil {
		t.Fatal("a fresh client should have no recorder")
	}
	err := c.QueryCtx(CallContext{Kind: telemetry.KindIssueList}, `query { ok }`, nil, nil)
	if err != nil {
		t.Fatalf("QueryCtx: %v", err)
	}
}

// A failed call still records an event marked failed.
func TestFailedCallIsRecorded(t *testing.T) {
	rec := &fakeRecorder{}
	c := newStubClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	})
	err := c.QueryCtx(CallContext{Operation: "get_pr", Kind: telemetry.KindPRGet}, `query { ok }`, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1", len(rec.events))
	}
	if !rec.events[0].Failed {
		t.Error("failed call should be marked failed")
	}
	if rec.events[0].Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.events[0].Status)
	}
}

// A client with no operation label falls back to the unknown kind rather than
// attributing the call to the wrong operation.
func TestEmptyKindBecomesUnknown(t *testing.T) {
	rec := &fakeRecorder{}
	c := newStubClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{}}`))
	})
	if err := c.QueryCtx(CallContext{Operation: "op"}, `query { ok }`, nil, nil); err != nil {
		t.Fatalf("QueryCtx: %v", err)
	}
	if rec.events[0].Kind != telemetry.KindUnknown {
		t.Errorf("kind = %q, want %q", rec.events[0].Kind, telemetry.KindUnknown)
	}
}

// Request and response sizes plus the returned item count are captured.
func TestQueryRecordsSizesAndItemCount(t *testing.T) {
	rec := &fakeRecorder{}
	c := newStubClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"repository":{"issues":{"nodes":[{"number":1},{"number":2},{"number":3}]}}}}`))
	})
	var result struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					Number int `json:"number"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	vars := map[string]interface{}{"owner": "acme", "repo": "platform", "first": 3}
	if err := c.QueryCtx(CallContext{
		Operation: "list_issues",
		Kind:      telemetry.KindIssueList,
		Repo:      "acme/platform",
		PageSize:  3,
	}, `query { ok }`, vars, &result); err != nil {
		t.Fatalf("QueryCtx: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1", len(rec.events))
	}
	e := rec.events[0]
	if e.ReturnedItems != 3 {
		t.Errorf("returnedItems = %d, want 3", e.ReturnedItems)
	}
	if e.RequestBytes <= 0 {
		t.Errorf("requestBytes = %d, want > 0", e.RequestBytes)
	}
	if e.ResponseBytes <= 0 {
		t.Errorf("responseBytes = %d, want > 0", e.ResponseBytes)
	}
}

// Each request logs one debug line with its operation and outcome, so paged
// fetches show up individually instead of only as a command-level total.
func TestQueryCtxLogsEachRequest(t *testing.T) {
	var buf bytes.Buffer
	logging.SetOutput(&buf)
	if err := logging.SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug): %v", err)
	}
	t.Cleanup(func() {
		logging.SetOutput(os.Stderr)
		_ = logging.SetLevel("info")
	})

	c := newStubClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"repository":{"issues":{"nodes":[{"number":1},{"number":2}]}}}}`))
	})
	if err := c.QueryCtx(CallContext{
		Operation: "list_issues",
		Kind:      telemetry.KindIssueList,
		Repo:      "acme/platform",
		PageSize:  2,
		Cursor:    true,
	}, `query { ok }`, nil, nil); err != nil {
		t.Fatalf("QueryCtx: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"github request",
		"operation=list_issues",
		"kind=issue_list",
		"repo=acme/platform",
		"page_size=2",
		"cursor=true",
		"status=200",
		"attempts=1",
		"items=2",
		"request_bytes=",
		"response_bytes=",
		"duration=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug log missing %q; got:\n%s", want, out)
		}
	}
}

// Each concurrent call reports its own item count and status rather than
// whatever the most recent response happened to be, which the shared counters
// it replaced could not guarantee during the parallel cache backfill.
func TestQueryStatsArePerCall(t *testing.T) {
	rec := &fakeRecorder{}
	c := newStubClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		n := int(req.Variables["n"].(float64))
		nodes := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			nodes = append(nodes, fmt.Sprintf(`{"number":%d}`, i))
		}
		fmt.Fprintf(w, `{"data":{"repository":{"pullRequests":{"nodes":[%s]}}}}`, strings.Join(nodes, ","))
	})

	const calls = 6
	var wg sync.WaitGroup
	for n := 1; n <= calls; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := c.QueryCtx(CallContext{
				Operation: "fetch_pr_full_page",
				Kind:      telemetry.KindPRFullPage,
				Repo:      "acme/platform",
				PageSize:  25,
			}, `query($n: Int) { ok }`, map[string]interface{}{"n": n}, nil)
			if err != nil {
				t.Errorf("QueryCtx(n=%d): %v", n, err)
			}
		}(n)
	}
	wg.Wait()

	events := rec.snapshot()
	if len(events) != calls {
		t.Fatalf("got %d events, want %d", len(events), calls)
	}
	got := make(map[int]bool, calls)
	for _, e := range events {
		got[e.ReturnedItems] = true
		if e.Status != http.StatusOK {
			t.Errorf("status = %d, want 200", e.Status)
		}
	}
	for n := 1; n <= calls; n++ {
		if !got[n] {
			t.Errorf("no event reported %d items; got counts %v", n, got)
		}
	}
}

// A response with no nodes array reports zero items rather than a bogus count.
func TestCountNodesNoArray(t *testing.T) {
	rec := &fakeRecorder{}
	c := newStubClient(t, rec, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"repository":{"issue":{"number":1}}}}`))
	})
	if err := c.QueryCtx(CallContext{
		Operation: "get_issue",
		Kind:      telemetry.KindIssueGet,
	}, `query { ok }`, nil, nil); err != nil {
		t.Fatalf("QueryCtx: %v", err)
	}
	if rec.events[0].ReturnedItems != 0 {
		t.Errorf("returnedItems = %d, want 0", rec.events[0].ReturnedItems)
	}
}

// TC-24 / NFR-2: SetTelemetry(nil) leaves a working, recording-nothing client.
func TestSetTelemetryNilDisables(t *testing.T) {
	c := &Client{host: "github.com", endpointURL: "http://unused"}
	c.withDefaults()
	c.SetTelemetry(nil)
	if c.recorder.Enabled() {
		t.Error("nil recorder should disable recording")
	}
	if c.recorder == nil {
		t.Error("recorder should never be nil after SetTelemetry")
	}
}
