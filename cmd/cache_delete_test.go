package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/github"
)

// TestCacheDelete exercises one-item deletion on both backends: the detected
// type is deleted, the other type's item with the same number stays, and a
// second delete reports that nothing is cached.
func TestCacheDelete(t *testing.T) {
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
			oldRepo, oldType := repoFlag, cacheDeleteType
			repoFlag = "acme/widget"
			cacheDeleteType = ""
			t.Cleanup(func() { repoFlag, cacheDeleteType = oldRepo, oldType })
			storageFlag = tc.backend

			store, err := newStore()
			if err != nil {
				t.Fatalf("newStore: %v", err)
			}
			if err := store.SaveIssue("github.com", "acme", "widget", &github.Issue{
				Number: 1, Title: "Cached issue", State: "OPEN", Author: github.Actor{Login: "alice"},
			}); err != nil {
				t.Fatalf("SaveIssue: %v", err)
			}
			if err := store.SavePR("github.com", "acme", "widget", &github.PullRequest{
				Number: 2, Title: "Cached PR", State: "OPEN", Author: github.Actor{Login: "bob"},
			}); err != nil {
				t.Fatalf("SavePR: %v", err)
			}
			store.Close()

			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			if err := runCacheDelete(cmd, []string{"2"}); err != nil {
				t.Fatalf("runCacheDelete: %v", err)
			}
			if !strings.Contains(out.String(), "Deleted cached pull request #2 for acme/widget.") {
				t.Errorf("output = %q, want pull request deletion confirmation", out.String())
			}

			store, err = newStore()
			if err != nil {
				t.Fatalf("newStore after delete: %v", err)
			}
			if _, _, err := store.LoadPR("github.com", "acme", "widget", 2); err == nil {
				t.Error("pull request 2 is still cached after deletion")
			}
			if _, _, err := store.LoadIssue("github.com", "acme", "widget", 1); err != nil {
				t.Errorf("issue 1 was deleted with the pull request: %v", err)
			}
			store.Close()

			out.Reset()
			cacheDeleteType = "prs"
			if err := runCacheDelete(cmd, []string{"2"}); err != nil {
				t.Fatalf("second runCacheDelete: %v", err)
			}
			if !strings.Contains(out.String(), "No cached pull request #2 found for acme/widget.") {
				t.Errorf("second output = %q, want none-found message", out.String())
			}
		})
	}
}

// TestCacheDelete_TypeRestrictsKind verifies that --type limits the delete to
// the named kind instead of detecting it from the cache.
func TestCacheDelete_TypeRestrictsKind(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	oldRepo, oldType := repoFlag, cacheDeleteType
	repoFlag = "acme/widget"
	cacheDeleteType = "prs"
	t.Cleanup(func() { repoFlag, cacheDeleteType = oldRepo, oldType })

	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	if err := store.SaveIssue("github.com", "acme", "widget", &github.Issue{
		Number: 3, Title: "Cached issue", State: "OPEN", Author: github.Actor{Login: "alice"},
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	store.Close()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runCacheDelete(cmd, []string{"3"}); err != nil {
		t.Fatalf("runCacheDelete: %v", err)
	}
	if !strings.Contains(out.String(), "No cached pull request #3 found for acme/widget.") {
		t.Errorf("output = %q, want none-found message for the restricted type", out.String())
	}

	store, err = newStore()
	if err != nil {
		t.Fatalf("newStore after delete: %v", err)
	}
	defer store.Close()
	if _, _, err := store.LoadIssue("github.com", "acme", "widget", 3); err != nil {
		t.Errorf("issue 3 was deleted despite --type prs: %v", err)
	}
}

// TestCacheDelete_RejectsInvalidInput covers the number and --type validation.
func TestCacheDelete_RejectsInvalidInput(t *testing.T) {
	oldRepo, oldType := repoFlag, cacheDeleteType
	repoFlag = "acme/widget"
	t.Cleanup(func() { repoFlag, cacheDeleteType = oldRepo, oldType })

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})

	cacheDeleteType = ""
	for _, bad := range []string{"abc", "0", "-1"} {
		if err := runCacheDelete(cmd, []string{bad}); err == nil {
			t.Errorf("runCacheDelete(%q) succeeded, want an invalid-number error", bad)
		}
	}

	cacheDeleteType = "both"
	if err := runCacheDelete(cmd, []string{"1"}); err == nil {
		t.Error("runCacheDelete with --type both succeeded, want an invalid-type error")
	}
}
