package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var (
	issueCommentBody     string
	issueCommentBodyFile string
)

var issueCommentCmd = &cobra.Command{
	Use:   "comment <number>",
	Short: "Comment on an issue",
	Long: `Add a comment to an issue.

Supports --body for inline text or --body-file to read from a file or stdin.`,
	Args: cobra.ExactArgs(1),
	RunE: runIssueComment,
}

func init() {
	issueCommentCmd.Flags().StringVarP(&issueCommentBody, "body", "b", "", "Comment body text")
	issueCommentCmd.Flags().StringVarP(&issueCommentBodyFile, "body-file", "F", "", "Read body text from file (use \"-\" for stdin)")
	issueCommentCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	issueCommentCmd.AddCommand(issueCommentEditCmd)
	issueCommentCmd.AddCommand(issueCommentDeleteCmd)

	issueCmd.AddCommand(issueCommentCmd)
}

func runIssueComment(cmd *cobra.Command, args []string) error {
	issueNumber, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	body, err := resolveBodyFlags(issueCommentBody, issueCommentBodyFile)
	if err != nil {
		return err
	}

	owner, name, err := gh.ResolveRepo(issueRepo)
	if err != nil {
		return err
	}

	issueID, err := gh.GetIssueNodeID(owner, name, issueNumber)
	if err != nil {
		return err
	}

	commentID, err := gh.AddTopLevelComment(issueID, body)
	if err != nil {
		return err
	}

	fmt.Printf("Created comment %s on issue #%d\n", commentID, issueNumber)
	return nil
}
