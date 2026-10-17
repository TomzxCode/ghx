package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/github"
)

func TestRepoDelete(t *testing.T) {
	for _, tc := range []struct {
		name    string
		backend string
	}{
		{name: "default sqlite", backend: ""},
		{name: "file", backend: "file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCleanStorageFlags(t)
			resetTelemetryState(t)
			off := false
			telemetryFlag = &off
			oldRepo := repoFlag
			repoFlag = ""
			t.Cleanup(func() { repoFlag = oldRepo })
			storageFlag = tc.backend

			store, err := newStore()
			if err != nil {
				t.Fatalf("newStore: %v", err)
			}
			if err := store.SaveIssue("github.com", "acme", "widget", &github.Issue{
				Number: 1,
				Title:  "Cached issue",
				State:  "OPEN",
				Author: github.Actor{Login: "alice"},
			}); err != nil {
				t.Fatalf("SaveIssue: %v", err)
			}
			store.Close()

			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			if err := runRepoDelete(cmd, []string{"acme/widget"}); err != nil {
				t.Fatalf("runRepoDelete: %v", err)
			}
			if !strings.Contains(out.String(), "Deleted cached data for acme/widget.") {
				t.Errorf("output = %q, want deletion confirmation", out.String())
			}

			store, err = newStore()
			if err != nil {
				t.Fatalf("newStore after delete: %v", err)
			}
			repos, err := store.ListCachedRepos()
			store.Close()
			if err != nil {
				t.Fatalf("ListCachedRepos: %v", err)
			}
			if len(repos) != 0 {
				t.Errorf("repos after delete = %+v, want none", repos)
			}

			out.Reset()
			if err := runRepoDelete(cmd, []string{"acme/widget"}); err != nil {
				t.Fatalf("second runRepoDelete: %v", err)
			}
			if !strings.Contains(out.String(), "No cached data found for acme/widget.") {
				t.Errorf("second output = %q, want none-found message", out.String())
			}
		})
	}
}

func TestRepoDelete_RejectsArgumentWithRepoFlag(t *testing.T) {
	oldRepo := repoFlag
	repoFlag = "acme/other"
	t.Cleanup(func() { repoFlag = oldRepo })

	cmd := &cobra.Command{}
	if err := runRepoDelete(cmd, []string{"acme/widget"}); err == nil {
		t.Fatal("expected an error when both an argument and --repo are given")
	}
}
