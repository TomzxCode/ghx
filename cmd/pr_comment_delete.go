package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var prCommentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Delete a comment",
	Long: `Delete an existing PR comment.

Automatically detects whether the comment is an inline review comment
(PullRequestReviewComment) or a top-level issue comment (IssueComment).

Use 'ghx pr threads <number> --comments --ids' to find comment IDs.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		commentID := args[0]

		nodeType, err := gh.GetNodeType(commentID)
		if err != nil {
			return err
		}

		switch nodeType {
		case "PullRequestReviewComment":
			err = gh.DeleteReviewComment(commentID)
		case "IssueComment":
			err = gh.DeleteIssueComment(commentID)
		default:
			return fmt.Errorf("unsupported comment type: %s", nodeType)
		}

		if err != nil {
			return err
		}

		fmt.Printf("Deleted comment %s\n", commentID)
		return nil
	},
}

func init() {
	prCommentCmd.AddCommand(prCommentDeleteCmd)
}
