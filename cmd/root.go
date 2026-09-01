package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/version"
)

var (
	repoFlag   string
	apiURLFlag string
	cacheDir   string
)

var rootCmd = &cobra.Command{
	Use:           "ghx",
	Short:         "Extended GitHub CLI with local caching",
	Version:       version.Get(),
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `ghx is an extended GitHub CLI. It caches issues, pull requests, and their
comments locally to minimise API calls (cache at ~/.cache/ghx/cache/<host>/<owner>/<repo>),
and provides PR/issue comment operations beyond the standard gh CLI: inline review
comments, line-range comments, thread replies, pending reviews, and local stashes.`,
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&repoFlag, "repo", "R", "", "Repository in [HOST/]OWNER/REPO format")
	rootCmd.PersistentFlags().StringVar(&apiURLFlag, "api-url", "", "Override the GitHub GraphQL API endpoint URL (for testing)")
	rootCmd.PersistentFlags().StringVar(&cacheDir, "cache-dir", "", "Override the cache directory path")

	rootCmd.AddCommand(issueCmd)
	rootCmd.AddCommand(prCmd)
	rootCmd.AddCommand(cacheCmd)
	rootCmd.AddCommand(repoCmd)
	rootCmd.AddCommand(statsCmd)
}

// getRepo resolves the target repository from the --repo flag or the current
// directory's git remote.
func getRepo() (*gitremote.Repo, error) {
	if repoFlag != "" {
		return gitremote.ParseRepo(repoFlag)
	}
	return gitremote.DetectRepo()
}

// resolveOwnerName resolves the target repository and returns its owner and
// name, for use by the gh comment/review operations (which target github.com).
func resolveOwnerName() (owner, name string, err error) {
	repo, err := getRepo()
	if err != nil {
		return "", "", err
	}
	return repo.Owner, repo.Name, nil
}

// newClient creates a GitHub client, using --api-url if provided.
func newClient(host string) (*github.Client, error) {
	if apiURLFlag != "" {
		return github.NewClientWithURL(apiURLFlag, "test-token", host)
	}
	return github.NewClient(host)
}

// newStore creates a cache store, using --cache-dir if provided.
func newStore() *cache.Store {
	if cacheDir != "" {
		return cache.NewStoreWithPath(cacheDir)
	}
	return cache.NewStore()
}
