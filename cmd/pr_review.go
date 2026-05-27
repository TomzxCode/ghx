package cmd

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Manage pull request reviews",
}

func init() {
	prCmd.AddCommand(reviewCmd)
}

var reviewCreateCmd = &cobra.Command{
	Use:   "create <number>",
	Short: "Start a pending review",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prNumber, err := parsePRNumber(args[0])
		if err != nil {
			return err
		}

		owner, name, err := gh.ResolveRepo(prRepo)
		if err != nil {
			return err
		}

		reviewID, err := gh.CreatePendingReview(owner, name, prNumber)
		if err != nil {
			return err
		}

		fmt.Printf("Created pending review %s on PR #%d\n", reviewID, prNumber)
		return nil
	},
}

var reviewSubmitCmd = &cobra.Command{
	Use:   "submit <number>",
	Short: "Submit a pending review",
	Long: `Submit your pending review on a pull request.

Finds your current pending review and submits it with the given event.
Use --review to submit a specific review by ID.`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewSubmit,
}

var (
	reviewEvent   string
	reviewBody    string
	reviewBodyFile string
	reviewIDFlag  string
)

func init() {
	reviewSubmitCmd.Flags().StringVar(&reviewEvent, "event", "COMMENT", "Review event: COMMENT, APPROVE, or REQUEST_CHANGES")
	reviewSubmitCmd.Flags().StringVarP(&reviewBody, "body", "b", "", "Review summary body")
	reviewSubmitCmd.Flags().StringVarP(&reviewBodyFile, "body-file", "F", "", "Read body from file (use \"-\" for stdin)")
	reviewSubmitCmd.Flags().StringVar(&reviewIDFlag, "review", "", "Specific review ID to submit (defaults to your current pending review)")

	reviewCmd.AddCommand(reviewCreateCmd)
	reviewCmd.AddCommand(reviewSubmitCmd)
	reviewCmd.AddCommand(reviewListCmd)
	reviewCmd.AddCommand(reviewDiscardCmd)
}

func runReviewSubmit(cmd *cobra.Command, args []string) error {
	prNumber, err := parsePRNumber(args[0])
	if err != nil {
		return err
	}

	validEvents := map[string]bool{"COMMENT": true, "APPROVE": true, "REQUEST_CHANGES": true}
	if !validEvents[reviewEvent] {
		return fmt.Errorf("invalid event %q: must be COMMENT, APPROVE, or REQUEST_CHANGES", reviewEvent)
	}

	body, _ := resolveBodyFlags(reviewBody, reviewBodyFile)

	owner, name, err := gh.ResolveRepo(prRepo)
	if err != nil {
		return err
	}

	reviewID := reviewIDFlag
	if reviewID == "" {
		reviews, err := gh.ListPendingReviews(owner, name, prNumber)
		if err != nil {
			return err
		}
		if len(reviews) == 0 {
			return fmt.Errorf("no pending review found for PR #%d", prNumber)
		}
		reviewID = reviews[0].ID
	}

	if err := gh.SubmitReview(reviewID, reviewEvent, body); err != nil {
		return err
	}

	fmt.Printf("Submitted review %s as %s\n", reviewID, reviewEvent)
	return nil
}

var reviewListCmd = &cobra.Command{
	Use:   "list <number>",
	Short: "List pending reviews",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Println("No pending reviews found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "REVIEW_ID\tAUTHOR\tCREATED")
		for _, r := range reviews {
			fmt.Fprintf(w, "%s\t%s\t%s\n", r.ID, r.Author, r.CreatedAt)
		}
		w.Flush()

		return nil
	},
}

var reviewDiscardCmd = &cobra.Command{
	Use:   "discard <review-id>",
	Short: "Discard a pending review",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := gh.DeleteReview(args[0]); err != nil {
			return err
		}

		fmt.Printf("Discarded review %s\n", args[0])
		return nil
	},
}

func parsePRNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid PR number: %s", s)
	}
	return n, nil
}
