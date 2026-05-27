package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var (
	issueEditBody     string
	issueEditBodyFile string
)

var issueCommentEditCmd = &cobra.Command{
	Use:   "edit <comment-id>",
	Short: "Edit an existing issue comment",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueCommentEdit,
}

func init() {
	issueCommentEditCmd.Flags().StringVarP(&issueEditBody, "body", "b", "", "New comment body text")
	issueCommentEditCmd.Flags().StringVarP(&issueEditBodyFile, "body-file", "F", "", "Read body text from file (use \"-\" for stdin)")
	issueCommentEditCmd.MarkFlagsMutuallyExclusive("body", "body-file")
}

func runIssueCommentEdit(cmd *cobra.Command, args []string) error {
	commentID := args[0]

	body, err := resolveBodyFlags(issueEditBody, issueEditBodyFile)
	if err != nil {
		return err
	}

	if err := gh.EditIssueComment(commentID, body); err != nil {
		return err
	}

	fmt.Printf("Updated comment %s\n", commentID)
	return nil
}
