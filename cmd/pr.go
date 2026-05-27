package cmd

import "github.com/spf13/cobra"

var prRepo string

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Manage pull requests",
}

func init() {
	prCmd.PersistentFlags().StringVarP(&prRepo, "repo", "R", "", "Repository using the OWNER/REPO format")
	rootCmd.AddCommand(prCmd)
}
