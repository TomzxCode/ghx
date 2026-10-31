package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

// setupSearchTest isolates the storage and telemetry globals used by the
// search commands, and points the cache at a fresh temporary directory.
func setupSearchTest(t *testing.T) {
	t.Helper()
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	oldRepo := repoFlag
	repoFlag = ""
	t.Cleanup(func() { repoFlag = oldRepo })

	searchIssuesAssignee, searchIssuesAuthor = "", ""
	searchIssuesLabels, searchIssuesLimit = nil, 30
	searchIssuesMilestone, searchIssuesState = "", "all"
	searchIssuesJSON, searchIssuesNoTruncate = "", false
	searchIssuesApp, searchIssuesClosed, searchIssuesCommenter = "", "", ""
	searchIssuesComments, searchIssuesCreated = "", ""
	searchIssuesIncludePRs, searchIssuesInvolves = false, ""
	searchIssuesMatch, searchIssuesMentions = nil, ""
	searchIssuesNoAssignee, searchIssuesNoLabel, searchIssuesNoMilestone = false, false, false
	searchIssuesOrder, searchIssuesOwner, searchIssuesSort = "desc", nil, ""
	searchIssuesTeamMentions, searchIssuesUpdated, searchIssuesTemplate = "", "", ""

	searchPRsAssignee, searchPRsAuthor = "", ""
	searchPRsBase, searchPRsHead, searchPRsLabels = "", "", nil
	searchPRsDraft, searchPRsLimit = false, 30
	searchPRsState, searchPRsJSON, searchPRsNoTruncate = "all", "", false
	searchPRsApp, searchPRsChecks, searchPRsClosed = "", "", ""
	searchPRsCommenter, searchPRsComments, searchPRsCreated = "", "", ""
	searchPRsInvolves, searchPRsMatch, searchPRsMentions = "", nil, ""
	searchPRsMerged, searchPRsMergedAt, searchPRsMilestone = false, "", ""
	searchPRsNoAssignee, searchPRsNoLabel, searchPRsNoMilestone = false, false, false
	searchPRsOrder, searchPRsOwner = "desc", nil
	searchPRsReview, searchPRsReviewRequested, searchPRsReviewedBy = "", "", ""
	searchPRsSort, searchPRsTeamMentions = "", ""
	searchPRsUpdated, searchPRsTemplate = "", ""
}

