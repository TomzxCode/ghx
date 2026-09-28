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

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Work with GitHub issues",
}

// issue list flags
var (
	issueListApp        string
	issueListAssignee   string
	issueListAuthor     string
	issueListLabels     []string
	issueListLimit      int
	issueListMention    string
	issueListMilestone  string
	issueListSearch     string
	issueListState      string
	issueListJSON       bool
	issueListNoTruncate bool
)

// issue view flags
var (
	issueViewComments bool
	issueViewJSON     bool
	issueViewRefresh  bool
	issueViewIDs      bool
)

var issueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues",
	RunE:  runIssueList,
}

var issueViewCmd = &cobra.Command{
	Use:   "view <number>",
	Short: "View an issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueView,
}

func init() {
	issueCmd.AddCommand(issueListCmd)
	issueCmd.AddCommand(issueViewCmd)

	issueListCmd.Flags().StringVar(&issueListApp, "app", "", "Filter by GitHub App author")
	issueListCmd.Flags().StringVarP(&issueListAssignee, "assignee", "a", "", "Filter by assignee")
	issueListCmd.Flags().StringVarP(&issueListAuthor, "author", "A", "", "Filter by author")
	issueListCmd.Flags().StringSliceVarP(&issueListLabels, "label", "l", nil, "Filter by label")
	issueListCmd.Flags().IntVarP(&issueListLimit, "limit", "L", 1000, "Maximum number of issues to fetch")
	issueListCmd.Flags().StringVar(&issueListMention, "mention", "", "Filter by mention")
	issueListCmd.Flags().StringVarP(&issueListMilestone, "milestone", "m", "", "Filter by milestone number or title")
	issueListCmd.Flags().StringVarP(&issueListSearch, "search", "S", "", "Search issues with query")
	issueListCmd.Flags().StringVarP(&issueListState, "state", "s", "open", "Filter by state: {open|closed|all}")
	issueListCmd.Flags().BoolVar(&issueListJSON, "json", false, "Output as JSON")
	issueListCmd.Flags().BoolVar(&issueListNoTruncate, "no-truncate", false, "Don't truncate long titles")

	issueViewCmd.Flags().BoolVarP(&issueViewComments, "comments", "c", false, "View issue comments")
	issueViewCmd.Flags().BoolVar(&issueViewJSON, "json", false, "Output as JSON")
	issueViewCmd.Flags().BoolVar(&issueViewRefresh, "refresh", false, "Force fetch from GitHub and update cache")
	issueViewCmd.Flags().BoolVar(&issueViewIDs, "ids", false, "Show comment IDs (useful for editing/deleting)")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func runIssueList(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	store := newStore()

	// Serve from cache when it is fresh.
	if fresh, _ := store.IsIssuesCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
		if issues, err := store.LoadAllIssues(repo.Host, repo.Owner, repo.Name); err == nil {
			filtered := filterIssues(issues, issueListState, issueListAssignee, issueListAuthor,
				issueListLabels, issueListMilestone, issueListMention, issueListApp, issueListSearch)
			sort.Slice(filtered, func(i, j int) bool {
				return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
			})
			total := len(filtered)
			if issueListLimit > 0 && len(filtered) > issueListLimit {
				filtered = filtered[:issueListLimit]
			}
			return printIssueList(filtered, total, issueListJSON, issueListNoTruncate)
		}
	}

	// Fall back to the GitHub API.
	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}

	opts := github.IssueListOptions{
		Limit:     issueListLimit,
		State:     issueListState,
		Assignee:  issueListAssignee,
		Author:    issueListAuthor,
		Labels:    issueListLabels,
		Milestone: issueListMilestone,
		Mention:   issueListMention,
		Search:    issueListSearch,
		App:       issueListApp,
	}

	issues, err := client.ListIssues(repo.Owner, repo.Name, opts)
	if err != nil {
		return err
	}

	return printIssueList(issues, len(issues), issueListJSON, issueListNoTruncate)
}

func runIssueView(cmd *cobra.Command, args []string) error {
	number, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	repo, err := getRepo()
	if err != nil {
		return err
	}

	store := newStore()

	if !issueViewRefresh {
		// When a full cache is fresh, treat it as authoritative: don't hit the API
		// if the item isn't there — it simply doesn't exist (or wasn't cached).
		if fresh, _ := store.IsIssuesCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
			issue, _, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, number)
			if err != nil {
				return fmt.Errorf("issue #%d not found in cache; run `ghx cache --force` to refresh", number)
			}
			return printIssueView(issue, issueViewComments, issueViewJSON, issueViewIDs)
		}

		// No fresh full cache — try the individual file, then fall back to the API.
		if issue, mtime, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, number); err == nil {
			if time.Since(mtime) < 60*time.Minute {
				return printIssueView(issue, issueViewComments, issueViewJSON, issueViewIDs)
			}
		}
	}

	client, err := newClient(repo.Host)
	if err != nil {
		return err
	}

	issue, err := client.GetIssue(repo.Owner, repo.Name, number)
	if err != nil {
		return err
	}

	_ = store.SaveIssue(repo.Host, repo.Owner, repo.Name, issue)
	return printIssueView(issue, issueViewComments, issueViewJSON, issueViewIDs)
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

