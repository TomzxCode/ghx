package cmd

import (
	"os"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

// telemetryRecorder is the process-wide recorder. It is opened lazily on first
// use and is flushed once at command exit.
var telemetryRecorder telemetry.Recorder = telemetry.Nop{}

// telemetryStore is the persistent sink when telemetry is enabled.
var telemetryStore *telemetry.Store

// telemetryAggregator accumulates this run's events for the post-run summary.
var telemetryAggregator *telemetry.Aggregator

// telemetryEnabled reports whether recording is currently on.
func telemetryEnabled() bool {
	return telemetryRecorder != nil && telemetryRecorder.Enabled()
}

// resolveTelemetryDBPath applies flag, then env, then default precedence.
func resolveTelemetryDBPath() string {
	if telemetryDBFlag != "" {
		return telemetryDBFlag
	}
	if env := os.Getenv("GHX_TELEMETRY_DB"); env != "" {
		return env
	}
	return telemetry.DefaultPath()
}

// telemetryWanted decides whether this invocation should record, using the
// precedence: explicit flag, then environment variable, then the persisted
// global setting, then the shipped default (enabled).
//
// Both the flag and the env var accept an explicit off, so a single run can opt
// out without changing the global setting: `--telemetry=false` or
// `GHX_TELEMETRY=0`.
func telemetryWanted() bool {
	if telemetryFlag != nil {
		return *telemetryFlag
	}
	if env, ok := os.LookupEnv("GHX_TELEMETRY"); ok {
		return envTruthy(env)
	}
	cfgPath := telemetry.ConfigPath(resolveTelemetryDBPath())
	if cfg, ok := telemetry.LoadConfig(cfgPath); ok {
		return cfg.Enabled
	}
	// Missing or corrupt config: fall back to the shipped default rather than
	// treating an unreadable file as an explicit "disabled".
	return telemetry.DefaultConfig().Enabled
}

// envTruthy parses an environment value as a boolean, treating the common deny
// values as off.
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off", "":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// initTelemetry resolves whether telemetry is enabled and opens the sink. It is
// idempotent and safe to call from every command; a failure to open the sink
// degrades to a disabled recorder rather than failing the command (NFR-2).
func initTelemetry() telemetry.Recorder {
	if telemetryEnabled() {
		return telemetryRecorder
	}
	if !telemetryWanted() {
		telemetryRecorder = telemetry.Nop{}
		return telemetryRecorder
	}

	store, err := telemetry.Open(resolveTelemetryDBPath())
	if err != nil {
		// Recording is best-effort: an unusable sink disables telemetry for
		// this run instead of failing the command.
		telemetryRecorder = telemetry.Nop{}
		return telemetryRecorder
	}
	telemetryStore = store
	telemetryAggregator = telemetry.NewAggregator()
	telemetryRecorder = telemetry.NewMulti(
		telemetry.NewAsync(store),
		telemetryAggregator,
	)
	return telemetryRecorder
}

// flushTelemetry drains queued events and closes the sink. Call it once, after
// the command's work is done. It never returns an error: a flush failure is
// invisible to the user (NFR-2).
func flushTelemetry() {
	if telemetryRecorder == nil {
		return
	}
	telemetryRecorder.Flush(2 * time.Second)
	if telemetryStore != nil {
		_ = telemetryStore.Close()
	}
}
