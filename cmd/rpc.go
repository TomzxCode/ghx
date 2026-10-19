package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/rpc"
	"github.com/tomzxcode/ghx/internal/version"
)

var (
	rpcAddr      string
	rpcURL       string
	rpcToken     string
	rpcAllowExec bool
)

var rpcCmd = &cobra.Command{
	Use:   "rpc",
	Short: "Serve or call the JSON-RPC API",
	Long: `Exposes the ghx CLI over JSON-RPC 2.0.

The server offers typed methods for the read and cache commands (repo.list,
issue.list, issue.view, pr.list, pr.view, pr.threads, repo.delete,
cache.refresh) and, unless disabled, a cli.run method that executes any ghx
command and returns its stdout, stderr, and exit code.

Clients can call methods with "ghx rpc call", use --remote on any command to
run it on a server, or speak JSON-RPC directly over HTTP POST /rpc.`,
}

var rpcServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the JSON-RPC server",
	Long: `Starts an HTTP server that answers JSON-RPC 2.0 requests at POST /rpc.

By default it listens on 127.0.0.1:8090. Binding a non-loopback address
requires --token so the write-capable methods are not exposed unauthenticated.
The server uses the same cache and GitHub client configuration as the CLI;
pass --storage, --cache-dir, and --api-url when starting it to select them.`,
	RunE: runRPCServe,
}

