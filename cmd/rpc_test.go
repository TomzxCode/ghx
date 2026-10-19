package cmd

import (
	"reflect"
	"testing"
)

func TestTakeRemoteArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantURL   string
		wantToken string
		wantRest  []string
		wantOK    bool
	}{
		{
			name:     "no remote flag",
			args:     []string{"issue", "list", "--state", "open"},
			wantOK:   false,
			wantRest: nil,
		},
		{
			name:     "remote with separate URL",
			args:     []string{"--remote", "http://host:9000", "issue", "list"},
			wantURL:  "http://host:9000",
			wantRest: []string{"issue", "list"},
			wantOK:   true,
		},
		{
			name:     "remote with equals URL after the command",
			args:     []string{"issue", "list", "--remote=http://host:9000"},
			wantURL:  "http://host:9000",
			wantRest: []string{"issue", "list"},
			wantOK:   true,
		},
		{
			name:      "remote and token",
			args:      []string{"--remote", "http://host:9000", "--rpc-token", "secret", "repo", "list"},
			wantURL:   "http://host:9000",
			wantToken: "secret",
			wantRest:  []string{"repo", "list"},
			wantOK:    true,
		},
		{
			name:     "token without remote is left alone",
			args:     []string{"--rpc-token", "secret", "issue", "list"},
			wantOK:   false,
			wantRest: nil,
		},
		{
			name:     "remote without a URL uses the environment default",
			args:     []string{"--remote", "issue", "list"},
			wantRest: []string{"issue", "list"},
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GHX_RPC_URL", "")
			t.Setenv("GHX_RPC_TOKEN", "")
			url, token, rest, ok := takeRemoteArgs(tt.args)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
			if !reflect.DeepEqual(rest, tt.wantRest) {
				t.Errorf("rest = %#v, want %#v", rest, tt.wantRest)
			}
		})
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8090", "localhost:8090", "[::1]:8090"} {
		if !isLoopbackAddr(addr) {
			t.Errorf("expected %q to be loopback", addr)
		}
	}
	for _, addr := range []string{":8090", "0.0.0.0:8090", "192.168.1.10:8090", "example.com:8090"} {
		if isLoopbackAddr(addr) {
			t.Errorf("expected %q not to be loopback", addr)
		}
	}
}

func TestRunServeRefusesNonLoopbackWithoutToken(t *testing.T) {
	oldAddr, oldToken := serveAddr, serveRPCToken
	defer func() {
		serveAddr, serveRPCToken = oldAddr, oldToken
	}()
	serveAddr, serveRPCToken = "0.0.0.0:8080", ""

	if err := runServe(serveCmd, nil); err == nil {
		t.Fatal("expected runServe to refuse a non-loopback address without a token")
	}
}

func TestSplitRPCExecArgs(t *testing.T) {
	url, token, rest, err := splitRPCExecArgs(
		[]string{"--url", "http://host:9000", "--token", "secret", "--", "issue", "list", "--state", "open"},
		"http://default", "default-token",
	)
	if err != nil {
		t.Fatalf("splitRPCExecArgs: %v", err)
	}
	if url != "http://host:9000" || token != "secret" {
		t.Fatalf("url/token = %q/%q", url, token)
	}
	want := []string{"issue", "list", "--state", "open"}
	if !reflect.DeepEqual(rest, want) {
		t.Fatalf("rest = %#v, want %#v", rest, want)
	}
}
