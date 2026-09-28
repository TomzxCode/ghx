---
date: "2026-07-04"
scope: "FEAT-0008-sqlite-backend"
entry: "code"
flags: "fix: true (mechanical alignment), create-issues: false"
status: complete
---

# Propagation Report

**Date:** 2026-07-04
**Scope:** FEAT-0008-sqlite-backend
**Entry:** code (the SQLite backend implementation; commits `2c3d4f0`..`f9aa6c9`)

## Consistency Status

| Feature | Downward updates | Upward drift | IDs | Review order | Status |
|---|---|---|---|---|---|
| FEAT-0008-sqlite-backend | 0 needed (dependents already updated during implementation) | 2 findings (1 fixed, 1 open) | clean | 1 inversion (fixed) | in sync |

## Updates (downward)

None required.
The default-backend change was propagated to `tests.md`, docs, requirements, specification, and plan during implementation, so no dependent still described the old version once the pass ran.

## Questions Raised

None.
Every disagreement had a single resolution (align the spec listing to the implemented interface; add the missing tests review), so no question needed a human decision.

## Traceability Matrix (per feature)

### FEAT-0008-sqlite-backend

| FR / NFR | Spec section | Plan phase | Task(s) | Test(s) | Code location | Docs | Issue AC |
|---|---|---|---|---|---|---|---|
| FR-01 | Data Models / Store CRUD | Ph2 | (none) | `sqlite_store_test.go` | `internal/cache/sqlite_store.go` | cache.md | AC1 |
| FR-02 | SQLite schema | Ph2 | (none) | `sqlite_store_test.go` | `internal/cache/schema.sql` | cache.md | AC2 |
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
| NFR-05 | Data Models (single file) | Ph2 | (none) | `sqlite_store_test.go` (reopen) | `sqlite_store.go` | cache.md | AC1 |
| NFR-06 | (none) | (none) | (none) | (not asserted) | `sqlite_store.go` | (n/a) | AC1 |

Every task column is empty because the task decomposition was intentionally skipped (the user directed implementation from the plan).

## Summary

| Class | Critical | High | Medium | Low | Total |
|---|---|---|---|---|---|
| Orphan upstream | 0 | 0 | 1 | 0 | 1 |
| Orphan downstream | 0 | 0 | 1 | 1 | 2 |
| Broken reference | 0 | 0 | 0 | 0 | 0 |
| Status inversion | 0 | 0 | 0 | 1 | 1 |
| **Total** | **0** | **0** | **2** | **2** | **4** |

## Findings

### 1. Specification interface drifted from the implemented interface
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (code without spec backing) / spec drift
**Severity:** Low
**Direction:** Upward
**Edge(s):** Specification ↔ Requirements, Code ↔ Specification
**Location:** `specification.md` §API Contracts
**Finding:** The spec listed `QueryIssues`/`QueryPRs` with a `ctx context.Context` parameter (the implementation has none) and omitted `Kind()`/`Location()` (added in Phase 4 for FR-07).
**Impact:** The spec no longer described the shipped interface; a future implementation following the spec would not compile against the code.
**Recommendation:** Align the spec listing to the implemented interface.
**Resolution:** Fixed (applied).

### 2. `tests.md` marked approved with no `review-tests` artifact
**Feature:** FEAT-0008-sqlite-backend
**Class:** Status inversion (review-order)
**Severity:** Low
**Direction:** Upward
**Edge(s):** Tests ↔ Task (review-approval monotonicity)
**Location:** `tests.md` / missing `review-tests.md`
**Finding:** `tests.md` was authored with `status: approved` during Phase 5 without a matching review pass, while the code below it was already implemented.
**Impact:** The tests artifact was self-approved; the review loop for it was skipped.
**Resolution:** Fixed (added `review-tests.md`, verdict approved).

### 3. No task decomposition (`tasks/` absent)
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan upstream
**Severity:** Medium
**Direction:** Upward
**Edge(s):** Plan ↔ Tasks, Tests ↔ Tasks, Tasks ↔ Code
**Location:** `.sdlc/features/FEAT-0008-sqlite-backend/tasks/`
**Finding:** The plan's phases have no task files, so the Plan↔Tasks and Tests↔Tasks edges are absent. This was an explicit user decision to implement directly from the plan.
**Impact:** No per-task progress tracking or task↔test traceability; the plan phases are the de facto tasks.
**Recommendation:** Accept as-is (plan phases served as the breakdown), or backfill `tasks/` from the plan phases if task-level tracking is wanted. Human decision.

### 4. FR-07 reporting not covered by an automated test
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (behavior without a test)
**Severity:** Medium
**Direction:** Downward
**Edge(s):** Code ↔ Tests
**Location:** `cmd/cache.go` (`Using sqlite cache backend at ...`)
**Finding:** `Kind()` is asserted in `cmd/storage_test.go`, but the human-facing backend/path output line and `Location()` are not asserted.
**Impact:** A regression in the FR-07 output would not be caught by CI.
**Recommendation:** Add an assertion on the printed output and `Location()`. Human decision (small).

### 5. `cache migrate` command handler not covered by a command-level test
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (behavior without a test)
**Severity:** Low
**Direction:** Downward
**Edge(s):** Code ↔ Tests
**Location:** `cmd/cache.go` (`runCacheMigrate`)
**Finding:** `cache.Migrate` is well tested, but `runCacheMigrate` (repo selection, output, multi-repo loop) is not.
**Impact:** Low; the handler is a thin wrapper over tested logic. Covered manually end to end.
**Recommendation:** Add a command-level migration test. Human decision (small).

### 6. NFR-06 (filesystem permissions) not asserted
**Feature:** FEAT-0008-sqlite-backend
**Class:** Orphan downstream (behavior without a test)
**Severity:** Low
**Direction:** Downward
**Edge(s):** Code ↔ Tests
**Location:** `internal/cache/sqlite_store.go`
**Finding:** The database file inherits the cache directory's permissions; no test asserts this.
**Impact:** Negligible for a single-user local CLI; documented as not asserted in `tests.md`.
**Recommendation:** Accept as documented, or add a permissions test. Human decision.

## Fixes Applied

| Finding / Update | Fix applied | Files changed |
|---|---|---|
| 1. Spec interface drift | Aligned the spec listing to the implemented interface (`ctx` removed; `Kind`/`Location` added); bumped `revision: 1`; re-validated `review-specification.md` | `.sdlc/.../specification.md`, `.sdlc/.../review-specification.md` |
| 2. Missing tests review | Authored `review-tests.md` (verdict approved) | `.sdlc/.../review-tests.md` |

## Recommended Actions

| Priority | Action | Owner decision |
|---|---|---|
| Medium | Add an automated assertion for FR-07 backend/path output and `Location()` (finding 4) | implement |
| Medium | Decide whether to backfill `tasks/` from the plan phases (finding 3) | accept as-is / backfill |
| Low | Add a command-level `cache migrate` test (finding 5) | implement |
| Low | Accept the documented NFR-06 gap or add a permissions test (finding 6) | accept / implement |
