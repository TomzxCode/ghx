---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Codebase Analysis: SQLite backend for issue/PR storage

## Overview

The feature touches a clean, well-factored storage layer: a single concrete `cache.Store` in `internal/cache/store.go` consumed through ~10 methods by the CLI commands, with all filtering performed in-memory in the `cmd` package after a full load.
The changeability outlook is good because the store already has a narrow, stable method set and the commands depend on it through one factory (`newStore`), so introducing a storage interface and a second implementation has a contained blast radius.
The main design tension is FR-03 (indexed queries rather than full scans): today list commands call `LoadAllIssues`/`LoadAllPRs` then `filterIssues`/`filterPRs` in memory, so honoring FR-03 fully requires extending the store contract with predicate-bearing query methods rather than only making the load faster.

## Scope of Analysis

Examined the cache store, the command layer that consumes it, the filtering logic, the data model, and the build/CI pipeline.

Search entry points:

- `rg` for store method usage (`LoadAllIssues`, `LoadAllPRs`, `LoadIssue`, `SaveIssue`, `SaveCacheInfo`, `ListRepos`, `IsCacheFresh`) across `cmd/` and `internal/`.
- Read `internal/cache/store.go` (full), `internal/github/types.go`, `cmd/cache.go`, `cmd/issue.go` (filter + list), `cmd/root.go` (`newStore`).

Out of scope: the GitHub API client (`internal/github`, `internal/gh`), the mock server (`internal/mockserver`), and review-thread/stash features (`cmd/pr_threads.go`, `internal/gh/stash.go`), which do not read or write the issue/PR cache.

## Relevant Existing Components

| Component | Path | Responsibility | Interaction |
|---|---|---|---|
| `cache.Store` | `internal/cache/store.go` | File-per-item JSON cache; CRUD for issues/PRs, cache-info metadata, freshness, repo listing | Refactor |
| `newStore()` factory | `cmd/root.go:79` | Constructs the store used by every command | Refactor |
| Fetch driver | `cmd/cache.go:40` | `cache` command: fetches via API, calls `SaveIssue`/`SavePR`/`SaveCacheInfoFull` per page, manages resume cursors | Reuse as-is |
| Issue list/view | `cmd/issue.go:98,155,163` | `LoadAllIssues` then in-memory `filterIssues`; `LoadIssue` for view | Extend |
| PR list/view | `cmd/pr.go` (parallel to issue) | `LoadAllPRs` then in-memory `filterPRs`; `LoadPR` for view | Extend |
| `filterIssues` / `filterPRs` | `cmd/issue.go:188` (+ pr.go) | In-memory predicate filtering (state, assignee, author, labels, milestone, search); skips mention/app (not in cache) | Reuse as-is |
| Data model | `internal/github/types.go` | `Issue`, `PullRequest`, `Comment`, `Actor`, `Label`, `Milestone` structs | Reuse as-is |
| `CacheInfo` | `internal/cache/store.go:14` | `cachedAt`, `duration`, `complete`, `issueCursor`, `prCursor` | Reuse as-is (semantics) |
| CLI flags/config | `cmd/root.go` | Global flags and `--cache-dir` | Extend |
| Build/CI | `Makefile`, `.github/workflows/build.yml` | Cross-compile darwin/linux/windows amd64+arm64, CGO-free | Read-only / verify |

## Dependency and Coupling Map

```
cmd/* ──newStore()──► cache.Store (concrete)
                        │ methods: SaveIssue/LoadIssue/LoadAllIssues
                        │          SavePR/LoadPR/LoadAllPRs
                        │          SaveCacheInfo(Full)/LoadCacheInfo/IsCacheFresh(WithDuration)
                        │          ListCachedRepos
                        ▼
                 github types (Issue, PullRequest, Comment, ...)
```

- Commands depend on `cache.Store` only through `newStore()` (cmd/root.go:79) and the method set above; no command reaches into the filesystem directly.
- Coupling is synchronous and in-process; there is no shared state beyond the on-disk cache directory.
- `filterIssues`/`filterPRs` are free functions in the `cmd` package operating on loaded slices; they are decoupled from the store.
- **Blast radius:** replacing the concrete `*cache.Store` with an interface ripples to every command's variable declarations and to `newStore()`'s return type, but not to call sites (method names are unchanged). Extending the read path with query methods touches only the list commands.

## Changeability Assessment

### cache.Store

- **Current state:** Concrete struct holding `baseDir`; file-per-item JSON via `os.ReadDir` + `json.Unmarshal`; atomic metadata writes (`atomicWrite`, store.go:310); `ListCachedRepos` walks three directory levels.
- **Change disposition:** Refactor
- **Rationale:** Extract a `Store` interface from the existing method set and keep this implementation as `fileStore` unchanged. This is the minimal seam that lets a SQLite implementation satisfy FR-01/FR-04 without altering current behavior, and it preserves the exact method names call sites already use.
- **Risk:** Low — the method set is small and stable; extraction is mechanical.
- **Constraints:** The file layout (`<base>/<host>/<owner>/<repo>/{issues,prs}/N.json`, `.cache_info.json`) and `CacheInfo` semantics must not change so existing caches keep working (FR-04 backward compatibility).

### newStore() factory

- **Current state:** Returns `*cache.Store`, selecting `NewStoreWithPath(cacheDir)` when `--cache-dir` is set else `NewStore()` (cmd/root.go:79).
- **Change disposition:** Refactor
- **Rationale:** Return the new `Store` interface and select the implementation from a backend flag/env (FR-04). This is the single injection point, so backend selection is centralized here.
- **Risk:** Low — one function; call sites use the returned value through methods only.
- **Constraints:** Must continue to honor `--cache-dir` for both backends.

