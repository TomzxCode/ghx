package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

func newMockServer(t *testing.T) (*Server, *mockserver.Server, cache.Store, *gitremote.Repo) {
	t.Helper()
	scenario := mockserver.NewScenarioBuilder("acme", "testrepo").
		AddIssue("Mock issue", "Issue **body**", 24*time.Hour).
		AddPR("Mock PR", "PR body", "feature", 24*time.Hour).
		Build()
	mock := mockserver.NewServer(scenario)
	t.Cleanup(mock.Close)

	store := cache.NewStoreWithPath(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	repo := &gitremote.Repo{Host: "mock", Owner: "acme", Name: "testrepo"}

	srv := NewServer(Config{
		Store: store,
		ClientFactory: func(host string) (*github.Client, error) {
			return github.NewClientWithURL(mock.URL(), "test-token", host)
		},
		DefaultRepo: func() (*gitremote.Repo, error) { return repo, nil },
		Version:     "test",
	})
	return srv, mock, store, repo
}

func call(t *testing.T, srv *Server, method string, params any) (json.RawMessage, *Error) {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		raw = data
	}
	req, err := json.Marshal(requestForTest{JSONRPC: "2.0", ID: 1, Method: method, Params: raw})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := postRPC(t, srv, string(req))
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	data, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return data, nil
}

type requestForTest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func TestIssueListAndViewFromAPI(t *testing.T) {
	srv, _, store, repo := newMockServer(t)

	raw, rpcErr := call(t, srv, "issue.list", IssueListParams{State: "all"})
	if rpcErr != nil {
		t.Fatalf("issue.list: %+v", rpcErr)
	}
	var list IssueListResult
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Title != "Mock issue" {
		t.Fatalf("unexpected items: %+v", list)
	}
	if list.Source != "api" {
		t.Fatalf("source = %q", list.Source)
	}

	raw, rpcErr = call(t, srv, "issue.view", IssueViewParams{Number: 1})
	if rpcErr != nil {
		t.Fatalf("issue.view: %+v", rpcErr)
	}
	var issue github.Issue
	if err := json.Unmarshal(raw, &issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	if issue.Number != 1 || issue.Title != "Mock issue" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	if _, _, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, 1); err != nil {
		t.Fatalf("issue was not cached: %v", err)
	}
}

