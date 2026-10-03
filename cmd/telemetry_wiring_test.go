package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

// resetTelemetryState clears the package-level recorder and flag so each
// precedence case starts clean.
func resetTelemetryState(t *testing.T) {
	t.Helper()
	telemetryRecorder = telemetry.Nop{}
	telemetryStore = nil
	telemetryAggregator = nil
	telemetryFlag = nil
	telemetryDBFlag = ""
	t.Cleanup(func() {
		telemetryRecorder = telemetry.Nop{}
		telemetryStore = nil
		telemetryAggregator = nil
		telemetryFlag = nil
		telemetryDBFlag = ""
	})
}

// With no flag, env, or config, telemetry is enabled by default.
func TestTelemetryWantedDefaultOn(t *testing.T) {
	resetTelemetryState(t)
	t.Setenv("GHX_TELEMETRY", "")
	os.Unsetenv("GHX_TELEMETRY")
	telemetryDBFlag = filepath.Join(t.TempDir(), "telemetry.db")
	if !telemetryWanted() {
		t.Error("telemetry should be enabled by default")
	}
}

// A persisted disable turns recording off.
func TestTelemetryWantedConfigDisabled(t *testing.T) {
	resetTelemetryState(t)
	os.Unsetenv("GHX_TELEMETRY")
	dir := t.TempDir()
	telemetryDBFlag = filepath.Join(dir, "telemetry.db")
	if err := telemetry.SaveConfig(telemetry.ConfigPath(telemetryDBFlag), telemetry.Config{Enabled: false}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if telemetryWanted() {
		t.Error("a persisted disable should turn telemetry off")
	}
}

// The flag overrides both the config and the environment.
func TestTelemetryWantedFlagPrecedence(t *testing.T) {
	resetTelemetryState(t)
	dir := t.TempDir()
	telemetryDBFlag = filepath.Join(dir, "telemetry.db")
	if err := telemetry.SaveConfig(telemetry.ConfigPath(telemetryDBFlag), telemetry.Config{Enabled: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	off := false
	telemetryFlag = &off
	if telemetryWanted() {
		t.Error("--telemetry=false should win over an enabled config")
	}
	on := true
	telemetryFlag = &on
	t.Setenv("GHX_TELEMETRY", "0")
	if !telemetryWanted() {
		t.Error("--telemetry should win over GHX_TELEMETRY=0")
	}
}

// The environment variable overrides the persisted setting, both ways.
func TestTelemetryWantedEnvPrecedence(t *testing.T) {
	resetTelemetryState(t)
	dir := t.TempDir()
	telemetryDBFlag = filepath.Join(dir, "telemetry.db")
	if err := telemetry.SaveConfig(telemetry.ConfigPath(telemetryDBFlag), telemetry.Config{Enabled: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	t.Setenv("GHX_TELEMETRY", "0")
	if telemetryWanted() {
		t.Error("GHX_TELEMETRY=0 should override an enabled config")
	}
	t.Setenv("GHX_TELEMETRY", "1")
	if err := telemetry.SaveConfig(telemetry.ConfigPath(telemetryDBFlag), telemetry.Config{Enabled: false}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if !telemetryWanted() {
		t.Error("GHX_TELEMETRY=1 should override a disabled config")
	}
}

// A corrupt config falls back to the default rather than erroring.
func TestTelemetryWantedCorruptConfig(t *testing.T) {
	resetTelemetryState(t)
	os.Unsetenv("GHX_TELEMETRY")
	dir := t.TempDir()
	telemetryDBFlag = filepath.Join(dir, "telemetry.db")
	if err := os.WriteFile(telemetry.ConfigPath(telemetryDBFlag), []byte("{not json"), 0644); err != nil {
		t.Fatalf("writing corrupt config: %v", err)
	}
	if !telemetryWanted() {
		t.Error("a corrupt config should fall back to the default (enabled)")
	}
}
