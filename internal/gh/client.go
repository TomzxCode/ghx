package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type gqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

var client = &http.Client{Timeout: 30 * time.Second}

func resolveToken() string {
	for _, env := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := os.Getenv(env); t != "" {
			return t
		}
	}
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t
		}
	}
	return ""
}

func GraphQL(query string, variables map[string]interface{}) (json.RawMessage, error) {
	token := resolveToken()
	if token == "" {
		return nil, fmt.Errorf("no GitHub token found; set GH_TOKEN or GITHUB_TOKEN, or run `gh auth login`")
	}

	body, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.github.com/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ghx/0.1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := parseResponse(resp)
	if err != nil {
		return nil, err
	}

	return respBody, nil
}

func parseResponse(resp *http.Response) (json.RawMessage, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, buf.String())
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(buf.Bytes(), &gqlResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}

	return gqlResp.Data, nil
}

func ResolveRepo(repoFlag string) (owner, name string, err error) {
	if repoFlag != "" {
		parts := strings.SplitN(repoFlag, "/", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid repo format: %s (expected OWNER/REPO)", repoFlag)
		}
		return parts[0], parts[1], nil
	}

	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to determine current repo: %w", err)
	}
	remoteURL := strings.TrimSpace(string(out))

	owner, name = parseRemoteURL(remoteURL)
	if owner == "" || name == "" {
		return "", "", fmt.Errorf("could not parse repo from remote: %s", remoteURL)
	}
	return owner, name, nil
}

func parseRemoteURL(raw string) (owner, name string) {
	u := raw
	u = strings.TrimSuffix(u, ".git")

	if strings.HasPrefix(u, "https://") {
		u = strings.TrimPrefix(u, "https://")
		parts := strings.SplitN(u, "/", 3)
		if len(parts) >= 2 {
			return parts[1], parts[2]
		}
	}

	if strings.HasPrefix(u, "http://") {
		u = strings.TrimPrefix(u, "http://")
		parts := strings.SplitN(u, "/", 3)
		if len(parts) >= 2 {
			return parts[1], parts[2]
		}
	}

	if strings.Contains(u, ":") {
		parts := strings.SplitN(u, ":", 2)
		if len(parts) == 2 {
			pathParts := strings.SplitN(parts[1], "/", 3)
			if len(pathParts) >= 2 {
				return pathParts[0], pathParts[1]
			}
		}
	}

	return "", ""
}
