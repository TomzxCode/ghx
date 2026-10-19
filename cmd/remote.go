package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/tomzxcode/ghx/internal/rpc"
)

// exitError carries a child command's exit status so Execute can mirror it
// instead of always exiting 1.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("exit status %d", e.code)
}

// takeRemoteArgs removes a --remote flag (and an optional --rpc-token) from the
// command line. When --remote is present, the remaining arguments are forwarded
// to the server instead of being executed locally.
func takeRemoteArgs(args []string) (url, token string, rest []string, ok bool) {
	// Only strip --rpc-token when --remote is actually present, so the flag is
	// not silently dropped from a local invocation.
	for _, arg := range args {
		if arg == "--remote" || strings.HasPrefix(arg, "--remote=") {
			ok = true
			break
		}
	}
	if !ok {
		return "", "", nil, false
	}

	url = os.Getenv("GHX_RPC_URL")
	token = os.Getenv("GHX_RPC_TOKEN")
	rest = make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--remote":
			ok = true
			if i+1 < len(args) && looksLikeRemoteURL(args[i+1]) {
				url = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--remote="):
			ok = true
			url = strings.TrimPrefix(arg, "--remote=")
		case arg == "--rpc-token":
			if i+1 < len(args) {
				token = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--rpc-token="):
			token = strings.TrimPrefix(arg, "--rpc-token=")
		default:
			rest = append(rest, arg)
		}
	}
	return url, token, rest, ok
}

// runRemote executes the remaining command on the server and returns its exit
// status. Output is forwarded verbatim so scripts behave as they do locally.
func runRemote(url, token string, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: --remote requires a ghx command")
		return 1
	}
	client := rpc.NewClient(url, token)
	result, err := client.Exec(context.Background(), args, readPipedStdin())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return 1
	}
	writeRemoteOutput(result)
	return result.ExitCode
}

// looksLikeRemoteURL distinguishes an explicit --remote value from the first
// command argument, so `--remote issue list` uses GHX_RPC_URL while
// `--remote http://host:8090 issue list` uses the given address.
func looksLikeRemoteURL(s string) bool {
	if strings.Contains(s, "://") {
		return true
	}
	host, port, err := net.SplitHostPort(s)
	return err == nil && host != "" && port != ""
}
