package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
)

var (
	statsAuthors     []string
	statsReviewers   []string
	statsFrom        string
	statsTo          string
	statsState       string
	statsOutput      string
	statsIncludeBots bool
	statsListPRs     bool
)

var statsCmd = &cobra.Command{
	Use:   "stats [REPO...]",
	Short: "Generate an HTML report of repository activity",
	Long: `Generates an HTML report describing pull request activity for one or more
repositories during a period.

Repositories are given as positional [HOST/]OWNER/REPO arguments (defaulting to
the current repository or --repo). The report includes an author x reviewer
matrix counting how many PRs each combination appears in, together with the
median and average time from PR creation to the reviewer's first comment.

Data is served from the local cache when available (run ` + "`ghx cache`" + ` first
for full history); it falls back to the GitHub API when nothing is cached.`,
	RunE: runStats,
}

func init() {
	statsCmd.Flags().StringSliceVarP(&statsAuthors, "author", "A", nil, "Only count PRs authored by these users")
	statsCmd.Flags().StringSliceVar(&statsReviewers, "reviewer", nil, "Only count comments by these reviewers")
	statsCmd.Flags().StringVar(&statsFrom, "from", "", "Period start (YYYY-MM-DD or RFC3339)")
	statsCmd.Flags().StringVar(&statsTo, "to", "", "Period end, inclusive (YYYY-MM-DD or RFC3339)")
	statsCmd.Flags().StringVarP(&statsState, "state", "s", "all", "Filter by PR state: {open|closed|merged|all}")
	statsCmd.Flags().StringVarP(&statsOutput, "output", "o", "stats-report.html", "Output HTML file path")
	statsCmd.Flags().BoolVar(&statsIncludeBots, "include-bots", false, "Include bot accounts as reviewers")
	statsCmd.Flags().BoolVar(&statsListPRs, "list-prs", false, "Include the table of individual pull requests (can be large)")
}

func runStats(cmd *cobra.Command, args []string) error {
	from, err := parseStatsTime(statsFrom, false)
	if err != nil {
		return err
	}
	to, err := parseStatsTime(statsTo, true)
	if err != nil {
		return err
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return fmt.Errorf("--to (%s) is before --from (%s)", statsTo, statsFrom)
	}

	repos, err := resolveStatsRepos(args)
	if err != nil {
		return err
	}

	store := newStore()
	var allPRs []*repoPR
	for _, repo := range repos {
		prs, err := loadStatsPRs(store, repo, from)
		if err != nil {
			return err
		}
		for _, pr := range prs {
			allPRs = append(allPRs, &repoPR{Repo: repo.Owner + "/" + repo.Name, PR: pr})
		}
		fmt.Fprintf(os.Stderr, "Loaded %d pull request(s) for %s/%s\n", len(prs), repo.Owner, repo.Name)
	}

	filters := statsFilters{
		Authors:     toLowerSet(statsAuthors),
		Reviewers:   toLowerSet(statsReviewers),
		From:        from,
		To:          to,
		State:       statsState,
		IncludeBots: statsIncludeBots,
		ListPRs:     statsListPRs,
	}

	report := buildReport(repos, allPRs, filters)

	html, err := renderReport(report)
	if err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}
	if err := os.WriteFile(statsOutput, []byte(html), 0644); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	fmt.Printf("Report written to %s (%d pull request(s) across %d repositories).\n",
		statsOutput, report.TotalPRs, len(repos))
	return nil
}

// resolveStatsRepos parses positional [HOST/]OWNER/REPO arguments, falling back
// to the --repo flag or the current directory's git remote when none are given.
func resolveStatsRepos(args []string) ([]*gitremote.Repo, error) {
	if len(args) == 0 {
		repo, err := getRepo()
		if err != nil {
			return nil, err
		}
		return []*gitremote.Repo{repo}, nil
	}

	repos := make([]*gitremote.Repo, 0, len(args))
	seen := make(map[string]bool)
	for _, arg := range args {
		repo, err := gitremote.ParseRepo(arg)
		if err != nil {
			return nil, err
		}
		key := repo.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		repos = append(repos, repo)
	}
	return repos, nil
}

// loadStatsPRs returns the pull requests used for the report. A fresh cache is
// served as-is, a stale cache is served with a warning, and an empty cache
// falls back to the GitHub API. The fallback deliberately uses the search API
// (FetchPRsUpdated) rather than the plain PR list because the reviewer and
// time-to-first-comment metrics require comment authors and timestamps, which
// the list connection does not return. The search API is day-granular and
// capped at 1000 results.
func loadStatsPRs(store *cache.Store, repo *gitremote.Repo, from time.Time) ([]*github.PullRequest, error) {
	fresh, _ := store.IsCacheFresh(repo.Host, repo.Owner, repo.Name)
	if prs, err := store.LoadAllPRs(repo.Host, repo.Owner, repo.Name); err == nil && len(prs) > 0 {
		if !fresh {
			fmt.Fprintf(os.Stderr, "warning: cache for %s/%s is stale; run `ghx cache -R %s/%s` for complete data\n",
				repo.Owner, repo.Name, repo.Owner, repo.Name)
		}
		return prs, nil
	}

	client, err := newClient(repo.Host)
	if err != nil {
		return nil, err
	}
	since := from
	if since.IsZero() {
		since = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	fmt.Fprintf(os.Stderr, "No cached pull requests for %s/%s; fetching (with comments) from GitHub since %s...\n",
		repo.Owner, repo.Name, since.Format("2006-01-02"))
	return client.FetchPRsUpdated(repo.Owner, repo.Name, since, nil)
}

// toLowerSet converts a slice of logins to a lowercase set. An empty input
// returns nil, meaning "no restriction".
func toLowerSet(logins []string) map[string]bool {
	if len(logins) == 0 {
		return nil
	}
	set := make(map[string]bool, len(logins))
	for _, l := range logins {
		set[strings.ToLower(strings.TrimSpace(l))] = true
	}
	return set
}

// ---------------------------------------------------------------------------
// Period parsing
// ---------------------------------------------------------------------------

var statsTimeLayouts = []string{
	"2006-01-02",
	"2006-01-02 15:04",
	"2006-01-02 15:04:05",
	time.RFC3339,
}

// parseStatsTime parses a period boundary. Date-only values are interpreted as
// midnight, or as the last nanosecond of the day when end is true, so --to is
// inclusive. An empty string returns the zero time (unbounded).
func parseStatsTime(s string, end bool) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, nil
	}
	for _, layout := range statsTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			if end && layout == "2006-01-02" {
				return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location()), nil
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD or RFC3339", s)
}
