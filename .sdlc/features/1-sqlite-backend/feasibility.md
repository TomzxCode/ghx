---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Feasibility Assessment: SQLite backend for issue/PR storage

## Overview

Introduces a SQLite storage backend alongside the existing file-per-item JSON cache, with a selectable backend and a migration path, to make list/filter/search on large cached repositories faster.
This assessment synthesizes the requirements, the existing-solutions survey, and the codebase analysis to judge whether the feature is viable to build and maintain.

## Technical Feasibility

| Criterion | Assessment |
|---|---|
| Required technologies | `modernc.org/sqlite` (new, pure-Go, permissive); `database/sql` (stdlib); embedded SQL schema via `go:embed` |
| Integration complexity | Low-Medium — clean seams; the store is consumed through one factory and a small method set (see codebase analysis) |
| Technical risks | (1) NFR-01 performance gap is asserted but unmeasured; (2) FR-03 strictness (pushed-down SQL predicates vs. load-all) is unresolved; (3) binary-size increase from the transpiled driver; (4) FTS5 availability must be confirmed for the pinned driver version |
| Existing components to reuse | `cache.Store` method set and `CacheInfo` semantics, `github` types (unchanged), `filterIssues`/`filterPRs` as a result-set oracle, `atomicWrite` pattern for safe writes |

**Verdict:** Feasible with conditions

The build is technically straightforward given the contained blast radius, but two unknowns (the unmeasured benchmark and the CGO-free cross-build) must be validated before full commitment, and the FR-03 scope decision shapes the design.

## Financial Feasibility

| Criterion | Assessment |
|---|---|
| Estimated effort | M (interface extraction + SQLite store + schema + migration command + tests + docs; contained to `internal/cache` and `cmd`) |
| Infrastructure costs | None — embedded single-file DB, no hosting or runtime services |
| Third-party costs | None — permissive-license dependency, no paid APIs or SaaS |
| ROI expectation | High for the large-repo and agent/automation use cases, which are the product's core and growing audience; low recurring cost |

**Verdict:** Feasible

## Operational Feasibility

| Criterion | Assessment |
|---|---|
| Team availability | Available — the maintainer (author) drives the project |
| Skill gaps | None material — Go + SQL are within the existing skill set |
| Maintenance burden | Medium — two backends and a schema to maintain; mitigated by the shared `Store` interface, shared types, and the filter oracle keeping semantics aligned |
| Organizational alignment | Fits roadmap — directly advances the "fast offline-capable queries" core goal |

**Verdict:** Feasible

## Go/No-Go Decision

**Overall verdict:** Go with conditions

**Conditions:**

- Run the baseline file-backend benchmark early (before or at the start of implementation) to confirm the performance gap is material and NFR-01's >=2x target is realistic; this also closes the needs-assessment condition to proceed.
- Prove `modernc.org/sqlite` builds CGO-free for all four release targets (darwin/linux/windows, amd64+arm64) in CI before declaring NFR-04 satisfied.
- Resolve the FR-03 scope decision (ship query methods now, or v1 with `LoadAll*` over SQLite and defer pushed-down predicates) in the specification phase.

**Reversibility:** The change is reversible. SQLite is an opt-in alternative backend; the file backend and existing caches remain untouched, so selecting the file backend fully restores prior behavior. The interface extraction is a revertible refactor, and migration is non-destructive (the source file cache is preserved, not consumed). No one-way-door commitment is introduced.

## Open Questions

1. Is the binary-size increase from `modernc.org/sqlite` acceptable for a tool distributed as pre-built binaries, or does it need evaluation before adoption?
2. Will maintaining two backends long-term be worth it, or should the file backend be deprecated once SQLite is proven (affects default-selection open question)?
