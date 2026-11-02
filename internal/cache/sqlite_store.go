package cache

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/logging"
)

//go:embed schema.sql
var schemaSQL string

// DBFileName is the SQLite database file created under the cache root.
const DBFileName = "cache.db"

// sqliteStore is the SQLite cache backend. A single database holds every
// repository, keyed by (host, owner, repo).
type sqliteStore struct {
	db   *sql.DB
	path string
}

// NewSQLiteStore opens (or creates) the SQLite cache database under baseDir,
// applying the schema if needed. The returned store must be closed by the
// caller.
func NewSQLiteStore(baseDir string) (Store, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}
	path := filepath.Join(baseDir, DBFileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite cache: %w", err)
	}
	// SQLite allows a single writer; a single pooled connection avoids
	// "database is locked" churn within one process.
	db.SetMaxOpenConns(1)

	// busy_timeout is applied before the journal mode switch: setting WAL takes
	// a lock another ghx process may hold, and until the busy handler is
	// installed that attempt returns SQLITE_BUSY at once instead of waiting, so
	// parallel invocations fail while the cache database is being created.
	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := execLogged(db, "Open", pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("applying %q: %w", pragma, err)
		}
	}
	if _, err := execLogged(db, "Schema", schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	version, err := sqliteUserVersion(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("reading schema version: %w", err)
	}
	if version < schemaVersionKeysNormalized {
		if err := normalizeSQLiteKeys(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("normalizing cache keys: %w", err)
		}
	}
	// An up-to-date database skips the column probes and the version write, so
	// opening the cache costs a single PRAGMA. A newer version (written by a
	// later ghx) is left alone rather than stamped down.
	if version < schemaVersionCurrent {
		if err := migrateSQLiteSchema(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrating schema: %w", err)
		}
		if _, err := execLogged(db, "Schema", fmt.Sprintf("PRAGMA user_version = %d", schemaVersionCurrent)); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting schema version: %w", err)
		}
	}
	return normalize(&sqliteStore{db: db, path: path}), nil
}

// migrateSQLiteSchema applies additive column migrations to databases created
// by an earlier schema revision. schema.sql runs CREATE TABLE IF NOT EXISTS, so
// a column added to an existing table must be added here instead. Each table's
// columns are read once, however many migrations it needs, and a migration just
// applied updates the cached column set.
func migrateSQLiteSchema(db *sql.DB) error {
	columns := map[string]map[string]bool{}
	for _, m := range []struct{ table, column, ddl string }{
		{
			table:  "pull_requests",
			column: "head_ref_oid",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN head_ref_oid TEXT NOT NULL DEFAULT ''",
		},
		{
			table:  "pull_requests",
			column: "reviews",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN reviews TEXT NOT NULL DEFAULT '[]'",
		},
		{
			table:  "pull_requests",
			column: "timeline",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN timeline TEXT NOT NULL DEFAULT '[]'",
		},
		{
			table:  "pull_requests",
			column: "review_requests",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN review_requests TEXT",
		},
		{
			table:  "cache_meta",
			column: "issues_cached_at",
			ddl:    "ALTER TABLE cache_meta ADD COLUMN issues_cached_at TEXT",
		},
		{
			table:  "cache_meta",
			column: "prs_cached_at",
			ddl:    "ALTER TABLE cache_meta ADD COLUMN prs_cached_at TEXT",
		},
		{
			table:  "cache_meta",
			column: "pr_page_cursor",
			ddl:    "ALTER TABLE cache_meta ADD COLUMN pr_page_cursor TEXT",
		},
		{
			table:  "cache_meta",
			column: "pr_floor",
			ddl:    "ALTER TABLE cache_meta ADD COLUMN pr_floor TEXT",
		},
		{
			table:  "pull_requests",
			column: "additions",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN additions INTEGER NOT NULL DEFAULT 0",
		},
		{
			table:  "pull_requests",
			column: "deletions",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN deletions INTEGER NOT NULL DEFAULT 0",
		},
		{
			table:  "issues",
			column: "author_type",
			ddl:    "ALTER TABLE issues ADD COLUMN author_type TEXT NOT NULL DEFAULT ''",
		},
		{
			table:  "pull_requests",
			column: "checks",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN checks TEXT",
		},
		{
			table:  "pull_requests",
			column: "review_threads",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN review_threads TEXT",
		},
		{
			table:  "pull_requests",
			column: "author_type",
			ddl:    "ALTER TABLE pull_requests ADD COLUMN author_type TEXT NOT NULL DEFAULT ''",
		},
	} {
		cols, ok := columns[m.table]
		if !ok {
			var err error
			cols, err = tableColumns(db, m.table)
			if err != nil {
				return err
			}
			columns[m.table] = cols
		}
		if cols[m.column] {
			continue
		}
		if _, err := execLogged(db, "Migrate", m.ddl); err != nil {
			return fmt.Errorf("adding %s.%s: %w", m.table, m.column, err)
		}
		cols[m.column] = true
	}
	return nil
}

