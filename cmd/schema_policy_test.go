package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

// withSchemaPolicy sets the active --on-schema-change policy for one test.
func withSchemaPolicy(t *testing.T, policy cache.SchemaPolicy) {
	t.Helper()
	old := activeSchemaPolicy
	activeSchemaPolicy = policy
	t.Cleanup(func() { activeSchemaPolicy = old })
}

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

// cachedIssueStamp reads a file-backed issue's recorded schema version.
func cachedIssueStamp(t *testing.T, dir string, number int) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "github.com", "acme", "myproject", "issues", strconv.Itoa(number)+".json"))
	if err != nil {
		t.Fatalf("read cached issue: %v", err)
	}
	var stamp struct {
		SchemaVersion int `json:"_schemaVersion"`
	}
	if err := json.Unmarshal(data, &stamp); err != nil {
		t.Fatalf("decode cached issue: %v", err)
	}
	return stamp.SchemaVersion
}

// TestIssueView_OutdatedSchemaServedUnderServePolicy verifies that the default
// policy serves a cached issue written by an older record schema without
// calling GitHub: the mock server is closed, so any API read would fail.
func TestIssueView_OutdatedSchemaServedUnderServePolicy(t *testing.T) {
	scenario := readCacheScenario()
	srv := mockserver.NewServer(scenario)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)
	withSchemaPolicy(t, cache.SchemaServe)

	store := cache.NewStoreWithPath(dir)
	if err := store.SaveIssue("github.com", "acme", "myproject", &github.Issue{Number: 1, Title: "Cached issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SaveCacheInfo("github.com", "acme", "myproject", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	store.Close()
	restampCachedRecord(t, filepath.Join(dir, "github.com", "acme", "myproject", "issues", "1.json"), 2)
	srv.Close()

	out := captureStdout(t, func() {
		if err := runIssueView(nil, []string{"1"}); err != nil {
			t.Fatalf("runIssueView: %v", err)
		}
	})
	if out == "" {
		t.Error("runIssueView printed nothing")
	}
	if got := cachedIssueStamp(t, dir, 1); got != 2 {
		t.Errorf("serving the cache restamped the record to %d, want the original 2 left alone", got)
	}
}

// TestIssueView_OutdatedSchemaRefetchedUnderRefetchPolicy verifies that the
// refetch policy treats a record written by an older schema as a cache miss and
// rewrites it with the current version.
func TestIssueView_OutdatedSchemaRefetchedUnderRefetchPolicy(t *testing.T) {
	srv := mockserver.NewServer(readCacheScenario())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)
	withSchemaPolicy(t, cache.SchemaRefetch)

	store := cache.NewStoreWithPath(dir)
	if err := store.SaveIssue("github.com", "acme", "myproject", &github.Issue{Number: 1, Title: "Old cached issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SaveCacheInfo("github.com", "acme", "myproject", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	store.Close()
	restampCachedRecord(t, filepath.Join(dir, "github.com", "acme", "myproject", "issues", "1.json"), 2)

	out := captureStdout(t, func() {
		if err := runIssueView(nil, []string{"1"}); err != nil {
			t.Fatalf("runIssueView: %v", err)
		}
	})
	if out == "" {
		t.Error("runIssueView printed nothing")
	}
	if got := cachedIssueStamp(t, dir, 1); got != cache.IssueSchemaVersion {
		t.Errorf("record schema after refetch = %d, want %d", got, cache.IssueSchemaVersion)
	}
}

// TestIssueList_OutdatedSchemaServedUnderServePolicy verifies the list path
// serves an outdated cache under the default policy: the server is closed, so a
// list would fail if it fell back to the API.
func TestIssueList_OutdatedSchemaServedUnderServePolicy(t *testing.T) {
	srv := mockserver.NewServer(readCacheScenario())
	dir := t.TempDir()
	withReadFlags(t, srv.URL(), dir)
	withSchemaPolicy(t, cache.SchemaServe)

	store := cache.NewStoreWithPath(dir)
	if err := store.SaveIssue("github.com", "acme", "myproject", &github.Issue{Number: 1, Title: "Cached issue", State: "OPEN"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SaveCacheInfo("github.com", "acme", "myproject", 60); err != nil {
		t.Fatalf("SaveCacheInfo: %v", err)
	}
	store.Close()
	restampCachedRecord(t, filepath.Join(dir, "github.com", "acme", "myproject", "issues", "1.json"), 2)
	srv.Close()

	out := captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})
	if out == "" {
		t.Error("runIssueList printed nothing")
	}
}
