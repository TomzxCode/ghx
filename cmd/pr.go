package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/logging"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// ---------------------------------------------------------------------------
// Command tree
// ---------------------------------------------------------------------------

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Work with GitHub pull requests",
}

// pr list flags
var (
	prListApp        string
	prListAssignee   string
	prListAuthor     string
	prListBase       string
	prListDraft      bool
	prListHead       string
	prListLabels     []string
	prListLimit      int
	prListSearch     string
	prListState      string
	prListJSON       string
	prListNoTruncate bool
)

// pr view flags
var (
	prViewComments bool
	prViewJSON     bool
	prViewRefresh  bool
)

// pr checks flags
var prChecksJSON bool

var prListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pull requests",
	RunE:  runPRList,
}

var prViewCmd = &cobra.Command{
	Use:   "view <number>",
	Short: "View a pull request",
	Args:  cobra.ExactArgs(1),
	RunE:  runPRView,
}

var prChecksCmd = &cobra.Command{
	Use:   "checks <number>",
	Short: "List the checks behind a pull request's head-commit rollup",
	Long: `Lists each check run and commit status reporting on a pull request's head
commit, with the aggregate rollup state. The checks are read live: the cached
pull request record carries only the rollup's state and how many checks there
are, not the checks themselves.`,
	Args: cobra.ExactArgs(1),
	RunE: runPRChecks,
}

func init() {
	prCmd.AddCommand(prListCmd)
	prCmd.AddCommand(prViewCmd)
	prCmd.AddCommand(prChecksCmd)

	prChecksCmd.Flags().BoolVar(&prChecksJSON, "json", false, "Output as JSON")

	prListCmd.Flags().StringVar(&prListApp, "app", "", "Filter by GitHub App author")
	prListCmd.Flags().StringVarP(&prListAssignee, "assignee", "a", "", "Filter by assignee (login or @me)")
	prListCmd.Flags().StringVarP(&prListAuthor, "author", "A", "", "Filter by author (login or @me)")
	prListCmd.Flags().StringVarP(&prListBase, "base", "B", "", "Filter by base branch")
	prListCmd.Flags().BoolVarP(&prListDraft, "draft", "d", false, "Filter by draft state")
	prListCmd.Flags().StringVarP(&prListHead, "head", "H", "", "Filter by head branch")
	prListCmd.Flags().StringSliceVarP(&prListLabels, "label", "l", nil, "Filter by label")
	prListCmd.Flags().IntVarP(&prListLimit, "limit", "L", 1000, "Maximum number of items to fetch")
	prListCmd.Flags().StringVarP(&prListSearch, "search", "S", "", "Search pull requests with query")
	prListCmd.Flags().StringVarP(&prListState, "state", "s", "open", "Filter by state: {open|closed|merged|all}")
	prListCmd.Flags().StringVar(&prListJSON, "json", "", "Output JSON with the specified fields (comma-separated; omit for all)")
	prListCmd.Flags().Lookup("json").NoOptDefVal = prJSONAll
	prListCmd.Flags().BoolVar(&prListNoTruncate, "no-truncate", false, "Don't truncate long titles")

	prViewCmd.Flags().BoolVarP(&prViewComments, "comments", "c", false, "View pull request comments")
	prViewCmd.Flags().BoolVar(&prViewJSON, "json", false, "Output as JSON")
	prViewCmd.Flags().BoolVar(&prViewRefresh, "refresh", false, "Force fetch from GitHub and update cache")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func runPRList(cmd *cobra.Command, args []string) error {
	jsonFields, err := resolvePRListJSONFields(args)
	if err != nil {
		return err
	}

	repos, err := getRepos()
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()

	me := &meResolver{}
	clients := make(map[string]*github.Client)
	lists := make([]repoPRList, 0, len(repos))
	for _, repo := range repos {
		assignee, err := me.resolve(prListAssignee, repo.Host)
		if err != nil {
			return err
		}
		author, err := me.resolve(prListAuthor, repo.Host)
		if err != nil {
			return err
		}
		prs, total, err := listPRsForRepo(store, repo, assignee, author, func() (*github.Client, error) {
			return clientForHost(clients, repo.Host)
		})
		if err != nil {
			return err
		}
		lists = append(lists, repoPRList{repo: repo, prs: prs, total: total})
	}
	return printPRLists(lists, jsonFields, prListNoTruncate)
}

// repoPRList is one repository's pull requests within a `pr list` run.
type repoPRList struct {
	repo  *gitremote.Repo
	prs   []*github.PullRequest
	total int
}

// listPRsForRepo lists one repository's pull requests from the cache when it is
// fresh, or from the GitHub API otherwise. The limit applies per repository.
func listPRsForRepo(store cache.Store, repo *gitremote.Repo, assignee, author string, clientFor func() (*github.Client, error)) ([]*github.PullRequest, int, error) {
	cacheStart := time.Now()
	if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
		prs, err := store.QueryPRs(repo.Host, repo.Owner, repo.Name, cache.PRQuery{
			State:    prListState,
			Assignee: assignee,
			Author:   author,
			Labels:   prListLabels,
			BaseRef:  prListBase,
			HeadRef:  prListHead,
			Draft:    prListDraft,
			Search:   prListSearch,
		})
		if err == nil {
			sort.Slice(prs, func(i, j int) bool {
				return prs[i].UpdatedAt.After(prs[j].UpdatedAt)
			})
			total := len(prs)
			if prListLimit > 0 && len(prs) > prListLimit {
				prs = prs[:prListLimit]
			}
			logging.Debug("pr list: served from cache", "repo", repoLabel(repo.Host, repo.Owner, repo.Name), "count", total, "duration", time.Since(cacheStart))
			recordRetrievalOutcome(repo, telemetry.RetrievalPRList, true)
			return prs, total, nil
		}
		logging.Debug("pr list: cache query failed, falling back to API", "repo", repoLabel(repo.Host, repo.Owner, repo.Name), "error", err, "duration", time.Since(cacheStart))
	}

	// Fall back to the GitHub API.
	client, err := clientFor()
	if err != nil {
		return nil, 0, err
	}

	opts := github.PRListOptions{
		Limit:    prListLimit,
		State:    prListState,
		Assignee: assignee,
		Author:   author,
		Labels:   prListLabels,
		Base:     prListBase,
		Head:     prListHead,
		Draft:    prListDraft,
		Search:   prListSearch,
		App:      prListApp,
	}

	listStart := time.Now()
	prs, err := client.ListPRs(repo.Owner, repo.Name, opts)
	if err != nil {
		return nil, 0, err
	}
	logging.Debug("pr list: fetched from API", "repo", repoLabel(repo.Host, repo.Owner, repo.Name), "count", len(prs), "duration", time.Since(listStart))
	recordRetrievalOutcome(repo, telemetry.RetrievalPRList, false)

	backfillPRDetails(store, client, repo, prs)
	return prs, len(prs), nil
}

