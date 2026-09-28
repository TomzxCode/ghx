package cache

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tomzxcode/ghx/internal/github"
)

const (
	qHost  = "github.com"
	qOwner = "acme"
	qRepo  = "big"
)

// seedQueryFixture writes a deterministic, varied set of issues and PRs into a
// backend so query behavior can be compared across backends.
func seedQueryFixture(t *testing.T, s Store) {
	t.Helper()
	for i := 1; i <= 12; i++ {
		state := "OPEN"
		if i%2 == 0 {
			state = "CLOSED"
		}
		author := "alice"
		if i%3 == 0 {
			author = "bob"
		}
		var milestone *github.Milestone
		if i%4 == 0 {
			milestone = &github.Milestone{Number: 1, Title: "v1.0"}
		}
		var assignees []github.Actor
		if i%2 == 0 {
			assignees = []github.Actor{{Login: "carol"}}
		}
		var labels []github.Label
		if i%2 == 0 {
			labels = append(labels, github.Label{Name: "bug"})
		}
		if i%5 == 0 {
			labels = append(labels, github.Label{Name: "area-x"})
		}
		issue := &github.Issue{
			Number:    i,
			Title:     fmt.Sprintf("widget issue %d", i),
			State:     state,
			Author:    github.Actor{Login: author},
			Assignees: assignees,
			Labels:    labels,
			Milestone: milestone,
			Body:      "about the thing",
			CreatedAt: utc(2024, 1, 1, 0, 0),
			UpdatedAt: utc(2024, 1, 1, 0, 0),
		}
		if err := s.SaveIssue(qHost, qOwner, qRepo, issue); err != nil {
			t.Fatalf("SaveIssue(%d): %v", i, err)
		}
	}

	for i := 1; i <= 8; i++ {
		state := "OPEN"
		if i%2 == 0 {
			state = "MERGED"
		}
		author := "alice"
		if i%2 == 0 {
			author = "bob"
		}
		base := "main"
		if i%3 == 0 {
			base = "develop"
		}
		pr := &github.PullRequest{
			Number:      100 + i,
			Title:       fmt.Sprintf("widget pr %d", i),
			State:       state,
			IsDraft:     i%4 == 0,
			Author:      github.Actor{Login: author},
			BaseRefName: base,
			HeadRefName: fmt.Sprintf("branch-%d", i),
			Body:        "about the thing",
			CreatedAt:   utc(2024, 1, 1, 0, 0),
			UpdatedAt:   utc(2024, 1, 1, 0, 0),
		}
		if err := s.SavePR(qHost, qOwner, qRepo, pr); err != nil {
			t.Fatalf("SavePR(%d): %v", pr.Number, err)
		}
	}
}

// TestQueryEquivalence asserts the SQLite query path returns the same set as the
// file backend for a matrix of issue and PR queries (FR-03, FR-06, NFR-03).
func TestQueryEquivalence(t *testing.T) {
	file := NewStoreWithPath(t.TempDir())
	sq, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sq.Close()
	seedQueryFixture(t, file)
	seedQueryFixture(t, sq)

	issueQueries := []IssueQuery{
		{},
		{State: "open"},
		{State: "closed"},
		{State: "all"},
		{State: "all", Author: "alice"},
		{State: "all", Author: "bob"},
		{State: "all", Assignee: "carol"},
		{State: "all", Labels: []string{"bug"}},
		{State: "all", Labels: []string{"bug", "area-x"}},
		{State: "all", Milestone: "v1.0"},
		{State: "all", Milestone: "1"},
		{State: "all", Search: "widget"},
		{State: "all", Search: "thing"},
		{State: "open", Author: "alice", Labels: []string{"bug"}},
		{State: "closed", Labels: []string{"bug", "area-x"}, Search: "widget"},
	}
	for _, q := range issueQueries {
		want, err := file.QueryIssues(qHost, qOwner, qRepo, q)
		if err != nil {
			t.Fatalf("file QueryIssues(%+v): %v", q, err)
		}
		got, err := sq.QueryIssues(qHost, qOwner, qRepo, q)
		if err != nil {
			t.Fatalf("sqlite QueryIssues(%+v): %v", q, err)
		}
		compareIssueSlices(t, q, want, got)
	}

	prQueries := []PRQuery{
		{},
		{State: "open"},
		{State: "merged"},
		{State: "all"},
		{State: "all", Author: "alice"},
		{State: "all", Author: "bob"},
		{State: "all", BaseRef: "main"},
		{State: "all", BaseRef: "develop"},
		{State: "all", HeadRef: "branch-3"},
		{State: "all", Draft: true},
		{State: "all", Search: "widget"},
		{State: "all", Search: "thing"},
		{State: "open", Author: "bob", BaseRef: "main", Search: "widget"},
	}
	for _, q := range prQueries {
		want, err := file.QueryPRs(qHost, qOwner, qRepo, q)
		if err != nil {
			t.Fatalf("file QueryPRs(%+v): %v", q, err)
		}
		got, err := sq.QueryPRs(qHost, qOwner, qRepo, q)
		if err != nil {
			t.Fatalf("sqlite QueryPRs(%+v): %v", q, err)
		}
		comparePRSlices(t, q, want, got)
	}
}

