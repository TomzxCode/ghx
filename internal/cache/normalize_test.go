package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomzxcode/ghx/internal/github"
)

// backend unwraps the store decorators to reach the concrete backend. It exists
// for in-package tests that need to inspect the backend directly (for example
// the SQLite query planner), which is otherwise hidden by the normalize and
// instrumentation layers.
func backend(s Store) Store {
	for {
		switch v := s.(type) {
		case *normalizeStore:
			s = v.store
		case *InstrumentedStore:
			s = v.store
		default:
			return s
		}
	}
}

// TestStore_NormalizesRepoKeys verifies that a repository is stored under one
// key regardless of the casing it is written or read with, on both backends.
func TestStore_NormalizesRepoKeys(t *testing.T) {
	stores := map[string]func(t *testing.T) Store{
		"file": func(t *testing.T) Store { return NewStoreWithPath(t.TempDir()) },
		"sqlite": func(t *testing.T) Store {
			s, err := NewSQLiteStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			return s
		},
	}
	for name, newStore := range stores {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			defer s.Close()

			if err := s.SaveIssue("github.com", "TomzxCode", "ai-text-editor", &github.Issue{Number: 1, Title: "hello"}); err != nil {
				t.Fatalf("SaveIssue: %v", err)
			}
			if err := s.SavePR("github.com", "TomzxCode", "ai-text-editor", &github.PullRequest{Number: 1, Title: "pr"}); err != nil {
				t.Fatalf("SavePR: %v", err)
			}

			issue, _, err := s.LoadIssue("github.com", "tomzxcode", "ai-text-editor", 1)
			if err != nil {
				t.Fatalf("LoadIssue under lower-case key: %v", err)
			}
			if issue.Title != "hello" {
				t.Errorf("issue title = %q, want hello", issue.Title)
			}

			repos, err := s.ListCachedRepos()
			if err != nil {
				t.Fatalf("ListCachedRepos: %v", err)
			}
			if len(repos) != 1 {
				t.Fatalf("got %d cached repositories, want 1: %+v", len(repos), repos)
			}
			if repos[0].Host != "github.com" || repos[0].Owner != "tomzxcode" || repos[0].Repo != "ai-text-editor" {
				t.Errorf("repository coordinates = %s/%s/%s, want lower-case", repos[0].Host, repos[0].Owner, repos[0].Repo)
			}
		})
	}
}

// TestSQLiteNormalizeKeys_MigratesExistingRows verifies that a database written
// with mixed-case keys is collapsed to one lower-case entry on open, keeping the
// most recently written row.
func TestSQLiteNormalizeKeys_MigratesExistingRows(t *testing.T) {
	dir := t.TempDir()

	store, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	raw := backend(store).(*sqliteStore)

	// Write the same issue under two spellings, bypassing the normalizing
	// layer to reproduce a pre-migration database. The second write is newer.
	old := &github.Issue{Number: 1, Title: "old"}
	if err := raw.SaveIssue("github.com", "TomzxCode", "ai-text-editor", old); err != nil {
		t.Fatalf("SaveIssue old: %v", err)
	}
	newer := &github.Issue{Number: 1, Title: "newer"}
	if err := raw.SaveIssue("github.com", "tomzxcode", "ai-text-editor", newer); err != nil {
		t.Fatalf("SaveIssue newer: %v", err)
	}
	if _, err := raw.db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatalf("reset user_version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening runs the migration.
	reopened, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer reopened.Close()

	all, err := reopened.LoadAllIssues("github.com", "tomzxcode", "ai-text-editor")
	if err != nil {
		t.Fatalf("LoadAllIssues: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d issues after migration, want 1", len(all))
	}
	if all[0].Title != "newer" {
		t.Errorf("surviving issue title = %q, want newer (most recent write)", all[0].Title)
	}

	repos, err := reopened.ListCachedRepos()
	if err != nil {
		t.Fatalf("ListCachedRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("got %d repositories after migration, want 1", len(repos))
	}
}

// TestFileNormalizeKeys_MigratesExistingDirs verifies that a file cache written
// with mixed-case directory names is renamed to the lower-case key on open.
func TestFileNormalizeKeys_MigratesExistingDirs(t *testing.T) {
	dir := t.TempDir()

	// Reproduce a pre-migration file cache: an issue stored under a
	// mixed-case owner directory.
	issueDir := filepath.Join(dir, "github.com", "TomzxCode", "ai-text-editor", "issues")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(&github.Issue{Number: 1, Title: "cached"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "1.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := NewStoreWithPath(dir)
	defer store.Close()

	issue, _, err := store.LoadIssue("github.com", "tomzxcode", "ai-text-editor", 1)
	if err != nil {
		t.Fatalf("LoadIssue after migration: %v", err)
	}
	if issue.Title != "cached" {
		t.Errorf("issue title = %q, want cached", issue.Title)
	}

	repos, err := store.ListCachedRepos()
	if err != nil {
		t.Fatalf("ListCachedRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("got %d repositories after migration, want 1: %+v", len(repos), repos)
	}
	if repos[0].Owner != "tomzxcode" {
		t.Errorf("owner = %q, want tomzxcode", repos[0].Owner)
	}
}
