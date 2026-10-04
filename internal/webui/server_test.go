package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
)

// newTestStore creates a store backed by a temp dir and seeds it with the
// given issues and PRs.
func newTestStore(t *testing.T) cache.Store {
	t.Helper()
	return cache.NewStoreWithPath(t.TempDir())
}

func makeIssue(number int, title, state, body string, updatedAt time.Time) *github.Issue {
	return &github.Issue{
		Number:    number,
		Title:     title,
		State:     state,
		Author:    github.Actor{Login: "alice"},
		Labels:    []github.Label{{Name: "bug", Color: "d73a4a"}},
		CreatedAt: updatedAt.Add(-24 * time.Hour),
		UpdatedAt: updatedAt,
		URL:       "https://github.com/acme/testrepo/issues/" + strconv.Itoa(number),
		Body:      body,
		Comments: []github.Comment{
			{
				ID:        "c1",
				Author:    github.Actor{Login: "bob"},
				Body:      "A comment",
				CreatedAt: updatedAt,
				URL:       "https://github.com/acme/testrepo/issues/" + strconv.Itoa(number) + "#comment",
			},
		},
	}
}

func makePR(number int, title, state string, draft bool, mergedAt *time.Time, updatedAt time.Time) *github.PullRequest {
	return &github.PullRequest{
		Number:      number,
		Title:       title,
		State:       state,
		IsDraft:     draft,
		Author:      github.Actor{Login: "carol"},
		BaseRefName: "main",
		HeadRefName: "feature",
		CreatedAt:   updatedAt.Add(-24 * time.Hour),
		UpdatedAt:   updatedAt,
		MergedAt:    mergedAt,
		URL:         "https://github.com/acme/testrepo/pull/" + strconv.Itoa(number),
		Body:        "PR body",
	}
}

func seedRepo(t *testing.T, store cache.Store, host, owner, repo string, issues []*github.Issue, prs []*github.PullRequest) {
	t.Helper()
	for _, issue := range issues {
		if err := store.SaveIssue(host, owner, repo, issue); err != nil {
			t.Fatalf("SaveIssue: %v", err)
		}
	}
	for _, pr := range prs {
		if err := store.SavePR(host, owner, repo, pr); err != nil {
			t.Fatalf("SavePR: %v", err)
		}
	}
	if err := store.SaveCacheInfo(host, owner, repo, 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("response is not JSON: %v (%q)", err, rec.Body.String())
		}
	}
	return rec, body
}

func items(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("no items in response: %v", body)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		out = append(out, e.(map[string]any))
	}
	return out
}

func newTestServer(store cache.Store, refresh RefreshFunc) *Server {
	return New(store, refresh)
}

// ---------------------------------------------------------------------------
// /api/repos
// ---------------------------------------------------------------------------

func TestReposEndpoint(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	seedRepo(t, store, "github.com", "acme", "zeta", []*github.Issue{makeIssue(1, "i", "OPEN", "", now)}, nil)
	seedRepo(t, store, "github.com", "acme", "alpha", nil, []*github.PullRequest{makePR(2, "p", "OPEN", false, nil, now)})

	srv := newTestServer(store, nil)
	rec, body := get(t, srv.Handler(), "/api/repos")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	repos, ok := body["repos"].([]any)
	if !ok || len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %v", body)
	}
	first := repos[0].(map[string]any)
	// Sorted by owner then repo name: alpha comes first.
	if first["repo"] != "alpha" {
		t.Fatalf("expected alpha first, got %v", first["repo"])
	}
	if first["prCount"].(float64) != 1 || first["issueCount"].(float64) != 0 {
		t.Fatalf("unexpected counts: %v", first)
	}
	if first["complete"] != true {
		t.Fatalf("expected complete=true: %v", first)
	}
	if first["cachedAt"] == nil {
		t.Fatal("expected cachedAt to be set")
	}
}

// ---------------------------------------------------------------------------
// List filtering
// ---------------------------------------------------------------------------

func TestListIssuesFiltering(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	issues := []*github.Issue{
		makeIssue(1, "Open bug report", "OPEN", "first", now),
		makeIssue(2, "Closed feature", "CLOSED", "second", now.Add(-time.Hour)),
		makeIssue(3, "Open docs task", "OPEN", "third", now.Add(-2*time.Hour)),
	}
	seedRepo(t, store, "github.com", "acme", "testrepo", issues, nil)
	h := newTestServer(store, nil).Handler()

	// Default state=open.
	rec, body := get(t, h, "/api/repos/github.com/acme/testrepo/issues")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := len(items(t, body)); got != 2 {
		t.Fatalf("expected 2 open issues, got %d", got)
	}

	// state=all returns everything, sorted by updatedAt desc.
	_, body = get(t, h, "/api/repos/github.com/acme/testrepo/issues?state=all")
	all := items(t, body)
	if len(all) != 3 {
		t.Fatalf("expected 3 issues, got %d", len(all))
	}
	if all[0]["number"].(float64) != 1 {
		t.Fatalf("expected newest first, got %v", all[0]["number"])
	}
	if all[0]["state"] != "open" || all[0]["kind"] != "issue" || all[0]["author"] != "alice" {
		t.Fatalf("unexpected summary: %v", all[0])
	}

	// Text query.
	_, body = get(t, h, "/api/repos/github.com/acme/testrepo/issues?state=all&q=docs")
	if got := len(items(t, body)); got != 1 {
		t.Fatalf("expected 1 match for q=docs, got %d", got)
	}

	// Invalid state.
	rec, _ = get(t, h, "/api/repos/github.com/acme/testrepo/issues?state=merged")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for merged issue state, got %d", rec.Code)
	}

	// Unknown repo.
	rec, _ = get(t, h, "/api/repos/github.com/acme/missing/issues")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown repo, got %d", rec.Code)
	}
}

