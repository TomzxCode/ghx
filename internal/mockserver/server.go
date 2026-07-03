package mockserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

type gqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type gqlError struct {
	Message string `json:"message"`
}

type gqlResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Errors []gqlError  `json:"errors,omitempty"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor,omitempty"`
}

type Server struct {
	mu       sync.RWMutex
	scenario *Scenario
	server   *httptest.Server
}

func NewServer(scenario *Scenario) *Server {
	s := &Server{
		scenario: scenario,
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handleGraphQL))
	return s
}

func (s *Server) URL() string {
	return s.server.URL
}

func (s *Server) Close() {
	s.server.Close()
}

// UpdateScenario atomically replaces the scenario data. Use this to simulate
// activity over time (e.g. new issues, comments, merges) while the server is running.
func (s *Server) UpdateScenario(fn func(scenario *Scenario)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.scenario)
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req gqlRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "parse request", http.StatusBadRequest)
		return
	}

	if r.Header.Get("Authorization") == "" {
		writeGQL(w, nil, []gqlError{{Message: "unauthorized"}})
		return
	}

	s.mu.RLock()
	data, errs := s.route(req.Query, req.Variables)
	s.mu.RUnlock()

	writeGQL(w, data, errs)
}

func (s *Server) route(query string, vars map[string]interface{}) (interface{}, []gqlError) {
	switch {
	case strings.Contains(query, "issue(number:"):
		return s.getIssue(vars)
	case strings.Contains(query, "pullRequest(number:"):
		return s.getPR(vars)
	case strings.Contains(query, "search(query:"):
		return s.search(vars)
	case strings.Contains(query, "issues(first:") && strings.Contains(query, "states: [OPEN, CLOSED]"):
		return s.fetchAllIssues(vars)
	case strings.Contains(query, "issues(first:"):
		return s.listIssues(vars)
	case strings.Contains(query, "pullRequests(first: 100") && strings.Contains(query, "states: [OPEN, CLOSED, MERGED]"):
		return s.fetchAllPRs(vars)
	case strings.Contains(query, "pullRequests(first:"):
		return s.listPRs(vars)
	default:
		preview := query
		if len(preview) > 80 {
			preview = preview[:80]
		}
		return nil, []gqlError{{Message: "unrecognized query: " + preview}}
	}
}

func (s *Server) getIssue(vars map[string]interface{}) (interface{}, []gqlError) {
	owner := strVar(vars, "owner")
	repo := strVar(vars, "repo")
	number := intVar(vars, "number")

	for i := range s.scenario.Issues {
		si := &s.scenario.Issues[i]
		if si.Owner == owner && si.Repo == repo && si.Number == number {
			return map[string]interface{}{
				"repository": map[string]interface{}{
					"issue": issueToNodeFull(&si.Issue),
				},
			}, nil
		}
	}
	return nil, []gqlError{{Message: fmt.Sprintf("issue #%d not found", number)}}
}

func (s *Server) getPR(vars map[string]interface{}) (interface{}, []gqlError) {
	owner := strVar(vars, "owner")
	repo := strVar(vars, "repo")
	number := intVar(vars, "number")

	for i := range s.scenario.PRs {
		sp := &s.scenario.PRs[i]
		if sp.Owner == owner && sp.Repo == repo && sp.Number == number {
			return map[string]interface{}{
				"repository": map[string]interface{}{
					"pullRequest": prToNodeFull(&sp.PullRequest),
				},
			}, nil
		}
	}
	return nil, []gqlError{{Message: fmt.Sprintf("pull request #%d not found", number)}}
}

func (s *Server) listIssues(vars map[string]interface{}) (interface{}, []gqlError) {
	owner := strVar(vars, "owner")
	repo := strVar(vars, "repo")
	first := intVar(vars, "first")
	states := statesVar(vars, "states")

	filtered := s.filterIssues(owner, repo, states, vars, "since")
	return s.paginateNodes(filtered, first, vars, func(i int) interface{} {
		return issueToNodeSummary(&s.scenario.Issues[filtered[i]].Issue)
	}), nil
}

func (s *Server) fetchAllIssues(vars map[string]interface{}) (interface{}, []gqlError) {
	owner := strVar(vars, "owner")
	repo := strVar(vars, "repo")

	filtered := s.filterIssues(owner, repo, nil, vars, "since")
	sortByUpdatedAtAsc(filtered, func(i int) time.Time { return s.scenario.Issues[filtered[i]].Issue.UpdatedAt })
	return s.paginateNodes(filtered, 100, vars, func(i int) interface{} {
		return issueToNodeFull(&s.scenario.Issues[filtered[i]].Issue)
	}), nil
}

