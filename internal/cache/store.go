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
	ListCachedRepos() ([]CachedRepo, error)
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
