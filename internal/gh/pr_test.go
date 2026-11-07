package gh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomzxcode/ghx/internal/github"
)

// withGraphQLServer points the package at a local GraphQL server and restores
// the default client when the test finishes.
func withGraphQLServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(handler)
	client, err := github.NewClientWithURL(srv.URL, "test-token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	SetClient(client)
	t.Cleanup(func() {
		SetClient(nil)
		srv.Close()
	})
	return srv
}

func TestSetThreadResolved(t *testing.T) {
	tests := []struct {
		name     string
		call     func(string) error
		mutation string
		resolved bool
	}{
		{
			name:     "resolve",
			call:     ResolveThread,
			mutation: "resolveReviewThread",
			resolved: true,
		},
		{
			name:     "unresolve",
			call:     UnresolveThread,
			mutation: "unresolveReviewThread",
			resolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threadID := "PRRT_kwDOtest"

			withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Query     string                 `json:"query"`
					Variables map[string]interface{} `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if !strings.Contains(req.Query, tt.mutation+"(input: {threadId: $threadId})") {
					t.Errorf("query does not call %s: %s", tt.mutation, req.Query)
				}
				if got := req.Variables["threadId"]; got != threadID {
					t.Errorf("threadId = %v, want %s", got, threadID)
				}

				body, _ := json.Marshal(map[string]interface{}{
					"data": map[string]interface{}{
						tt.mutation: map[string]interface{}{
							"thread": map[string]interface{}{
								"id":         threadID,
								"isResolved": tt.resolved,
							},
						},
					},
				})
				w.Header().Set("Content-Type", "application/json")
				w.Write(body)
			})

			if err := tt.call(threadID); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
		})
	}
}

func TestSetThreadResolvedMissingThread(t *testing.T) {
	withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"resolveReviewThread":{"thread":null}}}`))
	})

	if err := ResolveThread("PRRT_missing"); err == nil {
		t.Fatal("expected an error when the thread is missing")
	}
}

func TestMutationRetriesTransientError(t *testing.T) {
	attempts := 0
	withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"addComment":{"commentEdge":{"node":{"id":"IC_retry"}}}}}`))
	})

	id, err := AddTopLevelComment("PR_1", "hello")
	if err != nil {
		t.Fatalf("AddTopLevelComment: %v", err)
	}
	if id != "IC_retry" {
		t.Errorf("comment id = %q, want IC_retry", id)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestSetThreadResolvedGraphQLError(t *testing.T) {
	withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"errors":[{"message":"Could not resolve to a node with the global id"}]}`))
	})

	err := UnresolveThread("PRRT_bad")
	if err == nil || !strings.Contains(err.Error(), "Could not resolve to a node") {
		t.Fatalf("unexpected error: %v", err)
	}
}
