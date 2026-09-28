package cmd

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

// withCleanStorageFlags isolates the storage-related globals and env for a test.
func withCleanStorageFlags(t *testing.T) {
	t.Helper()
	savedStorage, savedDir := storageFlag, cacheDir
	savedEnv, hadEnv := os.LookupEnv("GHX_STORAGE")
	t.Cleanup(func() {
		storageFlag, cacheDir = savedStorage, savedDir
		if hadEnv {
			os.Setenv("GHX_STORAGE", savedEnv)
		} else {
			os.Unsetenv("GHX_STORAGE")
		}
	})
	storageFlag = ""
	os.Unsetenv("GHX_STORAGE")
	cacheDir = t.TempDir()
}

// TestNewStore_DefaultBackendIsSQLite asserts SQLite is the default backend when
// neither --storage nor GHX_STORAGE is set.
func TestNewStore_DefaultBackendIsSQLite(t *testing.T) {
	withCleanStorageFlags(t)

	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	defer store.Close()
	if store.Kind() != "sqlite" {
		t.Errorf("default backend = %q, want %q", store.Kind(), "sqlite")
	}
}

// TestNewStore_FileIsSelectable asserts the file backend can still be selected.
func TestNewStore_FileIsSelectable(t *testing.T) {
	withCleanStorageFlags(t)

	storageFlag = "file"
	store, err := newStore()
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	defer store.Close()
	if store.Kind() != "file" {
		t.Errorf("--storage file backend = %q, want %q", store.Kind(), "file")
	}
}

// TestNewStore_InvalidBackend asserts an unknown backend is rejected.
func TestNewStore_InvalidBackend(t *testing.T) {
	withCleanStorageFlags(t)

	storageFlag = "bogus"
	if _, err := newStore(); err == nil {
		t.Error("expected an error for an unknown --storage value")
	}
}

// TestCache_DefaultBackendIsSQLite verifies the fetch command writes to the
// SQLite backend by default.
func TestCache_DefaultBackendIsSQLite(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	srv := mockserver.NewServer(sinceScenario(now))
	t.Cleanup(srv.Close)
	setCacheTestEnv(t, srv.URL(), t.TempDir())
	// setCacheTestEnv pins the file backend for cache-inspection tests; clear it
	// so this test exercises the default (SQLite).
	storageFlag = ""
	os.Unsetenv("GHX_STORAGE")

	out := runCacheCapture(t)
	if !strings.Contains(out, "Using sqlite cache backend") {
		t.Fatalf("expected the SQLite backend notice, got %q", out)
	}

	store, err := cache.NewSQLiteStore(cacheDir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	issues, err := store.LoadAllIssues("github.com", "acme", "myproject")
	if err != nil {
		t.Fatalf("LoadAllIssues: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected issues to be cached in SQLite by default")
	}
}
