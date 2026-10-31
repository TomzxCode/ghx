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
	PRCursor    *time.Time `json:"prCursor,omitempty"`    // max updatedAt written for PRs (frontier)

	// PRPageCursor is the GraphQL endCursor of the last fully written page of
	// the ascending pull request walk. The pullRequests connection has no
	// server-side date filter, so this page cursor is the resume position: an
	// interrupted backfill continues from it, and a completed cache reads only
	// the pages after it on later runs.
	PRPageCursor string `json:"prPageCursor,omitempty"`

	// PRFloor is the oldest updatedAt the pull request cache promises to cover.
	// It is set when a walk starts at a requested date (--since) instead of the
	// beginning of the repository's history. Nil means coverage is unbounded
	// below.
	PRFloor *time.Time `json:"prFloor,omitempty"`

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

// DefaultItemFreshness is the window in which a single cached issue or pull
// request is served without a refetch when the repository has no cache
// metadata, for example a cache populated only by view and list reads.
const DefaultItemFreshness = 60 * time.Minute

// IsItemFresh reports whether a cached issue or pull request last written at
// cachedAt may be served without a refetch. The window is the repository's
// configured cache duration, so an item refreshed within that window is a hit
// even when the repository's cache as a whole is stale. A repository with no
// cache metadata falls back to DefaultItemFreshness, and a non-positive
// duration (the cache is never fresh) makes every item stale.
func IsItemFresh(store Store, host, owner, repo string, cachedAt time.Time) bool {
	window := DefaultItemFreshness
	if info, err := store.LoadCacheInfo(host, owner, repo); err == nil && info != nil {
		window = time.Duration(info.Duration) * time.Minute
	}
	if window <= 0 {
		return false
	}
	return time.Since(cachedAt) < window
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
	// DeleteRepo removes every cached item and the cache metadata for one
	// repository. It reports whether anything was deleted, so callers can
	// distinguish "removed" from "nothing cached" without a prior read.
	DeleteRepo(host, owner, repo string) (bool, error)

	// DeleteType removes every cached item of one kind ("issues" or "prs") and
	// its coverage. It reports how many item rows were deleted.
	DeleteType(host, owner, repo, kind string) (int, error)
	// SaveSegments records covered half-open updatedAt ranges for one kind
	// ("issues" or "prs"), merging them with the existing coverage.
	SaveSegments(host, owner, repo, kind string, segments []Segment) error
	// LoadSegments returns the merged coverage segments for one kind.
	LoadSegments(host, owner, repo, kind string) ([]Segment, error)
	// DeleteSegments clears the coverage for one kind.
	DeleteSegments(host, owner, repo, kind string) error

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

// NewStoreWithPath opens the file-based cache store rooted at baseDir. On first
// open it normalizes any mixed-case repository directories, then wraps the store
// so every key is canonical.
func NewStoreWithPath(baseDir string) Store {
	normalizeFileKeys(baseDir)
	return normalize(&fileStore{baseDir: baseDir})
}
