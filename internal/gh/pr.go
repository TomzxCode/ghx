package gh

import (
	"encoding/json"
	"fmt"
)

type ReviewThread struct {
	ID         string          `json:"id"`
	Path       string          `json:"path"`
	Line       *int            `json:"line"`
	StartLine  *int            `json:"startLine"`
	DiffSide   string          `json:"diffSide"`
	IsResolved bool            `json:"isResolved"`
	IsOutdated bool            `json:"isOutdated"`
	Comments   []ReviewComment `json:"comments"`
}

type ReviewComment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
	ReviewID  string `json:"reviewId"`
}

type PendingReview struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

type SavedThread struct {
	Path      string
	Line      *int
	StartLine *int
	Side      string
	Bodies    []string
}

func GetPRNodeID(owner, name string, number int) (string, error) {
	query := `
	query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			pullRequest(number: $number) {
				id
			}
		}
	}`

	data, err := GraphQL(query, map[string]interface{}{
		"owner":  owner,
		"name":   name,
		"number": number,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		Repository struct {
			PullRequest struct {
				ID string `json:"id"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.Repository.PullRequest.ID, nil
}

func AddTopLevelComment(subjectId, body string) (string, error) {
	query := `
	mutation($subjectId: ID!, $body: String!) {
		addComment(input: {subjectId: $subjectId, body: $body}) {
			commentEdge {
				node {
					id
				}
			}
		}
	}`

	data, err := GraphQL(query, map[string]interface{}{
		"subjectId": subjectId,
		"body":      body,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		AddComment struct {
			CommentEdge *struct {
				Node struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"commentEdge"`
		} `json:"addComment"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if result.AddComment.CommentEdge == nil {
		return "", fmt.Errorf("failed to create comment")
	}

	return result.AddComment.CommentEdge.Node.ID, nil
}

func AddReviewThread(pullRequestId, reviewId, body, path string, line, startLine *int, side, subjectType string) (string, error) {
	query := `
	mutation($pullRequestId: ID, $pullRequestReviewId: ID, $body: String!, $path: String, $line: Int, $side: DiffSide, $startLine: Int, $subjectType: PullRequestReviewThreadSubjectType) {
		addPullRequestReviewThread(input: {
			pullRequestId: $pullRequestId,
			pullRequestReviewId: $pullRequestReviewId,
			body: $body,
			path: $path,
			line: $line,
			side: $side,
			startLine: $startLine,
			subjectType: $subjectType
		}) {
			thread {
				id
				path
				line
				comments(first: 1) {
					nodes {
						id
					}
				}
			}
		}
	}`

	vars := map[string]interface{}{
		"pullRequestId":     nil,
		"pullRequestReviewId": nil,
		"body":              body,
		"path":              nil,
		"line":              nil,
		"side":              nil,
		"startLine":         nil,
		"subjectType":       nil,
	}

	if pullRequestId != "" {
		vars["pullRequestId"] = pullRequestId
	}
	if reviewId != "" {
		vars["pullRequestReviewId"] = reviewId
	}

	if path != "" {
		vars["path"] = path
	}
	if line != nil {
		vars["line"] = *line
	}
	if startLine != nil {
		vars["startLine"] = *startLine
	}
	if side != "" {
		vars["side"] = side
	}
	if subjectType != "" {
		vars["subjectType"] = subjectType
	}

	data, err := GraphQL(query, vars)
	if err != nil {
		return "", err
	}

	var result struct {
		AddPullRequestReviewThread struct {
			Thread *struct {
				ID   string `json:"id"`
				Path string `json:"path"`
			} `json:"thread"`
		} `json:"addPullRequestReviewThread"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if result.AddPullRequestReviewThread.Thread == nil {
		return "", fmt.Errorf("failed to create review thread: file or line may not be in the PR diff")
	}

	return result.AddPullRequestReviewThread.Thread.ID, nil
}

func ReplyToThread(threadId, reviewId, body string) (string, error) {
	query := `
	mutation($pullRequestReviewThreadId: ID!, $pullRequestReviewId: ID, $body: String!) {
		addPullRequestReviewThreadReply(input: {
			pullRequestReviewThreadId: $pullRequestReviewThreadId,
			pullRequestReviewId: $pullRequestReviewId,
			body: $body
		}) {
			comment {
				id
			}
		}
	}`

	vars := map[string]interface{}{
		"pullRequestReviewThreadId": threadId,
		"pullRequestReviewId":       nil,
		"body":                      body,
	}

	if reviewId != "" {
		vars["pullRequestReviewId"] = reviewId
	}

	data, err := GraphQL(query, vars)
	if err != nil {
		return "", err
	}

	var result struct {
		AddPullRequestReviewThreadReply struct {
			Comment struct {
				ID string `json:"id"`
			} `json:"comment"`
		} `json:"addPullRequestReviewThreadReply"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.AddPullRequestReviewThreadReply.Comment.ID, nil
}

func GetNodeType(nodeId string) (string, error) {
	query := `
	query($id: ID!) {
		node(id: $id) {
			__typename
		}
	}`

	data, err := GraphQL(query, map[string]interface{}{
		"id": nodeId,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		Node struct {
			Typename string `json:"__typename"`
		} `json:"node"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.Node.Typename, nil
}

func EditReviewComment(commentId, body string) error {
	query := `
	mutation($pullRequestReviewCommentId: ID!, $body: String!) {
		updatePullRequestReviewComment(input: {
			pullRequestReviewCommentId: $pullRequestReviewCommentId,
			body: $body
		}) {
			pullRequestReviewComment {
				id
			}
		}
	}`

	_, err := GraphQL(query, map[string]interface{}{
		"pullRequestReviewCommentId": commentId,
		"body":                       body,
	})
	return err
}

func EditIssueComment(commentId, body string) error {
	query := `
	mutation($id: ID!, $body: String!) {
		updateIssueComment(input: {
			id: $id,
			body: $body
		}) {
			issueComment {
				id
			}
		}
	}`

	_, err := GraphQL(query, map[string]interface{}{
		"id":   commentId,
		"body": body,
	})
	return err
}

func ListThreads(owner, name string, number int) ([]ReviewThread, error) {
	query := `
	query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			pullRequest(number: $number) {
				reviewThreads(first: 100) {
					nodes {
						id
						path
						line
						startLine
						diffSide
						isResolved
						isOutdated
						comments(first: 50) {
							nodes {
								id
								body
								author { login }
								createdAt
								pullRequestReview { id }
							}
						}
					}
				}
			}
		}
	}`

	data, err := GraphQL(query, map[string]interface{}{
		"owner":  owner,
		"name":   name,
		"number": number,
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						ID         string `json:"id"`
						Path       string `json:"path"`
						Line       *int   `json:"line"`
						StartLine  *int   `json:"startLine"`
						DiffSide   string `json:"diffSide"`
						IsResolved bool   `json:"isResolved"`
						IsOutdated bool   `json:"isOutdated"`
						Comments   struct {
							Nodes []struct {
								ID     string `json:"id"`
								Body   string `json:"body"`
								Author struct {
									Login string `json:"login"`
								} `json:"author"`
								CreatedAt          string `json:"createdAt"`
								PullRequestReview  struct {
									ID string `json:"id"`
								} `json:"pullRequestReview"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	threads := make([]ReviewThread, 0, len(result.Repository.PullRequest.ReviewThreads.Nodes))
	for _, t := range result.Repository.PullRequest.ReviewThreads.Nodes {
		comments := make([]ReviewComment, 0, len(t.Comments.Nodes))
		for _, c := range t.Comments.Nodes {
			comments = append(comments, ReviewComment{
				ID:        c.ID,
				Body:      c.Body,
				Author:    c.Author.Login,
				CreatedAt: c.CreatedAt,
				ReviewID:  c.PullRequestReview.ID,
			})
		}
		threads = append(threads, ReviewThread{
			ID:         t.ID,
			Path:       t.Path,
			Line:       t.Line,
			StartLine:  t.StartLine,
			DiffSide:   t.DiffSide,
			IsResolved: t.IsResolved,
			IsOutdated: t.IsOutdated,
			Comments:   comments,
		})
	}

	return threads, nil
}

func GetCurrentUser() (string, error) {
	query := `
	query {
		viewer {
			login
		}
	}`

	data, err := GraphQL(query, nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.Viewer.Login, nil
}

func CreatePendingReview(prID string) (string, error) {
	query := `
	mutation($pullRequestId: ID!) {
		addPullRequestReview(input: {
			pullRequestId: $pullRequestId
		}) {
			pullRequestReview {
				id
			}
		}
	}`

	data, err := GraphQL(query, map[string]interface{}{
		"pullRequestId": prID,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		AddPullRequestReview struct {
			Review struct {
				ID string `json:"id"`
			} `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.AddPullRequestReview.Review.ID, nil
}

func FindOrCreatePendingReview(owner, name string, number int) (string, error) {
	reviews, err := ListPendingReviews(owner, name, number)
	if err != nil {
		return "", err
	}

	if len(reviews) > 0 {
		return reviews[0].ID, nil
	}

	prID, err := GetPRNodeID(owner, name, number)
	if err != nil {
		return "", err
	}

	return CreatePendingReview(prID)
}

func ListPendingReviews(owner, name string, number int) ([]PendingReview, error) {
	query := `
	query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			pullRequest(number: $number) {
				reviews(first: 25, states: [PENDING]) {
					nodes {
						id
						author { login }
						createdAt
					}
				}
			}
		}
	}`

	data, err := GraphQL(query, map[string]interface{}{
		"owner":  owner,
		"name":   name,
		"number": number,
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Repository struct {
			PullRequest struct {
				Reviews struct {
					Nodes []struct {
						ID        string `json:"id"`
						Author    struct {
							Login string `json:"login"`
						} `json:"author"`
						CreatedAt string `json:"createdAt"`
					} `json:"nodes"`
				} `json:"reviews"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	currentUser, _ := GetCurrentUser()
	reviews := make([]PendingReview, 0)
	for _, r := range result.Repository.PullRequest.Reviews.Nodes {
		if currentUser != "" && r.Author.Login != currentUser {
			continue
		}
		reviews = append(reviews, PendingReview{
			ID:        r.ID,
			Author:    r.Author.Login,
			CreatedAt: r.CreatedAt,
		})
	}

	return reviews, nil
}

func SubmitReview(reviewId, event, body string) error {
	query := `
	mutation($pullRequestReviewId: ID!, $event: PullRequestReviewEvent!, $body: String) {
		submitPullRequestReview(input: {
			pullRequestReviewId: $pullRequestReviewId,
			event: $event,
			body: $body
		}) {
			pullRequestReview {
				id
			}
		}
	}`

	vars := map[string]interface{}{
		"pullRequestReviewId": reviewId,
		"event":               event,
		"body":                nil,
	}

	if body != "" {
		vars["body"] = body
	}

	_, err := GraphQL(query, vars)
	return err
}

func DeleteReview(reviewId string) error {
	query := `
	mutation($pullRequestReviewId: ID!) {
		deletePullRequestReview(input: {
			pullRequestReviewId: $pullRequestReviewId
		}) {
			pullRequestReview {
				id
			}
		}
	}`

	_, err := GraphQL(query, map[string]interface{}{
		"pullRequestReviewId": reviewId,
	})
	return err
}

func GetPendingReviewThreads(owner, name string, number int, reviewId string) ([]SavedThread, error) {
	threads, err := ListThreads(owner, name, number)
	if err != nil {
		return nil, err
	}

	var saved []SavedThread
	for _, t := range threads {
		if len(t.Comments) == 0 {
			continue
		}
		if t.Comments[0].ReviewID != reviewId {
			continue
		}

		bodies := make([]string, 0, len(t.Comments))
		for _, c := range t.Comments {
			bodies = append(bodies, c.Body)
		}

		saved = append(saved, SavedThread{
			Path:      t.Path,
			Line:      t.Line,
			StartLine: t.StartLine,
			Side:      t.DiffSide,
			Bodies:    bodies,
		})
	}

	return saved, nil
}

func RestorePendingReview(prID string, threads []SavedThread) (string, error) {
	reviewID, err := CreatePendingReview(prID)
	if err != nil {
		return "", fmt.Errorf("create pending review: %w", err)
	}

	for _, t := range threads {
		threadID, err := AddReviewThread("", reviewID, t.Bodies[0], t.Path, t.Line, t.StartLine, t.Side, "")
		if err != nil {
			return reviewID, fmt.Errorf("restore thread on %s: %w", t.Path, err)
		}

		for _, body := range t.Bodies[1:] {
			_, err := ReplyToThread(threadID, reviewID, body)
			if err != nil {
				return reviewID, fmt.Errorf("restore reply on %s: %w", t.Path, err)
			}
		}
	}

	return reviewID, nil
}
