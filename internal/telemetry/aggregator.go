package telemetry

import (
	"sort"
	"sync"
	"time"
)

// Aggregator is an in-memory Recorder that accumulates duration samples per
// kind so a run can print a timing summary without re-reading the database.
// It is safe for concurrent use, which matters because FetchPRsUpdated records
// from several goroutines.
type Aggregator struct {
	mu      sync.Mutex
	total   int
	retried int
	byKind  map[string]*kindStats
	start   time.Time
	end     time.Time
}

type kindStats struct {
	count       int
	retried     int
	failed      int
	samples     []int64 // milliseconds
	sentSamples []int64 // request bytes
	byteSamples []int64 // response bytes
	itemSamples []int64
}

// NewAggregator returns an empty aggregator.
func NewAggregator() *Aggregator {
	return &Aggregator{byKind: map[string]*kindStats{}, start: time.Now()}
}

// Record adds one event's duration to its kind's sample set.
func (a *Aggregator) Record(e Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.total++
	if e.Retried() {
		a.retried++
	}
	st := a.byKind[e.Kind]
	if st == nil {
		st = &kindStats{}
		a.byKind[e.Kind] = st
	}
	st.count++
	if e.Retried() {
		st.retried++
	}
	if e.Failed {
		st.failed++
	}
	a.end = time.Now()
	st.samples = append(st.samples, e.Duration.Milliseconds())
	if e.RequestBytes > 0 {
		st.sentSamples = append(st.sentSamples, int64(e.RequestBytes))
	}
	if e.ResponseBytes > 0 {
		st.byteSamples = append(st.byteSamples, int64(e.ResponseBytes))
	}
	if e.ReturnedItems > 0 {
		st.itemSamples = append(st.itemSamples, int64(e.ReturnedItems))
	}
}

// Enabled reports true.
func (a *Aggregator) Enabled() bool { return true }

// Flush is a no-op; the aggregator is in-memory.
func (a *Aggregator) Flush(time.Duration) {}

// Summary snapshots the accumulated statistics, sorted by total duration
// descending so the most expensive kind is first.
type Summary struct {
	Total         int
	Retried       int
	Elapsed       time.Duration
	KindSummaries []KindSummary
}

// KindSummary is the per-kind statistics within a run.
type KindSummary struct {
	Kind           string
	Count          int
	Retried        int
	Failed         int
	TotalMs        int64
	P50Ms          int64
	P95Ms          int64
	TotalSentBytes int64
	TotalRecvBytes int64
	P50Bytes       int64
	P95Bytes       int64
	P50Items       int
}

// Summary returns the aggregated run statistics.
func (a *Aggregator) Summary() Summary {
	a.mu.Lock()
	defer a.mu.Unlock()

	s := Summary{
		Total:   a.total,
		Retried: a.retried,
		Elapsed: a.end.Sub(a.start),
	}
	for kind, st := range a.byKind {
		ks := KindSummary{
			Kind:     kind,
			Count:    st.count,
			Retried:  st.retried,
			Failed:   st.failed,
			P50Ms:    percentile(st.samples, 50),
			P95Ms:    percentile(st.samples, 95),
			P50Bytes: percentile(st.byteSamples, 50),
			P95Bytes: percentile(st.byteSamples, 95),
		}
		if len(st.itemSamples) > 0 {
			ks.P50Items = int(percentile(st.itemSamples, 50))
		}
		for _, v := range st.samples {
			ks.TotalMs += v
		}
		for _, v := range st.sentSamples {
			ks.TotalSentBytes += v
		}
		for _, v := range st.byteSamples {
			ks.TotalRecvBytes += v
		}
		s.KindSummaries = append(s.KindSummaries, ks)
	}
	sort.Slice(s.KindSummaries, func(i, j int) bool {
		if s.KindSummaries[i].TotalMs != s.KindSummaries[j].TotalMs {
			return s.KindSummaries[i].TotalMs > s.KindSummaries[j].TotalMs
		}
		return s.KindSummaries[i].Kind < s.KindSummaries[j].Kind
	})
	return s
}

// percentile returns the nearest-rank percentile of an unsorted sample set.
func percentile(samples []int64, p int) int64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

// Ensure Aggregator satisfies Recorder at compile time.
var _ Recorder = (*Aggregator)(nil)
