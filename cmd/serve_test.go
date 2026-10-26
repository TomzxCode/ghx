package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/mockserver"
	"github.com/tomzxcode/ghx/internal/webui"
)

// TestServeRefreshWithMockServer exercises the serve refresh path end to end:
// the injected RefreshFunc fetches from a mock GitHub API through the same
// fetchRepoData logic used by `ghx cache`, populating the store so the UI
// endpoints see the new data.
func TestServeRefreshWithMockServer(t *testing.T) {
	scenario := mockserver.NewScenarioBuilder("acme", "testrepo").
		AddIssue("Mock issue", "Issue **body**", 24*time.Hour).
		AddPR("Mock PR", "PR body", "feature", 24*time.Hour).
		Build()
	mockSrv := mockserver.NewServer(scenario)
	t.Cleanup(mockSrv.Close)

	store := cache.NewStoreWithPath(t.TempDir())
	repo := &gitremote.Repo{Host: "mock", Owner: "acme", Name: "testrepo"}
	client, err := github.NewClientWithURL(mockSrv.URL(), "test-token", repo.Host)
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	refresh := serveRefreshFuncWithClient(store, client)
	handler := webui.New(store, refresh).Handler()

	// The repo must exist in the cache before the UI offers a refresh.
	if err := store.SaveCacheInfo(repo.Host, repo.Owner, repo.Name, 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/repos/mock/acme/testrepo/refresh", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d: %s", rec.Code, rec.Body.String())
	}
	var counts struct {
		Issues int `json:"issues"`
		PRs    int `json:"prs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &counts); err != nil {
		t.Fatalf("refresh response is not JSON: %v", err)
	}
	if counts.Issues != 1 || counts.PRs != 1 {
		t.Fatalf("expected 1 issue and 1 PR, got %+v", counts)
	}

	// The cache info reflects a complete fetch.
	info, err := store.LoadCacheInfo(repo.Host, repo.Owner, repo.Name)
	if err != nil || !info.Complete {
		t.Fatalf("expected complete cache info, got %v %v", info, err)
	}

	// The list endpoint serves the refreshed data.
	req = httptest.NewRequest(http.MethodGet, "/api/repos/mock/acme/testrepo/issues?state=all", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("list response is not JSON: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 issue in list, got %d", len(list.Items))
	}

	// The detail endpoint renders the markdown body.
	req = httptest.NewRequest(http.MethodGet, "/api/repos/mock/acme/testrepo/issues/1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var detail map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("detail response is not JSON: %v", err)
	}
	if bodyHTML, _ := detail["bodyHTML"].(string); len(bodyHTML) == 0 || !strings.Contains(bodyHTML, "<strong>body</strong>") {
		t.Fatalf("expected rendered markdown body, got %q", bodyHTML)
	}
}

// serveRefreshFuncWithClient is serveRefreshFunc with an explicit client so the
// test can point it at the mock server.
func serveRefreshFuncWithClient(store cache.Store, client *github.Client) webui.RefreshFunc {
	return func(host, owner, name string) (int, int, error) {
		repo := &gitremote.Repo{Host: host, Owner: owner, Name: name}
		info, _ := store.LoadCacheInfo(host, owner, name)
		if info == nil {
			info = &cache.CacheInfo{}
		}
		return fetchRepoData(store, client, repo, info, FetchOptions{
			FetchIssues: true,
			FetchPRs:    true,
			Duration:    60,
		})
	}
}

// TestServeItemRefreshWithMockServer exercises the single-item refresh path end
// to end: it re-fetches one cached issue or PR from the mock API even while the
// repository cache is fresh, and rewrites the cached record.
func TestServeItemRefreshWithMockServer(t *testing.T) {
	scenario := mockserver.NewScenarioBuilder("acme", "testrepo").
		AddIssue("Mock issue", "Issue **body**", 24*time.Hour).
		AddPR("Mock PR", "PR body", "feature", 24*time.Hour).
		Build()
	mockSrv := mockserver.NewServer(scenario)
	t.Cleanup(mockSrv.Close)

	store := cache.NewStoreWithPath(t.TempDir())
	repo := &gitremote.Repo{Host: "mock", Owner: "acme", Name: "testrepo"}
	client, err := github.NewClientWithURL(mockSrv.URL(), "test-token", repo.Host)
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}

	// Warm the cache through the repo-level fetch so both items exist and the
	// cache counts as fresh.
	if _, _, err := fetchRepoData(store, client, repo, &cache.CacheInfo{}, FetchOptions{
		FetchIssues: true,
		FetchPRs:    true,
		Duration:    60,
	}); err != nil {
		t.Fatalf("fetchRepoData: %v", err)
	}

	srv := webui.New(store, nil)
	srv.SetItemRefreshFunc(func(_ string, _ string, _ string, kind string, number int) error {
		return refreshItem(store, client, repo, kind, number)
	})
	handler := srv.Handler()

	// Change what the API serves: only a forced item fetch sees the new titles.
	mockSrv.UpdateScenario(func(s *mockserver.Scenario) {
		for i := range s.Issues {
			s.Issues[i].Issue.Title = "Updated issue"
		}
		for i := range s.PRs {
			s.PRs[i].PullRequest.Title = "Updated PR"
		}
	})

	for _, tc := range []struct {
		path string
	}{
		{"/api/repos/mock/acme/testrepo/issues/1/refresh"},
		{"/api/repos/mock/acme/testrepo/prs/2/refresh"},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", tc.path, rec.Code, rec.Body.String())
		}
	}

	issue, _, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, 1)
	if err != nil || issue.Title != "Updated issue" {
		t.Fatalf("expected the refreshed issue in the cache, got %v %v", issue, err)
	}
	pr, _, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, 2)
	if err != nil || pr.Title != "Updated PR" {
		t.Fatalf("expected the refreshed pull request in the cache, got %v %v", pr, err)
	}
}