var rpcCallCmd = &cobra.Command{
	Use:   "call <method> [params-json]",
	Short: "Call one method on a running RPC server",
	Long: `Calls one method and prints its JSON result.

params-json is a JSON object. Use "-" to read it from stdin, or "@path" to
read it from a file. When omitted, the method is called without params.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runRPCCall,
}

var rpcMethodsCmd = &cobra.Command{
	Use:   "methods",
	Short: "List the methods a running RPC server exposes",
	Args:  cobra.NoArgs,
	RunE:  runRPCMethods,
}

var rpcExecCmd = &cobra.Command{
	Use:                   "exec [--url URL] [--token TOKEN] [--] <ghx args...>",
	Short:                 "Run a ghx command on a running RPC server",
	DisableFlagParsing:    true,
	Args:                  cobra.ArbitraryArgs,
	DisableFlagsInUseLine: true,
	RunE:                  runRPCExec,
}

func init() {
	rpcCmd.PersistentFlags().StringVar(&rpcURL, "url", defaultRPCURL(), "RPC server URL (env GHX_RPC_URL)")
	rpcCmd.PersistentFlags().StringVar(&rpcToken, "token", os.Getenv("GHX_RPC_TOKEN"), "Bearer token for the RPC client or server (env GHX_RPC_TOKEN)")

	rpcServeCmd.Flags().StringVar(&rpcAddr, "addr", "127.0.0.1:8090", "Address to listen on")
	rpcServeCmd.Flags().BoolVar(&rpcAllowExec, "allow-exec", true, "Expose cli.run, which executes ghx commands for clients")

	rpcCmd.AddCommand(rpcServeCmd)
	rpcCmd.AddCommand(rpcCallCmd)
	rpcCmd.AddCommand(rpcMethodsCmd)
	rpcCmd.AddCommand(rpcExecCmd)
	rootCmd.AddCommand(rpcCmd)
}

func defaultRPCURL() string {
	if v := os.Getenv("GHX_RPC_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8090"
}

func runRPCServe(cmd *cobra.Command, args []string) error {
	if rpcToken == "" && !isLoopbackAddr(rpcAddr) {
		return fmt.Errorf("refusing to listen on %s without --token: the RPC API can modify the cache and GitHub", rpcAddr)
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()

	server := rpc.NewServer(newRPCConfig(store, rpcToken, rpcAllowExec))
	httpServer := &http.Server{
		Addr:              rpcAddr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	fmt.Printf("Serving ghx JSON-RPC at http://%s/rpc\n", rpcAddr)
	if rpcToken != "" {
		fmt.Println("Bearer token authentication is required.")
	}
	if rpcAllowExec {
		fmt.Println("cli.run is enabled: clients can execute ghx commands on this server.")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-stop:
		fmt.Println("\nShutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	}
}

func runRPCCall(cmd *cobra.Command, args []string) error {
	params, err := readRPCParams(args)
	if err != nil {
		return err
	}
	client := rpc.NewClient(rpcURL, rpcToken)
	raw, err := client.Call(cmd.Context(), args[0], params)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		fmt.Fprintln(os.Stdout, string(raw))
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(pretty)
}

func readRPCParams(args []string) (json.RawMessage, error) {
	if len(args) < 2 {
		return nil, nil
	}
	raw := strings.TrimSpace(args[1])
	switch {
	case raw == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading params from stdin: %w", err)
		}
		raw = strings.TrimSpace(string(data))
	case strings.HasPrefix(raw, "@"):
		data, err := os.ReadFile(strings.TrimPrefix(raw, "@"))
		if err != nil {
			return nil, fmt.Errorf("reading params file: %w", err)
		}
		raw = strings.TrimSpace(string(data))
	}
	if raw == "" {
		return nil, nil
	}
	if !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("params is not valid JSON")
	}
	return json.RawMessage(raw), nil
}

func runRPCMethods(cmd *cobra.Command, args []string) error {
	client := rpc.NewClient(rpcURL, rpcToken)
	info, err := client.Discover(cmd.Context())
	if err != nil {
		return err
	}
	if len(info.Methods) == 0 {
		fmt.Println("No methods exposed.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "METHOD\tDESCRIPTION")
	for _, m := range info.Methods {
		fmt.Fprintf(w, "%s\t%s\n", m.Name, m.Description)
	}
	return w.Flush()
}

// runRPCExec implements `ghx rpc exec`. Flag parsing is disabled so every
// argument after the command belongs to the remote ghx invocation. Leading
// --url/--token/--allow-exec options are recognised for convenience.
func runRPCExec(cmd *cobra.Command, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return cmd.Help()
	}
	url, token, remoteArgs, err := splitRPCExecArgs(args, rpcURL, rpcToken)
	if err != nil {
		return err
	}
	if len(remoteArgs) == 0 {
		return fmt.Errorf("no ghx command given: ghx rpc exec -- issue list")
	}
	client := rpc.NewClient(url, token)
	result, err := client.Exec(cmd.Context(), remoteArgs, readPipedStdin())
	if err != nil {
		return err
	}
	writeRemoteOutput(result)
	if result.ExitCode != 0 {
		return &exitError{code: result.ExitCode}
	}
	return nil
}

func splitRPCExecArgs(args []string, defaultURL, defaultToken string) (string, string, []string, error) {
	url, token := defaultURL, defaultToken
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--":
			return url, token, args[i+1:], nil
		case arg == "--url":
			if i+1 >= len(args) {
				return "", "", nil, fmt.Errorf("--url requires a value")
			}
			url = args[i+1]
			i += 2
		case strings.HasPrefix(arg, "--url="):
			url = strings.TrimPrefix(arg, "--url=")
			i++
		case arg == "--token":
			if i+1 >= len(args) {
				return "", "", nil, fmt.Errorf("--token requires a value")
			}
			token = args[i+1]
			i += 2
		case strings.HasPrefix(arg, "--token="):
			token = strings.TrimPrefix(arg, "--token=")
			i++
		default:
			return url, token, args[i:], nil
		}
	}
	return url, token, nil, nil
}

// writeRemoteOutput prints a remote command's captured output and mirrors its
// exit status by returning an error when the command failed.
func writeRemoteOutput(result rpc.ExecResult) {
	if result.Stdout != "" {
		_, _ = io.WriteString(os.Stdout, result.Stdout)
	}
	if result.Stderr != "" {
		_, _ = io.WriteString(os.Stderr, result.Stderr)
	}
}

// readPipedStdin returns piped standard input so commands that read stdin
// (for example --body-file -) work through a remote server. A terminal is not
// read, since that would block waiting for input that will never come.
func readPipedStdin() string {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return string(data)
}

// newRPCConfig wires the RPC server to the same helpers the CLI commands use.
func newRPCConfig(store cache.Store, token string, allowExec bool) rpc.Config {
	return rpc.Config{
		Store: store,
		ClientFactory: func(host string) (*github.Client, error) {
			return newClient(host)
		},
		DefaultRepo:    getRepo,
		Refresh:        rpcRefreshFunc(store),
		ParseSince:     parseSinceDate,
		BackfillIssues: backfillIssueDetails,
		BackfillPRs:    backfillPRDetails,
		Version:        version.Get(),
		Token:          token,
		AllowExec:      allowExec,
		Exec: rpc.ExecConfig{
			BaseArgs: rpcBaseArgs(),
		},
	}
}

// rpcBaseArgs returns the global flags a cli.run child process needs so it
// reads and writes the same cache and GitHub endpoint as the server.
func rpcBaseArgs() []string {
	var args []string
	if repoFlag != "" {
		args = append(args, "--repo", repoFlag)
	}
	if storageFlag != "" {
		args = append(args, "--storage", storageFlag)
	}
	if cacheDir != "" {
		args = append(args, "--cache-dir", cacheDir)
	}
	if apiURLFlag != "" {
		args = append(args, "--api-url", apiURLFlag)
	}
	if logLevelFlag != "" {
		args = append(args, "--log-level", logLevelFlag)
	}
	if telemetryDBFlag != "" {
		args = append(args, "--telemetry-db", telemetryDBFlag)
	}
	if telemetryFlag != nil && !*telemetryFlag {
		args = append(args, "--telemetry=false")
	}
	return args
}

// rpcRefreshFunc adapts the cache command's fetch logic to the RPC server.
func rpcRefreshFunc(store cache.Store) rpc.RefreshFunc {
	return func(ctx context.Context, req rpc.RefreshRequest) (rpc.RefreshResult, error) {
		client, err := newClient(req.Repo.Host)
		if err != nil {
			return rpc.RefreshResult{}, err
		}
		client.SetRetries(req.Retries)

		fetchIssues, fetchPRs := true, true
		switch req.Type {
		case "issues":
			fetchPRs = false
		case "prs":
			fetchIssues = false
		}

		info, _ := store.LoadCacheInfo(req.Repo.Host, req.Repo.Owner, req.Repo.Name)
		if info == nil {
			info = &cache.CacheInfo{}
		}
		issues, prs, err := fetchRepoData(store, client, req.Repo, info, FetchOptions{
			Force:       req.Force,
			Since:       req.Since,
			FetchIssues: fetchIssues,
			FetchPRs:    fetchPRs,
			Duration:    req.Duration,
		})
		return rpc.RefreshResult{Issues: issues, PRs: prs}, err
	}
}

// isLoopbackAddr reports whether addr binds only to a loopback interface.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