func runPRView(cmd *cobra.Command, args []string) error {
	number, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid pull request number: %s", args[0])
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

	if !prViewRefresh {
		// One metadata read answers both checks: whether the full cache is
		// fresh, and how long an individual item may be served.
		info, _ := store.LoadCacheInfo(repo.Host, repo.Owner, repo.Name)
		// When a full cache is fresh, serve the item from it. A missing item is
		// treated as a cache miss (fetched and cached below) rather than an
		// error, so browsing fills gaps the full cache does not cover.
		if info.PRsFreshAt(time.Now()) {
			lookupStart := time.Now()
			if pr, _, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, number); err == nil {
				logging.Debug("pr view: served from cache", "number", number, "source", "full-cache", "duration", time.Since(lookupStart))
				recordRetrievalOutcome(repo, telemetry.RetrievalPRView, true)
				return printPRView(pr, prViewComments, prViewJSON)
			}
			logging.Debug("pr view: not in fresh cache, fetching from API", "number", number, "duration", time.Since(lookupStart))
		} else if pr, mtime, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, number); err == nil {
			// No fresh full cache — serve the individual item while it is
			// within the repository's configured freshness window.
			if cache.IsItemFreshAt(info, mtime) {
				logging.Debug("pr view: served from cache", "number", number, "source", "item", "age", time.Since(mtime))
				recordRetrievalOutcome(repo, telemetry.RetrievalPRView, true)
				return printPRView(pr, prViewComments, prViewJSON)
			}
			logging.Debug("pr view: cached item is stale, fetching from API", "number", number, "age", time.Since(mtime))
		} else {
			logging.Debug("pr view: not in cache, fetching from API", "number", number, "error", err)
		}
	}

	// Issues and pull requests share one number sequence, so a cached issue
	// proves no pull request with this number exists. Report the type instead
	// of asking GitHub for a pull request it cannot return.
	if _, _, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, number); err == nil {
		logging.Debug("pr view: cached item is an issue", "number", number)
		return fmt.Errorf("#%d is an issue, not a pull request; use \"ghx issue view %d\"", number, number)
	}

	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}

	fetchStart := time.Now()
	pr, err := client.GetPR(repo.Owner, repo.Name, number)
	if err != nil {
		return err
	}
	fetchDuration := time.Since(fetchStart)
	if !prViewRefresh {
		// A forced refresh asks the API for fresh data directly; it is not a
		// cache miss, so it stays out of the hit rate.
		recordRetrievalOutcome(repo, telemetry.RetrievalPRView, false)
	}

	saveStart := time.Now()
	saveErr := store.SavePR(repo.Host, repo.Owner, repo.Name, pr)
	if saveErr != nil {
		logging.Warn("caching pull request", "number", number, "error", saveErr)
	}
	logging.Debug("pr view: fetched from API", "number", number, "fetch", fetchDuration, "save", time.Since(saveStart))
	return printPRView(pr, prViewComments, prViewJSON)
}

