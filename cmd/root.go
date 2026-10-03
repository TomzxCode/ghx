package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/version"
)

var (
	repoFlag           string
	apiURLFlag         string
	cacheDir           string
	storageFlag        string
	telemetryFlag      *bool
	telemetryFlagValue bool
	telemetryDBFlag    string
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
	// PersistentPreRunE runs before every command's RunE, so the --telemetry
	// flag's explicit-or-unset state is available to initTelemetry.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("telemetry") {
			v := telemetryFlagValue
			telemetryFlag = &v
		}
		return nil
	},
}

// Execute is the entry point called from main.
func Execute() {
	defer flushTelemetry()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&repoFlag, "repo", "R", "", "Repository in [HOST/]OWNER/REPO format")
	rootCmd.PersistentFlags().StringVar(&apiURLFlag, "api-url", "", "Override the GitHub GraphQL API endpoint URL (for testing)")
	rootCmd.PersistentFlags().StringVar(&cacheDir, "cache-dir", "", "Override the cache directory path")
	rootCmd.PersistentFlags().StringVar(&storageFlag, "storage", "", "Cache storage backend: sqlite (default) or file (env GHX_STORAGE)")
	rootCmd.PersistentFlags().BoolVar(&telemetryFlagValue, "telemetry", false, "Record API and cache timings locally (default enabled; env GHX_TELEMETRY, use --telemetry=false to opt out)")
	rootCmd.PersistentFlags().StringVar(&telemetryDBFlag, "telemetry-db", "", "Telemetry database path (env GHX_TELEMETRY_DB)")

	rootCmd.AddCommand(issueCmd)
	rootCmd.AddCommand(prCmd)
	rootCmd.AddCommand(cacheCmd)
	rootCmd.AddCommand(repoCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(telemetryCmd)
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

// newClient creates a GitHub client, using --api-url if provided. When
// telemetry is enabled, the client is instrumented so every GraphQL call is
// recorded.
func newClient(host string) (*github.Client, error) {
	var (
		client *github.Client
		err    error
	)
	if apiURLFlag != "" {
		client, err = github.NewClientWithURL(apiURLFlag, "test-token", host)
	} else {
		client, err = github.NewClient(host)
	}
	if err != nil {
		return nil, err
	}
	client.SetTelemetry(initTelemetry())
	return client, nil
}

// newStore creates a cache store. The backend is selected from --storage or the
// GHX_STORAGE environment variable (default: sqlite). --cache-dir overrides the
// cache root for both backends. When telemetry is enabled, the store is wrapped
// so its operations are recorded.
func newStore() (cache.Store, error) {
	backend := strings.ToLower(storageFlag)
	if backend == "" {
		backend = strings.ToLower(os.Getenv("GHX_STORAGE"))
	}
	var (
		store cache.Store
		err   error
	)
	rec := initTelemetry()
	switch backend {
	case "file":
		if cacheDir != "" {
			store = cache.NewStoreWithPath(cacheDir)
		} else {
			store = cache.NewStore()
		}
	case "", "sqlite":
		base := cacheDir
		if base == "" {
			base = cache.DefaultDir()
		}
		store, err = cache.NewSQLiteStore(base)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid --storage %q: use sqlite or file", backend)
	}
	if telemetryEnabled() {
		// The decorator labels events with the repository when known; the
		// operations themselves pass their own coordinates.
		store = cache.Instrument(store, rec, "", "", "")
	}
	return store, nil
}
