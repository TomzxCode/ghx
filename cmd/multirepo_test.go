package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
)

// seedFreshRepo writes fresh cache metadata and the given items for one
// repository, so a list command reads it without reaching for the API.
func seedFreshRepo(t *testing.T, dir, owner, name string, issues []*github.Issue, prs []*github.PullRequest) {
	t.Helper()
	store := cache.NewStoreWithPath(dir)
	defer store.Close()
	for _, issue := range issues {
		if err := store.SaveIssue("github.com", owner, name, issue); err != nil {
			t.Fatalf("SaveIssue(%s/%s#%d): %v", owner, name, issue.Number, err)
		}
	}
	for _, pr := range prs {
		if err := store.SavePR("github.com", owner, name, pr); err != nil {
			t.Fatalf("SavePR(%s/%s#%d): %v", owner, name, pr.Number, err)
		}
	}
	now := time.Now()
	if err := store.SaveCacheInfoFull("github.com", owner, name, &cache.CacheInfo{
		CachedAt:       now,
		Duration:       60,
		IssuesCachedAt: now,
		PRsCachedAt:    now,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull(%s/%s): %v", owner, name, err)
	}
}

func TestGetReposDeduplicates(t *testing.T) {
	old := repoFlags
	repoFlags = []string{"Acme/One", "github.com/acme/one", "acme/two,acme/one"}
	t.Cleanup(func() { repoFlags = old })

	repos, err := getRepos()
	if err != nil {
		t.Fatalf("getRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("repos = %d, want 2: %+v", len(repos), repos)
	}
	if got := repos[0].Owner + "/" + repos[0].Name; got != "acme/one" {
		t.Errorf("first repo = %q, want acme/one", got)
	}
	if got := repos[1].Owner + "/" + repos[1].Name; got != "acme/two" {
		t.Errorf("second repo = %q, want acme/two", got)
	}
}

func TestGetRepoRejectsMultiple(t *testing.T) {
	old := repoFlags
	repoFlags = []string{"acme/one", "acme/two"}
	t.Cleanup(func() { repoFlags = old })

	if _, err := getRepo(); err == nil {
		t.Fatal("expected an error when a single-repository command gets several --repo values")
	}
}

func TestIssueListMultipleRepos(t *testing.T) {
	dir := t.TempDir()
	withReadFlags(t, "http://127.0.0.1:1/graphql", dir)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	repoFlags = []string{"acme/one", "acme/two"}

	seedFreshRepo(t, dir, "acme", "one", []*github.Issue{
		{Number: 1, Title: "First repo issue", State: "OPEN", UpdatedAt: time.Now()},
	}, nil)
	seedFreshRepo(t, dir, "acme", "two", []*github.Issue{
		{Number: 7, Title: "Second repo issue", State: "OPEN", UpdatedAt: time.Now()},
	}, nil)

	out := captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})

	for _, want := range []string{"acme/one#1", "acme/two#7", "First repo issue", "Second repo issue"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestIssueListMultipleReposJSON(t *testing.T) {
	dir := t.TempDir()
	withReadFlags(t, "http://127.0.0.1:1/graphql", dir)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	repoFlags = []string{"acme/one", "acme/two"}
	issueListJSON = true

	seedFreshRepo(t, dir, "acme", "one", []*github.Issue{
		{Number: 1, Title: "First repo issue", State: "OPEN", UpdatedAt: time.Now()},
	}, nil)
	seedFreshRepo(t, dir, "acme", "two", nil, nil)

	out := captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})

	var got map[string][]github.Issue
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decoding JSON: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("repositories = %d, want 2: %s", len(got), out)
	}
	if issues := got["acme/one"]; len(issues) != 1 || issues[0].Number != 1 {
		t.Errorf("acme/one = %+v, want issue #1", issues)
	}
	if issues, ok := got["acme/two"]; !ok || len(issues) != 0 {
		t.Errorf("acme/two = %+v (present %v), want an empty list", issues, ok)
	}
}

func TestPRListMultipleReposJSON(t *testing.T) {
	dir := t.TempDir()
	withPRListFlags(t, "http://127.0.0.1:1/graphql", dir)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	repoFlags = []string{"acme/one", "acme/two"}
	prListJSON = "number"

	seedFreshRepo(t, dir, "acme", "one", nil, []*github.PullRequest{
		{Number: 3, Title: "First repo PR", State: "OPEN", HeadRefName: "one", BaseRefName: "main", UpdatedAt: time.Now()},
	})
	seedFreshRepo(t, dir, "acme", "two", nil, []*github.PullRequest{
		{Number: 4, Title: "Second repo PR", State: "OPEN", HeadRefName: "two", BaseRefName: "main", UpdatedAt: time.Now()},
	})

	out := captureStdout(t, func() {
		if err := runPRList(nil, nil); err != nil {
			t.Fatalf("runPRList: %v", err)
		}
	})

	var got map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decoding JSON: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("repositories = %d, want 2: %s", len(got), out)
	}
	for repo, number := range map[string]float64{"acme/one": 3, "acme/two": 4} {
		rows := got[repo]
		if len(rows) != 1 {
			t.Fatalf("%s rows = %v, want one", repo, rows)
		}
		if rows[0]["number"] != number {
			t.Errorf("%s number = %v, want %v", repo, rows[0]["number"], number)
		}
		if len(rows[0]) != 1 {
			t.Errorf("%s row has extra fields: %v", repo, rows[0])
		}
	}
}

func TestIssueListSingleRepoKeepsOutputShape(t *testing.T) {
	dir := t.TempDir()
	withReadFlags(t, "http://127.0.0.1:1/graphql", dir)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	repoFlags = []string{"acme/one"}

	seedFreshRepo(t, dir, "acme", "one", []*github.Issue{
		{Number: 1, Title: "Only issue", State: "OPEN", UpdatedAt: time.Now()},
	}, nil)

	out := captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})

	if strings.Contains(out, "acme/one#") {
		t.Errorf("single-repository output should not carry a repository prefix:\n%s", out)
	}
	if !strings.Contains(out, "#1") {
		t.Errorf("output missing #1:\n%s", out)
	}
}
