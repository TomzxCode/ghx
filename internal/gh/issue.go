package gh

import (
	"encoding/json"
	"fmt"
)

type Issue struct {
	Title  string
	Body   string
	Author string
	State  string
}

type IssueComment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

func GetIssueNodeID(owner, name string, number int) (string, error) {
	query := `
	query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			issue(number: $number) {
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
			Issue struct {
				ID string `json:"id"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.Repository.Issue.ID, nil
}

func GetIssue(owner, name string, number int) (*Issue, []IssueComment, error) {
	query := `
	query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			issue(number: $number) {
				title
				body
				author { login }
				state
				comments(first: 100) {
					nodes {
						id
						body
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
		return nil, nil, err
	}

	var result struct {
		Repository struct {
			Issue struct {
				Title  string `json:"title"`
				Body   string `json:"body"`
				Author struct {
					Login string `json:"login"`
				} `json:"author"`
				State    string `json:"state"`
				Comments struct {
					Nodes []struct {
						ID        string `json:"id"`
						Body      string `json:"body"`
						Author    struct {
							Login string `json:"login"`
						} `json:"author"`
						CreatedAt string `json:"createdAt"`
					} `json:"nodes"`
				} `json:"comments"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	issue := &Issue{
		Title:  result.Repository.Issue.Title,
		Body:   result.Repository.Issue.Body,
		Author: result.Repository.Issue.Author.Login,
		State:  result.Repository.Issue.State,
	}

	comments := make([]IssueComment, 0, len(result.Repository.Issue.Comments.Nodes))
	for _, c := range result.Repository.Issue.Comments.Nodes {
		comments = append(comments, IssueComment{
			ID:        c.ID,
			Body:      c.Body,
			Author:    c.Author.Login,
			CreatedAt: c.CreatedAt,
		})
	}

	return issue, comments, nil
}

func ListIssueComments(owner, name string, number int) ([]IssueComment, error) {
	query := `
	query($owner: String!, $name: String!, $number: Int!) {
		repository(owner: $owner, name: $name) {
			issue(number: $number) {
				comments(first: 100) {
					nodes {
						id
						body
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
			Issue struct {
				Comments struct {
					Nodes []struct {
						ID        string `json:"id"`
						Body      string `json:"body"`
						Author    struct {
							Login string `json:"login"`
						} `json:"author"`
						CreatedAt string `json:"createdAt"`
					} `json:"nodes"`
				} `json:"comments"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	comments := make([]IssueComment, 0, len(result.Repository.Issue.Comments.Nodes))
	for _, c := range result.Repository.Issue.Comments.Nodes {
		comments = append(comments, IssueComment{
			ID:        c.ID,
			Body:      c.Body,
			Author:    c.Author.Login,
			CreatedAt: c.CreatedAt,
		})
	}

	return comments, nil
}