// seedSearchCache writes a small two-repository cache. Dates, comments,
// reviews, and checks are fixed so the filter tests are deterministic.
func seedSearchCache(t *testing.T) {
	t.Helper()
	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	defer store.Close()

	day := func(n int) time.Time {
		return time.Date(2026, 1, n, 12, 0, 0, 0, time.UTC)
	}
	comment := func(login, body string, at time.Time) github.Comment {
		return github.Comment{Author: github.Actor{Login: login}, Body: body, CreatedAt: at}
	}

	issues := []*github.Issue{
		{
			Number:    1,
			Title:     "Fix login bug",
			State:     "OPEN",
			Author:    github.Actor{Login: "alice"},
			Assignees: []github.Actor{{Login: "alice"}},
			Milestone: &github.Milestone{Number: 2, Title: "v2"},
			Body:      "Login fails on Safari, cc @dave",
			Comments:  []github.Comment{comment("erin", "I can reproduce this.", day(3))},
			CreatedAt: day(1),
			UpdatedAt: day(3),
		},
		{
			Number:    2,
			Title:     "Add dark mode",
			State:     "CLOSED",
			Author:    github.Actor{Login: "carol"},
			Labels:    []github.Label{{Name: "enhancement"}},
			Body:      "Add a dark theme",
			ClosedAt:  ptrTime(day(5)),
			CreatedAt: time.Date(2025, 12, 20, 12, 0, 0, 0, time.UTC),
			UpdatedAt: day(1),
		},
	}
	for _, issue := range issues {
		if err := store.SaveIssue("github.com", "acme", "widget", issue); err != nil {
			t.Fatalf("SaveIssue: %v", err)
		}
	}
	if err := store.SaveIssue("github.com", "acme", "gadget", &github.Issue{
		Number:    7,
		Title:     "Login timeout",
		State:     "OPEN",
		Author:    github.Actor{Login: "bob"},
		Assignees: []github.Actor{{Login: "bob"}},
		Labels:    []github.Label{{Name: "bug"}},
		Milestone: &github.Milestone{Number: 2, Title: "v2"},
		Body:      "SSO login hangs; please ping @org/platform-team",
		CreatedAt: day(2),
		UpdatedAt: day(2),
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	prs := []*github.PullRequest{
		{
			Number:      10,
			Title:       "Fix crash on start",
			State:       "MERGED",
			Author:      github.Actor{Login: "alice"},
			Assignees:   []github.Actor{{Login: "alice"}},
			Labels:      []github.Label{{Name: "bug"}},
			Milestone:   &github.Milestone{Number: 2, Title: "v2"},
			BaseRefName: "main",
			HeadRefName: "fix/crash",
			Body:        "Fixes the startup crash",
			Comments:    []github.Comment{comment("erin", "LGTM", day(3))},
			Reviews: []github.PRReview{
				{Author: github.Actor{Login: "carol"}, State: "APPROVED", SubmittedAt: day(2)},
			},
			ReviewDecision: "APPROVED",
			Checks:         &github.PRChecks{State: "SUCCESS", Total: 2},
			CreatedAt:      day(1),
			UpdatedAt:      day(3),
			MergedAt:       ptrTime(day(2)),
			ClosedAt:       ptrTime(day(2)),
		},
		{
			Number:         11,
			Title:          "WIP dark mode",
			State:          "OPEN",
			IsDraft:        true,
			Author:         github.Actor{Login: "bob"},
			BaseRefName:    "develop",
			HeadRefName:    "feat/dark-mode",
			Body:           "Adds a dark theme",
			ReviewDecision: "REVIEW_REQUIRED",
			ReviewRequests: []github.ReviewRequest{{Type: "User", Login: "dave"}},
			Checks:         &github.PRChecks{State: "PENDING", Total: 1},
			CreatedAt:      day(1),
			UpdatedAt:      day(2),
		},
	}
	for _, pr := range prs {
		if err := store.SavePR("github.com", "acme", "widget", pr); err != nil {
			t.Fatalf("SavePR: %v", err)
		}
	}
	if err := store.SavePR("github.com", "acme", "gadget", &github.PullRequest{
		Number:         12,
		Title:          "Crash on exit",
		State:          "OPEN",
		Author:         github.Actor{Login: "bob"},
		BaseRefName:    "main",
		HeadRefName:    "fix/exit",
		Body:           "Exit crashes",
		Comments:       []github.Comment{comment("erin", "still crashing", day(1))},
		Reviews:        []github.PRReview{{Author: github.Actor{Login: "frank"}, State: "CHANGES_REQUESTED", SubmittedAt: day(1)}},
		ReviewDecision: "CHANGES_REQUESTED",
		Checks:         &github.PRChecks{State: "FAILURE", Total: 3},
		CreatedAt:      time.Date(2025, 12, 28, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      day(1),
	}); err != nil {
		t.Fatalf("SavePR: %v", err)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestSearchIssues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		backend string
	}{
		{name: "sqlite", backend: ""},
		{name: "file", backend: "file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSearchTest(t)
			storageFlag = tc.backend
			seedSearchCache(t)

			out := captureStdout(t, func() {
				if err := runSearchIssues(nil, []string{"login"}); err != nil {
					t.Fatalf("runSearchIssues: %v", err)
				}
			})
			if !strings.Contains(out, "acme/widget#1") || !strings.Contains(out, "acme/gadget#7") {
				t.Errorf("output = %q, want both login matches", out)
			}
			if strings.Contains(out, "acme/widget#2") {
				t.Errorf("output = %q, must not include the unrelated issue", out)
			}
			// Most recently updated first.
			if strings.Index(out, "acme/widget#1") > strings.Index(out, "acme/gadget#7") {
				t.Errorf("output = %q, want widget#1 (newer) before gadget#7", out)
			}

			// Terms are ANDed across title and body, and a quoted phrase is
			// matched as written.
			out = captureStdout(t, func() {
				if err := runSearchIssues(nil, []string{"login", "safari"}); err != nil {
					t.Fatalf("runSearchIssues: %v", err)
				}
			})
			if !strings.Contains(out, "acme/widget#1") || strings.Contains(out, "gadget#7") {
				t.Errorf("AND query output = %q, want only widget#1", out)
			}

			out = captureStdout(t, func() {
				if err := runSearchIssues(nil, []string{"login bug"}); err != nil {
					t.Fatalf("runSearchIssues: %v", err)
				}
			})
			if !strings.Contains(out, "acme/widget#1") || strings.Contains(out, "gadget#7") {
				t.Errorf("phrase query output = %q, want only widget#1", out)
			}
		})
	}
}

func TestSearchIssues_FiltersAndLimit(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	searchIssuesState = "open"
	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, nil); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#1") || !strings.Contains(out, "acme/gadget#7") {
		t.Errorf("output = %q, want open issues", out)
	}
	if strings.Contains(out, "acme/widget#2") {
		t.Errorf("output = %q, closed issue must be filtered out", out)
	}

	searchIssuesState = "all"
	searchIssuesAuthor = "carol"
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, nil); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#2") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("author filter output = %q, want only widget#2", out)
	}

	searchIssuesAuthor = ""
	searchIssuesLabels = []string{"enhancement"}
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, nil); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#2") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("label filter output = %q, want only widget#2", out)
	}

	searchIssuesLabels = nil
	searchIssuesLimit = 1
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, nil); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if strings.Count(out, "acme/") != 1 {
		t.Errorf("limit output = %q, want exactly one result", out)
	}
}

