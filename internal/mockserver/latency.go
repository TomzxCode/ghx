package mockserver

import (
	"math/rand"
	"sort"
	"time"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

// LatencyModel makes generated scenarios reproduce measured per-kind latency
// instead of an assumed one. It samples from the recorded duration
// distribution (not a fitted mean) so the long tail (502s, rate-limit waits)
// survives into the simulation.
type LatencyModel struct {
	// Samples maps an operation kind to observed durations, in milliseconds.
	Samples map[string][]int64
	// ResponseBytes maps an operation kind to observed response payload sizes in
	// bytes, so a scenario can also model payload size alongside latency.
	ResponseBytes map[string][]int64
	// Seed makes sampling deterministic.
	Seed int64
}

// NewLatencyModel builds a model from stored telemetry events, keeping the
// durations per kind. Events with an unknown or empty kind are ignored so the
// model tolerates forward-compatible additions.
func NewLatencyModel(events []telemetry.StoredEvent, seed int64) *LatencyModel {
	m := &LatencyModel{
		Samples:       map[string][]int64{},
		ResponseBytes: map[string][]int64{},
		Seed:          seed,
	}
	for _, e := range events {
		if e.Kind == "" {
			continue
		}
		if e.DurationMs > 0 {
			m.Samples[e.Kind] = append(m.Samples[e.Kind], e.DurationMs)
		}
		if e.ResponseBytes > 0 {
			m.ResponseBytes[e.Kind] = append(m.ResponseBytes[e.Kind], int64(e.ResponseBytes))
		}
	}
	for _, s := range m.Samples {
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	}
	for _, s := range m.ResponseBytes {
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	}
	return m
}

// Kinds reports how many operation kinds the model covers.
func (m *LatencyModel) Kinds() int {
	if m == nil {
		return 0
	}
	return len(m.Samples)
}

// TotalSamples reports the number of recorded durations in the model.
func (m *LatencyModel) TotalSamples() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, s := range m.Samples {
		n += len(s)
	}
	return n
}

// Sample returns a delay drawn from the recorded distribution for kind, or zero
// when the kind has no samples. The same seed and call order reproduce the same
// sequence.
func (m *LatencyModel) Sample(kind string, rng *rand.Rand) time.Duration {
	if m == nil || rng == nil {
		return 0
	}
	samples := m.Samples[kind]
	if len(samples) == 0 {
		return 0
	}
	ms := samples[rng.Intn(len(samples))]
	return time.Duration(ms) * time.Millisecond
}

// SampleResponseBytes returns a response payload size drawn from the recorded
// distribution for kind, or zero when the kind has no size samples.
func (m *LatencyModel) SampleResponseBytes(kind string, rng *rand.Rand) int {
	if m == nil || rng == nil {
		return 0
	}
	samples := m.ResponseBytes[kind]
	if len(samples) == 0 {
		return 0
	}
	return int(samples[rng.Intn(len(samples))])
}