func compareIssueSlices(t *testing.T, q IssueQuery, want, got []*github.Issue) {
	t.Helper()
	wm := map[int]*github.Issue{}
	for _, x := range want {
		wm[x.Number] = x
	}
	gm := map[int]*github.Issue{}
	for _, x := range got {
		gm[x.Number] = x
	}
	if len(wm) != len(gm) {
		t.Errorf("issue q=%+v: size want %d got %d (%v vs %v)", q, len(wm), len(gm), keysI(wm), keysI(gm))
		return
	}
	for n, w := range wm {
		g, ok := gm[n]
		if !ok {
			t.Errorf("issue q=%+v: missing #%d", q, n)
			continue
		}
		if !reflect.DeepEqual(w, g) {
			t.Errorf("issue q=%+v: #%d differs\nwant %+v\ngot  %+v", q, n, w, g)
		}
	}
}

func comparePRSlices(t *testing.T, q PRQuery, want, got []*github.PullRequest) {
	t.Helper()
	wm := map[int]*github.PullRequest{}
	for _, x := range want {
		wm[x.Number] = x
	}
	gm := map[int]*github.PullRequest{}
	for _, x := range got {
		gm[x.Number] = x
	}
	if len(wm) != len(gm) {
		t.Errorf("pr q=%+v: size want %d got %d", q, len(wm), len(gm))
		return
	}
	for n, w := range wm {
		g, ok := gm[n]
		if !ok {
			t.Errorf("pr q=%+v: missing #%d", q, n)
			continue
		}
		if !reflect.DeepEqual(w, g) {
			t.Errorf("pr q=%+v: #%d differs\nwant %+v\ngot  %+v", q, n, w, g)
		}
	}
}

func keysI(m map[int]*github.Issue) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestSQLiteQueryUsesIndex asserts the state-filtered query is served by the
// indexed column rather than a full scan (FR-03).
func TestSQLiteQueryUsesIndex(t *testing.T) {
	sq, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sq.Close()
	s := sq.(*sqliteStore)

	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT number FROM issues
		WHERE host=? AND owner=? AND repo=? AND state=? COLLATE NOCASE`,
		qHost, qOwner, qRepo, "OPEN")
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	// FR-03 requires an indexed lookup rather than a full scan. SQLite may pick
	// either the composite primary-key index or a secondary index; both satisfy
	// the requirement, so accept any index and reject a bare table scan.
	planStr := plan.String()
	if strings.Contains(planStr, "SCAN issues") && !strings.Contains(planStr, "USING") {
		t.Errorf("expected an indexed lookup, got a full scan, plan:\n%s", planStr)
	}
	if !strings.Contains(planStr, "USING INDEX") && !strings.Contains(planStr, "USING COVERING INDEX") {
		t.Errorf("expected an index to be used, plan:\n%s", planStr)
	}
}
