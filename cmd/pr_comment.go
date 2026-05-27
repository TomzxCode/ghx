package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	gh "github.com/tomzxcode/ghx/internal/gh"
)

var (
	commentBody         string
	commentBodyFile     string
	commentFile         string
	commentLine         string
	commentSide         string
	commentReplyThread  string
)

var prCommentCmd = &cobra.Command{
	Use:   "comment <number>",
	Short: "Comment on a pull request",
	Long: `Add a comment to a pull request.

Supports top-level comments, inline comments on specific files/lines,
file-level comments, and replies to existing review threads.

Without --file, adds a top-level comment.
With --file (no --line), adds a file-level comment.
With --file and --line, adds an inline comment.
With --reply-thread, replies to an existing review thread.`,
	Args: cobra.ExactArgs(1),
	RunE: runPRComment,
}

func init() {
	prCommentCmd.Flags().StringVarP(&commentBody, "body", "b", "", "Comment body text")
	prCommentCmd.Flags().StringVarP(&commentBodyFile, "body-file", "F", "", "Read body text from file (use \"-\" for stdin)")
	prCommentCmd.Flags().StringVar(&commentFile, "file", "", "File path for inline comments")
	prCommentCmd.Flags().StringVar(&commentLine, "line", "", "Line number or range (e.g., 42 or 42-45)")
	prCommentCmd.Flags().StringVar(&commentSide, "side", "RIGHT", "Diff side: LEFT or RIGHT")
	prCommentCmd.Flags().StringVar(&commentReplyThread, "reply-thread", "", "Thread ID to reply to")
	prCommentCmd.MarkFlagsMutuallyExclusive("body", "body-file")
	prCommentCmd.MarkFlagsMutuallyExclusive("reply-thread", "file")
	prCommentCmd.MarkFlagsMutuallyExclusive("reply-thread", "line")

	prCmd.AddCommand(prCommentCmd)
}

func resolveBodyFlags(body, bodyFile string) (string, error) {
	if body != "" {
		return body, nil
	}
	if bodyFile != "" {
		var r io.Reader
		if bodyFile == "-" {
			r = os.Stdin
		} else {
			f, err := os.Open(bodyFile)
			if err != nil {
				return "", fmt.Errorf("open body file: %w", err)
			}
			defer f.Close()
			r = f
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return "", fmt.Errorf("read body: %w", err)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("required: --body or --body-file")
}

func parseLineRange(lineStr string) (line *int, startLine *int, err error) {
	if lineStr == "" {
		return nil, nil, nil
	}

	parts := strings.SplitN(lineStr, "-", 2)
	if len(parts) == 1 {
		l, e := strconv.Atoi(strings.TrimSpace(parts[0]))
		if e != nil {
			return nil, nil, fmt.Errorf("invalid line number: %s", lineStr)
		}
		return &l, nil, nil
	}

	start, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if e1 != nil || e2 != nil {
		return nil, nil, fmt.Errorf("invalid line range: %s", lineStr)
	}
	if start >= end {
		return nil, nil, fmt.Errorf("invalid line range: start must be less than end")
	}
	return &end, &start, nil
}

func runPRComment(cmd *cobra.Command, args []string) error {
	prNumber, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
	}

	if commentLine != "" && commentFile == "" {
		return fmt.Errorf("--line requires --file")
	}
	if cmd.Flags().Changed("side") && commentFile == "" {
		return fmt.Errorf("--side requires --file")
	}

	body, err := resolveBodyFlags(commentBody, commentBodyFile)
	if err != nil {
		return err
	}

	owner, name, err := gh.ResolveRepo(prRepo)
	if err != nil {
		return err
	}

	if commentReplyThread != "" {
		commentID, err := gh.ReplyToThread(commentReplyThread, body)
		if err != nil {
			return err
		}
		fmt.Printf("Replied to thread %s (comment %s)\n", commentReplyThread, commentID)
		return nil
	}

	prID, err := gh.GetPRNodeID(owner, name, prNumber)
	if err != nil {
		return err
	}

	if commentFile != "" {
		line, startLine, err := parseLineRange(commentLine)
		if err != nil {
			return err
		}

		subjectType := ""
		if line == nil {
			subjectType = "FILE"
		}

		threadID, err := gh.AddReviewThread(prID, body, commentFile, line, startLine, commentSide, subjectType)
		if err != nil {
			return err
		}

		if line != nil {
			fmt.Printf("Created inline comment on %s:%s (thread %s)\n", commentFile, commentLine, threadID)
		} else {
			fmt.Printf("Created file-level comment on %s (thread %s)\n", commentFile, threadID)
		}
		return nil
	}

	commentID, err := gh.AddTopLevelComment(prID, body)
	if err != nil {
		return err
	}
	fmt.Printf("Created comment %s on PR #%d\n", commentID, prNumber)
	return nil
}