func TestListPRsMergedState(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	mergedAt := now.Add(-time.Hour)
	prs := []*github.PullRequest{
		makePR(1, "Open PR", "OPEN", false, nil, now),
		makePR(2, "Merged PR", "CLOSED", false, &mergedAt, now.Add(-time.Hour)),
		makePR(3, "Draft PR", "OPEN", true, nil, now.Add(-2*time.Hour)),
	}
	seedRepo(t, store, "github.com", "acme", "testrepo", nil, prs)
	h := newTestServer(store, nil).Handler()

	_, body := get(t, h, "/api/repos/github.com/acme/testrepo/prs?state=all")
	all := items(t, body)
	if len(all) != 3 {
		t.Fatalf("expected 3 PRs, got %d", len(all))
	}
	// A PR with MergedAt set but State CLOSED is reported merged.
	merged := all[1]
	if merged["state"] != "merged" || merged["number"].(float64) != 2 {
		t.Fatalf("expected PR 2 merged, got %v", merged)
	}
	if all[2]["isDraft"] != true {
		t.Fatalf("expected PR 3 draft, got %v", all[2])
	}

	_, body = get(t, h, "/api/repos/github.com/acme/testrepo/prs?state=merged")
	if got := len(items(t, body)); got != 1 {
		t.Fatalf("expected 1 merged PR, got %d", got)
	}

	// merged is invalid for issues.
	rec, _ := get(t, h, "/api/repos/github.com/acme/testrepo/issues?state=merged")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Detail
// ---------------------------------------------------------------------------

func TestDetailIssue(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	issue := makeIssue(7, "Escaped", "OPEN", "Body with **bold** and <script>alert(1)</script>", now)
	seedRepo(t, store, "github.com", "acme", "testrepo", []*github.Issue{issue}, nil)
	h := newTestServer(store, nil).Handler()

	rec, body := get(t, h, "/api/repos/github.com/acme/testrepo/issues/7")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	bodyHTML, _ := body["bodyHTML"].(string)
	if !strings.Contains(bodyHTML, "<strong>bold</strong>") {
		t.Fatalf("expected rendered bold, got %q", bodyHTML)
	}
	if strings.Contains(bodyHTML, "<script>") {
		t.Fatalf("raw script tag leaked: %q", bodyHTML)
	}

	comments, ok := body["comments"].([]any)
	if !ok || len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %v", body["comments"])
	}
	comment := comments[0].(map[string]any)
	if comment["author"] != "bob" || !strings.Contains(comment["bodyHTML"].(string), "A comment") {
		t.Fatalf("unexpected comment: %v", comment)
	}
	if body["kind"] != "issue" || body["number"].(float64) != 7 {
		t.Fatalf("unexpected detail: %v", body)
	}

	// Missing issue.
	rec, _ = get(t, h, "/api/repos/github.com/acme/testrepo/issues/99")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDetailPR(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	mergedAt := now.Add(-time.Hour)
	pr := makePR(5, "Merged PR", "CLOSED", false, &mergedAt, now)
	seedRepo(t, store, "github.com", "acme", "testrepo", nil, []*github.PullRequest{pr})
	h := newTestServer(store, nil).Handler()

	rec, body := get(t, h, "/api/repos/github.com/acme/testrepo/prs/5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body["kind"] != "pr" || body["state"] != "merged" {
		t.Fatalf("unexpected summary: %v", body)
	}
	if body["baseRefName"] != "main" || body["headRefName"] != "feature" {
		t.Fatalf("expected branch names, got %v", body)
	}
	if body["mergedAt"] == nil {
		t.Fatal("expected mergedAt to be set")
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestRefreshEndpoint(t *testing.T) {
	store := newTestStore(t)
	seedRepo(t, store, "github.com", "acme", "testrepo", nil, nil)

	var mu sync.Mutex
	calls := 0
	running := make(chan struct{})
	release := make(chan struct{})
	refresh := func(host, owner, repo string) (int, int, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		close(running) // signal first refresh in flight
		<-release      // block until the test releases it
		if err := store.SaveIssue(host, owner, repo, makeIssue(1, "fetched", "OPEN", "", time.Now())); err != nil {
			return 0, 0, err
		}
		return 1, 2, nil
	}

	h := newTestServer(store, refresh).Handler()

	doPost := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/repos/github.com/acme/testrepo/refresh", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	var first *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		first = doPost()
		close(done)
	}()

	<-running // wait until the first refresh is in flight

	// A second concurrent refresh must be rejected.
	second := doPost()
	if second.Code != http.StatusConflict {
		t.Fatalf("expected 409 for concurrent refresh, got %d", second.Code)
	}

	close(release)
	<-done
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", first.Code, first.Body.String())
	}

	// The refresh wrote into the store and its counts are returned.
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("expected 1 refresh call, got %d", gotCalls)
	}
	issue, _, err := store.LoadIssue("github.com", "acme", "testrepo", 1)
	if err != nil || issue.Title != "fetched" {
		t.Fatalf("expected fetched issue in store, got %v %v", issue, err)
	}

	// GET is not allowed.
	req := httptest.NewRequest(http.MethodGet, "/api/repos/github.com/acme/testrepo/refresh", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Search (palette)
// ---------------------------------------------------------------------------

func TestSearchScoring(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	issues := []*github.Issue{
		makeIssue(12, "Fix login flow", "OPEN", "", now),
		makeIssue(123, "Add fluzzy search", "OPEN", "", now.Add(-time.Hour)),
		makeIssue(5, "Refactor fluzzy logic", "OPEN", "", now.Add(-2*time.Hour)),
	}
	seedRepo(t, store, "github.com", "acme", "testrepo", issues, nil)
	h := newTestServer(store, nil).Handler()

	base := "/api/search?host=github.com&owner=acme&repo=testrepo"

	// Empty query returns recent items.
	_, body := get(t, h, base+"&scope=repo")
	if got := len(items(t, body)); got != 3 {
		t.Fatalf("expected 3 results for empty query, got %d", got)
	}

	// Exact number match wins over title matches.
	_, body = get(t, h, base+"&scope=repo&q=%23123")
	res := items(t, body)
	if len(res) == 0 || res[0]["number"].(float64) != 123 {
		t.Fatalf("expected #123 first, got %v", res)
	}

	// Word prefix match.
	_, body = get(t, h, base+"&scope=repo&q=log")
	res = items(t, body)
	if len(res) == 0 || res[0]["number"].(float64) != 12 {
		t.Fatalf("expected #12 first for q=log, got %v", res)
	}

	// Subsequence match.
	_, body = get(t, h, base+"&scope=repo&q=flzy")
	res = items(t, body)
	if len(res) != 2 {
		t.Fatalf("expected 2 subsequence matches for q=flzy, got %d", len(res))
	}

	// scope=all includes other repos with host/owner/repo set.
	seedRepo(t, store, "github.com", "other", "repo2", []*github.Issue{makeIssue(1, "login elsewhere", "OPEN", "", now.Add(-30*time.Minute))}, nil)
	_, body = get(t, h, base+"&scope=all&q=log")
	res = items(t, body)
	if len(res) != 3 {
		t.Fatalf("expected 3 scope=all matches, got %d", len(res))
	}
	if res[1]["owner"] != "other" || res[1]["host"] != "github.com" {
		t.Fatalf("expected repo fields on scope=all results, got %v", res[1])
	}

	// scope=repo requires the repo params.
	rec, _ := get(t, h, "/api/search?scope=repo&q=x")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing repo params, got %d", rec.Code)
	}

	// Invalid scope.
	rec, _ = get(t, h, base+"&scope=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid scope, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Palette score unit tests
// ---------------------------------------------------------------------------

func TestPaletteScore(t *testing.T) {
	cases := []struct {
		query  string
		number int
		title  string
		want   int
	}{
		{"123", 123, "anything", 3},
		{"#123", 123, "anything", 3},
		{"fix", 1, "Fix the bug", 2},
		{"FIX", 1, "fix the bug", 2},
		{"ftb", 1, "Fix The bug", 1},
		{"zzz", 1, "Fix the bug", 0},
		{"", 1, "Fix the bug", 0},
	}
	for _, tc := range cases {
		if got := paletteScore(tc.query, tc.number, tc.title); got != tc.want {
			t.Errorf("paletteScore(%q, %d, %q) = %d, want %d", tc.query, tc.number, tc.title, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Static assets
// ---------------------------------------------------------------------------

func TestStaticAssets(t *testing.T) {
	store := newTestStore(t)
	h := newTestServer(store, nil).Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/assets/app.js") {
		t.Fatal("index.html does not reference app.js")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type %q", ct)
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("app.js: code=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing asset, got %d", rec.Code)
	}

	// Unknown top-level path.
	req = httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
