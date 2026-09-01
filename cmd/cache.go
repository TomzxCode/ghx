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
(the default) refreshes both.`,
	RunE: runCache,
}

func init() {
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

	store := newStore()
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

	// --force discards partial resume state and starts a full fetch. With a
	// partial --type, only the selected portion is reset.
	if cacheForce {
		if fetchIssues {
			info.IssueCursor = nil
		}
		if fetchPRs {
			info.PRCursor = nil
		}
		if fetchIssues && fetchPRs {
			info.Complete = false
		}
	}

	// --since only applies to a complete cache: applying it to an interrupted
	// fetch would mark the cache complete while most history is missing, so in
	// that case finish the full fetch instead and ignore --since.
	if sinceTime != nil && !info.Complete {
		fmt.Println("Cache is incomplete; resuming the full fetch (--since ignored).")
		sinceTime = nil
	}

	if !cacheForce && sinceTime == nil && cacheType == "both" && info.Complete && time.Since(info.CachedAt) < time.Duration(cacheDuration)*time.Minute {
		fmt.Printf("Cache is still fresh (within %d minutes). Use --force to refresh anyway.\n", cacheDuration)
		return nil
	}

	saveInfo := func() error {
		return store.SaveCacheInfoFull(repo.Host, repo.Owner, repo.Name, info)
	}

	if fetchIssues {
		// --- Issues ---
		// Issues are fetched oldest-first. `since = IssueCursor` either resumes an
		// interrupted fetch (cursor mid-history) or delta-updates a complete cache
		// (cursor near newest). nil cursor => cold fetch from the beginning.
		// An explicit --since overrides the cursor for this run.
		issueSince := info.IssueCursor
		if sinceTime != nil {
			issueSince = sinceTime
		}
		if issueSince != nil && !info.Complete {
			fmt.Printf("Resuming issues from %s for %s/%s...\n", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
		} else if issueSince != nil && sinceTime != nil {
			fmt.Printf("Fetching issues created or updated since %s for %s/%s...\n", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
		} else if issueSince != nil {
			fmt.Printf("Fetching issues updated since %s for %s/%s...\n", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
		} else {
			fmt.Printf("Caching issues for %s/%s...\n", repo.Owner, repo.Name)
		}
		tracker := newProgressTracker("issues", rateLimitStatus(client))
		issueCount := 0
		issueHandler := func(batch []*github.Issue, total int) error {
			tracker.setTotal(total)
			for _, issue := range batch {
				if err := store.SaveIssue(repo.Host, repo.Owner, repo.Name, issue); err != nil {
					return fmt.Errorf("saving issue #%d: %w", issue.Number, err)
				}
				advanceCursor(&info.IssueCursor, issue.UpdatedAt)
			}
			issueCount += len(batch)
			tracker.set(issueCount)
			return saveInfo() // persist resume cursor after each page
		}
		if _, err := client.FetchAllIssues(repo.Owner, repo.Name, issueSince, issueHandler); err != nil {
			tracker.done()
			return fmt.Errorf("fetching issues: %w", err)
		}
		tracker.done()
		fmt.Printf("Cached %d issue(s).\n", issueCount)
	}

	if fetchPRs {
		if !fetchIssues {
			fmt.Println("Skipping issues (--type prs).")
		}

		// --- Pull requests ---
		// The pullRequests connection has no server-side date filter, so the
		// delta/resume/since fetch walks the connection newest-first and stops
		// at the cutoff (exact timestamps, fetched with the same stable API as
		// the cold path).
		prCount := 0
		prTracker := newProgressTracker("pull requests", rateLimitStatus(client))
		prHandler := func(batch []*github.PullRequest, total int) error {
			prTracker.setTotal(total)
			for _, pr := range batch {
				if err := store.SavePR(repo.Host, repo.Owner, repo.Name, pr); err != nil {
					return fmt.Errorf("saving PR #%d: %w", pr.Number, err)
				}
				advanceCursor(&info.PRCursor, pr.UpdatedAt)
			}
			prCount += len(batch)
			prTracker.set(prCount)
			return saveInfo()
		}
		if info.PRCursor == nil && sinceTime == nil {
			fmt.Printf("Caching pull requests for %s/%s...\n", repo.Owner, repo.Name)
			if _, err := client.FetchAllPRs(repo.Owner, repo.Name, prHandler); err != nil {
				prTracker.done()
				return fmt.Errorf("fetching pull requests: %w", err)
			}
		} else {
			// Both delta/resume (cursor) and an explicit --since use the same
			// newest-first connection walk with an exact-timestamp cutoff.
			prSince := sinceTime
			if prSince == nil && info.PRCursor != nil {
				prSince = info.PRCursor
			}
			if sinceTime != nil {
				fmt.Printf("Fetching pull requests created or updated since %s for %s/%s...\n", prSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
			} else {
				fmt.Printf("Fetching pull requests updated since %s for %s/%s...\n", prSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
			}
			// Phase 1 (locating the window) gets its own spinner bar so
			// users see progress before full pages start arriving.
			walkTracker := newProgressTracker("scanning pull requests", rateLimitStatus(client))
			if _, err := client.FetchPRsUpdated(repo.Owner, repo.Name, *prSince, prHandler, func(scanned int) {
				walkTracker.set(scanned)
			}); err != nil {
				walkTracker.done()
				prTracker.done()
				return fmt.Errorf("fetching pull requests: %w", err)
			}
			walkTracker.done()
		}
		prTracker.done()
		fmt.Printf("Cached %d pull request(s).\n", prCount)
	}
	if !fetchPRs {
		fmt.Println("Skipping pull requests (--type issues).")
	}

	// Only a run that fetched both portions may mark the cache complete: the
	// skipped portion of a partial run may never have been fetched.
	if fetchIssues && fetchPRs {
		info.Complete = true
	}
	info.CachedAt = time.Now()
	info.Duration = cacheDuration
	if err := saveInfo(); err != nil {
		return fmt.Errorf("saving cache info: %w", err)
	}

	fmt.Printf("Cache updated. Valid for %d minute(s).\n", cacheDuration)
	return nil
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
}

// progressWriter returns os.Stderr when it is a terminal device, otherwise
// io.Discard so progress bars never clutter captured or piped output.
func progressWriter() io.Writer {
	if fi, err := os.Stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return os.Stderr
	}
	return io.Discard
}
