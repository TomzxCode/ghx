package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Work with cached repositories",
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List locally cached repositories",
	RunE:  runRepoList,
}

func init() {
	repoCmd.AddCommand(repoListCmd)
}

func runRepoList(cmd *cobra.Command, args []string) error {
	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()
	repos, err := store.ListCachedRepos()
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		fmt.Println("No cached repositories found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tISSUES\tPRS\tCACHED AGE\tSTATUS")
	for _, r := range repos {
		repoStr := r.Host + "/" + r.Owner + "/" + r.Repo
		if r.Host == "github.com" {
			repoStr = r.Owner + "/" + r.Repo
		}
		age := "—"
		status := "no cache info"
		if r.Info != nil {
			d := time.Since(r.Info.CachedAt)
			age = formatDuration(d)
			if d < time.Duration(r.Info.Duration)*time.Minute {
				status = "fresh"
			} else {
				status = "stale"
			}
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n",
			repoStr, r.IssueCount, r.PRCount, age, status)
	}
	w.Flush()
	return nil
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
