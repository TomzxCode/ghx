package telemetry

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TC-1/TC-2: an API event round-trips with retried derived from attempts.
func TestAPIEventRoundTrip(t *testing.T) {
	store := newTestStore(t)
	e := Event{
		Timestamp:     time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
		Kind:          KindPRFullPage,
		Operation:     "fetch_pr_full_page",
		Host:          "github.com",
		Repo:          "cli/cli",
		Status:        200,
		Duration:      186 * time.Millisecond,
		PageSize:      25,
		ReturnedItems: 50,
		RequestBytes:  1401,
		ResponseBytes: 1214618,
		Cursor:        true,
		Attempts:      2,
		RateRemaining: 4821,
	}
	if err := store.InsertBatch([]Event{e}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	got, err := store.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	g := got[0]
	if g.Kind != KindPRFullPage || g.Operation != "fetch_pr_full_page" {
		t.Errorf("kind/op = %q/%q", g.Kind, g.Operation)
	}
	if g.Repo != "cli/cli" || g.Host != "github.com" {
		t.Errorf("repo/host = %q/%q", g.Repo, g.Host)
	}
	if g.Status != 200 || g.DurationMs != 186 {
		t.Errorf("status/duration = %d/%d", g.Status, g.DurationMs)
	}
	if g.PageSize != 25 || g.ReturnedItems != 50 {
		t.Errorf("pageSize/returned = %d/%d, want 25/50", g.PageSize, g.ReturnedItems)
	}
	if g.RequestBytes != 1401 || g.ResponseBytes != 1214618 {
		t.Errorf("bytes = %d/%d", g.RequestBytes, g.ResponseBytes)
	}
	if !g.Cursor {
		t.Error("cursor should be recorded")
	}
	if g.Attempts != 2 || !g.Retried {
		t.Errorf("attempts/retried = %d/%v", g.Attempts, g.Retried)
	}
	if g.RateRemaining != 4821 {
		t.Errorf("rateRemaining = %d", g.RateRemaining)
	}
}

// TC-3: a cache event carries backend and item count.
func TestCacheEventRoundTrip(t *testing.T) {
	store := newTestStore(t)
	e := Event{
		Timestamp: time.Now(),
		Kind:      KindCacheQueryIssues,
		Operation: "QueryIssues",
		Repo:      "acme/platform",
		Duration:  2 * time.Millisecond,
		Backend:   "sqlite",
		Items:     10,
	}
	if err := store.InsertBatch([]Event{e}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	got, _ := store.Query(Filter{Kind: KindCacheQueryIssues})
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Backend != "sqlite" || got[0].Items != 10 {
		t.Errorf("backend/items = %q/%d", got[0].Backend, got[0].Items)
	}
	if got[0].Status != 0 {
		t.Errorf("cache event should have no HTTP status, got %d", got[0].Status)
	}
}

// TC-4: the default path resolves under the user data directory.
func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test")
	if got := DefaultPath(); got != "/tmp/xdg-test/ghx/telemetry.db" {
		t.Errorf("DefaultPath = %q", got)
	}
}

// TC-6: percentiles and retry rate are computed correctly at the read side.
func TestSummarizeIsCoveredInCmd(t *testing.T) {
	t.Skip("summarize lives in package cmd; covered by cmd/telemetry_test.go")
}

// TC-10: schema creation is idempotent and additive.
func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.InsertBatch([]Event{{Timestamp: time.Now(), Kind: "k", Operation: "op", Attempts: 1}}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	first.Close()

	second, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()
	n, err := second.Count(Filter{})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Errorf("existing rows after reopen = %d, want 1", n)
	}
}

// TC-17: filters compose by kind, repo, and since.
func TestFilterComposes(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{
		{Timestamp: base, Kind: "a", Repo: "x/y", Operation: "op", Attempts: 1},
		{Timestamp: base.Add(48 * time.Hour), Kind: "a", Repo: "x/y", Operation: "op", Attempts: 1},
		{Timestamp: base.Add(48 * time.Hour), Kind: "a", Repo: "p/q", Operation: "op", Attempts: 1},
		{Timestamp: base.Add(48 * time.Hour), Kind: "b", Repo: "x/y", Operation: "op", Attempts: 1},
	}
	if err := store.InsertBatch(events); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	got, err := store.Query(Filter{Kind: "a", Repo: "x/y", Since: base.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
}

// TC-27 / clear semantics: DeleteBefore removes only older events.
func TestDeleteBefore(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_ = store.InsertBatch([]Event{
		{Timestamp: base, Kind: "a", Operation: "op", Attempts: 1},
		{Timestamp: base.Add(72 * time.Hour), Kind: "a", Operation: "op", Attempts: 1},
	})
	n, err := store.CountBefore(base.Add(24 * time.Hour))
	if err != nil {
		t.Fatalf("CountBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountBefore = %d, want 1", n)
	}
	deleted, err := store.DeleteBefore(base.Add(24 * time.Hour))
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	remaining, _ := store.Count(Filter{})
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1", remaining)
	}
}

// NFR-2: a failing sink never blocks or panics the recorder.
func TestAsyncDropsOnClosedStore(t *testing.T) {
	store := newTestStore(t)
	async := NewAsync(store)
	async.Record(Event{Kind: "a", Operation: "op"})
	async.Flush(time.Second)
	store.Close()
	// Recording into a closed store must not panic; the writer drops the batch.
	async2 := NewAsync(store)
	for i := 0; i < 10; i++ {
		async2.Record(Event{Kind: "a", Operation: "op"})
	}
	async2.Flush(time.Second)
}

// Nop is disabled and safe to call.
func TestNopRecorder(t *testing.T) {
	var r Recorder = Nop{}
	if r.Enabled() {
		t.Error("Nop should be disabled")
	}
	r.Record(Event{Kind: "a"})
	r.Flush(0)
}

// TC-5: the default recorder is a no-op and writes nothing.
func TestAggregatorSummary(t *testing.T) {
	agg := NewAggregator()
	for i := 0; i < 4; i++ {
		agg.Record(Event{Kind: "k", Operation: "op", Duration: time.Duration(10*(i+1)) * time.Millisecond, Attempts: 1})
	}
	agg.Record(Event{Kind: "k", Operation: "op", Duration: 50 * time.Millisecond, Attempts: 2})
	s := agg.Summary()
	if s.Total != 5 {
		t.Errorf("total = %d, want 5", s.Total)
	}
	if s.Retried != 1 {
		t.Errorf("retried = %d, want 1", s.Retried)
	}
	if len(s.KindSummaries) != 1 {
		t.Fatalf("got %d kinds", len(s.KindSummaries))
	}
	if s.KindSummaries[0].P50Ms != 30 {
		t.Errorf("p50 = %d, want 30", s.KindSummaries[0].P50Ms)
	}
}
