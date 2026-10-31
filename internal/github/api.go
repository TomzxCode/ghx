package github

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomzxcode/ghx/internal/logging"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// repoKey formats owner/repo for telemetry attribution.
func repoKey(owner, repo string) string {
	return owner + "/" + repo
}

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

// actorNode is a GraphQL Actor. Typename is the interface's concrete type
// (User, Bot, Organization, Mannequin, EnterpriseUserAccount) and is the only
// reliable way to tell an application's account from a person's.
type actorNode struct {
	Login    string `json:"login"`
	Typename string `json:"__typename"`
}

// actor converts an API actor node into the normalized Actor.
func (a actorNode) actor() Actor {
	return Actor{Login: a.Login, Type: a.Typename}
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

// requestedReviewerNode is the RequestedReviewer union member: people and bots
// carry a login, teams carry a name and slug.
type requestedReviewerNode struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
}

type reviewRequestNode struct {
	RequestedReviewer *requestedReviewerNode `json:"requestedReviewer"`
}

type reviewRequestConnection struct {
	Nodes []reviewRequestNode `json:"nodes"`
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

type statusCheckContextsNode struct {
	TotalCount int `json:"totalCount"`
}

type statusCheckRollupNode struct {
	State    string                  `json:"state"`
	Contexts statusCheckContextsNode `json:"contexts"`
}

type statusCheckContextNode struct {
	Typename    string `json:"__typename"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	State       string `json:"state"`
	Description string `json:"description"`
	DetailsURL  string `json:"detailsUrl"`
	TargetURL   string `json:"targetUrl"`
	Context     string `json:"context"`
}

type commitNode struct {
	Oid               string                 `json:"oid"`
	StatusCheckRollup *statusCheckRollupNode `json:"statusCheckRollup"`
}

type commitEdgeNode struct {
	Commit commitNode `json:"commit"`
}

type commitsConnection struct {
	Nodes []commitEdgeNode `json:"nodes"`
}

type reviewThreadNode struct {
	IsResolved bool `json:"isResolved"`
}

type reviewThreadConnection struct {
	TotalCount int                `json:"totalCount"`
	Nodes      []reviewThreadNode `json:"nodes"`
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
	HeadRefOid     string                         `json:"headRefOid"`
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
	ReviewRequests *reviewRequestConnection       `json:"reviewRequests"`
	Commits        *commitsConnection             `json:"commits"`
	ReviewThreads  *reviewThreadConnection        `json:"reviewThreads"`
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
	HeadRefOid     string                         `json:"headRefOid"`
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
	Commits        *commitsConnection             `json:"commits"`
	ReviewThreads  *reviewThreadConnection        `json:"reviewThreads"`
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
		Author:       n.Author.actor(),
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		ClosedAt:     n.ClosedAt,
		URL:          n.URL,
		Body:         n.Body,
		CommentCount: n.Comments.TotalCount,
	}
	for _, a := range n.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, a.actor())
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
		Author:         n.Author.actor(),
		BaseRefName:    n.BaseRefName,
		HeadRefName:    n.HeadRefName,
		HeadRefOid:     n.HeadRefOid,
		CreatedAt:      n.CreatedAt,
		UpdatedAt:      n.UpdatedAt,
		MergedAt:       n.MergedAt,
		ClosedAt:       n.ClosedAt,
		URL:            n.URL,
		Body:           n.Body,
		CommentCount:   n.Comments.TotalCount,
	}
	applyPRExtras(pr, n.Additions, n.Deletions, n.Reviews.Nodes, n.TimelineItems.Nodes)
	applyReviewRequests(pr, n.ReviewRequests)
	applyChecks(pr, n.Commits)
	applyReviewThreads(pr, n.ReviewThreads)
	for _, a := range n.Assignees.Nodes {
		pr.Assignees = append(pr.Assignees, a.actor())
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

// applyChecks fills the head commit's check rollup. A nil connection means the
// query did not ask for it (or the cache row predates the field), which leaves
// pr.Checks nil so readers can tell "unknown" from "no checks"; a commit
// without a rollup records a non-nil rollup with an empty state.
func applyChecks(pr *PullRequest, conn *commitsConnection) {
	if conn == nil {
		return
	}
	pr.Checks = &PRChecks{}
	if len(conn.Nodes) == 0 {
		return
	}
	rollup := conn.Nodes[0].Commit.StatusCheckRollup
	if rollup == nil {
		return
	}
	pr.Checks.State = rollup.State
	pr.Checks.Total = rollup.Contexts.TotalCount
}

// applyReviewThreads fills the review-thread counts. A nil connection means the
// query did not ask for them (or the cache row predates the field).
func applyReviewThreads(pr *PullRequest, conn *reviewThreadConnection) {
	if conn == nil {
		return
	}
	open := 0
	for _, thread := range conn.Nodes {
		if !thread.IsResolved {
			open++
		}
	}
	pr.ReviewThreads = &PRThreadCounts{Total: conn.TotalCount, Open: open}
}

// applyReviewRequests fills the current review requests. A nil connection
// means the query did not ask for them (or the cache row predates the field),
// which leaves pr.ReviewRequests nil so readers can tell "unknown" from "none";
// an empty connection records a non-nil empty slice.
func applyReviewRequests(pr *PullRequest, conn *reviewRequestConnection) {
	if conn == nil {
		return
	}
	pr.ReviewRequests = make([]ReviewRequest, 0, len(conn.Nodes))
	for _, n := range conn.Nodes {
		if n.RequestedReviewer == nil {
			continue
		}
		rr := ReviewRequest{
			Type:  n.RequestedReviewer.Typename,
			Login: n.RequestedReviewer.Login,
			Name:  n.RequestedReviewer.Name,
		}
		if rr.Name == "" {
			rr.Name = n.RequestedReviewer.Slug
		}
		if rr.Login == "" && rr.Name == "" {
			continue
		}
		pr.ReviewRequests = append(pr.ReviewRequests, rr)
	}
}

// applyPRExtras fills the review-analytics fields (additions, deletions,
// reviews, timeline events) on a PullRequest from parsed API nodes.
func applyPRExtras(pr *PullRequest, additions, deletions int, reviews []reviewNode, timeline []timelineNode) {
	pr.Additions = additions
	pr.Deletions = deletions
	for _, r := range reviews {
		pr.Reviews = append(pr.Reviews, PRReview{
			Author:      r.Author.actor(),
			State:       r.State,
			SubmittedAt: r.SubmittedAt,
		})
	}
	for _, t := range timeline {
		kind, ok := timelineKindFromTypename(t.Typename)
		if !ok {
			continue
		}
		ev := TimelineEvent{Kind: kind, Actor: t.Actor.actor(), CreatedAt: t.CreatedAt}
		if t.RequestedReviewer != nil {
			ev.RequestedReviewer = t.RequestedReviewer.actor()
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
		Author:       n.Author.actor(),
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		ClosedAt:     n.ClosedAt,
		URL:          n.URL,
		Body:         n.Body,
		CommentCount: n.Comments.TotalCount,
	}
	for _, a := range n.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, a.actor())
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
		Author:         n.Author.actor(),
		BaseRefName:    n.BaseRefName,
		HeadRefName:    n.HeadRefName,
		HeadRefOid:     n.HeadRefOid,
		CreatedAt:      n.CreatedAt,
		UpdatedAt:      n.UpdatedAt,
		MergedAt:       n.MergedAt,
		ClosedAt:       n.ClosedAt,
		URL:            n.URL,
		Body:           n.Body,
		CommentCount:   n.Comments.TotalCount,
	}
	applyPRExtras(pr, n.Additions, n.Deletions, n.Reviews.Nodes, n.TimelineItems.Nodes)
	applyChecks(pr, n.Commits)
	applyReviewThreads(pr, n.ReviewThreads)
	for _, a := range n.Assignees.Nodes {
		pr.Assignees = append(pr.Assignees, a.actor())
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
		Author:    c.Author.actor(),
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
        author { __typename login }
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
      author { __typename login }
      assignees(first: 10) { nodes { login } }
      labels(first: 20) { nodes { name color } }
      milestone { number title }
      createdAt updatedAt closedAt url body
      comments(first: 100) {
        nodes { id author { __typename login } body createdAt updatedAt url }
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
        author { __typename login }
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
        author { __typename login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        createdAt updatedAt closedAt url body
        comments(first: 100) {
          totalCount
          nodes { id author { __typename login } body createdAt updatedAt url }
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

		if err := c.QueryCtx(CallContext{
			Operation: "list_issues",
			Kind:      telemetry.KindIssueList,
			Repo:      repoKey(owner, repo),
			PageSize:  pageSize,
			Cursor:    cursor != "",
		}, listIssuesQuery, vars, &result); err != nil {
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

		if err := c.QueryCtx(CallContext{
			Operation: "search_issues",
			Kind:      telemetry.KindIssueSearch,
			Repo:      repoKey(owner, repo),
			PageSize:  pageSize,
			Cursor:    cursor != "",
		}, searchIssuesQuery, vars, &result); err != nil {
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

	if err := c.QueryCtx(CallContext{
		Operation: "get_issue",
		Kind:      telemetry.KindIssueGet,
		Repo:      repoKey(owner, repo),
	}, getIssueQuery, vars, &result); err != nil {
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
	return c.FetchIssuesUntil(owner, repo, since, nil, onBatch)
}

// FetchIssuesUntil is FetchAllIssues with an exclusive upper bound on
// updatedAt. Items at or after until are dropped and the pagination stops on
// the page that reaches the bound. A nil until walks to the head.
func (c *Client) FetchIssuesUntil(owner, repo string, since, until *time.Time, onBatch IssueBatchFunc) ([]*Issue, error) {
	var issues []*Issue
	var cursor string
	total := 0
	var cutoff time.Time
	if until != nil {
		cutoff = until.UTC().Truncate(time.Second)
	}

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

		if err := c.QueryCtx(CallContext{
			Operation: "fetch_all_issues",
			Kind:      telemetry.KindIssueFull,
			Repo:      repoKey(owner, repo),
			PageSize:  100,
			Cursor:    cursor != "",
		}, fetchAllIssuesQuery, vars, &result); err != nil {
			return issues, err
		}

		if total == 0 {
			total = result.Repository.Issues.TotalCount
		}

		var batch []*Issue
		stopped := false
		for i := range result.Repository.Issues.Nodes {
			issue := nodeToIssue(&result.Repository.Issues.Nodes[i])
			if !cutoff.IsZero() && !issue.UpdatedAt.Before(cutoff) {
				stopped = true
				continue
			}
			batch = append(batch, issue)
		}
		issues = append(issues, batch...)

		if onBatch != nil {
			if err := onBatch(batch, total); err != nil {
				return issues, err
			}
		}

		if stopped || !result.Repository.Issues.PageInfo.HasNextPage {
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

// prChecksSelection is the head commit's check rollup, shared by the list and
// detail queries. Only the aggregate state and the number of contexts are
// cached; GetPRChecks reads the individual checks on demand.
const prChecksSelection = `
commits(last: 1) {
  nodes {
    commit {
      statusCheckRollup {
        state
        contexts { totalCount }
      }
    }
  }
}`

// prReviewThreadsSelection counts a pull request's review threads. A single
// page is read because GitHub cannot filter the connection by resolution
// state, so a pull request with more than a hundred threads undercounts the
// open ones; totalCount stays exact.
const prReviewThreadsSelection = `
reviewThreads(first: 100) {
  totalCount
  nodes { isResolved }
}`

const listPRsQuery = `
query($owner: String!, $repo: String!, $first: Int!, $states: [PullRequestState!], $labels: [String!], $baseRefName: String, $headRefName: String, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $first, states: $states, labels: $labels, baseRefName: $baseRefName, headRefName: $headRefName, after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state isDraft reviewDecision
        author { __typename login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        baseRefName headRefName headRefOid
        createdAt updatedAt mergedAt closedAt url body
        ` + prChecksSelection + `
        comments { totalCount }
      }
    }
  }
}`

// prTimelineSelection is the timelineItems selection shared by the
// cache-facing queries. PullRequestTimelineItems is a union, so member fields
// require inline fragments; RequestedReviewer is a union of User, Bot, Team,
// EnterpriseTeam and Mannequin, of which User and Bot logins are read (Team
// and EnterpriseTeam have no login and Mannequin accounts cannot review).
const prTimelineSelection = `
timelineItems(first: 250, itemTypes: [READY_FOR_REVIEW_EVENT, CONVERT_TO_DRAFT_EVENT, REVIEW_REQUESTED_EVENT]) {
  nodes {
    __typename
    ... on ReadyForReviewEvent { actor { __typename login } createdAt }
    ... on ConvertToDraftEvent { actor { __typename login } createdAt }
    ... on ReviewRequestedEvent { actor { __typename login } createdAt requestedReviewer { ... on User { __typename login } ... on Bot { __typename login } } }
  }
}`

// prReviewRequestsSelection is the current pending review requests. Unlike
// the timeline's requestedReviewer (which the stats command treats as
// user-only activity), this connection includes teams and reflects removals,
// so it is the source for the reviewers shown on a pull request.
const prReviewRequestsSelection = `
reviewRequests(first: 100) {
  nodes {
    requestedReviewer {
      __typename
      ... on User { login }
      ... on Bot { login }
      ... on Mannequin { login }
      ... on Team { name slug }
      ... on EnterpriseTeam { name slug }
    }
  }
}`

const getPRQuery = `
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      number title state isDraft reviewDecision
      author { __typename login }
      assignees(first: 10) { nodes { login } }
      labels(first: 20) { nodes { name color } }
      milestone { number title }
      baseRefName headRefName headRefOid
      createdAt updatedAt mergedAt closedAt url body
      additions deletions
      reviews(first: 100) {
        nodes { author { __typename login } state submittedAt }
      }
      ` + prTimelineSelection + `
      ` + prReviewRequestsSelection + `
      ` + prChecksSelection + `
      ` + prReviewThreadsSelection + `
      comments(first: 100) {
        nodes { id author { __typename login } body createdAt updatedAt url }
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
        author { __typename login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        baseRefName headRefName headRefOid
        createdAt updatedAt mergedAt closedAt url body
        ` + prChecksSelection + `
        comments { totalCount }
      }
    }
  }
}`

const fetchAllPRsQuery = `
query($owner: String!, $repo: String!, $after: String, $dir: OrderDirection!, $first: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $first, states: [OPEN, CLOSED, MERGED], after: $after, orderBy: {field: UPDATED_AT, direction: $dir}) {
      totalCount
      pageInfo { hasNextPage endCursor }
      nodes {
        number title state isDraft reviewDecision
        author { __typename login }
        assignees(first: 10) { nodes { login } }
        labels(first: 20) { nodes { name color } }
        milestone { number title }
        baseRefName headRefName headRefOid
        createdAt updatedAt mergedAt closedAt url body
        additions deletions
        reviews(first: 100) {
          nodes { author { __typename login } state submittedAt }
        }
` + prTimelineSelection + `
` + prReviewRequestsSelection + `
` + prChecksSelection + `
` + prReviewThreadsSelection + `
        comments(first: 100) {
          totalCount
          nodes { id author { __typename login } body createdAt updatedAt url }
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

		if err := c.QueryCtx(CallContext{
			Operation: "list_prs",
			Kind:      telemetry.KindPRList,
			Repo:      repoKey(owner, repo),
			PageSize:  pageSize,
			Cursor:    cursor != "",
		}, listPRsQuery, vars, &result); err != nil {
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

		if err := c.QueryCtx(CallContext{
			Operation: "search_prs",
			Kind:      telemetry.KindPRSearch,
			Repo:      repoKey(owner, repo),
			PageSize:  pageSize,
			Cursor:    cursor != "",
		}, searchPRsQuery, vars, &result); err != nil {
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

	if err := c.QueryCtx(CallContext{
		Operation: "get_pr",
		Kind:      telemetry.KindPRGet,
		Repo:      repoKey(owner, repo),
	}, getPRQuery, vars, &result); err != nil {
		return nil, err
	}
	if result.Repository.PullRequest == nil {
		return nil, fmt.Errorf("pull request #%d not found", number)
	}
	return nodeToPR(result.Repository.PullRequest), nil
}

// prChecksPageSize is the most check contexts one detail read lists. It is not
// part of the cached record because a hundred contexts per pull request would
// dominate a repository-wide sync; GetPRChecks reads them on demand.
const prChecksPageSize = 100

var prChecksQuery = fmt.Sprintf(`
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      commits(last: 1) {
        nodes {
          commit {
            statusCheckRollup {
              state
              contexts(first: %d) {
                totalCount
                nodes {
                  __typename
                  ... on CheckRun { name status conclusion detailsUrl }
                  ... on StatusContext { context state description targetUrl }
                }
              }
            }
          }
        }
      }
    }
  }
}`, prChecksPageSize)

// GetPRChecks reads a pull request's head-commit rollup with each check that
// reported on it, at most prChecksPageSize of them. A pull request without a
// rollup returns an empty state and no checks.
func (c *Client) GetPRChecks(owner, repo string, number int) (*PRCheckRollup, error) {
	vars := map[string]interface{}{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	}

	var result struct {
		Repository struct {
			PullRequest *struct {
				Commits struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup *struct {
								State    string `json:"state"`
								Contexts struct {
									TotalCount int                      `json:"totalCount"`
									Nodes      []statusCheckContextNode `json:"nodes"`
								} `json:"contexts"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	if err := c.QueryCtx(CallContext{
		Operation: "get_pr_checks",
		Kind:      telemetry.KindPRGet,
		Repo:      repoKey(owner, repo),
	}, prChecksQuery, vars, &result); err != nil {
		return nil, err
	}
	if result.Repository.PullRequest == nil {
		return nil, fmt.Errorf("pull request #%d not found", number)
	}

	rollup := &PRCheckRollup{Checks: []PRCheck{}}
	commits := result.Repository.PullRequest.Commits.Nodes
	if len(commits) == 0 || commits[0].Commit.StatusCheckRollup == nil {
		return rollup, nil
	}
	status := commits[0].Commit.StatusCheckRollup
	rollup.State = status.State
	rollup.Total = status.Contexts.TotalCount
	for _, n := range status.Contexts.Nodes {
		check := PRCheck{
			Type:        n.Typename,
			Name:        n.Name,
			Status:      n.Status,
			Conclusion:  n.Conclusion,
			State:       n.State,
			Description: n.Description,
			URL:         n.DetailsURL,
		}
		if n.Typename == "StatusContext" {
			check.Name = n.Context
			check.URL = n.TargetURL
		}
		if check.Name == "" {
			continue
		}
		rollup.Checks = append(rollup.Checks, check)
	}
	if rollup.Total < len(rollup.Checks) {
		rollup.Total = len(rollup.Checks)
	}
	return rollup, nil
}

// FetchAllPRs retrieves every pull request (all states) with comments for
// caching, oldest-updated-first. It is the cold path of FetchPRsAsc and exists
// for callers that want one complete pass and no resume position.
func (c *Client) FetchAllPRs(owner, repo string, onBatch PRBatchFunc) ([]*PullRequest, error) {
	return c.FetchPRsAsc(owner, repo, "", func(batch []*PullRequest, total int, _ string, _ bool) error {
		if onBatch == nil {
			return nil
		}
		return onBatch(batch, total)
	})
}

// FetchPRsAsc retrieves pull requests in updatedAt ascending order, starting
// after the given page cursor. It is the cache walk:
//
//   - A cold fetch starts with an empty cursor and reads from the oldest
//     updatedAt forward.
//   - A resumed fetch passes the endCursor persisted after the last fully
//     written page, so an interrupted backfill continues instead of starting
//     over.
//   - A completed cache passes its tail cursor on every run, so the walk
//     returns only the pull requests updated since that position.
//
// Ascending order is what makes the cursor resumable: an item updated while the
// walk is paused moves to the end of the list, where the walk re-reads it, and
// an unfetched item can only move further forward. Nothing is skipped.
//
// Cursors are opaque and GitHub documents them for use within one traversal.
// If the stored cursor is rejected on the first request, the walk restarts once
// from the beginning and logs it. onPage is invoked per page so callers can
// persist items and the page cursor incrementally; a non-nil error aborts the
// walk.
func (c *Client) FetchPRsAsc(owner, repo, after string, onPage PRPageFunc) ([]*PullRequest, error) {
	var prs []*PullRequest
	err := c.WalkPRsAsc(owner, repo, after, func(batch []*PullRequest, total int, next string, more bool) error {
		prs = append(prs, batch...)
		if onPage != nil {
			return onPage(batch, total, next, more)
		}
		return nil
	})
	return prs, err
}

// WalkPRsAsc is FetchPRsAsc without retaining the pages it fetches, for callers
// that persist each page as it arrives. A full history backfill would otherwise
// hold every pull request in memory at once.
func (c *Client) WalkPRsAsc(owner, repo, after string, onPage PRPageFunc) error {
	return c.WalkPRsAscUntil(owner, repo, after, time.Time{}, onPage)
}

// WalkPRsAscUntil is WalkPRsAsc with an exclusive upper bound on updatedAt.
// Pages are filtered to items before stopAt, and the walk stops as soon as it
// reaches the bound. A zero stopAt walks to the head. Unlike the unbounded
// walk, a rejected starting cursor is not replaced with a restart from the
// beginning: callers use this for one coverage segment, and restarting would
// cross into other segments.
func (c *Client) WalkPRsAscUntil(owner, repo, after string, stopAt time.Time, onPage PRPageFunc) error {
	cursor := after
	total := 0
	restarted := false
	stopAt = stopAt.UTC().Truncate(time.Second)

	for {
		vars := map[string]interface{}{
			"owner": owner,
			"repo":  repo,
			"dir":   "ASC",
			"first": 50,
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

		err := c.QueryCtx(CallContext{
			Operation: "fetch_prs_asc",
			Kind:      telemetry.KindPRFull,
			Repo:      repoKey(owner, repo),
			PageSize:  50,
			Cursor:    cursor != "",
		}, fetchAllPRsQuery, vars, &result)
		if err != nil {
			// A stored cursor can outlive the traversal that produced it. Only
			// the unbounded walk may recover by restarting from the beginning;
			// a bounded segment must not leave its range.
			if stopAt.IsZero() && !restarted && after != "" && cursor == after && isCursorRejection(err) {
				restarted = true
				cursor = ""
				logging.Warn("stored pull request cursor was rejected; restarting the walk from the beginning",
					"repo", repoKey(owner, repo), "error", err)
				continue
			}
			return err
		}

		if total == 0 {
			total = result.Repository.PullRequests.TotalCount
		}

		var batch []*PullRequest
		for i := range result.Repository.PullRequests.Nodes {
			batch = append(batch, nodeToPR(&result.Repository.PullRequests.Nodes[i]))
		}

		next := result.Repository.PullRequests.PageInfo.EndCursor
		hasNext := result.Repository.PullRequests.PageInfo.HasNextPage
		stopped := false
		if !stopAt.IsZero() {
			kept := batch[:0]
			for _, pr := range batch {
				if !pr.UpdatedAt.Before(stopAt) {
					stopped = true
					continue
				}
				kept = append(kept, pr)
			}
			batch = kept
			hasNext = hasNext && !stopped
		}
		if onPage != nil {
			if err := onPage(batch, total, next, hasNext); err != nil {
				return err
			}
		}

		if !hasNext {
			break
		}
		cursor = next
	}

	return nil
}

// isCursorRejection reports whether err looks like GitHub refusing an opaque
// pagination cursor, so the walk can restart from the head. Matching on the
// message is imprecise, but it is only consulted for a stored cursor on the
// first request of a walk.
func isCursorRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cursor") &&
		(strings.Contains(msg, "invalid") || strings.Contains(msg, "expired") || strings.Contains(msg, "not valid"))
}

// SearchCount returns the search index's count of items of kind ("issues" or
// "prs") updated in the given range. It sizes progress rows for coverage gaps.
// GitHub search ranges are inclusive at both ends, so an item exactly on a
// boundary can be counted in adjacent queries.
func (c *Client) SearchCount(owner, repo, kind string, start, end time.Time) (int, error) {
	term := "is:pr"
	if kind == "issues" {
		term = "is:issue"
	}
	var updated string
	switch {
	case !start.IsZero() && !end.IsZero():
		updated = fmt.Sprintf("%s..%s", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	case !start.IsZero():
		updated = ">=" + start.UTC().Format(time.RFC3339)
	case !end.IsZero():
		updated = "<=" + end.UTC().Format(time.RFC3339)
	default:
		return 0, nil
	}
	q := fmt.Sprintf("repo:%s/%s %s updated:%s", owner, repo, term, updated)
	var result struct {
		Search struct {
			IssueCount int `json:"issueCount"`
		} `json:"search"`
	}
	if err := c.QueryCtx(CallContext{
		Operation: "search_count",
		Kind:      telemetry.KindPRSearch,
		Repo:      repoKey(owner, repo),
	}, countPRsQuery, map[string]interface{}{"q": q}, &result); err != nil {
		return 0, err
	}
	return result.Search.IssueCount, nil
}

const countPRsQuery = `
query($q: String!) {
  search(query: $q, type: ISSUE, first: 1) {
    issueCount
  }
}`

// CountIssues returns the total number of issues in the repository, without a
// state or date filter. It sizes the cache figure in progress rows, which is
// distinct from the server-filtered count a --since window fetches.
func (c *Client) CountIssues(owner, repo string) (int, error) {
	var result struct {
		Repository struct {
			Issues struct {
				TotalCount int `json:"totalCount"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := c.QueryCtx(CallContext{
		Operation: "count_issues",
		Kind:      telemetry.KindIssueList,
		Repo:      repoKey(owner, repo),
	}, countIssuesQuery, map[string]interface{}{"owner": owner, "repo": repo}, &result); err != nil {
		return 0, err
	}
	return result.Repository.Issues.TotalCount, nil
}

const countIssuesQuery = `
query($owner: String!, $repo: String!) {
  repository(owner: $owner, name: $repo) {
    issues(first: 1) {
      totalCount
    }
  }
}`

// prUpdatedAfter returns the updatedAt timestamps of up to first pull requests
// after the cursor in the given direction, using the light two-field selection.
func (c *Client) prUpdatedAfter(owner, repo, after string, first int, dir string) ([]time.Time, error) {
	vars := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
		"first": first,
		"dir":   dir,
	}
	if after != "" {
		vars["after"] = after
	}
	var result struct {
		Repository struct {
			PullRequests struct {
				Nodes []struct {
					UpdatedAt time.Time `json:"updatedAt"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := c.QueryCtx(CallContext{
		Operation: "probe_prs_after",
		Kind:      telemetry.KindPRScan,
		Repo:      repoKey(owner, repo),
		PageSize:  first,
		Cursor:    after != "",
	}, fetchPRsUpdatedLightQuery, vars, &result); err != nil {
		return nil, err
	}
	nodes := result.Repository.PullRequests.Nodes
	out := make([]time.Time, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.UpdatedAt)
	}
	return out, nil
}

// OldestPRUpdatedAt returns the oldest pull request updatedAt, used as the
// default start for a full coverage backfill.
func (c *Client) OldestPRUpdatedAt(owner, repo string) (time.Time, error) {
	ts, err := c.prUpdatedAfter(owner, repo, "", 1, "ASC")
	if err != nil {
		return time.Time{}, err
	}
	if len(ts) == 0 {
		return time.Time{}, nil
	}
	return ts[0], nil
}

const oldestIssueQuery = `
query($owner: String!, $repo: String!) {
  repository(owner: $owner, name: $repo) {
    issues(first: 1, states: [OPEN, CLOSED], orderBy: {field: UPDATED_AT, direction: ASC}) {
      nodes { updatedAt }
    }
  }
}`

// OldestIssueUpdatedAt returns the oldest issue updatedAt, used as the default
// start for a full coverage backfill.
func (c *Client) OldestIssueUpdatedAt(owner, repo string) (time.Time, error) {
	var result struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					UpdatedAt time.Time `json:"updatedAt"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := c.QueryCtx(CallContext{
		Operation: "oldest_issue",
		Kind:      telemetry.KindIssueList,
		Repo:      repoKey(owner, repo),
	}, oldestIssueQuery, map[string]interface{}{"owner": owner, "repo": repo}, &result); err != nil {
		return time.Time{}, err
	}
	if len(result.Repository.Issues.Nodes) == 0 {
		return time.Time{}, nil
	}
	return result.Repository.Issues.Nodes[0].UpdatedAt, nil
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
// read-only windowed reports such as `ghx stats`. Caching no longer uses this
// path: the cache resumes from the stored ascending page cursor (FetchPRsAsc),
// which needs no newest-first walk. It runs in two phases:
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
//     block is fetched as a chain of pages seeded by the block's cursor;
//     chains run prBlocksInFlight at a time in parallel. Each page is sized
//     to the in-window items its block is expected to hold, so a window with
//     one changed PR costs one small page instead of a full one.
//
// onBatch is invoked per completed page (in completion order; the walk is
// order-independent) with total reporting the exact number of in-window PRs so
// callers can render a determinate progress bar. A non-nil error from onBatch
// aborts the fetch.
func (c *Client) FetchPRsUpdated(owner, repo string, since time.Time, onBatch PRBatchFunc) ([]*PullRequest, error) {
	// Phase 1: locate the window with the light query, recording each
	// block's start cursor (the light page's own start cursor) so phase 2
	// can fetch blocks concurrently.
	var candidateCount int
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

		if err := c.QueryCtx(CallContext{
			Operation: "fetch_prs_updated_scan",
			Kind:      telemetry.KindPRScan,
			Repo:      repoKey(owner, repo),
			PageSize:  prScanPageSize,
			Cursor:    cursor != "",
		}, fetchPRsUpdatedLightQuery, vars, &result); err != nil {
			return nil, err
		}

		page := result.Repository.PullRequests.Nodes
		var pageOldest time.Time
		count := 0
		for _, n := range page {
			if pageOldest.IsZero() || n.UpdatedAt.Before(pageOldest) {
				pageOldest = n.UpdatedAt
			}
			if !n.UpdatedAt.Before(since) {
				count++
			}
		}
		blockCandidates = append(blockCandidates, count)
		candidateCount += count

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
	results := make(chan pageResult, blocks*(prPagesPerBlock+2)) // buffered: workers never block
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
				remaining := blockCandidates[blockIdx]
				// Size each page to the number of in-window items this block
				// is expected to hold, plus one lookahead node so the last
				// page also reveals whether the cutoff was passed without an
				// extra request. A small delta (often just the boundary item
				// at the cutoff) therefore fetches a small page instead of a
				// full one. The walk still stops on the timestamp cutoff
				// rather than on this estimate, so ties and items updated
				// between the scan and this fetch are not dropped.
				for page := 0; page <= prPagesPerBlock; page++ {
					if abort.Load() {
						break
					}
					first := remaining + 1
					if first > prFullPageSize {
						first = prFullPageSize
					}
					if first < 1 {
						first = 1
					}
					res, err := c.fetchPRPage(owner, repo, cursor, since, first)
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
					remaining -= len(res.batch)
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

// fetchPRPage fetches one full-payload page of at most first PRs starting at
// the given after-cursor and returns those updated at or after since.
func (c *Client) fetchPRPage(owner, repo, after string, since time.Time, first int) (prPage, error) {
	vars := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
		"first": first,
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

	if err := c.QueryCtx(CallContext{
		Operation: "fetch_pr_full_page",
		Kind:      telemetry.KindPRFullPage,
		Repo:      repoKey(owner, repo),
		PageSize:  first,
		Cursor:    after != "",
	}, fetchAllPRsQuery, vars, &result); err != nil {
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

const countPRsTotalQuery = `
query($owner: String!, $repo: String!) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: 1, states: [OPEN, CLOSED, MERGED]) {
      totalCount
    }
  }
}`

// CountPRs returns the total number of pull requests in the repository.
func (c *Client) CountPRs(owner, repo string) (int, error) {
	var result struct {
		Repository struct {
			PullRequests struct {
				TotalCount int `json:"totalCount"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := c.QueryCtx(CallContext{
		Operation: "count_prs",
		Kind:      telemetry.KindPRList,
		Repo:      repoKey(owner, repo),
	}, countPRsTotalQuery, map[string]interface{}{"owner": owner, "repo": repo}, &result); err != nil {
		return 0, err
	}
	return result.Repository.PullRequests.TotalCount, nil
}
