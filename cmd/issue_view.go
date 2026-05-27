package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var issueViewShowIDs bool

var issueViewCmd = &cobra.Command{
	Use:   "view <number>",
	Short: "View an issue and its comments",
	Long: `View an issue's title, description, and comments.

Use --ids to show comment IDs (useful for editing/deleting).`,
	Args: cobra.ExactArgs(1),
	RunE: runIssueView,
}

func init() {
	issueViewCmd.Flags().BoolVar(&issueViewShowIDs, "ids", false, "Show comment IDs")

	issueCmd.AddCommand(issueViewCmd)
}

func runIssueView(cmd *cobra.Command, args []string) error {
	issueNumber, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	owner, name, err := gh.ResolveRepo(issueRepo)
	if err != nil {
		return err
	}

	issue, comments, err := gh.GetIssue(owner, name, issueNumber)
	if err != nil {
		return err
	}

	fmt.Printf("%s  [%s]  %s\n\n", issue.Title, strings.ToLower(issue.State), issue.Author)
	if issue.Body != "" {
		fmt.Println(issue.Body)
		fmt.Println()
	}

	if len(comments) == 0 {
		fmt.Println("No comments found.")
		return nil
	}

	fmt.Printf("%d comment(s):\n\n", len(comments))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, c := range comments {
		for i, line := range strings.Split(c.Body, "\n") {
			if i == 0 {
				if issueViewShowIDs {
					fmt.Fprintf(w, "%s\t%s\t%s\n", c.ID, c.Author, line)
				} else {
					fmt.Fprintf(w, "%s\t%s\n", c.Author, line)
				}
			} else {
				fmt.Fprintf(w, "\t\t%s\n", line)
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	return nil
}