func (s *Server) listPRs(vars map[string]interface{}) (interface{}, []gqlError) {
	owner := strVar(vars, "owner")
	repo := strVar(vars, "repo")
	first := intVar(vars, "first")
	states := statesVar(vars, "states")

	filtered := s.filterPRs(owner, repo, states)
	return s.paginatePRs(filtered, first, vars), nil
}

func (s *Server) fetchAllPRs(vars map[string]interface{}) (interface{}, []gqlError) {
	owner := strVar(vars, "owner")
	repo := strVar(vars, "repo")

	filtered := s.filterPRs(owner, repo, nil)
	sortByUpdatedAtAsc(filtered, func(i int) time.Time { return s.scenario.PRs[filtered[i]].PullRequest.UpdatedAt })
	return s.paginatePRsFull(filtered, 100, vars), nil
}

func (s *Server) search(vars map[string]interface{}) (interface{}, []gqlError) {
	q := strVar(vars, "query")
	first := intVar(vars, "first")

	owner, repo := parseRepoFromQuery(q)
	isPR := strings.Contains(q, "is:pr")
	isIssue := strings.Contains(q, "is:issue")

	var nodes []interface{}

	if isIssue || !isPR {
		for i := range s.scenario.Issues {
			si := &s.scenario.Issues[i]
			if si.Owner == owner && si.Repo == repo && matchSearchFilters(&si.Issue, q) {
				nodes = append(nodes, searchNodeFromIssue(&si.Issue))
			}
		}
	}

	if isPR {
		for i := range s.scenario.PRs {
			sp := &s.scenario.PRs[i]
			if sp.Owner == owner && sp.Repo == repo && matchSearchFiltersPR(&sp.PullRequest, q) {
				nodes = append(nodes, searchNodeFromPR(&sp.PullRequest))
			}
		}
	}

	after := intVar(vars, "after")
	end := after + first
	if end > len(nodes) {
		end = len(nodes)
	}
	hasNext := end < len(nodes)

	var cursor string
	if hasNext {
		cursor = encodeCursor(end)
	}

	return map[string]interface{}{
		"search": map[string]interface{}{
			"pageInfo": pageInfo{HasNextPage: hasNext, EndCursor: cursor},
			"nodes":    nodes[after:end],
		},
	}, nil
}

func (s *Server) filterIssues(owner, repo string, states []string, vars map[string]interface{}, sinceKey string) []int {
	var indices []int
	for i := range s.scenario.Issues {
		si := &s.scenario.Issues[i]
		if si.Owner != owner || si.Repo != repo {
			continue
		}
		if !inStates(si.State, states) {
			continue
		}
		if sinceStr, ok := vars[sinceKey].(string); ok && sinceStr != "" {
			since, err := time.Parse(time.RFC3339, sinceStr)
			if err == nil && si.UpdatedAt.Before(since) {
				continue
			}
		}
		indices = append(indices, i)
	}
	return indices
}

