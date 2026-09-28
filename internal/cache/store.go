package cache

import (
	"os"
	"path/filepath"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

// CacheInfo records the state of the on-disk cache for a repository.
type CacheInfo struct {
	CachedAt    time.Time  `json:"cachedAt"`
	Duration    int        `json:"duration"`              // minutes
	Complete    bool       `json:"complete,omitempty"`    // last full fetch reached the newest item
	IssueCursor *time.Time `json:"issueCursor,omitempty"` // max updatedAt written for issues (resume point)
	PRCursor    *time.Time `json:"prCursor,omitempty"`    // max updatedAt written for PRs (resume point)

	// IssuesCachedAt and PRsCachedAt record when each data type was last
	// refreshed. A partial run (--type issues|prs) updates only the type it
	// fetched, so reads can be served from the cache for that type without
	// pretending the other type is fresh. When zero they fall back to
	// CachedAt, preserving the legacy on-disk format and full-run behavior.
	IssuesCachedAt time.Time `json:"issuesCachedAt,omitempty"`
	PRsCachedAt    time.Time `json:"prsCachedAt,omitempty"`
}

// IssuesUpdatedAt returns the timestamp used to judge issue freshness.
func (i *CacheInfo) IssuesUpdatedAt() time.Time {
	if !i.IssuesCachedAt.IsZero() {
		return i.IssuesCachedAt
	}
	return i.CachedAt
}

// PRsUpdatedAt returns the timestamp used to judge pull request freshness.
func (i *CacheInfo) PRsUpdatedAt() time.Time {
	if !i.PRsCachedAt.IsZero() {
		return i.PRsCachedAt
	}
	return i.CachedAt
}

// CachedRepo describes a repository found in the local cache.
type CachedRepo struct {
	Host       string
	Owner      string
	Repo       string
	Info       *CacheInfo
	IssueCount int
	PRCount    int
}

// IssueQuery describes the filters applied when reading cached issues. An empty
// field means "no filter"; State "all" or "" disables the state filter.
type IssueQuery struct {
	State     string
	Assignee  string
	Author    string
	Labels    []string
	Milestone string
	Search    string
}

// PRQuery describes the filters applied when reading cached pull requests. An
// empty field means "no filter"; State "all" or "" disables the state filter.
type PRQuery struct {
	State    string
	Assignee string
	Author   string
	Labels   []string
	BaseRef  string
	HeadRef  string
	Draft    bool
	Search   string
}

// Store is the cache storage backend. All methods are keyed by the repository
// coordinates (host, owner, repo) so a single backend can hold many
// repositories.
//
// The current implementation is fileStore (one JSON file per item). Additional
// backends implement this same interface so callers depend on the interface
// rather than a concrete store.
type Store interface {
	SaveIssue(host, owner, repo string, issue *github.Issue) error
	LoadIssue(host, owner, repo string, number int) (*github.Issue, time.Time, error)
	LoadAllIssues(host, owner, repo string) ([]*github.Issue, error)
	SavePR(host, owner, repo string, pr *github.PullRequest) error
	LoadPR(host, owner, repo string, number int) (*github.PullRequest, time.Time, error)
	LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error)
	SaveCacheInfo(host, owner, repo string, duration int) error
	SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error
	LoadCacheInfo(host, owner, repo string) (*CacheInfo, error)
	IsCacheFresh(host, owner, repo string) (bool, error)
	IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error)
	IsIssuesCacheFresh(host, owner, repo string) (bool, error)
	IsPRsCacheFresh(host, owner, repo string) (bool, error)
	IsIssuesCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error)
	IsPRsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error)
	ListCachedRepos() ([]CachedRepo, error)

	// QueryIssues returns the cached issues matching q. Backends may push the
	// scalar predicates to indexed storage, but the returned set is identical
	// to filtering LoadAllIssues with q.
	QueryIssues(host, owner, repo string, q IssueQuery) ([]*github.Issue, error)
	// QueryPRs is the pull-request equivalent of QueryIssues.
	QueryPRs(host, owner, repo string, q PRQuery) ([]*github.PullRequest, error)

	// Kind reports the backend name ("file" or "sqlite").
	Kind() string
	// Location reports where the backend stores its data (file: cache root;
	// sqlite: database path).
	Location() string
	Close() error
}

// DefaultDir returns the default cache directory (~/.cache/ghx/cache).
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".cache", "ghx", "cache")
}

// NewStore opens the file-based cache store using the default cache directory.
func NewStore() Store {
	return NewStoreWithPath(DefaultDir())
}

// NewStoreWithPath opens the file-based cache store rooted at baseDir.
func NewStoreWithPath(baseDir string) Store {
	return &fileStore{baseDir: baseDir}
}