func TestSearchIssues_MeAlias(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	stubGHUser(t, map[string]string{"github.com": "carol"})
	searchIssuesAuthor = "@me"

	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, nil); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#2") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("output = %q, want only carol's issue", out)
	}
}

func TestSearchPRs_MeAlias(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	stubGHUser(t, map[string]string{"github.com": "bob"})
	searchPRsAuthor = "@me"

	out := captureStdout(t, func() {
		if err := runSearchPRs(nil, nil); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#11") || !strings.Contains(out, "acme/gadget#12") {
		t.Errorf("output = %q, want bob's pull requests", out)
	}
	if strings.Contains(out, "acme/widget#10") {
		t.Errorf("output = %q, must not include alice's pull request", out)
	}
}

func TestSearchIssues_MeUnresolvable(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	stubGHUser(t, nil)
	searchIssuesAssignee = "@me"

	err := runSearchIssues(nil, nil)
	if err == nil || !strings.Contains(err.Error(), `cannot resolve "@me"`) {
		t.Errorf("expected an @me resolution error, got %v", err)
	}
}

func TestSearchIssues_RepoScope(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	repoFlag = "acme/gadget"

	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"login"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/gadget#7") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("output = %q, want only the scoped repository", out)
	}
}

func TestSearchIssues_NoCachedRepos(t *testing.T) {
	setupSearchTest(t)

	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"login"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, `No cached repositories found. Run "ghx cache" before "ghx search issues".`) {
		t.Errorf("output = %q, want the no-cache hint", out)
	}
}

func TestSearchIssues_NoMatches(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"nonexistent"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "No cached issues matched your search.") {
		t.Errorf("output = %q, want the no-match message", out)
	}
}

