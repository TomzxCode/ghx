---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Implementation Plan: SQLite backend for issue/PR storage

## Goal

Deliver a selectable, CGO-free SQLite storage backend for cached issues and PRs that makes list/filter/search on large repositories faster than the file backend, with a lossless migration path and no change to existing behavior when SQLite is not selected.
This plan implements the approved specification and satisfies the two feasibility gating conditions (baseline benchmark and CGO-free cross-build) as its first phase.

## Phases

### Phase 0: Baseline & validation spike

**Goal:** Confirm the two feasibility conditions before committing to the full build.
**Effort:** 0.5 day
**Depends on:** None

**Deliverables:**
- [ ] Add `modernc.org/sqlite` dependency; prove `go build` and the 4-target release cross-compilation succeed CGO-free (NFR-04)
- [ ] Write a baseline `go test -bench` benchmark for the file backend's `LoadAllIssues`/`filterIssues` over a generated large fixture (>=500 items) and record timings (NFR-01 gate)
- [ ] Decision gate: proceed only if the baseline shows material latency and the cross-build is clean

### Phase 1: Storage interface extraction

**Goal:** Introduce the backend-selection seam with zero behavior change.
**Effort:** 0.5 day
**Depends on:** Phase 0

**Deliverables:**
- [ ] Extract `cache.Store` interface from the current method set; rename the existing struct to `fileStore` (`internal/cache/store.go`)
- [ ] Update `newStore()` (cmd/root.go) to return the interface; widen `*cache.Store` variable declarations in `cmd/` and `internal/integration` tests
- [ ] Full existing test suite passes unchanged (no behavior change)

### Phase 2: SQLite store core (schema + CRUD)

**Goal:** A working SQLite backend that round-trips all fields losslessly.
**Effort:** 1.5 days
**Depends on:** Phase 1

**Deliverables:**
- [ ] Embedded `internal/cache/schema.sql` (`issues`, `pull_requests`, `cache_meta`, `PRAGMA user_version = 1`) via `go:embed`
- [ ] `sqliteStore` implementing `Save`/`Load`/`LoadAll` for issues and PRs, `CacheInfo` methods, `ListCachedRepos`, and `Close`; WAL + `synchronous=NORMAL` pragmas
- [ ] Round-trip tests asserting every field (incl. nullable timestamps, milestone, comments, labels, assignees) equals the file backend (NFR-03)

### Phase 3: Query methods (indexed reads)

**Goal:** Serve list/filter/search from indexed SQL rather than full scans.
**Effort:** 1.5 days
**Depends on:** Phase 2

**Deliverables:**
- [ ] Add `QueryIssues`/`QueryPRs` + `IssueQuery`/`PRQuery` to the `Store` interface
- [ ] `fileStore.Query*` delegates to `LoadAll*` + existing `filterIssues`/`filterPRs`
- [ ] `sqliteStore.Query*` pushes scalar predicates (repo, state, author) to indexed SQL; applies label/milestone/search via the canonical Go helpers on the candidate set
- [ ] Differential equivalence tests: SQLite query output equals file-backend output across fixture matrices (FR-03, FR-06, NFR-03)

### Phase 4: Backend selection + migration

**Goal:** Users can opt in to SQLite and migrate existing caches.
**Effort:** 1 day
**Depends on:** Phase 3

**Deliverables:**
- [ ] `--storage` flag and `GHX_STORAGE` env in `cmd/root.go`; `newStore()` selects backend (default `file`)
- [ ] `cache migrate` command: transactional, `INSERT OR REPLACE`, idempotent, copies `CacheInfo` to `cache_meta` (FR-05, NFR-02)
- [ ] Backend type and DB path surfaced in cache info/status output (FR-07)
- [ ] Migration tests: lossless round-trip, idempotent re-run, interrupted-migration leaves valid DB

### Phase 5: Wire list commands, docs, finalize benchmark

**Goal:** Feature complete, documented, and verified against targets.
**Effort:** 1 day
**Depends on:** Phase 4

**Deliverables:**
- [ ] `issue list` / `pr list` use `Query*` through the selected backend
- [ ] README documentation: enabling SQLite and migrating existing caches (FR-08)
- [ ] Finalize benchmark recording file vs SQLite timings; confirm >=2x speedup on >=500 items (NFR-01)
- [ ] Full test suite, `go vet`, linter, and 4-target cross-build green; FR/NFR coverage matrix checked

## Milestones

| Milestone | Phase | Success Criteria |
|---|---|---|
| M1: Feasibility validated | Phase 0 | CGO-free cross-build passes; baseline benchmark recorded and material |
| M2: Seam in place | Phase 1 | Interface extracted; all existing tests pass unchanged |
| M3: SQLite round-trips | Phase 2 | Lossless round-trip of all fields vs file backend |
| M4: Indexed reads | Phase 3 | Query results equal file backend; reads use indexed SQL |
| M5: Selectable + migrable | Phase 4 | `--storage sqlite` works; `cache migrate` is lossless and idempotent |
| M6: Feature complete | Phase 5 | Docs + benchmark target met; full suite + cross-build green |

## Dependencies

| Dependency | Type | Owner | Risk if Delayed |
|---|---|---|---|
| `modernc.org/sqlite` builds CGO-free for 4 targets | External | Maintainer | Blocks the feature; fallback would require CGO cross-toolchains (rejected) |
| Existing `cache.Store` method set stability | Internal | Maintainer | None expected; verified in codebase analysis |
| Large-fixture generator (mock server) | Internal | Maintainer | Already exists (`internal/mockserver` simulation generator) |

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| CGO-free build fails for a target | Low | High | Phase 0 spike gates the work before commitment |
| Baseline shows no material latency gain | Low | High | NFR-01 gate in Phase 0; if not met, revisit scope with the issue author |
| Label/milestone SQL predicates diverge from in-memory filter | Medium | Medium | Reuse canonical Go helpers on candidate set; differential equivalence tests |
| Binary-size increase unacceptable | Low | Medium | Measure in Phase 0; acceptable threshold decided before release |
| `LIKE` search too slow at scale | Medium | Low | FTS5 reserved as a future additive `user_version` bump |
| Migration corruption on interruption | Low | High | Transactional writes with rollback (NFR-02); interrupted-migration test |

## Timeline (if capacity is known)

Capacity is a single maintainer; estimates are person-days and sequential.

| Phase | Effort | Notes |
|---|---|---|
| Phase 0 | 0.5d | Gating spike |
| Phase 1 | 0.5d | Interface seam |
| Phase 2 | 1.5d | Schema + CRUD |
| Phase 3 | 1.5d | Query methods |
| Phase 4 | 1.0d | Selection + migration |
| Phase 5 | 1.0d | Wire-up + docs + benchmark |
| **Total** | **6.0d** | |
