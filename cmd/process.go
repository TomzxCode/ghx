package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/logging"
	"github.com/tomzxcode/ghx/internal/processflow"
	"github.com/tomzxcode/ghx/internal/processreport"
)

var (
	processWindow int
	processOutput string
)

var processCmd = &cobra.Command{
	Use:   "process [REPO...]",
	Short: "Generate an HTML production-line report of issue and PR flow",
	Long: `Generates an HTML report that reads issues and pull requests as a
production line for one or more repositories.

Repositories are given as positional [HOST/]OWNER/REPO arguments (defaulting to
the current repository or --repo). The report shows each line's station
inventory with queue ages (open issues; draft, ready, merged, and discarded
PRs), a cumulative flow diagram, cycle-time distributions, weekly arrivals
versus completions, Little's law, a Monte Carlo forecast of when the current
items clear, and the aging list of everything still on the line.

Data is served from the local cache when available (run ` + "`ghx cache`" + ` first
for full history); it falls back to the GitHub API when nothing is cached.`,
	RunE: runProcess,
}

func init() {
	processCmd.Flags().IntVar(&processWindow, "days", processflow.DefaultWindowDays, "Reporting window in days (1-730)")
	processCmd.Flags().StringVarP(&processOutput, "output", "o", "process-report.html", "Output HTML file path")
	rootCmd.AddCommand(processCmd)
}

func runProcess(cmd *cobra.Command, args []string) error {
	if processWindow < 1 || processWindow > 730 {
		return fmt.Errorf("--days must be between 1 and 730, got %d", processWindow)
	}
	repos, err := resolveStatsRepos(args)
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()

	var items []processflow.Item
	labels := make([]string, 0, len(repos))
	issueCount, prCount := 0, 0
	for _, repo := range repos {
		issues, err := loadProcessIssues(store, repo)
		if err != nil {
			return err
		}
		prs, err := loadProcessPRs(store, repo)
		if err != nil {
			return err
		}
		items = append(items, processflow.FromIssues(issues)...)
		items = append(items, processflow.FromPRs(prs)...)
		issueCount += len(issues)
		prCount += len(prs)
		labels = append(labels, repo.Owner+"/"+repo.Name)
		logging.Info("loaded repository", "issues", len(issues), "pulls", len(prs), "repo", repo.Owner+"/"+repo.Name)
	}

	report := processflow.Build(items, time.Now().UTC(), processWindow)
	html, err := processreport.Render(&report, processreport.Meta{Repos: labels})
	if err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}
	if err := os.WriteFile(processOutput, []byte(html), 0644); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	fmt.Printf("Report written to %s (%d issue(s) and %d pull request(s) across %d repositories, %d item(s) on the line).\n",
		processOutput, issueCount, prCount, len(repos), len(report.WIP))
	return nil
}

// loadProcessIssues returns the issues used for the report, following the
// stats loader: a fresh cache is served as-is, a stale cache is served with a
// warning, and an empty cache falls back to the GitHub API.
func loadProcessIssues(store cache.Store, repo *gitremote.Repo) ([]*github.Issue, error) {
	if issues, err := store.LoadAllIssues(repo.Host, repo.Owner, repo.Name); err == nil && len(issues) > 0 {
		if fresh, _ := store.IsIssuesCacheFresh(repo.Host, repo.Owner, repo.Name); !fresh {
			logging.Warn("issues cache is stale",
				"repo", repo.Owner+"/"+repo.Name,
				"hint", fmt.Sprintf("run `ghx cache -R %s/%s` for complete data", repo.Owner, repo.Name))
		}
		return issues, nil
	}
	client, err := newClient(repo.Host)
	if err != nil {
		return nil, err
	}
	logging.Info("no cached issues, fetching from GitHub", "repo", repo.Owner+"/"+repo.Name)
	return client.FetchAllIssues(repo.Owner, repo.Name, nil, nil)
}

// loadProcessPRs is loadProcessIssues for pull requests.
func loadProcessPRs(store cache.Store, repo *gitremote.Repo) ([]*github.PullRequest, error) {
	if prs, err := store.LoadAllPRs(repo.Host, repo.Owner, repo.Name); err == nil && len(prs) > 0 {
		if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); !fresh {
			logging.Warn("PR cache is stale",
				"repo", repo.Owner+"/"+repo.Name,
				"hint", fmt.Sprintf("run `ghx cache -R %s/%s` for complete data", repo.Owner, repo.Name))
		}
		return prs, nil
	}
	client, err := newClient(repo.Host)
	if err != nil {
		return nil, err
	}
	logging.Info("no cached pull requests, fetching from GitHub", "repo", repo.Owner+"/"+repo.Name)
	return client.FetchAllPRs(repo.Owner, repo.Name, nil)
}