// tableColumns returns a table's column names, read once per table so a run of
// migrations for the same table does not repeat the probe.
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := queryLogged(db, "Migrate", `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// schemaVersionCurrent is the SQLite user_version written once the schema and
// its additive column migrations are up to date. Bump it whenever a migration is
// added to migrateSQLiteSchema, so an up-to-date database skips the column
// probes on later opens.
const schemaVersionCurrent = 4

// Kind reports the backend name.
func (s *sqliteStore) Kind() string { return "sqlite" }

// Location reports the database path.
func (s *sqliteStore) Location() string { return s.path }

// Close closes the database connection.
func (s *sqliteStore) Close() error { return s.db.Close() }

// ---------------------------------------------------------------------------
// Encoding helpers
// ---------------------------------------------------------------------------

const sqliteTimeLayout = time.RFC3339Nano

func tstr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(sqliteTimeLayout)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(sqliteTimeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// nullTime renders an optional time for SQL binding (nil -> NULL).
func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(sqliteTimeLayout)
}

// nullJSON renders an optional JSON value for SQL binding. A nil interface or a
// typed nil pointer (e.g. (*github.Milestone)(nil)) binds as SQL NULL, so an
// absent optional struct does not round-trip into a non-nil zero value.
func nullJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Values are marshalled from plain structs; failure is not expected.
		return "null"
	}
	return string(b)
}

func decodeJSON(data string, v any) error {
	if data == "" {
		return nil
	}
	return json.Unmarshal([]byte(data), v)
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// sqlRunner is satisfied by *sql.DB and *sql.Tx, so the logging helpers cover
// statements run inside and outside a transaction.
type sqlRunner interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// maxQueryArgLen caps one bound argument's rendering, so a debug line stays
// readable when the argument is a cached body or a JSON collection.
const maxQueryArgLen = 60

// logSQL emits one debug line per statement: the store operation, the SQL with
// its whitespace collapsed, the bound arguments, and how long it took. The
// duration covers the statement's execution, not the caller's row scanning.
func logSQL(op, query string, args []any, start time.Time, err error) {
	if !logging.DebugEnabled() {
		return
	}
	attrs := []any{"operation", op, "sql", strings.Join(strings.Fields(query), " ")}
	if len(args) > 0 {
		attrs = append(attrs, "args", formatQueryArgs(args))
	}
	attrs = append(attrs, "duration", time.Since(start))
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	logging.Debug("sqlite query", attrs...)
}

// formatQueryArgs renders bound arguments on one line, truncating each value.
func formatQueryArgs(args []any) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		s := fmt.Sprint(arg)
		if len(s) > maxQueryArgLen {
			s = s[:maxQueryArgLen] + "..."
		}
		parts[i] = s
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func execLogged(r sqlRunner, op, query string, args ...any) (sql.Result, error) {
	start := time.Now()
	res, err := r.Exec(query, args...)
	logSQL(op, query, args, start, err)
	return res, err
}

func queryLogged(r sqlRunner, op, query string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	rows, err := r.Query(query, args...)
	logSQL(op, query, args, start, err)
	return rows, err
}

func queryRowLogged(r sqlRunner, op, query string, args ...any) *sql.Row {
	start := time.Now()
	row := r.QueryRow(query, args...)
	logSQL(op, query, args, start, nil)
	return row
}

// ---------------------------------------------------------------------------
// Issues
// ---------------------------------------------------------------------------

const issueCols = `number, title, state, author_login, author_type, assignees, labels, milestone,
	created_at, updated_at, closed_at, url, body, comment_count, comments, row_mtime`

// SaveIssue writes a single issue to the database.
func (s *sqliteStore) SaveIssue(host, owner, repo string, issue *github.Issue) error {
	milestone, err := nullJSON(issue.Milestone)
	if err != nil {
		return err
	}
	_, err = execLogged(s.db, "SaveIssue", `INSERT OR REPLACE INTO issues
		(host, owner, repo, number, title, state, author_login, author_type, assignees, labels, milestone,
		 created_at, updated_at, closed_at, url, body, comment_count, comments, row_mtime)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		host, owner, repo, issue.Number, issue.Title, issue.State, issue.Author.Login, issue.Author.Type,
		mustJSON(issue.Assignees), mustJSON(issue.Labels), milestone,
		tstr(issue.CreatedAt), tstr(issue.UpdatedAt), nullTime(issue.ClosedAt),
		issue.URL, issue.Body, issue.CommentCount, mustJSON(issue.Comments), tstr(time.Now()),
	)
	if err != nil {
		return fmt.Errorf("saving issue #%d: %w", issue.Number, err)
	}
	return nil
}

