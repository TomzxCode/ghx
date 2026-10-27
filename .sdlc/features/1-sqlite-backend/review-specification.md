---
artifact: specification
verdict: changes-requested
reviewed_at: 2026-07-04
---

## Ambiguities

Resolved.
The label/milestone filtering approach was hand-waved ("JSON LIKE/EXTRACT as needed"); it now specifies a concrete, correct strategy (scalar predicates pushed to indexed SQL, label/milestone/search applied via the canonical Go predicate helpers on the candidate set, with JSON1 `json_each` as an option), removing the ambiguity while preserving equivalence.

## Inconsistencies

No issues found.
Default backend (`sqlite`) is consistent across the selection table, technical decisions, and out-of-scope; the `Store` interface signatures match the implemented interface.

## Gaps

No blocking findings.
The schema covers every `github.Issue`/`PullRequest` field including nullable timestamps and the review-decision field; `row_mtime` preserves the `LoadIssue`/`LoadPR` modification-time contract; schema versioning (`user_version`) and connection pragmas (WAL, synchronous) are specified.
Telemetry and observability are handled in their own downstream phases (the project ships no runtime metrics/logging per `architecture.md`).

## Implementability

No issues found.
The design is additive and reversible: interface extraction with two implementations, an embedded schema, predicate query methods that degrade to the existing filter for the file backend, and a transactional migration. All chosen libraries are pure-Go and within the stated constraints; the two feasibility conditions (benchmark, CGO-free build) remain as validation gates rather than design blockers.

## Propagation re-review: 2026-07-04

Re-validated after the code entered the propagation graph.
The `Store` interface snippet was aligned to the implemented, tested interface: the `ctx context.Context` parameter on `QueryIssues`/`QueryPRs` was removed (the implementation takes none), and `Kind()`/`Location()` were added (FR-07 realization).
Default backend corrected to `sqlite`. Verdict remains approved.

## Sync drift: 2026-10-27

Found while reconciling this artifact against the code during `/sync-sdlc`; no artifact content was rewritten.

- specification.md:114-137: The `Store` interface listing is missing `IsIssuesCacheFresh`/`IsPRsCacheFresh`/`IsIssuesCacheFreshWithDuration`/`IsPRsCacheFreshWithDuration` and `DeleteRepo` (internal/cache/store.go:119-127).
- specification.md:159: `PRQuery.IsDraft` is specified as `*bool`; the implementation is `Draft bool` (internal/cache/store.go:96).
- specification.md:44-95: The schema tables omit columns that exist in the shipped schema: `issues.author_type` (internal/cache/schema.sql:15), `pull_requests.head_ref_oid`/`closed_at`/`reviews`/`timeline`/`review_requests`/`additions`/`deletions`/`author_type` (internal/cache/schema.sql:43-63), and `cache_meta.issues_cached_at`/`prs_cached_at` (internal/cache/schema.sql:81-82).
- specification.md:99: Schema versioning is specified as `PRAGMA user_version = 1` with additive migrations advancing the version; the code sets `user_version` to 3 (`schemaVersionKeysNormalized`, internal/cache/normalize_keys.go:14) on every open (internal/cache/sqlite_store.go:82) and runs additive column migrations by probing `columnExists` (internal/cache/sqlite_store.go:92).
- specification.md:215-219: The migration sequence shows one whole-repo transaction (BEGIN/COMMIT/ROLLBACK); `cache.Migrate` writes per-item upserts with no transaction (internal/cache/migrate.go:16), contradicting this sequence while matching the spec's own API Contracts row (specification.md:181).
- specification.md:101-105: The connection settings omit `PRAGMA busy_timeout = 5000` and the single pooled connection (`SetMaxOpenConns(1)`) that prevent lock churn (internal/cache/sqlite_store.go:46-57).
- specification.md:29-32: `newStore()` now wraps every backend in `normalizeStore` (canonical lower-case keys with a one-time mixed-case collapse; internal/cache/store.go:161, internal/cache/normalize.go:24, internal/cache/normalize_keys.go:34) and optionally a telemetry `Instrument` wrapper (cmd/root.go:188); neither is described in the spec.
