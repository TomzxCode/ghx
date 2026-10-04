package logging

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		name string
		want slog.Level
		ok   bool
	}{
		{"", slog.LevelInfo, true},
		{"info", slog.LevelInfo, true},
		{"INFO", slog.LevelInfo, true},
		{" debug ", slog.LevelDebug, true},
		{"warn", slog.LevelWarn, true},
		{"warning", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"verbose", 0, false},
	}
	for _, tc := range cases {
		got, err := ParseLevel(tc.name)
		if tc.ok && err != nil {
			t.Errorf("ParseLevel(%q) unexpected error: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("ParseLevel(%q) expected error, got %v", tc.name, got)
		}
		if tc.ok && got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSetLevel_ControlsEmission verifies SetLevel changes the global threshold
// so debug records are suppressed at info and emitted at debug.
func TestSetLevel_ControlsEmission(t *testing.T) {
	t.Cleanup(func() { _ = SetLevel("info") })

	if err := SetLevel("info"); err != nil {
		t.Fatalf("SetLevel(info): %v", err)
	}
	if Logger().Enabled(nil, slog.LevelDebug) {
		t.Error("debug should be disabled at info level")
	}
	if !Logger().Enabled(nil, slog.LevelInfo) {
		t.Error("info should be enabled at info level")
	}

	if err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug): %v", err)
	}
	if !Logger().Enabled(nil, slog.LevelDebug) {
		t.Error("debug should be enabled at debug level")
	}

	if err := SetLevel("bogus"); err == nil {
		t.Error("SetLevel(bogus) expected an error")
	}
}
