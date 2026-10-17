package cmd

import (
	"sync"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/logging"
)

// readCacheWorkers bounds how many per-item detail fetches run at once when the
// read commands (list, view) backfill the cache. It stays low, like the cache
// command's parallel PR pages, because GitHub's secondary rate limits react to
// concurrent request bursts.
const readCacheWorkers = 4

// backfillIssueDetails writes the full record for each listed issue the
// repository does not already have cached, so browsing a list incrementally
// fills the same cache `ghx cache` writes. Issues with no comments are stored
// from the list payload directly: with zero comments the list and detail
// queries select the same fields, so the list record already equals the detail
// record. Issues with comments are re-fetched with the detail query so the
// cached record carries the comment bodies. Failures are logged and never fail
// the listing.
func backfillIssueDetails(store cache.Store, client *github.Client, repo *gitremote.Repo, issues []*github.Issue) {
	scanStart := time.Now()
	missing := make([]*github.Issue, 0, len(issues))
	needDetail := 0
	for _, issue := range issues {
		if _, _, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, issue.Number); err == nil {
			continue
		}
		if issue.CommentCount > 0 {
			needDetail++
		}
		missing = append(missing, issue)
	}
	logging.Debug("cache backfill: scanned", "issues", len(issues), "missing", len(missing), "detail", needDetail, "duration", time.Since(scanStart))
	if len(missing) == 0 {
		return
	}
	logging.Info("caching issues not in the local cache", "count", len(missing))

	start := time.Now()
	runBounded(readCacheWorkers, len(missing), func(i int) {
		itemStart := time.Now()
		issue := missing[i]
		full := issue
		source := "list"
		if issue.CommentCount > 0 {
			source = "detail"
			fetched, err := client.GetIssue(repo.Owner, repo.Name, issue.Number)
			if err != nil {
				logging.Warn("fetching issue detail for cache", "number", issue.Number, "error", err)
				return
			}
			full = fetched
		}
		if err := store.SaveIssue(repo.Host, repo.Owner, repo.Name, full); err != nil {
			logging.Warn("caching issue", "number", issue.Number, "error", err)
			return
		}
		logging.Debug("cache backfill: issue", "number", issue.Number, "source", source, "duration", time.Since(itemStart))
	})
	logging.Debug("cache backfill: done", "cached", len(missing), "detail", needDetail, "workers", readCacheWorkers, "duration", time.Since(start))
}

// backfillPRDetails writes the full record for each listed pull request the
// repository does not already have cached. Unlike issues, a list PR payload is
// always missing detail the cache record carries (additions, deletions, reviews,
// timeline events), so every new entry is re-fetched with the same detail query
// `view` and `ghx cache` use. Failures are logged and never fail the listing.
func backfillPRDetails(store cache.Store, client *github.Client, repo *gitremote.Repo, prs []*github.PullRequest) {
	scanStart := time.Now()
	missing := make([]*github.PullRequest, 0, len(prs))
	for _, pr := range prs {
		if _, _, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, pr.Number); err == nil {
			continue
		}
		missing = append(missing, pr)
	}
	logging.Debug("cache backfill: scanned", "prs", len(prs), "missing", len(missing), "duration", time.Since(scanStart))
	if len(missing) == 0 {
		return
	}
	logging.Info("caching pull requests not in the local cache", "count", len(missing))

	start := time.Now()
	runBounded(readCacheWorkers, len(missing), func(i int) {
		itemStart := time.Now()
		pr := missing[i]
		full, err := client.GetPR(repo.Owner, repo.Name, pr.Number)
		if err != nil {
			logging.Warn("fetching pull request detail for cache", "number", pr.Number, "error", err)
			return
		}
		if err := store.SavePR(repo.Host, repo.Owner, repo.Name, full); err != nil {
			logging.Warn("caching pull request", "number", pr.Number, "error", err)
			return
		}
		logging.Debug("cache backfill: pr", "number", pr.Number, "source", "detail", "duration", time.Since(itemStart))
	})
	logging.Debug("cache backfill: done", "cached", len(missing), "detail", len(missing), "workers", readCacheWorkers, "duration", time.Since(start))
}

// runBounded runs fn(0)..fn(n-1) with at most workers in flight, blocking until
// every call has returned.
func runBounded(workers, n int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if workers > n {
		workers = n
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
