---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
revision: 1
---

# Specification: SQLite backend for issue/PR storage

## Overview

Introduces a selectable SQLite storage backend alongside the existing file-per-item JSON cache by extracting a `Store` interface, adding a SQLite implementation backed by the pure-Go `modernc.org/sqlite` driver, and providing a migration command.
List/filter/search read paths are extended with predicate-bearing query methods so the SQLite backend serves indexed SQL queries instead of full scans, while the file backend delegates to its existing load-all-then-filter behavior.
The design preserves the current `github` data model, the on-disk file layout, and `CacheInfo` semantics unchanged, so existing caches keep working and the change is fully reversible.

## Architecture

```
cmd/* ──newStore()──► cache.Store (interface)
                        ├── fileStore   (existing, unchanged behavior)
                        └── sqliteStore (new, modernc.org/sqlite)
                              │
                              ▼
                        <cache-dir>/cache.db  (single file, rows keyed by host/owner/repo)

cache migrate ── fileStore.ListCachedRepos/LoadAll* ──► sqliteStore (transactional)
```

- A new `cache.Store` interface is extracted from the current concrete struct's method set; `fileStore` is the renamed existing implementation, `sqliteStore` is the new implementation.
- `newStore()` (cmd/root.go) selects the implementation from `--storage`/`GHX_STORAGE` (default `sqlite`) and still honors `--cache-dir`.
- The fetch driver (`cmd/cache.go`) is unchanged: it calls the same `Save*`/`SaveCacheInfoFull` methods through the interface.
- List commands call new `QueryIssues`/`QueryPRs` methods; `fileStore` implements them as `LoadAll*` + the existing `filterIssues`/`filterPRs`, `sqliteStore` translates them to indexed SQL.

## Data Models

### SQLite schema (`internal/cache/schema.sql`, embedded via `go:embed`)

Scalar filter columns are stored as typed, indexed columns; nested arrays (comments, labels, assignees) and optional structs (milestone) are stored as JSON text to guarantee lossless round-trip of the `github` types (NFR-03).
Times are stored as RFC3339Nano strings; nullable times (`closedAt`, `mergedAt`) are SQL `NULL`.
A `row_mtime` column on each item table records when the row was last written, preserving the `LoadIssue`/`LoadPR` modification-time contract.

#### `issues`

| Column | Type | Constraints | Description |
|---|---|---|---|
| host | TEXT | PK part, not null | API host (e.g. `github.com`) |
| owner | TEXT | PK part, not null | Owner login |
| repo | TEXT | PK part, not null | Repository name |
| number | INTEGER | PK part, not null | Issue number |
| title | TEXT | not null | Issue title |
| state | TEXT | not null | `open` / `closed` |
| author_login | TEXT | not null | Author login |
| assignees | TEXT | not null | JSON `[]Actor` |
| labels | TEXT | not null | JSON `[]Label` |
| milestone | TEXT | nullable | JSON `*Milestone` |
| created_at | TEXT | not null | RFC3339Nano |
| updated_at | TEXT | not null | RFC3339Nano |
| closed_at | TEXT | nullable | RFC3339Nano |
| url | TEXT | not null | Issue URL |
| body | TEXT | not null | Issue body |
| comment_count | INTEGER | not null | Comment count |
| comments | TEXT | not null | JSON `[]Comment` |
| row_mtime | TEXT | not null | Row last-write time (RFC3339Nano) |

Primary key: `(host, owner, repo, number)`.
Indexes: `(host, owner, repo, state)`, `(host, owner, repo, author_login)`, `(host, owner, repo, updated_at)`.

#### `pull_requests`

Same shape as `issues` plus PR-specific columns:

| Column | Type | Constraints | Description |
|---|---|---|---|
| is_draft | INTEGER | not null | 0/1 |
| base_ref_name | TEXT | not null | Base ref |
| head_ref_name | TEXT | not null | Head ref |
| merged_at | TEXT | nullable | RFC3339Nano |
| review_decision | TEXT | nullable | Review decision |

Primary key and indexes mirror `issues` (state, author_login, updated_at).

#### `cache_meta`

