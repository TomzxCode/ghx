package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
)

var (
	cacheDuration int
	cacheForce    bool
	cacheSince    string
	cacheType     string
	cacheRetries  int
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Fetch and cache issues and PRs (including comments)",
	Long: `Fetches all issues and pull requests (with comments) from GitHub and writes them
to ~/.cache/ghx/cache/<host>/<owner>/<repo>. Subsequent list/view commands will
serve results from this cache until it expires.

Each page is written to disk as soon as it is fetched, and the resume position
is persisted after every page. If a fetch is interrupted (for example by a GitHub
rate limit), re-running the command resumes from the last page written rather
than starting over.

Use --since to refresh only entries created or updated on or after a given
date, instead of the default last-cache-write delta.

Use --type issues or --type prs to refresh only one of the two; --type both
(the default) refreshes both. A partial refresh only resets the freshness
window of the type it fetched, so a fresh issues cache still serves issue
reads while pull requests are re-fetched.`,
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
	cacheCmd.Flags().BoolVar(&cacheForce, "force", false, "Re-fetch even if the cache is still fresh")
	cacheCmd.Flags().StringVar(&cacheSince, "since", "", "Only refresh entries created or updated since this date (YYYY-MM-DD or RFC3339)")
	cacheCmd.Flags().StringVar(&cacheType, "type", "both", "Which entries to refresh: issues, prs, or both")
	cacheCmd.Flags().IntVar(&cacheRetries, "retries", 2, "Retries on transient errors (rate limits, 5xx); 0 means a single attempt. Total attempts = retries + 1")
}

