package github

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

// IssueListOptions carries filtering options for listing issues.
type IssueListOptions struct {
	Limit     int
	State     string // open | closed | all
	Assignee  string
	Author    string
	Labels    []string
	Milestone string
	Mention   string
	Search    string
	App       string
}

// PRListOptions carries filtering options for listing pull requests.
type PRListOptions struct {
	Limit    int
	State    string // open | closed | merged | all
	Assignee string
	Author   string
	Labels   []string
	Base     string
	Head     string
	Draft    bool
	Search   string
	App      string
}

// ---------------------------------------------------------------------------
// Internal API response node types
// ---------------------------------------------------------------------------

type actorNode struct {
	Login string `json:"login"`
}

type labelNode struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type milestoneNode struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type commentNode struct {
	ID        string    `json:"id"`
	Author    actorNode `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

type issueNode struct {
	Number    int                         `json:"number"`
	Title     string                      `json:"title"`
	State     string                      `json:"state"`
	Author    actorNode                   `json:"author"`
	Assignees struct{ Nodes []actorNode } `json:"assignees"`
	Labels    struct{ Nodes []labelNode } `json:"labels"`
	Milestone *milestoneNode              `json:"milestone"`
	CreatedAt time.Time                   `json:"createdAt"`
	UpdatedAt time.Time                   `json:"updatedAt"`
	ClosedAt  *time.Time                  `json:"closedAt"`
	URL       string                      `json:"url"`
	Body      string                      `json:"body"`
	Comments  struct {
		TotalCount int           `json:"totalCount"`
		Nodes      []commentNode `json:"nodes"`
	} `json:"comments"`
}

type prNode struct {
	Number         int                         `json:"number"`
	Title          string                      `json:"title"`
	State          string                      `json:"state"`
	IsDraft        bool                        `json:"isDraft"`
	ReviewDecision string                      `json:"reviewDecision"`
	Author         actorNode                   `json:"author"`
	Assignees      struct{ Nodes []actorNode } `json:"assignees"`
	Labels         struct{ Nodes []labelNode } `json:"labels"`
	Milestone      *milestoneNode              `json:"milestone"`
	BaseRefName    string                      `json:"baseRefName"`
	HeadRefName    string                      `json:"headRefName"`
	CreatedAt      time.Time                   `json:"createdAt"`
	UpdatedAt      time.Time                   `json:"updatedAt"`
	MergedAt       *time.Time                  `json:"mergedAt"`
	ClosedAt       *time.Time                  `json:"closedAt"`
	URL            string                      `json:"url"`
	Body           string                      `json:"body"`
	Comments       struct {
		TotalCount int           `json:"totalCount"`
		Nodes      []commentNode `json:"nodes"`
	} `json:"comments"`
}

// searchNode covers both Issue and PullRequest fields from a search result.
type searchNode struct {
	Typename       string                      `json:"__typename"`
	Number         int                         `json:"number"`
	Title          string                      `json:"title"`
	State          string                      `json:"state"`
	IsDraft        bool                        `json:"isDraft"`
	ReviewDecision string                      `json:"reviewDecision"`
	Author         actorNode                   `json:"author"`
	Assignees      struct{ Nodes []actorNode } `json:"assignees"`
	Labels         struct{ Nodes []labelNode } `json:"labels"`
	Milestone      *milestoneNode              `json:"milestone"`
	BaseRefName    string                      `json:"baseRefName"`
	HeadRefName    string                      `json:"headRefName"`
	CreatedAt      time.Time                   `json:"createdAt"`
	UpdatedAt      time.Time                   `json:"updatedAt"`
	MergedAt       *time.Time                  `json:"mergedAt"`
	ClosedAt       *time.Time                  `json:"closedAt"`
	URL            string                      `json:"url"`
	Body           string                      `json:"body"`
	Comments       struct {
		TotalCount int           `json:"totalCount"`
		Nodes      []commentNode `json:"nodes"`
	} `json:"comments"`
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func nodeToIssue(n *issueNode) *Issue {
	issue := &Issue{
		Number:       n.Number,
		Title:        n.Title,
		State:        n.State,
		Author:       Actor{Login: n.Author.Login},
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		ClosedAt:     n.ClosedAt,
		URL:          n.URL,
		Body:         n.Body,
		CommentCount: n.Comments.TotalCount,
	}
	for _, a := range n.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, Actor{Login: a.Login})
	}
	for _, l := range n.Labels.Nodes {
		issue.Labels = append(issue.Labels, Label{Name: l.Name, Color: l.Color})
	}
	if n.Milestone != nil {
		issue.Milestone = &Milestone{Number: n.Milestone.Number, Title: n.Milestone.Title}
	}
	for _, c := range n.Comments.Nodes {
		issue.Comments = append(issue.Comments, commentNodeToComment(c))
	}
	return issue
}

func nodeToPR(n *prNode) *PullRequest {
	pr := &PullRequest{
		Number:         n.Number,
		Title:          n.Title,
		State:          n.State,
		IsDraft:        n.IsDraft,
		ReviewDecision: n.ReviewDecision,
		Author:         Actor{Login: n.Author.Login},
		BaseRefName:    n.BaseRefName,
		HeadRefName:    n.HeadRefName,
		CreatedAt:      n.CreatedAt,
		UpdatedAt:      n.UpdatedAt,
		MergedAt:       n.MergedAt,
		ClosedAt:       n.ClosedAt,
		URL:            n.URL,
		Body:           n.Body,
		CommentCount:   n.Comments.TotalCount,
	}
	for _, a := range n.Assignees.Nodes {
		pr.Assignees = append(pr.Assignees, Actor{Login: a.Login})
	}
	for _, l := range n.Labels.Nodes {
		pr.Labels = append(pr.Labels, Label{Name: l.Name, Color: l.Color})
	}
	if n.Milestone != nil {
		pr.Milestone = &Milestone{Number: n.Milestone.Number, Title: n.Milestone.Title}
	}
	for _, c := range n.Comments.Nodes {
		pr.Comments = append(pr.Comments, commentNodeToComment(c))
	}
	return pr
}

func searchNodeToIssue(n *searchNode) *Issue {
	issue := &Issue{
		Number:       n.Number,
		Title:        n.Title,
		State:        n.State,
		Author:       Actor{Login: n.Author.Login},
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		ClosedAt:     n.ClosedAt,
		URL:          n.URL,
		Body:         n.Body,
		CommentCount: n.Comments.TotalCount,
	}
	for _, a := range n.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, Actor{Login: a.Login})
	}
	for _, l := range n.Labels.Nodes {
		issue.Labels = append(issue.Labels, Label{Name: l.Name, Color: l.Color})
	}
	if n.Milestone != nil {
		issue.Milestone = &Milestone{Number: n.Milestone.Number, Title: n.Milestone.Title}
	}
	for _, c := range n.Comments.Nodes {
		issue.Comments = append(issue.Comments, commentNodeToComment(c))
	}
	return issue
}

func searchNodeToPR(n *searchNode) *PullRequest {
	pr := &PullRequest{
		Number:         n.Number,
		Title:          n.Title,
		State:          n.State,
		IsDraft:        n.IsDraft,
		ReviewDecision: n.ReviewDecision,
		Author:         Actor{Login: n.Author.Login},
		BaseRefName:    n.BaseRefName,
		HeadRefName:    n.HeadRefName,
		CreatedAt:      n.CreatedAt,
		UpdatedAt:      n.UpdatedAt,
		MergedAt:       n.MergedAt,
		ClosedAt:       n.ClosedAt,
		URL:            n.URL,
		Body:           n.Body,
		CommentCount:   n.Comments.TotalCount,
	}
	for _, a := range n.Assignees.Nodes {
		pr.Assignees = append(pr.Assignees, Actor{Login: a.Login})
	}
	for _, l := range n.Labels.Nodes {
		pr.Labels = append(pr.Labels, Label{Name: l.Name, Color: l.Color})
	}
	if n.Milestone != nil {
		pr.Milestone = &Milestone{Number: n.Milestone.Number, Title: n.Milestone.Title}
	}
	for _, c := range n.Comments.Nodes {
		pr.Comments = append(pr.Comments, commentNodeToComment(c))
	}
	return pr
}

func commentNodeToComment(c commentNode) Comment {
	return Comment{
		ID:        c.ID,
		Author:    Actor{Login: c.Author.Login},
		Body:      c.Body,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		URL:       c.URL,
	}
}

// ---------------------------------------------------------------------------
// Issue API
// ---------------------------------------------------------------------------

const listIssuesQuery = `
query($owner: String!, $repo: String!, $first: Int!, $states: [IssueState!], $filterBy: IssueFilters, $after: String) {
  repository(owner: $owner, name: $repo) {
    issues(first: $first, states: $states, filterBy: $filterBy, after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state
        author { login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        createdAt updatedAt closedAt url body
        comments { totalCount }
      }
    }
  }
}`

const getIssueQuery = `
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    issue(number: $number) {
      number title state
      author { login }
      assignees(first: 10) { nodes { login } }
      labels(first: 20) { nodes { name color } }
      milestone { number title }
      createdAt updatedAt closedAt url body
      comments(first: 100) {
        nodes { id author { login } body createdAt updatedAt url }
      }
    }
  }
}`

const searchIssuesQuery = `
query($query: String!, $first: Int!, $after: String) {
  search(query: $query, type: ISSUE, first: $first, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes {
      __typename
      ... on Issue {
        number title state
        author { login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        createdAt updatedAt closedAt url body
        comments { totalCount }
      }
    }
  }
}`

const fetchAllIssuesQuery = `
query($owner: String!, $repo: String!, $after: String, $since: DateTime) {
  repository(owner: $owner, name: $repo) {
    issues(first: 100, states: [OPEN, CLOSED], filterBy: {since: $since}, after: $after, orderBy: {field: UPDATED_AT, direction: ASC}) {
      totalCount
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state
        author { login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        createdAt updatedAt closedAt url body
        comments(first: 100) {
          totalCount
          nodes { id author { login } body createdAt updatedAt url }
        }
      }
    }
  }
}`

// ListIssues fetches issues matching the given options, using search when needed.
func (c *Client) ListIssues(owner, repo string, opts IssueListOptions) ([]*Issue, error) {
	if opts.App != "" || opts.Search != "" {
		return c.searchIssues(owner, repo, opts)
	}
	return c.listIssuesDirect(owner, repo, opts)
}

func (c *Client) listIssuesDirect(owner, repo string, opts IssueListOptions) ([]*Issue, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	filterBy := map[string]interface{}{}
	if opts.Assignee != "" {
		filterBy["assignee"] = opts.Assignee
	}
	if opts.Author != "" {
		filterBy["createdBy"] = opts.Author
	}
	if len(opts.Labels) > 0 {
		filterBy["labels"] = opts.Labels
	}
	if opts.Milestone != "" {
		filterBy["milestone"] = opts.Milestone
	}
	if opts.Mention != "" {
		filterBy["mentioned"] = opts.Mention
	}

	var issues []*Issue
	var cursor string

	for {
		pageSize := limit - len(issues)
		if pageSize > 100 {
			pageSize = 100
		}
		if pageSize <= 0 {
			break
		}

		vars := map[string]interface{}{
			"owner":    owner,
			"repo":     repo,
			"first":    pageSize,
			"states":   issueStates(opts.State),
			"filterBy": filterBy,
		}
		if cursor != "" {
			vars["after"] = cursor
		}

		var result struct {
			Repository struct {
				Issues struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []issueNode `json:"nodes"`
				} `json:"issues"`
			} `json:"repository"`
		}

		if err := c.Query(listIssuesQuery, vars, &result); err != nil {
			return nil, err
		}

		for i := range result.Repository.Issues.Nodes {
			issues = append(issues, nodeToIssue(&result.Repository.Issues.Nodes[i]))
		}

		if !result.Repository.Issues.PageInfo.HasNextPage || len(issues) >= limit {
			break
		}
		cursor = result.Repository.Issues.PageInfo.EndCursor
	}

	return issues, nil
}

func (c *Client) searchIssues(owner, repo string, opts IssueListOptions) ([]*Issue, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	q := buildIssueSearchQuery(owner, repo, opts)
	var issues []*Issue
	var cursor string

	for {
		pageSize := limit - len(issues)
		if pageSize > 100 {
			pageSize = 100
		}
		if pageSize <= 0 {
			break
		}

		vars := map[string]interface{}{
			"query": q,
			"first": pageSize,
		}
		if cursor != "" {
			vars["after"] = cursor
		}

		var result struct {
			Search struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []searchNode `json:"nodes"`
			} `json:"search"`
		}

		if err := c.Query(searchIssuesQuery, vars, &result); err != nil {
			return nil, err
		}

		for i := range result.Search.Nodes {
			n := &result.Search.Nodes[i]
			if n.Typename == "Issue" && n.Number > 0 {
				issues = append(issues, searchNodeToIssue(n))
			}
		}

		if !result.Search.PageInfo.HasNextPage || len(issues) >= limit {
			break
		}
		cursor = result.Search.PageInfo.EndCursor
	}

	return issues, nil
}

// GetIssue fetches a single issue with all comments.
func (c *Client) GetIssue(owner, repo string, number int) (*Issue, error) {
	vars := map[string]interface{}{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	}

	var result struct {
		Repository struct {
			Issue *issueNode `json:"issue"`
		} `json:"repository"`
	}

	if err := c.Query(getIssueQuery, vars, &result); err != nil {
		return nil, err
	}
	if result.Repository.Issue == nil {
		return nil, fmt.Errorf("issue #%d not found", number)
	}
	return nodeToIssue(result.Repository.Issue), nil
}

// FetchAllIssues retrieves every issue (all states) with comments for caching,
// oldest-updated-first. If since is non-nil, only issues updated at or after
// that time are fetched (delta update / resume). onBatch is invoked with each
// page as it arrives so callers can persist incrementally; a non-nil error from
// onBatch aborts the fetch.
func (c *Client) FetchAllIssues(owner, repo string, since *time.Time, onBatch IssueBatchFunc) ([]*Issue, error) {
	var issues []*Issue
	var cursor string
	total := 0

	for {
		vars := map[string]interface{}{
			"owner": owner,
			"repo":  repo,
		}
		if since != nil {
			vars["since"] = since.UTC().Format(time.RFC3339)
		}
		if cursor != "" {
			vars["after"] = cursor
		}

		var result struct {
			Repository struct {
				Issues struct {
					TotalCount int `json:"totalCount"`
					PageInfo   struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []issueNode `json:"nodes"`
				} `json:"issues"`
			} `json:"repository"`
		}

		if err := c.Query(fetchAllIssuesQuery, vars, &result); err != nil {
			return issues, err
		}

		if total == 0 {
			total = result.Repository.Issues.TotalCount
		}

		var batch []*Issue
		for i := range result.Repository.Issues.Nodes {
			batch = append(batch, nodeToIssue(&result.Repository.Issues.Nodes[i]))
		}
		issues = append(issues, batch...)

		if onBatch != nil {
			if err := onBatch(batch, total); err != nil {
				return issues, err
			}
		}

		if !result.Repository.Issues.PageInfo.HasNextPage {
			break
		}
		cursor = result.Repository.Issues.PageInfo.EndCursor
	}

	return issues, nil
}

// ---------------------------------------------------------------------------
// Pull request API
// ---------------------------------------------------------------------------

const listPRsQuery = `
query($owner: String!, $repo: String!, $first: Int!, $states: [PullRequestState!], $labels: [String!], $baseRefName: String, $headRefName: String, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $first, states: $states, labels: $labels, baseRefName: $baseRefName, headRefName: $headRefName, after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state isDraft reviewDecision
        author { login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        baseRefName headRefName
        createdAt updatedAt mergedAt closedAt url body
        comments { totalCount }
      }
    }
  }
}`

const getPRQuery = `
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      number title state isDraft reviewDecision
      author { login }
      assignees(first: 10) { nodes { login } }
      labels(first: 20) { nodes { name color } }
      milestone { number title }
      baseRefName headRefName
      createdAt updatedAt mergedAt closedAt url body
      comments(first: 100) {
        nodes { id author { login } body createdAt updatedAt url }
      }
    }
  }
}`

const searchPRsQuery = `
query($query: String!, $first: Int!, $after: String) {
  search(query: $query, type: ISSUE, first: $first, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes {
      __typename
      ... on PullRequest {
        number title state isDraft reviewDecision
        author { login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        baseRefName headRefName
        createdAt updatedAt mergedAt closedAt url body
        comments { totalCount }
      }
    }
  }
}`

const fetchAllPRsQuery = `
query($owner: String!, $repo: String!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: 100, states: [OPEN, CLOSED, MERGED], after: $after, orderBy: {field: UPDATED_AT, direction: ASC}) {
      totalCount
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state isDraft reviewDecision
        author { login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        baseRefName headRefName
        createdAt updatedAt mergedAt closedAt url body
        comments(first: 100) {
          totalCount
          nodes { id author { login } body createdAt updatedAt url }
        }
      }
    }
  }
}`

// ListPRs fetches pull requests matching the given options, using search when needed.
func (c *Client) ListPRs(owner, repo string, opts PRListOptions) ([]*PullRequest, error) {
	if opts.Author != "" || opts.Assignee != "" || opts.App != "" || opts.Draft || opts.Search != "" {
		return c.searchPRs(owner, repo, opts)
	}
	return c.listPRsDirect(owner, repo, opts)
}

func (c *Client) listPRsDirect(owner, repo string, opts PRListOptions) ([]*PullRequest, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	var prs []*PullRequest
	var cursor string

	for {
		pageSize := limit - len(prs)
		if pageSize > 100 {
			pageSize = 100
		}
		if pageSize <= 0 {
			break
		}

		vars := map[string]interface{}{
			"owner":  owner,
			"repo":   repo,
			"first":  pageSize,
			"states": prStates(opts.State),
		}
		if len(opts.Labels) > 0 {
			vars["labels"] = opts.Labels
		}
		if opts.Base != "" {
			vars["baseRefName"] = opts.Base
		}
		if opts.Head != "" {
			vars["headRefName"] = opts.Head
		}
		if cursor != "" {
			vars["after"] = cursor
		}

		var result struct {
			Repository struct {
				PullRequests struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []prNode `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}

		if err := c.Query(listPRsQuery, vars, &result); err != nil {
			return nil, err
		}

		for i := range result.Repository.PullRequests.Nodes {
			prs = append(prs, nodeToPR(&result.Repository.PullRequests.Nodes[i]))
		}

		if !result.Repository.PullRequests.PageInfo.HasNextPage || len(prs) >= limit {
			break
		}
		cursor = result.Repository.PullRequests.PageInfo.EndCursor
	}

	return prs, nil
}