// runPRChecks lists the checks behind one pull request's rollup. The cached
// record carries only the rollup state, so this reads GitHub directly.
func runPRChecks(cmd *cobra.Command, args []string) error {
	number, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid pull request number: %s", args[0])
	}

	repo, err := getRepo()
	if err != nil {
		return err
	}

	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}

	rollup, err := client.GetPRChecks(repo.Owner, repo.Name, number)
	if err != nil {
		return err
	}
	if prChecksJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rollup)
	}
	return printPRChecks(rollup)
}

func formatReviewDecision(d string) string {
	switch d {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes requested"
	case "REVIEW_REQUIRED":
		return "review required"
	default:
		return d
	}
}

// formatCheckState renders a head-commit rollup state for the terminal.
func formatCheckState(s string) string {
	switch s {
	case "SUCCESS":
		return "passing"
	case "FAILURE", "ERROR":
		return "failing"
	case "PENDING", "EXPECTED":
		return "running"
	default:
		return s
	}
}

// ---------------------------------------------------------------------------
// Display
// ---------------------------------------------------------------------------

func printPRLists(lists []repoPRList, jsonFields []string, noTruncate bool) error {
	if jsonFields != nil {
		return printPRListsJSON(lists, jsonFields)
	}

	multi := len(lists) > 1
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	shown, total := 0, 0
	for _, list := range lists {
		for _, pr := range list.prs {
			draft := ""
			if pr.IsDraft {
				draft = " [draft]"
			}
			count := pr.CommentCount
			if count == 0 {
				count = len(pr.Comments)
			}
			title := pr.Title
			if !noTruncate {
				title = truncate(title, 55)
			}
			review := formatReviewDecision(pr.ReviewDecision)
			number := fmt.Sprintf("#%d", pr.Number)
			if multi {
				number = fmt.Sprintf("%s#%d", repoLabel(list.repo.Host, list.repo.Owner, list.repo.Name), pr.Number)
			}
			fmt.Fprintf(w, "%s\t%s%s\t%s → %s\t%s\t%d\t%s\n",
				number,
				title,
				draft,
				pr.HeadRefName,
				pr.BaseRefName,
				review,
				count,
				pr.UpdatedAt.Format("2006-01-02"),
			)
			shown++
		}
		total += list.total
	}
	w.Flush()

	if shown == 0 {
		fmt.Println("No pull requests found.")
		return nil
	}
	if total > shown {
		logging.Info("showing pull requests", "shown", shown, "total", total)
	}
	return nil
}

