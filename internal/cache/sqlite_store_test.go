package cache

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

// legacyPRsDDL is the pull_requests table as it existed before head_ref_oid was
// added, used to prove NewSQLiteStore migrates an existing database in place.
const legacyPRsDDL = `CREATE TABLE pull_requests (
    host            TEXT    NOT NULL,
    owner           TEXT    NOT NULL,
    repo            TEXT    NOT NULL,
    number          INTEGER NOT NULL,
    title           TEXT    NOT NULL,
    state           TEXT    NOT NULL,
    is_draft        INTEGER NOT NULL,
    author_login    TEXT    NOT NULL,
    assignees       TEXT    NOT NULL,
    labels          TEXT    NOT NULL,
    milestone       TEXT,
    base_ref_name   TEXT    NOT NULL,
    head_ref_name   TEXT    NOT NULL,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    merged_at       TEXT,
    closed_at       TEXT,
    url             TEXT    NOT NULL,
    body            TEXT    NOT NULL,
    comment_count   INTEGER NOT NULL,
    comments        TEXT    NOT NULL,
    review_decision TEXT,
    row_mtime       TEXT    NOT NULL,
    PRIMARY KEY (host, owner, repo, number)
)`

func utc(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func richIssue(n int) *github.Issue {
	closed := utc(2024, 3, 4, 5, 6)
	return &github.Issue{
		Number: n,
		Title:  "Fix the memory leak",
		State:  "CLOSED",
		Author: github.Actor{Login: "alice"},
		Assignees: []github.Actor{
			{Login: "bob"},
			{Login: "carol"},
		},
		Labels: []github.Label{
			{Name: "bug", Color: "d73a4a"},
			{Name: "p1", Color: "b60205"},
		},
		Milestone:    &github.Milestone{Number: 3, Title: "v1.0"},
		CreatedAt:    utc(2024, 1, 2, 3, 4),
		UpdatedAt:    utc(2024, 2, 3, 4, 5),
		ClosedAt:     &closed,
		URL:          "https://github.com/owner/repo/issues/1",
		Body:         "Issue body text.",
		CommentCount: 2,
		Comments: []github.Comment{
			{ID: "c1", Author: github.Actor{Login: "bob"}, Body: "first", CreatedAt: utc(2024, 1, 3, 0, 0), UpdatedAt: utc(2024, 1, 3, 0, 0), URL: "https://x/1"},
			{ID: "c2", Author: github.Actor{Login: "carol"}, Body: "second", CreatedAt: utc(2024, 1, 4, 0, 0), UpdatedAt: utc(2024, 1, 4, 0, 0), URL: "https://x/2"},
		},
	}
}

func richOpenIssue(n int) *github.Issue {
	return &github.Issue{
		Number:    n,
		Title:     "Add dark mode",
		State:     "OPEN",
		Author:    github.Actor{Login: "dave"},
		CreatedAt: utc(2024, 5, 6, 7, 8),
		UpdatedAt: utc(2024, 5, 7, 7, 8),
		URL:       "https://github.com/owner/repo/issues/2",
		Body:      "Please add dark mode.",
	}
}

func richPR(n int) *github.PullRequest {
	merged := utc(2024, 4, 5, 6, 7)
	return &github.PullRequest{
		Number:         n,
		Title:          "Add login feature",
		State:          "MERGED",
		IsDraft:        false,
		Author:         github.Actor{Login: "alice"},
		Assignees:      []github.Actor{{Login: "bob"}},
		Labels:         []github.Label{{Name: "feature", Color: "0e8a16"}},
		Milestone:      &github.Milestone{Number: 4, Title: "v2.0"},
		BaseRefName:    "main",
		HeadRefName:    "feat/login",
		HeadRefOid:     "0123456789abcdef0123456789abcdef01234567",
		CreatedAt:      utc(2024, 3, 1, 2, 3),
		UpdatedAt:      utc(2024, 3, 2, 2, 3),
		MergedAt:       &merged,
		URL:            "https://github.com/owner/repo/pull/1",
		Body:           "PR body text.",
		CommentCount:   1,
		Comments:       []github.Comment{{ID: "pc1", Author: github.Actor{Login: "bob"}, Body: "lgtm", CreatedAt: utc(2024, 3, 2, 0, 0), UpdatedAt: utc(2024, 3, 2, 0, 0)}},
		ReviewDecision: "APPROVED",
	}
}

func richDraftPR(n int) *github.PullRequest {
	return &github.PullRequest{
		Number:      n,
		Title:       "WIP: refactor auth",
		State:       "OPEN",
		IsDraft:     true,
		Author:      github.Actor{Login: "erin"},
		BaseRefName: "main",
		HeadRefName: "wip/auth",
		CreatedAt:   utc(2024, 6, 1, 0, 0),
		UpdatedAt:   utc(2024, 6, 2, 0, 0),
		URL:         "https://github.com/owner/repo/pull/3",
		Body:        "WIP",
	}
}

// TestSQLiteRoundTripMatchesFile saves a rich set of values into both backends
// and asserts the SQLite load is byte-for-byte identical to the file load
// (NFR-03), including nullable times, milestone, comments, labels, and
// assignees.
func TestSQLiteRoundTripMatchesFile(t *testing.T) {
	file := NewStoreWithPath(t.TempDir())
	sq, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sq.Close()

	const host, owner, repo = "github.com", "owner", "repo"
	issues := []*github.Issue{richIssue(1), richOpenIssue(2)}
	prs := []*github.PullRequest{richPR(10), richDraftPR(12)}

	for _, backends := range []Store{file, sq} {
		for _, is := range issues {
			if err := backends.SaveIssue(host, owner, repo, is); err != nil {
				t.Fatalf("SaveIssue(%d): %v", is.Number, err)
			}
		}
		for _, pr := range prs {
			if err := backends.SavePR(host, owner, repo, pr); err != nil {
				t.Fatalf("SavePR(%d): %v", pr.Number, err)
			}
		}
	}

	// Individual loads match.
	for _, is := range issues {
		gotFile, _, err := file.LoadIssue(host, owner, repo, is.Number)
		if err != nil {
			t.Fatalf("file LoadIssue(%d): %v", is.Number, err)
		}
		gotSq, _, err := sq.LoadIssue(host, owner, repo, is.Number)
		if err != nil {
			t.Fatalf("sqlite LoadIssue(%d): %v", is.Number, err)
		}
		if !reflect.DeepEqual(gotFile, gotSq) {
			t.Errorf("issue #%d differs\nfile:   %+v\nsqlite: %+v", is.Number, gotFile, gotSq)
		}
	}
	for _, pr := range prs {
		gotFile, _, err := file.LoadPR(host, owner, repo, pr.Number)
		if err != nil {
			t.Fatalf("file LoadPR(%d): %v", pr.Number, err)
		}
		gotSq, _, err := sq.LoadPR(host, owner, repo, pr.Number)
		if err != nil {
			t.Fatalf("sqlite LoadPR(%d): %v", pr.Number, err)
		}
		if !reflect.DeepEqual(gotFile, gotSq) {
			t.Errorf("PR #%d differs\nfile:   %+v\nsqlite: %+v", pr.Number, gotFile, gotSq)
		}
	}

	// Bulk loads return the same set (order normalization: key by number).
	compareIssues(t, "LoadAllIssues", mustLoadAllIssues(t, file, host, owner, repo), mustLoadAllIssues(t, sq, host, owner, repo))
	comparePRs(t, "LoadAllPRs", mustLoadAllPRs(t, file, host, owner, repo), mustLoadAllPRs(t, sq, host, owner, repo))

	// Cache metadata round-trips.
	cursor := utc(2024, 7, 8, 9, 10)
	info := &CacheInfo{CachedAt: utc(2024, 7, 8, 9, 10), Duration: 60, Complete: true, IssueCursor: &cursor}
	if err := file.SaveCacheInfoFull(host, owner, repo, info); err != nil {
		t.Fatalf("file SaveCacheInfoFull: %v", err)
	}
	if err := sq.SaveCacheInfoFull(host, owner, repo, info); err != nil {
		t.Fatalf("sqlite SaveCacheInfoFull: %v", err)
	}
	gotInfoFile, err := file.LoadCacheInfo(host, owner, repo)
	if err != nil {
		t.Fatalf("file LoadCacheInfo: %v", err)
	}
	gotInfoSq, err := sq.LoadCacheInfo(host, owner, repo)
	if err != nil {
		t.Fatalf("sqlite LoadCacheInfo: %v", err)
	}
	if !reflect.DeepEqual(gotInfoFile, gotInfoSq) {
		t.Errorf("cache info differs\nfile:   %+v\nsqlite: %+v", gotInfoFile, gotInfoSq)
	}
}

func mustLoadAllIssues(t *testing.T, s Store, host, owner, repo string) map[int]*github.Issue {
	t.Helper()
	issues, err := s.LoadAllIssues(host, owner, repo)
	if err != nil {
		t.Fatalf("LoadAllIssues: %v", err)
	}
	out := map[int]*github.Issue{}
	for _, is := range issues {
		out[is.Number] = is
	}
	return out
}

func mustLoadAllPRs(t *testing.T, s Store, host, owner, repo string) map[int]*github.PullRequest {
	t.Helper()
	prs, err := s.LoadAllPRs(host, owner, repo)
	if err != nil {
		t.Fatalf("LoadAllPRs: %v", err)
	}
	out := map[int]*github.PullRequest{}
	for _, pr := range prs {
		out[pr.Number] = pr
	}
	return out
}

func compareIssues(t *testing.T, label string, want, got map[int]*github.Issue) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: size mismatch want %d got %d", label, len(want), len(got))
	}
	for n, w := range want {
		g, ok := got[n]
		if !ok {
			t.Errorf("%s: missing #%d", label, n)
			continue
		}
		if !reflect.DeepEqual(w, g) {
			t.Errorf("%s: #%d differs\nwant: %+v\ngot:  %+v", label, n, w, g)
		}
	}
}

