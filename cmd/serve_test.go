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
			Force:       true,
			FetchIssues: true,
			FetchPRs:    true,
			Duration:    60,
		})
	}
}
