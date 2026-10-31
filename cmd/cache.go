package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/logging"
)

var (
	cacheDuration int
	cacheForce    bool
	cacheIfStale  bool
	cacheSince    string
	cacheType     string
	cacheRetries  int
	cacheWorkers  int
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Fetch and cache issues and PRs (including comments)",
	Long: `Fetches all issues and pull requests (with comments) from GitHub and writes them
to the local cache: the SQLite database ~/.cache/ghx/cache/cache.db by default, or
per-repository JSON files under ~/.cache/ghx/cache/<host>/<owner>/<repo> with
--storage file. Subsequent list/view commands will serve results from this cache
until it expires.

Each page is written to disk as soon as it is fetched, and the resume position
is persisted after every page. If a fetch is interrupted (for example by a GitHub
rate limit), re-running the command resumes from the last page written rather
than starting over.

Every run records the updatedAt ranges it has covered and fills only the gaps
between that coverage and the current head. An interrupted run resumes the
segment it had not finished. Repeating a run over a covered range does not read
it again.

Use --since to ensure coverage starts at a date. Ranges below that date are out
of scope until a later run asks for an earlier one. --force deletes the selected
type's cached items and coverage, then fetches them again; with --since it
rebuilds only that window.

Coverage is processed in segments that run concurrently: --workers sets the
number of segments in flight. The width is chosen from the uncovered duration:
issues target four segments per worker, full pull request pages two per slot,
clamped between 15 minutes and 7 days. At most two pull request pages run at
once because GitHub's secondary limits punish wider concurrency.

Use --type issues or --type prs to refresh only one of the two; --type both
(the default) refreshes both, concurrently, so a run takes about as long as the
slower of the two. A partial refresh only resets the freshness window of the
type it fetched, so a fresh issues cache still serves issue reads while pull
requests are re-fetched.`,
	RunE: runCache,
}

var cacheMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate cached repositories from the file backend to the SQLite backend",
	Long: `Copies cached issues, pull requests, and cache metadata from the file-based
cache into the SQLite cache. The source file cache is left intact, so the
migration is non-destructive and can be re-run (it is idempotent).

By default every cached repository is migrated; pass --repo [HOST/]OWNER/REPO
to migrate a single repository.`,
	RunE: runCacheMigrate,
}

func init() {
	cacheCmd.AddCommand(cacheMigrateCmd)
	cacheCmd.Flags().IntVar(&cacheDuration, "cache-duration", 60, "Cache duration in minutes")
	cacheCmd.Flags().BoolVar(&cacheForce, "force", false, "Delete the selected type's cached items and coverage, then re-fetch them (from --since when given)")
	cacheCmd.Flags().BoolVar(&cacheIfStale, "if-stale", false, "Do nothing when the cache is still fresh instead of delta-refreshing")
	cacheCmd.Flags().StringVar(&cacheSince, "since", "", "Ensure coverage starts at this date (YYYY-MM-DD or RFC3339)")
	cacheCmd.Flags().StringVar(&cacheType, "type", "both", "Which entries to refresh: issues, prs, or both")
	cacheCmd.Flags().IntVar(&cacheRetries, "retries", 2, "Retries on transient errors (rate limits, 5xx); 0 means a single attempt. Total attempts = retries + 1")
	cacheCmd.Flags().IntVar(&cacheWorkers, "workers", 2, "Concurrent coverage segments (1-16)")
}

// parseSinceDate parses a --since value. Supported formats: RFC3339
// ("2026-09-01T15:04:05Z"), date-only ("2026-09-01", interpreted as UTC
// midnight, matching GitHub search's day-granular qualifiers), and naive
// datetimes ("2026-09-01 15:04[.05]", interpreted in the local timezone).
func parseSinceDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []struct {
		layout   string
		location *time.Location
	}{
		{time.RFC3339, nil}, // the string carries its own offset
		{"2006-01-02", time.UTC},
		{"2006-01-02 15:04:05", time.Local},
		{"2006-01-02 15:04", time.Local},
		{"2006-01-02T15:04:05", time.Local},
		{"2006-01-02T15:04", time.Local},
	}
	for _, l := range layouts {
		var (
			t   time.Time
			err error
		)
		if l.location == nil {
			t, err = time.Parse(l.layout, s)
		} else {
			t, err = time.ParseInLocation(l.layout, s, l.location)
		}
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid --since value %q: use YYYY-MM-DD or RFC3339 (e.g. 2026-09-01T15:04:05Z)", s)
}

