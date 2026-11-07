package gh

import (
	"encoding/json"
	"fmt"
)

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
