package cache

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
)

// schemaVersionKeysNormalized is the SQLite user_version at which stored
// repository coordinates have been lower-cased. NewSQLiteStore runs the
// normalization once, for databases written before the cache keyed on the
// canonical form.
const schemaVersionKeysNormalized = 3

// sqliteUserVersion reads the database's schema version.
func sqliteUserVersion(db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// normalizeSQLiteKeys lowercases the repository coordinates stored in every
// table, so a repository cached under a mixed-case spelling (for example
// "TomzxCode/ai-text-editor" alongside "tomzxcode/ai-text-editor") collapses to
// one entry. Where lower-casing a key collides with an existing row, the most
// recently written row wins; the loser is deleted first so the primary key
// never conflicts.
//
// It is idempotent: running it on an already-normalized database changes
// nothing.
func normalizeSQLiteKeys(db *sql.DB) error {
	statements := []string{
		// Issues: drop duplicate (number) rows per lower-cased key, keeping the
		// newest write, then lower-case the surviving rows.
		`DELETE FROM issues WHERE rowid NOT IN (
			SELECT rowid FROM (
				SELECT rowid, ROW_NUMBER() OVER (
					PARTITION BY lower(host), lower(owner), lower(repo), number
					ORDER BY row_mtime DESC, rowid DESC
				) AS rn FROM issues
			) WHERE rn = 1
		)`,
		`UPDATE issues SET host = lower(host), owner = lower(owner), repo = lower(repo)`,
		// Pull requests: same treatment.
		`DELETE FROM pull_requests WHERE rowid NOT IN (
			SELECT rowid FROM (
				SELECT rowid, ROW_NUMBER() OVER (
					PARTITION BY lower(host), lower(owner), lower(repo), number
					ORDER BY row_mtime DESC, rowid DESC
				) AS rn FROM pull_requests
			) WHERE rn = 1
		)`,
		`UPDATE pull_requests SET host = lower(host), owner = lower(owner), repo = lower(repo)`,
		// Cache metadata: one row per repository, keeping the newest.
		`DELETE FROM cache_meta WHERE rowid NOT IN (
			SELECT rowid FROM (
				SELECT rowid, ROW_NUMBER() OVER (
					PARTITION BY lower(host), lower(owner), lower(repo)
					ORDER BY cached_at DESC, rowid DESC
				) AS rn FROM cache_meta
			) WHERE rn = 1
		)`,
		`UPDATE cache_meta SET host = lower(host), owner = lower(owner), repo = lower(repo)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// fileKeysMarker records that the one-time key normalization has run for a file
// cache, so later opens skip the directory walk.
const fileKeysMarker = ".normalized-keys-v1"

// normalizeFileKeys lowercases the host/owner/repo directory names of a file
// cache, merging directories that differ only by case. It runs once, guarded by
// a marker file, and is best-effort: a failure leaves the cache readable under
// its existing keys rather than blocking the command.
func normalizeFileKeys(baseDir string) {
	if _, err := os.Stat(filepath.Join(baseDir, fileKeysMarker)); err == nil {
		return
	}
	if _, err := os.Stat(baseDir); err != nil {
		return // no cache yet; nothing to normalize and no marker to write
	}
	normalizeFileDirLevel(baseDir, 0)
	if err := os.WriteFile(filepath.Join(baseDir, fileKeysMarker), nil, 0644); err != nil {
		// The next open retries; the cache still works under its old keys.
		return
	}
}

// normalizeFileDirLevel lowercases the directory names at the given depth under
// dir (0 = host, 1 = owner, 2 = repo), merging any that collide, then recurses.
func normalizeFileDirLevel(dir string, depth int) {
	if depth > 2 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		path := filepath.Join(dir, name)
		if name != lower {
			target := filepath.Join(dir, lower)
			if err := mergeDir(path, target); err != nil {
				continue
			}
			path = target
		}
		normalizeFileDirLevel(path, depth+1)
	}
}

// mergeDir moves every file and subdirectory of src into dst, then removes src.
// A file with the same name in dst is overwritten, matching the cache's
// last-write-wins semantics for a single item.
func mergeDir(src, dst string) error {
	if srcInfo, err := os.Stat(src); err == nil {
		if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(srcInfo, dstInfo) {
			// A case-insensitive filesystem resolves src and dst to the same
			// directory. Merging it into itself would delete its contents, so
			// rename it to record the lower-case spelling instead.
			return os.Rename(src, dst)
		}
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := mergeDir(from, to); err != nil {
				return err
			}
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return os.RemoveAll(src)
}
