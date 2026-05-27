package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var reviewStashCmd = &cobra.Command{
	Use:   "stash",
	Short: "Stash and restore pending review comments",
}

func init() {
	reviewStashCmd.AddCommand(reviewStashPushCmd)
	reviewStashCmd.AddCommand(reviewStashPopCmd)
	reviewStashCmd.AddCommand(reviewStashListCmd)
}

var reviewStashPushCmd = &cobra.Command{
	Use:   "push <number>",
	Short: "Stash pending review comments to local disk",
	Long: `Save all pending review comments to a local stash file and delete
the pending review from GitHub. Use 'stash pop' to restore them later.`,
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

	if err := gh.SaveStash(owner, name, prNumber, threads); err != nil {
		return fmt.Errorf("save stash: %w", err)
	}

	if err := gh.DeleteReview(reviewID); err != nil {
		return fmt.Errorf("delete pending review: %w", err)
	}

	totalComments := 0
	for _, t := range threads {
		totalComments += len(t.Bodies)
	}

	fmt.Printf("Stashed %d threads (%d comments) from review %s\n", len(threads), totalComments, reviewID)
	return nil
}

var reviewStashPopCmd = &cobra.Command{
	Use:   "pop <number>",
	Short: "Restore stashed pending review comments",
	Long: `Restore previously stashed pending review comments by creating
a new pending review on GitHub and recreating all threads.`,
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

	threads, err := gh.LoadStash(owner, name, prNumber)
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

	if err := gh.ClearStash(owner, name, prNumber); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear stash file: %v\n", err)
	}

	totalComments := 0
	for _, t := range threads {
		totalComments += len(t.Bodies)
	}

	fmt.Printf("Popped %d threads (%d comments) into review %s\n", len(threads), totalComments, newReviewID)
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

	threads, err := gh.LoadStash(owner, name, prNumber)
	if err != nil {
		return err
	}

	totalComments := 0
	for _, t := range threads {
		totalComments += len(t.Bodies)
	}

	fmt.Printf("Stash: %d threads, %d comments\n\n", len(threads), totalComments)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FILE\tLINE\tCOMMENTS")
	for _, t := range threads {
		line := "-"
		if t.Line != nil {
			line = fmt.Sprintf("%d", *t.Line)
			if t.StartLine != nil {
				line = fmt.Sprintf("%d-%d", *t.StartLine, *t.Line)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%d\n", t.Path, line, len(t.Bodies))
	}
	w.Flush()

	return nil
}