func TestIssueListFromCache(t *testing.T) {
	srv, _, store, repo := newMockServer(t)
	now := time.Now()
	if err := store.SaveIssue(repo.Host, repo.Owner, repo.Name, &github.Issue{
		Number:    7,
		Title:     "Cached issue",
		State:     "OPEN",
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SaveCacheInfo(repo.Host, repo.Owner, repo.Name, 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}

	raw, rpcErr := call(t, srv, "issue.list", IssueListParams{})
	if rpcErr != nil {
		t.Fatalf("issue.list: %+v", rpcErr)
	}
	var list IssueListResult
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Source != "cache" {
		t.Fatalf("source = %q", list.Source)
	}
	if len(list.Items) != 1 || list.Items[0].Number != 7 {
		t.Fatalf("unexpected items: %+v", list.Items)
	}
}

func TestPRListAndView(t *testing.T) {
	srv, _, _, _ := newMockServer(t)

	raw, rpcErr := call(t, srv, "pr.list", PRListParams{State: "all"})
	if rpcErr != nil {
		t.Fatalf("pr.list: %+v", rpcErr)
	}
	var list PRListResult
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Title != "Mock PR" {
		t.Fatalf("unexpected items: %+v", list)
	}

	number := list.Items[0].Number
	raw, rpcErr = call(t, srv, "pr.view", PRViewParams{Number: number})
	if rpcErr != nil {
		t.Fatalf("pr.view: %+v", rpcErr)
	}
	var pr github.PullRequest
	if err := json.Unmarshal(raw, &pr); err != nil {
		t.Fatalf("decode pr: %v", err)
	}
	if pr.Title != "Mock PR" {
		t.Fatalf("title = %q", pr.Title)
	}
}

// TestIssueViewCachedPRReportsOtherType verifies that issue.view reports the
// type mismatch for a number cached as a pull request instead of querying
// GitHub.
func TestIssueViewCachedPRReportsOtherType(t *testing.T) {
	srv, _, store, repo := newMockServer(t)
	if err := store.SavePR(repo.Host, repo.Owner, repo.Name, &github.PullRequest{Number: 2, Title: "Cached PR"}); err != nil {
		t.Fatalf("SavePR: %v", err)
	}

	_, rpcErr := call(t, srv, "issue.view", IssueViewParams{Number: 2})
	if rpcErr == nil {
		t.Fatal("issue.view succeeded for a cached pull request")
	}
	if !strings.Contains(rpcErr.Message, "is a pull request, not an issue") {
		t.Errorf("error = %q, want the pull request type message", rpcErr.Message)
	}
}

// TestPRViewCachedIssueReportsOtherType is the pull-request counterpart of
// TestIssueViewCachedPRReportsOtherType.
func TestPRViewCachedIssueReportsOtherType(t *testing.T) {
	srv, _, store, repo := newMockServer(t)
	if err := store.SaveIssue(repo.Host, repo.Owner, repo.Name, &github.Issue{Number: 1, Title: "Cached issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	_, rpcErr := call(t, srv, "pr.view", PRViewParams{Number: 1})
	if rpcErr == nil {
		t.Fatal("pr.view succeeded for a cached issue")
	}
	if !strings.Contains(rpcErr.Message, "is an issue, not a pull request") {
		t.Errorf("error = %q, want the issue type message", rpcErr.Message)
	}
}

func TestRepoListAndDelete(t *testing.T) {
	srv, _, store, repo := newMockServer(t)
	if err := store.SaveCacheInfo(repo.Host, repo.Owner, repo.Name, 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}

	raw, rpcErr := call(t, srv, "repo.list", nil)
	if rpcErr != nil {
		t.Fatalf("repo.list: %+v", rpcErr)
	}
	var list RepoListResult
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Repos) != 1 || list.Repos[0].Label != "mock/acme/testrepo" {
		t.Fatalf("unexpected repos: %+v", list.Repos)
	}

	raw, rpcErr = call(t, srv, "repo.delete", RepoDeleteParams{})
	if rpcErr != nil {
		t.Fatalf("repo.delete: %+v", rpcErr)
	}
	var deleted RepoDeleteResult
	if err := json.Unmarshal(raw, &deleted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !deleted.Deleted {
		t.Fatalf("expected deleted repository")
	}
}

func TestCacheRefresh(t *testing.T) {
	srv, _, _, _ := newMockServer(t)
	var got RefreshRequest
	srv.cfg.Refresh = func(ctx context.Context, req RefreshRequest) (RefreshResult, error) {
		got = req
		return RefreshResult{Issues: 3, PRs: 4}, nil
	}
	srv.cfg.ParseSince = func(s string) (time.Time, error) {
		return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), nil
	}

	raw, rpcErr := call(t, srv, "cache.refresh", CacheRefreshParams{
		Type:  "issues",
		Since: "2026-09-01",
	})
	if rpcErr != nil {
		t.Fatalf("cache.refresh: %+v", rpcErr)
	}
	var res CacheRefreshResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Issues != 3 || res.PRs != 4 {
		t.Fatalf("unexpected counts: %+v", res)
	}
	if got.Type != "issues" || got.Since == nil {
		t.Fatalf("request not forwarded: %+v", got)
	}

	if _, rpcErr := call(t, srv, "cache.refresh", CacheRefreshParams{Force: true, Since: "2026-09-01"}); rpcErr == nil {
		t.Fatal("expected force+since error")
	}
}

func TestInvalidParams(t *testing.T) {
	srv, _, _, _ := newMockServer(t)

	rec := postRPC(t, srv, `{"jsonrpc":"2.0","id":1,"method":"issue.list","params":[1,2,3]}`)
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
		t.Fatalf("expected invalid params, got %+v", resp)
	}

	if _, rpcErr := call(t, srv, "issue.view", IssueViewParams{Number: 0}); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Fatalf("expected invalid params, got %+v", rpcErr)
	}
	if _, rpcErr := call(t, srv, "issue.list", IssueListParams{State: "bogus"}); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Fatalf("expected invalid state, got %+v", rpcErr)
	}
}

func TestMissingRepoWithNoDefault(t *testing.T) {
	store := cache.NewStoreWithPath(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	srv := NewServer(Config{Store: store})

	req := `{"jsonrpc":"2.0","id":1,"method":"issue.list","params":{}}`
	rec := postRPC(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
		t.Fatalf("expected invalid params, got %+v", resp)
	}
}
