package github

import (
	"encoding/json"
	"time"

	"github.com/tomzxcode/ghx/internal/logging"
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
	reqBytes := requestBytes(query, variables)
	stats, err := c.queryWithStats(query, variables, result)
	d := time.Since(start)
	c.recordCall(ctx, start, d, reqBytes, stats)
	logCall(ctx, d, reqBytes, stats, err)
	return err
}

// logCall emits one debug line per GraphQL request so individual fetches (one
// per page, typically) are visible instead of only the command-level totals.
// Every field describes this call alone, so lines stay correct even while the
// cache backfill fetches several pages in parallel.
func logCall(ctx CallContext, d time.Duration, reqBytes int, stats callStats, err error) {
	args := []any{
		"operation", ctx.Operation,
		"kind", ctx.Kind,
		"repo", ctx.Repo,
		"page_size", ctx.PageSize,
		"cursor", ctx.Cursor,
		"status", stats.status,
		"attempts", stats.attemptCount(),
		"items", returnedItems(ctx, stats),
		"request_bytes", reqBytes,
		"response_bytes", stats.responseBytes,
		"duration", d,
	}
	if err != nil {
		args = append(args, "error", err)
	}
	logging.Debug("github request", args...)
}

// returnedItems reports how many nodes the response carried, preferring the
// caller's own count when it set one.
func returnedItems(ctx CallContext, stats callStats) int {
	if ctx.ReturnedItems > 0 {
		return ctx.ReturnedItems
	}
	return stats.items
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
func (c *Client) recordCall(ctx CallContext, start time.Time, d time.Duration, reqBytes int, stats callStats) {
	rec := c.recorder
	if rec == nil || !rec.Enabled() {
		return
	}
	kind := ctx.Kind
	if kind == "" {
		kind = telemetry.KindUnknown
	}
	rec.Record(telemetry.Event{
		Timestamp:     start,
		Kind:          kind,
		Operation:     ctx.Operation,
		Host:          c.host,
		Repo:          ctx.Repo,
		Status:        stats.status,
		Duration:      d,
		PageSize:      ctx.PageSize,
		ReturnedItems: returnedItems(ctx, stats),
		RequestBytes:  reqBytes,
		ResponseBytes: stats.responseBytes,
		Cursor:        ctx.Cursor,
		Attempts:      stats.attemptCount(),
		RateRemaining: stats.rateRemaining,
		Failed:        stats.failed,
	})
}
