package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Retry behaviour used when a sane value is not explicitly configured.
const (
	defaultMaxRetries     = 3
	defaultInitialBackoff = 10 * time.Second
	defaultMaxBackoff     = 60 * time.Second
)

// Client is a minimal GitHub GraphQL client.
type Client struct {
	token          string
	host           string
	endpointURL    string
	httpClient     *http.Client
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	sleep          func(time.Duration)
}

type gqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type gqlError struct {
	Message string `json:"message"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

// NewClient creates a client for the given GitHub host (e.g. "github.com").
func NewClient(host string) (*Client, error) {
	token := resolveToken(host)
	if token == "" {
		return nil, fmt.Errorf("no GitHub token found; set GH_TOKEN or GITHUB_TOKEN, or run `gh auth login`")
	}
	return (&Client{
		token:      token,
		host:       host,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}).withDefaults(), nil
}

// NewClientWithURL creates a client that points at a custom GraphQL endpoint URL.
// The host is used for cache directory separation and defaults to the URL host.
// This is useful for testing against a mock server.
func NewClientWithURL(endpointURL, token, host string) (*Client, error) {
	if host == "" {
		host = "mock"
	}
	return (&Client{
		token:       token,
		host:        host,
		endpointURL: endpointURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}).withDefaults(), nil
}

// withDefaults populates retry/backoff fields with sensible defaults when the
// caller has not set them. It is a no-op for fields already configured.
func (c *Client) withDefaults() *Client {
	if c.maxRetries == 0 {
		c.maxRetries = defaultMaxRetries
	}
	if c.initialBackoff == 0 {
		c.initialBackoff = defaultInitialBackoff
	}
	if c.maxBackoff == 0 {
		c.maxBackoff = defaultMaxBackoff
	}
	if c.sleep == nil {
		c.sleep = time.Sleep
	}
	return c
}

func resolveToken(host string) string {
	for _, env := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := os.Getenv(env); t != "" {
			return t
		}
	}
	// Try gh CLI as fallback.
	if out, err := exec.Command("gh", "auth", "token", "--hostname", host).Output(); err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t
		}
	}
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func (c *Client) endpoint() string {
	if c.endpointURL != "" {
		return c.endpointURL
	}
	if c.host == "github.com" {
		return "https://api.github.com/graphql"
	}
	return fmt.Sprintf("https://%s/api/graphql", c.host)
}

// Query executes a GraphQL query and unmarshals the data field into result.
// It retries transient GitHub rate limit responses (HTTP 429 and secondary
// rate limit 403s) using exponential backoff, honouring GitHub's Retry-After
// header when present. Other errors are returned immediately.
func (c *Client) Query(query string, variables map[string]interface{}, result interface{}) error {
	body, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	maxAttempts := c.maxRetries + 1
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			wait := c.nextBackoff(lastErr, attempt-1)
			fmt.Fprintf(os.Stderr, "gh-cached: GitHub rate limit hit, retrying in %s (attempt %d of %d)...\n", wait, attempt+1, maxAttempts)
			c.sleep(wait)
		}

		lastErr = c.queryOnce(body, result)
		if lastErr == nil {
			return nil
		}

		var rle *RateLimitError
		if !errors.As(lastErr, &rle) {
			return lastErr // not retryable
		}
	}

	var rle *RateLimitError
	if errors.As(lastErr, &rle) {
		rle.Attempts = maxAttempts
	}
	return lastErr
}

// queryOnce performs a single GraphQL request attempt and parses the response.
func (c *Client) queryOnce(body []byte, result interface{}) error {
	req, err := http.NewRequest("POST", c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ghx/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if rle, ok := parseRateLimit(resp, respBody); ok {
		return rle
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("parsing GraphQL response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("GraphQL errors: %s", strings.Join(msgs, "; "))
	}

	if result != nil {
		if err := json.Unmarshal(gqlResp.Data, result); err != nil {
			return fmt.Errorf("parsing response data: %w", err)
		}
	}
	return nil
}

// nextBackoff computes the wait before the next attempt. When the previous
// error carried a Retry-After value, that is used (capped at maxBackoff);
// otherwise exponential backoff is applied (initialBackoff * 2^attempt).
func (c *Client) nextBackoff(err error, attempt int) time.Duration {
	var rle *RateLimitError
	if errors.As(err, &rle) && rle.RetryAfter > 0 {
		if rle.RetryAfter > c.maxBackoff {
			return c.maxBackoff
		}
		return rle.RetryAfter
	}
	wait := c.initialBackoff << attempt
	if wait <= 0 || wait > c.maxBackoff {
		return c.maxBackoff
	}
	return wait
}
