package cache

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
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

// TestSQLiteStore_OpenChecksEachTableOnce verifies that a first open reads each
// table's columns once instead of once per pending migration, and that a second
// open at the current schema version probes nothing and rewrites no version.
func TestSQLiteStore_OpenChecksEachTableOnce(t *testing.T) {
	buf := captureLogs(t)
	if err := logging.SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug): %v", err)
	}
	dir := t.TempDir()

	s, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	s.Close()

	out := buf.String()
	for _, table := range []string{"pull_requests", "cache_meta", "issues"} {
		if got := strings.Count(out, `sql="PRAGMA table_info(`+table+`)"`); got != 1 {
			t.Errorf("table %s probed %d times on first open, want 1", table, got)
		}
	}
	if !strings.Contains(out, "PRAGMA user_version = 4") {
		t.Errorf("first open did not stamp the schema version; got:\n%s", out)
	}

	buf.Reset()
	s, err = NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("second NewSQLiteStore: %v", err)
	}
	s.Close()

	out = buf.String()
	if strings.Contains(out, "PRAGMA table_info") {
		t.Errorf("second open probed table columns; got:\n%s", out)
	}
	if strings.Contains(out, "PRAGMA user_version =") {
		t.Errorf("second open rewrote the schema version; got:\n%s", out)
	}
}

// TestSQLiteStore_KeepsNewerSchemaVersion verifies that a database stamped by a
// later ghx is left alone instead of being migrated and stamped back down.
func TestSQLiteStore_KeepsNewerSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	s.Close()

	path := filepath.Join(dir, DBFileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		db.Close()
		t.Fatalf("setting user_version: %v", err)
	}
	db.Close()

	s, err = NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore after newer version: %v", err)
	}
	s.Close()

	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open after reopen: %v", err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("reading user_version: %v", err)
	}
	if version != 99 {
		t.Errorf("user_version = %d after reopen, want 99 kept", version)
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