// parseSinceDate parses a --since value. Supported formats: RFC3339
// ("2026-09-01T15:04:05Z"), date-only ("2026-09-01", interpreted as UTC
// midnight, matching GitHub search's day-granular qualifiers), and naive
// datetimes ("2026-09-01 15:04[.05]", interpreted in the local timezone).
func parseSinceDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []struct {
		layout string
		utc    bool
	}{
		{time.RFC3339, false},
		{"2006-01-02", true},
		{"2006-01-02 15:04:05", false},
		{"2006-01-02 15:04", false},
		{"2006-01-02T15:04:05", false},
		{"2006-01-02T15:04", false},
	}
	for _, l := range layouts {
		if t, err := time.Parse(l.layout, s); err == nil {
			if l.utc {
				t = t.UTC()
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid --since value %q: use YYYY-MM-DD or RFC3339 (e.g. 2026-09-01T15:04:05Z)", s)
}

// FetchEvent describes progress during a repository fetch. Phase is "issues",
// "prs", or "prs-scan" (the newest-first walk locating the delta window);
// Kind is "start", "batch", or "done"; Message carries the human-readable
// phase description on start (empty for silent phases); Done is the number of
// items written (or scanned for prs-scan) so far; Total is the server-reported
// total (0 when unknown).
type FetchEvent struct {
	Phase   string
	Kind    string
	Message string
	Done    int
	Total   int
}

// FetchProgress receives fetch progress events. It may be nil.
type FetchProgress func(ev FetchEvent)

// FetchOptions controls a fetchRepoData run.
type FetchOptions struct {
	Force       bool       // discard resume state for the selected portions and full-fetch
	Since       *time.Time // explicit cutoff overriding the resume cursors
	FetchIssues bool
	FetchPRs    bool
	Duration    int           // freshness window recorded on completion; <= 0 leaves the previous value
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
	if sinceTime != nil && cacheForce {
		return fmt.Errorf("--force and --since cannot be combined: --force re-fetches everything, --since refreshes a targeted window")
	}

	// --type selects which portions to refresh. Any explicit targeting
	// (--since or --type != both) bypasses the freshness short-circuit.
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

	if !cacheForce && sinceTime == nil && cacheType == "both" && info.Complete && time.Since(info.CachedAt) < time.Duration(cacheDuration)*time.Minute {
		fmt.Printf("Cache is still fresh (within %d minutes). Use --force to refresh anyway.\n", cacheDuration)
		return nil
	}

	if _, _, err := fetchRepoData(store, client, repo, info, FetchOptions{
		Force:       cacheForce,
		Since:       sinceTime,
		FetchIssues: fetchIssues,
		FetchPRs:    fetchPRs,
		Duration:    cacheDuration,
		OnProgress:  cliFetchProgress(client),
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
// runs. opts.Since, when set, overrides the cursors with an explicit cutoff.
// opts.OnProgress may be nil; when set it receives start/batch/done events for
// each phase. Returns the number of issues and PRs written. On error the store
// keeps what was written before the failure and info holds the resume cursor
// for the next attempt.
//
// Freshness is recorded per data type: a partial run (--type issues|prs)
// updates only the timestamp of the portion it fetched, so the other portion
// keeps its own age. The cache is only marked complete when both portions were
// fetched.
func fetchRepoData(store cache.Store, client *github.Client, repo *gitremote.Repo, info *cache.CacheInfo, opts FetchOptions) (int, int, error) {
	if opts.Force {
		if opts.FetchIssues {
			info.IssueCursor = nil
		}
		if opts.FetchPRs {
			info.PRCursor = nil
		}
		if opts.FetchIssues && opts.FetchPRs {
			info.Complete = false
		}
	}

	// --since only applies to a complete cache: applying it to an interrupted
	// fetch would mark the cache complete while most history is missing, so in
	// that case finish the full fetch instead and ignore --since.
	if opts.Since != nil && !info.Complete {
		fmt.Println("Cache is incomplete; resuming the full fetch (--since ignored).")
		opts.Since = nil
	}

	notify := func(ev FetchEvent) {
		if opts.OnProgress != nil {
			opts.OnProgress(ev)
		}
	}

	saveInfo := func() error {
		return store.SaveCacheInfoFull(repo.Host, repo.Owner, repo.Name, info)
	}

	issueCount := 0
	prCount := 0

	if opts.FetchIssues {
		// --- Issues ---
		// Issues are fetched oldest-first. `since = IssueCursor` either resumes an
		// interrupted fetch (cursor mid-history) or delta-updates a complete cache
		// (cursor near newest). nil cursor => cold fetch from the beginning.
		// An explicit --since overrides the cursor for this run.
		issueSince := info.IssueCursor
		if opts.Since != nil {
			issueSince = opts.Since
		}
		switch {
		case issueSince != nil && !info.Complete:
			notify(FetchEvent{Phase: "issues", Kind: "start", Message: fmt.Sprintf("Resuming issues from %s for %s/%s...", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)})
		case issueSince != nil && opts.Since != nil:
			notify(FetchEvent{Phase: "issues", Kind: "start", Message: fmt.Sprintf("Fetching issues created or updated since %s for %s/%s...", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)})
		case issueSince != nil:
			notify(FetchEvent{Phase: "issues", Kind: "start", Message: fmt.Sprintf("Fetching issues updated since %s for %s/%s...", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)})
		default:
			notify(FetchEvent{Phase: "issues", Kind: "start", Message: fmt.Sprintf("Caching issues for %s/%s...", repo.Owner, repo.Name)})
		}
		issueHandler := func(batch []*github.Issue, total int) error {
			for _, issue := range batch {
				if err := store.SaveIssue(repo.Host, repo.Owner, repo.Name, issue); err != nil {
					return fmt.Errorf("saving issue #%d: %w", issue.Number, err)
				}
				advanceCursor(&info.IssueCursor, issue.UpdatedAt)
			}
			issueCount += len(batch)
			notify(FetchEvent{Phase: "issues", Kind: "batch", Done: issueCount, Total: total})
			return saveInfo() // persist resume cursor after each page
		}
		if _, err := client.FetchAllIssues(repo.Owner, repo.Name, issueSince, issueHandler); err != nil {
			notify(FetchEvent{Phase: "issues", Kind: "done", Done: issueCount})
			return issueCount, prCount, fmt.Errorf("fetching issues: %w", err)
		}
		notify(FetchEvent{Phase: "issues", Kind: "done", Done: issueCount})
	}

	if opts.FetchPRs {
		if !opts.FetchIssues {
			fmt.Println("Skipping issues (--type prs).")
		}

		// --- Pull requests ---
		// The pullRequests connection has no server-side date filter, so the
		// delta/resume/since fetch walks the connection newest-first and stops
		// at the cutoff (exact timestamps, fetched with the same stable API as
		// the cold path).
		prHandler := func(batch []*github.PullRequest, total int) error {
			for _, pr := range batch {
				if err := store.SavePR(repo.Host, repo.Owner, repo.Name, pr); err != nil {
					return fmt.Errorf("saving PR #%d: %w", pr.Number, err)
				}
				advanceCursor(&info.PRCursor, pr.UpdatedAt)
			}
			prCount += len(batch)
			notify(FetchEvent{Phase: "prs", Kind: "batch", Done: prCount, Total: total})
			return saveInfo()
		}
		if info.PRCursor == nil && opts.Since == nil {
			notify(FetchEvent{Phase: "prs", Kind: "start", Message: fmt.Sprintf("Caching pull requests for %s/%s...", repo.Owner, repo.Name)})
			if _, err := client.FetchAllPRs(repo.Owner, repo.Name, prHandler); err != nil {
				notify(FetchEvent{Phase: "prs", Kind: "done", Done: prCount})
				return issueCount, prCount, fmt.Errorf("fetching pull requests: %w", err)
			}
		} else {
			// Both delta/resume (cursor) and an explicit --since use the same
			// newest-first connection walk with an exact-timestamp cutoff.
			prSince := opts.Since
			if prSince == nil && info.PRCursor != nil {
				prSince = info.PRCursor
			}
			if opts.Since != nil {
				notify(FetchEvent{Phase: "prs", Kind: "start", Message: fmt.Sprintf("Fetching pull requests created or updated since %s for %s/%s...", prSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)})
			} else {
				notify(FetchEvent{Phase: "prs", Kind: "start", Message: fmt.Sprintf("Fetching pull requests updated since %s for %s/%s...", prSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)})
			}
			// Phase 1 (locating the window) gets its own spinner bar so
			// users see progress before full pages start arriving.
			if _, err := client.FetchPRsUpdated(repo.Owner, repo.Name, *prSince, prHandler, func(scanned int) {
				notify(FetchEvent{Phase: "prs-scan", Kind: "batch", Done: scanned})
			}); err != nil {
				notify(FetchEvent{Phase: "prs-scan", Kind: "done"})
				notify(FetchEvent{Phase: "prs", Kind: "done", Done: prCount})
				return issueCount, prCount, fmt.Errorf("fetching pull requests: %w", err)
			}
			notify(FetchEvent{Phase: "prs-scan", Kind: "done"})
		}
		notify(FetchEvent{Phase: "prs", Kind: "done", Done: prCount})
	}
	if !opts.FetchPRs {
		fmt.Println("Skipping pull requests (--type issues).")
	}

	return finalizeFetch(store, repo, info, opts, issueCount, prCount)
}

// finalizeFetch records freshness per data type and persists the final cache
// info. A partial run only resets the window of the portion it fetched;
// CachedAt is advanced only when both portions were fetched, so full runs keep
// the legacy single-timestamp behavior.
func finalizeFetch(store cache.Store, repo *gitremote.Repo, info *cache.CacheInfo, opts FetchOptions, issueCount, prCount int) (int, int, error) {
	now := time.Now()
	if opts.FetchIssues {
		info.IssuesCachedAt = now
	}
	if opts.FetchPRs {
		info.PRsCachedAt = now
	}
	if opts.FetchIssues && opts.FetchPRs {
		info.CachedAt = now
		info.Complete = true
	}
	if opts.Duration > 0 {
		info.Duration = opts.Duration
	}
	if err := store.SaveCacheInfoFull(repo.Host, repo.Owner, repo.Name, info); err != nil {
		return issueCount, prCount, fmt.Errorf("saving cache info: %w", err)
	}
	return issueCount, prCount, nil
}

// cliFetchProgress renders fetch events as CLI status lines and progress bars.
// Phases map onto labelled bars; unknown phases still print their messages.
func cliFetchProgress(client *github.Client) FetchProgress {
	type phaseBar struct {
		tracker *progressTracker
	}
	phases := map[string]*phaseBar{}
	return func(ev FetchEvent) {
		p := phases[ev.Phase]
		if p == nil {
			p = &phaseBar{}
			phases[ev.Phase] = p
		}
		switch ev.Kind {
		case "start":
			if ev.Message != "" {
				fmt.Println(ev.Message)
			}
		case "batch":
			if p.tracker == nil {
				p.tracker = newProgressTracker(fetchPhaseLabel(ev.Phase), rateLimitStatus(client))
			}
			p.tracker.setTotal(ev.Total)
			p.tracker.set(ev.Done)
		case "done":
			if p.tracker != nil {
				p.tracker.done()
				p.tracker = nil
			}
			switch ev.Phase {
			case "issues":
				fmt.Printf("Cached %d issue(s).\n", ev.Done)
			case "prs":
				fmt.Printf("Cached %d pull request(s).\n", ev.Done)
			}
		}
	}
}

// fetchPhaseLabel maps a fetch phase onto its progress bar label.
func fetchPhaseLabel(phase string) string {
	switch phase {
	case "prs":
		return "pull requests"
	case "prs-scan":
		return "scanning pull requests"
	default:
		return phase
	}
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

// rateLimitStatus returns a status closure for progress trackers that
// surfaces the live x-ratelimit-remaining value from the last API response,
// or an empty suffix before the first response arrives.
func rateLimitStatus(client *github.Client) func() string {
	return func() string {
		if n := client.RateLimitRemaining(); n >= 0 {
			return fmt.Sprintf(" | %d points left", n)
		}
		return ""
	}
}

// progressTracker adapts github batch callbacks onto a schollz progress bar.
// The bar is created lazily on the first set/setTotal so it can adopt the
// server-reported total (determinate bar) or fall back to a spinner when the
// total is unknown.
type progressTracker struct {
	label  string
	status func() string // optional extra status appended to the description
	writer io.Writer
	bar    *progressbar.ProgressBar
	maxSet bool
}

// newProgressTracker creates a bar tracker. status, when non-nil, is
// evaluated on every update and appended to the bar description (used to show
// the live rate limit remaining).
func newProgressTracker(label string, status func() string) *progressTracker {
	return &progressTracker{label: label, status: status, writer: progressWriter()}
}

// describe refreshes the bar description with the current status suffix.
func (t *progressTracker) describe() {
	if t.bar == nil || t.status == nil {
		return
	}
	t.bar.Describe(t.label + t.status())
}

func (t *progressTracker) setTotal(total int) {
	if t.bar == nil {
		max := -1 // -1 selects spinner mode for an unknown length
		if total > 0 {
			max = total
		}
		t.bar = progressbar.NewOptions(max,
			progressbar.OptionSetDescription(t.label),
			progressbar.OptionSetWriter(t.writer),
			progressbar.OptionShowCount(),
			progressbar.OptionSetWidth(30),
			progressbar.OptionSetPredictTime(false),
			progressbar.OptionThrottle(50*time.Millisecond),
		)
		if t.writer == os.Stderr {
			setActiveBar(t.bar)
		}
	} else if total > 0 && !t.maxSet {
		t.bar.ChangeMax(total)
		t.maxSet = true
	}
	if total > 0 {
		t.maxSet = true
	}
	t.describe()
}

func (t *progressTracker) set(current int) {
	if t.bar == nil {
		t.setTotal(0)
	}
	t.describe()
	t.bar.Set(current)
}

// done finalises the bar: fills a determinate bar if it did not naturally
// complete, then emits a trailing newline.
func (t *progressTracker) done() {
	if t.bar == nil {
		return
	}
	if !t.bar.IsFinished() && t.bar.GetMax() > 0 {
		t.bar.Finish()
	}
	fmt.Fprintln(t.writer)
	if t.writer == os.Stderr {
		setActiveBar(nil)
	}
}

// progressWriter returns os.Stderr when it is a terminal device, otherwise
// io.Discard so progress bars never clutter captured or piped output.
func progressWriter() io.Writer {
	if fi, err := os.Stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return os.Stderr
	}
	return io.Discard
}