// FetchEvent describes progress during a repository fetch. Phase is "issues"
// or "prs"; Kind is "start", "batch", "done", or "notice"; Message carries the
// human-readable text for a start or notice (empty for silent phases); Cached
// is the number of items of that type already in the cache when the phase
// started; Done is the number of items written so far; Total is the number of
// items this run expects to fetch (0 when unknown).
type FetchEvent struct {
	Phase      string
	Kind       string
	Message    string
	Cached     int
	CacheTotal int // total items of this type in the repository (0 unknown)
	Done       int
	Total      int // items this run expects to fetch (0 unknown)
}

// FetchProgress receives fetch progress events. It may be nil.
type FetchProgress func(ev FetchEvent)

// FetchOptions controls a fetchRepoData run.
type FetchOptions struct {
	Force       bool       // delete the selected types and re-fetch them
	Since       *time.Time // lower bound for the covered range
	FetchIssues bool
	FetchPRs    bool
	Duration    int           // freshness window recorded on completion; <= 0 leaves the previous value
	Workers     int           // concurrent coverage segments; 0 uses the default
	OnProgress  FetchProgress // may be nil
}

func runCache(cmd *cobra.Command, args []string) error {
	var sinceTime *time.Time
	if cacheSince != "" {
		t, err := parseSinceDate(cacheSince)
		if err != nil {
			return err
		}
		sinceTime = &t
	}
	// --type selects which portions to refresh. An explicit targeting
	// (--since or --type != both) always fetches; only --if-stale can skip a
	// run on a fresh cache.
	fetchIssues, fetchPRs := true, true
	switch cacheType {
	case "", "both":
	case "issues":
		fetchPRs = false
	case "prs":
		fetchIssues = false
	default:
		return fmt.Errorf("invalid --type %q: use issues, prs, or both", cacheType)
	}

	repo, err := getRepo()
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()
	if store.Kind() == "sqlite" {
		fmt.Printf("Using sqlite cache backend at %s\n", store.Location())
	}
	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}
	if cacheRetries < 0 {
		return fmt.Errorf("invalid --retries %d: must be 0 or greater", cacheRetries)
	}
	client.SetRetries(cacheRetries)

	info, _ := store.LoadCacheInfo(repo.Host, repo.Owner, repo.Name)
	if info == nil {
		info = &cache.CacheInfo{}
	}

	// A plain run always delta-refreshes: the delta is the cheapest possible
	// check for changes (three small requests when nothing moved), so a probe
	// could not save anything. --if-stale opts into the old no-op behavior for
	// scripts that only need the cache warm.
	if cacheIfStale && !cacheForce && sinceTime == nil && cacheType == "both" && info.Complete && time.Since(info.CachedAt) < time.Duration(cacheDuration)*time.Minute {
		fmt.Printf("Cache is still fresh (within %d minutes); nothing to do. Re-run without --if-stale to fill the uncovered ranges, or with --force for a clean rebuild.\n", cacheDuration)
		return nil
	}

	var phases []string
	if fetchIssues {
		phases = append(phases, "issues")
	}
	if fetchPRs {
		phases = append(phases, "prs")
	}

	if _, _, err := fetchRepoData(store, client, repo, info, FetchOptions{
		Force:       cacheForce,
		Since:       sinceTime,
		FetchIssues: fetchIssues,
		FetchPRs:    fetchPRs,
		Duration:    cacheDuration,
		Workers:     cacheWorkers,
		OnProgress:  cliFetchProgress(client, phases...),
	}); err != nil {
		return err
	}

	fmt.Printf("Cache updated. Valid for %d minute(s).\n", cacheDuration)
	printRunSummary()
	return nil
}

