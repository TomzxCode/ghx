package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// Store manages the on-disk cache at ~/.cache/ghx/cache.
type Store struct {
	baseDir string
}

// NewStore creates a Store using the default cache directory.
func NewStore() *Store {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return &Store{baseDir: filepath.Join(home, ".cache", "ghx", "cache")}
}

// NewStoreWithPath creates a Store using the given base directory.
func NewStoreWithPath(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

func (s *Store) repoDir(host, owner, repo string) string {
	return filepath.Join(s.baseDir, host, owner, repo)
}

func (s *Store) issueDir(host, owner, repo string) string {
	return filepath.Join(s.repoDir(host, owner, repo), "issues")
}

func (s *Store) prDir(host, owner, repo string) string {
	return filepath.Join(s.repoDir(host, owner, repo), "prs")
}

// SaveIssue writes a single issue to disk.
func (s *Store) SaveIssue(host, owner, repo string, issue *github.Issue) error {
	dir := s.issueDir(host, owner, repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating issue dir: %w", err)
	}
	data, err := json.MarshalIndent(issue, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, strconv.Itoa(issue.Number)+".json"), data, 0644)
}

// SavePR writes a single pull request to disk.
func (s *Store) SavePR(host, owner, repo string, pr *github.PullRequest) error {
	dir := s.prDir(host, owner, repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating pr dir: %w", err)
	}
	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, strconv.Itoa(pr.Number)+".json"), data, 0644)
}

// LoadIssue reads a single issue from disk, returning the file's modification time.
func (s *Store) LoadIssue(host, owner, repo string, number int) (*github.Issue, time.Time, error) {
	path := filepath.Join(s.issueDir(host, owner, repo), strconv.Itoa(number)+".json")
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var issue github.Issue
	if err := json.Unmarshal(data, &issue); err != nil {
		return nil, time.Time{}, err
	}
	return &issue, info.ModTime(), nil
}

// LoadPR reads a single pull request from disk, returning the file's modification time.
func (s *Store) LoadPR(host, owner, repo string, number int) (*github.PullRequest, time.Time, error) {
	path := filepath.Join(s.prDir(host, owner, repo), strconv.Itoa(number)+".json")
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var pr github.PullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, time.Time{}, err
	}
	return &pr, info.ModTime(), nil
}

// LoadAllIssues reads every cached issue for a repository.
func (s *Store) LoadAllIssues(host, owner, repo string) ([]*github.Issue, error) {
	dir := s.issueDir(host, owner, repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var issues []*github.Issue
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var issue github.Issue
		if err := json.Unmarshal(data, &issue); err != nil {
			continue
		}
		issues = append(issues, &issue)
	}
	return issues, nil
}

// LoadAllPRs reads every cached pull request for a repository.
func (s *Store) LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error) {
	dir := s.prDir(host, owner, repo)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var prs []*github.PullRequest
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var pr github.PullRequest
		if err := json.Unmarshal(data, &pr); err != nil {
			continue
		}
		prs = append(prs, &pr)
	}
	return prs, nil
}

// SaveCacheInfo writes the cache metadata file, marking the cache as complete
// at the current time with the given duration.
func (s *Store) SaveCacheInfo(host, owner, repo string, duration int) error {
	info := &CacheInfo{
		CachedAt: time.Now(),
		Duration: duration,
		Complete: true,
	}
	return s.SaveCacheInfoFull(host, owner, repo, info)
}

// SaveCacheInfoFull writes the given cache metadata atomically. Use this during
// a fetch to persist resume cursors before the fetch completes; SaveCacheInfo
// (above) is the convenience helper for the success case.
func (s *Store) SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error {
	dir := s.repoDir(host, owner, repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, ".cache_info.json"), data, 0644)
}

// LoadCacheInfo reads the cache metadata file.
func (s *Store) LoadCacheInfo(host, owner, repo string) (*CacheInfo, error) {
	data, err := os.ReadFile(filepath.Join(s.repoDir(host, owner, repo), ".cache_info.json"))
	if err != nil {
		return nil, err
	}
	var info CacheInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// IsCacheFresh reports whether the cache is complete and was populated within
// its stored duration. An interrupted (incomplete) fetch is never fresh.
func (s *Store) IsCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	return time.Since(info.CachedAt) < time.Duration(info.Duration)*time.Minute, nil
}

// IsCacheFreshWithDuration reports whether the cache is complete and was
// populated within the given duration.
func (s *Store) IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	return time.Since(info.CachedAt) < time.Duration(duration)*time.Minute, nil
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

// ListCachedRepos walks the cache directory and returns all cached repositories.
func (s *Store) ListCachedRepos() ([]CachedRepo, error) {
	hosts, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var repos []CachedRepo
	for _, hostEntry := range hosts {
		if !hostEntry.IsDir() {
			continue
		}
		host := hostEntry.Name()
		owners, err := os.ReadDir(filepath.Join(s.baseDir, host))
		if err != nil {
			continue
		}
		for _, ownerEntry := range owners {
			if !ownerEntry.IsDir() {
				continue
			}
			owner := ownerEntry.Name()
			repoEntries, err := os.ReadDir(filepath.Join(s.baseDir, host, owner))
			if err != nil {
				continue
			}
			for _, repoEntry := range repoEntries {
				if !repoEntry.IsDir() {
					continue
				}
				repoName := repoEntry.Name()
				cr := CachedRepo{Host: host, Owner: owner, Repo: repoName}
				cr.Info, _ = s.LoadCacheInfo(host, owner, repoName)
				if entries, err := os.ReadDir(s.issueDir(host, owner, repoName)); err == nil {
					for _, e := range entries {
						if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
							cr.IssueCount++
						}
					}
				}
				if entries, err := os.ReadDir(s.prDir(host, owner, repoName)); err == nil {
					for _, e := range entries {
						if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
							cr.PRCount++
						}
					}
				}
				repos = append(repos, cr)
			}
		}
	}
	return repos, nil
}

// atomicWrite writes data to path via a temp file in the same directory followed
// by a rename, so an interrupted write cannot leave a truncated cache metadata
// file. Resume cursors are persisted frequently during a fetch, so atomicity
// matters here.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
