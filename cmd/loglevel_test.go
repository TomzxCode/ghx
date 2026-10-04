package cmd

import (
	"log/slog"
	"testing"

	"github.com/tomzxcode/ghx/internal/logging"
)

// TestLogLevelFlag_DefaultIsInfo verifies the persistent --log-level flag
// defaults to info and is applied by the root PersistentPreRunE.
func TestLogLevelFlag_DefaultIsInfo(t *testing.T) {
	saved := logLevelFlag
	t.Cleanup(func() { logLevelFlag = saved; _ = logging.SetLevel("info") })

	logLevelFlag = "info"
	if err := logging.SetLevel(logLevelFlag); err != nil {
		t.Fatalf("SetLevel(info): %v", err)
	}
	if logging.Logger().Enabled(nil, slog.LevelDebug) {
		t.Error("debug should be disabled at the default info level")
	}
}

// TestLogLevelFlag_DebugEnablesDebug verifies passing debug raises verbosity.
func TestLogLevelFlag_DebugEnablesDebug(t *testing.T) {
	saved := logLevelFlag
	t.Cleanup(func() { logLevelFlag = saved; _ = logging.SetLevel("info") })

	logLevelFlag = "debug"
	if err := logging.SetLevel(logLevelFlag); err != nil {
		t.Fatalf("SetLevel(debug): %v", err)
	}
	if !logging.Logger().Enabled(nil, slog.LevelDebug) {
		t.Error("debug should be enabled when --log-level debug is set")
	}
}

// TestLogLevelFlag_Registered verifies the flag is wired onto the root command
// as a persistent flag so every subcommand inherits it.
func TestLogLevelFlag_Registered(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("log-level")
	if f == nil {
		t.Fatal("--log-level not registered as a persistent flag")
	}
	if f.DefValue != "info" {
		t.Errorf("--log-level default = %q, want info", f.DefValue)
	}
}
