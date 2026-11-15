package cache

import (
	"fmt"

	"github.com/tomzxcode/ghx/internal/logging"
)

// MigrationResult summarises how many items a migration copied.
type MigrationResult struct {
	Issues int
	PRs    int
}

// Migrate copies every cached issue, pull request, and the cache metadata for a
// single repository from src into dst.
//
// It relies on each backend's idempotent upsert semantics (the file backend
// overwrites by item number; the SQLite backend uses INSERT OR REPLACE), so
// re-running produces the same result and an interrupted run leaves dst valid
// and re-runnable (NFR-02). The source store is never modified.
func Migrate(src, dst Store, host, owner, repo string) (MigrationResult, error) {
	var res MigrationResult

	// Records are copied as they are and dst stamps them with this ghx's
	// version. That is right only when src was written by the same record
	// schema, so warn when it was not: the copy would otherwise look current
	// while carrying fields of an earlier schema.
	for _, kind := range []string{KindIssues, KindPRs} {
		status, err := src.RecordSchemaStatus(host, owner, repo, kind)
		if err != nil || status.Matches() {
			continue
		}
		logging.Warn(fmt.Sprintf("migrating %s that %s; the copy is stamped with schema %d, so run \"ghx cache --type %s --force\" to refetch them",
			schemaKindLabel(kind), describeRecordVersions(status), status.Current, kind),
			"repo", repoKey(host, owner, repo), "kind", kind,
			"outdated", status.Outdated, "unversioned", status.Unversioned)
	}

	issues, err := src.LoadAllIssues(host, owner, repo)
	if err != nil {
		return res, err
	}
	for _, issue := range issues {
		if err := dst.SaveIssue(host, owner, repo, issue); err != nil {
			return res, err
		}
		res.Issues++
	}

	prs, err := src.LoadAllPRs(host, owner, repo)
	if err != nil {
		return res, err
	}
	for _, pr := range prs {
		if err := dst.SavePR(host, owner, repo, pr); err != nil {
			return res, err
		}
		res.PRs++
	}

	if info, err := src.LoadCacheInfo(host, owner, repo); err == nil && info != nil {
		if err := dst.SaveCacheInfoFull(host, owner, repo, info); err != nil {
			return res, err
		}
	}

	for _, kind := range []string{"issues", "prs"} {
		segments, err := src.LoadSegments(host, owner, repo, kind)
		if err != nil {
			return res, err
		}
		if len(segments) == 0 {
			continue
		}
		if err := dst.SaveSegments(host, owner, repo, kind, segments); err != nil {
			return res, err
		}
	}
	return res, nil
}
