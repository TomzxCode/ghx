package cache

import (
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

func makeIssues() []*github.Issue {
	now := time.Now()
	return []*github.Issue{
		{
			Number:    1,
			Title:     "Fix the memory leak",
			State:     "OPEN",
			Author:    github.Actor{Login: "alice"},
			Assignees: []github.Actor{{Login: "bob"}},
			Labels:    []github.Label{{Name: "bug"}, {Name: "p1"}},
			Milestone: &github.Milestone{Number: 1, Title: "v1.0"},
			UpdatedAt: now,
		},
		{
			Number:    2,
			Title:     "Add dark mode",
			State:     "CLOSED",
			Author:    github.Actor{Login: "carol"},
			Labels:    []github.Label{{Name: "enhancement"}},
			UpdatedAt: now.Add(-time.Hour),
		},
		{
			Number:    3,
			Title:     "Memory leak follow-up",
			State:     "OPEN",
			Author:    github.Actor{Login: "alice"},
			Labels:    []github.Label{{Name: "bug"}},
			UpdatedAt: now.Add(-2 * time.Hour),
		},
	}
}

func TestFilterIssues_State(t *testing.T) {
	issues := makeIssues()

	open := filterIssues(issues, IssueQuery{State: "open"})
	if len(open) != 2 {
		t.Errorf("open: got %d, want 2", len(open))
	}

	closed := filterIssues(issues, IssueQuery{State: "closed"})
	if len(closed) != 1 {
		t.Errorf("closed: got %d, want 1", len(closed))
	}

	all := filterIssues(issues, IssueQuery{State: "all"})
	if len(all) != 3 {
		t.Errorf("all: got %d, want 3", len(all))
	}
}

func TestFilterIssues_Author(t *testing.T) {
	issues := makeIssues()
	got := filterIssues(issues, IssueQuery{State: "all", Author: "alice"})
	if len(got) != 2 {
		t.Errorf("author=alice: got %d, want 2", len(got))
	}
}

func TestFilterIssues_Assignee(t *testing.T) {
	issues := makeIssues()
	got := filterIssues(issues, IssueQuery{State: "all", Assignee: "bob"})
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("assignee=bob: got %v", got)
	}
}

func TestFilterIssues_Labels(t *testing.T) {
	issues := makeIssues()

	bugOnly := filterIssues(issues, IssueQuery{State: "all", Labels: []string{"bug"}})
	if len(bugOnly) != 2 {
		t.Errorf("label=bug: got %d, want 2", len(bugOnly))
	}

	// must have BOTH labels
	bugAndP1 := filterIssues(issues, IssueQuery{State: "all", Labels: []string{"bug", "p1"}})
	if len(bugAndP1) != 1 || bugAndP1[0].Number != 1 {
		t.Errorf("label=bug+p1: got %v", bugAndP1)
	}
}

func TestFilterIssues_Milestone(t *testing.T) {
	issues := makeIssues()

	byTitle := filterIssues(issues, IssueQuery{State: "all", Milestone: "v1.0"})
	if len(byTitle) != 1 || byTitle[0].Number != 1 {
		t.Errorf("milestone=v1.0 (title): got %v", byTitle)
	}

	byNumber := filterIssues(issues, IssueQuery{State: "all", Milestone: "1"})
	if len(byNumber) != 1 || byNumber[0].Number != 1 {
		t.Errorf("milestone=1 (number): got %v", byNumber)
	}
}

func TestFilterIssues_Search(t *testing.T) {
	issues := makeIssues()

	got := filterIssues(issues, IssueQuery{State: "all", Search: "memory"})
	if len(got) != 2 {
		t.Errorf("search=memory: got %d, want 2", len(got))
	}
}

// ---------------------------------------------------------------------------

func makePRs() []*github.PullRequest {
	now := time.Now()
	return []*github.PullRequest{
		{
			Number:      10,
			Title:       "Add login feature",
			State:       "OPEN",
			IsDraft:     false,
			Author:      github.Actor{Login: "alice"},
			Assignees:   []github.Actor{{Login: "dave"}},
			Labels:      []github.Label{{Name: "feature"}},
			BaseRefName: "main",
			HeadRefName: "feat/login",
			UpdatedAt:   now,
		},
		{
			Number:      11,
			Title:       "Fix crash on startup",
			State:       "MERGED",
			IsDraft:     false,
			Author:      github.Actor{Login: "bob"},
			BaseRefName: "main",
			HeadRefName: "fix/startup",
			UpdatedAt:   now.Add(-time.Hour),
		},
		{
			Number:      12,
			Title:       "WIP: refactor auth",
			State:       "OPEN",
			IsDraft:     true,
			Author:      github.Actor{Login: "alice"},
			BaseRefName: "main",
			HeadRefName: "wip/auth",
			UpdatedAt:   now.Add(-2 * time.Hour),
		},
	}
}

func TestFilterPRs_State(t *testing.T) {
	prs := makePRs()

	open := filterPRs(prs, PRQuery{State: "open"})
	if len(open) != 2 {
		t.Errorf("state=open: got %d, want 2", len(open))
	}

	merged := filterPRs(prs, PRQuery{State: "merged"})
	if len(merged) != 1 || merged[0].Number != 11 {
		t.Errorf("state=merged: got %v", merged)
	}
}

func TestFilterPRs_Author(t *testing.T) {
	prs := makePRs()
	got := filterPRs(prs, PRQuery{State: "all", Author: "alice"})
	if len(got) != 2 {
		t.Errorf("author=alice: got %d, want 2", len(got))
	}
}

func TestFilterPRs_Draft(t *testing.T) {
	prs := makePRs()
	got := filterPRs(prs, PRQuery{State: "all", Draft: true})
	if len(got) != 1 || got[0].Number != 12 {
		t.Errorf("draft: got %v", got)
	}
}

func TestFilterPRs_Base(t *testing.T) {
	prs := makePRs()
	got := filterPRs(prs, PRQuery{State: "all", BaseRef: "main"})
	if len(got) != 3 {
		t.Errorf("base=main: got %d, want 3", len(got))
	}
}

func TestFilterPRs_Head(t *testing.T) {
	prs := makePRs()
	got := filterPRs(prs, PRQuery{State: "all", HeadRef: "feat/login"})
	if len(got) != 1 || got[0].Number != 10 {
		t.Errorf("head=feat/login: got %v", got)
	}
}

func TestFilterPRs_Search(t *testing.T) {
	prs := makePRs()
	got := filterPRs(prs, PRQuery{State: "all", Search: "crash"})
	if len(got) != 1 || got[0].Number != 11 {
		t.Errorf("search=crash: got %v", got)
	}
}
