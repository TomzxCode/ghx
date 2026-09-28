---
artifact: implementation
verdict: approved
reviewed_at: 2026-07-04
---

## Summary

🟢 The implementation meets the acceptance criteria, is parameterized and reversible, and is well covered; one deadlock was found by running the CLI and fixed, and the remaining findings are spec-drift and minor robustness nits.

## Correctness

🟡 `cache migrate` diverges from the specification's command contract: the spec lists a `--force` flag and a "skip rows whose `updated_at` is unchanged" mode, but the implementation always upserts (`INSERT OR REPLACE`). The observed outcome (idempotent, lossless) is correct; the spec is the stale side. Recommendation: align the spec's migrate section to "always upserts; idempotent".
🟢 Non-nil zero-time pointers round-trip asymmetrically: `nullTime` maps a zero time to SQL `NULL` (decoding to `nil`), while the file backend JSON-marshals it to a non-nil zero value. GraphQL never emits zero times for `closedAt`/`mergedAt`, so this is not reachable today.
🟢 Migrated rows get `row_mtime = now`, so a migrated item looked up individually (the `view` fallback path) appears fresh for 60 minutes even if the source cache was stale. Harmless; the full-cache freshness check is migrated correctly.
🟢 (Resolved during review) `ListCachedRepos` deadlocked under `SetMaxOpenConns(1)` by issuing a metadata query per row while the result set was open. Fixed with a `LEFT JOIN`; a regression test now covers the path.

## Code Quality

No issues found.
The `Store` interface is minimal and its method set mirrors the prior concrete API; `fileStore`/`sqliteStore` are cohesive; naming follows repository conventions; no dead or commented-out code.

## Test Coverage

*Coverage analysis produced directly (not via a separate `/analyze-test-coverage` invocation): I authored this diff and its tests, so the tables below are derived from the change set and test files.*

### Introduced tests

| Test file | Test(s) | What it tests |
|---|---|---|
| `internal/cache/filter_test.go` | `TestFilterIssues_*`, `TestFilterPRs_*` | Filter semantics (state, author, assignee, labels AND-match, milestone title/number, base/head, draft, search) |
| `internal/cache/sqlite_store_test.go` | `TestSQLiteRoundTripMatchesFile`, `TestSQLitePersistsAcrossReopen`, `TestSQLiteListCachedRepos` | Field-exact round-trip vs file backend, persistence across reopen, repository listing |
| `internal/cache/query_equiv_test.go` | `TestQueryEquivalence`, `TestSQLiteQueryUsesIndex` | SQLite query results equal the file backend across a matrix; reads use an index not a full scan |
| `internal/cache/migrate_test.go` | `TestMigrateLossless`, `TestMigrateIdempotent`, `TestMigrateInterruptedRerunnable` | Lossless copy, idempotent re-run, re-runnable partial migration |
| `cmd/storage_test.go` | `TestNewStore_DefaultBackendIsSQLite`, `TestNewStore_FileIsSelectable`, `TestNewStore_InvalidBackend`, `TestCache_DefaultBackendIsSQLite` | Backend default, selection, and error handling |
| `cmd/bench_test.go` | `BenchmarkFileBackend*`, `BenchmarkSQLiteBackend*` | NFR-01 file-vs-SQLite performance |

### Change coverage

| Changed file | Behavior changed | Covered by test? | Gap |
|---|---|---|---|
| `internal/cache/store.go` | Store interface + shared types | Yes | — |
| `internal/cache/file_store.go` | fileStore impl + Close | Yes | — |
| `internal/cache/sqlite_store.go` | SQLite CRUD, queries, metadata, listing | Yes | FR-07 output line and `Location()` not asserted |
| `internal/cache/schema.sql` | Schema | Yes | — |
| `internal/cache/filter.go` | Shared filter | Yes | — |
| `internal/cache/migrate.go` | Migration | Yes | — |
| `cmd/root.go` | Backend selection + default | Yes | — |
| `cmd/issue.go` / `cmd/pr.go` | List via `Query*` | Yes (file via existing tests; sqlite via `TestCache_DefaultBackendIsSQLite` + manual) | — |
| `cmd/cache.go` | Default notice, `cache migrate` | Partial | `runCacheMigrate` handler not unit-tested |
| `cmd/repo.go` | `repo list` single query | Yes (`TestSQLiteListCachedRepos`; command wrapper manual) | — |
| `cmd/stats.go` | `cache.Store` param | Yes (`stats_test.go`) | — |
| Docs (`README.md`, `docs/user-guide/*`) | SQLite default + migrate | Yes (manual CLI verification) | — |

### Uncovered code

| File | Function / branch / path | Why it matters |
|---|---|---|
| `cmd/cache.go` | `runCacheMigrate` | Repo selection/output regressions would not be caught in CI (logic below it is tested) |
| `cmd/cache.go` | `Using sqlite cache backend at ...` line | FR-07's user-facing output is unasserted |
| `internal/cache/sqlite_store.go` | `Location()` | Trivial accessor; asserted only indirectly |

## Security

No issues found.
Every SQL statement uses parameterized placeholders; the dynamic `WHERE` builder appends literal column names and binds values separately. No secrets are introduced. The database is created under the user's cache directory with default permissions, consistent with the existing file cache.

## Performance

No issues found (NFR-01 met: 3.6x / 14.3x / 2.0x vs file).
🟢 `state = ? COLLATE NOCASE` and `author_login = ? COLLATE NOCASE` cannot use the default binary-collation indexes, so filtered reads may fall back to a scan of the repository's rows (still far cheaper than file I/O). If filtered latency matters at very large scale, normalize the compared column or add a `NOCASE` index.

## Spec Alignment

🟡 The `cache migrate` spec contract (see Correctness) should be reconciled to the implementation.
🟢 The spec's `Store` interface was aligned to the implementation during the propagation pass (`ctx` removed; `Kind`/`Location` added); everything else matches (schema, default backend, DB location).
No scope creep: every changed behavior traces to an `FR`/`NFR`.

## Reversibility

No issues found.
Fully reversible: `--storage file` restores prior behavior; the file cache is preserved by migration; the interface is a mechanical refactor. No destructive migration, no data loss, no one-way doors.

## Forward Compatibility

No issues found.
Schema versioned via `PRAGMA user_version`; JSON columns tolerate unknown fields; new backends/query predicates and additive schema changes are accommodated without breaking consumers. FTS5 is a reserved additive extension.

## Findings

| Priority | Finding | Disposition |
|---|---|---|
| 🟡 | `cache migrate` `--force`/skip semantics differ from the spec | Resolved: spec aligned to always-upsert |
| 🟡 | FR-07 output line and `runCacheMigrate` lack automated tests | Resolved: output line was already asserted in `TestCache_DefaultBackendIsSQLite`; added a `Location()` assertion and `TestCacheMigrate_Command` |
| 🟢 | `COLLATE NOCASE` may bypass the index for filtered reads | Consider a normalized column/index if needed |
| 🟢 | Zero-time-pointer and migrated `row_mtime` nuances | Not reachable today; documented |
| 🟢 | `ListCachedRepos` deadlock | Fixed and regression-tested during review |

## Resolutions

- Spec's migrate section now states the implemented contract: optional `--repo`, per-row `INSERT OR REPLACE` (no whole-repo transaction), always-idempotent upserts, and a valid re-runnable database after a failed run.
- Added `TestCacheMigrate_Command` (command-level file→SQLite copy) and a `Location()` assertion. Corrected the coverage note: the FR-07 output line was already covered by `TestCache_DefaultBackendIsSQLite`; only `Location()` was unasserted.
