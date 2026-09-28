package cache

import (
	"reflect"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

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