// printPRListsJSON emits one JSON array for a single repository, keeping the
// existing output shape, and an object keyed by repository label when several
// repositories were listed.
func printPRListsJSON(lists []repoPRList, fields []string) error {
	if len(lists) == 1 {
		return printPRListJSON(lists[0].prs, fields)
	}

	out := make(map[string][]map[string]any, len(lists))
	for _, list := range lists {
		out[repoLabel(list.repo.Host, list.repo.Owner, list.repo.Name)] = prJSONRows(list.prs, fields)
	}
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(out)
}

// printPRChecks renders the head commit's rollup and each check reporting on it.
func printPRChecks(rollup *github.PRCheckRollup) error {
	if rollup == nil || len(rollup.Checks) == 0 {
		fmt.Println("No checks on the head commit.")
		return nil
	}
	fmt.Printf("%s • %d check(s)\n\n", formatCheckState(rollup.State), rollup.Total)
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	for _, check := range rollup.Checks {
		fmt.Fprintf(w, "%s\t%s\n", checkState(check), check.Name)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if rollup.Total > len(rollup.Checks) {
		fmt.Printf("\nShowing %d of %d checks.\n", len(rollup.Checks), rollup.Total)
	}
	return nil
}

// checkState renders one check's state: a check run reports its conclusion once
// it finishes and its status while it runs, and a commit status reports a state
// directly.
func checkState(check github.PRCheck) string {
	raw := check.State
	if check.Type != "StatusContext" {
		raw = check.Conclusion
		if check.Status != "" && !strings.EqualFold(check.Status, "COMPLETED") {
			raw = check.Status
		}
	}
	raw = strings.ToUpper(raw)
	if formatted := formatCheckState(raw); formatted != raw {
		return formatted
	}
	return strings.ToLower(raw)
}

func printPRView(pr *github.PullRequest, showComments bool, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(pr)
	}

	fmt.Printf("#%d %s\n", pr.Number, pr.Title)

	draftTag := ""
	if pr.IsDraft {
		draftTag = " • DRAFT"
	}
	commentCount := pr.CommentCount
	if commentCount == 0 {
		commentCount = len(pr.Comments)
	}
	fmt.Printf("%s%s • opened by %s • %d comment(s)\n",
		strings.ToUpper(pr.State), draftTag, pr.Author.Login, commentCount)
	fmt.Println()

	fmt.Printf("Branch:    %s → %s\n", pr.HeadRefName, pr.BaseRefName)
	if pr.ReviewDecision != "" {
		fmt.Printf("Review:    %s\n", formatReviewDecision(pr.ReviewDecision))
	}
	if pr.Checks != nil && pr.Checks.State != "" {
		fmt.Printf("Checks:    %s\n", formatCheckState(pr.Checks.State))
	}
	if pr.ReviewThreads != nil && pr.ReviewThreads.Total > 0 {
		fmt.Printf("Threads:   %d open of %d\n", pr.ReviewThreads.Open, pr.ReviewThreads.Total)
	}
	if len(pr.Labels) > 0 {
		names := make([]string, len(pr.Labels))
		for i, l := range pr.Labels {
			names[i] = l.Name
		}
		fmt.Printf("Labels:    %s\n", strings.Join(names, ", "))
	}
	if len(pr.Assignees) > 0 {
		logins := make([]string, len(pr.Assignees))
		for i, a := range pr.Assignees {
			logins[i] = a.Login
		}
		fmt.Printf("Assignees: %s\n", strings.Join(logins, ", "))
	}
	if pr.Milestone != nil {
		fmt.Printf("Milestone: %s\n", pr.Milestone.Title)
	}
	fmt.Printf("Created:   %s\n", pr.CreatedAt.Format("2006-01-02 15:04"))
	if pr.MergedAt != nil {
		fmt.Printf("Merged:    %s\n", pr.MergedAt.Format("2006-01-02 15:04"))
	} else if pr.ClosedAt != nil {
		fmt.Printf("Closed:    %s\n", pr.ClosedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("URL:       %s\n", pr.URL)
	fmt.Println()
	fmt.Println(pr.Body)

	if showComments && len(pr.Comments) > 0 {
		for i, c := range pr.Comments {
			fmt.Printf("\n── Comment %d by %s (%s) ──\n\n",
				i+1, c.Author.Login, c.CreatedAt.Format("2006-01-02 15:04"))
			fmt.Println(c.Body)
		}
	}
	return nil
}
