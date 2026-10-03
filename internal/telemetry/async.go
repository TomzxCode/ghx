package telemetry

import (
	"sync"
	"time"
)

// queueSize bounds the in-memory buffer. It is generous enough that a normal
// run never drops an event, and bounded so a stalled sink cannot grow memory
// without limit. Events are dropped, never blocked on, once it is full.
const queueSize = 4096

// batchSize is how many queued events a single transaction writes.
const batchSize = 128

// flushEvery bounds how long an event can sit in the queue before it is written
// even when the queue has not filled a batch.
const flushEvery = 250 * time.Millisecond

// Async is the enabled Recorder. Record appends to a bounded channel and returns
// immediately; a single goroutine drains the channel into the Store in batches.
// This is what keeps the per-call overhead under the NFR-1 budget and guarantees
// callers never block on disk I/O.
type Async struct {
	store *Store
	ch    chan Event
	done  chan struct{}
	once  sync.Once

	mu      sync.Mutex
	dropped int
}

// NewAsync starts an enabled recorder writing to store.
func NewAsync(store *Store) *Async {
	a := &Async{
		store: store,
		ch:    make(chan Event, queueSize),
		done:  make(chan struct{}),
	}
	go a.run()
	return a
}

// Record queues an event without blocking. When the queue is full (the sink is
// slower than the call rate) the event is dropped and counted.
func (a *Async) Record(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	if e.Attempts == 0 {
		e.Attempts = 1
	}
	select {
	case a.ch <- e:
	default:
		a.mu.Lock()
		a.dropped++
		a.mu.Unlock()
	}
}

// Enabled reports true for an async recorder.
func (a *Async) Enabled() bool { return true }

// Dropped reports how many events were discarded because the queue was full.
func (a *Async) Dropped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dropped
}

// Flush stops accepting events, drains the queue with a bounded wait, and
// returns. It is safe to call more than once.
func (a *Async) Flush(timeout time.Duration) {
	a.once.Do(func() {
		close(a.ch)
		select {
		case <-a.done:
		case <-time.After(timeout):
			// The writer is wedged (for example a locked database); abandon the
			// remaining events rather than delay the command.
		}
	})
}

func (a *Async) run() {
	defer close(a.done)
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	batch := make([]Event, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Best-effort: a failed write drops the batch. Telemetry must never
		// surface as a command failure (NFR-2).
		_ = a.store.InsertBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case e, ok := <-a.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, e)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Ensure Async satisfies Recorder at compile time.
var _ Recorder = (*Async)(nil)

// Multi fans one event out to several recorders, so both the per-run in-memory
// aggregator and the persistent sink can observe every event.
type Multi struct {
	recorders []Recorder
}

// NewMulti combines recorders. Nil entries are ignored.
func NewMulti(rs ...Recorder) *Multi {
	var kept []Recorder
	for _, r := range rs {
		if r != nil {
			kept = append(kept, r)
		}
	}
	return &Multi{recorders: kept}
}

// Record forwards to every recorder.
func (m *Multi) Record(e Event) {
	for _, r := range m.recorders {
		r.Record(e)
	}
}

// Enabled reports whether any recorder is enabled.
func (m *Multi) Enabled() bool {
	for _, r := range m.recorders {
		if r.Enabled() {
			return true
		}
	}
	return false
}

// Flush flushes every recorder.
func (m *Multi) Flush(timeout time.Duration) {
	for _, r := range m.recorders {
		r.Flush(timeout)
	}
}
