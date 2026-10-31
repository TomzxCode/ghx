package github

import "time"

// Issue represents a GitHub issue as stored on disk and used throughout the app.
type Issue struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	State        string     `json:"state"`
	Author       Actor      `json:"author"`
	Assignees    []Actor    `json:"assignees"`
	Labels       []Label    `json:"labels"`
	Milestone    *Milestone `json:"milestone,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	ClosedAt     *time.Time `json:"closedAt,omitempty"`
	URL          string     `json:"url"`
	Body         string     `json:"body"`
	CommentCount int        `json:"commentCount"`
	Comments     []Comment  `json:"comments"`
}

// PullRequest represents a GitHub pull request as stored on disk.
type PullRequest struct {
	Number         int             `json:"number"`
	Title          string          `json:"title"`
	State          string          `json:"state"`
	IsDraft        bool            `json:"isDraft"`
	Author         Actor           `json:"author"`
	Assignees      []Actor         `json:"assignees"`
	Labels         []Label         `json:"labels"`
	Milestone      *Milestone      `json:"milestone,omitempty"`
	BaseRefName    string          `json:"baseRefName"`
	HeadRefName    string          `json:"headRefName"`
	HeadRefOid     string          `json:"headRefOid"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	MergedAt       *time.Time      `json:"mergedAt,omitempty"`
	ClosedAt       *time.Time      `json:"closedAt,omitempty"`
	URL            string          `json:"url"`
	Body           string          `json:"body"`
	CommentCount   int             `json:"commentCount"`
	Comments       []Comment       `json:"comments"`
	ReviewDecision string          `json:"reviewDecision,omitempty"`
	Additions      int             `json:"additions"`
	Deletions      int             `json:"deletions"`
	Reviews        []PRReview      `json:"reviews,omitempty"`
	Timeline       []TimelineEvent `json:"timeline,omitempty"`
	// ReviewRequests is the set of reviews GitHub currently waits on. It is
	// nil when the record predates the field (an older cache row), and empty
	// (non-nil) when the PR has no pending review requests.
	ReviewRequests []ReviewRequest `json:"reviewRequests"`
	// Checks is the head commit's check rollup. It is nil when the record
	// predates the field (an older cache row); a record with no checks on its
	// head commit carries a non-nil rollup with an empty state.
	Checks *PRChecks `json:"checks,omitempty"`
	// ReviewThreads counts the review threads, and how many are still open.
	// It is nil when the record predates the field. Open counts the threads a
	// single read carried (the first hundred), so a busier pull request
	// undercounts it; Total comes from the connection and stays exact.
	ReviewThreads *PRThreadCounts `json:"reviewThreads,omitempty"`
}

// PRChecks is the head commit's check rollup: the aggregate state GitHub
// reports and how many check contexts the commit carries. The contexts
// themselves are not cached; GetPRChecks reads them on demand.
type PRChecks struct {
	// State is GitHub's rollup state (SUCCESS, FAILURE, ERROR, PENDING,
	// EXPECTED), or empty when the commit has no checks.
	State string `json:"state"`
	// Total is every check context on the commit, which can exceed the ones
	// a detail read lists.
	Total int `json:"total"`
}

// PRThreadCounts is how many review threads a pull request carries and how
// many of them are still open.
type PRThreadCounts struct {
	Total int `json:"total"`
	Open  int `json:"open"`
}

// PRCheck is one check run or commit status reporting on a pull request's head
// commit. CheckRun entries carry Status and Conclusion; StatusContext entries
// carry State.
type PRCheck struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Status      string `json:"status,omitempty"`
	Conclusion  string `json:"conclusion,omitempty"`
	State       string `json:"state,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// PRCheckRollup is a pull request's head-commit rollup with each check that
// reported on it, as GetPRChecks reads it.
type PRCheckRollup struct {
	State  string    `json:"state"`
	Total  int       `json:"total"`
	Checks []PRCheck `json:"checks"`
}

// ReviewRequest is a pending review request on a pull request. Login is set
// for people and bots; Name is set for teams (for example a team slug).
type ReviewRequest struct {
	Type  string `json:"type,omitempty"`
	Login string `json:"login,omitempty"`
	Name  string `json:"name,omitempty"`
}

// PRReview is a single review submitted on a pull request.
type PRReview struct {
	Author      Actor     `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// TimelineEventKind identifies a pull request timeline event tracked for
// statistics.
type TimelineEventKind string

const (
	TimelineReadyForReview   TimelineEventKind = "ready_for_review"
	TimelineConvertedToDraft TimelineEventKind = "converted_to_draft"
	TimelineReviewRequested  TimelineEventKind = "review_requested"
)

// TimelineEvent is a normalized pull request timeline event. GitHub records
// review re-requests as additional ReviewRequestedEvent entries rather than a
// distinct type; the stats command treats later request events for a reviewer
// who already reviewed as re-requests.
type TimelineEvent struct {
	Kind              TimelineEventKind `json:"kind"`
	Actor             Actor             `json:"actor"`
	RequestedReviewer Actor             `json:"requestedReviewer,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
}

// Comment is a single issue or PR comment.
type Comment struct {
	ID        string    `json:"id"`
	Author    Actor     `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

// Actor is a GitHub user or bot. Type is the GraphQL __typename of the
// account (User, Bot, Organization, Mannequin or EnterpriseUserAccount) and is
// empty for data cached before the type was requested.
type Actor struct {
	Login string `json:"login"`
	Type  string `json:"type,omitempty"`
}

// Label is a GitHub label.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Milestone is a GitHub milestone.
type Milestone struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// IssueBatchFunc is invoked for each page of issues during a cache fetch.
// batch is the page's issues; total is the server-reported total (0 when
// unknown). A non-nil error aborts the fetch.
type IssueBatchFunc func(batch []*Issue, total int) error

// PRBatchFunc is the pull-request equivalent of IssueBatchFunc.
type PRBatchFunc func(batch []*PullRequest, total int) error

// PRPageFunc is invoked for each page of an ascending pull request walk.
// after is the page's endCursor (empty when the response carried none) and
// more reports whether another page follows. A non-nil error aborts the walk.
type PRPageFunc func(batch []*PullRequest, total int, after string, more bool) error
