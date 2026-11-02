package cache

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/tomzxcode/ghx/internal/logging"
)

// captureLogs points the global logger at a buffer for the test and restores
// stderr and the info level afterwards.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logging.SetOutput(&buf)
	t.Cleanup(func() {
		logging.SetOutput(os.Stderr)
		_ = logging.SetLevel("info")
	})
	return &buf
}

// TestSQLiteStore_LogsQueries verifies that with debug logging every statement
// is logged with the store operation it serves, its SQL, and its bound
// arguments.
func TestSQLiteStore_LogsQueries(t *testing.T) {
	buf := captureLogs(t)
	if err := logging.SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug): %v", err)
	}

	s, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	const host, owner, repo = "github.com", "acme", "big"
	if err := s.SavePR(host, owner, repo, samplePR(42)); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
	if _, _, err := s.LoadPR(host, owner, repo, 42); err != nil {
		t.Fatalf("LoadPR: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"sqlite query",
		"operation=SavePR",
		`sql="INSERT OR REPLACE INTO pull_requests`,
		`args="[github.com, acme, big, 42`,
		"duration=",
		"operation=LoadPR",
		`sql="SELECT number, title, state, is_draft`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q; got:\n%s", want, out)
		}
	}
}

// TestSQLiteStore_QueryLogRequiresDebug verifies that the statements stay out of
// the log at the default info level.
func TestSQLiteStore_QueryLogRequiresDebug(t *testing.T) {
	buf := captureLogs(t)
	if err := logging.SetLevel("info"); err != nil {
		t.Fatalf("SetLevel(info): %v", err)
	}

	s, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	if err := s.SavePR("github.com", "acme", "big", samplePR(1)); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
	if strings.Contains(buf.String(), "sqlite query") {
		t.Errorf("statements were logged at info level:\n%s", buf.String())
	}
}

// TestFormatQueryArgs_TruncatesLongValues verifies that a large bound value, such
// as a cached body or JSON collection, is cut down on the log line.
func TestFormatQueryArgs_TruncatesLongValues(t *testing.T) {
	got := formatQueryArgs([]any{"short", strings.Repeat("x", 200)})

	if !strings.Contains(got, "short") {
		t.Errorf("formatQueryArgs = %q, want the short value kept", got)
	}
	if !strings.Contains(got, strings.Repeat("x", maxQueryArgLen)+"...") {
		t.Errorf("formatQueryArgs = %q, want the long value truncated at %d chars", got, maxQueryArgLen)
	}
	if strings.Contains(got, strings.Repeat("x", maxQueryArgLen+1)) {
		t.Errorf("formatQueryArgs = %q, want the long value cut", got)
	}
}
