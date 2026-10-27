package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gitremote"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Work with cached repositories",
}

// repo list flags
var repoListJSON string

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List locally cached repositories",
	Long: `Lists every repository in the local cache with its issue and PR counts,
cache age, freshness, and a cache hit rate with the retrieval count per
operation.

Pass --json to emit the same data as JSON, optionally limited to a
comma-separated list of fields.

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

	repoListCmd.Flags().StringVar(&repoListJSON, "json", "", "Output JSON with the specified fields (comma-separated; omit for all)")
	repoListCmd.Flags().Lookup("json").NoOptDefVal = repoJSONAll
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
	jsonFields, err := resolveRepoListJSONFields(args)
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()
	repos, err := store.ListCachedRepos()
	if err != nil {
		return err
	}

	if len(jsonFields) > 0 {
		return printRepoListJSON(cmd, repos, jsonFields)
	}

	if len(repos) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No cached repositories found.")
		return nil
	}

	now := time.Now()
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 8, 2, ' ', 0)
	header := []string{"REPO", "ISSUES", "PRS", "CACHED AGE", "STATUS"}
	for _, op := range repoHitRateOperations {
		header = append(header, op.column)
	}
	fmt.Fprintln(w, strings.Join(header, "\t"))
	rates := retrievalHitRates()
	for _, r := range repos {
		age := "—"
		if r.Info != nil {
			age = formatDuration(now.Sub(r.Info.CachedAt))
		}
		cells := []string{
			repoLabel(r.Host, r.Owner, r.Repo),
			strconv.Itoa(r.IssueCount),
			strconv.Itoa(r.PRCount),
			age,
			repoStatus(r.Info, now),
		}
		for _, op := range repoHitRateOperations {
			cells = append(cells, hitRateLabel(rates, r.Host, r.Owner, r.Repo, op.operation))
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	w.Flush()
	return nil
}

// repoStatus classifies a repository's cache metadata: "fresh" while it is
// within its configured duration, "stale" past it, and "no cache info" when
// the repository has no cache metadata.
func repoStatus(info *cache.CacheInfo, now time.Time) string {
	if info == nil {
		return "no cache info"
	}
	if now.Sub(info.CachedAt) < time.Duration(info.Duration)*time.Minute {
		return "fresh"
	}
	return "stale"
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
