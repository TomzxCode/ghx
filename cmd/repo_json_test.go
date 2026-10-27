package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// withRepoListJSON isolates repo list's --json flag for the duration of a test.
func withRepoListJSON(t *testing.T, value string) {
	t.Helper()
	old := repoListJSON
	repoListJSON = value
	t.Cleanup(func() { repoListJSON = old })
}

// seedCachedRepo writes one issue so the repository appears in the cache, plus
// cache metadata when info is non-nil.
func seedCachedRepo(t *testing.T, host, owner, repo string, info *cache.CacheInfo) {
	t.Helper()
	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	defer store.Close()
	if err := store.SaveIssue(host, owner, repo, &github.Issue{
		Number: 1, Title: "Cached issue", State: "OPEN", Author: github.Actor{Login: "alice"},
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if info != nil {
		if err := store.SaveCacheInfoFull(host, owner, repo, info); err != nil {
			t.Fatalf("SaveCacheInfoFull: %v", err)
		}
	}
}

// seedCachedPR writes one pull request so the repository's PR count is non-zero.
func seedCachedPR(t *testing.T, host, owner, repo string, number int) {
	t.Helper()
	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	defer store.Close()
	if err := store.SavePR(host, owner, repo, &github.PullRequest{
		Number: number, Title: "Cached PR", State: "OPEN",
	}); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
}

func TestResolveRepoJSONFields(t *testing.T) {
	got, err := resolveRepoJSONFields("nameWithOwner,issueCount,prCount")
	if err != nil {
		t.Fatalf("resolveRepoJSONFields: %v", err)
	}
	if strings.Join(got, ",") != "nameWithOwner,issueCount,prCount" {
		t.Errorf("got %v", got)
	}

	// Duplicates collapse.
	got, err = resolveRepoJSONFields("nameWithOwner, nameWithOwner,prCount")
	if err != nil {
		t.Fatalf("resolveRepoJSONFields: %v", err)
	}
	if strings.Join(got, ",") != "nameWithOwner,prCount" {
		t.Errorf("dedupe: got %v", got)
	}

	// The sentinel selects every supported field.
	all, err := resolveRepoJSONFields(repoJSONAll)
	if err != nil {
		t.Fatalf("resolveRepoJSONFields(all): %v", err)
	}
	if len(all) != len(repoJSONFields) {
		t.Errorf("all fields: got %d, want %d", len(all), len(repoJSONFields))
	}

	// Unknown fields are rejected with the available list.
	if _, err := resolveRepoJSONFields("nameWithOwner,bogus"); err == nil ||
		!strings.Contains(err.Error(), "unknown JSON field") {
		t.Errorf("unknown field: got err %v", err)
	}
}

func TestResolveRepoListJSONFields(t *testing.T) {
	withRepoListJSON(t, "")

	// Not requested.
	if fields, err := resolveRepoListJSONFields(nil); err != nil || fields != nil {
		t.Errorf("unset: got %v, %v; want nil, nil", fields, err)
	}

	// Bare --json selects every field.
	repoListJSON = repoJSONAll
	fields, err := resolveRepoListJSONFields(nil)
	if err != nil {
		t.Fatalf("bare: %v", err)
	}
	if len(fields) != len(repoJSONFields) {
		t.Errorf("bare: got %d fields, want %d", len(fields), len(repoJSONFields))
	}

	// Space form: pflag leaves the field list as a positional argument.
	fields, err = resolveRepoListJSONFields([]string{"nameWithOwner,issueCount"})
	if err != nil {
		t.Fatalf("space form: %v", err)
	}
	if strings.Join(fields, ",") != "nameWithOwner,issueCount" {
		t.Errorf("space form: got %v", fields)
	}

	// Equals form sets the value directly.
	repoListJSON = "nameWithOwner,prCount"
	fields, err = resolveRepoListJSONFields(nil)
	if err != nil {
		t.Fatalf("equals form: %v", err)
	}
	if strings.Join(fields, ",") != "nameWithOwner,prCount" {
		t.Errorf("equals form: got %v", fields)
	}
}

// TestRepoList_JSONSelectedFields verifies the selected fields, the computed
// freshness status, and the structured hit rates.
func TestRepoList_JSONSelectedFields(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	telemetryDBFlag = filepath.Join(t.TempDir(), "telemetry.db")
	withRepoListJSON(t, repoJSONAll)

	seedCachedRepo(t, "github.com", "acme", "widget", &cache.CacheInfo{
		CachedAt: time.Now(), Duration: 60,
	})
	seedCachedPR(t, "github.com", "acme", "widget", 7)

	now := time.Now()
	tel, err := telemetry.Open(telemetryDBFlag)
	if err != nil {
		t.Fatalf("open telemetry: %v", err)
	}
	err = tel.InsertBatch([]telemetry.Event{
		{Timestamp: now, Kind: telemetry.KindCacheHit, Operation: telemetry.RetrievalIssueList, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheHit, Operation: telemetry.RetrievalIssueList, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheMiss, Operation: telemetry.RetrievalIssueList, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheMiss, Operation: telemetry.RetrievalPRView, Host: "github.com", Repo: "acme/widget"},
	})
	if err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	tel.Close()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runRepoList(cmd, []string{"nameWithOwner,issueCount,prCount,cachedAt,cacheDurationMinutes,status,hitRates"}); err != nil {
		t.Fatalf("runRepoList: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d repos, want 1\n%s", len(got), buf.String())
	}
	row := got[0]
	if len(row) != 7 {
		t.Errorf("got keys %v, want exactly the 7 selected fields", row)
	}
	if row["nameWithOwner"] != "acme/widget" {
		t.Errorf("nameWithOwner = %v", row["nameWithOwner"])
	}
	if n, _ := row["issueCount"].(float64); n != 1 {
		t.Errorf("issueCount = %v, want 1", row["issueCount"])
	}
	if n, _ := row["prCount"].(float64); n != 1 {
		t.Errorf("prCount = %v, want 1", row["prCount"])
	}
	if row["cachedAt"] == nil {
		t.Error("cachedAt is null, want a timestamp")
	}
	if n, _ := row["cacheDurationMinutes"].(float64); n != 60 {
		t.Errorf("cacheDurationMinutes = %v, want 60", row["cacheDurationMinutes"])
	}
	if row["status"] != "fresh" {
		t.Errorf("status = %v, want fresh", row["status"])
	}

	rates, ok := row["hitRates"].(map[string]any)
	if !ok {
		t.Fatalf("hitRates = %v, want an object", row["hitRates"])
	}
	issueList, ok := rates["issueList"].(map[string]any)
	if !ok {
		t.Fatalf("hitRates.issueList = %v, want an object", rates["issueList"])
	}
	if n, _ := issueList["hits"].(float64); n != 2 {
		t.Errorf("issueList.hits = %v, want 2", issueList["hits"])
	}
	if n, _ := issueList["misses"].(float64); n != 1 {
		t.Errorf("issueList.misses = %v, want 1", issueList["misses"])
	}
	if n, _ := issueList["total"].(float64); n != 3 {
		t.Errorf("issueList.total = %v, want 3", issueList["total"])
	}
	if n, _ := issueList["percent"].(float64); n != 67 {
		t.Errorf("issueList.percent = %v, want 67", issueList["percent"])
	}
	prView, ok := rates["prView"].(map[string]any)
	if !ok {
		t.Fatalf("hitRates.prView = %v, want an object", rates["prView"])
	}
	if n, _ := prView["percent"].(float64); n != 0 {
		t.Errorf("prView.percent = %v, want 0", prView["percent"])
	}
	if _, ok := rates["issueView"]; ok {
		t.Error("hitRates.issueView is present, want it omitted with no recorded retrievals")
	}
}

// TestRepoList_JSONWithoutCacheInfo verifies the null fields and the "no cache
// info" status when a repository has data but no cache metadata.
func TestRepoList_JSONWithoutCacheInfo(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	telemetryDBFlag = filepath.Join(t.TempDir(), "missing.db")
	withRepoListJSON(t, repoJSONAll)

	seedCachedRepo(t, "ghe.example.com", "acme", "widget", nil)

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runRepoList(cmd, []string{"nameWithOwner,host,owner,name,cachedAt,cacheAge,cacheDurationMinutes,status,hitRates"}); err != nil {
		t.Fatalf("runRepoList: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d repos, want 1", len(got))
	}
	row := got[0]
	if row["nameWithOwner"] != "ghe.example.com/acme/widget" {
		t.Errorf("nameWithOwner = %v", row["nameWithOwner"])
	}
	if row["host"] != "ghe.example.com" {
		t.Errorf("host = %v", row["host"])
	}
	if row["owner"] != "acme" {
		t.Errorf("owner = %v", row["owner"])
	}
	if row["name"] != "widget" {
		t.Errorf("name = %v", row["name"])
	}
	for _, field := range []string{"cachedAt", "cacheAge", "cacheDurationMinutes"} {
		if row[field] != nil {
			t.Errorf("%s = %v, want null without cache info", field, row[field])
		}
	}
	if row["status"] != "no cache info" {
		t.Errorf("status = %v, want \"no cache info\"", row["status"])
	}
	rates, ok := row["hitRates"].(map[string]any)
	if !ok {
		t.Fatalf("hitRates = %v, want an empty object", row["hitRates"])
	}
	if len(rates) != 0 {
		t.Errorf("hitRates = %v, want empty", rates)
	}
}

// TestRepoList_JSONStale verifies a repository past its configured duration is
// reported stale, with a human-readable age.
func TestRepoList_JSONStale(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	telemetryDBFlag = filepath.Join(t.TempDir(), "missing.db")
	withRepoListJSON(t, "status,cacheAge")

	seedCachedRepo(t, "github.com", "acme", "widget", &cache.CacheInfo{
		CachedAt: time.Now().Add(-2 * time.Hour), Duration: 60,
	})

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runRepoList(cmd, nil); err != nil {
		t.Fatalf("runRepoList: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d repos, want 1", len(got))
	}
	if got[0]["status"] != "stale" {
		t.Errorf("status = %v, want stale", got[0]["status"])
	}
	if got[0]["cacheAge"] != "2h0m" {
		t.Errorf("cacheAge = %v, want 2h0m", got[0]["cacheAge"])
	}
}

// TestRepoHitRates verifies every operation maps to its own JSON key and that
// operations with no recorded retrievals are omitted.
func TestRepoHitRates(t *testing.T) {
	rates := map[string]telemetry.CacheHitRate{
		hitRateKey("github.com", "acme/widget", telemetry.RetrievalIssueList): {Hits: 2, Misses: 1},
		hitRateKey("github.com", "acme/widget", telemetry.RetrievalIssueView): {Hits: 3, Misses: 1},
		hitRateKey("github.com", "acme/widget", telemetry.RetrievalPRList):    {Hits: 1, Misses: 3},
		hitRateKey("github.com", "acme/widget", telemetry.RetrievalPRView):    {Hits: 5, Misses: 5},
	}

	got := repoHitRates(rates, "github.com", "acme", "widget")
	want := map[string]repoHitRate{
		"issueList": {Hits: 2, Misses: 1, Total: 3, Percent: 67},
		"issueView": {Hits: 3, Misses: 1, Total: 4, Percent: 75},
		"prList":    {Hits: 1, Misses: 3, Total: 4, Percent: 25},
		"prView":    {Hits: 5, Misses: 5, Total: 10, Percent: 50},
	}
	for field, wantRate := range want {
		if got[field] != wantRate {
			t.Errorf("%s = %+v, want %+v", field, got[field], wantRate)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d rates, want %d", len(got), len(want))
	}

	// An operation with no retrievals is omitted, not zero-valued.
	got = repoHitRates(map[string]telemetry.CacheHitRate{
		hitRateKey("github.com", "acme/widget", telemetry.RetrievalPRView): {},
	}, "github.com", "acme", "widget")
	if len(got) != 0 {
		t.Errorf("zero-retrieval rates = %v, want none", got)
	}
}

func TestRepoList_JSONEmptyIsArray(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	withRepoListJSON(t, "nameWithOwner")

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runRepoList(cmd, nil); err != nil {
		t.Fatalf("runRepoList: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty result should print [], got %q", buf.String())
	}
}

// TestRepoList_CommandJSONFlagParsing drives the real cobra command so the
// `--json field1,field2` form is exercised exactly as the shell passes it
// (pflag leaves the field list as a positional argument after the value-less
// flag).
func TestRepoList_CommandJSONFlagParsing(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	withRepoListJSON(t, "")

	seedCachedRepo(t, "github.com", "acme", "widget", nil)

	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	rootCmd.SetArgs([]string{
		"--cache-dir", cacheDir,
		"--storage", "sqlite",
		"repo", "list",
		"--json", "nameWithOwner,issueCount",
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
		t.Fatalf("got %d repos, want 1\n%s", len(got), out)
	}
	if len(got[0]) != 2 {
		t.Errorf("got keys %v, want exactly nameWithOwner,issueCount", got[0])
	}
	if got[0]["nameWithOwner"] != "acme/widget" {
		t.Errorf("nameWithOwner = %v", got[0]["nameWithOwner"])
	}
}

// TestRepoList_CommandBareJSON selects every field through the real command.
func TestRepoList_CommandBareJSON(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	withRepoListJSON(t, "")

	seedCachedRepo(t, "github.com", "acme", "widget", nil)

	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	rootCmd.SetArgs([]string{
		"--cache-dir", cacheDir,
		"--storage", "sqlite",
		"repo", "list",
		"--json",
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
		t.Fatalf("got %d repos, want 1\n%s", len(got), out)
	}
	if len(got[0]) != len(repoJSONFields) {
		t.Errorf("got %d keys, want all %d fields", len(got[0]), len(repoJSONFields))
	}
}

// TestRepoList_TableStatus covers the two non-fresh rows the table can print
// after the status logic moved into repoStatus.
func TestRepoList_TableStatus(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	withRepoListJSON(t, "")

	seedCachedRepo(t, "github.com", "acme", "nometa", nil)
	seedCachedRepo(t, "github.com", "acme", "oldie", &cache.CacheInfo{
		CachedAt: time.Now().Add(-2 * time.Hour), Duration: 60,
	})

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runRepoList(cmd, nil); err != nil {
		t.Fatalf("runRepoList: %v", err)
	}
	for _, want := range []string{"no cache info", "stale", "2h0m"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q:\n%s", want, buf.String())
		}
	}
}
