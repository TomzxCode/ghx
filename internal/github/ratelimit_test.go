package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// githubSecondaryRateLimitBody mirrors the exact body GitHub returns for a
// secondary rate limit, so detection is tested against real-world input.
const githubSecondaryRateLimitBody = `{
  "documentation_url": "https://docs.github.com/graphql/overview/rate-limits-and-node-limits-for-the-graphql-api#secondary-rate-limits",
  "message": "You have exceeded a secondary rate limit. Please wait a few minutes before you try again."
}`

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"-5", 0},
		{"2", 2 * time.Second},
		{"90", 90 * time.Second},
		{"not-a-number", 0},
	}
	for _, c := range cases {
		if got := parseRetryAfter(c.in); got != c.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseRateLimit(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		want       bool
		wantRetry  time.Duration
	}{
		{
			name:   "secondary rate limit 403",
			status: http.StatusForbidden,
			body:   githubSecondaryRateLimitBody,
			want:   true,
		},
		{
			name:       "403 with Retry-After header",
			status:     http.StatusForbidden,
			body:       `{"message":"some rate limit"}`,
			retryAfter: "30",
			want:       true,
			wantRetry:  30 * time.Second,
		},
		{
			name:   "429 too many requests",
			status: http.StatusTooManyRequests,
			body:   `{"message":"rate limit exceeded"}`,
			want:   true,
		},
		{
			name:   "plain 403 forbidden is not retryable",
			status: http.StatusForbidden,
			body:   `{"message":"Resource not accessible by personal access token"}`,
			want:   false,
		},
		{
			name:   "200 success is not retryable",
			status: http.StatusOK,
			body:   `{"data":null}`,
			want:   false,
		},
		{
			name:   "500 server error is not retryable",
			status: http.StatusInternalServerError,
			body:   `{"message":"server error"}`,
			want:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: c.status,
				Header:     http.Header{},
			}
			if c.retryAfter != "" {
				resp.Header.Set("Retry-After", c.retryAfter)
			}

			rle, ok := parseRateLimit(resp, []byte(c.body))
			if ok != c.want {
				t.Fatalf("parseRateLimit ok = %v, want %v", ok, c.want)
			}
			if !ok {
				return
			}
			if rle.Status != c.status {
				t.Errorf("Status = %d, want %d", rle.Status, c.status)
			}
			if rle.RetryAfter != c.wantRetry {
				t.Errorf("RetryAfter = %v, want %v", rle.RetryAfter, c.wantRetry)
			}
		})
	}
}

func TestRateLimitError_Message(t *testing.T) {
	err := &RateLimitError{
		Status:   http.StatusForbidden,
		Message:  "You have exceeded a secondary rate limit.",
		Attempts: 4,
	}
	msg := err.Error()
	for _, sub := range []string{"HTTP 403", "4 attempt(s)", "secondary rate limit", "wait a few minutes"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("error message %q missing %q", msg, sub)
		}
	}
}

// rateLimitServer returns a 403 secondary rate limit response for the first
// failN requests, then a successful GraphQL response. It returns the number of
// requests received via the returned counter.
func rateLimitServer(t *testing.T, failN int) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		if int(n) <= failN {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(githubSecondaryRateLimitBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// fastRetryClient returns a client whose backoff and sleep are instant, so
// retry behaviour can be tested without slowing the suite down.
func fastRetryClient(t *testing.T, url string, maxRetries int) *Client {
	t.Helper()
	c, err := NewClientWithURL(url, "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	c.maxRetries = maxRetries
	c.initialBackoff = time.Millisecond
	c.maxBackoff = 5 * time.Millisecond
	c.sleep = func(time.Duration) {}
	return c
}

func TestQuery_RetriesAndSucceeds(t *testing.T) {
	srv, count := rateLimitServer(t, 2)
	c := fastRetryClient(t, srv.URL, 3)

	if err := c.Query("query { root }", nil, nil); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if got := atomic.LoadInt32(count); got != 3 {
		t.Errorf("server received %d requests, want 3 (2 rate-limited + 1 success)", got)
	}
}

func TestQuery_RateLimitExhausted(t *testing.T) {
	srv, count := rateLimitServer(t, 100) // always rate limited
	c := fastRetryClient(t, srv.URL, 2)

	err := c.Query("query { root }", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rle.Attempts != 3 { // 2 retries + 1 initial = 3 attempts
		t.Errorf("Attempts = %d, want 3", rle.Attempts)
	}
	if got := atomic.LoadInt32(count); got != 3 {
		t.Errorf("server received %d requests, want 3", got)
	}
}

func TestQuery_NoRetryOnGenuineForbidden(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	t.Cleanup(srv.Close)

	c := fastRetryClient(t, srv.URL, 3)

	err := c.Query("query { root }", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var rle *RateLimitError
	if errors.As(err, &rle) {
		t.Fatalf("non-rate-limit 403 should not be a RateLimitError, got %v", rle)
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("expected HTTP 403 in error, got %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("server received %d requests, want 1 (no retries)", got)
	}
}

func TestNextBackoff_HonoursRetryAfter(t *testing.T) {
	c := &Client{
		initialBackoff: time.Second,
		maxBackoff:     60 * time.Second,
	}
	rle := &RateLimitError{RetryAfter: 7 * time.Second}
	if got := c.nextBackoff(rle, 0); got != 7*time.Second {
		t.Errorf("nextBackoff with RetryAfter = %v, want 7s", got)
	}

	// Retry-After above maxBackoff is capped.
	rleOver := &RateLimitError{RetryAfter: 5 * time.Minute}
	if got := c.nextBackoff(rleOver, 0); got != 60*time.Second {
		t.Errorf("nextBackoff over max = %v, want 60s", got)
	}
}

func TestNextBackoff_Exponential(t *testing.T) {
	c := &Client{
		initialBackoff: 5 * time.Second,
		maxBackoff:     60 * time.Second,
	}
	plainErr := errors.New("not a rate limit error")
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 5 * time.Second},
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{3, 40 * time.Second},
		{4, 60 * time.Second}, // capped
	}
	for _, tc := range cases {
		if got := c.nextBackoff(plainErr, tc.attempt); got != tc.want {
			t.Errorf("nextBackoff(attempt=%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}
