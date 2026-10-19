package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomzxcode/ghx/internal/cache"
)

func newTestServer(t *testing.T, token string) *Server {
	t.Helper()
	store := cache.NewStoreWithPath(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	return NewServer(Config{
		Store:   store,
		Version: "test",
		Token:   token,
	})
}

func postRPC(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestMethodNotFound(t *testing.T) {
	srv := newTestServer(t, "")
	rec := postRPC(t, srv, `{"jsonrpc":"2.0","id":1,"method":"nope","params":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("expected method not found, got %+v", resp)
	}
	if string(resp.ID) != "1" {
		t.Fatalf("id = %s", resp.ID)
	}
}

func TestParseError(t *testing.T) {
	srv := newTestServer(t, "")
	rec := postRPC(t, srv, `{not json`)
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeParseError {
		t.Fatalf("expected parse error, got %+v", resp)
	}
}

func TestWrongVersionIsInvalidRequest(t *testing.T) {
	srv := newTestServer(t, "")
	rec := postRPC(t, srv, `{"jsonrpc":"1.0","id":1,"method":"rpc.discover"}`)
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("expected invalid request, got %+v", resp)
	}
}

func TestNotificationProducesNoContent(t *testing.T) {
	srv := newTestServer(t, "")
	rec := postRPC(t, srv, `{"jsonrpc":"2.0","method":"rpc.discover"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestBatch(t *testing.T) {
	srv := newTestServer(t, "")
	rec := postRPC(t, srv, `[
		{"jsonrpc":"2.0","id":1,"method":"rpc.discover"},
		{"jsonrpc":"2.0","method":"rpc.discover"},
		{"jsonrpc":"2.0","id":2,"method":"nope"}
	]`)
	var resp []Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("responses = %d, want 2 (notification omitted)", len(resp))
	}
	if resp[0].Error != nil || resp[1].Error == nil {
		t.Fatalf("unexpected responses: %+v", resp)
	}
}

func TestEmptyBatchIsInvalid(t *testing.T) {
	srv := newTestServer(t, "")
	rec := postRPC(t, srv, `[]`)
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("expected invalid request, got %+v", resp)
	}
}

func TestAuth(t *testing.T) {
	srv := newTestServer(t, "secret")

	req := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"rpc.discover"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"rpc.discover"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDiscoverAndInfo(t *testing.T) {
	srv := newTestServer(t, "")
	client := NewClient("", "")
	_ = client

	rec := postRPC(t, srv, `{"jsonrpc":"2.0","id":1,"method":"rpc.discover"}`)
	var discover struct {
		Result DiscoverResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &discover); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if discover.Result.Version != "test" {
		t.Fatalf("version = %q", discover.Result.Version)
	}
	names := map[string]bool{}
	for _, m := range discover.Result.Methods {
		names[m.Name] = true
	}
	for _, want := range []string{"server.info", "repo.list", "issue.list", "issue.view", "pr.list", "pr.view", "pr.threads", "cache.refresh"} {
		if !names[want] {
			t.Errorf("method %q missing", want)
		}
	}
	if names["cli.run"] {
		t.Errorf("cli.run should be disabled")
	}
}

func TestExecDisabledByDefault(t *testing.T) {
	srv := newTestServer(t, "")
	rec := postRPC(t, srv, `{"jsonrpc":"2.0","id":1,"method":"cli.run","params":{"args":["version"]}}`)
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("expected method not found, got %+v", resp)
	}
}

func TestExec(t *testing.T) {
	store := cache.NewStoreWithPath(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	srv := NewServer(Config{
		Store:     store,
		AllowExec: true,
		Exec:      ExecConfig{Binary: "/bin/sh", BaseArgs: []string{"-c"}},
	})

	rec := postRPC(t, srv, `{"jsonrpc":"2.0","id":1,"method":"cli.run","params":{"args":["printf hello; exit 3"]}}`)
	var resp struct {
		Result ExecResult `json:"result"`
		Error  *Error     `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	if resp.Result.Stdout != "hello" {
		t.Fatalf("stdout = %q", resp.Result.Stdout)
	}
	if resp.Result.ExitCode != 3 {
		t.Fatalf("exit = %d", resp.Result.ExitCode)
	}
}

func TestClientRoundTrip(t *testing.T) {
	srv := newTestServer(t, "")
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL, "")
	info, err := client.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if info.Name != "ghx" {
		t.Fatalf("name = %q", info.Name)
	}

	if _, err := client.Call(context.Background(), "nope", nil); err == nil {
		t.Fatal("expected method not found error")
	}
}