func (s *Server) filterPRs(owner, repo string, states []string) []int {
	var indices []int
	for i := range s.scenario.PRs {
		sp := &s.scenario.PRs[i]
		if sp.Owner != owner || sp.Repo != repo {
			continue
		}
		if !inStates(sp.State, states) {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

func (s *Server) paginateNodes(indices []int, pageSize int, vars map[string]interface{}, toNode func(int) interface{}) interface{} {
	start := intVar(vars, "after")
	end := start + pageSize
	if end > len(indices) {
		end = len(indices)
	}
	hasNext := end < len(indices)

	nodes := make([]interface{}, 0, end-start)
	for i := start; i < end; i++ {
		nodes = append(nodes, toNode(i))
	}

	var nextCursor string
	if hasNext {
		nextCursor = encodeCursor(end)
	}

	return map[string]interface{}{
		"repository": map[string]interface{}{
			"issues": map[string]interface{}{
				"totalCount": len(indices),
				"pageInfo":   pageInfo{HasNextPage: hasNext, EndCursor: nextCursor},
				"nodes":      nodes,
			},
		},
	}
}

func (s *Server) paginatePRs(indices []int, pageSize int, vars map[string]interface{}) interface{} {
	start := intVar(vars, "after")
	end := start + pageSize
	if end > len(indices) {
		end = len(indices)
	}
	hasNext := end < len(indices)

	nodes := make([]interface{}, 0, end-start)
	for i := start; i < end; i++ {
		nodes = append(nodes, prToNodeSummary(&s.scenario.PRs[indices[i]].PullRequest))
	}

	var nextCursor string
	if hasNext {
		nextCursor = encodeCursor(end)
	}

	return map[string]interface{}{
		"repository": map[string]interface{}{
			"pullRequests": map[string]interface{}{
				"totalCount": len(indices),
				"pageInfo":   pageInfo{HasNextPage: hasNext, EndCursor: nextCursor},
				"nodes":      nodes,
			},
		},
	}
}

func (s *Server) paginatePRsFull(indices []int, pageSize int, vars map[string]interface{}) interface{} {
	start := intVar(vars, "after")
	end := start + pageSize
	if end > len(indices) {
		end = len(indices)
	}
	hasNext := end < len(indices)

	nodes := make([]interface{}, 0, end-start)
	for i := start; i < end; i++ {
		nodes = append(nodes, prToNodeFull(&s.scenario.PRs[indices[i]].PullRequest))
	}

	var nextCursor string
	if hasNext {
		nextCursor = encodeCursor(end)
	}

	return map[string]interface{}{
		"repository": map[string]interface{}{
			"pullRequests": map[string]interface{}{
				"totalCount": len(indices),
				"pageInfo":   pageInfo{HasNextPage: hasNext, EndCursor: nextCursor},
				"nodes":      nodes,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Node conversion helpers (produce maps matching GitHub GraphQL JSON shape)
// ---------------------------------------------------------------------------

func actorNode(login string) map[string]interface{} {
	return map[string]interface{}{"login": login}
}

func assigneesNode(assignees []github.Actor) map[string]interface{} {
	nodes := make([]interface{}, len(assignees))
	for i, a := range assignees {
		nodes[i] = actorNode(a.Login)
	}
	return map[string]interface{}{"nodes": nodes}
}

func labelsNode(labels []github.Label) map[string]interface{} {
	nodes := make([]interface{}, len(labels))
	for i, l := range labels {
		nodes[i] = map[string]interface{}{"name": l.Name, "color": l.Color}
	}
	return map[string]interface{}{"nodes": nodes}
}

func milestoneNode(m *github.Milestone) interface{} {
	if m == nil {
		return nil
	}
	return map[string]interface{}{"number": m.Number, "title": m.Title}
}

func commentNodes(comments []github.Comment) map[string]interface{} {
	nodes := make([]interface{}, len(comments))
	for i, c := range comments {
		nodes[i] = map[string]interface{}{
			"id":        c.ID,
			"author":    actorNode(c.Author.Login),
			"body":      c.Body,
			"createdAt": c.CreatedAt,
			"updatedAt": c.UpdatedAt,
			"url":       c.URL,
		}
	}
	return map[string]interface{}{
		"totalCount": len(comments),
		"nodes":      nodes,
	}
}

func commentsSummary(count int) map[string]interface{} {
	return map[string]interface{}{
		"totalCount": count,
		"nodes":      []interface{}{},
	}
}

func issueToNodeSummary(issue *github.Issue) map[string]interface{} {
	commentCount := issue.CommentCount
	if commentCount == 0 {
		commentCount = len(issue.Comments)
	}
	return map[string]interface{}{
		"number":    issue.Number,
		"title":     issue.Title,
		"state":     issue.State,
		"author":    actorNode(issue.Author.Login),
		"assignees": assigneesNode(issue.Assignees),
		"labels":    labelsNode(issue.Labels),
		"milestone": milestoneNode(issue.Milestone),
		"createdAt": issue.CreatedAt,
		"updatedAt": issue.UpdatedAt,
		"closedAt":  issue.ClosedAt,
		"url":       issue.URL,
		"body":      issue.Body,
		"comments":  commentsSummary(commentCount),
	}
}

func issueToNodeFull(issue *github.Issue) map[string]interface{} {
	return map[string]interface{}{
		"number":    issue.Number,
		"title":     issue.Title,
		"state":     issue.State,
		"author":    actorNode(issue.Author.Login),
		"assignees": assigneesNode(issue.Assignees),
		"labels":    labelsNode(issue.Labels),
		"milestone": milestoneNode(issue.Milestone),
		"createdAt": issue.CreatedAt,
		"updatedAt": issue.UpdatedAt,
		"closedAt":  issue.ClosedAt,
		"url":       issue.URL,
		"body":      issue.Body,
		"comments":  commentNodes(issue.Comments),
	}
}

func prToNodeSummary(pr *github.PullRequest) map[string]interface{} {
	commentCount := pr.CommentCount
	if commentCount == 0 {
		commentCount = len(pr.Comments)
	}
	return map[string]interface{}{
		"number":         pr.Number,
		"title":          pr.Title,
		"state":          pr.State,
		"isDraft":        pr.IsDraft,
		"reviewDecision": pr.ReviewDecision,
		"author":         actorNode(pr.Author.Login),
		"assignees":      assigneesNode(pr.Assignees),
		"labels":         labelsNode(pr.Labels),
		"milestone":      milestoneNode(pr.Milestone),
		"baseRefName":    pr.BaseRefName,
		"headRefName":    pr.HeadRefName,
		"createdAt":      pr.CreatedAt,
		"updatedAt":      pr.UpdatedAt,
		"mergedAt":       pr.MergedAt,
		"closedAt":       pr.ClosedAt,
		"url":            pr.URL,
		"body":           pr.Body,
		"comments":       commentsSummary(commentCount),
	}
}

func prToNodeFull(pr *github.PullRequest) map[string]interface{} {
	return map[string]interface{}{
		"number":         pr.Number,
		"title":          pr.Title,
		"state":          pr.State,
		"isDraft":        pr.IsDraft,
		"reviewDecision": pr.ReviewDecision,
		"author":         actorNode(pr.Author.Login),
		"assignees":      assigneesNode(pr.Assignees),
		"labels":         labelsNode(pr.Labels),
		"milestone":      milestoneNode(pr.Milestone),
		"baseRefName":    pr.BaseRefName,
		"headRefName":    pr.HeadRefName,
		"createdAt":      pr.CreatedAt,
		"updatedAt":      pr.UpdatedAt,
		"mergedAt":       pr.MergedAt,
		"closedAt":       pr.ClosedAt,
		"url":            pr.URL,
		"body":           pr.Body,
		"comments":       commentNodes(pr.Comments),
	}
}

func searchNodeFromIssue(issue *github.Issue) map[string]interface{} {
	commentCount := issue.CommentCount
	if commentCount == 0 {
		commentCount = len(issue.Comments)
	}
	return map[string]interface{}{
		"__typename": "Issue",
		"number":     issue.Number,
		"title":      issue.Title,
		"state":      issue.State,
		"author":     actorNode(issue.Author.Login),
		"assignees":  assigneesNode(issue.Assignees),
		"labels":     labelsNode(issue.Labels),
		"milestone":  milestoneNode(issue.Milestone),
		"createdAt":  issue.CreatedAt,
		"updatedAt":  issue.UpdatedAt,
		"closedAt":   issue.ClosedAt,
		"url":        issue.URL,
		"body":       issue.Body,
		"comments":   commentsSummary(commentCount),
	}
}

func searchNodeFromPR(pr *github.PullRequest) map[string]interface{} {
	commentCount := pr.CommentCount
	if commentCount == 0 {
		commentCount = len(pr.Comments)
	}
	return map[string]interface{}{
		"__typename":     "PullRequest",
		"number":         pr.Number,
		"title":          pr.Title,
		"state":          pr.State,
		"isDraft":        pr.IsDraft,
		"reviewDecision": pr.ReviewDecision,
		"author":         actorNode(pr.Author.Login),
		"assignees":      assigneesNode(pr.Assignees),
		"labels":         labelsNode(pr.Labels),
		"milestone":      milestoneNode(pr.Milestone),
		"baseRefName":    pr.BaseRefName,
		"headRefName":    pr.HeadRefName,
		"createdAt":      pr.CreatedAt,
		"updatedAt":      pr.UpdatedAt,
		"mergedAt":       pr.MergedAt,
		"closedAt":       pr.ClosedAt,
		"url":            pr.URL,
		"body":           pr.Body,
		"comments":       commentsSummary(commentCount),
	}
}

// ---------------------------------------------------------------------------
// Search query matching
// ---------------------------------------------------------------------------

func matchSearchFilters(issue *github.Issue, q string) bool {
	for _, part := range strings.Fields(q) {
		switch {
		case strings.HasPrefix(part, "repo:"):
		case strings.HasPrefix(part, "sort:"):
		case part == "is:issue":
		case strings.HasPrefix(part, "updated:>="):
			if d, ok := parseUpdatedSince(part); ok && issue.UpdatedAt.Before(d) {
				return false
			}
		case part == "is:open":
			if !strings.EqualFold(issue.State, "OPEN") {
				return false
			}
		case part == "is:closed":
			if !strings.EqualFold(issue.State, "CLOSED") {
				return false
			}
		case strings.HasPrefix(part, "author:"):
			a := strings.TrimPrefix(part, "author:")
			if !strings.EqualFold(issue.Author.Login, a) {
				return false
			}
		case strings.HasPrefix(part, "assignee:"):
			a := strings.TrimPrefix(part, "assignee:")
			found := false
			for _, as := range issue.Assignees {
				if strings.EqualFold(as.Login, a) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case strings.HasPrefix(part, "label:"):
			label := strings.Trim(part, `label:"`)
			found := false
			for _, l := range issue.Labels {
				if strings.EqualFold(l.Name, label) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func matchSearchFiltersPR(pr *github.PullRequest, q string) bool {
	for _, part := range strings.Fields(q) {
		switch {
		case strings.HasPrefix(part, "repo:"):
		case strings.HasPrefix(part, "sort:"):
		case part == "is:pr":
		case strings.HasPrefix(part, "updated:>="):
			if d, ok := parseUpdatedSince(part); ok && pr.UpdatedAt.Before(d) {
				return false
			}
		case part == "is:open":
			if !strings.EqualFold(pr.State, "OPEN") {
				return false
			}
		case part == "is:closed":
			if !strings.EqualFold(pr.State, "CLOSED") {
				return false
			}
		case part == "is:merged":
			if !strings.EqualFold(pr.State, "MERGED") {
				return false
			}
		case strings.HasPrefix(part, "author:"):
			a := strings.TrimPrefix(part, "author:")
			if !strings.EqualFold(pr.Author.Login, a) {
				return false
			}
		case strings.HasPrefix(part, "assignee:"):
			a := strings.TrimPrefix(part, "assignee:")
			found := false
			for _, as := range pr.Assignees {
				if strings.EqualFold(as.Login, a) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case strings.HasPrefix(part, "label:"):
			label := strings.Trim(part, `label:"`)
			found := false
			for _, l := range pr.Labels {
				if strings.EqualFold(l.Name, label) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case part == "draft:true":
			if !pr.IsDraft {
				return false
			}
		case strings.HasPrefix(part, "base:"):
			b := strings.TrimPrefix(part, "base:")
			if !strings.EqualFold(pr.BaseRefName, b) {
				return false
			}
		case strings.HasPrefix(part, "head:"):
			h := strings.TrimPrefix(part, "head:")
			if !strings.EqualFold(pr.HeadRefName, h) {
				return false
			}
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strVar(vars map[string]interface{}, key string) string {
	v, _ := vars[key].(string)
	return v
}

func intVar(vars map[string]interface{}, key string) int {
	switch v := vars[key].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func statesVar(vars map[string]interface{}, key string) []string {
	raw, ok := vars[key]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	states := make([]string, len(arr))
	for i, v := range arr {
		states[i] = fmt.Sprintf("%v", v)
	}
	return states
}

func inStates(state string, states []string) bool {
	if len(states) == 0 {
		return true
	}
	for _, s := range states {
		if strings.EqualFold(state, s) {
			return true
		}
	}
	return false
}

func parseRepoFromQuery(q string) (string, string) {
	for _, part := range strings.Fields(q) {
		if strings.HasPrefix(part, "repo:") {
			rp := strings.TrimPrefix(part, "repo:")
			parts := strings.SplitN(rp, "/", 2)
			if len(parts) == 2 {
				return parts[0], parts[1]
			}
		}
	}
	return "", ""
}

func encodeCursor(offset int) string {
	return strconv.Itoa(offset)
}

// parseUpdatedSince parses an `updated:>=YYYY-MM-DD` search qualifier into the
// start of that day (UTC). ok is false when the value cannot be parsed.
func parseUpdatedSince(part string) (time.Time, bool) {
	raw := strings.TrimPrefix(part, "updated:>=")
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// sortByUpdatedAtAsc sorts indices in place by the updatedAt timestamp returned
// by key (key receives a position in indices), oldest-first. This mirrors the
// ASC ordering requested by the fetchAll queries, so multi-page fetches resume
// correctly.
func sortByUpdatedAtAsc(indices []int, key func(pos int) time.Time) {
	sort.Slice(indices, func(i, j int) bool {
		return key(i).Before(key(j))
	})
}

func writeGQL(w http.ResponseWriter, data interface{}, errs []gqlError) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(gqlResponse{Data: data, Errors: errs})
}