func TestSearchIssues_JSON(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"login"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#1") {
		t.Fatalf("text output = %q, want a match", out)
	}

	searchIssuesJSON = searchJSONAll
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"login"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})

	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal JSON output %q: %v", out, err)
	}
	if len(results) != 2 {
		t.Fatalf("JSON results = %d, want 2", len(results))
	}
	repo, ok := results[0]["repository"].(map[string]any)
	if !ok {
		t.Fatalf("first result has no repository object: %v", results[0])
	}
	if repo["nameWithOwner"] != "acme/widget" {
		t.Errorf("repository.nameWithOwner = %v, want acme/widget", repo["nameWithOwner"])
	}
	if results[0]["title"] != "Fix login bug" {
		t.Errorf("title = %v, want the embedded issue field", results[0]["title"])
	}
}

func TestSearchPRs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		backend string
	}{
		{name: "sqlite", backend: ""},
		{name: "file", backend: "file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSearchTest(t)
			storageFlag = tc.backend
			seedSearchCache(t)

			out := captureStdout(t, func() {
				if err := runSearchPRs(nil, []string{"crash"}); err != nil {
					t.Fatalf("runSearchPRs: %v", err)
				}
			})
			if !strings.Contains(out, "acme/widget#10") || !strings.Contains(out, "acme/gadget#12") {
				t.Errorf("output = %q, want both crash matches", out)
			}
			if !strings.Contains(out, "merged") {
				t.Errorf("output = %q, want the merged state", out)
			}

			// Issue matches must not leak into PR results.
			if strings.Contains(out, "acme/widget#1 ") {
				t.Errorf("output = %q, must not include issues", out)
			}
		})
	}
}

func TestSearchPRs_Filters(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	searchPRsState = "merged"
	out := captureStdout(t, func() {
		if err := runSearchPRs(nil, nil); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#10") || strings.Contains(out, "acme/widget#11") {
		t.Errorf("state filter output = %q, want only the merged PR", out)
	}

	searchPRsState = "all"
	searchPRsDraft = true
	out = captureStdout(t, func() {
		if err := runSearchPRs(nil, nil); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#11") || strings.Contains(out, "acme/widget#10") {
		t.Errorf("draft filter output = %q, want only the draft PR", out)
	}

	searchPRsDraft = false
	searchPRsBase = "main"
	out = captureStdout(t, func() {
		if err := runSearchPRs(nil, nil); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#10") || !strings.Contains(out, "acme/gadget#12") {
		t.Errorf("base filter output = %q, want both main-based PRs", out)
	}
	if strings.Contains(out, "acme/widget#11") {
		t.Errorf("base filter output = %q, must exclude develop-based PR", out)
	}

	searchPRsBase = ""
	searchPRsLabels = []string{"bug"}
	out = captureStdout(t, func() {
		if err := runSearchPRs(nil, nil); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#10") || strings.Contains(out, "acme/gadget#12") {
		t.Errorf("label filter output = %q, want only the bug-labelled PR", out)
	}
}

func TestSearchPRs_JSON(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	searchPRsJSON = searchJSONAll

	out := captureStdout(t, func() {
		if err := runSearchPRs(nil, []string{"dark"}); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})

	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal JSON output %q: %v", out, err)
	}
	if len(results) != 1 {
		t.Fatalf("JSON results = %d, want 1", len(results))
	}
	if results[0]["isDraft"] != true {
		t.Errorf("isDraft = %v, want the embedded PR field", results[0]["isDraft"])
	}
	repo, ok := results[0]["repository"].(map[string]any)
	if !ok || repo["nameWithOwner"] != "acme/widget" {
		t.Errorf("repository = %v, want acme/widget", results[0]["repository"])
	}
}

func TestSearchPRs_NoMatches(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	out := captureStdout(t, func() {
		if err := runSearchPRs(nil, []string{"nonexistent"}); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})
	if !strings.Contains(out, "No cached pull requests matched your search.") {
		t.Errorf("output = %q, want the no-match message", out)
	}
}

func TestSearch_RejectsNegativeLimit(t *testing.T) {
	setupSearchTest(t)
	searchIssuesLimit = -1
	if err := runSearchIssues(nil, nil); err == nil || !strings.Contains(err.Error(), "invalid --limit") {
		t.Errorf("expected invalid limit error, got %v", err)
	}
}

func runSearchIssuesOutput(t *testing.T) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := runSearchIssues(nil, nil); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
}

func runSearchPRsOutput(t *testing.T) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := runSearchPRs(nil, nil); err != nil {
			t.Fatalf("runSearchPRs: %v", err)
		}
	})
}

