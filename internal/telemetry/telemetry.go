// Package telemetry records local, opt-in timing events for GitHub API calls and
// cache operations. It exists so performance work and the mock server's
// simulation generator can rely on measured per-call costs rather than assumed
// ones.
//
// The recorder is always present but is a no-op unless explicitly enabled. Every
// write path is best-effort: a failure to record never fails, slows, or changes
// the exit code of the command that produced the event.
package telemetry

import "time"

// Event kinds. A kind is the unit of analysis: durations are grouped by kind so
// the cost of a PR full page is not averaged together with the cost of a scan
// page. New kinds are additive; readers must tolerate unknown values.
const (
	// API call kinds.
	KindIssueList    = "issue_list"
	KindIssueGet     = "issue_get"
	KindIssueFull    = "issue_full_fetch"
	KindPRList       = "pr_list"
	KindPRGet        = "pr_get"
	KindPRFull       = "pr_full_fetch"
	KindPRScan       = "pr_scan"
	KindPRFullPage   = "pr_full_page"
	KindIssueSearch  = "issue_search"
	KindPRSearch     = "pr_search"
	KindIssueMigrate = "migrate" // reserved; unused by the API client
	KindUnknown      = "unknown"
)

// Cache operation kinds.
const (
	KindCacheQueryIssues = "cache_query_issues"
	KindCacheQueryPRs    = "cache_query_prs"
	KindCacheLoadIssue   = "cache_load_issue"
	KindCacheLoadPR      = "cache_load_pr"
	KindCacheSaveIssue   = "cache_save_issue"
	KindCacheSavePR      = "cache_save_pr"
	KindCacheLoadAll     = "cache_load_all"
	KindCacheInfo        = "cache_info"
	KindCacheOther       = "cache_other"
)

// Event is one recorded call. API and cache events share the struct so the sink
// has a single row type; fields that do not apply to a given kind are left at
// their zero value and stored as NULL.
type Event struct {
	// Timestamp is when the call started, in UTC.
	Timestamp time.Time
	// Kind groups events for analysis (see the Kind* constants).
	Kind string
	// Operation is the concrete GraphQL operation or store method name.
	Operation string
	// Host is the API host, empty for cache operations.
	Host string
	// Repo is owner/repo, empty when unknown.
	Repo string
	// Status is the HTTP status of the last attempt, 0 for cache operations.
	Status int
	// Duration is the total call duration, including retry backoff waits.
	Duration time.Duration
	// PageSize is the requested page size for paginated calls, 0 otherwise.
	PageSize int
	// ReturnedItems is how many nodes the response actually carried, 0 when
	// unknown. It differs from PageSize at the end of a window.
	ReturnedItems int
	// RequestBytes is the marshalled request body size in bytes, 0 when unknown.
	RequestBytes int
	// ResponseBytes is the response body size in bytes, 0 when unknown.
	ResponseBytes int
	// Cursor reports whether a pagination cursor was supplied.
	Cursor bool
	// Attempts is the number of attempts made (1 when not retried).
	Attempts int
	// RateRemaining is the x-ratelimit-remaining value when the response
	// carried one, -1 when unknown.
	RateRemaining int
	// Backend is the cache backend kind ("sqlite" or "file"), empty for API calls.
	Backend string
	// Items is the number of items read or written by a cache operation.
	Items int
	// Failed reports whether the call errored.
	Failed bool
}

// Retried reports whether the call took more than one attempt.
func (e Event) Retried() bool { return e.Attempts > 1 }

// Recorder accepts events. The enabled implementation buffers them; the disabled
// implementation drops them, so callers invoke Record unconditionally.
type Recorder interface {
	// Record queues one event. It never blocks on I/O and never returns an error.
	Record(e Event)
	// Enabled reports whether events are being written.
	Enabled() bool
	// Flush drains queued events, waiting at most timeout, then stops the writer.
	Flush(timeout time.Duration)
}

// Nop is a Recorder that discards every event. It is the disabled default.
type Nop struct{}

// Record discards the event.
func (Nop) Record(Event) {}

// Enabled reports false.
func (Nop) Enabled() bool { return false }

// Flush does nothing.
func (Nop) Flush(time.Duration) {}

// Ensure Nop satisfies Recorder at compile time.
var _ Recorder = Nop{}
