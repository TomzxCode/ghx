package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Work with cached repositories",
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List locally cached repositories",
	Long: `Lists every repository in the local cache with its issue and PR counts,
cache age, freshness, and a cache hit rate with the retrieval count per
operation.

Hit rates are read from the local telemetry database and are split by
retrieval operation: issue list, issue view, PR list, and PR view. A dash
means no retrieval of that kind has been recorded for the repository.`,
	RunE: runRepoList,
}

var repoDeleteCmd = &cobra.Command{
	Use:   "delete [REPO]",
	Short: "Delete all cached data for a repository",
	Long: `Deletes the locally cached issues, pull requests, and cache metadata for one
repository. The repository is given as [HOST/]OWNER/REPO, or resolved from
--repo or the current directory's git remote when omitted.

The data is local only; the next cache, list, or view command fetches it again
from GitHub. With the SQLite backend, only that repository's rows are removed
from the shared database.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRepoDelete,
}

func init() {
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoDeleteCmd)
}

// repoLabel formats repository coordinates the way the CLI displays them:
// OWNER/REPO on github.com, HOST/OWNER/REPO elsewhere.
func repoLabel(host, owner, repo string) string {
	if host == "" || host == "github.com" {
		return owner + "/" + repo
	}
	return host + "/" + owner + "/" + repo
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
		fmt.Fprintln(cmd.OutOrStdout(), "No cached repositories found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tISSUES\tPRS\tCACHED AGE\tSTATUS\tISSUE-LIST\tISSUE-VIEW\tPR-LIST\tPR-VIEW")
	rates := retrievalHitRates()
	for _, r := range repos {
		repoStr := repoLabel(r.Host, r.Owner, r.Repo)
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
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			repoStr, r.IssueCount, r.PRCount, age, status,
			hitRateLabel(rates, r.Host, r.Owner, r.Repo, telemetry.RetrievalIssueList),
			hitRateLabel(rates, r.Host, r.Owner, r.Repo, telemetry.RetrievalIssueView),
			hitRateLabel(rates, r.Host, r.Owner, r.Repo, telemetry.RetrievalPRList),
			hitRateLabel(rates, r.Host, r.Owner, r.Repo, telemetry.RetrievalPRView))
	}
	w.Flush()
	return nil
}

// resolveRepoArg resolves the repository targeted by a command that takes an
// optional [HOST/]OWNER/REPO argument. Without an argument it falls back to
// --repo or the current directory's git remote.
func resolveRepoArg(args []string) (*gitremote.Repo, error) {
	if len(args) == 0 {
		return getRepo()
	}
	if repoFlag != "" {
		return nil, fmt.Errorf("specify the repository as an argument or with --repo, not both")
	}
	return gitremote.ParseRepo(args[0])
}

func runRepoDelete(cmd *cobra.Command, args []string) error {
	repo, err := resolveRepoArg(args)
	if err != nil {
		return err
	}
	label := repoLabel(repo.Host, repo.Owner, repo.Name)

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()

	deleted, err := store.DeleteRepo(repo.Host, repo.Owner, repo.Name)
	if err != nil {
		return fmt.Errorf("deleting cache for %s: %w", label, err)
	}
	if !deleted {
		fmt.Fprintf(cmd.OutOrStdout(), "No cached data found for %s.\n", label)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted cached data for %s.\n", label)
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
