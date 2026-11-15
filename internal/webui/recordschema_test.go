package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
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

// TestListAndDetail_WarnAboutOutdatedRecordSchema verifies the UI payload names
// an older record schema, so the page can warn instead of presenting the data
// as current.
func TestListAndDetail_WarnAboutOutdatedRecordSchema(t *testing.T) {
	dir := t.TempDir()
	store := cache.NewStoreWithPath(dir)
	now := time.Now()
	seedRepo(t, store, "github.com", "acme", "testrepo", []*github.Issue{makeIssue(1, "cached issue", "OPEN", "", now)}, nil)
	restampCachedRecord(t, filepath.Join(dir, "github.com", "acme", "testrepo", "issues", "1.json"), 2)

	srv := newTestServer(store, nil)

	rec, body := get(t, srv.Handler(), "/api/repos/github.com/acme/testrepo/issues?state=all")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	warning, _ := body["schemaWarning"].(string)
	for _, want := range []string{"record schema 2", "ghx cache --type issues --force"} {
		if !strings.Contains(warning, want) {
			t.Errorf("list schemaWarning missing %q: %q", want, warning)
		}
	}

	rec, body = get(t, srv.Handler(), "/api/repos/github.com/acme/testrepo/issues/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	warning, _ = body["schemaWarning"].(string)
	if !strings.Contains(warning, "record schema 2") {
		t.Errorf("detail schemaWarning = %q, want the mismatch", warning)
	}
}

// TestList_OmitsSchemaWarningForCurrentRecords verifies the warning is absent
// when every cached record matches the current schema.
func TestList_OmitsSchemaWarningForCurrentRecords(t *testing.T) {
	store := newTestStore(t)
	seedRepo(t, store, "github.com", "acme", "testrepo", []*github.Issue{makeIssue(1, "issue", "OPEN", "", time.Now())}, nil)

	srv := newTestServer(store, nil)
	rec, body := get(t, srv.Handler(), "/api/repos/github.com/acme/testrepo/issues?state=all")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, ok := body["schemaWarning"]; ok {
		t.Errorf("schemaWarning present for current records: %v", body["schemaWarning"])
	}
}
