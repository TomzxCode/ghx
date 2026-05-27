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
			CommentEdge struct {
				Node struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"commentEdge"`
		} `json:"addComment"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.AddComment.CommentEdge.Node.ID, nil
}

func AddReviewThread(pullRequestId, body, path string, line, startLine *int, side, subjectType string) (string, error) {
	query := `
	mutation($pullRequestId: ID!, $body: String!, $path: String, $line: Int, $side: DiffSide, $startLine: Int, $subjectType: PullRequestReviewThreadSubjectType) {
		addPullRequestReviewThread(input: {
			pullRequestId: $pullRequestId,
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
		"pullRequestId": pullRequestId,
		"body":          body,
		"path":          nil,
		"line":          nil,
		"side":          nil,
		"startLine":     nil,
		"subjectType":   nil,
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
			Thread struct {
				ID   string `json:"id"`
				Path string `json:"path"`
			} `json:"thread"`
		} `json:"addPullRequestReviewThread"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.AddPullRequestReviewThread.Thread.ID, nil
}

func ReplyToThread(threadId, body string) (string, error) {
	query := `
	mutation($pullRequestReviewThreadId: ID!, $body: String!) {
		addPullRequestReviewThreadReply(input: {
			pullRequestReviewThreadId: $pullRequestReviewThreadId,
			body: $body
		}) {
			comment {
				id
			}
		}
	}`

	data, err := GraphQL(query, map[string]interface{}{
		"pullRequestReviewThreadId": threadId,
		"body":                      body,
	})
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
								CreatedAt string `json:"createdAt"`
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