func comparePRs(t *testing.T, label string, want, got map[int]*github.PullRequest) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: size mismatch want %d got %d", label, len(want), len(got))
	}
	for n, w := range want {
		g, ok := got[n]
		if !ok {
			t.Errorf("%s: missing #%d", label, n)
			continue
		}
		if !reflect.DeepEqual(w, g) {
			t.Errorf("%s: #%d differs\nwant: %+v\ngot:  %+v", label, n, w, g)
		}
	}
}

// TestSQLitePersistsAcrossReopen asserts data survives closing and reopening
// the database (single-file portability).
func TestSQLitePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	const host, owner, repo = "github.com", "owner", "repo"

	sq, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := sq.SaveIssue(host, owner, repo, richIssue(1)); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := sq.SavePR(host, owner, repo, richPR(10)); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
	if err := sq.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer reopened.Close()

	issues, err := reopened.LoadAllIssues(host, owner, repo)
	if err != nil || len(issues) != 1 {
		t.Fatalf("LoadAllIssues after reopen: n=%d err=%v", len(issues), err)
	}
	prs, err := reopened.LoadAllPRs(host, owner, repo)
	if err != nil || len(prs) != 1 {
		t.Fatalf("LoadAllPRs after reopen: n=%d err=%v", len(prs), err)
	}
}

