package gh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withGraphQLServer points the package at a local GraphQL server and restores
// the real endpoint when the test finishes.
func withGraphQLServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	t.Setenv("GH_TOKEN", "test-token")

	srv := httptest.NewServer(handler)
	old := graphqlEndpoint
	graphqlEndpoint = srv.URL
	t.Cleanup(func() {
		graphqlEndpoint = old
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
				var req gqlRequest
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
