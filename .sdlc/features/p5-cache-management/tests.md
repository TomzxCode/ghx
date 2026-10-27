---
title: "Cache Management"
status: done
---

# Test plan: Cache Management

## Scope

Tests cover the `ghx cache` command, the read-command cache paths (fresh serving, item freshness, backfill), the cache stores (file and SQLite backends), key normalization, per-type freshness, migration, and backend selection.
Retry behavior of the GraphQL client behind `--retries` is owned by the API client tests in `internal/github` and listed here only as supporting evidence.
Performance against real GitHub rate limits (NFR-01) is not tested; the mock server has no rate-limit budget.

## Unit tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | File backend saves and loads issues, PRs, and all items | `sampleIssue`/`samplePR` via `newTempStore` (`internal/cache/store_test.go:50,81,101`) | Round-trip preserves every field |
| TC-2 | Cache info save/load, including resume cursors and complete flag | `CacheInfo` with cursors (`internal/cache/store_test.go:132,181`) | Fields persist in `.cache_info.json` |
| TC-3 | Full-cache freshness requires the complete flag and honors the duration | Fresh and stale infos (`internal/cache/store_test.go:151,168,210`) | Incomplete or expired cache is never fresh |
| TC-4 | Per-type freshness: a partial `--type` run freshens only its type | Infos with only `issuesCachedAt`/`prsCachedAt` (`internal/cache/per_type_freshness_test.go:11`) | Each type judged by its own timestamp |
| TC-5 | Item freshness window for a cache without metadata | `IsItemFresh` cases (`internal/cache/item_freshness_test.go:12`) | Served within the window, stale outside, zero duration never fresh |
| TC-6 | SQLite round-trip matches the file backend, including actor type | Same fixtures through both backends (`internal/cache/sqlite_store_test.go:151,293`) | Identical items and freshness answers |
| TC-7 | SQLite cache persists across close and reopen | Two `NewSQLiteStore` opens (`internal/cache/sqlite_store_test.go:340`) | Items and metadata survive the reopen |
| TC-8 | Additive SQLite schema migration (head_ref_oid, per-type freshness columns) | Legacy-schema database (`internal/cache/sqlite_store_test.go:376,475`) | Columns added, old data readable, freshness falls back to `cached_at` |
| TC-9 | SQLite lists every cached repository with counts and metadata | Multi-repo database (`internal/cache/sqlite_store_test.go:412`) | One row per repository |
| TC-10 | DeleteRepo removes one repository and reports a repeat as false | Two repositories per backend (`internal/cache/store_test.go:229`, `internal/cache/sqlite_store_test.go:649`) | Other repository untouched |
| TC-11 | File-to-SQLite migration is lossless, idempotent, and re-runnable after interruption | Populated file store (`internal/cache/migrate_test.go:37,75,104`) | Counts match, source intact, destination valid |
| TC-12 | Repository keys normalize to lower case, and pre-normalization data is collapsed | Mixed-case keys per backend (`internal/cache/normalize_test.go:31,79,134`) | One entry per repository, newest write wins |
| TC-13 | In-memory issue and PR filters (state, author, assignee, labels, milestone, refs, draft, search) | Table-driven queries (`internal/cache/filter_test.go:42-201`) | Only matching items returned |
| TC-14 | SQLite query results equal the file backend's and use indexes | Same queries on both backends (`internal/cache/query_equiv_test.go:96,225`) | Identical sets; no full-table plan |
| TC-15 | `--since` parsing: RFC3339, date-only, naive datetimes, whitespace | `parseSinceDate` table (`cmd/cache_since_test.go:18,85`) | Parsed values or a named error |

## Integration tests