func TestSearchIssues_MatchFields(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"reproduce"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#1") {
		t.Errorf("default match output = %q, want comments searched by default", out)
	}

	searchIssuesMatch = []string{"body"}
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"reproduce"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "No cached issues matched") {
		t.Errorf("--match body output = %q, want no match", out)
	}

	searchIssuesMatch = []string{"comments"}
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"reproduce"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "acme/widget#1") {
		t.Errorf("--match comments output = %q, want widget#1", out)
	}

	searchIssuesMatch = []string{"title"}
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"reproduce"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	if !strings.Contains(out, "No cached issues matched") {
		t.Errorf("--match title output = %q, want no match", out)
	}
}

func TestSearchIssues_CommenterMentionsInvolves(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	searchIssuesCommenter = "erin"
	out := runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#1") || strings.Contains(out, "acme/gadget#7") {
		t.Errorf("--commenter output = %q, want widget#1 only", out)
	}

	searchIssuesCommenter = ""
	searchIssuesMentions = "dave"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#1") || strings.Contains(out, "acme/gadget#7") {
		t.Errorf("--mentions output = %q, want widget#1 only", out)
	}

	searchIssuesMentions = "ave"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "No cached issues matched") {
		t.Errorf("--mentions partial output = %q, want no partial login match", out)
	}

	searchIssuesMentions = "org/platform-team"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/gadget#7") {
		t.Errorf("--mentions team output = %q, want gadget#7", out)
	}

	searchIssuesMentions = ""
	searchIssuesTeamMentions = "org/platform-team"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/gadget#7") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("--team-mentions output = %q, want gadget#7 only", out)
	}

	searchIssuesTeamMentions = ""
	searchIssuesInvolves = "erin"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#1") {
		t.Errorf("--involves commenter output = %q, want widget#1", out)
	}

	searchIssuesInvolves = "bob"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/gadget#7") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("--involves author output = %q, want gadget#7 only", out)
	}
}

func TestSearchIssues_AbsenceFilters(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	searchIssuesNoAssignee = true
	out := runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#2") || strings.Contains(out, "acme/widget#1") || strings.Contains(out, "gadget#7") {
		t.Errorf("--no-assignee output = %q, want widget#2 only", out)
	}

	searchIssuesNoAssignee = false
	searchIssuesNoLabel = true
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#1") || strings.Contains(out, "acme/widget#2") {
		t.Errorf("--no-label output = %q, want widget#1 only", out)
	}

	searchIssuesNoLabel = false
	searchIssuesNoMilestone = true
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#2") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("--no-milestone output = %q, want widget#2 only", out)
	}
}

func TestSearchIssues_DateAndNumberFilters(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	searchIssuesCreated = "2026-01-02"
	out := runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/gadget#7") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("--created output = %q, want gadget#7 only", out)
	}

	searchIssuesCreated = "2026-01-01..2026-01-02"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#1") || !strings.Contains(out, "acme/gadget#7") {
		t.Errorf("--created range output = %q, want both January issues", out)
	}

	searchIssuesCreated = ">2026-01-01"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/gadget#7") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("--created > output = %q, want gadget#7 only", out)
	}

	searchIssuesCreated = ""
	searchIssuesUpdated = "<2026-01-02"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#2") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("--updated output = %q, want widget#2 only", out)
	}

	searchIssuesUpdated = ""
	searchIssuesClosed = "2026-01-05"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#2") {
		t.Errorf("--closed output = %q, want widget#2", out)
	}

	searchIssuesClosed = ""
	searchIssuesComments = ">=1"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#1") || strings.Contains(out, "acme/gadget#7") {
		t.Errorf("--comments output = %q, want widget#1 only", out)
	}
}

