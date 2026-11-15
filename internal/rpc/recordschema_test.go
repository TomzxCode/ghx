package rpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
)

// restampCachedRecord rewrites a file-backed record's schema stamp, standing in
// for a record written by a different ghx version.
func restampCachedRecord(t *testing.T, path string, version int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cached record: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode cached record: %v", err)
	}
	record["_schemaVersion"] = version
	out, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("encode cached record: %v", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatalf("write cached record: %v", err)
	}
}

// TestIssueList_WarnsAboutOutdatedRecordSchema verifies a list served from a
// cache written by an older record schema reports the warning with its result.
func TestIssueList_WarnsAboutOutdatedRecordSchema(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStoreWithPath(dir)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveIssue("mock", "acme", "testrepo", &github.Issue{
		Number: 1, Title: "cached issue", State: "OPEN", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SaveCacheInfo("mock", "acme", "testrepo", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	restampCachedRecord(t, filepath.Join(dir, "mock", "acme", "testrepo", "issues", "1.json"), 2)

	repo := &gitremote.Repo{Host: "mock", Owner: "acme", Name: "testrepo"}
	srv := NewServer(Config{
		Store:       store,
		DefaultRepo: func() (*gitremote.Repo, error) { return repo, nil },
		Version:     "test",
	})

	raw, rpcErr := call(t, srv, "issue.list", IssueListParams{})
	if rpcErr != nil {
		t.Fatalf("issue.list: %+v", rpcErr)
	}
	var result IssueListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Source != "cache" {
		t.Fatalf("source = %q, want cache", result.Source)
	}
	for _, want := range []string{"record schema 2", "ghx cache --type issues --force"} {
		if !strings.Contains(result.Warning, want) {
			t.Errorf("warning missing %q: %q", want, result.Warning)
		}
	}
}

// TestIssueList_RefetchPolicyTreatsOutdatedRecordsAsMiss verifies the server's
// schema policy decides whether an outdated cache is served, and that a served
// current cache carries no warning.
func TestIssueList_RefetchPolicyTreatsOutdatedRecordsAsMiss(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStoreWithPath(dir)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveIssue("mock", "acme", "testrepo", &github.Issue{
		Number: 1, Title: "cached issue", State: "OPEN", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SaveCacheInfo("mock", "acme", "testrepo", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	restampCachedRecord(t, filepath.Join(dir, "mock", "acme", "testrepo", "issues", "1.json"), 2)

	repo := &gitremote.Repo{Host: "mock", Owner: "acme", Name: "testrepo"}
	srv := NewServer(Config{
		Store:        store,
		DefaultRepo:  func() (*gitremote.Repo, error) { return repo, nil },
		SchemaPolicy: cache.SchemaRefetch,
		// No client factory: serving the cache would succeed, so reaching the
		// API proves the outdated records were treated as a miss.
		Version: "test",
	})

	_, rpcErr := call(t, srv, "issue.list", IssueListParams{})
	if rpcErr == nil {
		t.Fatal("issue.list served the outdated cache under the refetch policy")
	}
}
