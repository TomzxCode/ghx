package telemetry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DBFileName is the telemetry database file name under the telemetry directory.
const DBFileName = "telemetry.db"

// schemaSQL creates the append-only event table. It is intentionally additive:
// new fields become nullable columns so a newer binary can read a database
// written by an older one and vice versa.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS telemetry_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp      TEXT    NOT NULL,
    kind           TEXT    NOT NULL,
    operation      TEXT    NOT NULL,
    host           TEXT,
    repo           TEXT,
    status         INTEGER,
    duration_ms    INTEGER NOT NULL,
    page_size      INTEGER,
    items_returned INTEGER,
    request_bytes  INTEGER,
    response_bytes INTEGER,
    cursor         INTEGER NOT NULL DEFAULT 0,
    attempts       INTEGER NOT NULL DEFAULT 1,
    retried        INTEGER NOT NULL DEFAULT 0,
    rate_remaining INTEGER,
    backend        TEXT,
    items          INTEGER,
    failed         INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_telemetry_timestamp ON telemetry_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_telemetry_kind      ON telemetry_events(kind);
CREATE INDEX IF NOT EXISTS idx_telemetry_repo      ON telemetry_events(repo);
CREATE INDEX IF NOT EXISTS idx_telemetry_kind_time ON telemetry_events(kind, timestamp);
`

// DefaultDir returns the default directory for the telemetry database. It sits
// next to the cache under the user data directory, never inside the object
// cache.
func DefaultDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "ghx")
}

// DefaultPath returns the default telemetry database path.
func DefaultPath() string {
	dir := DefaultDir()
	if dir == "" {
		return DBFileName
	}
	return filepath.Join(dir, DBFileName)
}

// Open opens (creating if needed) the telemetry database at path and returns a
// store ready to accept batches. The caller must Close it.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating telemetry dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening telemetry db: %w", err)
	}
	// A single writer connection avoids "database is locked" churn between the
	// batched writer goroutine and any reader in the same process.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 2000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("applying %q: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying telemetry schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating telemetry schema: %w", err)
	}
	return &Store{db: db, path: path}, nil
}

// migrate adds columns introduced after the initial schema to databases created
// by an earlier binary. It is additive and idempotent: adding a column that
// already exists is treated as already applied, so a newer binary can read an
// older database and vice versa (forward compatibility).
func migrate(db *sql.DB) error {
	existing, err := columns(db, "telemetry_events")
	if err != nil {
		return err
	}
	additive := []struct{ name, ddl string }{
		{"items_returned", "ALTER TABLE telemetry_events ADD COLUMN items_returned INTEGER"},
		{"request_bytes", "ALTER TABLE telemetry_events ADD COLUMN request_bytes INTEGER"},
		{"response_bytes", "ALTER TABLE telemetry_events ADD COLUMN response_bytes INTEGER"},
	}
	for _, m := range additive {
		if existing[m.name] {
			continue
		}
		if _, err := db.Exec(m.ddl); err != nil {
			return fmt.Errorf("adding %s: %w", m.name, err)
		}
	}
	return nil
}

// columns reports which columns a table already has.
func columns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var (
			cid         int
			name, ctype string
			notnull, pk int
			dflt        sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// Store is the SQLite sink for telemetry events.
type Store struct {
	db   *sql.DB
	path string
}

// Path reports the database file path.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// InsertBatch writes every event in one transaction. A failure aborts the whole
// batch, and the caller drops the events rather than retrying into a hot loop.
func (s *Store) InsertBatch(events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO telemetry_events
		 (timestamp, kind, operation, host, repo, status, duration_ms,
		  page_size, items_returned, request_bytes, response_bytes,
		  cursor, attempts, retried, rate_remaining, backend, items, failed)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, e := range events {
		if _, err := stmt.Exec(
			e.Timestamp.UTC().Format(time.RFC3339Nano),
			e.Kind,
			e.Operation,
			nullString(e.Host),
			nullString(e.Repo),
			nullInt(e.Status, e.Status != 0),
			e.Duration.Milliseconds(),
			nullInt(e.PageSize, e.PageSize != 0),
			nullInt(e.ReturnedItems, e.ReturnedItems != 0),
			nullInt(e.RequestBytes, e.RequestBytes != 0),
			nullInt(e.ResponseBytes, e.ResponseBytes != 0),
			boolInt(e.Cursor),
			maxInt(e.Attempts, 1),
			boolInt(e.Retried()),
			nullInt(e.RateRemaining, e.RateRemaining >= 0),
			nullString(e.Backend),
			nullInt(e.Items, e.Items != 0),
			boolInt(e.Failed),
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Query reads events back, applying the given filter. The result is ordered by
// timestamp ascending so summaries are stable.
func (s *Store) Query(f Filter) ([]StoredEvent, error) {
	q := `SELECT timestamp, kind, operation, host, repo, status, duration_ms,
	             page_size, items_returned, request_bytes, response_bytes,
	             cursor, attempts, retried, rate_remaining, backend, items, failed
	      FROM telemetry_events`
	where, args := f.clause()
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY timestamp ASC, id ASC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StoredEvent
	for rows.Next() {
		var (
			ts, kind, op              string
			host, repo, backend       sql.NullString
			status, pageSize          sql.NullInt64
			itemsReturned             sql.NullInt64
			reqBytes, respBytes       sql.NullInt64
			rate, items               sql.NullInt64
			dur                       int64
			cursor, attempts, retried int64
			failed                    int64
		)
		if err := rows.Scan(&ts, &kind, &op, &host, &repo, &status, &dur,
			&pageSize, &itemsReturned, &reqBytes, &respBytes,
			&cursor, &attempts, &retried, &rate, &backend, &items, &failed); err != nil {
			return nil, err
		}
		parsed, _ := time.Parse(time.RFC3339Nano, ts)
		out = append(out, StoredEvent{
			Timestamp:     parsed,
			Kind:          kind,
			Operation:     op,
			Host:          host.String,
			Repo:          repo.String,
			Status:        int(status.Int64),
			DurationMs:    dur,
			PageSize:      int(pageSize.Int64),
			ReturnedItems: int(itemsReturned.Int64),
			RequestBytes:  int(reqBytes.Int64),
			ResponseBytes: int(respBytes.Int64),
			Cursor:        cursor != 0,
			Attempts:      int(attempts),
			Retried:       retried != 0,
			RateRemaining: int(rate.Int64),
			Backend:       backend.String,
			Items:         int(items.Int64),
			Failed:        failed != 0,
		})
	}
	return out, rows.Err()
}

// Count reports the number of stored events matching the filter.
func (s *Store) Count(f Filter) (int, error) {
	q := "SELECT COUNT(*) FROM telemetry_events"
	where, args := f.clause()
	if where != "" {
		q += " WHERE " + where
	}
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CountBefore reports how many events DeleteBefore(t) would remove.
func (s *Store) CountBefore(t time.Time) (int, error) {
	q := "SELECT COUNT(*) FROM telemetry_events"
	args := []interface{}{}
	if !t.IsZero() {
		q += " WHERE timestamp < ?"
		args = append(args, t.UTC().Format(time.RFC3339Nano))
	}
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// DeleteBefore removes events strictly before t, returning how many were
// removed. A zero t deletes every event.
func (s *Store) DeleteBefore(t time.Time) (int, error) {
	var (
		res sql.Result
		err error
	)
	if t.IsZero() {
		res, err = s.db.Exec("DELETE FROM telemetry_events")
	} else {
		res, err = s.db.Exec("DELETE FROM telemetry_events WHERE timestamp < ?",
			t.UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Filter narrows a read. Zero values mean "no filter".
type Filter struct {
	Kind  string
	Repo  string
	Since time.Time
}

func (f Filter) clause() (string, []interface{}) {
	var parts []string
	var args []interface{}
	if f.Kind != "" {
		parts = append(parts, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Repo != "" {
		parts = append(parts, "repo = ?")
		args = append(args, f.Repo)
	}
	if !f.Since.IsZero() {
		parts = append(parts, "timestamp >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	where := ""
	for i, p := range parts {
		if i > 0 {
			where += " AND "
		}
		where += p
	}
	return where, args
}

// StoredEvent is one event as read back from the database.
type StoredEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	Kind          string    `json:"kind"`
	Operation     string    `json:"operation"`
	Host          string    `json:"host,omitempty"`
	Repo          string    `json:"repo,omitempty"`
	Status        int       `json:"status,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	PageSize      int       `json:"page_size,omitempty"`
	ReturnedItems int       `json:"items_returned,omitempty"`
	RequestBytes  int       `json:"request_bytes,omitempty"`
	ResponseBytes int       `json:"response_bytes,omitempty"`
	Cursor        bool      `json:"cursor"`
	Attempts      int       `json:"attempts"`
	Retried       bool      `json:"retried"`
	RateRemaining int       `json:"rate_remaining,omitempty"`
	Backend       string    `json:"backend,omitempty"`
	Items         int       `json:"items,omitempty"`
	Failed        bool      `json:"failed,omitempty"`
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int, valid bool) interface{} {
	if !valid {
		return nil
	}
	return n
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
