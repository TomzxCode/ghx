package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/mockserver"
)

func prJSONScenario() *mockserver.Scenario {
	return mockserver.NewScenarioBuilder("acme", "myproject").
		WithNow(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)).
		AddPR("Fix crash on start", "Fixes the crash.", "fix/crash", 8*24*time.Hour,
			mockserver.WithPRState("MERGED"),
		).
		AddPR("Add dark mode", "Adds dark theme.", "feat/dark-mode", 3*24*time.Hour).
		Build()
}

// withPRListFlags isolates the pr list globals and points them at a mock server
// and a temp cache dir for the duration of a test.
func withPRListFlags(t *testing.T, apiURL, dir string) {
	t.Helper()
	oldRepo, oldAPI, oldDir, oldStorage := repoFlag, apiURLFlag, cacheDir, storageFlag
	oldState, oldHead, oldJSON, oldLimit := prListState, prListHead, prListJSON, prListLimit
	oldNoTruncate, oldBase, oldApp := prListNoTruncate, prListBase, prListApp
	t.Cleanup(func() {
		repoFlag, apiURLFlag, cacheDir, storageFlag = oldRepo, oldAPI, oldDir, oldStorage
		prListState, prListHead, prListJSON, prListLimit = oldState, oldHead, oldJSON, oldLimit
		prListNoTruncate, prListBase, prListApp = oldNoTruncate, oldBase, oldApp
	})
	repoFlag = "acme/myproject"
	apiURLFlag = apiURL
	cacheDir = dir
	storageFlag = "file"
	prListState = "open"
	prListHead = ""
	prListJSON = ""
	prListLimit = 1000
	prListNoTruncate = false
	prListBase = ""
	prListApp = ""
}

func TestResolvePRJSONFields(t *testing.T) {
	got, err := resolvePRJSONFields("number,headRefOid,mergedAt")
	if err != nil {
		t.Fatalf("resolvePRJSONFields: %v", err)
	}
	want := []string{"number", "headRefOid", "mergedAt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}

	// Duplicates collapse.
	got, err = resolvePRJSONFields("number, number,title")
	if err != nil {
		t.Fatalf("resolvePRJSONFields: %v", err)
	}
	if strings.Join(got, ",") != "number,title" {
		t.Errorf("dedupe: got %v", got)
	}

	// The sentinel selects every supported field.
	all, err := resolvePRJSONFields(prJSONAll)
	if err != nil {
		t.Fatalf("resolvePRJSONFields(all): %v", err)
	}
	if len(all) != len(prJSONFields) {
		t.Errorf("all fields: got %d, want %d", len(all), len(prJSONFields))
	}

	// Unknown fields are rejected with the available list.
	if _, err := resolvePRJSONFields("number,bogus"); err == nil ||
		!strings.Contains(err.Error(), "unknown JSON field") {
		t.Errorf("unknown field: got err %v", err)
	}
}

func TestResolvePRListJSONFields(t *testing.T) {
	old := prListJSON
	t.Cleanup(func() { prListJSON = old })

	// Not requested.
	prListJSON = ""
	if fields, err := resolvePRListJSONFields(nil); err != nil || fields != nil {
		t.Errorf("unset: got %v, %v; want nil, nil", fields, err)
	}

	// Bare --json selects every field.
	prListJSON = prJSONAll
	fields, err := resolvePRListJSONFields(nil)
	if err != nil {
		t.Fatalf("bare: %v", err)
	}
	if len(fields) != len(prJSONFields) {
		t.Errorf("bare: got %d fields, want %d", len(fields), len(prJSONFields))
	}

	// Space form: pflag leaves the field list as a positional argument.
	fields, err = resolvePRListJSONFields([]string{"number,mergedAt"})
	if err != nil {
		t.Fatalf("space form: %v", err)
	}
	if strings.Join(fields, ",") != "number,mergedAt" {
		t.Errorf("space form: got %v", fields)
	}

	// Equals form sets the value directly.
	prListJSON = "number,headRefOid"
	fields, err = resolvePRListJSONFields(nil)
	if err != nil {
		t.Fatalf("equals form: %v", err)
	}
	if strings.Join(fields, ",") != "number,headRefOid" {
		t.Errorf("equals form: got %v", fields)
	}
}

func TestPRList_JSONSelectedFields(t *testing.T) {
	srv := mockserver.NewServer(prJSONScenario())
	t.Cleanup(srv.Close)
	withPRListFlags(t, srv.URL(), t.TempDir())

	prListState = "merged"
	prListHead = "fix/crash"
	prListJSON = prJSONAll // --json precedes its value

	var err error
	out := captureStdout(t, func() {
		err = runPRList(nil, []string{"number,headRefOid,mergedAt"})
	})
	if err != nil {
		t.Fatalf("runPRList: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("got %d PRs, want 1\n%s", len(got), out)
	}
	if len(got[0]) != 3 {
		t.Errorf("got keys %v, want exactly number,headRefOid,mergedAt", got[0])
	}
	if n, _ := got[0]["number"].(float64); n != 1 {
		t.Errorf("number = %v, want 1", got[0]["number"])
	}
	oid, _ := got[0]["headRefOid"].(string)
	if len(oid) != 40 {
		t.Errorf("headRefOid = %q, want a 40-character SHA", oid)
	}
	if got[0]["mergedAt"] == nil {
		t.Error("mergedAt is null, want a timestamp")
	}
}

// TestPRList_CommandJSONFlagParsing drives the real cobra command so the
// `--json field1,field2` form is exercised exactly as the shell passes it
// (pflag leaves the field list as a positional argument after the value-less
// flag).
func TestPRList_CommandJSONFlagParsing(t *testing.T) {
	srv := mockserver.NewServer(prJSONScenario())
	t.Cleanup(srv.Close)
	withPRListFlags(t, srv.URL(), t.TempDir())

	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	rootCmd.SetArgs([]string{
		"--api-url", srv.URL(),
		"--cache-dir", cacheDir,
		"--storage", "file",
		"--repo", "acme/myproject",
		"pr", "list",
		"--state", "merged",
		"--head", "fix/crash",
		"--json", "number,headRefOid,mergedAt",
	})

	var err error
	out := captureStdout(t, func() { err = rootCmd.Execute() })
	if err != nil {
		t.Fatalf("rootCmd.Execute: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("got %d PRs, want 1\n%s", len(got), out)
	}
	if len(got[0]) != 3 {
		t.Errorf("got keys %v, want exactly number,headRefOid,mergedAt", got[0])
	}
}

func TestPRList_JSONEmptyIsArray(t *testing.T) {
	srv := mockserver.NewServer(prJSONScenario())
	t.Cleanup(srv.Close)
	withPRListFlags(t, srv.URL(), t.TempDir())

	prListState = "merged"
	prListHead = "does-not-exist"
	prListJSON = "number"

	out := captureStdout(t, func() {
		if err := runPRList(nil, nil); err != nil {
			t.Fatalf("runPRList: %v", err)
		}
	})
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty result should print [], got %q", out)
	}
}
