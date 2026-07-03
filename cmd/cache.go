package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
)

var (
	cacheDuration int
	cacheForce    bool
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
than starting over.`,
	RunE: runCache,
}

func init() {
	cacheCmd.Flags().IntVar(&cacheDuration, "cache-duration", 60, "Cache duration in minutes")
	cacheCmd.Flags().BoolVar(&cacheForce, "force", false, "Re-fetch even if the cache is still fresh")
}

func runCache(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	store := newStore()
	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}

	info, _ := store.LoadCacheInfo(repo.Host, repo.Owner, repo.Name)
	if info == nil {
		info = &cache.CacheInfo{}
	}

	// --force discards any partial resume state and starts a full fetch.
	if cacheForce {
		info.Complete = false
		info.IssueCursor = nil
		info.PRCursor = nil
	}

	if !cacheForce && info.Complete && time.Since(info.CachedAt) < time.Duration(cacheDuration)*time.Minute {
		fmt.Printf("Cache is still fresh (within %d minutes). Use --force to refresh anyway.\n", cacheDuration)
		return nil
	}

	saveInfo := func() error {
		return store.SaveCacheInfoFull(repo.Host, repo.Owner, repo.Name, info)
	}

	// --- Issues ---
	// Issues are fetched oldest-first. `since = IssueCursor` either resumes an
	// interrupted fetch (cursor mid-history) or delta-updates a complete cache
	// (cursor near newest). nil cursor => cold fetch from the beginning.
	issueSince := info.IssueCursor
	if issueSince != nil && !info.Complete {
		fmt.Printf("Resuming issues from %s for %s/%s...\n", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
	} else if issueSince != nil {
		fmt.Printf("Fetching issues updated since %s for %s/%s...\n", issueSince.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
	} else {
		fmt.Printf("Caching issues for %s/%s...\n", repo.Owner, repo.Name)
	}
	tracker := newProgressTracker("issues")
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

	// --- Pull requests ---
	// The pullRequests connection has no server-side date filter, so a cold
	// fetch (no cursor) uses the connection, while delta/resume (cursor set)
	// uses the search API with updated:>=cursor.
	prCount := 0
	prTracker := newProgressTracker("pull requests")
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
	if info.PRCursor == nil {
		fmt.Printf("Caching pull requests for %s/%s...\n", repo.Owner, repo.Name)
		if _, err := client.FetchAllPRs(repo.Owner, repo.Name, prHandler); err != nil {
			prTracker.done()
			return fmt.Errorf("fetching pull requests: %w", err)
		}
	} else {
		fmt.Printf("Fetching pull requests updated since %s for %s/%s...\n", info.PRCursor.Format("2006-01-02 15:04"), repo.Owner, repo.Name)
		if _, err := client.FetchPRsUpdated(repo.Owner, repo.Name, *info.PRCursor, prHandler); err != nil {
			prTracker.done()
			return fmt.Errorf("fetching pull requests: %w", err)
		}
	}
	prTracker.done()
	fmt.Printf("Cached %d pull request(s).\n", prCount)

	// Mark the cache complete. CachedAt uses the newest data timestamp when
	// available (more accurate than wall-clock against server timestamps).
	info.Complete = true
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

// progressTracker adapts github batch callbacks onto a schollz progress bar.
// The bar is created lazily on the first set/setTotal so it can adopt the
// server-reported total (determinate bar) or fall back to a spinner when the
// total is unknown.
type progressTracker struct {
	label  string
	writer io.Writer
	bar    *progressbar.ProgressBar
	maxSet bool
}

func newProgressTracker(label string) *progressTracker {
	return &progressTracker{label: label, writer: progressWriter()}
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
}

func (t *progressTracker) set(current int) {
	if t.bar == nil {
		t.setTotal(0)
	}
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
