package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
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

// TestHandler_TimestampPrefix verifies every line starts with a local-time
// timestamp before the level, so log output can be ordered and correlated.
func TestHandler_TimestampPrefix(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newHandler(&buf, level))
	logger.Info("hello", "k", "v")

	line := strings.TrimSuffix(buf.String(), "\n")
	fields := strings.SplitN(line, " ", 4)
	if len(fields) < 4 {
		t.Fatalf("log line has too few fields: %q", line)
	}

	stamp := fields[0] + " " + fields[1]
	ts, err := time.ParseInLocation(timeFormat, stamp, time.Local)
	if err != nil {
		t.Fatalf("timestamp %q does not parse as %q: %v", stamp, timeFormat, err)
	}
	if time.Since(ts) > time.Minute {
		t.Errorf("timestamp %v is not close to now", ts)
	}
	if fields[2] != "info" {
		t.Errorf("level field = %q, want info", fields[2])
	}
	if !strings.HasPrefix(fields[3], "hello") {
		t.Errorf("message field = %q, want it to start with hello", fields[3])
	}
}
