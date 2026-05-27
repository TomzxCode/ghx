package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var (
	editBody     string
	editBodyFile string
)

var prCommentEditCmd = &cobra.Command{
	Use:   "edit <comment-id>",
	Short: "Edit an existing comment",
	Long: `Edit an existing PR comment.

Automatically detects whether the comment is an inline review comment
(PullRequestReviewComment) or a top-level issue comment (IssueComment).`,
	Args: cobra.ExactArgs(1),
	RunE: runPRCommentEdit,
}

func init() {
	prCommentEditCmd.Flags().StringVarP(&editBody, "body", "b", "", "New comment body text")
	prCommentEditCmd.Flags().StringVarP(&editBodyFile, "body-file", "F", "", "Read body text from file (use \"-\" for stdin)")
	prCommentEditCmd.MarkFlagsMutuallyExclusive("body", "body-file")

	prCommentCmd.AddCommand(prCommentEditCmd)
}

func runPRCommentEdit(cmd *cobra.Command, args []string) error {
	commentID := args[0]

	body, err := resolveBodyFlags(editBody, editBodyFile)
	if err != nil {
		return err
	}

	nodeType, err := gh.GetNodeType(commentID)
	if err != nil {
		return err
	}

	switch nodeType {
	case "PullRequestReviewComment":
		err = gh.EditReviewComment(commentID, body)
	case "IssueComment":
		err = gh.EditIssueComment(commentID, body)
	default:
		return fmt.Errorf("unsupported comment type: %s", nodeType)
	}

	if err != nil {
		return err
	}

	fmt.Printf("Updated comment %s\n", commentID)
	return nil
}
