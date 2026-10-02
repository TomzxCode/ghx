---
date: "2026-10-02"
scope: "FEAT-0008-sqlite-backend"
entry: "code"
flags: "fix: false, create-issues: false"
status: complete
---

# Propagation Report

**Date:** 2026-10-02
**Scope:** FEAT-0008-sqlite-backend (change set: commit `82941df`, "Add gh-compatible --json field selection to pr list")
**Entry:** code

This run supersedes the 2026-07-04 report.
Unresolved findings from that run are carried forward at the end.

## Change Set

- `pull_requests` gains a `head_ref_oid` column; `schema.sql` records it and `SavePR`/`scanPR`/`prCols` round-trip it.
- `NewSQLiteStore` advances `PRAGMA user_version` from 1 to 2 and runs an additive `migrateSQLiteSchema` that adds `head_ref_oid` to databases created by the earlier schema.
- `github.PullRequest` gains the `HeadRefOid` field (the change also touched the file backend, which serializes the struct as JSON and needed no schema work).

## Consistency Status

| Feature | Downward updates | Upward drift | IDs | Review order | Status |
|---|---|---|---|---|---|
| FEAT-0008-sqlite-backend | 1 proposed | 4 findings | clean | all approved, now stale | drifted |

## Updates (downward)

| # | Artifact | Edge | Change | Resolution | Status |
|---|---|---|---|---|---|
| 1 | `tests.md` | code -> tests | new `head_ref_oid` column and `TestSQLiteMigratesHeadRefOid` | add the test to the test-files table and the FR-02/NFR-03 coverage rows | proposed (report-only) |

No other dependent described the old schema; the user guide documents backend selection, not columns, so it needed no rewrite.

## Questions Raised

| # | Edge | Disagreement | Resolutions | Where recorded |
|---|---|---|---|---|
| 1 | Requirements ↔ Code | The code changed the in-memory `github.PullRequest` type, which this feature's constraint and out-of-scope explicitly forbid | (a) relax the constraint, noting the model was extended by FEAT-0002's list-JSON work, (b) treat it as a violation and revert the field | report only |
| 2 | Observability ↔ Code | The spec claims a "schema version newer than binary" check that the code never performs | (a) implement the check, (b) drop the failure-mode row | report only |

## Traceability Matrix (per feature)

### FEAT-0008-sqlite-backend

