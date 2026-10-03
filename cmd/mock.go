package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

var (
	mockServeUsers          int
	mockServeRepos          []string
	mockServeHistory        time.Duration
	mockServeIssuesPerRepo  int
	mockServePRsPerRepo     int
	mockServeCommentsIssue  int
	mockServeCommentsPR     int
	mockServeAssigneesIssue int
	mockServeAssigneesPR    int
	mockServeLabels         int
	mockServeMilestones     int
	mockServeCloseRate      float64
	mockServeMergeRate      float64
	mockServeDraftRate      float64
	mockServeReviewRate     float64
	mockServeSeed           int64
	mockServeBursts         int
	mockServePreset         string
	mockServeStats          bool
	mockServeLatencyFrom    string
)

var mockCmd = &cobra.Command{
	Use:   "mock",
	Short: "Mock GitHub API server for testing",
}

var mockServeCmd = &cobra.Command{
	Use:          "serve",
	Short:        "Start a mock GitHub GraphQL server",
	SilenceUsage: true,
	RunE:         runMockServe,
}

func init() {
	mockServeCmd.Flags().IntVar(&mockServeUsers, "users", 0, "Number of simulated users (0 = use preset)")
	mockServeCmd.Flags().StringSliceVar(&mockServeRepos, "repos", nil, `Repositories in "owner/repo" format (comma-separated)`)
	mockServeCmd.Flags().DurationVar(&mockServeHistory, "history", 0, "Duration of simulated history (e.g. 720h = 30 days)")
	mockServeCmd.Flags().IntVar(&mockServeIssuesPerRepo, "issues-per-repo", 0, "Issues per repo (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServePRsPerRepo, "prs-per-repo", 0, "PRs per repo (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServeCommentsIssue, "comments-per-issue", 0, "Max comments per issue (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServeCommentsPR, "comments-per-pr", 0, "Max comments per PR (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServeAssigneesIssue, "assignees-per-issue", 0, "Max assignees per issue (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServeAssigneesPR, "assignees-per-pr", 0, "Max assignees per PR (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServeLabels, "labels-per-item", 0, "Max labels per item (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServeMilestones, "milestones-per-repo", 0, "Milestones per repo (0 = use preset)")
	mockServeCmd.Flags().Float64Var(&mockServeCloseRate, "close-rate", -1, "Fraction of issues closed [0,1] (-1 = use preset)")
	mockServeCmd.Flags().Float64Var(&mockServeMergeRate, "merge-rate", -1, "Fraction of PRs merged [0,1] (-1 = use preset)")
	mockServeCmd.Flags().Float64Var(&mockServeDraftRate, "draft-rate", -1, "Fraction of PRs as drafts [0,1] (-1 = use preset)")
	mockServeCmd.Flags().Float64Var(&mockServeReviewRate, "review-rate", -1, "Fraction of PRs reviewed [0,1] (-1 = use preset)")
	mockServeCmd.Flags().Int64Var(&mockServeSeed, "seed", 0, "RNG seed (0 = use preset)")
	mockServeCmd.Flags().IntVar(&mockServeBursts, "activity-bursts", -1, "Number of high-activity windows (-1 = use preset)")
	mockServeCmd.Flags().StringVar(&mockServePreset, "preset", "default", "Config preset: default, small, or none (all flags required)")
	mockServeCmd.Flags().BoolVar(&mockServeStats, "stats", false, "Print simulation stats and exit without starting server")
	mockServeCmd.Flags().StringVar(&mockServeLatencyFrom, "latency-from", "", "Inject per-kind response latency fitted from a telemetry database or exported JSON file")

	mockCmd.AddCommand(mockServeCmd)
	rootCmd.AddCommand(mockCmd)
}

func runMockServe(cmd *cobra.Command, args []string) error {
	cfg, err := buildSimConfig(cmd)
	if err != nil {
		return err
	}

	if mockServeStats {
		fmt.Print(mockserver.SimulationStats(cfg))
		return nil
	}

	fmt.Fprintln(os.Stderr, mockserver.SimulationStats(cfg))
	fmt.Fprintln(os.Stderr, "Generating scenario...")

	scenario := mockserver.Generate(cfg)

	srv := mockserver.NewServer(scenario)
	defer srv.Close()

	if mockServeLatencyFrom != "" {
		model, err := loadLatencyModel(mockServeLatencyFrom)
		if err != nil {
			return err
		}
		srv.WithLatency(model)
		fmt.Fprintf(os.Stderr, "Injecting latency for %d kind(s) from %s (%d sample(s)).\n\n",
			model.Kinds(), mockServeLatencyFrom, model.TotalSamples())
	}

	fmt.Fprintf(os.Stderr, "Mock server ready.\n\n")
	fmt.Fprintf(os.Stderr, "  URL: %s\n\n", srv.URL())
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  ghx --api-url %s --cache-dir /tmp/mock-cache --repo %s cache\n",
		srv.URL(), cfg.Repos[0])
	fmt.Fprintf(os.Stderr, "  ghx --api-url %s --cache-dir /tmp/mock-cache --repo %s issue list --state all\n",
		srv.URL(), cfg.Repos[0])
	fmt.Fprintf(os.Stderr, "\nListening (Ctrl+C to stop)...\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Fprintln(os.Stderr, "\nShutting down.")
	return nil
}

func buildSimConfig(c *cobra.Command) (mockserver.SimulationConfig, error) {
	var cfg mockserver.SimulationConfig

	switch strings.ToLower(mockServePreset) {
	case "default":
		cfg = mockserver.DefaultConfig()
	case "small":
		cfg = mockserver.SmallConfig()
	case "none":
		cfg = mockserver.SimulationConfig{}
	default:
		return cfg, fmt.Errorf("unknown preset %q (use: default, small, none)", mockServePreset)
	}

	if c.Flags().Changed("users") {
		cfg.NumUsers = mockServeUsers
	}
	if len(mockServeRepos) > 0 {
		cfg.Repos = mockServeRepos
	}
	if c.Flags().Changed("history") {
		cfg.History = mockServeHistory
	}
	if c.Flags().Changed("issues-per-repo") {
		cfg.IssuesPerRepo = mockServeIssuesPerRepo
	}
	if c.Flags().Changed("prs-per-repo") {
		cfg.PRsPerRepo = mockServePRsPerRepo
	}
	if c.Flags().Changed("comments-per-issue") {
		cfg.CommentsPerIssue = mockServeCommentsIssue
	}
	if c.Flags().Changed("comments-per-pr") {
		cfg.CommentsPerPR = mockServeCommentsPR
	}
	if c.Flags().Changed("assignees-per-issue") {
		cfg.AssigneesPerIssue = mockServeAssigneesIssue
	}
	if c.Flags().Changed("assignees-per-pr") {
		cfg.AssigneesPerPR = mockServeAssigneesPR
	}
	if c.Flags().Changed("labels-per-item") {
		cfg.LabelsPerItem = mockServeLabels
	}
	if c.Flags().Changed("milestones-per-repo") {
		cfg.MilestonesPerRepo = mockServeMilestones
	}
	if c.Flags().Changed("close-rate") {
		cfg.CloseRate = mockServeCloseRate
	}
	if c.Flags().Changed("merge-rate") {
		cfg.MergeRate = mockServeMergeRate
	}
	if c.Flags().Changed("draft-rate") {
		cfg.DraftRate = mockServeDraftRate
	}
	if c.Flags().Changed("review-rate") {
		cfg.ReviewRate = mockServeReviewRate
	}
	if c.Flags().Changed("seed") {
		cfg.Seed = mockServeSeed
	}
	if c.Flags().Changed("activity-bursts") {
		cfg.ActivityBursts = mockServeBursts
	}

	if len(cfg.Repos) == 0 {
		return cfg, fmt.Errorf("at least one --repos entry is required")
	}
	if cfg.NumUsers <= 0 {
		return cfg, fmt.Errorf("--users must be > 0")
	}

	return cfg, nil
}
