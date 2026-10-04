package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/webui"
)

var (
	serveAddr string
	serveOpen bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve a local web UI over the cached issues and PRs",
	Long: `Starts a local web server that browses the ghx cache: issue and PR lists and
details for every cached repository, markdown rendering, a Ctrl+P search palette,
and a refresh action that reuses the cache command's fetch logic.

The UI is read-only against the cache except for the explicit refresh action.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", "127.0.0.1:8080", "Address to listen on")
	serveCmd.Flags().BoolVar(&serveOpen, "open", false, "Open the browser after starting the server")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()
	server := webui.New(store, serveRefreshFunc(store))

	url := "http://" + serveAddr
	fmt.Printf("Serving ghx cache at %s\n", url)
	if serveOpen {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(os.Stderr, "Could not open browser: %s\n", err)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(serveAddr)
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
		repo := &gitremote.Repo{Host: host, Owner: owner, Name: name}
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
