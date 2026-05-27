package cmd

import "github.com/spf13/cobra"

var issueRepo string

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Manage issues",
}

func init() {
	issueCmd.PersistentFlags().StringVarP(&issueRepo, "repo", "R", "", "Repository using the OWNER/REPO format")
	rootCmd.AddCommand(issueCmd)
}
