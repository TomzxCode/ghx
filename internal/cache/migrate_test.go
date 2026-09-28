package cache

import (
	"reflect"
	"testing"

	"github.com/tomzxcode/ghx/internal/github"
)

func seedMigrationSource(t *testing.T) Store {
	t.Helper()
	src := NewStoreWithPath(t.TempDir())
	for _, is := range []*github.Issue{richIssue(1), richOpenIssue(2)} {
		if err := src.SaveIssue(qHost, qOwner, qRepo, is); err != nil {
			t.Fatalf("SaveIssue(%d): %v", is.Number, err)
		}
	}
	for _, pr := range []*github.PullRequest{richPR(10), richDraftPR(12)} {
		if err := src.SavePR(qHost, qOwner, qRepo, pr); err != nil {
			t.Fatalf("SavePR(%d): %v", pr.Number, err)
		}
	}
	cursor := utc(2024, 7, 8, 9, 10)
	if err := src.SaveCacheInfoFull(qHost, qOwner, qRepo, &CacheInfo{
		CachedAt:    utc(2024, 7, 8, 9, 10),
		Duration:    60,
		Complete:    true,
		IssueCursor: &cursor,
	}); err != nil {
		t.Fatalf("SaveCacheInfoFull: %v", err)
	}
	return src
}

// TestMigrateLossless asserts migrating file -> SQLite preserves every field and
// the cache metadata (FR-05, NFR-03).
func TestMigrateLossless(t *testing.T) {
	src := seedMigrationSource(t)
	dst, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer dst.Close()

	res, err := Migrate(src, dst, qHost, qOwner, qRepo)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Issues != 2 || res.PRs != 2 {
		t.Errorf("counts = %+v, want 2 issues and 2 PRs", res)
	}

	compareIssues(t, "migrated issues",
		mustLoadAllIssues(t, src, qHost, qOwner, qRepo),
		mustLoadAllIssues(t, dst, qHost, qOwner, qRepo))
	comparePRs(t, "migrated PRs",
		mustLoadAllPRs(t, src, qHost, qOwner, qRepo),
		mustLoadAllPRs(t, dst, qHost, qOwner, qRepo))

	srcInfo, err := src.LoadCacheInfo(qHost, qOwner, qRepo)
	if err != nil {
		t.Fatalf("src LoadCacheInfo: %v", err)
	}
	dstInfo, err := dst.LoadCacheInfo(qHost, qOwner, qRepo)
	if err != nil {
		t.Fatalf("dst LoadCacheInfo: %v", err)
	}
	if !reflect.DeepEqual(srcInfo, dstInfo) {
		t.Errorf("cache info differs\nsrc: %+v\ndst: %+v", srcInfo, dstInfo)
	}
}

// TestMigrateIdempotent asserts re-running a migration produces the same result
// with no duplicate rows.
func TestMigrateIdempotent(t *testing.T) {
	src := seedMigrationSource(t)
	dst, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer dst.Close()

	for i := 0; i < 2; i++ {
		if _, err := Migrate(src, dst, qHost, qOwner, qRepo); err != nil {
			t.Fatalf("Migrate run %d: %v", i, err)
		}
		issues, err := dst.LoadAllIssues(qHost, qOwner, qRepo)
		if err != nil {
			t.Fatalf("LoadAllIssues run %d: %v", i, err)
		}
		prs, err := dst.LoadAllPRs(qHost, qOwner, qRepo)
		if err != nil {
			t.Fatalf("LoadAllPRs run %d: %v", i, err)
		}
		if len(issues) != 2 || len(prs) != 2 {
			t.Fatalf("run %d: got %d issues and %d PRs, want 2 and 2", i, len(issues), len(prs))
		}
	}
}

// TestMigrateInterruptedRerunnable simulates an interrupted migration (a partial
// database) and asserts the database stays valid and a re-run completes it
// (NFR-02).
func TestMigrateInterruptedRerunnable(t *testing.T) {
	src := seedMigrationSource(t)
	dir := t.TempDir()

	// First attempt writes only one row, then the process "dies".
	partial, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := partial.SaveIssue(qHost, qOwner, qRepo, richIssue(1)); err != nil {
		t.Fatalf("partial SaveIssue: %v", err)
	}
	if err := partial.Close(); err != nil {
		t.Fatalf("partial Close: %v", err)
	}

	// Reopen: the database must be valid, and a full migration must complete it.
	dst, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer dst.Close()

	if _, err := Migrate(src, dst, qHost, qOwner, qRepo); err != nil {
		t.Fatalf("Migrate after interruption: %v", err)
	}
	issues, err := dst.LoadAllIssues(qHost, qOwner, qRepo)
	if err != nil {
		t.Fatalf("LoadAllIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("after re-run: got %d issues, want 2", len(issues))
	}

	// The database must survive another reopen (no corruption).
	if err := dst.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	again, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("second reopen: %v", err)
	}
	defer again.Close()
	if n, err := again.LoadAllPRs(qHost, qOwner, qRepo); err != nil || len(n) != 2 {
		t.Errorf("after reopen: got %d PRs err=%v, want 2", len(n), err)
	}
}
