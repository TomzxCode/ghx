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
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	IsDraft        bool       `json:"isDraft"`
	Author         Actor      `json:"author"`
	Assignees      []Actor    `json:"assignees"`
	Labels         []Label    `json:"labels"`
	Milestone      *Milestone `json:"milestone,omitempty"`
	BaseRefName    string     `json:"baseRefName"`
	HeadRefName    string     `json:"headRefName"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	MergedAt       *time.Time `json:"mergedAt,omitempty"`
	ClosedAt       *time.Time `json:"closedAt,omitempty"`
	URL            string     `json:"url"`
	Body           string     `json:"body"`
	CommentCount   int        `json:"commentCount"`
	Comments       []Comment  `json:"comments"`
	ReviewDecision string     `json:"reviewDecision,omitempty"`
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

// Actor is a GitHub user or bot.
type Actor struct {
	Login string `json:"login"`
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