// printRunSummary prints the per-run telemetry timing breakdown when telemetry
// is enabled. It is a no-op otherwise (FR-6, FR-10).
func printRunSummary() {
	if telemetryAggregator == nil {
		return
	}
	s := telemetryAggregator.Summary()
	if s.Total == 0 {
		return
	}
	var totalSent, totalRecv int64
	for _, k := range s.KindSummaries {
		totalSent += k.TotalSentBytes
		totalRecv += k.TotalRecvBytes
	}
	fmt.Fprintf(os.Stderr, "Timings (%.1fs total, %d call(s), %d retried, sent %s, received %s):\n",
		s.Elapsed.Seconds(), s.Total, s.Retried, humanBytes(totalSent), humanBytes(totalRecv))
	for _, k := range s.KindSummaries {
		fmt.Fprintf(os.Stderr, "  %-18s %5d call(s)  p50 %6dms  p95 %6dms  sent %8s  recv %8s  p50 %d items\n",
			k.Kind, k.Count, k.P50Ms, k.P95Ms,
			humanBytes(k.TotalSentBytes), humanBytes(k.TotalRecvBytes), k.P50Items)
	}
}

// runCacheMigrate copies the file-based cache into the SQLite cache for one or
// all cached repositories. The file cache is left in place.
func runCacheMigrate(cmd *cobra.Command, args []string) error {
	base := cacheDir
	if base == "" {
		base = cache.DefaultDir()
	}
	src := cache.NewStoreWithPath(base)
	defer src.Close()
	dst, err := cache.NewSQLiteStore(base)
	if err != nil {
		return err
	}
	defer dst.Close()

	var repos []cache.CachedRepo
	if repoFlag != "" {
		repo, err := getRepo()
		if err != nil {
			return err
		}
		repos = []cache.CachedRepo{{Host: repo.Host, Owner: repo.Owner, Repo: repo.Name}}
	} else {
		repos, err = src.ListCachedRepos()
		if err != nil {
			return err
		}
	}
	if len(repos) == 0 {
		fmt.Println("No cached repositories to migrate.")
		return nil
	}

	fmt.Printf("Migrating %d repository(ies) from file cache to %s\n", len(repos), dst.Location())
	var totalIssues, totalPRs int
	for _, r := range repos {
		res, err := cache.Migrate(src, dst, r.Host, r.Owner, r.Repo)
		if err != nil {
			return fmt.Errorf("migrating %s/%s: %w", r.Owner, r.Repo, err)
		}
		totalIssues += res.Issues
		totalPRs += res.PRs
		fmt.Printf("  %s/%s: %d issue(s), %d pull request(s)\n", r.Owner, r.Repo, res.Issues, res.PRs)
	}
	fmt.Printf("Migration complete: %d issue(s), %d pull request(s).\n", totalIssues, totalPRs)
	return nil
}

// fetchRepoData fetches issues and/or pull requests for repo into the store,
// resuming from (and advancing) the cursors in info. When opts.Force is true
// the resume state of the selected portions is discarded and a full fetch
// runs. opts.Since, when set, windows issues with the server-side filter and
// seeds the pull request walk at that instant, recording it as the cache floor.
// opts.OnProgress may be nil; when set it receives start/batch/done events for
// each phase. Returns the number of issues and PRs written. On error the store
// keeps what was written before the failure and info holds the resume cursor
// for the next attempt.
//
// When both portions are selected they are fetched concurrently, each writing
// its own rows, so a run costs about as long as the slower portion instead of
// the sum of both. A failure in one portion does not discard what the other
// wrote. Progress callbacks are serialized, and the cache metadata is guarded
// by a mutex because both portions advance their cursor and rewrite it after
// every page.
//
// Freshness is recorded per data type: a partial run (--type issues|prs)
// updates only the timestamp of the portion it fetched, so the other portion
// keeps its own age. The cache is only marked complete when both portions were
// fetched.
const (
	coverageIssues = "issues"
	coveragePRs    = "prs"
)