func scanIssue(row rowScanner) (*github.Issue, time.Time, error) {
	var (
		number                                int
		title, state, authorLogin, authorType string
		assigneesJSON, labelsJSON             string
		milestoneJSON                         sql.NullString
		createdAtS, updatedAtS                string
		closedAtS                             sql.NullString
		url, body, commentsJSON, rowMtimeS    string
		commentCount                          int
	)
	if err := row.Scan(&number, &title, &state, &authorLogin, &authorType, &assigneesJSON, &labelsJSON, &milestoneJSON,
		&createdAtS, &updatedAtS, &closedAtS, &url, &body, &commentCount, &commentsJSON, &rowMtimeS); err != nil {
		return nil, time.Time{}, err
	}

	issue := &github.Issue{
		Number:       number,
		Title:        title,
		State:        state,
		Author:       github.Actor{Login: authorLogin, Type: authorType},
		URL:          url,
		Body:         body,
		CommentCount: commentCount,
		CreatedAt:    parseTime(createdAtS),
		UpdatedAt:    parseTime(updatedAtS),
	}
	if err := decodeJSON(assigneesJSON, &issue.Assignees); err != nil {
		return nil, time.Time{}, err
	}
	if err := decodeJSON(labelsJSON, &issue.Labels); err != nil {
		return nil, time.Time{}, err
	}
	if err := decodeJSON(commentsJSON, &issue.Comments); err != nil {
		return nil, time.Time{}, err
	}
	if milestoneJSON.Valid && milestoneJSON.String != "" {
		var m github.Milestone
		if err := json.Unmarshal([]byte(milestoneJSON.String), &m); err != nil {
			return nil, time.Time{}, err
		}
		issue.Milestone = &m
	}
	if closedAtS.Valid && closedAtS.String != "" {
		t := parseTime(closedAtS.String)
		issue.ClosedAt = &t
	}
	return issue, parseTime(rowMtimeS), nil
}

// LoadIssue reads a single issue, returning the row's last-write time.
func (s *sqliteStore) LoadIssue(host, owner, repo string, number int) (*github.Issue, time.Time, error) {
	row := queryRowLogged(s.db, "LoadIssue", `SELECT `+issueCols+` FROM issues WHERE host=? AND owner=? AND repo=? AND number=?`,
		host, owner, repo, number)
	return scanIssue(row)
}

