package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitError is returned when GitHub responds with a rate limit
// (HTTP 429, or a 403 secondary rate limit). RetryAfter is the wait duration
// GitHub advertised via the Retry-After header, or 0 when GitHub did not
// provide one. Attempts is the number of times the request was tried.
type RateLimitError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
	Attempts   int
}

func (e *RateLimitError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "rate limit exceeded"
	}
	suffix := ""
	if e.RetryAfter > 0 {
		suffix = fmt.Sprintf("; GitHub suggests waiting %s", e.RetryAfter)
	}
	return fmt.Sprintf(
		"GitHub API rate limit exceeded (HTTP %d) after %d attempt(s): %s%s. "+
			"Please wait a few minutes and retry.",
		e.Status, e.Attempts, msg, suffix,
	)
}

// rateLimitBody is the JSON shape GitHub returns for rate limit errors.
type rateLimitBody struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url"`
}

// parseRateLimit inspects an HTTP response and decides whether it represents a
// retryable GitHub rate limit. When it does, it returns a *RateLimitError and
// ok=true. A genuine 403 (forbidden, missing scope, private repo) is not
// treated as retryable unless GitHub explicitly signals a rate limit.
func parseRateLimit(resp *http.Response, body []byte) (*RateLimitError, bool) {
	status := resp.StatusCode
	if status != http.StatusTooManyRequests && status != http.StatusForbidden {
		return nil, false
	}

	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	var rl rateLimitBody
	_ = json.Unmarshal(body, &rl)

	lowerMsg := strings.ToLower(rl.Message)
	lowerBody := strings.ToLower(string(body))
	isRateLimit := status == http.StatusTooManyRequests ||
		strings.Contains(lowerMsg, "rate limit") ||
		strings.Contains(lowerBody, "rate limit") ||
		retryAfter > 0
	if !isRateLimit {
		return nil, false
	}

	return &RateLimitError{
		Status:     status,
		Message:    rl.Message,
		RetryAfter: retryAfter,
	}, true
}

// parseRetryAfter parses a Retry-After header value into a duration. Supports
// both the delta-seconds form and the HTTP-date form. Returns 0 when the
// header is absent or cannot be parsed.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
