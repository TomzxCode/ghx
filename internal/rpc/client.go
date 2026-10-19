package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// Client is a JSON-RPC 2.0 client for a ghx RPC server.
type Client struct {
	// URL is the server address, either http://host:port or http://host:port/rpc.
	URL   string
	Token string
	// HTTPClient may be nil, in which case a client with no timeout is used.
	// Long cache refreshes are bounded by the request context instead.
	HTTPClient *http.Client
	nextID     atomic.Int64
}

// NewClient creates a client for url.
func NewClient(url, token string) *Client {
	return &Client{URL: url, Token: token}
}

// endpoint normalizes the configured URL to the /rpc path.
func (c *Client) endpoint() (string, error) {
	raw := strings.TrimSpace(c.URL)
	if raw == "" {
		return "", fmt.Errorf("no RPC server URL configured (use --url or GHX_RPC_URL)")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid RPC server URL %q: %w", raw, err)
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/rpc"
	}
	return u.String(), nil
}

type clientRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

type clientResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
	ID      json.RawMessage `json:"id"`
}

// Call invokes method and returns the raw result. params may be nil.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	endpoint, err := c.endpoint()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(clientRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      c.nextID.Add(1),
	})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("RPC server returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	var decoded clientResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if decoded.Error != nil {
		return nil, decoded.Error
	}
	return decoded.Result, nil
}

// CallInto invokes method and decodes the result into out.
func (c *Client) CallInto(ctx context.Context, method string, params, out any) error {
	raw, err := c.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding %s result: %w", method, err)
	}
	return nil
}

// Discover calls rpc.discover.
func (c *Client) Discover(ctx context.Context) (DiscoverResult, error) {
	var out DiscoverResult
	err := c.CallInto(ctx, "rpc.discover", nil, &out)
	return out, err
}

// Exec runs a CLI command on the server and returns its output and exit code.
func (c *Client) Exec(ctx context.Context, args []string, stdin string) (ExecResult, error) {
	var out ExecResult
	err := c.CallInto(ctx, "cli.run", ExecParams{Args: args, Stdin: stdin}, &out)
	return out, err
}