// LoadAllIssues reads every cached issue for a repository.
func (s *sqliteStore) LoadAllIssues(host, owner, repo string) ([]*github.Issue, error) {
	rows, err := queryLogged(s.db, "LoadAllIssues", `SELECT `+issueCols+` FROM issues WHERE host=? AND owner=? AND repo=? ORDER BY number`,
		host, owner, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*github.Issue
	for rows.Next() {
		issue, _, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// QueryIssues pushes the scalar predicates to indexed SQL columns to obtain a
// candidate set, then applies the canonical in-memory filter so the result set
// matches the file backend exactly. Label/milestone/search predicates are not
// pushed down (they involve JSON collections or substring matching).
func (s *sqliteStore) QueryIssues(host, owner, repo string, q IssueQuery) ([]*github.Issue, error) {
	where := []string{"host = ?", "owner = ?", "repo = ?"}
	args := []any{host, owner, repo}
	if q.State != "" && !strings.EqualFold(q.State, "all") {
		where = append(where, "state = ? COLLATE NOCASE")
		args = append(args, q.State)
	}
	if q.Author != "" {
		where = append(where, "author_login = ? COLLATE NOCASE")
		args = append(args, q.Author)
	}
	rows, err := queryLogged(s.db, "QueryIssues", `SELECT `+issueCols+` FROM issues WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*github.Issue
	for rows.Next() {
		issue, _, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return filterIssues(issues, q), nil
}

// ---------------------------------------------------------------------------
// Pull requests
// ---------------------------------------------------------------------------

const prCols = `number, title, state, is_draft, author_login, author_type, assignees, labels, milestone,
	base_ref_name, head_ref_name, head_ref_oid, created_at, updated_at, merged_at, closed_at,
	url, body, comment_count, comments, reviews, timeline, review_requests, review_decision,
	checks, review_threads, additions, deletions, row_mtime`

// SavePR writes a single pull request to the database.
func (s *sqliteStore) SavePR(host, owner, repo string, pr *github.PullRequest) error {
	milestone, err := nullJSON(pr.Milestone)
	if err != nil {
		return err
	}
	isDraft := 0
	if pr.IsDraft {
		isDraft = 1
	}
	reviewDecision := any(nil)
	if pr.ReviewDecision != "" {
		reviewDecision = pr.ReviewDecision
	}
	var reviewRequests any
	if pr.ReviewRequests != nil {
		reviewRequests = mustJSON(pr.ReviewRequests)
	}
	var checks, reviewThreads any
	if pr.Checks != nil {
		checks = mustJSON(pr.Checks)
	}
	if pr.ReviewThreads != nil {
		reviewThreads = mustJSON(pr.ReviewThreads)
	}
	_, err = execLogged(s.db, "SavePR", `INSERT OR REPLACE INTO pull_requests
		(host, owner, repo, number, title, state, is_draft, author_login, author_type, assignees, labels, milestone,
		 base_ref_name, head_ref_name, head_ref_oid, created_at, updated_at, merged_at, closed_at,
		 url, body, comment_count, comments, reviews, timeline, review_requests, review_decision,
		 checks, review_threads, additions, deletions, row_mtime)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		host, owner, repo, pr.Number, pr.Title, pr.State, isDraft, pr.Author.Login, pr.Author.Type,
		mustJSON(pr.Assignees), mustJSON(pr.Labels), milestone,
		pr.BaseRefName, pr.HeadRefName, pr.HeadRefOid, tstr(pr.CreatedAt), tstr(pr.UpdatedAt),
		nullTime(pr.MergedAt), nullTime(pr.ClosedAt),
		pr.URL, pr.Body, pr.CommentCount, mustJSON(pr.Comments), mustJSON(pr.Reviews), mustJSON(pr.Timeline),
		reviewRequests, reviewDecision, checks, reviewThreads, pr.Additions, pr.Deletions, tstr(time.Now()),
	)
	if err != nil {
		return fmt.Errorf("saving PR #%d: %w", pr.Number, err)
	}
	return nil
}

func scanPR(row rowScanner) (*github.PullRequest, time.Time, error) {
	var (
		number                    int
		title, state, authorLogin string
		authorType                string
		isDraft                   bool
		assigneesJSON, labelsJSON string
		milestoneJSON             sql.NullString
		baseRef, headRef          string
		headRefOid                string
		createdAtS, updatedAtS    string
		mergedAtS, closedAtS      sql.NullString
		url, body, commentsJSON   string
		reviewsJSON, timelineJSON string
		reviewRequestsJSON        sql.NullString
		checksJSON                sql.NullString
		reviewThreadsJSON         sql.NullString
		commentCount              int
		reviewDecision            sql.NullString
		additions, deletions      int
		rowMtimeS                 string
	)
	if err := row.Scan(&number, &title, &state, &isDraft, &authorLogin, &authorType, &assigneesJSON, &labelsJSON, &milestoneJSON,
		&baseRef, &headRef, &headRefOid, &createdAtS, &updatedAtS, &mergedAtS, &closedAtS,
		&url, &body, &commentCount, &commentsJSON, &reviewsJSON, &timelineJSON, &reviewRequestsJSON,
		&reviewDecision, &checksJSON, &reviewThreadsJSON, &additions, &deletions, &rowMtimeS); err != nil {
		return nil, time.Time{}, err
	}

	pr := &github.PullRequest{
		Number:       number,
		Title:        title,
		State:        state,
		IsDraft:      isDraft,
		Author:       github.Actor{Login: authorLogin, Type: authorType},
		BaseRefName:  baseRef,
		HeadRefName:  headRef,
		HeadRefOid:   headRefOid,
		CreatedAt:    parseTime(createdAtS),
		UpdatedAt:    parseTime(updatedAtS),
		URL:          url,
		Body:         body,
		CommentCount: commentCount,
		Additions:    additions,
		Deletions:    deletions,
	}
	if reviewDecision.Valid {
		pr.ReviewDecision = reviewDecision.String
	}
	if err := decodeJSON(assigneesJSON, &pr.Assignees); err != nil {
		return nil, time.Time{}, err
	}
	if err := decodeJSON(labelsJSON, &pr.Labels); err != nil {
		return nil, time.Time{}, err
	}
	if err := decodeJSON(commentsJSON, &pr.Comments); err != nil {
		return nil, time.Time{}, err
	}
	if err := decodeJSON(reviewsJSON, &pr.Reviews); err != nil {
		return nil, time.Time{}, err
	}
	if err := decodeJSON(timelineJSON, &pr.Timeline); err != nil {
		return nil, time.Time{}, err
	}
	if reviewRequestsJSON.Valid {
		if err := decodeJSON(reviewRequestsJSON.String, &pr.ReviewRequests); err != nil {
			return nil, time.Time{}, err
		}
	}
	if checksJSON.Valid && checksJSON.String != "" {
		pr.Checks = &github.PRChecks{}
		if err := decodeJSON(checksJSON.String, pr.Checks); err != nil {
			return nil, time.Time{}, err
		}
	}
	if reviewThreadsJSON.Valid && reviewThreadsJSON.String != "" {
		pr.ReviewThreads = &github.PRThreadCounts{}
		if err := decodeJSON(reviewThreadsJSON.String, pr.ReviewThreads); err != nil {
			return nil, time.Time{}, err
		}
	}
	if milestoneJSON.Valid && milestoneJSON.String != "" {
		var m github.Milestone
		if err := json.Unmarshal([]byte(milestoneJSON.String), &m); err != nil {
			return nil, time.Time{}, err
		}
		pr.Milestone = &m
	}
	if mergedAtS.Valid && mergedAtS.String != "" {
		t := parseTime(mergedAtS.String)
		pr.MergedAt = &t
	}
	if closedAtS.Valid && closedAtS.String != "" {
		t := parseTime(closedAtS.String)
		pr.ClosedAt = &t
	}
	return pr, parseTime(rowMtimeS), nil
}

// LoadPR reads a single pull request, returning the row's last-write time.
func (s *sqliteStore) LoadPR(host, owner, repo string, number int) (*github.PullRequest, time.Time, error) {
	row := queryRowLogged(s.db, "LoadPR", `SELECT `+prCols+` FROM pull_requests WHERE host=? AND owner=? AND repo=? AND number=?`,
		host, owner, repo, number)
	return scanPR(row)
}

// LoadAllPRs reads every cached pull request for a repository.
func (s *sqliteStore) LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error) {
	rows, err := queryLogged(s.db, "LoadAllPRs", `SELECT `+prCols+` FROM pull_requests WHERE host=? AND owner=? AND repo=? ORDER BY number`,
		host, owner, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prs []*github.PullRequest
	for rows.Next() {
		pr, _, err := scanPR(rows)
		if err != nil {
			return nil, err
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

// QueryPRs pushes the scalar predicates (repo, state, author, base, head, draft)
// to indexed SQL columns, then applies the canonical in-memory filter for exact
// equivalence with the file backend.
func (s *sqliteStore) QueryPRs(host, owner, repo string, q PRQuery) ([]*github.PullRequest, error) {
	where := []string{"host = ?", "owner = ?", "repo = ?"}
	args := []any{host, owner, repo}
	if q.State != "" && !strings.EqualFold(q.State, "all") {
		where = append(where, "state = ? COLLATE NOCASE")
		args = append(args, q.State)
	}
	if q.Author != "" {
		where = append(where, "author_login = ? COLLATE NOCASE")
		args = append(args, q.Author)
	}
	if q.BaseRef != "" {
		where = append(where, "base_ref_name = ? COLLATE NOCASE")
		args = append(args, q.BaseRef)
	}
	if q.HeadRef != "" {
		where = append(where, "head_ref_name = ? COLLATE NOCASE")
		args = append(args, q.HeadRef)
	}
	if q.Draft {
		where = append(where, "is_draft = 1")
	}
	rows, err := queryLogged(s.db, "QueryPRs", `SELECT `+prCols+` FROM pull_requests WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prs []*github.PullRequest
	for rows.Next() {
		pr, _, err := scanPR(rows)
		if err != nil {
			return nil, err
		}
		prs = append(prs, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return filterPRs(prs, q), nil
}

// ---------------------------------------------------------------------------
// Cache metadata
// ---------------------------------------------------------------------------

// SaveCacheInfo marks the cache complete at the current time with the given duration.
func (s *sqliteStore) SaveCacheInfo(host, owner, repo string, duration int) error {
	now := time.Now()
	return s.SaveCacheInfoFull(host, owner, repo, &CacheInfo{
		CachedAt:       now,
		Duration:       duration,
		Complete:       true,
		IssuesCachedAt: now,
		PRsCachedAt:    now,
	})
}

// SaveCacheInfoFull upserts the cache metadata row.
func (s *sqliteStore) SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error {
	complete := 0
	if info.Complete {
		complete = 1
	}
	_, err := execLogged(s.db, "SaveCacheInfoFull", `INSERT OR REPLACE INTO cache_meta
		(host, owner, repo, cached_at, duration, complete, issue_cursor, pr_cursor, pr_page_cursor, pr_floor, issues_cached_at, prs_cached_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		host, owner, repo, tstr(info.CachedAt), info.Duration, complete,
		nullTime(info.IssueCursor), nullTime(info.PRCursor), info.PRPageCursor, nullTime(info.PRFloor),
		nullTime(&info.IssuesCachedAt), nullTime(&info.PRsCachedAt),
	)
	if err != nil {
		return fmt.Errorf("saving cache info: %w", err)
	}
	return nil
}

// LoadCacheInfo reads the cache metadata row.
func (s *sqliteStore) LoadCacheInfo(host, owner, repo string) (*CacheInfo, error) {
	var (
		cachedAtS                     string
		duration, complete            int
		issueCursorS, prCursorS       sql.NullString
		prPageCursorS, prFloorS       sql.NullString
		issuesCachedAtS, prsCachedAtS sql.NullString
	)
	row := queryRowLogged(s.db, "LoadCacheInfo", `SELECT cached_at, duration, complete, issue_cursor, pr_cursor, pr_page_cursor, pr_floor, issues_cached_at, prs_cached_at
		FROM cache_meta WHERE host=? AND owner=? AND repo=?`, host, owner, repo)
	if err := row.Scan(&cachedAtS, &duration, &complete, &issueCursorS, &prCursorS, &prPageCursorS, &prFloorS, &issuesCachedAtS, &prsCachedAtS); err != nil {
		return nil, err
	}
	info := &CacheInfo{
		CachedAt: parseTime(cachedAtS),
		Duration: duration,
		Complete: complete != 0,
	}
	if prPageCursorS.Valid {
		info.PRPageCursor = prPageCursorS.String
	}
	if prFloorS.Valid && prFloorS.String != "" {
		t := parseTime(prFloorS.String)
		info.PRFloor = &t
	}
	if issueCursorS.Valid && issueCursorS.String != "" {
		t := parseTime(issueCursorS.String)
		info.IssueCursor = &t
	}
	if prCursorS.Valid && prCursorS.String != "" {
		t := parseTime(prCursorS.String)
		info.PRCursor = &t
	}
	if issuesCachedAtS.Valid && issuesCachedAtS.String != "" {
		info.IssuesCachedAt = parseTime(issuesCachedAtS.String)
	}
	if prsCachedAtS.Valid && prsCachedAtS.String != "" {
		info.PRsCachedAt = parseTime(prsCachedAtS.String)
	}
	return info, nil
}

// IsCacheFresh reports whether the cache is complete and both issues and PRs
// were populated within the stored duration.
func (s *sqliteStore) IsCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	d := time.Duration(info.Duration) * time.Minute
	return time.Since(info.IssuesUpdatedAt()) < d && time.Since(info.PRsUpdatedAt()) < d, nil
}

// IsIssuesCacheFresh reports whether issues were cached within the stored duration.
func (s *sqliteStore) IsIssuesCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return info.IssuesFreshAt(time.Now()), nil
}

// IsPRsCacheFresh reports whether pull requests were cached within the stored duration.
func (s *sqliteStore) IsPRsCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return info.PRsFreshAt(time.Now()), nil
}

// IsCacheFreshWithDuration reports whether the cache is complete and both
// issues and PRs were populated within the given duration.
func (s *sqliteStore) IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	d := time.Duration(duration) * time.Minute
	return time.Since(info.IssuesUpdatedAt()) < d && time.Since(info.PRsUpdatedAt()) < d, nil
}

// IsIssuesCacheFreshWithDuration reports whether issues were cached within the given duration (minutes).
func (s *sqliteStore) IsIssuesCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return time.Since(info.IssuesUpdatedAt()) < time.Duration(duration)*time.Minute, nil
}

// IsPRsCacheFreshWithDuration reports whether PRs were cached within the given duration (minutes).
func (s *sqliteStore) IsPRsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return time.Since(info.PRsUpdatedAt()) < time.Duration(duration)*time.Minute, nil
}

// ListCachedRepos returns every repository present in the database.
//
// The cache metadata is joined in a single query rather than loaded per row:
// with a single pooled connection (SetMaxOpenConns(1)), issuing a second query
// while the result set is still open would deadlock.
func (s *sqliteStore) ListCachedRepos() ([]CachedRepo, error) {
	rows, err := queryLogged(s.db, "ListCachedRepos", `
		SELECT u.host, u.owner, u.repo,
		       m.cached_at, m.duration, m.complete, m.issue_cursor, m.pr_cursor,
		       m.pr_page_cursor, m.pr_floor, m.issues_cached_at, m.prs_cached_at,
		       (SELECT COUNT(*) FROM issues i WHERE i.host=u.host AND i.owner=u.owner AND i.repo=u.repo),
		       (SELECT COUNT(*) FROM pull_requests p WHERE p.host=u.host AND p.owner=u.owner AND p.repo=u.repo)
		FROM (
			SELECT host, owner, repo FROM cache_meta
			UNION SELECT host, owner, repo FROM issues
			UNION SELECT host, owner, repo FROM pull_requests
		) u
		LEFT JOIN cache_meta m
		       ON m.host=u.host AND m.owner=u.owner AND m.repo=u.repo
		ORDER BY u.host, u.owner, u.repo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []CachedRepo
	for rows.Next() {
		var (
			cr                          CachedRepo
			cachedAt                    sql.NullString
			duration, complete          sql.NullInt64
			issueCursor, prCursor       sql.NullString
			prPageCursor, prFloor       sql.NullString
			issuesCachedAt, prsCachedAt sql.NullString
		)
		if err := rows.Scan(&cr.Host, &cr.Owner, &cr.Repo,
			&cachedAt, &duration, &complete, &issueCursor, &prCursor,
			&prPageCursor, &prFloor, &issuesCachedAt, &prsCachedAt,
			&cr.IssueCount, &cr.PRCount); err != nil {
			return nil, err
		}
		if cachedAt.Valid {
			info := &CacheInfo{
				CachedAt: parseTime(cachedAt.String),
				Duration: int(duration.Int64),
				Complete: complete.Int64 != 0,
			}
			if prPageCursor.Valid {
				info.PRPageCursor = prPageCursor.String
			}
			if prFloor.Valid && prFloor.String != "" {
				t := parseTime(prFloor.String)
				info.PRFloor = &t
			}
			if issueCursor.Valid && issueCursor.String != "" {
				t := parseTime(issueCursor.String)
				info.IssueCursor = &t
			}
			if prCursor.Valid && prCursor.String != "" {
				t := parseTime(prCursor.String)
				info.PRCursor = &t
			}
			if issuesCachedAt.Valid && issuesCachedAt.String != "" {
				info.IssuesCachedAt = parseTime(issuesCachedAt.String)
			}
			if prsCachedAt.Valid && prsCachedAt.String != "" {
				info.PRsCachedAt = parseTime(prsCachedAt.String)
			}
			cr.Info = info
		}
		repos = append(repos, cr)
	}
	return repos, rows.Err()
}

// SaveViewerTeams replaces the host's cached team memberships in one
// transaction, so a failed save never leaves a partial team list behind.
func (s *sqliteStore) SaveViewerTeams(host string, teams []github.ViewerTeam) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := execLogged(tx, "SaveViewerTeams", `DELETE FROM viewer_teams WHERE host = ?`, host); err != nil {
		return err
	}
	for _, t := range teams {
		if _, err := execLogged(tx, "SaveViewerTeams", `INSERT INTO viewer_teams
			(host, organization, slug, name) VALUES (?,?,?,?)`,
			host, t.Organization, t.Slug, t.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadViewerTeams returns the host's cached team memberships.
func (s *sqliteStore) LoadViewerTeams(host string) ([]github.ViewerTeam, error) {
	rows, err := queryLogged(s.db, "LoadViewerTeams", `SELECT organization, slug, name FROM viewer_teams
		WHERE host = ? ORDER BY organization, slug`, host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []github.ViewerTeam
	for rows.Next() {
		var t github.ViewerTeam
		if err := rows.Scan(&t.Organization, &t.Slug, &t.Name); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// DeleteRepo removes every issue, pull request, and cache metadata row for one
// repository, leaving other repositories in the shared database untouched. The
// deletes run in a transaction so an interrupted call cannot leave a partially
// deleted repository. It reports false when nothing was cached.
func (s *sqliteStore) DeleteRepo(host, owner, repo string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	deleted := false
	for _, table := range []string{"issues", "pull_requests", "cache_meta", "coverage_segments"} {
		res, err := execLogged(tx, "DeleteRepo",
			`DELETE FROM `+table+` WHERE host = ? AND owner = ? AND repo = ?`,
			host, owner, repo)
		if err != nil {
			tx.Rollback()
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			tx.Rollback()
			return false, err
		}
		if n > 0 {
			deleted = true
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return deleted, nil
}

// SaveSegments merges new coverage into the repository's segments for one kind.
// The load, merge, delete, and insert run in one transaction so concurrent
// workers cannot lose an update.
func (s *sqliteStore) SaveSegments(host, owner, repo, kind string, segments []Segment) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, err := loadSegments(tx, "SaveSegments", host, owner, repo, kind)
	if err != nil {
		return err
	}
	merged := MergeSegments(append(existing, segments...))
	if _, err := execLogged(tx, "SaveSegments", `DELETE FROM coverage_segments WHERE host=? AND owner=? AND repo=? AND kind=?`,
		host, owner, repo, kind); err != nil {
		return err
	}
	now := tstr(time.Now())
	for _, seg := range merged {
		if _, err := execLogged(tx, "SaveSegments", `INSERT INTO coverage_segments
			(host, owner, repo, kind, start_at, end_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			host, owner, repo, kind, tstr(seg.Start), tstr(seg.End), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadSegments returns the merged coverage for one kind.
func (s *sqliteStore) LoadSegments(host, owner, repo, kind string) ([]Segment, error) {
	segments, err := loadSegments(s.db, "LoadSegments", host, owner, repo, kind)
	if err != nil {
		return nil, err
	}
	return MergeSegments(segments), nil
}

// DeleteSegments clears the coverage for one kind.
func (s *sqliteStore) DeleteSegments(host, owner, repo, kind string) error {
	_, err := execLogged(s.db, "DeleteSegments", `DELETE FROM coverage_segments WHERE host=? AND owner=? AND repo=? AND kind=?`,
		host, owner, repo, kind)
	return err
}

// loadSegments runs the coverage read on a database or transaction handle.
func loadSegments(q sqlRunner, op, host, owner, repo, kind string) ([]Segment, error) {
	rows, err := queryLogged(q, op, `SELECT start_at, end_at FROM coverage_segments
		WHERE host=? AND owner=? AND repo=? AND kind=? ORDER BY start_at`,
		host, owner, repo, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Segment
	for rows.Next() {
		var startS, endS string
		if err := rows.Scan(&startS, &endS); err != nil {
			return nil, err
		}
		out = append(out, Segment{Start: parseTime(startS), End: parseTime(endS)})
	}
	return out, rows.Err()
}

// DeleteType removes one kind's cached items and coverage in a transaction.
func (s *sqliteStore) DeleteType(host, owner, repo, kind string) (int, error) {
	table := "issues"
	if kind == KindPRs {
		table = "pull_requests"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := execLogged(tx, "DeleteType", `DELETE FROM `+table+` WHERE host=? AND owner=? AND repo=?`, host, owner, repo)
	if err != nil {
		return 0, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err := execLogged(tx, "DeleteType", `DELETE FROM coverage_segments WHERE host=? AND owner=? AND repo=? AND kind=?`,
		host, owner, repo, kind); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(deleted), nil
}

// DeleteItem removes one item's row. It reports false when the item was not
// cached, so a caller can tell a deletion from a missing entry.
func (s *sqliteStore) DeleteItem(host, owner, repo, kind string, number int) (bool, error) {
	table := "issues"
	if kind == KindPRs {
		table = "pull_requests"
	}
	res, err := execLogged(s.db, "DeleteItem", `DELETE FROM `+table+` WHERE host=? AND owner=? AND repo=? AND number=?`,
		host, owner, repo, number)
	if err != nil {
		return false, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}