| FR / NFR | Spec section | Plan phase | Task(s) | Test(s) | Code location | Docs | Issue AC |
|---|---|---|---|---|---|---|---|
| FR-01 | Data Models / Store CRUD | Ph2 | (none) | `sqlite_store_test.go` | `internal/cache/sqlite_store.go` | cache.md | AC1 |
| FR-02 | SQLite schema | Ph2 | (none) | `sqlite_store_test.go`, `TestSQLiteMigratesHeadRefOid` | `internal/cache/schema.sql` | cache.md | AC2 |
| FR-03 | API Contracts (Query*) | Ph3 | (none) | `query_equiv_test.go` | `internal/cache/sqlite_store.go` | cache.md | AC3 |
| FR-04 | Backend selection | Ph4 | (none) | `cmd/storage_test.go` | `cmd/root.go` | README, cache.md, flag-reference.md | AC4 |
| FR-05 | New CLI command / Migration | Ph4 | (none) | `migrate_test.go` | `internal/cache/migrate.go`, `cmd/cache.go` | cache.md | AC5 |
| FR-06 | Technical Decisions (search) | Ph3 | (none) | `filter_test.go`, `query_equiv_test.go` | `internal/cache/filter.go` | cache.md | AC3 |
| FR-07 | API Contracts (Kind/Location) | Ph4 | (none) | (no direct test) | `cmd/cache.go`, `internal/cache/store.go` | flag-reference.md | AC4 |
| FR-08 | (plan Goal) | Ph5 | (none) | docs build | README.md, docs/user-guide/*.md | (self) | AC8 |
| NFR-01 | Performance decision | Ph0/Ph5 | (none) | `cmd/bench_test.go` | `internal/cache/sqlite_store.go` | cache.md | AC6 |
| NFR-02 | Sequences (migration) | Ph2/Ph4 | (none) | `migrate_test.go` | `migrate.go`, `sqlite_store.go` | cache.md | AC7 |
| NFR-03 | Data Models | Ph2/Ph3 | (none) | `sqlite_store_test.go`, `migrate_test.go`, `query_equiv_test.go` | `sqlite_store.go` | cache.md | AC7 |
| NFR-04 | Technical Decisions (driver) | Ph0 | (none) | CGO-free cross-build | `go.mod` | (n/a) | AC1 |
| NFR-05 | Data Models (single file) | Ph2 | (none) | `sqlite_store_test.go` (reopen) | `sqlite_store.go` | (n/a) | AC1 |
| NFR-06 | (none) | (none) | (none) | (not asserted) | `sqlite_store.go` | (n/a) | AC1 |
| (none) | (none) | (none) | (none) | `TestSQLiteMigratesHeadRefOid` | `migrateSQLiteSchema`, `columnExists` | (none) | (none) |

Rows unchanged from the 2026-07-04 run are reproduced for continuity.
Every task column remains empty because the task decomposition was intentionally skipped (the user directed implementation from the plan).

## Summary

| Class | Critical | High | Medium | Low | Total |
|---|---|---|---|---|---|
| Orphan upstream | 0 | 0 | 0 | 1 | 1 |
| Orphan downstream | 0 | 0 | 2 | 1 | 3 |
| Broken reference | 0 | 0 | 0 | 0 | 0 |
| Status inversion | 0 | 0 | 0 | 0 | 0 |
| **Total** | **0** | **0** | **2** | **2** | **4** |

Carried-forward findings from 2026-07-04 are listed separately below and are not counted here.

## Findings

### 1. `pull_requests` schema table omits `head_ref_oid`
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (code without spec backing)
**Severity:** Medium
**Direction:** Upward
**Edge(s):** Specification ↔ Code
**Location:** `specification.md` §Data Models / `pull_requests`
**Finding:** The `pull_requests` column table lists `is_draft`, `base_ref_name`, `head_ref_name`, `merged_at`, and `review_decision`, but not the new `head_ref_oid` column the code writes and reads.
**Impact:** The schema specification no longer matches `schema.sql`; a re-implementation from the spec would drop a persisted field (and FR-02 / NFR-03 round-trip coverage with it).
**Recommendation:** Add a `head_ref_oid | TEXT | not null (default '') | Head commit SHA` row.

### 2. The "no in-memory model change" constraint is contradicted by the code
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (code crossing a stated boundary) / premise conflict
**Severity:** Medium
**Direction:** Upward
**Edge(s):** Requirements ↔ Code
**Location:** `requirements.md` §Constraints ("No schema changes to the in-memory `github.Issue` / `github.PullRequest` types"); `specification.md` §Overview and §Out of Scope
**Finding:** The change adds `HeadRefOid` to `github.PullRequest`, which this feature explicitly listed as out of scope and as a constraint ("SQLite is a storage alternative, not a model change").
**Impact:** Two artifacts still assert a boundary the code no longer honors, so the feature reads as violated rather than superseded.
**Recommendation:** Relax the constraint and out-of-scope wording to note the model was later extended by the PR list-JSON work (FEAT-0002), keeping "SQLite does not change the model" as a statement about this feature's own scope.

### 3. Schema version documented as 1 while the code advances to 2
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (stale spec value)
**Severity:** Low
**Direction:** Upward
**Edge(s):** Specification ↔ Code
**Location:** `specification.md` §Schema versioning ("`PRAGMA user_version = 1;` set at creation")
**Finding:** `NewSQLiteStore` now sets `PRAGMA user_version = 2` and runs an additive migration, so the spec's stated version is stale.
**Impact:** Low; the spec's migration approach ("future additive migrations check and advance `user_version`") is exactly what the code does, only the number is wrong.
**Recommendation:** State the current version (2) and record that `head_ref_oid` was the first additive migration.

### 4. Observability claims a schema-version check the code does not implement
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan upstream (documented behavior with no code)
**Severity:** Low
**Direction:** Upward
**Edge(s):** Observability ↔ Code, Observability ↔ Specification
**Location:** `observability.md` §Failure Modes ("Schema version newer than binary | `PRAGMA user_version` check")
**Finding:** `NewSQLiteStore` writes `user_version` but never reads it, so an older binary opening a newer database silently rewrites the version instead of reporting that the cache was written by a newer `ghx`.
**Impact:** Low for a single-user local CLI, but the documented failure behavior does not exist; this is the first schema bump that makes the gap observable in practice.
**Recommendation:** Either implement the `user_version` compare-and-report path, or remove the failure-mode row.

### 5. `tests.md` does not list the migration test or the new column
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (new test missing from the test plan)
**Severity:** Low
**Direction:** Downward
**Edge(s):** Code ↔ Tests
**Location:** `tests.md` §Test Files, §FR / NFR Coverage Matrix
**Finding:** `sqlite_store_test.go` now contains `TestSQLiteMigratesHeadRefOid`, and `schema.sql` has a new column, but the test plan neither lists the test nor mentions the column under FR-02/NFR-03.
**Impact:** Low; the test runs, but the test plan understates coverage.
**Recommendation:** Add the test to the SQLite row and FR-02 / NFR-03 coverage.

## Carried Forward From 2026-07-04 (Unresolved)

These predate this change and remain open.

| # | Finding | Class | Severity | Recommendation |
|---|---|---|---|---|
| 3 | No `tasks/` decomposition; plan phases served as the breakdown | Orphan upstream | Medium | accept as-is or backfill `tasks/` |
| 4 | FR-07 backend/path output and `Location()` not asserted by a test | Orphan downstream | Medium | add an assertion |
| 5 | `cache migrate` command handler not covered by a command-level test | Orphan downstream | Low | add a command-level test |
| 6 | NFR-06 filesystem permissions not asserted (documented as such) | Orphan downstream | Low | accept as documented or add a test |

## Fixes Applied

None.
This was a report-only run (`--fix` not set).
With `--fix`, the safe upward action would have been to regress `review-specification.md`, `review-requirements.md`, and `review-tests.md` to `changes-requested`, since all three are currently `approved` while their artifacts have drifted.

## Recommended Actions

| Priority | Action | Owner decision |
|---|---|---|
| Medium | Add `head_ref_oid` to the spec schema table (finding 1) | update spec |
| Medium | Relax the model-change constraint and out-of-scope wording (finding 2) | update requirements + spec |
| Low | Correct the documented `user_version` to 2 (finding 3) | update spec |
| Low | Implement or drop the "schema version newer than binary" row (finding 4) | implement / remove |
| Low | Add the migration test and column to `tests.md` (finding 5) | update tests plan |
| Medium | Carry-forward: tasks decomposition decision (prior finding 3) | accept / backfill |
| Medium | Carry-forward: FR-07 output assertion (prior finding 4) | implement |
| Low | Carry-forward: migrate command test (prior finding 5) | implement |
| Low | Carry-forward: NFR-06 permissions (prior finding 6) | accept / implement |
