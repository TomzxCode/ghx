package cache

import (
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

// normalizeRepoKey lowercases repository coordinates. GitHub owner and
// repository names are case-insensitive, so the cache keys on the lower-case
// form: a repository has one entry however its name was spelled, and a lookup
// cannot miss because of casing.
func normalizeRepoKey(host, owner, repo string) (string, string, string) {
	return strings.ToLower(host), strings.ToLower(owner), strings.ToLower(repo)
}

// normalizeStore wraps a backend so every operation uses normalized
// coordinates. Canonicalizing in one place means no caller (CLI, web UI,
// migration) can create or read a duplicate entry by varying the casing.
//
// It mirrors the Store interface like InstrumentedStore does, so a backend
// method never has to remember to normalize.
type normalizeStore struct {
	store Store
}

// normalize wraps store so its coordinates are canonicalized. A nil store is
// returned unchanged.
func normalize(store Store) Store {
	if store == nil {
		return store
	}
	return &normalizeStore{store: store}
}

// SaveIssue records the issue under the normalized repository key.
func (s *normalizeStore) SaveIssue(host, owner, repo string, issue *github.Issue) error {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.SaveIssue(host, owner, repo, issue)
}

// LoadIssue reads the issue under the normalized repository key.
func (s *normalizeStore) LoadIssue(host, owner, repo string, number int) (*github.Issue, time.Time, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.LoadIssue(host, owner, repo, number)
}

// LoadAllIssues reads the issues under the normalized repository key.
func (s *normalizeStore) LoadAllIssues(host, owner, repo string) ([]*github.Issue, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.LoadAllIssues(host, owner, repo)
}

// QueryIssues reads the matching issues under the normalized repository key.
func (s *normalizeStore) QueryIssues(host, owner, repo string, q IssueQuery) ([]*github.Issue, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.QueryIssues(host, owner, repo, q)
}

// SavePR records the pull request under the normalized repository key.
func (s *normalizeStore) SavePR(host, owner, repo string, pr *github.PullRequest) error {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.SavePR(host, owner, repo, pr)
}

// LoadPR reads the pull request under the normalized repository key.
func (s *normalizeStore) LoadPR(host, owner, repo string, number int) (*github.PullRequest, time.Time, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.LoadPR(host, owner, repo, number)
}

// LoadAllPRs reads the pull requests under the normalized repository key.
func (s *normalizeStore) LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.LoadAllPRs(host, owner, repo)
}

// QueryPRs reads the matching pull requests under the normalized repository key.
func (s *normalizeStore) QueryPRs(host, owner, repo string, q PRQuery) ([]*github.PullRequest, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.QueryPRs(host, owner, repo, q)
}

// SaveCacheInfo writes the metadata under the normalized repository key.
func (s *normalizeStore) SaveCacheInfo(host, owner, repo string, duration int) error {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.SaveCacheInfo(host, owner, repo, duration)
}

// SaveCacheInfoFull writes the metadata under the normalized repository key.
func (s *normalizeStore) SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.SaveCacheInfoFull(host, owner, repo, info)
}

// LoadCacheInfo reads the metadata under the normalized repository key.
func (s *normalizeStore) LoadCacheInfo(host, owner, repo string) (*CacheInfo, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.LoadCacheInfo(host, owner, repo)
}

// IsCacheFresh checks freshness under the normalized repository key.
func (s *normalizeStore) IsCacheFresh(host, owner, repo string) (bool, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.IsCacheFresh(host, owner, repo)
}

// IsCacheFreshWithDuration checks freshness under the normalized repository key.
func (s *normalizeStore) IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.IsCacheFreshWithDuration(host, owner, repo, duration)
}

// IsIssuesCacheFresh checks issue freshness under the normalized repository key.
func (s *normalizeStore) IsIssuesCacheFresh(host, owner, repo string) (bool, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.IsIssuesCacheFresh(host, owner, repo)
}

// IsPRsCacheFresh checks pull-request freshness under the normalized key.
func (s *normalizeStore) IsPRsCacheFresh(host, owner, repo string) (bool, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.IsPRsCacheFresh(host, owner, repo)
}

// IsIssuesCacheFreshWithDuration checks issue freshness under the normalized key.
func (s *normalizeStore) IsIssuesCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.IsIssuesCacheFreshWithDuration(host, owner, repo, duration)
}

// IsPRsCacheFreshWithDuration checks pull-request freshness under the normalized key.
func (s *normalizeStore) IsPRsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.IsPRsCacheFreshWithDuration(host, owner, repo, duration)
}

// DeleteRepo removes the cache under the normalized repository key.
func (s *normalizeStore) DeleteRepo(host, owner, repo string) (bool, error) {
	host, owner, repo = normalizeRepoKey(host, owner, repo)
	return s.store.DeleteRepo(host, owner, repo)
}

// ListCachedRepos returns every cached repository with normalized coordinates,
// so a backend that still holds mixed-case keys cannot surface a duplicate.
func (s *normalizeStore) ListCachedRepos() ([]CachedRepo, error) {
	repos, err := s.store.ListCachedRepos()
	if err != nil {
		return nil, err
	}
	for i := range repos {
		repos[i].Host, repos[i].Owner, repos[i].Repo = normalizeRepoKey(repos[i].Host, repos[i].Owner, repos[i].Repo)
	}
	return repos, nil
}

// Kind delegates to the wrapped backend.
func (s *normalizeStore) Kind() string { return s.store.Kind() }

// Location delegates to the wrapped backend.
func (s *normalizeStore) Location() string { return s.store.Location() }

// Close delegates to the wrapped backend.
func (s *normalizeStore) Close() error { return s.store.Close() }

// Ensure normalizeStore satisfies Store at compile time.
var _ Store = (*normalizeStore)(nil)
