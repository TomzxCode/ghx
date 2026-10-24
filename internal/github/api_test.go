package github

import (
	"strings"
	"testing"
)

func TestIssueStates(t *testing.T) {
	cases := []struct{ in, want string }{
		{"open", "OPEN"},
		{"closed", "CLOSED"},
		{"all", "OPEN,CLOSED"},
		{"", "OPEN,CLOSED"},
	}
	for _, c := range cases {
		got := strings.Join(issueStates(c.in), ",")
		if got != c.want {
			t.Errorf("issueStates(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPRStates(t *testing.T) {
	cases := []struct{ in, want string }{
		{"open", "OPEN"},
		{"closed", "CLOSED"},
		{"merged", "MERGED"},
		{"all", "OPEN,CLOSED,MERGED"},
		{"", "OPEN,CLOSED,MERGED"},
	}
	for _, c := range cases {
		got := strings.Join(prStates(c.in), ",")
		if got != c.want {
			t.Errorf("prStates(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildIssueSearchQuery(t *testing.T) {
	cases := []struct {
		opts IssueListOptions
		want []string // substrings that must appear
	}{
		{
			IssueListOptions{State: "open", Author: "alice"},
			[]string{"repo:owner/repo", "is:issue", "is:open", "author:alice"},
		},
		{
			IssueListOptions{State: "closed", Assignee: "bob", Labels: []string{"bug", "p1"}},
			[]string{"is:closed", "assignee:bob", `label:"bug"`, `label:"p1"`},
		},
		{
			IssueListOptions{Milestone: "v2.0", Mention: "carol"},
			[]string{`milestone:"v2.0"`, "mentions:carol"},
		},
		{
			IssueListOptions{App: "my-bot", Search: "memory leak"},
			[]string{"author:app/my-bot", "memory leak"},
		},
		{
			IssueListOptions{State: "all"},
			[]string{"is:issue"},
			// "all" must NOT inject a state qualifier
		},
	}
	for _, c := range cases {
		q := buildIssueSearchQuery("owner", "repo", c.opts)
		for _, sub := range c.want {
			if !strings.Contains(q, sub) {
				t.Errorf("buildIssueSearchQuery: %q not found in %q", sub, q)
			}
		}
		if c.opts.State == "all" && (strings.Contains(q, "is:open") || strings.Contains(q, "is:closed")) {
			t.Errorf("buildIssueSearchQuery: state=all should not add is:open/is:closed, got %q", q)
		}
	}
}

func TestBuildPRSearchQuery(t *testing.T) {
	cases := []struct {
		opts PRListOptions
		want []string
	}{
		{
			PRListOptions{State: "open", Author: "alice", Base: "main"},
			[]string{"is:pr", "is:open", "author:alice", "base:main"},
		},
		{
			PRListOptions{State: "merged", Draft: true},
			[]string{"is:merged", "draft:true"},
		},
		{
			PRListOptions{Head: "feature-x", App: "ci-bot"},
			[]string{"head:feature-x", "author:app/ci-bot"},
		},
		{
			PRListOptions{State: "all"},
			[]string{"is:pr"},
		},
	}
	for _, c := range cases {
		q := buildPRSearchQuery("owner", "repo", c.opts)
		for _, sub := range c.want {
			if !strings.Contains(q, sub) {
				t.Errorf("buildPRSearchQuery: %q not found in %q", sub, q)
			}
		}
	}
}

// TestNodeToPRCarriesActorType asserts the GraphQL __typename of each actor is
// preserved in the normalized structs, which is what lets callers tell an app's
// account from a person's.
func TestNodeToPRCarriesActorType(t *testing.T) {
	n := &prNode{}
	n.Author = actorNode{Login: "dependabot", Typename: "Bot"}
	n.Comments.Nodes = []commentNode{
		{ID: "c1", Author: actorNode{Login: "services-db", Typename: "Bot"}},
	}
	n.Reviews.Nodes = []reviewNode{
		{Author: actorNode{Login: "graphite-app", Typename: "Bot"}, State: "COMMENTED"},
	}
	n.TimelineItems.Nodes = []timelineNode{
		{
			Typename:          "ReviewRequestedEvent",
			Actor:             actorNode{Login: "alice", Typename: "User"},
			RequestedReviewer: &actorNode{Login: "shopify-bumperbot", Typename: "Bot"},
		},
	}

	pr := nodeToPR(n)

	if got, want := pr.Author, (Actor{Login: "dependabot", Type: "Bot"}); got != want {
		t.Errorf("author = %+v, want %+v", got, want)
	}
	if got := pr.Comments[0].Author; got != (Actor{Login: "services-db", Type: "Bot"}) {
		t.Errorf("comment author = %+v, want Bot type", got)
	}
	if got := pr.Reviews[0].Author; got != (Actor{Login: "graphite-app", Type: "Bot"}) {
		t.Errorf("review author = %+v, want Bot type", got)
	}
	if len(pr.Timeline) != 1 {
		t.Fatalf("timeline length = %d, want 1", len(pr.Timeline))
	}
	if got := pr.Timeline[0].Actor.Type; got != "User" {
		t.Errorf("timeline actor type = %q, want User", got)
	}
	if got := pr.Timeline[0].RequestedReviewer; got != (Actor{Login: "shopify-bumperbot", Type: "Bot"}) {
		t.Errorf("requested reviewer = %+v, want Bot type", got)
	}
}

// TestQueriesRequestActorTypename guards the selections: without __typename on
// an actor the account type is unavailable no matter what the parser does.
func TestQueriesRequestActorTypename(t *testing.T) {
	queries := map[string]string{
		"listIssues":     listIssuesQuery,
		"getIssue":       getIssueQuery,
		"fetchAllIssues": fetchAllIssuesQuery,
		"listPRs":        listPRsQuery,
		"getPR":          getPRQuery,
		"fetchAllPRs":    fetchAllPRsQuery,
		"searchPRs":      searchPRsQuery,
		"searchIssues":   searchIssuesQuery,
	}
	for name, q := range queries {
		if !strings.Contains(q, "author { __typename login }") {
			t.Errorf("%s: author selection is missing __typename", name)
		}
	}
	if !strings.Contains(prTimelineSelection, "... on Bot { __typename login }") {
		t.Error("prTimelineSelection: requestedReviewer is missing the Bot fragment")
	}
}
