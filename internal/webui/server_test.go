package webui

import (
	"encoding/json"
	"errors"
	"fmt"
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
	assertRecentCachedAt(t, body)

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
	assertRecentCachedAt(t, body)
}

func TestDetailPRReviewers(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	pr := makePR(5, "Reviewed PR", "OPEN", false, nil, now)
	pr.Reviews = []github.PRReview{
		{Author: github.Actor{Login: "bob"}, State: "APPROVED", SubmittedAt: now.Add(-4 * time.Hour)},
		// A later comment review must not hide bob's approval.
		{Author: github.Actor{Login: "bob"}, State: "COMMENTED", SubmittedAt: now.Add(-3 * time.Hour)},
		{Author: github.Actor{Login: "dave"}, State: "CHANGES_REQUESTED", SubmittedAt: now.Add(-3 * time.Hour)},
		{Author: github.Actor{Login: "frank"}, State: "APPROVED", SubmittedAt: now.Add(-3 * time.Hour)},
		{Author: github.Actor{Login: "grace"}, State: "COMMENTED", SubmittedAt: now.Add(-3 * time.Hour)},
		// The pull request author is never a reviewer of their own PR.
		{Author: github.Actor{Login: "carol"}, State: "COMMENTED", SubmittedAt: now.Add(-3 * time.Hour)},
	}
	// Current review requests are authoritative: erin never reviewed, frank was
	// re-requested after approving, and ml-infra-gpu is a team request.
	pr.ReviewRequests = []github.ReviewRequest{
		{Type: "User", Login: "erin"},
		{Type: "Team", Name: "ml-infra-gpu"},
		{Type: "User", Login: "frank"},
		{Type: "User", Login: "carol"},
	}
	// A stale timeline request for someone no longer requested must not appear.
	pr.Timeline = []github.TimelineEvent{
		{Kind: github.TimelineReviewRequested, RequestedReviewer: github.Actor{Login: "heidi"}, CreatedAt: now.Add(-5 * time.Hour)},
	}
	seedRepo(t, store, "github.com", "acme", "testrepo", nil, []*github.PullRequest{pr})
	h := newTestServer(store, nil).Handler()

	rec, body := get(t, h, "/api/repos/github.com/acme/testrepo/prs/5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	raw, ok := body["reviewers"].([]any)
	if !ok {
		t.Fatalf("expected reviewers in detail, got %v", body["reviewers"])
	}
	got := map[string]string{}
	for _, e := range raw {
		r := e.(map[string]any)
		name, _ := r["login"].(string)
		if name == "" {
			name, _ = r["name"].(string)
		}
		got[name] = r["state"].(string)
	}
	want := map[string]string{
		"bob":          "approved",
		"dave":         "changes_requested",
		"erin":         "pending",
		"frank":        "pending",
		"grace":        "commented",
		"ml-infra-gpu": "pending",
	}
	if len(got) != len(want) {
		t.Fatalf("reviewers = %v, want %v", got, want)
	}
	for login, state := range want {
		if got[login] != state {
			t.Errorf("reviewer %s state = %q, want %q", login, got[login], state)
		}
	}
}

// TestDetailPRReviewersLegacyTimeline covers cached PRs that predate the
// reviewRequests field: reviewers come from the timeline's user-only requests,
// and the pull request author is still excluded.
func TestDetailPRReviewersLegacyTimeline(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	pr := makePR(6, "Legacy reviewed PR", "OPEN", false, nil, now)
	pr.Reviews = []github.PRReview{
		{Author: github.Actor{Login: "bob"}, State: "APPROVED", SubmittedAt: now.Add(-2 * time.Hour)},
		{Author: github.Actor{Login: "carol"}, State: "COMMENTED", SubmittedAt: now.Add(-2 * time.Hour)},
	}
	pr.Timeline = []github.TimelineEvent{
		{Kind: github.TimelineReviewRequested, RequestedReviewer: github.Actor{Login: "bob"}, CreatedAt: now.Add(-3 * time.Hour)},
		{Kind: github.TimelineReviewRequested, RequestedReviewer: github.Actor{Login: "erin"}, CreatedAt: now.Add(-3 * time.Hour)},
		{Kind: github.TimelineReviewRequested, RequestedReviewer: github.Actor{Login: "carol"}, CreatedAt: now.Add(-3 * time.Hour)},
	}
	seedRepo(t, store, "github.com", "acme", "testrepo", nil, []*github.PullRequest{pr})
	h := newTestServer(store, nil).Handler()

	rec, body := get(t, h, "/api/repos/github.com/acme/testrepo/prs/6")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	raw, ok := body["reviewers"].([]any)
	if !ok {
		t.Fatalf("expected reviewers in detail, got %v", body["reviewers"])
	}
	got := map[string]string{}
	for _, e := range raw {
		r := e.(map[string]any)
		got[r["login"].(string)] = r["state"].(string)
	}
	want := map[string]string{"bob": "approved", "erin": "pending"}
	if len(got) != len(want) {
		t.Fatalf("reviewers = %v, want %v", got, want)
	}
	for login, state := range want {
		if got[login] != state {
			t.Errorf("reviewer %s state = %q, want %q", login, got[login], state)
		}
	}
}

// assertRecentCachedAt checks that a detail response reports when its cache row
// was written, and that the timestamp is recent.
func assertRecentCachedAt(t *testing.T, body map[string]any) {
	t.Helper()
	raw, ok := body["cachedAt"].(string)
	if !ok || raw == "" {
		t.Fatalf("expected cachedAt in detail, got %v", body["cachedAt"])
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("cachedAt is not RFC3339Nano: %v", err)
	}
	if time.Since(ts) > time.Minute {
		t.Fatalf("expected a recent cachedAt, got %s", ts)
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

// TestRefreshRepoCachedOnlyByView covers a repository whose items were written
// by the read-through caching of `issue view`/`pr view` and therefore has no
// cache metadata. The UI lists it, so it must be readable and refreshable, and
// the refresh starts a cold fetch.
func TestRefreshRepoCachedOnlyByView(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	if err := store.SaveIssue("github.com", "acme", "viewonly", makeIssue(1, "viewed", "OPEN", "", now)); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SavePR("github.com", "acme", "viewonly", makePR(2, "viewed", "OPEN", false, nil, now)); err != nil {
		t.Fatalf("SavePR: %v", err)
	}

	var (
		mu        sync.Mutex
		refreshed bool
	)
	refresh := func(host, owner, repo string) (int, int, error) {
		mu.Lock()
		refreshed = true
		mu.Unlock()
		return 1, 1, nil
	}
	h := newTestServer(store, refresh).Handler()

	rec, body := get(t, h, "/api/repos/github.com/acme/viewonly/issues?state=all")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := len(items(t, body)); got != 1 {
		t.Fatalf("expected 1 issue, got %d", got)
	}

	rec, _ = get(t, h, "/api/repos/github.com/acme/viewonly/prs/2")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/repos/github.com/acme/viewonly/refresh", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d: %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	called := refreshed
	mu.Unlock()
	if !called {
		t.Fatal("expected the refresh callback to run")
	}

	// A repository with no items and no metadata is still unknown.
	req = httptest.NewRequest(http.MethodPost, "/api/repos/github.com/acme/missing/refresh", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown repo, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Item refresh
// ---------------------------------------------------------------------------

func TestItemRefreshEndpoint(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	seedRepo(t, store, "github.com", "acme", "testrepo",
		[]*github.Issue{makeIssue(1, "stale", "OPEN", "", now)},
		[]*github.PullRequest{makePR(2, "stale", "OPEN", false, nil, now)})

	var (
		mu    sync.Mutex
		calls []string
	)
	running := make(chan struct{})
	release := make(chan struct{})

	srv := newTestServer(store, nil)
	srv.SetItemRefreshFunc(func(host, owner, repo, kind string, number int) error {
		mu.Lock()
		calls = append(calls, fmt.Sprintf("%s/%s/%s/%s/%d", host, owner, repo, kind, number))
		first := len(calls) == 1
		mu.Unlock()
		if first {
			close(running)
			<-release
		}
		if kind == "pr" {
			return store.SavePR(host, owner, repo, makePR(number, "fetched", "OPEN", false, nil, time.Now()))
		}
		return store.SaveIssue(host, owner, repo, makeIssue(number, "fetched", "OPEN", "", time.Now()))
	})
	h := srv.Handler()

	doPost := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	var first *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		first = doPost("/api/repos/github.com/acme/testrepo/issues/1/refresh")
		close(done)
	}()
	<-running

	// A concurrent refresh of the same item is rejected.
	if got := doPost("/api/repos/github.com/acme/testrepo/issues/1/refresh"); got.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a concurrent item refresh, got %d", got.Code)
	}
	// A different item is a different key, so it runs while the first is in flight.
	if got := doPost("/api/repos/github.com/acme/testrepo/prs/2/refresh"); got.Code != http.StatusOK {
		t.Fatalf("expected 200 for a different item, got %d: %s", got.Code, got.Body.String())
	}

	close(release)
	<-done
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", first.Code, first.Body.String())
	}

	// The callback rewrote both cached records.
	issue, _, err := store.LoadIssue("github.com", "acme", "testrepo", 1)
	if err != nil || issue.Title != "fetched" {
		t.Fatalf("expected refreshed issue in store, got %v %v", issue, err)
	}
	pr, _, err := store.LoadPR("github.com", "acme", "testrepo", 2)
	if err != nil || pr.Title != "fetched" {
		t.Fatalf("expected refreshed pull request in store, got %v %v", pr, err)
	}

	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	want := []string{
		"github.com/acme/testrepo/issue/1",
		"github.com/acme/testrepo/pr/2",
	}
	if fmt.Sprint(gotCalls) != fmt.Sprint(want) {
		t.Fatalf("refresh calls = %v, want %v", gotCalls, want)
	}

	// GET is not allowed.
	req := httptest.NewRequest(http.MethodGet, "/api/repos/github.com/acme/testrepo/issues/1/refresh", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	// An item that is not cached cannot be refreshed.
	if got := doPost("/api/repos/github.com/acme/testrepo/issues/99/refresh"); got.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a missing item, got %d", got.Code)
	}

	// An unknown sub-resource is not the refresh endpoint.
	if got := doPost("/api/repos/github.com/acme/testrepo/issues/1/bogus"); got.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown sub-resource, got %d", got.Code)
	}

	// Without a callback the endpoint reports that refreshing is unavailable.
	plain := newTestServer(store, nil).Handler()
	req = httptest.NewRequest(http.MethodPost, "/api/repos/github.com/acme/testrepo/issues/1/refresh", nil)
	rec = httptest.NewRecorder()
	plain.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 without a callback, got %d", rec.Code)
	}

	// A repository that is not cached is unknown.
	if got := doPost("/api/repos/github.com/acme/missing/issues/1/refresh"); got.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown repository, got %d", got.Code)
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

// ---------------------------------------------------------------------------
// /stats
// ---------------------------------------------------------------------------

func TestStatsEndpoint(t *testing.T) {
	var got StatsQuery
	srv := newTestServer(newTestStore(t), nil)
	srv.SetStatsFunc(func(q StatsQuery) (string, error) {
		got = q
		return "<html>report</html>", nil
	})
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet,
		"/stats?repo=github.com/acme/one&repo=github.com/acme/two&from=2026-01-01&to=2026-01-31"+
			"&state=merged&author=alice&author=bob&reviewer=carol&include-bots=true&list-prs=1&top=3", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", ct)
	}
	if body := rec.Body.String(); body != "<html>report</html>" {
		t.Fatalf("body = %q", body)
	}
	want := StatsQuery{
		Repos:       []string{"github.com/acme/one", "github.com/acme/two"},
		From:        "2026-01-01",
		To:          "2026-01-31",
		State:       "merged",
		Authors:     []string{"alice", "bob"},
		Reviewers:   []string{"carol"},
		IncludeBots: true,
		ListPRs:     true,
		Top:         3,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("query = %+v, want %+v", got, want)
	}

	// Defaults: state=all and top=5, no repos or filters.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/stats", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("default status = %d", rec.Code)
	}
	if got.State != "all" || got.Top != 5 || len(got.Repos) != 0 {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestStatsEndpoint_ValidationAndAvailability(t *testing.T) {
	srv := newTestServer(newTestStore(t), nil)
	srv.SetStatsFunc(func(StatsQuery) (string, error) { return "<html></html>", nil })
	h := srv.Handler()

	// Invalid state.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats?state=bogus", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid state: got %d, want 400", rec.Code)
	}

	// Invalid top.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats?top=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid top: got %d, want 400", rec.Code)
	}

	// Renderer failure surfaces as a 500 with the JSON error shape.
	srv.SetStatsFunc(func(StatsQuery) (string, error) { return "", errors.New("boom") })
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("renderer failure: got %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("error body = %q", rec.Body.String())
	}

	// Without a renderer the endpoint reports unavailability.
	srv.SetStatsFunc(nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("no renderer: got %d, want 501", rec.Code)
	}

	// POST is not allowed.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/stats", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: got %d, want 405", rec.Code)
	}
}
