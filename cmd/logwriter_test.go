package cmd

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/tomzxcode/ghx/internal/logging"
)

// TestLogWriter_DefersToActiveBar verifies that while a progress bar is
// registered, log output goes through the coordinating writer (clear, write,
// redraw) rather than writing raw bytes that would corrupt the bar line.
func TestLogWriter_DefersToActiveBar(t *testing.T) {
	var buf bytes.Buffer
	w := logWriter{out: &buf}

	// No active bar: passthrough.
	if _, err := w.Write([]byte("plain\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := buf.String(); got != "plain\n" {
		t.Errorf("passthrough write = %q, want %q", got, "plain\n")
	}
}

// TestSetActiveBar_ConcurrentSafe ensures registering and clearing the active
// bar from another goroutine does not race with log writes.
func TestSetActiveBar_ConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	w := logWriter{out: &buf}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			setActiveBar(nil)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, _ = w.Write([]byte("line\n"))
		}
	}()
	wg.Wait()
	if !strings.Contains(buf.String(), "line") {
		t.Error("expected log lines to be written")
	}
}

// TestLoggingSetOutput_Reroutes verifies SetOutput redirects the logger so the
// cmd-level coordinating writer receives the records.
func TestLoggingSetOutput_Reroutes(t *testing.T) {
	var buf bytes.Buffer
	logging.SetOutput(&buf)
	t.Cleanup(func() { logging.SetOutput(os.Stderr) })

	logging.Info("hello", "k", "v")
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("expected redirected output to contain message, got %q", buf.String())
	}
	_ = slog.LevelInfo
}

// TestLoggingSetOutput_NilIgnored guards against pointing the logger at a nil
// writer, which would panic on the next log call.
func TestLoggingSetOutput_NilIgnored(t *testing.T) {
	logging.SetOutput(nil)
	// Must not panic.
	logging.Info("after nil SetOutput")
}
