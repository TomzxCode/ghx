// Package rpc implements a JSON-RPC 2.0 server over HTTP for the ghx CLI.
//
// The server exposes typed methods for the read and cache operations (issue
// list/view, PR list/view, review threads, cached repositories, cache refresh)
// and an optional cli.run method that executes any ghx command in a child
// process. Clients can therefore use structured results for the common reads
// and still reach every command through one protocol.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/logging"
)

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// CodeApplication is used for errors raised by a method implementation.
	CodeApplication = -32000
)

// Request is a JSON-RPC 2.0 request. ID is nil for a notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// Error is a JSON-RPC error object and also implements the error interface so
// method handlers can return one directly.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements error.
func (e *Error) Error() string {
	return e.Message
}

// Errorf builds an *Error with the given code.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// maxBodyBytes bounds a request body to keep a malformed client from
// exhausting server memory.
const maxBodyBytes = 16 << 20

// ServeHTTP handles one JSON-RPC request, or a batch of them. Notifications
// (requests without an id) are executed but produce no response body.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		logging.Info("rpc request", "method", "", "status", "method_not_allowed", "error", "JSON-RPC requests must use POST", "duration", time.Since(start))
		http.Error(w, "JSON-RPC requests must use POST", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="ghx"`)
		logging.Info("rpc request", "method", "", "status", "unauthorized", "error", "unauthorized", "duration", time.Since(start))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		logging.Info("rpc request", "method", "", "status", "parse_error", "error", err.Error(), "duration", time.Since(start))
		writeResponse(w, errorResponse(nil, CodeParseError, "reading request body: %v", err))
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		logging.Info("rpc request", "method", "", "status", "parse_error", "error", "empty request body", "duration", time.Since(start))
		writeResponse(w, errorResponse(nil, CodeParseError, "empty request body"))
		return
	}

	if body[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil {
			writeResponse(w, errorResponse(nil, CodeParseError, "invalid JSON: %v", err))
			return
		}
		if len(batch) == 0 {
			writeResponse(w, errorResponse(nil, CodeInvalidRequest, "empty batch"))
			return
		}
		responses := make([]*Response, 0, len(batch))
		for _, item := range batch {
			resp := s.handleRequest(r.Context(), item)
			if resp != nil {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, responses)
		return
	}

	resp := s.handleRequest(r.Context(), body)
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeResponse(w, resp)
}

// handleRequest decodes and executes one request. It returns nil for a
// notification.
func (s *Server) handleRequest(ctx context.Context, raw json.RawMessage) *Response {
	start := time.Now()
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		logging.Info("rpc request", "method", "", "params", string(raw), "status", "parse_error", "error", err.Error(), "duration", time.Since(start))
		return errorResponse(nil, CodeParseError, "invalid JSON: %v", err)
	}
	if req.JSONRPC != "2.0" {
		logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "invalid_request", "error", `jsonrpc must be "2.0"`, "duration", time.Since(start))
		return errorResponse(req.ID, CodeInvalidRequest, `jsonrpc must be "2.0"`)
	}
	if strings.TrimSpace(req.Method) == "" {
		logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "invalid_request", "error", "method is required", "duration", time.Since(start))
		return errorResponse(req.ID, CodeInvalidRequest, "method is required")
	}

	method, ok := s.methods[req.Method]
	if !ok {
		if isNotification(req.ID) {
			logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "method_not_found", "duration", time.Since(start))
			return nil
		}
		logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "method_not_found", "duration", time.Since(start))
		return errorResponse(req.ID, CodeMethodNotFound, "method %q not found", req.Method)
	}

	result, err := method.handler(ctx, req.Params)
	if isNotification(req.ID) {
		logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "ok", "duration", time.Since(start))
		return nil
	}
	if err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "error", "error", rpcErr.Message, "duration", time.Since(start))
			return &Response{JSONRPC: "2.0", ID: normalizeID(req.ID), Error: rpcErr}
		}
		logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "error", "error", err.Error(), "duration", time.Since(start))
		return errorResponse(req.ID, CodeApplication, "%s", err.Error())
	}
	logging.Info("rpc request", "method", req.Method, "params", string(req.Params), "status", "ok", "duration", time.Since(start))
	_ = result
	return &Response{JSONRPC: "2.0", ID: normalizeID(req.ID), Result: result}
}

// isNotification reports whether the request omitted its id. A present null id
// is treated as a normal request, as the specification recommends replying to
// it.
func isNotification(id json.RawMessage) bool {
	return id == nil
}

// normalizeID makes sure every response carries an id field, using null when
// the request id is absent.
func normalizeID(id json.RawMessage) json.RawMessage {
	if id == nil {
		return json.RawMessage("null")
	}
	return id
}

func errorResponse(id json.RawMessage, code int, format string, args ...any) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      normalizeID(id),
		Error:   Errorf(code, format, args...),
	}
}

func writeResponse(w http.ResponseWriter, resp *Response) {
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