// TestSQLiteMigratesHeadRefOid opens a database created before the column
// existed and verifies the migration adds it and that values round-trip.
func TestSQLiteMigratesHeadRefOid(t *testing.T) {
	dir := t.TempDir()

	legacy, err := sql.Open("sqlite", filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := legacy.Exec(legacyPRsDDL); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	sq, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore (migrate): %v", err)
	}
	defer sq.Close()

	const host, owner, repo = "github.com", "owner", "repo"
	pr := richPR(10)
	if err := sq.SavePR(host, owner, repo, pr); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
	got, _, err := sq.LoadPR(host, owner, repo, pr.Number)
	if err != nil {
		t.Fatalf("LoadPR: %v", err)
	}
	if got.HeadRefOid != pr.HeadRefOid {
		t.Errorf("HeadRefOid = %q, want %q", got.HeadRefOid, pr.HeadRefOid)
	}
}

// TestSQLiteListCachedRepos covers the repo-listing path (which previously
// deadlocked by issuing a metadata query while the result set was open).
func TestSQLiteListCachedRepos(t *testing.T) {
	sq, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sq.Close()

	// Repo A: issues + PRs + cache metadata.
	if err := sq.SaveIssue(qHost, "acme", "alpha", richIssue(1)); err != nil {
		t.Fatalf("SaveIssue alpha: %v", err)
	}
	if err := sq.SaveIssue(qHost, "acme", "alpha", richOpenIssue(2)); err != nil {
		t.Fatalf("SaveIssue alpha: %v", err)
	}
	if err := sq.SavePR(qHost, "acme", "alpha", richPR(10)); err != nil {
		t.Fatalf("SavePR alpha: %v", err)
	}
	if err := sq.SaveCacheInfo(qHost, "acme", "alpha", 60); err != nil {
		t.Fatalf("SaveCacheInfo alpha: %v", err)
	}
	// Repo B: rows only, no cache metadata.
	if err := sq.SaveIssue(qHost, "acme", "beta", richIssue(3)); err != nil {
		t.Fatalf("SaveIssue beta: %v", err)
	}

	repos, err := sq.ListCachedRepos()
	if err != nil {
		t.Fatalf("ListCachedRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	byRepo := map[string]CachedRepo{}
	for _, r := range repos {
		byRepo[r.Repo] = r
	}
	alpha, ok := byRepo["alpha"]
	if !ok {
		t.Fatal("alpha not listed")
	}
	if alpha.IssueCount != 2 || alpha.PRCount != 1 {
		t.Errorf("alpha counts = %d issues / %d PRs, want 2 / 1", alpha.IssueCount, alpha.PRCount)
	}
	if alpha.Info == nil || !alpha.Info.Complete {
		t.Errorf("alpha info = %+v, want complete", alpha.Info)
	}
	beta, ok := byRepo["beta"]
	if !ok {
		t.Fatal("beta not listed")
	}
	if beta.IssueCount != 1 || beta.PRCount != 0 {
		t.Errorf("beta counts = %d issues / %d PRs, want 1 / 0", beta.IssueCount, beta.PRCount)
	}
	if beta.Info != nil {
		t.Errorf("beta info = %+v, want nil (no cache metadata)", beta.Info)
	}
}