func (c *Client) searchPRs(owner, repo string, opts PRListOptions) ([]*PullRequest, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	q := buildPRSearchQuery(owner, repo, opts)
	var prs []*PullRequest
	var cursor string

	for {
		pageSize := limit - len(prs)
		if pageSize > 100 {
			pageSize = 100
		}
		if pageSize <= 0 {
			break
		}

		vars := map[string]interface{}{
			"query": q,
			"first": pageSize,
		}
		if cursor != "" {
			vars["after"] = cursor
		}

		var result struct {
			Search struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []searchNode `json:"nodes"`
			} `json:"search"`
		}

		if err := c.Query(searchPRsQuery, vars, &result); err != nil {
			return nil, err
		}

		for i := range result.Search.Nodes {
			n := &result.Search.Nodes[i]
			if n.Typename == "PullRequest" && n.Number > 0 {
				prs = append(prs, searchNodeToPR(n))
			}
		}

		if !result.Search.PageInfo.HasNextPage || len(prs) >= limit {
			break
		}
		cursor = result.Search.PageInfo.EndCursor
	}

	return prs, nil
}

// GetPR fetches a single pull request with all comments.
func (c *Client) GetPR(owner, repo string, number int) (*PullRequest, error) {
	vars := map[string]interface{}{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	}

	var result struct {
		Repository struct {
			PullRequest *prNode `json:"pullRequest"`
		} `json:"repository"`
	}

	if err := c.Query(getPRQuery, vars, &result); err != nil {
		return nil, err
	}
	if result.Repository.PullRequest == nil {
		return nil, fmt.Errorf("pull request #%d not found", number)
	}
	return nodeToPR(result.Repository.PullRequest), nil
}

