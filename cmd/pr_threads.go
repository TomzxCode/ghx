package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var (
	threadsThreadID string
	threadsState    string
	threadsShowIDs  bool
)

var prThreadsCmd = &cobra.Command{
	Use:   "threads <number>",
	Short: "List review threads on a pull request",
	Long: `List review threads on a pull request.

Shows all comments within each thread by default.
Use --ids to show individual comment IDs (useful for editing/deleting).`,
	Args: cobra.ExactArgs(1),
	RunE: runPRThreads,
}

func init() {
	prThreadsCmd.Flags().StringVar(&threadsThreadID, "thread", "", "Show a specific thread by ID")
	prThreadsCmd.Flags().StringVar(&threadsState, "state", "open", "Filter by state: open, resolved, all")
	prThreadsCmd.Flags().BoolVar(&threadsShowIDs, "ids", false, "Show comment IDs")

	prCmd.AddCommand(prThreadsCmd)
}

func runPRThreads(cmd *cobra.Command, args []string) error {
	prNumber, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
	}

	owner, name, err := gh.ResolveRepo(prRepo)
	if err != nil {
		return err
	}

	threads, err := gh.ListThreads(owner, name, prNumber)
	if err != nil {
		return err
	}

	threads = filterThreads(threads)

	if threadsThreadID != "" {
		threads = filterByThreadID(threads, threadsThreadID)
		if len(threads) == 0 {
			return fmt.Errorf("thread %s not found", threadsThreadID)
		}
	}

	if len(threads) == 0 {
		fmt.Println("No review threads found.")
		return nil
	}

	printThreads(threads)

	return nil
}

func filterThreads(threads []gh.ReviewThread) []gh.ReviewThread {
	filtered := make([]gh.ReviewThread, 0, len(threads))
	for _, t := range threads {
		switch threadsState {
		case "open":
			if !t.IsResolved {
				filtered = append(filtered, t)
			}
		case "resolved":
			if t.IsResolved {
				filtered = append(filtered, t)
			}
		case "all":
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func filterByThreadID(threads []gh.ReviewThread, id string) []gh.ReviewThread {
	for _, t := range threads {
		if t.ID == id {
			return []gh.ReviewThread{t}
		}
	}
	return nil
}

func threadLineStr(t gh.ReviewThread) string {
	if t.StartLine != nil && t.Line != nil {
		return fmt.Sprintf("%d-%d", *t.StartLine, *t.Line)
	}
	if t.Line != nil {
		return fmt.Sprintf("%d", *t.Line)
	}
	return ""
}

func threadStateStr(t gh.ReviewThread) string {
	if t.IsResolved {
		return "resolved"
	}
	if t.IsOutdated {
		return "outdated"
	}
	return "open"
}

func printThreads(threads []gh.ReviewThread) {
	for _, t := range threads {
		loc := t.Path
		if t.Line != nil {
			loc = fmt.Sprintf("%s:%s", t.Path, threadLineStr(t))
		}

		if threadsShowIDs {
			fmt.Printf("%s  %s  [%s]\n", t.ID, loc, threadStateStr(t))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, c := range t.Comments {
				for i, line := range strings.Split(c.Body, "\n") {
					if i == 0 {
						fmt.Fprintf(w, "%s\t%s\t%s\n", c.ID, c.Author, line)
					} else {
						fmt.Fprintf(w, "\t\t%s\n", line)
					}
				}
			}
			w.Flush()
		} else {
			fmt.Printf("%s  [%s]\n", loc, threadStateStr(t))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, c := range t.Comments {
				for i, line := range strings.Split(c.Body, "\n") {
					if i == 0 {
						fmt.Fprintf(w, "%s\t%s\n", c.Author, line)
					} else {
						fmt.Fprintf(w, "\t%s\n", line)
					}
				}
			}
			w.Flush()
		}

		fmt.Println()
	}
}
