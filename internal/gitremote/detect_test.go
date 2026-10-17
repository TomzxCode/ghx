package gitremote

import (
	"testing"
)

func TestParseRepo(t *testing.T) {
	cases := []struct {
		input     string
		wantHost  string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"owner/repo", "github.com", "owner", "repo", false},
		{"github.com/owner/repo", "github.com", "owner", "repo", false},
		{"ghe.example.com/acme/tool", "ghe.example.com", "acme", "tool", false},
		{"TomzxCode/ai-text-editor", "github.com", "tomzxcode", "ai-text-editor", false},
		{"GitHub.com/Owner/Repo", "github.com", "owner", "repo", false},
		{"bad", "", "", "", true},
		{"a/b/c/d", "", "", "", true},
	}
	for _, c := range cases {
		r, err := ParseRepo(c.input)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseRepo(%q): expected error, got nil", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRepo(%q): unexpected error: %v", c.input, err)
			continue
		}
		if r.Host != c.wantHost || r.Owner != c.wantOwner || r.Name != c.wantName {
			t.Errorf("ParseRepo(%q) = {%s %s %s}, want {%s %s %s}",
				c.input, r.Host, r.Owner, r.Name, c.wantHost, c.wantOwner, c.wantName)
		}
	}
}

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		rawURL    string
		wantHost  string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"git@github.com:owner/repo.git", "github.com", "owner", "repo", false},
		{"git@github.com:owner/repo", "github.com", "owner", "repo", false},
		{"https://github.com/owner/repo.git", "github.com", "owner", "repo", false},
		{"https://github.com/owner/repo", "github.com", "owner", "repo", false},
		{"http://github.com/owner/repo", "github.com", "owner", "repo", false},
		{"git@ghe.example.com:acme/tool.git", "ghe.example.com", "acme", "tool", false},
		{"https://ghe.example.com/acme/tool", "ghe.example.com", "acme", "tool", false},
		{"git@github.com:TomzxCode/ai-text-editor.git", "github.com", "tomzxcode", "ai-text-editor", false},
		{"notaurl", "", "", "", true},
	}
	for _, c := range cases {
		r, err := parseRemoteURL(c.rawURL)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseRemoteURL(%q): expected error, got nil", c.rawURL)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseRemoteURL(%q): unexpected error: %v", c.rawURL, err)
			continue
		}
		if r.Host != c.wantHost || r.Owner != c.wantOwner || r.Name != c.wantName {
			t.Errorf("parseRemoteURL(%q) = {%s %s %s}, want {%s %s %s}",
				c.rawURL, r.Host, r.Owner, r.Name, c.wantHost, c.wantOwner, c.wantName)
		}
	}
}
