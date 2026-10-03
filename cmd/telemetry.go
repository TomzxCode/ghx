package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

var (
	telemetrySummaryKind  string
	telemetrySummaryRepo  string
	telemetrySummarySince string
	telemetrySummaryJSON  bool
	telemetryExportFormat string
	telemetryExportKind   string
	telemetryExportRepo   string
	telemetryExportSince  string
	telemetryExportOutput string
	telemetryClearBefore  string
	telemetryClearYes     bool
)

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Inspect and export recorded API and cache timings",
	Long: `Reads the local telemetry database written when recording is enabled
(--telemetry or GHX_TELEMETRY=1). The data is local-only and never uploaded.`,
}

var telemetrySummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Counts, duration percentiles, and retry rates by operation kind",
	RunE:  runTelemetrySummary,
}

var telemetryExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Emit recorded events as JSON",
	RunE:  runTelemetryExport,
}

var telemetryClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete recorded events (destructive)",
	RunE:  runTelemetryClear,
}

func init() {
	telemetryCmd.AddCommand(telemetrySummaryCmd)
	telemetryCmd.AddCommand(telemetryExportCmd)
	telemetryCmd.AddCommand(telemetryClearCmd)

	telemetrySummaryCmd.Flags().StringVar(&telemetrySummaryKind, "kind", "", "Restrict to one operation kind")
	telemetrySummaryCmd.Flags().StringVar(&telemetrySummaryRepo, "repo", "", "Restrict to one repository (owner/repo)")
	telemetrySummaryCmd.Flags().StringVar(&telemetrySummarySince, "since", "", "Only events at or after this date (YYYY-MM-DD or RFC3339)")
	telemetrySummaryCmd.Flags().BoolVar(&telemetrySummaryJSON, "json", false, "Emit the summary as JSON")

	telemetryExportCmd.Flags().StringVar(&telemetryExportFormat, "format", "json", "Output format (json)")
	telemetryExportCmd.Flags().StringVar(&telemetryExportKind, "kind", "", "Restrict to one operation kind")
	telemetryExportCmd.Flags().StringVar(&telemetryExportRepo, "repo", "", "Restrict to one repository (owner/repo)")
	telemetryExportCmd.Flags().StringVar(&telemetryExportSince, "since", "", "Only events at or after this date (YYYY-MM-DD or RFC3339)")
	telemetryExportCmd.Flags().StringVarP(&telemetryExportOutput, "output", "o", "", "Write to a file instead of stdout")

	telemetryClearCmd.Flags().StringVar(&telemetryClearBefore, "before", "", "Delete only events strictly before this date")
	telemetryClearCmd.Flags().BoolVarP(&telemetryClearYes, "yes", "y", false, "Skip the confirmation prompt")
}

// openTelemetryForRead opens the database for a read command, returning a clear
// error only when it is genuinely absent.
func openTelemetryForRead() (*telemetry.Store, string, error) {
	path := resolveTelemetryDBPath()
	if _, err := os.Stat(path); err != nil {
		return nil, path, fmt.Errorf("no telemetry data at %s", path)
	}
	store, err := telemetry.Open(path)
	if err != nil {
		return nil, path, fmt.Errorf("opening telemetry data at %s: %w", path, err)
	}
	return store, path, nil
}

// queryWithRetry reads events, retrying briefly while a concurrent writer is
// checkpointing the WAL. An in-flight `ghx cache` keeps the database open and
// commits into the -wal sidecar, so a reader can momentarily observe zero rows
// even though data exists; retrying avoids reporting a false "no data".
func queryWithRetry(store *telemetry.Store, f telemetry.Filter) ([]telemetry.StoredEvent, error) {
	var events []telemetry.StoredEvent
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		events, err = store.Query(f)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}
		// No rows visible yet: the writer may be mid-commit. Wait and retry,
		// but only for the unfiltered case, where "empty" is unambiguous once
		// the retries are exhausted.
		if f.Kind != "" || f.Repo != "" || !f.Since.IsZero() {
			return events, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return events, nil
}

// parseTelemetrySince parses the --since value shared by the read commands.
func parseTelemetrySince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid --since value %q", s)
}

func runTelemetrySummary(cmd *cobra.Command, args []string) error {
	since, err := parseTelemetrySince(telemetrySummarySince)
	if err != nil {
		return err
	}
	store, _, err := openTelemetryForRead()
	if err != nil {
		return err
	}
	defer store.Close()

	events, err := queryWithRetry(store, telemetry.Filter{
		Kind:  telemetrySummaryKind,
		Repo:  telemetrySummaryRepo,
		Since: since,
	})
	if err != nil {
		return err
	}
	if len(events) == 0 {
		// Distinguish "nothing ever recorded" from "nothing matched the
		// filter", and never call an actively-written database empty.
		if filtered := telemetrySummaryKind != "" || telemetrySummaryRepo != "" || !since.IsZero(); filtered {
			fmt.Println("No telemetry events matched the filter.")
			return nil
		}
		return fmt.Errorf("no telemetry events recorded at %s", store.Path())
	}

	summaries := summarize(events)
	if telemetrySummaryJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "kind\tcount\tp50_ms\tp95_ms\tretry_rate\tsent\treceived\tp50_size\tp50_items")
	var totalSent, totalRecv int64
	for _, s := range summaries {
		totalSent += s.TotalSentBytes
		totalRecv += s.TotalRecvBytes
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%.2f\t%s\t%s\t%s\t%d\n",
			s.Kind, s.Count, s.P50Ms, s.P95Ms, s.RetryRate,
			humanBytes(s.TotalSentBytes), humanBytes(s.TotalRecvBytes),
			humanBytes(s.P50Bytes), s.P50Items)
	}
	if len(summaries) > 1 {
		fmt.Fprintf(w, "TOTAL\t%d\t\t\t\t%s\t%s\t\t\n",
			totalEvents(summaries), humanBytes(totalSent), humanBytes(totalRecv))
	}
	return w.Flush()
}

