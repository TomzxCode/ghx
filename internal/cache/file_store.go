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

// fileStore is the file-per-item JSON cache backend. It stores each issue and
// pull request as <baseDir>/<host>/<owner>/<repo>/{issues,prs}/<number>.json and
// the per-repository metadata in .cache_info.json.
type fileStore struct {
	baseDir string
}

func (s *fileStore) repoDir(host, owner, repo string) string {
	return filepath.Join(s.baseDir, host, owner, repo)
}

func (s *fileStore) issueDir(host, owner, repo string) string {
	return filepath.Join(s.repoDir(host, owner, repo), "issues")
}

func (s *fileStore) prDir(host, owner, repo string) string {
	return filepath.Join(s.repoDir(host, owner, repo), "prs")
}

// SaveIssue writes a single issue to disk.
func (s *fileStore) SaveIssue(host, owner, repo string, issue *github.Issue) error {
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
func (s *fileStore) SavePR(host, owner, repo string, pr *github.PullRequest) error {
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
func (s *fileStore) LoadIssue(host, owner, repo string, number int) (*github.Issue, time.Time, error) {
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
func (s *fileStore) LoadPR(host, owner, repo string, number int) (*github.PullRequest, time.Time, error) {
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
func (s *fileStore) LoadAllIssues(host, owner, repo string) ([]*github.Issue, error) {
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
func (s *fileStore) LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error) {
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

// QueryIssues loads every cached issue and filters it in memory. This is the
// reference implementation whose result set the SQLite backend must match.
func (s *fileStore) QueryIssues(host, owner, repo string, q IssueQuery) ([]*github.Issue, error) {
	issues, err := s.LoadAllIssues(host, owner, repo)
	if err != nil {
		return nil, err
	}
	return filterIssues(issues, q), nil
}

// QueryPRs is the pull-request equivalent of QueryIssues.
func (s *fileStore) QueryPRs(host, owner, repo string, q PRQuery) ([]*github.PullRequest, error) {
	prs, err := s.LoadAllPRs(host, owner, repo)
	if err != nil {
		return nil, err
	}
	return filterPRs(prs, q), nil
}

// SaveCacheInfo writes the cache metadata file, marking the cache as complete
// at the current time with the given duration.
func (s *fileStore) SaveCacheInfo(host, owner, repo string, duration int) error {
	now := time.Now()
	info := &CacheInfo{
		CachedAt:       now,
		Duration:       duration,
		Complete:       true,
		IssuesCachedAt: now,
		PRsCachedAt:    now,
	}
	return s.SaveCacheInfoFull(host, owner, repo, info)
}

// SaveCacheInfoFull writes the given cache metadata atomically. Use this during
// a fetch to persist resume cursors before the fetch completes; SaveCacheInfo
// (above) is the convenience helper for the success case.
func (s *fileStore) SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error {
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
func (s *fileStore) LoadCacheInfo(host, owner, repo string) (*CacheInfo, error) {
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

// IsCacheFresh reports whether the cache is complete and both issues and PRs
// were populated within the stored duration. An interrupted (incomplete) fetch
// is never fresh.
func (s *fileStore) IsCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	d := time.Duration(info.Duration) * time.Minute
	return time.Since(info.IssuesUpdatedAt()) < d && time.Since(info.PRsUpdatedAt()) < d, nil
}

// IsIssuesCacheFresh reports whether issues were cached within the stored duration.
func (s *fileStore) IsIssuesCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return time.Since(info.IssuesUpdatedAt()) < time.Duration(info.Duration)*time.Minute, nil
}

// IsPRsCacheFresh reports whether pull requests were cached within the stored duration.
func (s *fileStore) IsPRsCacheFresh(host, owner, repo string) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return time.Since(info.PRsUpdatedAt()) < time.Duration(info.Duration)*time.Minute, nil
}

// IsCacheFreshWithDuration reports whether the cache is complete and both
// issues and PRs were populated within the given duration.
func (s *fileStore) IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	if !info.Complete {
		return false, nil
	}
	d := time.Duration(duration) * time.Minute
	return time.Since(info.IssuesUpdatedAt()) < d && time.Since(info.PRsUpdatedAt()) < d, nil
}

// IsIssuesCacheFreshWithDuration reports whether issues were cached within the given duration (minutes).
func (s *fileStore) IsIssuesCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return time.Since(info.IssuesUpdatedAt()) < time.Duration(duration)*time.Minute, nil
}

// IsPRsCacheFreshWithDuration reports whether PRs were cached within the given duration (minutes).
func (s *fileStore) IsPRsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	info, err := s.LoadCacheInfo(host, owner, repo)
	if err != nil {
		return false, err
	}
	return time.Since(info.PRsUpdatedAt()) < time.Duration(duration)*time.Minute, nil
}

// ListCachedRepos walks the cache directory and returns all cached repositories.
func (s *fileStore) ListCachedRepos() ([]CachedRepo, error) {
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

// Kind reports the backend name.
func (s *fileStore) Kind() string { return "file" }

// Location reports the cache root directory.
func (s *fileStore) Location() string { return s.baseDir }

// Close is a no-op for the file backend.
func (s *fileStore) Close() error { return nil }

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