// FetchAllPRs retrieves every pull request (all states) with comments for
// caching, oldest-updated-first. It is intended for the cold/full cache path
// (the pullRequests connection has no server-side date filter, so delta/resume
// is handled by FetchPRsUpdated). onBatch is invoked per page so callers can
// persist incrementally; a non-nil error aborts the fetch.
func (c *Client) FetchAllPRs(owner, repo string, onBatch PRBatchFunc) ([]*PullRequest, error) {
	var prs []*PullRequest
	var cursor string
	total := 0

	for {
		vars := map[string]interface{}{
			"owner": owner,
			"repo":  repo,
		}
		if cursor != "" {
			vars["after"] = cursor
		}

		var result struct {
			Repository struct {
				PullRequests struct {
					TotalCount int `json:"totalCount"`
					PageInfo   struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []prNode `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}

		if err := c.Query(fetchAllPRsQuery, vars, &result); err != nil {
			return prs, err
		}

		if total == 0 {
			total = result.Repository.PullRequests.TotalCount
		}

		var batch []*PullRequest
		for i := range result.Repository.PullRequests.Nodes {
			batch = append(batch, nodeToPR(&result.Repository.PullRequests.Nodes[i]))
		}
		prs = append(prs, batch...)

		if onBatch != nil {
			if err := onBatch(batch, total); err != nil {
				return prs, err
			}
		}

		if !result.Repository.PullRequests.PageInfo.HasNextPage {
			break
		}
		cursor = result.Repository.PullRequests.PageInfo.EndCursor
	}

	return prs, nil
}

const searchPRsForCacheQuery = `
query($query: String!, $first: Int!, $after: String) {
  search(query: $query, type: ISSUE, first: $first, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes {
      __typename
      ... on PullRequest {
        number title state isDraft reviewDecision
        author { login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        baseRefName headRefName
        createdAt updatedAt mergedAt closedAt url body
        comments(first: 100) {
          totalCount
          nodes { id author { login } body createdAt updatedAt url }
        }
      }
    }
  }
}`

// FetchPRsUpdated retrieves pull requests updated at or after since via the
// search API, for delta updates and resuming an interrupted fetch. The search
// connection is used because the pullRequests connection has no server-side
// date filter. Note that GitHub's search `updated:` qualifier is day-granular
// and capped at 1000 results, so very large/active repos may need a --force
// cold fetch. onBatch is invoked per page so callers can persist incrementally.
func (c *Client) FetchPRsUpdated(owner, repo string, since time.Time, onBatch PRBatchFunc) ([]*PullRequest, error) {
	q := fmt.Sprintf("repo:%s/%s is:pr updated:>=%s", owner, repo, since.UTC().Format("2006-01-02"))
	var prs []*PullRequest
	var cursor string

	for {
		pageSize := 100
		vars := map[string]interface{}{
			"query": q,
			"first": pageSize,
		}
		if cursor != "" {
			vars["after"] = cursor
		}

		var result struct {
			Search struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []searchNode `json:"nodes"`
			} `json:"search"`
		}

		if err := c.Query(searchPRsForCacheQuery, vars, &result); err != nil {
			return prs, err
		}

		var batch []*PullRequest
		for i := range result.Search.Nodes {
			n := &result.Search.Nodes[i]
			if n.Typename == "PullRequest" && n.Number > 0 {
				batch = append(batch, searchNodeToPR(n))
			}
		}
		prs = append(prs, batch...)

		if onBatch != nil {
			if err := onBatch(batch, 0); err != nil {
				return prs, err
			}
		}

		if !result.Search.PageInfo.HasNextPage {
			break
		}
		cursor = result.Search.PageInfo.EndCursor
	}

	return prs, nil
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

func buildIssueSearchQuery(owner, repo string, opts IssueListOptions) string {
	parts := []string{
		fmt.Sprintf("repo:%s/%s", owner, repo),
		"is:issue",
	}
	switch opts.State {
	case "open":
		parts = append(parts, "is:open")
	case "closed":
		parts = append(parts, "is:closed")
	}
	if opts.Author != "" {
		parts = append(parts, "author:"+opts.Author)
	}
	if opts.Assignee != "" {
		parts = append(parts, "assignee:"+opts.Assignee)
	}
	for _, l := range opts.Labels {
		parts = append(parts, `label:"`+l+`"`)
	}
	if opts.Milestone != "" {
		parts = append(parts, `milestone:"`+opts.Milestone+`"`)
	}
	if opts.Mention != "" {
		parts = append(parts, "mentions:"+opts.Mention)
	}
	if opts.App != "" {
		parts = append(parts, "author:app/"+opts.App)
	}
	if opts.Search != "" {
		parts = append(parts, opts.Search)
	}
	return strings.Join(parts, " ")
}

func buildPRSearchQuery(owner, repo string, opts PRListOptions) string {
	parts := []string{
		fmt.Sprintf("repo:%s/%s", owner, repo),
		"is:pr",
	}
	switch opts.State {
	case "open":
		parts = append(parts, "is:open")
	case "closed":
		parts = append(parts, "is:closed")
	case "merged":
		parts = append(parts, "is:merged")
	}
	if opts.Author != "" {
		parts = append(parts, "author:"+opts.Author)
	}
	if opts.Assignee != "" {
		parts = append(parts, "assignee:"+opts.Assignee)
	}
	for _, l := range opts.Labels {
		parts = append(parts, `label:"`+l+`"`)
	}
	if opts.Base != "" {
		parts = append(parts, "base:"+opts.Base)
	}
	if opts.Head != "" {
		parts = append(parts, "head:"+opts.Head)
	}
	if opts.Draft {
		parts = append(parts, "draft:true")
	}
	if opts.App != "" {
		parts = append(parts, "author:app/"+opts.App)
	}
	if opts.Search != "" {
		parts = append(parts, opts.Search)
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// State helpers
// ---------------------------------------------------------------------------

func issueStates(state string) []string {
	switch state {
	case "open":
		return []string{"OPEN"}
	case "closed":
		return []string{"CLOSED"}
	default: // "all" or ""
		return []string{"OPEN", "CLOSED"}
	}
}

func prStates(state string) []string {
	switch state {
	case "open":
		return []string{"OPEN"}
	case "closed":
		return []string{"CLOSED"}
	case "merged":
		return []string{"MERGED"}
	default: // "all" or ""
		return []string{"OPEN", "CLOSED", "MERGED"}
	}
}
