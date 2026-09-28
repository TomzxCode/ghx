---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Tests: SQLite backend for issue/PR storage

## Overview

Test plan and FR/NFR coverage matrix for the SQLite cache backend.
Unit tests live in `internal/cache/`; the end-to-end path was verified manually with the built binary against a seeded file cache.

## Test Files

| File | Covers |
|---|---|
| `internal/cache/file_store_test.go` (`store_test.go`) | File backend CRUD, freshness, cursors |
| `internal/cache/filter_test.go` | Filter semantics (state, author, assignee, labels, milestone, base/head, draft, search) |
| `internal/cache/sqlite_store_test.go` | SQLite round-trip vs file backend; persistence across reopen |
| `internal/cache/query_equiv_test.go` | SQLite vs file query equivalence matrix; indexed-query plan |
| `internal/cache/migrate_test.go` | Lossless migration; idempotent re-run; re-runnable partial migration |
| `cmd/bench_test.go` | File vs SQLite benchmarks (NFR-01) |

## FR / NFR Coverage Matrix

| ID | Requirement (short) | Coverage |
|---|---|---|
| FR-01 | SQLite backend persists issues/PRs | `sqlite_store.go`; `TestSQLiteRoundTripMatchesFile`, `TestSQLitePersistsAcrossReopen` |
| FR-02 | Schema covers all file-backend fields | `schema.sql`; `TestSQLiteRoundTripMatchesFile` (comments, labels, assignees, milestone, nullable timestamps, review decision) |
| FR-03 | Indexed SQL reads, not full scans | `sqliteStore.QueryIssues`/`QueryPRs`; `TestSQLiteQueryUsesIndex` (EXPLAIN QUERY PLAN), `TestQueryEquivalence` |
| FR-04 | Selectable backend, SQLite default, file available | `cmd/root.go` `newStore`; `--storage`/`GHX_STORAGE`; `cmd/storage_test.go` (default=sqlite, file selectable); e2e smoke |
| FR-05 | Migration path, lossless | `migrate.go`, `cache migrate`; `TestMigrateLossless`, `TestMigrateIdempotent` |
| FR-06 | Search titles/bodies | `filter.go` search; `TestFilterIssues_Search`, `TestFilterPRs_Search`, `TestQueryEquivalence` |
| FR-07 | Backend type/path surfaced | `runCache` prints backend+path when sqlite; `cache migrate` prints destination; `Kind`/`Location` |
| FR-08 | Docs for enabling and migrating | `README.md`; `docs/user-guide/cache.md`; `docs/user-guide/flag-reference.md` |
| NFR-01 | >=2x speedup on >=500 items | `BenchmarkFileBackend*` vs `BenchmarkSQLiteBackend*` |
| NFR-02 | No corruption on interrupted writes | `TestMigrateInterruptedRerunnable`; WAL + idempotent upserts |
| NFR-03 | Lossless round-trip | `TestSQLiteRoundTripMatchesFile`, `TestMigrateLossless`, `TestQueryEquivalence` |
| NFR-04 | CGO-free cross-build | `CGO_ENABLED=0` build for darwin/linux/windows (amd64/arm64) |
| NFR-05 | Single inspectable file | Single `cache.db`; `TestSQLitePersistsAcrossReopen` |
| NFR-06 | Consistent filesystem permissions | Database created under the cache directory with default permissions (not separately asserted) |

## Recorded NFR-01 Benchmark (500 issues)

| Query | File backend | SQLite backend | Speedup |
|---|---|---|---|
| list by state | 12.17 ms | 3.35 ms | 3.6x |
| list by author | 12.41 ms | 0.87 ms | 14.3x |
| search title/body | 12.94 ms | 6.48 ms | 2.0x |

All meet the >=2x target.
Search remains LIKE-based (FTS5 is deferred), so its speedup is the smallest.

## Out of Scope / Deferred

- FTS5 full-text search (reserved as a future additive `user_version` bump).
- Concurrent multi-process write guarantees beyond SQLite's defaults.
- Automatic dual-write keeping both backends in sync.
