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
	"github.com/tomzxcode/ghx/internal/github"
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
	prListJSON       bool
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
	prListCmd.Flags().BoolVar(&prListJSON, "json", false, "Output as JSON")
	prListCmd.Flags().BoolVar(&prListNoTruncate, "no-truncate", false, "Don't truncate long titles")

	prViewCmd.Flags().BoolVarP(&prViewComments, "comments", "c", false, "View pull request comments")
	prViewCmd.Flags().BoolVar(&prViewJSON, "json", false, "Output as JSON")
	prViewCmd.Flags().BoolVar(&prViewRefresh, "refresh", false, "Force fetch from GitHub and update cache")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func runPRList(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	store := newStore()

	// Serve from cache when it is fresh.
	if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
		if prs, err := store.LoadAllPRs(repo.Host, repo.Owner, repo.Name); err == nil {
			filtered := filterPRs(prs, prListState, prListAssignee, prListAuthor,
				prListLabels, prListBase, prListHead, prListApp, prListSearch, prListDraft)
			sort.Slice(filtered, func(i, j int) bool {
				return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
			})
			total := len(filtered)
			if prListLimit > 0 && len(filtered) > prListLimit {
				filtered = filtered[:prListLimit]
			}
			return printPRList(filtered, total, prListJSON, prListNoTruncate)
		}
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

	prs, err := client.ListPRs(repo.Owner, repo.Name, opts)
	if err != nil {
		return err
	}

	return printPRList(prs, len(prs), prListJSON, prListNoTruncate)
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

	store := newStore()

	if !prViewRefresh {
		// When a full cache is fresh, treat it as authoritative.
		if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
			pr, _, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, number)
			if err != nil {
				return fmt.Errorf("pull request #%d not found in cache; run `ghx cache --force` to refresh", number)
			}
			return printPRView(pr, prViewComments, prViewJSON)
		}

		// No fresh full cache — try the individual file, then fall back to the API.
		if pr, mtime, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, number); err == nil {
			if time.Since(mtime) < 60*time.Minute {
				return printPRView(pr, prViewComments, prViewJSON)
			}
		}
	}

	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}

	pr, err := client.GetPR(repo.Owner, repo.Name, number)
	if err != nil {
		return err
	}

	_ = store.SavePR(repo.Host, repo.Owner, repo.Name, pr)
	return printPRView(pr, prViewComments, prViewJSON)
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

func filterPRs(prs []*github.PullRequest, state, assignee, author string,
	labels []string, base, head, app, search string, draft bool) []*github.PullRequest {

	var result []*github.PullRequest
	for _, pr := range prs {
		if state != "all" && state != "" {
			if !strings.EqualFold(pr.State, state) {
				continue
			}
		}
		if assignee != "" {
			found := false
			for _, a := range pr.Assignees {
				if strings.EqualFold(a.Login, assignee) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if author != "" && !strings.EqualFold(pr.Author.Login, author) {
			continue
		}
		if len(labels) > 0 {
			if !hasAllLabelsPR(pr.Labels, labels) {
				continue
			}
		}
		if base != "" && !strings.EqualFold(pr.BaseRefName, base) {
			continue
		}
		if head != "" && !strings.EqualFold(pr.HeadRefName, head) {
			continue
		}
		if draft && !pr.IsDraft {
			continue
		}
		// app cannot be verified from cached data; skip.
		if search != "" {
			q := strings.ToLower(search)
			if !strings.Contains(strings.ToLower(pr.Title), q) &&
				!strings.Contains(strings.ToLower(pr.Body), q) {
				continue
			}
		}
		result = append(result, pr)
	}
	return result
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

func hasAllLabelsPR(prLabels []github.Label, wantLabels []string) bool {
	for _, want := range wantLabels {
		found := false
		for _, l := range prLabels {
			if strings.EqualFold(l.Name, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Display
// ---------------------------------------------------------------------------

func printPRList(prs []*github.PullRequest, total int, asJSON bool, noTruncate bool) error {
	if asJSON {
		if prs == nil {
			prs = []*github.PullRequest{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(prs)
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
		fmt.Fprintf(os.Stderr, "Showing %d of %d pull requests\n", len(prs), total)
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