func TestSearchIssues_SortAndOrder(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	out := runSearchIssuesOutput(t)
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, "acme/widget#1") {
		t.Errorf("default sort first row = %q, want the most recently updated issue", first)
	}

	searchIssuesSort = "created"
	out = runSearchIssuesOutput(t)
	first = strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, "acme/gadget#7") {
		t.Errorf("--sort created first row = %q, want gadget#7", first)
	}

	searchIssuesSort = "created"
	searchIssuesOrder = "asc"
	out = runSearchIssuesOutput(t)
	first = strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, "acme/widget#2") {
		t.Errorf("--order asc first row = %q, want widget#2", first)
	}
}

func TestSearchIssues_OwnerAndIncludePRs(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	if err := store.SaveIssue("github.com", "other", "tool", &github.Issue{
		Number: 5, Title: "Login elsewhere", State: "OPEN",
		Author: github.Actor{Login: "zoe"}, Body: "login",
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	store.Close()

	searchIssuesOwner = []string{"other"}
	out := runSearchIssuesOutput(t)
	if !strings.Contains(out, "other/tool#5") || strings.Contains(out, "acme/") {
		t.Errorf("--owner output = %q, want only the other owner's repository", out)
	}

	searchIssuesOwner = nil
	searchIssuesIncludePRs = true
	searchIssuesState = "closed"
	out = runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#2") || !strings.Contains(out, "[PR] Fix crash on start") {
		t.Errorf("--include-prs classed output = %q, want the closed issue and the merged PR", out)
	}
	if strings.Contains(out, "acme/gadget#7") {
		t.Errorf("--include-prs output = %q, must keep the open issue out", out)
	}
}

func TestSearchIssues_AppFilter(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	if err := store.SaveIssue("github.com", "acme", "widget", &github.Issue{
		Number: 3, Title: "Bump dependency", State: "OPEN",
		Author: github.Actor{Login: "dependabot[bot]", Type: "Bot"},
		Body:   "Bumps a dependency",
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	store.Close()

	searchIssuesApp = "dependabot"
	out := runSearchIssuesOutput(t)
	if !strings.Contains(out, "acme/widget#3") || strings.Contains(out, "acme/widget#1") {
		t.Errorf("--app output = %q, want dependabot's issue only", out)
	}
}

func TestSearchPRs_NewFilters(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)

	for _, tc := range []struct {
		name   string
		set    func()
		want   []string
		reject []string
	}{
		{
			name: "merged",
			set:  func() { searchPRsMerged = true },
			want: []string{"acme/widget#10"},
		},
		{
			name:   "merged-at",
			set:    func() { searchPRsMergedAt = "2026-01-02" },
			want:   []string{"acme/widget#10"},
			reject: []string{"acme/widget#11", "acme/gadget#12"},
		},
		{
			name:   "review approved",
			set:    func() { searchPRsReview = "approved" },
			want:   []string{"acme/widget#10"},
			reject: []string{"acme/widget#11", "acme/gadget#12"},
		},
		{
			name: "review changes requested",
			set:  func() { searchPRsReview = "changes_requested" },
			want: []string{"acme/gadget#12"},
		},
		{
			name: "review required",
			set:  func() { searchPRsReview = "required" },
			want: []string{"acme/widget#11"},
		},
		{
			name: "review requested",
			set:  func() { searchPRsReviewRequested = "dave" },
			want: []string{"acme/widget#11"},
		},
		{
			name:   "reviewed by",
			set:    func() { searchPRsReviewedBy = "carol" },
			want:   []string{"acme/widget#10"},
			reject: []string{"acme/gadget#12"},
		},
		{
			name: "checks failure",
			set:  func() { searchPRsChecks = "failure" },
			want: []string{"acme/gadget#12"},
		},
		{
			name: "checks pending",
			set:  func() { searchPRsChecks = "pending" },
			want: []string{"acme/widget#11"},
		},
		{
			name:   "milestone",
			set:    func() { searchPRsMilestone = "v2" },
			want:   []string{"acme/widget#10"},
			reject: []string{"acme/widget#11"},
		},
		{
			name: "closed date",
			set:  func() { searchPRsClosed = "2026-01-02" },
			want: []string{"acme/widget#10"},
		},
		{
			name: "commenter",
			set:  func() { searchPRsCommenter = "erin" },
			want: []string{"acme/widget#10", "acme/gadget#12"},
		},
		{
			name:   "no assignee",
			set:    func() { searchPRsNoAssignee = true },
			want:   []string{"acme/widget#11", "acme/gadget#12"},
			reject: []string{"acme/widget#10"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSearchTest(t)
			seedSearchCache(t)
			tc.set()
			out := runSearchPRsOutput(t)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output = %q, want %s", out, want)
				}
			}
			for _, reject := range tc.reject {
				if strings.Contains(out, reject) {
					t.Errorf("output = %q, must not contain %s", out, reject)
				}
			}
		})
	}
}

