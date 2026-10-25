package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/logging"
	"github.com/tomzxcode/ghx/internal/rpc"
	"github.com/tomzxcode/ghx/internal/webui"
)

var (
	serveHost         string
	servePort         int
	serveOpen         bool
	serveRPCToken     string
	serveRPCAllowExec bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve a local web UI over the cached issues and PRs",
	Long: `Starts a local web server that browses the ghx cache: issue and PR lists and
details for every cached repository, markdown rendering, a Ctrl+P search palette,
a refresh action that reuses the cache command's fetch logic, and a stats report
of PR activity at /stats (the same report ghx stats writes to a file).

The UI is read-only against the cache except for the explicit refresh action;
the stats report is computed from cached pull requests only.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "127.0.0.1", "Interface to bind the server to")
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port to bind the server to")
	serveCmd.Flags().BoolVar(&serveOpen, "open", false, "Open the browser after starting the server")
	serveCmd.Flags().StringVar(&serveRPCToken, "rpc-token", os.Getenv("GHX_RPC_TOKEN"), "Require this bearer token from JSON-RPC clients (env GHX_RPC_TOKEN)")
	serveCmd.Flags().BoolVar(&serveRPCAllowExec, "rpc-allow-exec", true, "Expose cli.run over JSON-RPC, which executes ghx commands for clients")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	addr := net.JoinHostPort(serveHost, strconv.Itoa(servePort))
	if serveRPCToken == "" && !isLoopbackAddr(addr) {
		return fmt.Errorf("refusing to expose the JSON-RPC API on %s without --rpc-token: clients could run ghx commands and change the cache", addr)
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()
	server := webui.New(store, serveRefreshFunc(store))
	server.SetStatsFunc(serveStatsFunc(store))
	server.Mount("/rpc", rpc.NewServer(newRPCConfig(store, serveRPCToken, serveRPCAllowExec)))

	url := "http://" + addr
	fmt.Printf("Serving ghx cache at %s\n", url)
	fmt.Printf("Serving ghx JSON-RPC at %s/rpc\n", url)
	if serveOpen {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(os.Stderr, "Could not open browser: %s\n", err)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(addr)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-stop:
		fmt.Println("\nShutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

// serveRefreshFunc adapts the shared cache fetch logic into a webui.RefreshFunc.
// Each call runs a forced (but resumable) full fetch for the given repository.
func serveRefreshFunc(store cache.Store) webui.RefreshFunc {
	return func(host, owner, name string) (int, int, error) {
		client, err := newClient(host)
		if err != nil {
			return 0, 0, err
		}
		// ParseRepo normalizes the coordinates, so a request that differs only
		// by casing targets the same cache entry as the CLI.
		repo, err := gitremote.ParseRepo(host + "/" + owner + "/" + name)
		if err != nil {
			return 0, 0, err
		}
		info, _ := store.LoadCacheInfo(host, owner, name)
		if info == nil {
			info = &cache.CacheInfo{}
		}
		return fetchRepoData(store, client, repo, info, FetchOptions{
			Force:       true,
			FetchIssues: true,
			FetchPRs:    true,
			Duration:    60,
		})
	}
}

// openBrowser opens the default browser at url (best effort).
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// requireCachedRepos verifies every requested repository has something in the
// cache. Only-viewed repositories (items without cache metadata) count, like
// the webui hasRepo check.
func requireCachedRepos(store cache.Store, coords [][3]string) error {
	cached, err := store.ListCachedRepos()
	if err != nil {
		return err
	}
	for _, c := range coords {
		found := false
		for _, cr := range cached {
			if strings.EqualFold(cr.Host, c[0]) && strings.EqualFold(cr.Owner, c[1]) && strings.EqualFold(cr.Repo, c[2]) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("repository %s/%s/%s is not cached", c[0], c[1], c[2])
		}
	}
	return nil
}

// serveStatsFunc adapts the cached pull requests into the stats report HTML.
// Unlike the stats command, it never falls back to the GitHub API: the UI is
// read-only against the cache, so run `ghx cache` (or use the refresh action)
// first for complete data.
func serveStatsFunc(store cache.Store) webui.StatsFunc {
	return func(q webui.StatsQuery) (string, error) {
		from, err := parseStatsTime(q.From, false)
		if err != nil {
			return "", err
		}
		to, err := parseStatsTime(q.To, true)
		if err != nil {
			return "", err
		}
		if !from.IsZero() && !to.IsZero() && to.Before(from) {
			return "", fmt.Errorf("to (%s) is before from (%s)", q.To, q.From)
		}
		repos, repoPRs, err := loadStatsRepoPRs(store, q.Repos)
		if err != nil {
			return "", err
		}
		if len(repos) == 0 {
			return "", fmt.Errorf("no cached repositories: run `ghx cache` first")
		}
		report := buildReport(repos, repoPRs, statsFilters{
			Authors:     toLowerSet(q.Authors),
			Reviewers:   toLowerSet(q.Reviewers),
			From:        from,
			To:          to,
			State:       q.State,
			IncludeBots: q.IncludeBots,
			ListPRs:     q.ListPRs,
			Top:         q.Top,
		})
		return renderReport(report)
	}
}

// loadStatsRepoPRs resolves the requested repositories (every cached one when
// names is empty) and loads their cached pull requests. Explicit names must be
// cached, matching the other repository-scoped endpoints.
func loadStatsRepoPRs(store cache.Store, names []string) ([]*gitremote.Repo, []*repoPR, error) {
	coords := make([][3]string, 0, len(names))
	if len(names) == 0 {
		cached, err := store.ListCachedRepos()
		if err != nil {
			return nil, nil, err
		}
		for _, cr := range cached {
			coords = append(coords, [3]string{cr.Host, cr.Owner, cr.Repo})
		}
	} else {
		// ParseRepo normalizes the coordinates, so a request that differs only
		// by casing targets the same cache entry as the CLI.
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			parts := strings.Split(name, "/")
			if len(parts) != 3 {
				return nil, nil, fmt.Errorf("invalid repository %q: expected host/owner/repo", name)
			}
			repo, err := gitremote.ParseRepo(name)
			if err != nil {
				return nil, nil, err
			}
			key := repo.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			coords = append(coords, [3]string{repo.Host, repo.Owner, repo.Name})
		}
		if err := requireCachedRepos(store, coords); err != nil {
			return nil, nil, err
		}
	}

	repos := make([]*gitremote.Repo, 0, len(coords))
	var prs []*repoPR
	for _, c := range coords {
		repo, err := gitremote.ParseRepo(c[0] + "/" + c[1] + "/" + c[2])
		if err != nil {
			return nil, nil, err
		}
		cached, err := store.LoadAllPRs(repo.Host, repo.Owner, repo.Name)
		if err != nil {
			return nil, nil, err
		}
		repos = append(repos, repo)
		for _, pr := range cached {
			prs = append(prs, &repoPR{Repo: repo.Owner + "/" + repo.Name, PR: pr})
		}
		logging.Debug("stats: loaded pull requests", "count", len(cached), "repo", repo.Owner+"/"+repo.Name)
	}
	return repos, prs, nil
}
