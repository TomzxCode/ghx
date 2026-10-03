package cache

import (
	"time"

	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// InstrumentedStore wraps a Store, recording one telemetry event per operation
// with its duration and result size. It decorates the interface so both the
// file and SQLite backends are covered without touching either (NFR-6).
//
// Instrumentation is best-effort: a nil or disabled recorder makes every method
// a straight passthrough.
type InstrumentedStore struct {
	store Store
	rec   telemetry.Recorder
	host  string
	owner string
	repo  string
}

// Instrument wraps store so its operations are recorded. host/owner/repo label
// the events; rec may be nil or disabled.
func Instrument(store Store, rec telemetry.Recorder, host, owner, repo string) Store {
	if store == nil {
		return store
	}
	if rec == nil {
		rec = telemetry.Nop{}
	}
	return &InstrumentedStore{store: store, rec: rec, host: host, owner: owner, repo: repo}
}

func (s *InstrumentedStore) record(kind, op string, start time.Time, items int, err error) {
	if s.rec == nil || !s.rec.Enabled() {
		return
	}
	s.rec.Record(telemetry.Event{
		Timestamp: start,
		Kind:      kind,
		Operation: op,
		Repo:      s.owner + "/" + s.repo,
		Duration:  time.Since(start),
		Items:     items,
		Backend:   s.store.Kind(),
		Failed:    err != nil,
		Attempts:  1,
	})
}

// SaveIssue records the write and its (single) item.
func (s *InstrumentedStore) SaveIssue(host, owner, repo string, issue *github.Issue) error {
	start := time.Now()
	err := s.store.SaveIssue(host, owner, repo, issue)
	s.record(telemetry.KindCacheSaveIssue, "SaveIssue", start, 1, err)
	return err
}

// LoadIssue records the read.
func (s *InstrumentedStore) LoadIssue(host, owner, repo string, number int) (*github.Issue, time.Time, error) {
	start := time.Now()
	issue, mtime, err := s.store.LoadIssue(host, owner, repo, number)
	s.record(telemetry.KindCacheLoadIssue, "LoadIssue", start, boolInt(issue != nil), err)
	return issue, mtime, err
}

// LoadAllIssues records the read and its result size.
func (s *InstrumentedStore) LoadAllIssues(host, owner, repo string) ([]*github.Issue, error) {
	start := time.Now()
	issues, err := s.store.LoadAllIssues(host, owner, repo)
	s.record(telemetry.KindCacheLoadAll, "LoadAllIssues", start, len(issues), err)
	return issues, err
}

// SavePR records the write and its (single) item.
func (s *InstrumentedStore) SavePR(host, owner, repo string, pr *github.PullRequest) error {
	start := time.Now()
	err := s.store.SavePR(host, owner, repo, pr)
	s.record(telemetry.KindCacheSavePR, "SavePR", start, 1, err)
	return err
}

// LoadPR records the read.
func (s *InstrumentedStore) LoadPR(host, owner, repo string, number int) (*github.PullRequest, time.Time, error) {
	start := time.Now()
	pr, mtime, err := s.store.LoadPR(host, owner, repo, number)
	s.record(telemetry.KindCacheLoadPR, "LoadPR", start, boolInt(pr != nil), err)
	return pr, mtime, err
}

// LoadAllPRs records the read and its result size.
func (s *InstrumentedStore) LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error) {
	start := time.Now()
	prs, err := s.store.LoadAllPRs(host, owner, repo)
	s.record(telemetry.KindCacheLoadAll, "LoadAllPRs", start, len(prs), err)
	return prs, err
}

// SaveCacheInfo records the metadata write.
func (s *InstrumentedStore) SaveCacheInfo(host, owner, repo string, duration int) error {
	start := time.Now()
	err := s.store.SaveCacheInfo(host, owner, repo, duration)
	s.record(telemetry.KindCacheInfo, "SaveCacheInfo", start, 0, err)
	return err
}

// SaveCacheInfoFull records the metadata write.
func (s *InstrumentedStore) SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error {
	start := time.Now()
	err := s.store.SaveCacheInfoFull(host, owner, repo, info)
	s.record(telemetry.KindCacheInfo, "SaveCacheInfoFull", start, 0, err)
	return err
}

// LoadCacheInfo records the metadata read.
func (s *InstrumentedStore) LoadCacheInfo(host, owner, repo string) (*CacheInfo, error) {
	start := time.Now()
	info, err := s.store.LoadCacheInfo(host, owner, repo)
	s.record(telemetry.KindCacheInfo, "LoadCacheInfo", start, 0, err)
	return info, err
}

// The freshness checks are cheap in-memory/metadata reads; they are delegated
// without recording so the event volume stays proportional to meaningful work.
func (s *InstrumentedStore) IsCacheFresh(host, owner, repo string) (bool, error) {
	return s.store.IsCacheFresh(host, owner, repo)
}

// IsCacheFreshWithDuration delegates without recording.
func (s *InstrumentedStore) IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	return s.store.IsCacheFreshWithDuration(host, owner, repo, duration)
}

// IsIssuesCacheFresh delegates without recording.
func (s *InstrumentedStore) IsIssuesCacheFresh(host, owner, repo string) (bool, error) {
	return s.store.IsIssuesCacheFresh(host, owner, repo)
}

// IsPRsCacheFresh delegates without recording.
func (s *InstrumentedStore) IsPRsCacheFresh(host, owner, repo string) (bool, error) {
	return s.store.IsPRsCacheFresh(host, owner, repo)
}

// IsIssuesCacheFreshWithDuration delegates without recording.
func (s *InstrumentedStore) IsIssuesCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	return s.store.IsIssuesCacheFreshWithDuration(host, owner, repo, duration)
}

// IsPRsCacheFreshWithDuration delegates without recording.
func (s *InstrumentedStore) IsPRsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error) {
	return s.store.IsPRsCacheFreshWithDuration(host, owner, repo, duration)
}

// ListCachedRepos records the scan and its result size.
func (s *InstrumentedStore) ListCachedRepos() ([]CachedRepo, error) {
	start := time.Now()
	repos, err := s.store.ListCachedRepos()
	s.record(telemetry.KindCacheInfo, "ListCachedRepos", start, len(repos), err)
	return repos, err
}

// QueryIssues records the query and its result size.
func (s *InstrumentedStore) QueryIssues(host, owner, repo string, q IssueQuery) ([]*github.Issue, error) {
	start := time.Now()
	issues, err := s.store.QueryIssues(host, owner, repo, q)
	s.record(telemetry.KindCacheQueryIssues, "QueryIssues", start, len(issues), err)
	return issues, err
}

// QueryPRs records the query and its result size.
func (s *InstrumentedStore) QueryPRs(host, owner, repo string, q PRQuery) ([]*github.PullRequest, error) {
	start := time.Now()
	prs, err := s.store.QueryPRs(host, owner, repo, q)
	s.record(telemetry.KindCacheQueryPRs, "QueryPRs", start, len(prs), err)
	return prs, err
}

// Kind delegates so callers still see the underlying backend name.
func (s *InstrumentedStore) Kind() string { return s.store.Kind() }

// Location delegates so callers still see the underlying storage location.
func (s *InstrumentedStore) Location() string { return s.store.Location() }

// Close delegates.
func (s *InstrumentedStore) Close() error { return s.store.Close() }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Ensure InstrumentedStore satisfies Store at compile time.
var _ Store = (*InstrumentedStore)(nil)
