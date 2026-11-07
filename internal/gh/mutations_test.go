package gh

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type testGraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func decodeTestRequest(t *testing.T, r *http.Request) testGraphQLRequest {
	t.Helper()

	var req testGraphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return req
}

func writeTestData(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":` + data + `}`))
}

func requireVariable(t *testing.T, vars map[string]interface{}, key string, want interface{}) {
	t.Helper()

	if got := vars[key]; got != want {
		t.Errorf("variables[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}

func TestAddTopLevelCommentRequest(t *testing.T) {
	withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		req := decodeTestRequest(t, r)
		if !strings.Contains(req.Query, "addComment(input: {subjectId: $subjectId, body: $body})") {
			t.Errorf("query does not call addComment: %s", req.Query)
		}
		requireVariable(t, req.Variables, "subjectId", "PR_1")
		requireVariable(t, req.Variables, "body", "hello")

		writeTestData(w, `{"addComment":{"commentEdge":{"node":{"id":"IC_1"}}}}`)
	})

	id, err := AddTopLevelComment("PR_1", "hello")
	if err != nil {
		t.Fatalf("AddTopLevelComment: %v", err)
	}
	if id != "IC_1" {
		t.Errorf("comment id = %q, want IC_1", id)
	}
}

func TestAddReviewThreadRequest(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		line, startLine := 12, 10
		withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
			req := decodeTestRequest(t, r)
			if !strings.Contains(req.Query, "addPullRequestReviewThread(input: {") {
				t.Errorf("query does not call addPullRequestReviewThread: %s", req.Query)
			}
			requireVariable(t, req.Variables, "pullRequestId", "PR_1")
			requireVariable(t, req.Variables, "pullRequestReviewId", "REV_1")
			requireVariable(t, req.Variables, "body", "hello")
			requireVariable(t, req.Variables, "path", "a.go")
			requireVariable(t, req.Variables, "line", float64(12))
			requireVariable(t, req.Variables, "startLine", float64(10))
			requireVariable(t, req.Variables, "side", "RIGHT")
			requireVariable(t, req.Variables, "subjectType", "FILE")

			writeTestData(w, `{"addPullRequestReviewThread":{"thread":{"id":"PRRT_1","path":"a.go"}}}`)
		})

		id, err := AddReviewThread("PR_1", "REV_1", "hello", "a.go", &line, &startLine, "RIGHT", "FILE")
		if err != nil {
			t.Fatalf("AddReviewThread: %v", err)
		}
		if id != "PRRT_1" {
			t.Errorf("thread id = %q, want PRRT_1", id)
		}
	})

	t.Run("required only", func(t *testing.T) {
		withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
			req := decodeTestRequest(t, r)
			requireVariable(t, req.Variables, "pullRequestId", "PR_1")
			requireVariable(t, req.Variables, "body", "hello")
			for _, key := range []string{"pullRequestReviewId", "path", "line", "startLine", "side", "subjectType"} {
				requireVariable(t, req.Variables, key, nil)
			}

			writeTestData(w, `{"addPullRequestReviewThread":{"thread":{"id":"PRRT_2"}}}`)
		})

		if _, err := AddReviewThread("PR_1", "", "hello", "", nil, nil, "", ""); err != nil {
			t.Fatalf("AddReviewThread: %v", err)
		}
	})
}

func TestReplyToThreadRequest(t *testing.T) {
	t.Run("with review", func(t *testing.T) {
		withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
			req := decodeTestRequest(t, r)
			if !strings.Contains(req.Query, "addPullRequestReviewThreadReply(input: {") {
				t.Errorf("query does not call addPullRequestReviewThreadReply: %s", req.Query)
			}
			requireVariable(t, req.Variables, "pullRequestReviewThreadId", "PRRT_1")
			requireVariable(t, req.Variables, "pullRequestReviewId", "REV_1")
			requireVariable(t, req.Variables, "body", "reply")

			writeTestData(w, `{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_1"}}}`)
		})

		id, err := ReplyToThread("PRRT_1", "REV_1", "reply")
		if err != nil {
			t.Fatalf("ReplyToThread: %v", err)
		}
		if id != "PRRC_1" {
			t.Errorf("comment id = %q, want PRRC_1", id)
		}
	})

	t.Run("without review", func(t *testing.T) {
		withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
			req := decodeTestRequest(t, r)
			requireVariable(t, req.Variables, "pullRequestReviewId", nil)
			writeTestData(w, `{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_2"}}}`)
		})

		if _, err := ReplyToThread("PRRT_1", "", "reply"); err != nil {
			t.Fatalf("ReplyToThread: %v", err)
		}
	})
}

func TestSubmitReviewRequest(t *testing.T) {
	t.Run("with body", func(t *testing.T) {
		withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
			req := decodeTestRequest(t, r)
			if !strings.Contains(req.Query, "submitPullRequestReview(input: {") {
				t.Errorf("query does not call submitPullRequestReview: %s", req.Query)
			}
			requireVariable(t, req.Variables, "pullRequestReviewId", "REV_1")
			requireVariable(t, req.Variables, "event", "APPROVE")
			requireVariable(t, req.Variables, "body", "LGTM")

			writeTestData(w, `{"submitPullRequestReview":{"pullRequestReview":{"id":"REV_1"}}}`)
		})

		if err := SubmitReview("REV_1", "APPROVE", "LGTM"); err != nil {
			t.Fatalf("SubmitReview: %v", err)
		}
	})

	t.Run("without body", func(t *testing.T) {
		withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
			req := decodeTestRequest(t, r)
			requireVariable(t, req.Variables, "body", nil)
			writeTestData(w, `{"submitPullRequestReview":{"pullRequestReview":{"id":"REV_1"}}}`)
		})

		if err := SubmitReview("REV_1", "APPROVE", ""); err != nil {
			t.Fatalf("SubmitReview: %v", err)
		}
	})
}

func TestMutationGraphQLError(t *testing.T) {
	withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Field is invalid"}]}`))
	})

	_, err := AddTopLevelComment("PR_1", "hello")
	if err == nil || !strings.Contains(err.Error(), "Field is invalid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMutationHTTPError(t *testing.T) {
	withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unprocessable entity", http.StatusUnprocessableEntity)
	})

	_, err := AddTopLevelComment("PR_1", "hello")
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("unexpected error: %v", err)
	}
}