func TestSearchPRs_SortByCreated(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	searchPRsSort = "created"
	searchPRsOrder = "asc"

	out := runSearchPRsOutput(t)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d rows, want 3: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "acme/gadget#12") || !strings.Contains(lines[1], "acme/widget#10") || !strings.Contains(lines[2], "acme/widget#11") {
		t.Errorf("rows = %q, want gadget#12, widget#10, widget#11", lines)
	}
}

func TestSearch_JSONFieldSelection(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	searchIssuesJSON = "number,title,repository"

	out := runSearchIssuesOutput(t)
	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal JSON output %q: %v", out, err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if len(results[0]) != 3 {
		t.Errorf("first row has %d fields, want 3: %v", len(results[0]), results[0])
	}
	if _, ok := results[0]["isPullRequest"]; ok {
		t.Errorf("unselected field isPullRequest was emitted: %v", results[0])
	}

	searchIssuesJSON = "bogusField"
	if err := runSearchIssues(nil, nil); err == nil || !strings.Contains(err.Error(), "unknown JSON field") {
		t.Errorf("expected unknown JSON field error, got %v", err)
	}
}

func TestSearch_Template(t *testing.T) {
	setupSearchTest(t)
	seedSearchCache(t)
	searchIssuesTemplate = "{{.number}} {{.title}}"

	out := captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"login"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	want := "1 Fix login bug\n7 Login timeout\n"
	if out != want {
		t.Errorf("template output = %q, want %q", out, want)
	}

	searchIssuesTemplate = "{{.repository.nameWithOwner}}#{{.number}}"
	out = captureStdout(t, func() {
		if err := runSearchIssues(nil, []string{"login"}); err != nil {
			t.Fatalf("runSearchIssues: %v", err)
		}
	})
	want = "acme/widget#1\nacme/gadget#7\n"
	if out != want {
		t.Errorf("template repository output = %q, want %q", out, want)
	}
}

func TestSearch_InvalidFilterValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func()
		want string
	}{
		{name: "created", set: func() { searchIssuesCreated = "nope" }, want: "invalid --created"},
		{name: "comments", set: func() { searchIssuesComments = "many" }, want: "invalid --comments"},
		{name: "sort", set: func() { searchIssuesSort = "best-match" }, want: "invalid --sort"},
		{name: "order", set: func() { searchIssuesOrder = "sideways" }, want: "invalid --order"},
		{name: "match", set: func() { searchIssuesMatch = []string{"labels"} }, want: "invalid --match"},
		{name: "state", set: func() { searchIssuesState = "merged" }, want: "invalid --state"},
		{name: "checks", set: func() { searchPRsChecks = "maybe" }, want: "invalid --checks"},
		{name: "review", set: func() { searchPRsReview = "unknown" }, want: "invalid --review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSearchTest(t)
			seedSearchCache(t)
			tc.set()

			var err error
			if tc.name == "checks" || tc.name == "review" {
				err = runSearchPRs(nil, nil)
			} else {
				err = runSearchIssues(nil, nil)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}