func filterIssues(issues []*github.Issue, state, assignee, author string,
	labels []string, milestone, mention, app, search string) []*github.Issue {

	var result []*github.Issue
	for _, issue := range issues {
		if state != "all" && state != "" {
			if !strings.EqualFold(issue.State, state) {
				continue
			}
		}
		if assignee != "" {
			found := false
			for _, a := range issue.Assignees {
				if strings.EqualFold(a.Login, assignee) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if author != "" && !strings.EqualFold(issue.Author.Login, author) {
			continue
		}
		if len(labels) > 0 {
			if !hasAllLabels(issue.Labels, labels) {
				continue
			}
		}
		if milestone != "" {
			if issue.Milestone == nil {
				continue
			}
			if !strings.EqualFold(issue.Milestone.Title, milestone) &&
				strconv.Itoa(issue.Milestone.Number) != milestone {
				continue
			}
		}
		// mention and app cannot be verified from cached data; skip filtering on them.
		if search != "" {
			q := strings.ToLower(search)
			if !strings.Contains(strings.ToLower(issue.Title), q) &&
				!strings.Contains(strings.ToLower(issue.Body), q) {
				continue
			}
		}
		result = append(result, issue)
	}
	return result
}

func hasAllLabels(issueLabels []github.Label, wantLabels []string) bool {
	for _, want := range wantLabels {
		found := false
		for _, l := range issueLabels {
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

func printIssueList(issues []*github.Issue, total int, asJSON bool, noTruncate bool) error {
	if asJSON {
		if issues == nil {
			issues = []*github.Issue{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(issues)
	}

	if len(issues) == 0 {
		fmt.Println("No issues found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	for _, issue := range issues {
		count := issue.CommentCount
		if count == 0 {
			count = len(issue.Comments)
		}
		title := issue.Title
		if !noTruncate {
			title = truncate(title, 60)
		}
		fmt.Fprintf(w, "#%d\t%s\t%s\t%d\t%s\n",
			issue.Number,
			title,
			strings.Join(labelNames(issue.Labels), ", "),
			count,
			issue.UpdatedAt.Format("2006-01-02"),
		)
	}
	w.Flush()

	if total > len(issues) {
		fmt.Fprintf(os.Stderr, "Showing %d of %d issues\n", len(issues), total)
	}
	return nil
}

func printIssueView(issue *github.Issue, showComments bool, asJSON bool, showIDs bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(issue)
	}

	fmt.Printf("#%d %s\n", issue.Number, issue.Title)

	commentCount := issue.CommentCount
	if commentCount == 0 {
		commentCount = len(issue.Comments)
	}
	fmt.Printf("%s • opened by %s • %d comment(s)\n",
		strings.ToUpper(issue.State), issue.Author.Login, commentCount)
	fmt.Println()

	if len(issue.Labels) > 0 {
		fmt.Printf("Labels:    %s\n", strings.Join(labelNames(issue.Labels), ", "))
	}
	if len(issue.Assignees) > 0 {
		logins := make([]string, len(issue.Assignees))
		for i, a := range issue.Assignees {
			logins[i] = a.Login
		}
		fmt.Printf("Assignees: %s\n", strings.Join(logins, ", "))
	}
	if issue.Milestone != nil {
		fmt.Printf("Milestone: %s\n", issue.Milestone.Title)
	}
	fmt.Printf("Created:   %s\n", issue.CreatedAt.Format("2006-01-02 15:04"))
	if issue.ClosedAt != nil {
		fmt.Printf("Closed:    %s\n", issue.ClosedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("URL:       %s\n", issue.URL)
	fmt.Println()
	fmt.Println(issue.Body)

	if showComments && len(issue.Comments) > 0 {
		for i, c := range issue.Comments {
			if showIDs && c.ID != "" {
				fmt.Printf("\n── Comment %d by %s (%s) [%s] ──\n\n",
					i+1, c.Author.Login, c.CreatedAt.Format("2006-01-02 15:04"), c.ID)
			} else {
				fmt.Printf("\n── Comment %d by %s (%s) ──\n\n",
					i+1, c.Author.Login, c.CreatedAt.Format("2006-01-02 15:04"))
			}
			fmt.Println(c.Body)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func labelNames(labels []github.Label) []string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
