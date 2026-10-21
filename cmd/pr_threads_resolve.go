package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var prThreadsResolveCmd = &cobra.Command{
	Use:   "resolve <thread-id>...",
	Short: "Resolve review threads",
	Long: `Mark one or more review threads as resolved.

Every thread is attempted even when one fails; failures are reported and the
command exits non-zero. Find thread IDs with 'ghx pr threads <number> --ids'.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setThreadsResolved(os.Stdout, args, true)
	},
}

var prThreadsUnresolveCmd = &cobra.Command{
	Use:   "unresolve <thread-id>...",
	Short: "Reopen resolved review threads",
	Long: `Reopen one or more resolved review threads.

Every thread is attempted even when one fails; failures are reported and the
command exits non-zero. Find thread IDs with
'ghx pr threads <number> --ids --state resolved'.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setThreadsResolved(os.Stdout, args, false)
	},
}

func setThreadsResolved(w io.Writer, threadIDs []string, resolved bool) error {
	set := gh.ResolveThread
	message := "Resolved thread %s\n"
	if !resolved {
		set = gh.UnresolveThread
		message = "Unresolved thread %s\n"
	}

	return applyThreadState(w, threadIDs, set, message)
}

// applyThreadState runs set on every thread, collecting failures instead of
// stopping at the first one.
func applyThreadState(w io.Writer, threadIDs []string, set func(string) error, message string) error {
	var failures []error
	for _, threadID := range threadIDs {
		if err := set(threadID); err != nil {
			failures = append(failures, fmt.Errorf("thread %s: %w", threadID, err))
			continue
		}
		fmt.Fprintf(w, message, threadID)
	}

	return errors.Join(failures...)
}

func init() {
	prThreadsCmd.AddCommand(prThreadsResolveCmd)
	prThreadsCmd.AddCommand(prThreadsUnresolveCmd)
}