| Column | Type | Constraints | Description |
|---|---|---|---|
| host | TEXT | PK part, not null | API host |
| owner | TEXT | PK part, not null | Owner login |
| repo | TEXT | PK part, not null | Repository name |
| cached_at | TEXT | not null | RFC3339Nano |
| duration | INTEGER | not null | Freshness minutes |
| complete | INTEGER | not null | 0/1 |
| issue_cursor | TEXT | nullable | RFC3339Nano resume cursor |
| pr_cursor | TEXT | nullable | RFC3339Nano resume cursor |

Primary key: `(host, owner, repo)`. Maps 1:1 to `CacheInfo`.

#### Schema versioning

`PRAGMA user_version = 1;` set at creation; future additive migrations check and advance `user_version`.

### Connection settings

- `PRAGMA journal_mode = WAL;` for read-during-write concurrency (NFR-02).
- `PRAGMA foreign_keys = ON;` (no FKs today, but reserved).
- `PRAGMA synchronous = NORMAL;` (safe under WAL).

## API Contracts

### `cache.Store` interface (`internal/cache/store.go`)

Existing methods (unchanged signatures), plus new query and lifecycle methods:

```go
type Store interface {
    // Existing
    SaveIssue(host, owner, repo string, issue *github.Issue) error
    LoadIssue(host, owner, repo string, number int) (*github.Issue, time.Time, error)
    LoadAllIssues(host, owner, repo string) ([]*github.Issue, error)
    SavePR(host, owner, repo string, pr *github.PullRequest) error
    LoadPR(host, owner, repo string, number int) (*github.PullRequest, time.Time, error)
    LoadAllPRs(host, owner, repo string) ([]*github.PullRequest, error)
    SaveCacheInfo(host, owner, repo string, duration int) error
    SaveCacheInfoFull(host, owner, repo string, info *CacheInfo) error
    LoadCacheInfo(host, owner, repo string) (*CacheInfo, error)
    IsCacheFresh(host, owner, repo string) (bool, error)
    IsCacheFreshWithDuration(host, owner, repo string, duration int) (bool, error)
    ListCachedRepos() ([]CachedRepo, error)

    // New
    QueryIssues(host, owner, repo string, q IssueQuery) ([]*github.Issue, error)
    QueryPRs(host, owner, repo string, q PRQuery) ([]*github.PullRequest, error)

    // Backend identity (FR-07)
    Kind() string
    Location() string
    Close() error
}
```

`fileStore.QueryIssues` = `LoadAllIssues` + `filterIssues`; `sqliteStore.QueryIssues` pushes the scalar, indexed predicates (repo, state, author) into the SQL `WHERE` to produce a candidate set, then applies label/milestone/search matching in Go by reusing the same predicate helpers (`hasAllLabels`, milestone title-or-number) to guarantee exact equivalence with the in-memory filter.
(Equivalently, label matching may use SQLite JSON1 `json_each` over the `labels` column; the candidate-set approach is the fallback that keeps semantics provably identical.)
`Kind()` reports `file` or `sqlite`; `Location()` reports the cache root (file) or database path (sqlite), surfaced per FR-07.
`Close()` is a no-op for `fileStore` and closes the DB connection for `sqliteStore`.

### Query structs

```go
type IssueQuery struct {
    State, Assignee, Author string
    Labels                  []string
    Milestone, Search       string
}

type PRQuery struct {
    State, Assignee, Author string
    Labels                  []string
    Milestone, Search       string
    BaseRef, HeadRef        string
    IsDraft                 *bool
}
```

Predicate semantics mirror `filterIssues`/`filterPRs` exactly (case-insensitive `EqualFold`, labels AND-match via `hasAllLabels`, milestone title-or-number match); `mention`/`app` remain unsupported from cache.

### Backend selection

| Source | Key | Values | Default |
|---|---|---|---|
| Flag | `--storage` | `file`, `sqlite` | `sqlite` |
| Env | `GHX_STORAGE` | `file`, `sqlite` | `sqlite` |

Flag overrides env; env overrides default. DB location: a single `<cache-dir>/cache.db`, with rows keyed by `(host, owner, repo)`.

### New CLI command

`cache migrate` — migrates the current repository's file cache into the SQLite backend.

