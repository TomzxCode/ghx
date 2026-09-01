package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// transientServer returns an HTML 502 Bad Gateway body (as proxies emit) for
// the first failN requests, then a successful GraphQL response. The request
// counter is returned so tests can assert how many attempts were made.
func transientServer(t *testing.T, failN int) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		if int(n) <= failN {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>\n<head><title>502 Bad Gateway</title></head>\n<body>\n<center><h1>502 Bad Gateway</h1></center>\n<hr><center>nginx</center>\n</body>\n</html>\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func TestQuery_RetriesTransient502AndSucceeds(t *testing.T) {
	srv, count := transientServer(t, 2)
	c := fastRetryClient(t, srv.URL, 3)

	if err := c.Query("query { root }", nil, nil); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if got := atomic.LoadInt32(count); got != 3 {
		t.Errorf("server received %d requests, want 3 (2 transient failures + 1 success)", got)
	}
}

func TestQuery_TransientExhausted(t *testing.T) {
	srv, count := transientServer(t, 100) // always 502
	c := fastRetryClient(t, srv.URL, 2)

	err := c.Query("query { root }", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var te *TransientError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TransientError, got %T: %v", err, err)
	}
	if te.Attempts != 3 { // 2 retries + 1 initial = 3 attempts
		t.Errorf("Attempts = %d, want 3", te.Attempts)
	}
	if te.Status != http.StatusBadGateway {
		t.Errorf("Status = %d, want 502", te.Status)
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("expected HTTP 502 in error, got %v", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("error message should collapse newlines, got %q", err.Error())
	}
	if got := atomic.LoadInt32(count); got != 3 {
		t.Errorf("server received %d requests, want 3", got)
	}
}

func TestQuery_NoRetryOn404(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	t.Cleanup(srv.Close)

	c := fastRetryClient(t, srv.URL, 3)
	err := c.Query("query { root }", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var te *TransientError
	if errors.As(err, &te) {
		t.Fatalf("404 should not be a TransientError, got %v", te)
	}
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("server received %d requests, want 1 (no retries)", got)
	}
}

func TestIsTransientStatus(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{522, true}, // Cloudflare origin timeout
		{http.StatusNotImplemented, false},
		{http.StatusTooManyRequests, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusOK, false},
	}
	for _, c := range cases {
		if got := isTransientStatus(c.status); got != c.want {
			t.Errorf("isTransientStatus(%d) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestTruncateBody(t *testing.T) {
	long := strings.Repeat("<html>\n<head><title>502 Bad Gateway</title></head>\n", 10)
	got := truncateBody([]byte(long))
	if len(got) > 200+len("...") {
		t.Errorf("truncated body too long: %d chars", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("expected whitespace collapsed, got %q", got)
	}

	if got := truncateBody([]byte(`{"message":"Not Found"}`)); got != `{"message":"Not Found"}` {
		t.Errorf("short body should pass through unchanged, got %q", got)
	}
	if got := truncateBody(nil); got != "" {
		t.Errorf("empty body should stay empty, got %q", got)
	}
}

func TestTransientError_Message(t *testing.T) {
	err := &TransientError{Status: 502, Message: "502 Bad Gateway", Attempts: 4}
	msg := err.Error()
	for _, sub := range []string{"HTTP 502", "4 attempt(s)", "502 Bad Gateway", "re-run"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("error message %q missing %q", msg, sub)
		}
	}

	empty := &TransientError{Status: 503, Attempts: 1}
	if !strings.Contains(empty.Error(), "transient server error") {
		t.Errorf("empty message should fall back, got %q", empty.Error())
	}
}

// TestRateLimitRemaining_HeaderCapture verifies the client records
// x-ratelimit-remaining from each response, starting from -1 (unknown).
func TestRateLimitRemaining_HeaderCapture(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", 4999-n))
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	t.Cleanup(srv.Close)

	c := fastRetryClient(t, srv.URL, 0)
	if got := c.RateLimitRemaining(); got != -1 {
		t.Errorf("initial RateLimitRemaining = %d, want -1", got)
	}
	if err := c.Query("query { root }", nil, nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := c.RateLimitRemaining(); got != 4998 {
		t.Errorf("RateLimitRemaining = %d, want 4998", got)
	}
	if err := c.Query("query { root }", nil, nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := c.RateLimitRemaining(); got != 4997 {
		t.Errorf("RateLimitRemaining = %d, want 4997 (updated per response)", got)
	}
}

// TestSetRetries verifies retry configuration: 0 disables retries and
// negatives clamp to 0.
func TestSetRetries(t *testing.T) {
	c := fastRetryClient(t, "http://127.0.0.1:1", 3) // unreachable host, retries irrelevant
	c.SetRetries(0)
	if c.maxRetries != 0 {
		t.Errorf("SetRetries(0): maxRetries = %d, want 0", c.maxRetries)
	}
	c.SetRetries(-5)
	if c.maxRetries != 0 {
		t.Errorf("SetRetries(-5): maxRetries = %d, want 0 (clamped)", c.maxRetries)
	}
	c.SetRetries(4)
	if c.maxRetries != 4 {
		t.Errorf("SetRetries(4): maxRetries = %d, want 4", c.maxRetries)
	}
}

// TestQuery_RetriesTruncatedBody verifies a 200 with an empty or truncated
// JSON body is retried as a transient failure instead of failing immediately.
func TestQuery_RetriesTruncatedBody(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if atomic.AddInt32(&count, 1) == 1 {
			return // empty body
		}
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	t.Cleanup(srv.Close)

	c := fastRetryClient(t, srv.URL, 3)
	if err := c.Query("query { root }", nil, nil); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Errorf("server received %d requests, want 2 (1 truncated + 1 success)", got)
	}
}

// TestQuery_NoRetryOnGarbageBody verifies a complete body that simply is not
// valid JSON fails fast as a hard error: it is deterministic, not transient.
func TestQuery_NoRetryOnGarbageBody(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`hello, this is not json`))
	}))
	t.Cleanup(srv.Close)

	c := fastRetryClient(t, srv.URL, 3)
	err := c.Query("query { root }", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var te *TransientError
	if errors.As(err, &te) {
		t.Fatalf("garbage body must not be a TransientError, got %v", te)
	}
	if !strings.Contains(err.Error(), "parsing GraphQL response") {
		t.Errorf("expected parse error message, got %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("server received %d requests, want 1 (no retries)", got)
	}
}

// TestQuery_RetriesTransportError verifies mid-body transport failures (for
// example an HTTP/2 stream cancel) are retried as transient errors.
func TestQuery_RetriesTransportError(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&count, 1) == 1 {
			// Send headers and abort mid-body, leaving the client with a
			// truncated response.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{`))
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	t.Cleanup(srv.Close)

	c := fastRetryClient(t, srv.URL, 3)
	if err := c.Query("query { root }", nil, nil); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Errorf("server received %d requests, want 2 (1 aborted + 1 success)", got)
	}
}

// TestQuery_TransientBackoffIsExponential verifies a 502 has no Retry-After,
// so nextBackoff falls through to the exponential branch.
func TestQuery_TransientBackoffIsExponential(t *testing.T) {
	var sleeps []time.Duration
	c := &Client{
		initialBackoff: time.Second,
		maxBackoff:     60 * time.Second,
		sleep:          func(d time.Duration) { sleeps = append(sleeps, d) },
	}
	err := &TransientError{Status: 502}
	if got := c.nextBackoff(err, 0); got != time.Second {
		t.Errorf("first backoff = %v, want 1s", got)
	}
	if got := c.nextBackoff(err, 2); got != 4*time.Second {
		t.Errorf("third backoff = %v, want 4s", got)
	}
	_ = sleeps
}
