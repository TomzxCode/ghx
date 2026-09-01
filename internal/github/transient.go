package github

import (
	"fmt"
	"net/http"
	"strings"
)

// TransientError is returned when GitHub responds with a transient server
// error (any 5xx except 501 Not Implemented). These are usually caused by
// temporary problems on GitHub's side or in intermediate proxies and typically
// resolve on retry. Attempts is the number of times the request was tried.
type TransientError struct {
	Status   int
	Message  string
	Attempts int
}

func (e *TransientError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "transient server error"
	}
	if e.Status == 0 {
		// Transport-level failure (connection reset, HTTP/2 stream cancel);
		// there is no HTTP status to report.
		return fmt.Sprintf(
			"GitHub API transient error after %d attempt(s): %s. "+
				"The request will succeed on a re-run once the connection stabilises.",
			e.Attempts, msg,
		)
	}
	return fmt.Sprintf(
		"GitHub API transient error (HTTP %d) after %d attempt(s): %s. "+
			"The request will succeed on a re-run once GitHub recovers.",
		e.Status, e.Attempts, msg,
	)
}

// isTransientStatus reports whether an HTTP status code is a transient server
// error worth retrying. 501 is a permanent "not implemented" and is excluded.
func isTransientStatus(status int) bool {
	return status >= 500 && status != http.StatusNotImplemented
}

// truncateBody collapses whitespace and caps the length of an HTTP error body
// so proxy HTML pages (for example an nginx 502 page) do not flood the
// terminal. Returns "..." as the truncation marker.
func truncateBody(body []byte) string {
	const maxLen = 200
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
