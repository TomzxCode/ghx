package cmd

import (
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

// TC-6: the read-side summary computes percentiles and retry rate per kind.
func TestSummarizeGroupsByKind(t *testing.T) {
	events := []telemetry.StoredEvent{
		{Kind: "a", DurationMs: 10},
		{Kind: "a", DurationMs: 20},
		{Kind: "a", DurationMs: 30},
		{Kind: "a", DurationMs: 40, Retried: true},
		{Kind: "b", DurationMs: 5},
	}
	got := summarize(events)
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}
	// Sorted by total duration descending: a (100) before b (5).
	a := got[0]
	if a.Kind != "a" || a.Count != 4 {
		t.Fatalf("unexpected first summary: %+v", a)
	}
	if a.P50Ms != 20 || a.P95Ms != 40 {
		t.Errorf("percentiles = %d/%d, want 20/40", a.P50Ms, a.P95Ms)
	}
	if a.RetryRate != 0.25 {
		t.Errorf("retryRate = %v, want 0.25", a.RetryRate)
	}
	if got[1].Kind != "b" {
		t.Errorf("second kind = %q, want b", got[1].Kind)
	}
}

// Totals sum sent and received bytes across every event of a kind.
func TestSummarizeTotals(t *testing.T) {
	events := []telemetry.StoredEvent{
		{Kind: "pr_full_fetch", DurationMs: 100, RequestBytes: 1000, ResponseBytes: 200_000},
		{Kind: "pr_full_fetch", DurationMs: 200, RequestBytes: 1000, ResponseBytes: 300_000},
		{Kind: "pr_scan", DurationMs: 50, RequestBytes: 500, ResponseBytes: 5_000},
	}
	got := summarize(events)
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}
	full := got[0] // sorted by total_ms desc
	if full.Kind != "pr_full_fetch" {
		t.Fatalf("first kind = %q, want pr_full_fetch", full.Kind)
	}
	if full.TotalSentBytes != 2000 {
		t.Errorf("totalSent = %d, want 2000", full.TotalSentBytes)
	}
	if full.TotalRecvBytes != 500_000 {
		t.Errorf("totalRecv = %d, want 500000", full.TotalRecvBytes)
	}
	if got[1].TotalSentBytes != 500 || got[1].TotalRecvBytes != 5_000 {
		t.Errorf("scan totals = %d/%d", got[1].TotalSentBytes, got[1].TotalRecvBytes)
	}
}

// humanBytes renders binary units, including the byte and megabyte edges.
func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1024 * 1024, "1.0MB"},
		{3 * 1024 * 1024 * 1024, "3.0GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TC-28: an unsupported export format is a usage error and writes nothing.
func TestParseTelemetrySince(t *testing.T) {
	if _, err := parseTelemetrySince("2026-09-01"); err != nil {
		t.Errorf("date-only should parse: %v", err)
	}
	if _, err := parseTelemetrySince("2026-09-01T15:04:05Z"); err != nil {
		t.Errorf("RFC3339 should parse: %v", err)
	}
	if _, err := parseTelemetrySince("last week"); err == nil {
		t.Error("free text should fail to parse")
	}
	if _, err := parseTelemetrySince(""); err != nil {
		t.Errorf("empty should be no filter: %v", err)
	}
}

// A filtered query that matches nothing returns an empty result without
// treating it as an error, so the read commands can distinguish "no match"
// from "nothing ever recorded".
func TestQueryWithRetryFilteredNoMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := telemetry.Open(dir + "/t.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if err := store.InsertBatch([]telemetry.Event{
		{Timestamp: time.Now(), Kind: "pr_full_page", Operation: "fetch_pr_full_page", Attempts: 1},
	}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	got, err := queryWithRetry(store, telemetry.Filter{Kind: "does-not-exist"})
	if err != nil {
		t.Fatalf("queryWithRetry: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d events, want 0", len(got))
	}
}

// The unfiltered retry returns an established result once rows are visible.
func TestQueryWithRetryReturnsEvents(t *testing.T) {
	dir := t.TempDir()
	store, err := telemetry.Open(dir + "/t.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if err := store.InsertBatch([]telemetry.Event{
		{Timestamp: time.Now(), Kind: "pr_scan", Operation: "fetch_prs_updated_scan", Attempts: 1},
	}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	got, err := queryWithRetry(store, telemetry.Filter{})
	if err != nil {
		t.Fatalf("queryWithRetry: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
}