// coveragePortion is one data type's planned coverage work.
type coveragePortion struct {
	kind        string
	cached      int
	cacheTotal  int
	targetStart time.Time
	segments    []cache.Segment
	gaps        []cache.Segment
	planned     []cache.Segment
	window      int
	done        atomic.Int64
}

type coverageWork struct {
	portion *coveragePortion
	segment cache.Segment
}

// coverageSyncer runs a coverage-driven cache refresh: it finds the gaps
// between what the cache already covers and the current head, processes them
// as independently seekable segments, and records each completed segment.
type coverageSyncer struct {
	store   cache.Store
	client  *github.Client
	repo    *gitremote.Repo
	info    *cache.CacheInfo
	opts    FetchOptions
	workers int
	now     time.Time
	sem     chan struct{}
	prSem   chan struct{}
	notify  func(FetchEvent)

	infoMu     sync.Mutex
	coverageMu sync.Mutex
	issues     *coveragePortion
	prs        *coveragePortion
}

func fetchRepoData(store cache.Store, client *github.Client, repo *gitremote.Repo, info *cache.CacheInfo, opts FetchOptions) (int, int, error) {
	workers := opts.Workers
	if workers <= 0 {
		workers = 2
	}
	if workers > 16 {
		workers = 16
	}
	prWorkers := workers
	if prWorkers > maxPRFullPages {
		prWorkers = maxPRFullPages
	}
	s := &coverageSyncer{
		store:   store,
		client:  client,
		repo:    repo,
		info:    info,
		opts:    opts,
		workers: workers,
		now:     time.Now().UTC().Truncate(time.Second),
		sem:     make(chan struct{}, workers),
		prSem:   make(chan struct{}, prWorkers),
	}

	fail := func(err error) (int, int, error) {
		issues, prs := s.counts()
		return issues, prs, err
	}

	var progressMu sync.Mutex
	s.notify = func(ev FetchEvent) {
		if opts.OnProgress == nil {
			if ev.Kind == "notice" {
				fmt.Println(ev.Message)
			}
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		opts.OnProgress(ev)
	}

	if opts.Force {
		if opts.FetchIssues {
			if _, err := store.DeleteType(repo.Host, repo.Owner, repo.Name, coverageIssues); err != nil {
				return 0, 0, fmt.Errorf("clearing cached issues: %w", err)
			}
			info.IssueCursor = nil
			info.IssuesCachedAt = time.Time{}
		}
		if opts.FetchPRs {
			if _, err := store.DeleteType(repo.Host, repo.Owner, repo.Name, coveragePRs); err != nil {
				return 0, 0, fmt.Errorf("clearing cached pull requests: %w", err)
			}
			info.PRCursor = nil
			info.PRPageCursor = ""
			info.PRFloor = nil
			info.PRsCachedAt = time.Time{}
		}
		if opts.FetchIssues && opts.FetchPRs {
			info.Complete = false
			info.CachedAt = time.Time{}
		}
	}

	if !opts.FetchIssues {
		s.notify(FetchEvent{Kind: "notice", Message: "Skipping issues (--type prs)."})
	}
	if !opts.FetchPRs {
		s.notify(FetchEvent{Kind: "notice", Message: "Skipping pull requests (--type issues)."})
	}

	cachedIssues, cachedPRs := cachedItemCounts(store, repo)
	var err error
	var planErrs []error
	if s.issues, err = s.plan(coverageIssues, opts.FetchIssues, cachedIssues); err != nil {
		planErrs = append(planErrs, err)
		s.issues = nil
	}
	if s.prs, err = s.plan(coveragePRs, opts.FetchPRs, cachedPRs); err != nil {
		planErrs = append(planErrs, err)
		s.prs = nil
	}
	if s.issues == nil && s.prs == nil {
		return fail(errors.Join(planErrs...))
	}

	for _, p := range []*coveragePortion{s.issues, s.prs} {
		if p == nil {
			continue
		}
		s.notify(FetchEvent{Phase: p.kind, Kind: "start", Cached: p.cached, CacheTotal: p.cacheTotal, Message: s.startMessage(p)})
	}

	var works []coverageWork
	n := 0
	if s.issues != nil {
		n = len(s.issues.planned)
	}
	if s.prs != nil && len(s.prs.planned) > n {
		n = len(s.prs.planned)
	}
	for i := 0; i < n; i++ {
		if s.issues != nil && i < len(s.issues.planned) {
			works = append(works, coverageWork{portion: s.issues, segment: s.issues.planned[i]})
		}
		if s.prs != nil && i < len(s.prs.planned) {
			works = append(works, coverageWork{portion: s.prs, segment: s.prs.planned[i]})
		}
	}
	if err := s.runWorks(works); err != nil {
		return fail(err)
	}

	// Catch-up: anything updated while the planned segments ran has an
	// updatedAt after s.now, so a tail from s.now to the new head covers it.
	tail, err := s.tailWorks()
	if err != nil {
		return fail(err)
	}
	if err := s.runWorks(tail); err != nil {
		return fail(err)
	}

	for _, p := range []*coveragePortion{s.issues, s.prs} {
		if p == nil {
			continue
		}
		if p.done.Load() == 0 {
			s.notify(FetchEvent{Phase: p.kind, Kind: "batch", Done: 0, Total: 0, Cached: p.cached, CacheTotal: p.cacheTotal})
		}
		s.notify(FetchEvent{Phase: p.kind, Kind: "done", Done: int(p.done.Load())})
	}

	if err := s.finalize(); err != nil {
		return fail(err)
	}
	issues, prs := s.counts()
	return issues, prs, errors.Join(planErrs...)
}

// plan loads coverage for one kind, seeds it from the legacy cursors when it is
// empty, and computes the gaps up to the run's head.
func (s *coverageSyncer) plan(kind string, enabled bool, cached int) (*coveragePortion, error) {
	if !enabled {
		return nil, nil
	}
	p := &coveragePortion{kind: kind, cached: cached}
	segments, err := s.store.LoadSegments(s.repo.Host, s.repo.Owner, s.repo.Name, kind)
	if err != nil {
		return nil, fmt.Errorf("loading %s coverage: %w", kindLabel(kind), err)
	}
	p.segments = segments
	if err := s.seedLegacy(p); err != nil {
		return nil, err
	}

	var start time.Time
	if s.opts.Since != nil {
		start = *s.opts.Since
	}
	if start.IsZero() && len(p.segments) > 0 {
		start = p.segments[0].Start
	}
	if start.IsZero() {
		oldest, err := s.oldestStart(kind)
		if err != nil {
			return nil, fmt.Errorf("finding the oldest %s: %w", kindLabel(kind), err)
		}
		start = oldest
	}
	if start.IsZero() {
		start = s.now
	}
	if start.After(s.now) {
		start = s.now
	}
	p.targetStart = start.UTC().Truncate(time.Second)
	p.gaps = cache.Uncovered(p.segments, p.targetStart, s.now)
	chunkSize := s.chunkSize(p.gaps, kind)
	p.planned = cache.Chunk(p.gaps, chunkSize)
	planAttrs := []any{
		"kind", kindLabel(kind),
		"target_start", p.targetStart.Format(time.RFC3339),
		"head", s.now.Format(time.RFC3339),
		"gaps", len(p.gaps),
		"segment_size", chunkSize,
		"segments", len(p.planned),
		"workers", s.workers,
	}
	if kind == coveragePRs {
		prSlots := s.workers
		if prSlots > maxPRFullPages {
			prSlots = maxPRFullPages
		}
		planAttrs = append(planAttrs, "pr_slots", prSlots)
	}
	if len(p.planned) > 0 {
		planAttrs = append(planAttrs,
			"first_segment", p.planned[0].Start.Format(time.RFC3339),
			"last_segment", p.planned[len(p.planned)-1].End.Format(time.RFC3339))
	}
	logging.Debug("coverage plan", planAttrs...)

	for _, gap := range p.gaps {
		if n, err := s.client.SearchCount(s.repo.Owner, s.repo.Name, kind, gap.Start, gap.End); err == nil {
			p.window += n
		} else {
			logging.Warn("counting coverage window", "kind", kind, "error", err)
		}
	}

	if kind == coverageIssues {
		if n, err := s.client.CountIssues(s.repo.Owner, s.repo.Name); err == nil {
			p.cacheTotal = n
		}
	} else {
		if n, err := s.client.CountPRs(s.repo.Owner, s.repo.Name); err == nil {
			p.cacheTotal = n
		}
	}
	return p, nil
}

// seedLegacy converts the old cursor model into an initial covered segment. An
// ascending walk from the beginning covers everything up to its frontier, so
// the seeded interval never over-claims.
func (s *coverageSyncer) seedLegacy(p *coveragePortion) error {
	if len(p.segments) > 0 {
		return nil
	}
	start := epochTime()
	end := time.Time{}
	if p.kind == coverageIssues {
		if s.info.IssueCursor == nil {
			return nil
		}
		end = *s.info.IssueCursor
	} else {
		if s.info.PRCursor == nil {
			return nil
		}
		if s.info.PRPageCursor == "" && !s.info.Complete {
			return nil
		}
		if s.info.PRFloor != nil {
			start = *s.info.PRFloor
		}
		end = *s.info.PRCursor
	}
	if end.After(s.now) {
		end = s.now
	}
	if !end.After(start) {
		return nil
	}
	seg := cache.Segment{Start: start.UTC().Truncate(time.Second), End: end.UTC().Truncate(time.Second)}
	if err := s.store.SaveSegments(s.repo.Host, s.repo.Owner, s.repo.Name, p.kind, []cache.Segment{seg}); err != nil {
		return fmt.Errorf("seeding %s coverage: %w", kindLabel(p.kind), err)
	}
	p.segments = []cache.Segment{seg}
	return nil
}

func (s *coverageSyncer) oldestStart(kind string) (time.Time, error) {
	if kind == coverageIssues {
		return s.client.OldestIssueUpdatedAt(s.repo.Owner, s.repo.Name)
	}
	return s.client.OldestPRUpdatedAt(s.repo.Owner, s.repo.Name)
}

func epochTime() time.Time {
	return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
}

func kindLabel(kind string) string {
	if kind == coveragePRs {
		return "pull requests"
	}
	return "issues"
}

const (
	minCoverageChunk = 15 * time.Minute
	maxCoverageChunk = 7 * 24 * time.Hour

	// Full PR pages are heavy enough that GitHub's secondary limits punish
	// wide concurrency. Two in flight is the setting the old windowed fetch
	// settled on; four still tripped 403s on a multi-day window in testing.
	maxPRFullPages = 2
)

// chunkSize picks a segment width from the uncovered duration. Issues target
// four chunks per worker; full PR pages target two chunks per effective PR
// slot, since they cannot run more than maxPRFullPages at once.
func (s *coverageSyncer) chunkSize(gaps []cache.Segment, kind string) time.Duration {
	var total time.Duration
	for _, gap := range gaps {
		total += gap.End.Sub(gap.Start)
	}
	if total <= 0 {
		return 0
	}
	perWorker := 4
	effective := s.workers
	if kind == coveragePRs {
		// Full PR pages are capped at maxPRFullPages, so targeting more
		// segments than those slots can consume only adds cursor requests.
		if effective > maxPRFullPages {
			effective = maxPRFullPages
		}
		perWorker = 2
	}
	size := total / time.Duration(effective*perWorker)
	if size < minCoverageChunk {
		size = minCoverageChunk
	}
	if size > maxCoverageChunk {
		size = maxCoverageChunk
	}
	return size
}

func (s *coverageSyncer) startMessage(p *coveragePortion) string {
	label := "issues"
	if p.kind == coveragePRs {
		label = "pull requests"
	}
	name := s.repo.Owner + "/" + s.repo.Name
	if s.opts.Since != nil {
		if p.kind == coverageIssues {
			return fmt.Sprintf("Fetching issues created or updated since %s for %s...", s.opts.Since.Format("2006-01-02 15:04"), name)
		}
		return fmt.Sprintf("Fetching pull requests updated since %s for %s...", s.opts.Since.Format("2006-01-02 15:04"), name)
	}
	if len(p.gaps) > 0 && !p.gaps[0].Start.Equal(p.targetStart) {
		return fmt.Sprintf("Fetching %s updated since %s for %s...", label, p.gaps[0].Start.Format("2006-01-02 15:04"), name)
	}
	return fmt.Sprintf("Caching %s for %s...", label, name)
}

func (s *coverageSyncer) runWorks(works []coverageWork) error {
	if len(works) == 0 {
		return nil
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}
	for _, w := range works {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.process(w); err != nil {
				recordErr(fmt.Errorf("%s %s: %w", w.portion.kind, w.segment.Start.Format(time.RFC3339), err))
				return
			}
			if err := s.mark(w); err != nil {
				recordErr(err)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (s *coverageSyncer) process(w coverageWork) error {
	p := w.portion
	// Acquire the kind-specific slot before the shared worker slot, so a
	// queued PR segment does not hold a worker slot that an issue could use.
	if p.kind == coveragePRs {
		s.prSem <- struct{}{}
		s.sem <- struct{}{}
		defer func() {
			<-s.sem
			<-s.prSem
		}()
	} else {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
	}

	if p.kind == coverageIssues {
		since := w.segment.Start
		until := w.segment.End
		_, err := s.client.FetchIssuesUntil(s.repo.Owner, s.repo.Name, &since, &until, func(batch []*github.Issue, _ int) error {
			for _, issue := range batch {
				if err := s.store.SaveIssue(s.repo.Host, s.repo.Owner, s.repo.Name, issue); err != nil {
					return fmt.Errorf("saving issue #%d: %w", issue.Number, err)
				}
				s.advance(&s.info.IssueCursor, issue.UpdatedAt)
			}
			if len(batch) == 0 {
				return nil
			}
			n := p.done.Add(int64(len(batch)))
			s.notify(FetchEvent{Phase: p.kind, Kind: "batch", Done: int(n), Total: p.window, Cached: p.cached, CacheTotal: p.cacheTotal})
			return nil
		})
		return err
	}

	cursor := github.PRCursorAt(w.segment.Start)
	return s.client.WalkPRsAscUntil(s.repo.Owner, s.repo.Name, cursor, w.segment.End, func(batch []*github.PullRequest, _ int, _ string, _ bool) error {
		for _, pr := range batch {
			if err := s.store.SavePR(s.repo.Host, s.repo.Owner, s.repo.Name, pr); err != nil {
				return fmt.Errorf("saving PR #%d: %w", pr.Number, err)
			}
			s.advance(&s.info.PRCursor, pr.UpdatedAt)
		}
		if len(batch) == 0 {
			return nil
		}
		n := p.done.Add(int64(len(batch)))
		s.notify(FetchEvent{Phase: p.kind, Kind: "batch", Done: int(n), Total: p.window, Cached: p.cached, CacheTotal: p.cacheTotal})
		return nil
	})
}

func (s *coverageSyncer) mark(w coverageWork) error {
	s.coverageMu.Lock()
	defer s.coverageMu.Unlock()
	if err := s.store.SaveSegments(s.repo.Host, s.repo.Owner, s.repo.Name, w.portion.kind, []cache.Segment{w.segment}); err != nil {
		return fmt.Errorf("recording %s coverage: %w", w.portion.kind, err)
	}
	return nil
}

// tailWorks builds the catch-up segments between the run's head and the head
// observed now. Every item updated during the planned pass falls in this range.
func (s *coverageSyncer) tailWorks() ([]coverageWork, error) {
	tailEnd := time.Now().UTC().Truncate(time.Second)
	if !tailEnd.After(s.now) {
		return nil, nil
	}
	var works []coverageWork
	for _, p := range []*coveragePortion{s.issues, s.prs} {
		if p == nil {
			continue
		}
		segments, err := s.store.LoadSegments(s.repo.Host, s.repo.Owner, s.repo.Name, p.kind)
		if err != nil {
			return nil, fmt.Errorf("loading %s coverage for catch-up: %w", p.kind, err)
		}
		p.segments = segments
		gaps := cache.Uncovered(segments, s.now, tailEnd)
		for _, gap := range gaps {
			if n, err := s.client.SearchCount(s.repo.Owner, s.repo.Name, p.kind, gap.Start, gap.End); err == nil {
				p.window += n
			}
		}
		planned := cache.Chunk(gaps, s.chunkSize(gaps, p.kind))
		logging.Debug("coverage catch-up", "kind", kindLabel(p.kind),
			"from", s.now.Format(time.RFC3339), "to", tailEnd.Format(time.RFC3339),
			"gaps", len(gaps), "segments", len(planned))
		for _, seg := range planned {
			works = append(works, coverageWork{portion: p, segment: seg})
		}
	}
	return works, nil
}

func (s *coverageSyncer) advance(cur **time.Time, t time.Time) {
	s.infoMu.Lock()
	defer s.infoMu.Unlock()
	advanceCursor(cur, t)
}

func (s *coverageSyncer) counts() (int, int) {
	issues, prs := 0, 0
	if s.issues != nil {
		issues = int(s.issues.done.Load())
	}
	if s.prs != nil {
		prs = int(s.prs.done.Load())
	}
	return issues, prs
}

func (s *coverageSyncer) finalize() error {
	now := time.Now()
	if s.issues != nil {
		s.info.IssuesCachedAt = now
	}
	if s.prs != nil {
		s.info.PRsCachedAt = now
	}
	if s.issues != nil && s.prs != nil {
		s.info.CachedAt = now
		s.info.Complete = true
	}
	if s.opts.Duration > 0 {
		s.info.Duration = s.opts.Duration
	}
	s.info.PRPageCursor = ""
	if s.prs != nil {
		segments, err := s.store.LoadSegments(s.repo.Host, s.repo.Owner, s.repo.Name, coveragePRs)
		if err != nil {
			return fmt.Errorf("loading pull request coverage: %w", err)
		}
		if len(segments) > 0 {
			floor := segments[0].Start
			s.info.PRFloor = &floor
		}
	}
	if err := s.store.SaveCacheInfoFull(s.repo.Host, s.repo.Owner, s.repo.Name, s.info); err != nil {
		return fmt.Errorf("saving cache info: %w", err)
	}
	return nil
}

// instead of from zero. A repository with nothing cached reports zeros.
func cachedItemCounts(store cache.Store, repo *gitremote.Repo) (issues, prs int) {
	repos, err := store.ListCachedRepos()
	if err != nil {
		return 0, 0
	}
	for _, r := range repos {
		if strings.EqualFold(r.Host, repo.Host) &&
			strings.EqualFold(r.Owner, repo.Owner) &&
			strings.EqualFold(r.Repo, repo.Name) {
			return r.IssueCount, r.PRCount
		}
	}
	return 0, 0
}

// advanceCursor sets *cur to t when t is later than the current value (or when
// *cur is nil), advancing the resume high-water mark.
func advanceCursor(cur **time.Time, t time.Time) {
	if t.IsZero() {
		return
	}
	if *cur == nil || t.After(**cur) {
		*cur = &t
	}
}
