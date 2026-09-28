package cache

import (
	"strconv"
	"strings"

	"github.com/tomzxcode/ghx/internal/github"
)

// filterIssues returns the issues matching q. It is the canonical predicate
// implementation shared by every backend: the SQLite backend pushes the scalar
// predicates into SQL for indexed retrieval, then re-applies this filter to the
// candidate rows so the result set matches the file backend exactly.
//
// The API's mention/app filters are not representable from cached data and are
// therefore ignored, matching the previous in-memory behavior.
func filterIssues(issues []*github.Issue, q IssueQuery) []*github.Issue {
	var result []*github.Issue
	for _, issue := range issues {
		if q.State != "all" && q.State != "" {
			if !strings.EqualFold(issue.State, q.State) {
				continue
			}
		}
		if q.Assignee != "" {
			found := false
			for _, a := range issue.Assignees {
				if strings.EqualFold(a.Login, q.Assignee) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if q.Author != "" && !strings.EqualFold(issue.Author.Login, q.Author) {
			continue
		}
		if len(q.Labels) > 0 {
			if !hasAllLabels(issue.Labels, q.Labels) {
				continue
			}
		}
		if q.Milestone != "" {
			if issue.Milestone == nil {
				continue
			}
			if !strings.EqualFold(issue.Milestone.Title, q.Milestone) &&
				strconv.Itoa(issue.Milestone.Number) != q.Milestone {
				continue
			}
		}
		if q.Search != "" {
			s := strings.ToLower(q.Search)
			if !strings.Contains(strings.ToLower(issue.Title), s) &&
				!strings.Contains(strings.ToLower(issue.Body), s) {
				continue
			}
		}
		result = append(result, issue)
	}
	return result
}

// filterPRs returns the pull requests matching q, with the same semantics as
// filterIssues.
func filterPRs(prs []*github.PullRequest, q PRQuery) []*github.PullRequest {
	var result []*github.PullRequest
	for _, pr := range prs {
		if q.State != "all" && q.State != "" {
			if !strings.EqualFold(pr.State, q.State) {
				continue
			}
		}
		if q.Assignee != "" {
			found := false
			for _, a := range pr.Assignees {
				if strings.EqualFold(a.Login, q.Assignee) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if q.Author != "" && !strings.EqualFold(pr.Author.Login, q.Author) {
			continue
		}
		if len(q.Labels) > 0 {
			if !hasAllLabels(pr.Labels, q.Labels) {
				continue
			}
		}
		if q.BaseRef != "" && !strings.EqualFold(pr.BaseRefName, q.BaseRef) {
			continue
		}
		if q.HeadRef != "" && !strings.EqualFold(pr.HeadRefName, q.HeadRef) {
			continue
		}
		if q.Draft && !pr.IsDraft {
			continue
		}
		if q.Search != "" {
			s := strings.ToLower(q.Search)
			if !strings.Contains(strings.ToLower(pr.Title), s) &&
				!strings.Contains(strings.ToLower(pr.Body), s) {
				continue
			}
		}
		result = append(result, pr)
	}
	return result
}

// hasAllLabels reports whether wantLabels is a subset of haveLabels
// (case-insensitive).
func hasAllLabels(haveLabels []github.Label, wantLabels []string) bool {
	for _, want := range wantLabels {
		found := false
		for _, l := range haveLabels {
			if strings.EqualFold(l.Name, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
