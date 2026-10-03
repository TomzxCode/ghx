package github

import (
	"encoding/json"
	"time"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

// CallContext labels one instrumented GraphQL call so the telemetry event can be
// attributed to an operation kind rather than lumped under "query". Callers set
// Operation as the GraphQL operation name (or a descriptive equivalent) and Kind
// as the analysis grouping.
type CallContext struct {
	// Operation is a stable operation name, e.g. "fetch_all_issues".
	Operation string
	// Kind is the analysis grouping, e.g. telemetry.KindPRFullPage.
	Kind string
	// Repo is owner/repo when known.
	Repo string
	// PageSize is the requested page size for paginated calls.
	PageSize int
	// ReturnedItems is how many nodes the response carried. Callers that can
	// count the page set it; 0 means "not reported".
	ReturnedItems int
	// Cursor reports whether a pagination cursor was supplied.
	Cursor bool
}

// SetTelemetry attaches a recorder to the client. A nil recorder disables
// recording. It must be called before the first request.
func (c *Client) SetTelemetry(rec telemetry.Recorder) {
	if rec == nil {
		c.recorder = telemetry.Nop{}
		return
	}
	c.recorder = rec
}

// QueryCtx is Query with an explicit operation label. All internal callers use
// it so every recorded call is attributable to a kind.
func (c *Client) QueryCtx(ctx CallContext, query string, variables map[string]interface{}, result interface{}) error {
	start := time.Now()
	err := c.Query(query, variables, result)
	c.recordCall(ctx, start, time.Since(start), requestBytes(query, variables))
	return err
}

// requestBytes estimates the marshalled request size without re-marshalling on
// the hot path; the exact size is only needed for telemetry, so an approximation
// that matches json.Marshal's output length for the same payload is sufficient.
func requestBytes(query string, variables map[string]interface{}) int {
	body, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return 0
	}
	return len(body)
}

// recordCall emits one telemetry event for a completed call. It is best-effort:
// a disabled recorder drops it, and nothing here can fail the call.
func (c *Client) recordCall(ctx CallContext, start time.Time, d time.Duration, reqBytes int) {
	rec := c.recorder
	if rec == nil || !rec.Enabled() {
		return
	}
	kind := ctx.Kind
	if kind == "" {
		kind = telemetry.KindUnknown
	}
	attempts := c.lastAttempts.Load()
	if attempts < 1 {
		attempts = 1
	}
	returned := ctx.ReturnedItems
	if returned == 0 {
		if n := c.lastItems.Load(); n > 0 {
			returned = int(n)
		}
	}
	rec.Record(telemetry.Event{
		Timestamp:     start,
		Kind:          kind,
		Operation:     ctx.Operation,
		Host:          c.host,
		Repo:          ctx.Repo,
		Status:        int(c.lastStatus.Load()),
		Duration:      d,
		PageSize:      ctx.PageSize,
		ReturnedItems: returned,
		RequestBytes:  reqBytes,
		ResponseBytes: int(c.lastResponseBytes.Load()),
		Cursor:        ctx.Cursor,
		Attempts:      int(attempts),
		RateRemaining: c.RateLimitRemaining(),
		Failed:        c.lastErr.Load() != 0,
	})
}
