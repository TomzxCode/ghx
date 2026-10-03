package mockserver

import (
	"math/rand"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

// TC-9/TC-30: the model keeps per-kind samples, ignores unknown kinds, and
// samples from the full distribution including the tail.
func TestLatencyModel(t *testing.T) {
	events := []telemetry.StoredEvent{
		{Kind: telemetry.KindPRFullPage, DurationMs: 100},
		{Kind: telemetry.KindPRFullPage, DurationMs: 200},
		{Kind: telemetry.KindPRFullPage, DurationMs: 5000}, // tail (a 502 retry)
		{Kind: telemetry.KindIssueFull, DurationMs: 50},
		{Kind: "", DurationMs: 999},                 // ignored
		{Kind: "brand-new-kind", DurationMs: 7},     // tolerated, allowed
		{Kind: telemetry.KindPRScan, DurationMs: 0}, // ignored
	}
	m := NewLatencyModel(events, 42)
	if m.Kinds() != 3 {
		t.Fatalf("kinds = %d, want 3", m.Kinds())
	}
	if m.TotalSamples() != 5 {
		t.Fatalf("samples = %d, want 5", m.TotalSamples())
	}

	rng := rand.New(rand.NewSource(42))
	var sawTail bool
	for i := 0; i < 200; i++ {
		d := m.Sample(telemetry.KindPRFullPage, rng)
		if d == 5000*time.Millisecond {
			sawTail = true
		}
	}
	if !sawTail {
		t.Error("sampling should reach the tail of the distribution")
	}

	// A kind with no samples yields no delay.
	if d := m.Sample("missing-kind", rng); d != 0 {
		t.Errorf("missing kind delay = %v, want 0", d)
	}
}

// Same seed reproduces the same sequence.
func TestLatencyModelDeterministic(t *testing.T) {
	events := []telemetry.StoredEvent{
		{Kind: "k", DurationMs: 10},
		{Kind: "k", DurationMs: 20},
		{Kind: "k", DurationMs: 30},
	}
	m := NewLatencyModel(events, 7)
	a := rand.New(rand.NewSource(7))
	b := rand.New(rand.NewSource(7))
	for i := 0; i < 50; i++ {
		if m.Sample("k", a) != m.Sample("k", b) {
			t.Fatalf("sample %d differed between identical seeds", i)
		}
	}
}

// A query shape maps to the same kind the client labels it with, so a fitted
// model lines up with what the server serves.
func TestRouteKindMatchesClientLabels(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"query($owner: String!) { repository { issue(number: $number) { number } } }", telemetry.KindIssueGet},
		{"query { repository { pullRequest(number: $number) { number } } }", telemetry.KindPRGet},
		{"query { search(query: $query, type: ISSUE, first: $first) { nodes { __typename } } }", telemetry.KindPRSearch},
		{"query { repository { issues(first: 100, states: [OPEN, CLOSED], after: $after) { nodes { number } } } }", telemetry.KindIssueFull},
		{"query { repository { issues(first: $first, states: $states) { nodes { number } } } }", telemetry.KindIssueList},
		{"query { repository { pullRequests(first: 50, states: [OPEN, CLOSED, MERGED]) { nodes { number } } } }", telemetry.KindPRFull},
		{"query { repository { pullRequests(first: $first, states: [OPEN, CLOSED, MERGED], after: $after) { nodes { number } } } }", telemetry.KindPRFullPage},
		{"query { repository { pullRequests(first: $first, states: $states) { nodes { number } } } }", telemetry.KindPRList},
	}
	for _, tc := range cases {
		if got := routeKind(tc.query); got != tc.want {
			t.Errorf("routeKind(%.40q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}
