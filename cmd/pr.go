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

func init() {
	prCmd.AddCommand(prListCmd)
	prCmd.AddCommand(prViewCmd)

	prListCmd.Flags().StringVar(&prListApp, "app", "", "Filter by GitHub App author")
	prListCmd.Flags().StringVarP(&prListAssignee, "assignee", "a", "", "Filter by assignee")
	prListCmd.Flags().StringVarP(&prListAuthor, "author", "A", "", "Filter by author")
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

	repo, err := getRepo()
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()

	// Serve from cache when it is fresh.
	cacheStart := time.Now()
	if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
		prs, err := store.QueryPRs(repo.Host, repo.Owner, repo.Name, cache.PRQuery{
			State:    prListState,
			Assignee: prListAssignee,
			Author:   prListAuthor,
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
			logging.Debug("pr list: served from cache", "count", total, "duration", time.Since(cacheStart))
			recordRetrievalOutcome(repo, telemetry.RetrievalPRList, true)
			return printPRList(prs, total, jsonFields, prListNoTruncate)
		}
		logging.Debug("pr list: cache query failed, falling back to API", "error", err, "duration", time.Since(cacheStart))
	}

	// Fall back to the GitHub API.
	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}

	opts := github.PRListOptions{
		Limit:    prListLimit,
		State:    prListState,
		Assignee: prListAssignee,
		Author:   prListAuthor,
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
		return err
	}
	logging.Debug("pr list: fetched from API", "count", len(prs), "duration", time.Since(listStart))
	recordRetrievalOutcome(repo, telemetry.RetrievalPRList, false)

	backfillPRDetails(store, client, repo, prs)
	return printPRList(prs, len(prs), jsonFields, prListNoTruncate)
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
		// When a full cache is fresh, serve the item from it. A missing item is
		// treated as a cache miss (fetched and cached below) rather than an
		// error, so browsing fills gaps the full cache does not cover.
		if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
			lookupStart := time.Now()
			if pr, _, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, number); err == nil {
				logging.Debug("pr view: served from cache", "number", number, "source", "full-cache", "duration", time.Since(lookupStart))
				recordRetrievalOutcome(repo, telemetry.RetrievalPRView, true)
				return printPRView(pr, prViewComments, prViewJSON)
			}
			logging.Debug("pr view: not in fresh cache, fetching from API", "number", number, "duration", time.Since(lookupStart))
		} else if pr, mtime, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, number); err == nil {
			// No fresh full cache — serve the individual file when it is recent.
			if time.Since(mtime) < 60*time.Minute {
				logging.Debug("pr view: served from cache", "number", number, "source", "item", "age", time.Since(mtime))
				recordRetrievalOutcome(repo, telemetry.RetrievalPRView, true)
				return printPRView(pr, prViewComments, prViewJSON)
			}
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

// ---------------------------------------------------------------------------
// Display
// ---------------------------------------------------------------------------

func printPRList(prs []*github.PullRequest, total int, jsonFields []string, noTruncate bool) error {
	if jsonFields != nil {
		return printPRListJSON(prs, jsonFields)
	}

	if len(prs) == 0 {
		fmt.Println("No pull requests found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	for _, pr := range prs {
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
		fmt.Fprintf(w, "#%d\t%s%s\t%s → %s\t%s\t%d\t%s\n",
			pr.Number,
			title,
			draft,
			pr.HeadRefName,
			pr.BaseRefName,
			review,
			count,
			pr.UpdatedAt.Format("2006-01-02"),
		)
	}
	w.Flush()

	if total > len(prs) {
		logging.Info("showing pull requests", "shown", len(prs), "total", total)
	}
	return nil
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
