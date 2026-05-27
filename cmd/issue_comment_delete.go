package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var issueCommentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Delete an issue comment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		commentID := args[0]

		if err := gh.DeleteIssueComment(commentID); err != nil {
			return err
		}

		fmt.Printf("Deleted comment %s\n", commentID)
		return nil
	},
}
