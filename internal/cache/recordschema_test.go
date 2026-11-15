package cache

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/logging"
)

func TestParseSchemaPolicy(t *testing.T) {
	tests := []struct {
		in      string
		want    SchemaPolicy
		wantErr bool
	}{
		{in: "", want: SchemaServe},
		{in: "serve", want: SchemaServe},
		{in: "SERVE", want: SchemaServe},
		{in: " refetch ", want: SchemaRefetch},
		{in: "Refetch", want: SchemaRefetch},
		{in: "always", wantErr: true},
	}
	for _, tc := range tests {
		got, err := ParseSchemaPolicy(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseSchemaPolicy(%q) = %q, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSchemaPolicy(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSchemaPolicy(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRecordSchemaVersionPerKind verifies each data kind has its own schema
// version, so bumping one cannot invalidate the other.
func TestRecordSchemaVersionPerKind(t *testing.T) {
	if got := RecordSchemaVersion(KindIssues); got != IssueSchemaVersion {
		t.Errorf("RecordSchemaVersion(issues) = %d, want %d", got, IssueSchemaVersion)
	}
	if got := RecordSchemaVersion(KindPRs); got != PRSchemaVersion {
		t.Errorf("RecordSchemaVersion(prs) = %d, want %d", got, PRSchemaVersion)
	}
	if got := RecordSchemaVersion("comments"); got != 0 {
		t.Errorf("RecordSchemaVersion(comments) = %d, want 0 for an unknown kind", got)
	}
}

// TestSchemaStatusAdd verifies how recorded versions are folded into a status:
// current records are not counted, an unversioned record is a mismatch rather
// than an assumption of the current schema, and the oldest version is kept.
func TestSchemaStatusAdd(t *testing.T) {
	status := SchemaStatus{Current: 2}
	status.add(2, 3) // current
	status.add(1, 2) // older
	status.add(0, 4) // unversioned
	if status.Outdated != 2 {
		t.Errorf("Outdated = %d, want 2", status.Outdated)
	}
	if status.Unversioned != 4 {
		t.Errorf("Unversioned = %d, want 4", status.Unversioned)
	}
	if status.Oldest != 1 {
		t.Errorf("Oldest = %d, want 1", status.Oldest)
	}
	if status.Matches() {
		t.Error("Matches() = true for a mismatched status")
	}
	if !(SchemaStatus{Current: 2}).Matches() {
		t.Error("Matches() = false for records that all carry the current version")
	}
	if (SchemaStatus{Current: 1, Unversioned: 1}).Matches() {
		t.Error("Matches() = true for a record with no recorded version")
	}
}

func TestSchemaWarning(t *testing.T) {
	if got := SchemaWarning(KindIssues, SchemaStatus{Current: 2}); got != "" {
		t.Errorf("SchemaWarning for matching records = %q, want empty", got)
	}
	got := SchemaWarning(KindPRs, SchemaStatus{Current: 2, Outdated: 4, Oldest: 1})
	for _, want := range []string{"cached pull requests", "record schema 1", "writes 2", "ghx cache --type prs --force", "-on-schema-change=refetch"} {
		if !strings.Contains(got, want) {
			t.Errorf("SchemaWarning missing %q: %s", want, got)
		}
	}
	unversioned := SchemaWarning(KindIssues, SchemaStatus{Current: 2, Unversioned: 4})
	if !strings.Contains(unversioned, "have no recorded schema version") {
		t.Errorf("SchemaWarning for unversioned records = %q, want it to name the missing version", unversioned)
	}
	mixed := SchemaWarning(KindIssues, SchemaStatus{Current: 2, Outdated: 1, Unversioned: 4, Oldest: 1})
	for _, want := range []string{"record schema 1", "4 have no recorded schema version"} {
		if !strings.Contains(mixed, want) {
			t.Errorf("SchemaWarning for mixed records missing %q: %s", want, mixed)
		}
	}
}

// TestSQLiteRecordSchemaStatus verifies the SQLite backend stamps each record
// with its kind's version and reports records written by another version.
func TestSQLiteRecordSchemaStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := store.SaveIssue("github.com", "acme", "repo", &github.Issue{Number: 1, Title: "issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SavePR("github.com", "acme", "repo", &github.PullRequest{Number: 2, Title: "pr"}); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
	assertStatus(t, store, KindIssues, SchemaStatus{Current: IssueSchemaVersion})
	assertStatus(t, store, KindPRs, SchemaStatus{Current: PRSchemaVersion})
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A row stamped by a different ghx version is reported as mismatched, and
	// only for its own kind.
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	if _, err := db.Exec(`UPDATE issues SET schema_version = 2 WHERE number = 1`); err != nil {
		t.Fatalf("restamp issue: %v", err)
	}
	db.Close()

	store, err = NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore after restamp: %v", err)
	}
	defer store.Close()
	assertStatus(t, store, KindIssues, SchemaStatus{Current: IssueSchemaVersion, Outdated: 1, Oldest: 2})
	assertStatus(t, store, KindPRs, SchemaStatus{Current: PRSchemaVersion})
}

// TestSQLiteRecordSchemaStatusLegacyDatabase verifies a database created before
// the version column existed is reported as unversioned rather than assumed to
// match, because nothing proves those rows carry the current record shape.
func TestSQLiteRecordSchemaStatusLegacyDatabase(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := store.SaveIssue("github.com", "acme", "repo", &github.Issue{Number: 1, Title: "issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	store.Close()

	// Drop the column and roll the version back, which is the state a cache
	// written by the previous ghx leaves behind.
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE issues DROP COLUMN schema_version`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatalf("roll user_version: %v", err)
	}
	db.Close()

	store, err = NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore on the older database: %v", err)
	}
	defer store.Close()
	assertStatus(t, store, KindIssues, SchemaStatus{Current: IssueSchemaVersion, Unversioned: 1})
}

// TestFileRecordSchemaStatus verifies the file backend writes the version with
// each record, reports records written by another version, and reports a record
// written before versioning as unversioned rather than current.
func TestFileRecordSchemaStatus(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreWithPath(dir)
	if err := store.SaveIssue("github.com", "acme", "repo", &github.Issue{Number: 1, Title: "issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SavePR("github.com", "acme", "repo", &github.PullRequest{Number: 1, Title: "pr"}); err != nil {
		t.Fatalf("SavePR: %v", err)
	}

	// The stamp is part of the record file.
	data, err := os.ReadFile(filepath.Join(dir, "github.com", "acme", "repo", "issues", "1.json"))
	if err != nil {
		t.Fatalf("read cached issue: %v", err)
	}
	if !strings.Contains(string(data), `"_schemaVersion": 1`) {
		t.Errorf("cached issue file has no schema stamp:\n%s", data)
	}
	assertStatus(t, store, KindIssues, SchemaStatus{Current: IssueSchemaVersion})
	assertStatus(t, store, KindPRs, SchemaStatus{Current: PRSchemaVersion})

	// Rewrite one issue as a different ghx version would have left it.
	issuePath := filepath.Join(dir, "github.com", "acme", "repo", "issues", "1.json")
	rewriteStamp(t, issuePath, 2)
	assertStatus(t, store, KindIssues, SchemaStatus{Current: IssueSchemaVersion, Outdated: 1, Oldest: 2})
	assertStatus(t, store, KindPRs, SchemaStatus{Current: PRSchemaVersion})

	// A record written before versioning has no stamp and counts as
	// unversioned: an absent version is not proof of the current schema.
	legacy := filepath.Join(dir, "github.com", "acme", "repo", "issues", "2.json")
	if err := os.WriteFile(legacy, []byte(`{"number": 2, "title": "legacy"}`), 0644); err != nil {
		t.Fatalf("write legacy issue: %v", err)
	}
	assertStatus(t, store, KindIssues, SchemaStatus{Current: IssueSchemaVersion, Outdated: 1, Unversioned: 1, Oldest: 2})
}

// TestCanServeCachedRecords verifies the policy decides whether an outdated
// cache is served or refetched, and that the warning is emitted.
func TestCanServeCachedRecords(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreWithPath(dir)
	if err := store.SaveIssue("github.com", "acme", "policy", &github.Issue{Number: 1, Title: "issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	// A second repository keeps the warning assertion independent of the other
	// tests' warning dedupe.
	if err := store.SaveIssue("github.com", "acme", "policy-serve", &github.Issue{Number: 1, Title: "issue"}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	rewriteStamp(t, filepath.Join(dir, "github.com", "acme", "policy", "issues", "1.json"), 2)
	rewriteStamp(t, filepath.Join(dir, "github.com", "acme", "policy-serve", "issues", "1.json"), 2)

	var buf bytes.Buffer
	logging.SetOutput(&buf)
	t.Cleanup(func() {
		logging.SetOutput(os.Stderr)
		_ = logging.SetLevel("info")
	})

	if !CanServeCachedRecords(store, "github.com", "acme", "policy", KindIssues, SchemaServe) {
		t.Error("SchemaServe must serve cached records")
	}
	if CanServeCachedRecords(store, "github.com", "acme", "policy", KindIssues, SchemaRefetch) {
		t.Error("SchemaRefetch must not serve an outdated cache")
	}
	// A kind whose records all match is unaffected by the refetch policy.
	if !CanServeCachedRecords(store, "github.com", "acme", "policy", KindPRs, SchemaRefetch) {
		t.Error("SchemaRefetch must serve a cache that matches the current schema")
	}

	// Serving an outdated cache warns once, naming the mismatch.
	buf.Reset()
	if !CanServeCachedRecords(store, "github.com", "acme", "policy-serve", KindIssues, SchemaServe) {
		t.Fatal("SchemaServe must serve cached records")
	}
	CanServeCachedRecords(store, "github.com", "acme", "policy-serve", KindIssues, SchemaServe)
	if got := strings.Count(buf.String(), "record schema 2"); got != 1 {
		t.Errorf("mismatch warning emitted %d times, want once; got:\n%s", got, buf.String())
	}
}

// assertStatus reads one kind's schema status and compares it.
func assertStatus(t *testing.T, store Store, kind string, want SchemaStatus) {
	t.Helper()
	got, err := store.RecordSchemaStatus("github.com", "acme", "repo", kind)
	if err != nil {
		t.Fatalf("RecordSchemaStatus(%s): %v", kind, err)
	}
	if got != want {
		t.Errorf("RecordSchemaStatus(%s) = %+v, want %+v", kind, got, want)
	}
}

// rewriteStamp rewrites a cached record's schema stamp, standing in for a
// record written by a different ghx version.
func rewriteStamp(t *testing.T, path string, version int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	record[recordSchemaVersionKey] = json.RawMessage(strconv.Itoa(version))
	out, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatalf("write record: %v", err)
	}
}
