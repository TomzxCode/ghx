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
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Retry behaviour used when a sane value is not explicitly configured.
// defaultMaxRetries is the number of additional attempts after the first, so
// the default is 3 total attempts.
const (
	defaultMaxRetries     = 2
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
	lastRemaining  atomic.Int64 // x-ratelimit-remaining of the last response, -1 = unknown
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
	c.lastRemaining.Store(-1)
	return c
}

// SetRetries overrides how many additional attempts are made after a failed
// initial request. 0 means a single attempt with no retries. Negative values
// are clamped to 0. Only use this before the first request.
func (c *Client) SetRetries(maxRetries int) {
	if maxRetries < 0 {
		maxRetries = 0
	}
	c.maxRetries = maxRetries
}

// RateLimitRemaining reports the x-ratelimit-remaining value from the most
// recent GraphQL response, or -1 when no response has been received yet. It
// is safe for concurrent use.
func (c *Client) RateLimitRemaining() int {
	return int(c.lastRemaining.Load())
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
// It retries transient failures using exponential backoff: GitHub rate limit
// responses (HTTP 429 and secondary rate limit 403s, honouring the Retry-After
// header) and transient server errors (5xx, for example an HTML 502 from a
// proxy). Other errors are returned immediately.
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
			fmt.Fprintf(os.Stderr, "%s, retrying in %s (attempt %d of %d)...\n", retryReason(lastErr), wait, attempt+1, maxAttempts)
			c.sleep(wait)
		}

		lastErr = c.queryOnce(body, result)
		if lastErr == nil {
			return nil
		}

		if !retryable(lastErr) {
			return lastErr // not retryable
		}
	}

	var rle *RateLimitError
	if errors.As(lastErr, &rle) {
		rle.Attempts = maxAttempts
	}
	var te *TransientError
	if errors.As(lastErr, &te) {
		te.Attempts = maxAttempts
	}
	return lastErr
}

// retryable reports whether an error from queryOnce should trigger another
// attempt: rate limits and transient server errors.
func retryable(err error) bool {
	var rle *RateLimitError
	var te *TransientError
	return errors.As(err, &rle) || errors.As(err, &te)
}

// retryReason describes the failure class for the retry log line.
func retryReason(err error) string {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return "GitHub rate limit hit"
	}
	return "transient GitHub error"
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
		// Transport failures (DNS, connection reset, HTTP/2 stream cancel)
		// are transient: retry with backoff like server-side 5xx.
		return &TransientError{Message: "executing request: " + err.Error()}
	}
	defer resp.Body.Close()

	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.lastRemaining.Store(int64(n))
		}
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TransientError{Message: "reading response body: " + err.Error()}
	}

	if rle, ok := parseRateLimit(resp, respBody); ok {
		return rle
	}

	if resp.StatusCode != http.StatusOK {
		body := truncateBody(respBody)
		if isTransientStatus(resp.StatusCode) {
			return &TransientError{Status: resp.StatusCode, Message: body}
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) && syn.Error() == "unexpected end of JSON input" {
			// Empty or truncated body: the request succeeded but the
			// response was cut off mid-stream, the flaky-edge variant of a
			// transient failure. Retry it like a 5xx.
			return &TransientError{Message: "parsing GraphQL response: " + err.Error()}
		}
		// A complete body that is not valid JSON is deterministic (broken
		// proxy, encoding bug): fail fast so it gets diagnosed.
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
		// The envelope parsed, so the body is complete; a data parse failure
		// here is deterministic, not transient.
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
