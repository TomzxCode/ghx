package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
)

// stubGHUser replaces the gh config lookup for the duration of a test and
// records the hosts it was asked about.
func stubGHUser(t *testing.T, logins map[string]string) *[]string {
	t.Helper()
	old := ghConfigGetUser
	calls := &[]string{}
	ghConfigGetUser = func(host string) string {
		*calls = append(*calls, host)
		return logins[host]
	}
	t.Cleanup(func() { ghConfigGetUser = old })
	return calls
}

func TestMeResolver_LiteralPassesThrough(t *testing.T) {
	calls := stubGHUser(t, nil)

	got, err := (&meResolver{}).resolve("alice", "github.com")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "alice" {
		t.Errorf("resolve = %q, want alice", got)
	}
	if len(*calls) != 0 {
		t.Errorf("gh config was read %d time(s) for a literal login", len(*calls))
	}
}

func TestMeResolver_ResolvesAtMostOncePerHost(t *testing.T) {
	calls := stubGHUser(t, map[string]string{
		"github.com":      "tomzx",
		"ghe.example.com": "tomek",
	})
	r := &meResolver{}

	for _, tc := range []struct {
		host, want string
	}{
		{host: "github.com", want: "tomzx"},
		{host: "ghe.example.com", want: "tomek"},
		{host: "github.com", want: "tomzx"},
	} {
		got, err := r.resolve("@me", tc.host)
		if err != nil {
			t.Fatalf("resolve(%q): %v", tc.host, err)
		}
		if got != tc.want {
			t.Errorf("resolve(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
	if len(*calls) != 2 {
		t.Errorf("gh config was read %d time(s), want one per host (2): %v", len(*calls), *calls)
	}
}

func TestMeResolver_NoStoredUser(t *testing.T) {
	stubGHUser(t, nil)

	_, err := (&meResolver{}).resolve("@me", "github.com")
	if err == nil {
		t.Fatal("expected an error when gh has no stored user")
	}
	for _, want := range []string{`cannot resolve "@me"`, "github.com", "gh auth login"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// freshIssueCache seeds a cache for acme/myproject that list commands serve
// without calling the API.
func freshIssueCache(t *testing.T, dir string) {
	t.Helper()
	store := cache.NewStoreWithPath(dir)
	defer store.Close()

	for _, issue := range []*github.Issue{
		{Number: 1, Title: "Alice issue", State: "OPEN", Author: github.Actor{Login: "alice"},
			Assignees: []github.Actor{{Login: "alice"}}},
		{Number: 2, Title: "Bob issue", State: "OPEN", Author: github.Actor{Login: "bob"},
			Assignees: []github.Actor{{Login: "bob"}}},
	} {
		if err := store.SaveIssue("github.com", "acme", "myproject", issue); err != nil {
			t.Fatalf("SaveIssue: %v", err)
		}
	}
	if err := store.SaveCacheInfoFull("github.com", "acme", "myproject", &cache.CacheInfo{
		CachedAt: time.Now(), Duration: 60,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}
}

func TestIssueList_MeAlias(t *testing.T) {
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	dir := t.TempDir()
	withReadFlags(t, "", dir)
	freshIssueCache(t, dir)
	stubGHUser(t, map[string]string{"github.com": "alice"})

	oldAssignee := issueListAssignee
	oldLabels, oldSearch, oldMilestone := issueListLabels, issueListSearch, issueListMilestone
	issueListAssignee = "@me"
	issueListLabels, issueListSearch, issueListMilestone = nil, "", ""
	t.Cleanup(func() {
		issueListAssignee = oldAssignee
		issueListLabels, issueListSearch, issueListMilestone = oldLabels, oldSearch, oldMilestone
	})

	out := captureStdout(t, func() {
		if err := runIssueList(nil, nil); err != nil {
			t.Fatalf("runIssueList: %v", err)
		}
	})
	if !strings.Contains(out, "#1") || strings.Contains(out, "Bob issue") {
		t.Errorf("output = %q, want only the issue assigned to @me", out)
	}
}

func TestPRList_MeAlias(t *testing.T) {
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	dir := t.TempDir()
	withReadFlags(t, "", dir)

	store := cache.NewStoreWithPath(dir)
	defer store.Close()
	for _, pr := range []*github.PullRequest{
		{Number: 1, Title: "Alice PR", State: "OPEN", Author: github.Actor{Login: "alice"}},
		{Number: 2, Title: "Bob PR", State: "OPEN", Author: github.Actor{Login: "bob"}},
	} {
		if err := store.SavePR("github.com", "acme", "myproject", pr); err != nil {
			t.Fatalf("SavePR: %v", err)
		}
	}
	if err := store.SaveCacheInfoFull("github.com", "acme", "myproject", &cache.CacheInfo{
		CachedAt: time.Now(), Duration: 60,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}
	stubGHUser(t, map[string]string{"github.com": "alice"})

	oldAuthor, oldState, oldLabels, oldLimit := prListAuthor, prListState, prListLabels, prListLimit
	prListAuthor, prListState, prListLabels, prListLimit = "@me", "open", nil, 1000
	t.Cleanup(func() {
		prListAuthor, prListState, prListLabels, prListLimit = oldAuthor, oldState, oldLabels, oldLimit
	})

	out := captureStdout(t, func() {
		if err := runPRList(nil, nil); err != nil {
			t.Fatalf("runPRList: %v", err)
		}
	})
	if !strings.Contains(out, "#1") || strings.Contains(out, "Bob PR") {
		t.Errorf("output = %q, want only the pull request authored by @me", out)
	}
}
