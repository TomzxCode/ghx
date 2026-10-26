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