| Aspect | Detail |
|---|---|
| Flags | `--repo` (existing), `--storage` (target, defaults `sqlite`), `--force` (overwrite existing rows) |
| Behavior | Iterates `LoadAllIssues`/`LoadAllPRs`, writes rows via `INSERT OR REPLACE` in a single transaction per repo; copies `CacheInfo` to `cache_meta` |
| Idempotency | Re-running upserts by PK; with `--force` it overwrites, without `--force` it skips rows whose `updated_at` is unchanged |
| Errors | Any error rolls back the transaction, leaving a valid DB (NFR-02) |

## Sequences

### List issues with SQLite

```
issue list --storage sqlite
   │
   ├── newStore() ── opens <repo>/cache.db (WAL)
   ├── QueryIssues(host, owner, repo, IssueQuery{State:"open",...})
   │       └── SELECT ... WHERE host=? AND owner=? AND repo=? AND state=?
   │             (indexed; no full scan)
   └── render results ── Close()
```

### Cache fetch with SQLite

```
cache --storage sqlite
   │
   ├── LoadCacheInfo ── SELECT from cache_meta
   ├── (stale?) ── GitHub API fetch ── SaveIssue per page ── INSERT OR REPLACE
   │                                  └── SaveCacheInfoFull ── UPSERT cache_meta (cursor)
   └── SaveCacheInfo (complete=true) ── UPSERT cache_meta
```

### Migration

```
cache migrate --repo owner/repo
   │
   ├── open fileStore + sqliteStore
   ├── BEGIN TRANSACTION
   ├── for each issue/PR in fileStore.LoadAll*: INSERT OR REPLACE into sqlite
   ├── UPSERT cache_meta from file CacheInfo
   ├── COMMIT  (or ROLLBACK on any error)
   └── report counts; file cache left untouched
```

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| SQLite driver | `modernc.org/sqlite` | Only mature pure-Go driver; satisfies NFR-04 (CGO-free cross-build) |
| Nested fields | JSON columns | Lossless round-trip of `github` types (NFR-03) without a large normalized schema; filter columns kept scalar and indexed for FR-03 |
| Filter strategy | Predicate query methods now (`QueryIssues`/`QueryPRs`) | Satisfies FR-03 strictly; file backend reuses `filterIssues` so semantics stay canonical |
| Search (FR-06) | `LIKE` on title/body for v1; FTS5 reserved as a future additive `user_version` bump | Keeps the initial schema simple; FTS5 deferred until search volume justifies it (open question) |
| DB layout | Single `<cache-dir>/cache.db`, rows keyed by `(host, owner, repo)` | Simpler than per-repo files; `ListCachedRepos` and migration are trivial; isolation is per-key, not per-file |
| Concurrency | WAL mode + transactional writes | Readers do not block the writer; interrupted writes/migrations roll back (NFR-02) |
| Schema evolution | `PRAGMA user_version` + embedded `schema.sql` | Lightweight; additive migrations advance the version without a migration framework |
| Default backend | `sqlite` | Per maintainer decision: faster by default; the file backend remains selectable via `--storage file` |
| Result-set equivalence | `filterIssues`/`filterPRs` kept as test oracle | Guarantees SQLite query results match the in-memory filter (FR-06, NFR-03) |

## Risks and Unknowns

1. Binary-size increase from `modernc.org/sqlite` is unmeasured; must be accepted before release (feasibility condition).
2. `LIKE`-based search performance on large bodies may be insufficient, requiring FTS5 sooner than expected.
3. Label/milestone filtering correctness is the highest-risk predicate; the design mitigates it by reusing the canonical Go predicate helpers on the SQL candidate set and asserting equality against `filterIssues`/`filterPRs` in tests.
4. Concurrent processes writing the same database rely on SQLite file locking; acceptable for a single-user CLI but untested under parallel agent loads.

## Out of Scope

- Changing the in-memory `github.Issue`/`github.PullRequest` types.
- Deprecating or removing the file-based backend (SQLite is the default; the file backend remains selectable).
- Full-text search via FTS5 in v1 (reserved as a future additive change).
- Automatic dual-write keeping both backends in sync (backend is a per-invocation choice).
- Multi-process locking guarantees beyond SQLite defaults.
