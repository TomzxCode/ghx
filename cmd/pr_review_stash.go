package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var (
	stashMessage string
	stashIndex   int
)

var reviewStashCmd = &cobra.Command{
	Use:   "stash",
	Short: "Stash and restore pending review comments",
}

func init() {
	reviewStashPushCmd.Flags().StringVarP(&stashMessage, "message", "m", "", "Stash description")
	reviewStashPopCmd.Flags().IntVar(&stashIndex, "stash", 0, "Stash entry index to pop (default: 0)")
	reviewStashDropCmd.Flags().IntVar(&stashIndex, "stash", 0, "Stash entry index to drop (default: 0)")
	reviewStashCmd.AddCommand(reviewStashPushCmd)
	reviewStashCmd.AddCommand(reviewStashPopCmd)
	reviewStashCmd.AddCommand(reviewStashListCmd)
	reviewStashCmd.AddCommand(reviewStashDropCmd)
}

var reviewStashPushCmd = &cobra.Command{
	Use:   "push <number>",
	Short: "Stash pending review comments to local disk",
	Long: `Save all pending review comments to a local stash entry and delete
the pending review from GitHub. Use 'stash pop' to restore them later.
Supports multiple stash entries like git stash.`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewStashPush,
}

func runReviewStashPush(cmd *cobra.Command, args []string) error {
	prNumber, err := parsePRNumber(args[0])
	if err != nil {
		return err
	}

	owner, name, err := gh.ResolveRepo(prRepo)
	if err != nil {
		return err
	}

	reviews, err := gh.ListPendingReviews(owner, name, prNumber)
	if err != nil {
		return err
	}
	if len(reviews) == 0 {
		return fmt.Errorf("no pending review found for %s/%s#%d", owner, name, prNumber)
	}

	reviewID := reviews[0].ID

	threads, err := gh.GetPendingReviewThreads(owner, name, prNumber, reviewID)
	if err != nil {
		return fmt.Errorf("fetch pending threads: %w", err)
	}

	if len(threads) == 0 {
		return fmt.Errorf("pending review %s has no comments to stash", reviewID)
	}

	total, err := gh.PushStash(owner, name, prNumber, threads, stashMessage)
	if err != nil {
		return fmt.Errorf("save stash: %w", err)
	}

	if err := gh.DeleteReview(reviewID); err != nil {
		return fmt.Errorf("delete pending review: %w", err)
	}

	totalComments := 0
	for _, t := range threads {
		totalComments += len(t.Bodies)
	}

	if stashMessage != "" {
		fmt.Printf("Saved stash@{0} \"%s\" (%d threads, %d comments) from review %s\n", stashMessage, len(threads), totalComments, reviewID)
	} else {
		fmt.Printf("Saved stash@{0} (%d threads, %d comments) from review %s\n", len(threads), totalComments, reviewID)
	}
	if total > 1 {
		fmt.Printf("(%d stash entries total)\n", total)
	}
	return nil
}

var reviewStashPopCmd = &cobra.Command{
	Use:   "pop <number>",
	Short: "Restore stashed pending review comments",
	Long: `Restore stashed pending review comments by creating
a new pending review on GitHub and recreating all threads.
Pops the stash entry at the given index (default: 0). Use --stash to pop a specific entry.`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewStashPop,
}

func runReviewStashPop(cmd *cobra.Command, args []string) error {
	prNumber, err := parsePRNumber(args[0])
	if err != nil {
		return err
	}

	owner, name, err := gh.ResolveRepo(prRepo)
	if err != nil {
		return err
	}

	threads, err := gh.GetStashEntry(owner, name, prNumber, stashIndex)
	if err != nil {
		return err
	}

	reviews, err := gh.ListPendingReviews(owner, name, prNumber)
	if err != nil {
		return err
	}
	if len(reviews) > 0 {
		return fmt.Errorf("existing pending review %s found; submit or discard it before popping stash", reviews[0].ID)
	}

	prID, err := gh.GetPRNodeID(owner, name, prNumber)
	if err != nil {
		return err
	}

	newReviewID, err := gh.RestorePendingReview(prID, threads)
	if err != nil {
		return fmt.Errorf("restore pending review: %w", err)
	}

	if err := gh.RemoveStashEntry(owner, name, prNumber, stashIndex); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove stash entry: %v\n", err)
	}

	totalComments := 0
	for _, t := range threads {
		totalComments += len(t.Bodies)
	}

	fmt.Printf("Popped stash@{%d} (%d threads, %d comments) into review %s\n", stashIndex, len(threads), totalComments, newReviewID)
	return nil
}

var reviewStashListCmd = &cobra.Command{
	Use:   "list <number>",
	Short: "List stashed pending review comments",
	Args:  cobra.ExactArgs(1),
	RunE:  runReviewStashList,
}

func runReviewStashList(cmd *cobra.Command, args []string) error {
	prNumber, err := parsePRNumber(args[0])
	if err != nil {
		return err
	}

	owner, name, err := gh.ResolveRepo(prRepo)
	if err != nil {
		return err
	}

	entries, err := gh.ListStashEntries(owner, name, prNumber)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, entry := range entries {
		totalComments := 0
		for _, t := range entry.Threads {
			totalComments += len(t.Bodies)
		}

		if entry.Message != "" {
			fmt.Fprintf(w, "stash@{%d}:\t%s\t(%d threads, %d comments)\n", i, entry.Message, len(entry.Threads), totalComments)
		} else {
			fmt.Fprintf(w, "stash@{%d}:\t%d threads, %d comments\n", i, len(entry.Threads), totalComments)
		}

		for _, t := range entry.Threads {
			line := "-"
			if t.Line != nil {
				line = fmt.Sprintf("%d", *t.Line)
				if t.StartLine != nil {
					line = fmt.Sprintf("%d-%d", *t.StartLine, *t.Line)
				}
			}
			fmt.Fprintf(w, "\t%s\t%s\t%d comment(s)\n", t.Path, line, len(t.Bodies))
		}

		if i < len(entries)-1 {
			fmt.Fprintln(w)
		}
	}
	w.Flush()

	return nil
}

var reviewStashDropCmd = &cobra.Command{
	Use:   "drop <number>",
	Short: "Drop a stash entry without restoring it",
	Long: `Remove a stash entry without restoring it.
By default drops stash@{0}. Use --stash to drop a specific entry.`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewStashDrop,
}

func runReviewStashDrop(cmd *cobra.Command, args []string) error {
	prNumber, err := parsePRNumber(args[0])
	if err != nil {
		return err
	}

	owner, name, err := gh.ResolveRepo(prRepo)
	if err != nil {
		return err
	}

	entries, err := gh.ListStashEntries(owner, name, prNumber)
	if err != nil {
		return err
	}

	if stashIndex < 0 || stashIndex >= len(entries) {
		return fmt.Errorf("stash@{%d} does not exist (have %d stash entries)", stashIndex, len(entries))
	}

	totalComments := 0
	for _, t := range entries[stashIndex].Threads {
		totalComments += len(t.Bodies)
	}

	if err := gh.DropStash(owner, name, prNumber, stashIndex); err != nil {
		return err
	}

	fmt.Printf("Dropped stash@{%d} (%d threads, %d comments)\n", stashIndex, len(entries[stashIndex].Threads), totalComments)
	return nil
}