// totalEvents sums the per-kind counts across a summary set.
func totalEvents(summaries []kindSummary) int {
	n := 0
	for _, s := range summaries {
		n += s.Count
	}
	return n
}

// humanBytes formats a byte count with a binary unit, keeping the summary
// readable when totals reach hundreds of megabytes.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(n)
	for i := 0; ; i++ {
		value /= unit
		if value < unit || i == len(units)-1 {
			return fmt.Sprintf("%.1f%s", value, units[i])
		}
	}
}

// kindSummary is the per-kind read-side aggregate.
type kindSummary struct {
	Kind           string  `json:"kind"`
	Count          int     `json:"count"`
	P50Ms          int64   `json:"p50_ms"`
	P95Ms          int64   `json:"p95_ms"`
	RetryRate      float64 `json:"retry_rate"`
	Failed         int     `json:"failed"`
	TotalMs        int64   `json:"total_ms"`
	TotalSentBytes int64   `json:"total_sent_bytes"`
	TotalRecvBytes int64   `json:"total_received_bytes"`
	P50Bytes       int64   `json:"p50_response_bytes"`
	P95Bytes       int64   `json:"p95_response_bytes"`
	P50Items       int     `json:"p50_items_returned"`
}

// summarize groups stored events by kind and computes percentiles.
func summarize(events []telemetry.StoredEvent) []kindSummary {
	byKind := map[string][]telemetry.StoredEvent{}
	for _, e := range events {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	out := make([]kindSummary, 0, len(byKind))
	for kind, es := range byKind {
		durations := make([]int64, 0, len(es))
		var (
			bytesSamples []int64
			itemSamples  []int64
		)
		var retried, failed, total, sent, recv int64
		for _, e := range es {
			durations = append(durations, e.DurationMs)
			total += e.DurationMs
			sent += int64(e.RequestBytes)
			recv += int64(e.ResponseBytes)
			if e.ResponseBytes > 0 {
				bytesSamples = append(bytesSamples, int64(e.ResponseBytes))
			}
			if e.ReturnedItems > 0 {
				itemSamples = append(itemSamples, int64(e.ReturnedItems))
			}
			if e.Retried {
				retried++
			}
			if e.Failed {
				failed++
			}
		}
		ks := kindSummary{
			Kind:           kind,
			Count:          len(es),
			P50Ms:          percentileOf(durations, 50),
			P95Ms:          percentileOf(durations, 95),
			Failed:         int(failed),
			TotalMs:        total,
			TotalSentBytes: sent,
			TotalRecvBytes: recv,
			P50Bytes:       percentileOf(bytesSamples, 50),
			P95Bytes:       percentileOf(bytesSamples, 95),
		}
		if len(itemSamples) > 0 {
			ks.P50Items = int(percentileOf(itemSamples, 50))
		}
		if len(es) > 0 {
			ks.RetryRate = float64(retried) / float64(len(es))
		}
		out = append(out, ks)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalMs != out[j].TotalMs {
			return out[i].TotalMs > out[j].TotalMs
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func runTelemetryExport(cmd *cobra.Command, args []string) error {
	if telemetryExportFormat != "json" {
		return fmt.Errorf("invalid --format %q: use --format json", telemetryExportFormat)
	}
	since, err := parseTelemetrySince(telemetryExportSince)
	if err != nil {
		return err
	}
	store, path, err := openTelemetryForRead()
	if err != nil {
		return err
	}
	defer store.Close()

	events, err := queryWithRetry(store, telemetry.Filter{
		Kind:  telemetryExportKind,
		Repo:  telemetryExportRepo,
		Since: since,
	})
	if err != nil {
		return err
	}
	if events == nil {
		events = []telemetry.StoredEvent{}
	}

	out := os.Stdout
	if telemetryExportOutput != "" {
		f, err := os.Create(telemetryExportOutput)
		if err != nil {
			return fmt.Errorf("creating %s: %w", telemetryExportOutput, err)
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(events); err != nil {
		return err
	}
	if telemetryExportOutput != "" {
		fmt.Printf("Exported %d event(s) to %s\n", len(events), telemetryExportOutput)
	} else {
		_ = path
	}
	return nil
}

func runTelemetryClear(cmd *cobra.Command, args []string) error {
	before, err := parseTelemetrySince(telemetryClearBefore)
	if err != nil {
		return err
	}
	store, path, err := openTelemetryForRead()
	if err != nil {
		return err
	}
	defer store.Close()

	// Count what would actually be deleted: events strictly before the cutoff,
	// or everything when no cutoff is given.
	n, err := store.CountBefore(before)
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Println("Nothing to delete.")
		return nil
	}

	if !telemetryClearYes {
		scope := "all recorded events"
		if !before.IsZero() {
			scope = fmt.Sprintf("%d recorded event(s) before %s", n, before.Format("2006-01-02"))
		}
		fmt.Printf("Delete %s from %s? [y/N] ", scope, path)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	deleted, err := store.DeleteBefore(before)
	if err != nil {
		return err
	}
	fmt.Printf("Deleted %d event(s).\n", deleted)
	return nil
}

// percentileOf returns the nearest-rank percentile of an unsorted set.
func percentileOf(samples []int64, p int) int64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (p*len(sorted) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}
