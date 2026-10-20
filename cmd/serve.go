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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gitremote"
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
and a refresh action that reuses the cache command's fetch logic.

The UI is read-only against the cache except for the explicit refresh action.`,
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
