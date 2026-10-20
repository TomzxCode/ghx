package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/telemetry"
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

// TestRepoList_CacheHitRates verifies that repo list reports a per-operation
// hit rate for issue and PR retrieval, and a dash when an operation has no
// recorded retrievals.
func TestRepoList_CacheHitRates(t *testing.T) {
	withCleanStorageFlags(t)
	resetTelemetryState(t)
	off := false
	telemetryFlag = &off
	telemetryDBFlag = filepath.Join(t.TempDir(), "telemetry.db")

	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	if err := store.SaveIssue("github.com", "acme", "widget", &github.Issue{
		Number: 1, Title: "Cached issue", State: "OPEN", Author: github.Actor{Login: "alice"},
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}
	if err := store.SaveCacheInfoFull("github.com", "acme", "widget", &cache.CacheInfo{
		CachedAt: time.Now(), Duration: 60,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}
	store.Close()

	now := time.Now()
	tel, err := telemetry.Open(telemetryDBFlag)
	if err != nil {
		t.Fatalf("open telemetry: %v", err)
	}
	err = tel.InsertBatch([]telemetry.Event{
		{Timestamp: now, Kind: telemetry.KindCacheHit, Operation: telemetry.RetrievalIssueList, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheHit, Operation: telemetry.RetrievalIssueList, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheMiss, Operation: telemetry.RetrievalIssueList, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheHit, Operation: telemetry.RetrievalIssueView, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheMiss, Operation: telemetry.RetrievalPRList, Host: "github.com", Repo: "acme/widget"},
		{Timestamp: now, Kind: telemetry.KindCacheHit, Operation: telemetry.RetrievalPRList, Host: "github.com", Repo: "acme/other"},
	})
	if err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	tel.Close()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runRepoList(cmd, nil); err != nil {
		t.Fatalf("runRepoList: %v", err)
	}

	for _, want := range []string{"ISSUE-LIST", "ISSUE-VIEW", "PR-LIST", "PR-VIEW", "67% (3)", "100% (1)", "0% (1)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	fields := strings.Fields(out.String())
	row := fields[len(fields)-9:]
	if got, want := row[8], "—"; got != want {
		t.Errorf("PR view hit rate = %q, want %q (no events recorded)", got, want)
	}
}

// TestRecordRetrievalOutcome verifies that a hit and a miss are written as
// separate telemetry events under their retrieval operation.
func TestRecordRetrievalOutcome(t *testing.T) {
	resetTelemetryState(t)
	telemetryDBFlag = filepath.Join(t.TempDir(), "telemetry.db")
	on := true
	telemetryFlag = &on

	repo := &gitremote.Repo{Host: "github.com", Owner: "acme", Name: "widget"}
	recordRetrievalOutcome(repo, telemetry.RetrievalIssueList, true)
	recordRetrievalOutcome(repo, telemetry.RetrievalPRView, false)
	flushTelemetry()

	store, err := telemetry.Open(telemetryDBFlag)
	if err != nil {
		t.Fatalf("open telemetry: %v", err)
	}
	defer store.Close()
	rates, err := store.CacheHitRates()
	if err != nil {
		t.Fatalf("CacheHitRates: %v", err)
	}
	got := map[string]telemetry.CacheHitRate{}
	for _, r := range rates {
		got[r.Operation] = r
	}
	if r := got[telemetry.RetrievalIssueList]; r.Hits != 1 || r.Misses != 0 {
		t.Errorf("issue list rate = %+v, want 1 hit and 0 misses", r)
	}
	if r := got[telemetry.RetrievalPRView]; r.Hits != 0 || r.Misses != 1 {
		t.Errorf("pr view rate = %+v, want 0 hits and 1 miss", r)
	}
}
