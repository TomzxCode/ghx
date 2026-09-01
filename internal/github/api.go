package github

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

type reviewNode struct {
	Author      actorNode `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type timelineNode struct {
	Typename          string     `json:"__typename"`
	Actor             actorNode  `json:"actor"`
	RequestedReviewer *actorNode `json:"requestedReviewer"`
	CreatedAt         time.Time  `json:"createdAt"`
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
	Number         int                            `json:"number"`
	Title          string                         `json:"title"`
	State          string                         `json:"state"`
	IsDraft        bool                           `json:"isDraft"`
	ReviewDecision string                         `json:"reviewDecision"`
	Author         actorNode                      `json:"author"`
	Assignees      struct{ Nodes []actorNode }    `json:"assignees"`
	Labels         struct{ Nodes []labelNode }    `json:"labels"`
	Milestone      *milestoneNode                 `json:"milestone"`
	BaseRefName    string                         `json:"baseRefName"`
	HeadRefName    string                         `json:"headRefName"`
	CreatedAt      time.Time                      `json:"createdAt"`
	UpdatedAt      time.Time                      `json:"updatedAt"`
	MergedAt       *time.Time                     `json:"mergedAt"`
	ClosedAt       *time.Time                     `json:"closedAt"`
	URL            string                         `json:"url"`
	Body           string                         `json:"body"`
	Additions      int                            `json:"additions"`
	Deletions      int                            `json:"deletions"`
	Reviews        struct{ Nodes []reviewNode }   `json:"reviews"`
	TimelineItems  struct{ Nodes []timelineNode } `json:"timelineItems"`
	Comments       struct {
		TotalCount int           `json:"totalCount"`
		Nodes      []commentNode `json:"nodes"`
	} `json:"comments"`
}

// searchNode covers both Issue and PullRequest fields from a search result.
type searchNode struct {
	Typename       string                         `json:"__typename"`
	Number         int                            `json:"number"`
	Title          string                         `json:"title"`
	State          string                         `json:"state"`
	IsDraft        bool                           `json:"isDraft"`
	ReviewDecision string                         `json:"reviewDecision"`
	Author         actorNode                      `json:"author"`
	Assignees      struct{ Nodes []actorNode }    `json:"assignees"`
	Labels         struct{ Nodes []labelNode }    `json:"labels"`
	Milestone      *milestoneNode                 `json:"milestone"`
	BaseRefName    string                         `json:"baseRefName"`
	HeadRefName    string                         `json:"headRefName"`
	CreatedAt      time.Time                      `json:"createdAt"`
	UpdatedAt      time.Time                      `json:"updatedAt"`
	MergedAt       *time.Time                     `json:"mergedAt"`
	ClosedAt       *time.Time                     `json:"closedAt"`
	URL            string                         `json:"url"`
	Body           string                         `json:"body"`
	Additions      int                            `json:"additions"`
	Deletions      int                            `json:"deletions"`
	Reviews        struct{ Nodes []reviewNode }   `json:"reviews"`
	TimelineItems  struct{ Nodes []timelineNode } `json:"timelineItems"`
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
	applyPRExtras(pr, n.Additions, n.Deletions, n.Reviews.Nodes, n.TimelineItems.Nodes)
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

// applyPRExtras fills the review-analytics fields (additions, deletions,
// reviews, timeline events) on a PullRequest from parsed API nodes.
func applyPRExtras(pr *PullRequest, additions, deletions int, reviews []reviewNode, timeline []timelineNode) {
	pr.Additions = additions
	pr.Deletions = deletions
	for _, r := range reviews {
		pr.Reviews = append(pr.Reviews, PRReview{
			Author:      Actor{Login: r.Author.Login},
			State:       r.State,
			SubmittedAt: r.SubmittedAt,
		})
	}
	for _, t := range timeline {
		kind, ok := timelineKindFromTypename(t.Typename)
		if !ok {
			continue
		}
		ev := TimelineEvent{Kind: kind, Actor: Actor{Login: t.Actor.Login}, CreatedAt: t.CreatedAt}
		if t.RequestedReviewer != nil {
			ev.RequestedReviewer = Actor{Login: t.RequestedReviewer.Login}
		}
		pr.Timeline = append(pr.Timeline, ev)
	}
}

// timelineKindFromTypename maps a GraphQL timeline item __typename to its
// normalized event kind. Untracked event types are dropped. Review
// re-requests have no GraphQL event type of their own; GitHub records them as
// additional ReviewRequestedEvent entries, which the stats command splits
// into initial requests and re-requests.
func timelineKindFromTypename(typename string) (TimelineEventKind, bool) {
	switch typename {
	case "ReadyForReviewEvent":
		return TimelineReadyForReview, true
	case "ConvertToDraftEvent":
		return TimelineConvertedToDraft, true
	case "ReviewRequestedEvent":
		return TimelineReviewRequested, true
	default:
		return "", false
	}
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
	applyPRExtras(pr, n.Additions, n.Deletions, n.Reviews.Nodes, n.TimelineItems.Nodes)
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

// PR delta-fetch tuning. prFullPageSize bounds each full-payload page: with
// reviews, timeline items, and comments attached, 50+ node pages regularly
// exceed what GitHub's GraphQL backend evaluates within its timeout and fail
// with HTML 502s from the edge. prScanPageSize is the light walk's page size;
// it must be a multiple of prFullPageSize so each block is a whole number of
// full pages. prBlocksInFlight bounds how many block chains are fetched in
// parallel; it is deliberately low because GitHub's secondary rate limits
// trigger on concurrent request patterns, and each trip costs a 60s
// Retry-After wait, which more than cancels the parallelism.
const (
	prScanPageSize   = 100
	prFullPageSize   = 25
	prBlocksInFlight = 2
	prPagesPerBlock  = prScanPageSize / prFullPageSize
)

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

// prTimelineSelection is the timelineItems selection shared by the
// cache-facing queries. PullRequestTimelineItems is a union, so member fields
// require inline fragments; RequestedReviewer is a union of User, Team and
// Mannequin, so only User logins are read.
const prTimelineSelection = `
timelineItems(first: 250, itemTypes: [READY_FOR_REVIEW_EVENT, CONVERT_TO_DRAFT_EVENT, REVIEW_REQUESTED_EVENT]) {
  nodes {
    __typename
    ... on ReadyForReviewEvent { actor { login } createdAt }
    ... on ConvertToDraftEvent { actor { login } createdAt }
    ... on ReviewRequestedEvent { actor { login } createdAt requestedReviewer { ... on User { login } } }
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
      additions deletions
      reviews(first: 100) {
        nodes { author { login } state submittedAt }
      }
      ` + prTimelineSelection + `
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
query($owner: String!, $repo: String!, $after: String, $dir: OrderDirection!) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: 50, states: [OPEN, CLOSED, MERGED], after: $after, orderBy: {field: UPDATED_AT, direction: $dir}) {
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
        additions deletions
        reviews(first: 100) {
          nodes { author { login } state submittedAt }
        }
` + prTimelineSelection + `
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
// (delta/resume is handled by FetchPRsUpdated). Pages are capped at 50 PRs:
// with reviews, timeline items, and comments attached, 100-node pages exceed
// what GitHub's GraphQL backend reliably evaluates and it responds with 502s.
// onBatch is invoked per page so callers can persist incrementally; a non-nil
// error aborts the fetch.
func (c *Client) FetchAllPRs(owner, repo string, onBatch PRBatchFunc) ([]*PullRequest, error) {
	var prs []*PullRequest
	var cursor string
	total := 0

	for {
		vars := map[string]interface{}{
			"owner": owner,
			"repo":  repo,
			"dir":   "ASC",
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

const fetchPRsUpdatedLightQuery = `
query($owner: String!, $repo: String!, $after: String, $dir: OrderDirection!, $first: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $first, states: [OPEN, CLOSED, MERGED], after: $after, orderBy: {field: UPDATED_AT, direction: $dir}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number
        updatedAt
      }
    }
  }
}`

// FetchPRsUpdated retrieves pull requests updated at or after since, for
// delta updates, resuming an interrupted fetch, and --since windows. It runs
// in two phases:
//
//  1. A lightweight newest-first walk over the pullRequests connection that
//     reads only number and updatedAt, prScanPageSize at a time, stopping as
//     soon as a page's oldest updatedAt falls before since. Light pages are
//     cheap, so scanning runs wide, and the walk yields the exact number of
//     in-window PRs plus each block's start cursor up front.
//  2. A full-payload fetch of the window. Pages carrying 50+ fully-populated
//     PR nodes (reviews, timeline items, comments) regularly exceed what
//     GitHub's GraphQL backend evaluates within its timeout, surfacing as
//     HTML 502s from the edge, so full pages are capped at prFullPageSize
//     nodes. The window is divided into prScanPageSize-wide blocks, and each
//     block is fetched as a chain of prPagesPerBlock full pages seeded by the
//     block's cursor; chains run prBlocksInFlight at a time in parallel.
//
// onBatch is invoked per completed page (in completion order; caching is
// order-independent) with total reporting the exact number of in-window PRs
// so callers can render a determinate progress bar. onScan, when non-nil, is
// invoked after each phase 1 page with the number of PRs scanned so far, so
// callers can show progress while the window is being located. A non-nil
// error from onBatch aborts the fetch.
func (c *Client) FetchPRsUpdated(owner, repo string, since time.Time, onBatch PRBatchFunc, onScan func(scanned int)) ([]*PullRequest, error) {
	// Phase 1: locate the window with the light query, recording each
	// block's start cursor (the light page's own start cursor) so phase 2
	// can fetch blocks concurrently.
	var candidateCount, scanned int
	blockCursors := []string{""} // blockCursors[b] = after-cursor for block b
	var blockCandidates []int    // candidates per walked block
	var cursor string

	for {
		vars := map[string]interface{}{
			"owner": owner,
			"repo":  repo,
			"first": prScanPageSize,
			"dir":   "DESC",
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
					Nodes []struct {
						Number    int       `json:"number"`
						UpdatedAt time.Time `json:"updatedAt"`
					} `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}

		if err := c.Query(fetchPRsUpdatedLightQuery, vars, &result); err != nil {
			return nil, err
		}

		page := result.Repository.PullRequests.Nodes
		var pageOldest time.Time
		count := 0
		for _, n := range page {
			scanned++
			if pageOldest.IsZero() || n.UpdatedAt.Before(pageOldest) {
				pageOldest = n.UpdatedAt
			}
			if !n.UpdatedAt.Before(since) {
				count++
			}
		}
		blockCandidates = append(blockCandidates, count)
		candidateCount += count
		if onScan != nil {
			onScan(scanned)
		}

		// Newest-first: once a page reaches items older than since, every
		// later page is older still. The walk continues while pageOldest is
		// on or after since, so equal timestamps straddling a page boundary
		// are not dropped.
		if pageOldest.Before(since) || !result.Repository.PullRequests.PageInfo.HasNextPage {
			break
		}
		cursor = result.Repository.PullRequests.PageInfo.EndCursor
		blockCursors = append(blockCursors, cursor)
	}

	if candidateCount == 0 {
		return nil, nil
	}

	// Phase 2: fetch the window's blocks in parallel. Candidate blocks form
	// a contiguous prefix (newest-first order), so trailing empty blocks are
	// dropped.
	blocks := len(blockCandidates)
	for blocks > 0 && blockCandidates[blocks-1] == 0 {
		blocks--
	}

	var prs []*PullRequest
	workers := prBlocksInFlight
	if blocks < workers {
		workers = blocks
	}

	type pageResult struct {
		batch []*PullRequest
		err   error
		done  bool // last message of a block
	}
	jobs := make(chan int)
	results := make(chan pageResult, blocks*(prPagesPerBlock+1)) // buffered: workers never block
	var wg sync.WaitGroup
	var mu sync.Mutex // guards firstErr
	var firstErr error
	var abort atomic.Bool // stop starting new pages after a failure
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker owns whole blocks: a chain of full pages seeded
			// by the block cursor, where each page's endCursor seeds the
			// next page of the same block.
			for blockIdx := range jobs {
				if abort.Load() {
					results <- pageResult{done: true}
					continue
				}
				cursor := blockCursors[blockIdx]
				for page := 0; page < prPagesPerBlock; page++ {
					if abort.Load() {
						break
					}
					res, err := c.fetchPRPage(owner, repo, cursor, since)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mu.Unlock()
						abort.Store(true)
						results <- pageResult{err: err}
						break
					}
					results <- pageResult{batch: res.batch}
					if res.oldest.Before(since) || !res.hasNext {
						break
					}
					cursor = res.end
				}
				results <- pageResult{done: true}
			}
		}()
	}
	for b := 0; b < blocks; b++ {
		jobs <- b
	}
	close(jobs)

	// Deliver pages as they complete so persistence and progress stay live.
	// The connection is not snapshot-consistent: while pages are in flight the
	// repo keeps churning, so pages can overlap at their edges. Deduplicate by
	// number so the cache count reflects reality.
	seen := make(map[int]bool, candidateCount)
	var onBatchErr error
	for blocksDone := 0; blocksDone < blocks; {
		res := <-results
		if res.done {
			blocksDone++
			continue
		}
		if res.err != nil {
			continue
		}
		unique := res.batch[:0:0]
		for _, pr := range res.batch {
			if !seen[pr.Number] {
				seen[pr.Number] = true
				unique = append(unique, pr)
			}
		}
		prs = append(prs, unique...)
		if onBatch != nil && onBatchErr == nil {
			if err := onBatch(unique, candidateCount); err != nil {
				onBatchErr = err
			}
		}
	}
	wg.Wait()
	if firstErr != nil {
		return prs, firstErr
	}
	if onBatchErr != nil {
		return prs, onBatchErr
	}
	return prs, nil
}

// prPage is one fetched full-payload page plus its pagination info.
type prPage struct {
	batch   []*PullRequest
	end     string // endCursor for the next page
	hasNext bool
	oldest  time.Time // oldest updatedAt in the page (zero when empty)
}

// fetchPRPage fetches one full-payload page of PRs starting at the given
// after-cursor and returns those updated at or after since.
func (c *Client) fetchPRPage(owner, repo, after string, since time.Time) (prPage, error) {
	vars := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
		"first": prFullPageSize,
		"dir":   "DESC",
	}
	if after != "" {
		vars["after"] = after
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

	if err := c.Query(fetchAllPRsQuery, vars, &result); err != nil {
		return prPage{}, err
	}

	page := result.Repository.PullRequests
	batch := make([]*PullRequest, 0, len(page.Nodes))
	var oldest time.Time
	for i := range page.Nodes {
		pr := nodeToPR(&page.Nodes[i])
		if oldest.IsZero() || pr.UpdatedAt.Before(oldest) {
			oldest = pr.UpdatedAt
		}
		if !pr.UpdatedAt.Before(since) {
			batch = append(batch, pr)
		}
	}
	return prPage{
		batch:   batch,
		end:     page.PageInfo.EndCursor,
		hasNext: page.PageInfo.HasNextPage,
		oldest:  oldest,
	}, nil
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