Command-level tests run against `internal/mockserver` with a temp cache directory.

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-16 | Plain run delta-refreshes a fresh complete cache | Complete cache from a cold run (`cmd/cache_fresh_test.go:14`) | Both types delta-fetched, window advanced, still complete |
| TC-17 | `--if-stale` no-op on a fresh cache and fetches when stale | Fresh cache, then aged cache (`cmd/cache_fresh_test.go:69,89`) | Zero requests while fresh; delta when stale |
| TC-18 | Issues and PRs fetch concurrently; a failing portion keeps the other portion's writes | Mock server with a failing phase (`cmd/cache_parallel_test.go:117,153`) | Both portions ran; partial results kept |
| TC-19 | Combined progress bar tracks concurrent portions on one bar | `combinedBar` events (`cmd/cache_parallel_test.go:201`) | Counts and totals sum across portions |
| TC-20 | `--since` refreshes a complete cache, is ignored when incomplete, and rejects `--force` | Complete and incomplete caches (`cmd/cache_since_test.go:192,270,308`) | Windowed delta; full fetch resumed; error on the conflict |
| TC-21 | `--type issues`/`--type prs` refresh one portion and never complete an incomplete cache | Single-type runs (`cmd/cache_type_test.go:13,28,62,97`) | Only the fetched type freshens; complete stays false |
| TC-22 | Backend selection: sqlite default, file selectable, invalid rejected | `--storage`/`GHX_STORAGE` matrix (`cmd/storage_test.go:35,52,67,78`) | Correct `Store.Kind()`; error names the fix |
| TC-23 | `ghx cache migrate` copies a file cache into SQLite | File cache with items (`cmd/storage_test.go:109`) | Items and metadata present in SQLite |
| TC-24 | Read commands backfill the cache without clobbering existing entries | List against empty and populated caches (`cmd/readcache_test.go:65,95,124`) | Fetched items stored; existing records kept |
| TC-25 | Views serve from the fresh full cache and from fresh items, refetch stale items, and report other-type numbers | Item/full-cache age variants (`cmd/readcache_test.go:190,223,248,283,318,352,388`) | Correct source per scenario; PR/issue redirect error |

## End-to-end tests

Run in `internal/integration` through the compiled CLI against the mock server.

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-26 | A fresh full cache serves issue lists with zero API calls | Warm `cache`, list twice (`internal/integration/cli_test.go:99`) | Second list makes 0 requests, same rows |
| TC-27 | A fresh full cache serves PR lists with zero API calls | Warm `cache`, list twice (`internal/integration/cli_test.go:185`) | Second list makes 0 requests |
| TC-28 | Views are served from the cache after warm-up, including comments and the issue/PR redirect | Warm `cache`, view items with flags (`internal/integration/cli_test.go:234`) | 0 requests per view; correct output; PR number redirects to `pr view` |

## Edge cases and failure scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-29 | Interrupted fetch leaves a valid cache and a persisted resume cursor | `SaveCacheInfoFull` keeps cursors after every page (TC-2, TC-18) |
| TC-30 | Migration interrupted midway | Destination stays valid and the rerun completes (TC-11) |
| TC-31 | Mixed-case repository spellings | Collapsed to one entry, newest write wins (TC-12) |
| TC-32 | `--since` on an incomplete cache | Ignored with a notice; the full fetch resumes (TC-20) |
| TC-33 | `--force` combined with `--since` | Error naming both effects (TC-20) |
| TC-34 | Invalid `--type` or `--storage` values | Errors naming the accepted values (TC-21, TC-22) |
| TC-35 | Item older than the configured duration with no full cache | Refetched from the API (TC-25) |
| TC-36 | Requested number belongs to the other type | Error pointing at `issue view`/`pr view` (TC-25, TC-28) |
| TC-37 | Legacy cache written before per-type freshness columns | Freshness falls back to `cached_at` (TC-8) |
| TC-38 | Repeated migration or repeated delete | Idempotent; second delete reports false (TC-10, TC-11) |

## Test infrastructure

- `internal/mockserver` builds GraphQL scenarios with fixed timestamps (`mockserver.NewScenarioBuilder`, `WithNow`).
- Command tests isolate the package-level flags with save/restore helpers and point `--api-url`, `--cache-dir`, and `--storage file` at temp directories (`cmd/cache_since_test.go:100`, `cmd/readcache_test.go:37`, `cmd/storage_test.go:18`).
- Store tests use `t.TempDir()` and the `newTempStore` helper; `runCLI` executes the built binary in `internal/integration`.
- Supporting retry evidence: `internal/github/transient_test.go:35,189`, `internal/github/ratelimit_test.go:165`.

## Coverage matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-1, TC-16, TC-24, TC-26 |
| FR-02 | TC-1, TC-2, TC-6, TC-22 (file backend; the default backend is SQLite) |
| FR-03 | TC-2, TC-6, TC-8 |
| FR-04 | TC-3, TC-4, TC-25, TC-35 |
| FR-05 | TC-18, TC-20, TC-21 |
| FR-06 | TC-16, TC-20, TC-29 |
| FR-07 | TC-19, TC-16 (status lines and per-phase messages) |
| FR-08 | TC-12, TC-31 |
| FR-09 | TC-17 |
| NFR-01 | Not tested (rate-limit budget out of mock scope; concurrency covered by TC-18) |
| NFR-02 | TC-1, TC-29, TC-30 (atomic file writes and resumable cursors) |
