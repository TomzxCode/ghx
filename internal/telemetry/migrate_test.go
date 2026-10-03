package telemetry

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// oldSchema is the telemetry_events table as it existed before response sizes
// and returned item counts were added.
const oldSchema = `
CREATE TABLE telemetry_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp      TEXT    NOT NULL,
    kind           TEXT    NOT NULL,
    operation      TEXT    NOT NULL,
    host           TEXT,
    repo           TEXT,
    status         INTEGER,
    duration_ms    INTEGER NOT NULL,
    page_size      INTEGER,
    cursor         INTEGER NOT NULL DEFAULT 0,
    attempts       INTEGER NOT NULL DEFAULT 1,
    retried        INTEGER NOT NULL DEFAULT 0,
    rate_remaining INTEGER,
    backend        TEXT,
    items          INTEGER,
    failed         INTEGER NOT NULL DEFAULT 0
);`

// TC-10b: opening a database written by an older binary migrates it additively
// and preserves existing rows, so schema additions are never breaking.
func TestOpenMigratesOldSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// Create a database with the pre-size schema and one row.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("creating old schema: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO telemetry_events (timestamp, kind, operation, duration_ms, attempts)
		 VALUES (?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), "pr_full_page", "fetch_pr_full_page", 7000, 1,
	); err != nil {
		t.Fatalf("inserting legacy row: %v", err)
	}
	db.Close()

	// The newer binary opens it.
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrating): %v", err)
	}
	defer store.Close()

	events, err := store.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].DurationMs != 7000 {
		t.Errorf("duration = %d, want 7000", events[0].DurationMs)
	}
	// The new columns exist and default to zero on legacy rows.
	if events[0].ResponseBytes != 0 || events[0].ReturnedItems != 0 {
		t.Errorf("legacy row sizes = %d/%d, want 0/0",
			events[0].ResponseBytes, events[0].ReturnedItems)
	}

	// A row written by the newer binary carries the new fields.
	if err := store.InsertBatch([]Event{{
		Timestamp:     time.Now(),
		Kind:          "pr_scan",
		Operation:     "fetch_prs_updated_scan",
		Duration:      300 * time.Millisecond,
		ReturnedItems: 100,
		ResponseBytes: 5120,
		Attempts:      1,
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	fresh, _ := store.Query(Filter{Kind: "pr_scan"})
	if len(fresh) != 1 || fresh[0].ResponseBytes != 5120 || fresh[0].ReturnedItems != 100 {
		t.Fatalf("new row not recorded correctly: %+v", fresh)
	}
}