### Issue/PR list path (cmd/issue.go, cmd/pr.go)

- **Current state:** `LoadAllIssues` returns the full slice, then `filterIssues` filters in memory (cmd/issue.go:98).
- **Change disposition:** Extend
- **Rationale:** To satisfy FR-03 (indexed queries, not full scans) the store must accept filter predicates. Add query methods (e.g. `QueryIssues(ctx, IssueQuery)`) to the interface; the file backend delegates to `LoadAllIssues` + the existing `filterIssues` (no behavior change), the SQLite backend translates the query into an indexed `WHERE` clause.
- **Risk:** Medium — this changes the most-used read path and must produce identical result sets (NFR-03). De-risk by keeping `filterIssues`/`filterPRs` as the canonical semantics and test oracle.
- **Constraints:** Result ordering and membership must match the in-memory filter exactly; `mention`/`app` filters remain unsupported from cache (cmd/issue.go:227).

### filterIssues / filterPRs

- **Current state:** Free functions in `cmd` implementing case-insensitive predicate matching (cmd/issue.go:188).
- **Change disposition:** Reuse as-is
- **Rationale:** Keep them as the file backend's filter implementation and as the reference oracle asserting SQLite query results match (FR-06 equivalence, NFR-03).
- **Risk:** Low.
- **Constraints:** Semantics (case-insensitive `EqualFold`, `hasAllLabels` AND-match, milestone title-or-number match) must be mirrored in the SQL predicates.

### Data model (github types)

- **Current state:** Plain structs with JSON tags (internal/github/types.go).
- **Change disposition:** Reuse as-is
- **Rationale:** SQLite is a storage alternative, not a model change (stated constraint). The schema maps these structs; migration round-trips them.
- **Risk:** Low.
- **Constraints:** No field additions/removals; nullable timestamps (`closedAt`, `mergedAt`) and optional `milestone` must round-trip.

### CacheInfo

- **Current state:** JSON metadata with resume cursors (store.go:14).
- **Change disposition:** Reuse as-is (semantics)
- **Rationale:** Both backends need the same metadata (freshness, completeness, cursors). The SQLite store persists an equivalent row in a metadata table; the struct stays shared.
- **Risk:** Low.
- **Constraints:** Cursor semantics (max `updatedAt` written, used for delta resume) must be preserved.

### CLI flags/config (cmd/root.go)

- **Current state:** Global flags including `--cache-dir`.
- **Change disposition:** Extend
- **Rationale:** Add a backend selector (flag and/or env, e.g. `--storage`/`GHX_STORAGE`) for FR-04, plus a DB path convention for FR-07 reporting.
- **Risk:** Low.
- **Constraints:** Default must keep current file-backend behavior unless SQLite is explicitly selected (open question: default on fresh installs).

### Build/CI (Makefile, .github/workflows/build.yml)

- **Current state:** CGO-free cross-compilation for 4 targets.
- **Change disposition:** Read-only / verify
- **Rationale:** Adding `modernc.org/sqlite` (pure Go) should not break CGO-free builds; NFR-04 requires verifying this in CI rather than assuming it.
- **Risk:** Low (pure-Go driver) but must be proven by a successful release build.
- **Constraints:** No new CGO dependency.

## Migration and Impact Considerations

- **File → SQLite (FR-05):** A new `migrate` command constructs both stores, iterates `fileStore.ListCachedRepos`, and for each repo writes `LoadAllIssues`/`LoadAllPRs` into the SQLite store inside a transaction. Idempotency via `INSERT OR REPLACE` keyed on `(host, owner, repo, number)`. An interrupted migration leaves a valid DB (transaction rollback) per NFR-02.
- **Backward compatibility:** The file backend remains the default unless opted in (pending open question), so existing caches and behavior are unchanged on upgrade. Selecting SQLite is per-invocation (or per-repo, open question), not destructive to file cache.
- **Rollout/de-risk:** Keep `filterIssues`/`filterPRs` results as a cross-check oracle in tests; run SQLite and file backends over identical fixtures and assert equal result sets (NFR-03, FR-06). The benchmark (NFR-01) double duty as a regression guard.
- **What else breaks:** Only variable declarations typed `*cache.Store` need to widen to the interface; no filesystem-layout or API-client changes. Integration tests (`internal/integration/integration_test.go:306,483`) construct file stores directly via `cache.NewStoreWithPath`, so they remain valid as long as that constructor is preserved (it stays the file-backend constructor).

## Assumptions About Existing Code

- `LoadAllIssues`/`LoadAllPRs` are the only read paths the list commands use (verified: cmd/issue.go:98, cmd/pr.go). To be re-confirmed during spec by grepping for any other `LoadAll*` callers.
- No other package imports `cache.Store` beyond `cmd/` (verified by `rg`: only `cmd/root.go`, `cmd/cache.go`, and `internal/integration/integration_test.go` reference it).
- `modernc.org/sqlite` builds CGO-free for all four release targets (high confidence from the existing-solutions survey; must be proven by a release build per NFR-04).

## Open Questions

1. Should the `Store` interface gain query methods (`QueryIssues`/`QueryPRs`) now, or ship v1 with `LoadAll*` backed by SQLite and defer pushed-down predicates until measured (FR-03 strictness)?
2. Is the backend selection global (one choice for all repos) or per-repo (a repo can be migrated independently)?
3. Should the SQLite DB live per-repo (`<repo>/cache.db`) or as one global DB keyed by `(host, owner, repo)` rows?
