package cache

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	_ "modernc.org/sqlite"

	"github.com/tomzxcode/ghx/internal/github"
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

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("applying %q: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting schema version: %w", err)
	}
	return &sqliteStore{db: db, path: path}, nil
}

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

// ---------------------------------------------------------------------------
// Issues
// ---------------------------------------------------------------------------

const issueCols = `number, title, state, author_login, assignees, labels, milestone,
	created_at, updated_at, closed_at, url, body, comment_count, comments, row_mtime`

// SaveIssue writes a single issue to the database.
func (s *sqliteStore) SaveIssue(host, owner, repo string, issue *github.Issue) error {
	milestone, err := nullJSON(issue.Milestone)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO issues
		(host, owner, repo, number, title, state, author_login, assignees, labels, milestone,
		 created_at, updated_at, closed_at, url, body, comment_count, comments, row_mtime)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		host, owner, repo, issue.Number, issue.Title, issue.State, issue.Author.Login,
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
		number                             int
		title, state, authorLogin          string
		assigneesJSON, labelsJSON          string
		milestoneJSON                      sql.NullString
		createdAtS, updatedAtS             string
		closedAtS                          sql.NullString
		url, body, commentsJSON, rowMtimeS string
		commentCount                       int
	)
	if err := row.Scan(&number, &title, &state, &authorLogin, &assigneesJSON, &labelsJSON, &milestoneJSON,
		&createdAtS, &updatedAtS, &closedAtS, &url, &body, &commentCount, &commentsJSON, &rowMtimeS); err != nil {
		return nil, time.Time{}, err
	}

	issue := &github.Issue{
		Number:       number,
		Title:        title,
		State:        state,
		Author:       github.Actor{Login: authorLogin},
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
	row := s.db.QueryRow(`SELECT `+issueCols+` FROM issues WHERE host=? AND owner=? AND repo=? AND number=?`,
		host, owner, repo, number)
	return scanIssue(row)
}

