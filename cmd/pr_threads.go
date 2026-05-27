package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"os"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var (
	threadsShowComments bool
	threadsThreadID     string
	threadsState        string
	threadsShowIDs      bool
)

var prThreadsCmd = &cobra.Command{
	Use:   "threads <number>",
	Short: "List review threads on a pull request",
	Long: `List review threads on a pull request.

Shows thread ID, file, line, state, and first comment preview by default.
Use --comments to show all comments within each thread.
Use --ids to show individual comment IDs (useful for editing).`,
	Args: cobra.ExactArgs(1),
	RunE: runPRThreads,
}

func init() {
	prThreadsCmd.Flags().BoolVar(&threadsShowComments, "comments", false, "Show all comments in each thread")
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

	if threadsShowComments {
		printThreadsWithComments(threads)
	} else {
		printThreadsTable(threads)
	}

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

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func printThreadsTable(threads []gh.ReviewThread) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "THREAD_ID\tFILE\tLINE\tSTATE\tAUTHOR\tPREVIEW")
	for _, t := range threads {
		author := ""
		preview := ""
		if len(t.Comments) > 0 {
			author = t.Comments[0].Author
			preview = truncate(t.Comments[0].Body, 60)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Path, threadLineStr(t), threadStateStr(t), author, preview)
	}
	w.Flush()
}

func printThreadsWithComments(threads []gh.ReviewThread) {
	for i, t := range threads {
		loc := t.Path
		if t.Line != nil {
			loc = fmt.Sprintf("%s:%s", t.Path, threadLineStr(t))
		}
		state := threadStateStr(t)
		fmt.Printf("%s  %s  [%s]\n", t.ID, loc, state)

		for _, c := range t.Comments {
			if threadsShowIDs {
				fmt.Printf("  %s\t%s\t%s\n", c.ID, c.Author, strings.ReplaceAll(c.Body, "\n", "\n  \t\t"))
			} else {
				fmt.Printf("  %s\t%s\n", c.Author, strings.ReplaceAll(c.Body, "\n", "\n  \t"))
			}
		}

		if i < len(threads)-1 {
			fmt.Println()
		}
	}
}