// LoadAllIssues reads every cached issue for a repository.
func (s *sqliteStore) LoadAllIssues(host, owner, repo string) ([]*github.Issue, error) {
	rows, err := s.db.Query(`SELECT `+issueCols+` FROM issues WHERE host=? AND owner=? AND repo=? ORDER BY number`,
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

// ---------------------------------------------------------------------------
// Pull requests
// ---------------------------------------------------------------------------

const prCols = `number, title, state, is_draft, author_login, assignees, labels, milestone,
	base_ref_name, head_ref_name, created_at, updated_at, merged_at, closed_at,
	url, body, comment_count, comments, review_decision, row_mtime`

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
	_, err = s.db.Exec(`INSERT OR REPLACE INTO pull_requests
		(host, owner, repo, number, title, state, is_draft, author_login, assignees, labels, milestone,
		 base_ref_name, head_ref_name, created_at, updated_at, merged_at, closed_at,
		 url, body, comment_count, comments, review_decision, row_mtime)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		host, owner, repo, pr.Number, pr.Title, pr.State, isDraft, pr.Author.Login,
		mustJSON(pr.Assignees), mustJSON(pr.Labels), milestone,
		pr.BaseRefName, pr.HeadRefName, tstr(pr.CreatedAt), tstr(pr.UpdatedAt),
		nullTime(pr.MergedAt), nullTime(pr.ClosedAt),
		pr.URL, pr.Body, pr.CommentCount, mustJSON(pr.Comments), reviewDecision, tstr(time.Now()),
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
		isDraft                   bool
		assigneesJSON, labelsJSON string
		milestoneJSON             sql.NullString
		baseRef, headRef          string
		createdAtS, updatedAtS    string
		mergedAtS, closedAtS      sql.NullString
		url, body, commentsJSON   string
		commentCount              int
		reviewDecision            sql.NullString
		rowMtimeS                 string
	)
	if err := row.Scan(&number, &title, &state, &isDraft, &authorLogin, &assigneesJSON, &labelsJSON, &milestoneJSON,
		&baseRef, &headRef, &createdAtS, &updatedAtS, &mergedAtS, &closedAtS,
		&url, &body, &commentCount, &commentsJSON, &reviewDecision, &rowMtimeS); err != nil {
		return nil, time.Time{}, err
	}

	pr := &github.PullRequest{
		Number:       number,
		Title:        title,
		State:        state,
		IsDraft:      isDraft,
		Author:       github.Actor{Login: authorLogin},
		BaseRefName:  baseRef,
		HeadRefName:  headRef,
		CreatedAt:    parseTime(createdAtS),
		UpdatedAt:    parseTime(updatedAtS),
		URL:          url,
		Body:         body,
		CommentCount: commentCount,
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
	row := s.db.QueryRow(`SELECT `+prCols+` FROM pull_requests WHERE host=? AND owner=? AND repo=? AND number=?`,
		host, owner, repo, number)
	return scanPR(row)
}

// LoadAllPRs reads every cached pull request for a repository.
func (s *sqliteStore) LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error) {
	rows, err := s.db.Query(`SELECT `+prCols+` FROM pull_requests WHERE host=? AND owner=? AND repo=? ORDER BY number`,
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

// ---------------------------------------------------------------------------
// Cache metadata
// ---------------------------------------------------------------------------

// SaveCacheInfo marks the cache complete at the current time with the given duration.
func (s *sqliteStore) SaveCacheInfo(host, owner, repo string, duration int) error {
	return s.SaveCacheInfoFull(host, owner, repo, &CacheInfo{
		CachedAt: time.Now(),
		Duration: duration,
		Complete: true,
	})
}

// SaveCacheInfoFull upserts the cache metadata row.
func (s *sqliteStore) SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error {
	complete := 0
	if info.Complete {
		complete = 1
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO cache_meta
		(host, owner, repo, cached_at, duration, complete, issue_cursor, pr_cursor)
		VALUES (?,?,?,?,?,?,?,?)`,
		host, owner, repo, tstr(info.CachedAt), info.Duration, complete,
		nullTime(info.IssueCursor), nullTime(info.PRCursor),
	)
	if err != nil {
		return fmt.Errorf("saving cache info: %w", err)
	}
	return nil
}

// LoadCacheInfo reads the cache metadata row.
func (s *sqliteStore) LoadCacheInfo(host, owner, repo string) (*CacheInfo, error) {
	var (
		cachedAtS               string
		duration, complete      int
		issueCursorS, prCursorS sql.NullString
	)
	row := s.db.QueryRow(`SELECT cached_at, duration, complete, issue_cursor, pr_cursor
		FROM cache_meta WHERE host=? AND owner=? AND repo=?`, host, owner, repo)
	if err := row.Scan(&cachedAtS, &duration, &complete, &issueCursorS, &prCursorS); err != nil {
		return nil, err
	}
	info := &CacheInfo{
		CachedAt: parseTime(cachedAtS),
		Duration: duration,
		Complete: complete != 0,
	}
	if issueCursorS.Valid && issueCursorS.String != "" {
		t := parseTime(issueCursorS.String)
		info.IssueCursor = &t
	}
	if prCursorS.Valid && prCursorS.String != "" {
		t := parseTime(prCursorS.String)
		info.PRCursor = &t
	}
	return info, nil
}

// IsCacheFresh reports whether the cache is complete and was populated within
// its stored duration.
func (s *sqliteStore) IsCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	return time.Since(info.CachedAt) < time.Duration(info.Duration)*time.Minute, nil
}

// IsCacheFreshWithDuration reports whether the cache is complete and was
// populated within the given duration.
func (s *sqliteStore) IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	return time.Since(info.CachedAt) < time.Duration(duration)*time.Minute, nil
}

// ListCachedRepos returns every repository present in the database.
func (s *sqliteStore) ListCachedRepos() ([]CachedRepo, error) {
	rows, err := s.db.Query(`
		SELECT u.host, u.owner, u.repo,
		       (SELECT COUNT(*) FROM issues i WHERE i.host=u.host AND i.owner=u.owner AND i.repo=u.repo),
		       (SELECT COUNT(*) FROM pull_requests p WHERE p.host=u.host AND p.owner=u.owner AND p.repo=u.repo)
		FROM (
			SELECT host, owner, repo FROM cache_meta
			UNION SELECT host, owner, repo FROM issues
			UNION SELECT host, owner, repo FROM pull_requests
		) u
		ORDER BY u.host, u.owner, u.repo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []CachedRepo
	for rows.Next() {
		var cr CachedRepo
		if err := rows.Scan(&cr.Host, &cr.Owner, &cr.Repo, &cr.IssueCount, &cr.PRCount); err != nil {
			return nil, err
		}
		cr.Info, _ = s.LoadCacheInfo(cr.Host, cr.Owner, cr.Repo)
		repos = append(repos, cr)
	}
	return repos, rows.Err()
}
