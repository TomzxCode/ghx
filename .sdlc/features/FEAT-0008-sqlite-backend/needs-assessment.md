---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Needs Assessment: SQLite backend for issue/PR storage

## Problem Statement

Cached repositories with hundreds or thousands of issues/PRs respond slowly to list, search, and filter operations because every query loads and JSON-parses the entire item set from disk into memory.
The current backend stores one JSON file per item (`issues/42.json`, `prs/10.json`), so `LoadAllIssues` / `LoadAllPRs` walk the directory, read each file, and unmarshal it before any filtering can begin (`internal/cache/store.go:117`, `store.go:145`).
This is acceptable for small repositories but becomes the dominant cost as cache size grows, undermining the tool's core promise of fast offline queries.

## Stakeholders

| Stakeholder | Role | How they experience the problem |
|---|---|---|
| CLI user | End user | Slow `issue list` / `pr list` and search on large cached repos |
| AI agents / automation | Consumer | Repeated cached reads stall agentic workflows that batch many queries |
| Developer / maintainer | Author | Tool fails to scale to the large-repo use case it is positioned for |

## Evidence of Need

| Source | What it shows | Strength |
|---|---|---|
| Issue #1 rationale | Author asserts file-based backend degrades at scale; lists indexed lookups, search, single-file storage, lower I/O as benefits | Weak |
| Code inspection | `LoadAllIssues` / `LoadAllPRs` read and unmarshal every file per query with no index (`store.go:117`, `store.go:145`) | Moderate |
| Acceptance criteria | Require benchmarks proving improvement over file-based on a few hundred items, implying the gap is not yet measured | Weak |

**Evidence rating:** Weak

The need is assumed rather than demonstrated: no benchmark, usage data, or user report quantifies the current pain.
The code structure confirms that O(n) full-scan-plus-parse happens on every list/search, which is plausible evidence of scaling pain, but the magnitude is unmeasured.

## Cost of Inaction

| Aspect | Impact |
|---|---|
| What breaks or degrades today | List/search/filter latency grows linearly with cached item count; worst for repos with thousands of issues |
| Existing workarounds | Users can keep caches small or re-run narrower queries, trading API calls for local speed |
| Trend | Growing, as agent/automation usage and cached repo sizes increase over time |

**Cost-of-inaction rating:** Moderate

The status quo is tolerable for small repos and already-shipped use cases, but the pain grows with cache size and the tool's expanding automation audience.

## Alternative Paths

| Alternative | How it addresses the need | Trade-offs |
|---|---|---|
| Optimize file backend (in-memory cache after first load, lazy load) | Avoids re-reading files on repeat queries within a process | Does not help cross-process or cold-start; no indexed search across fields |
| Sidecar index file (JSON/SQLite index over existing files) | Adds indexes without replacing file storage | Two sources of truth to keep in sync; still parses bodies on full load |
| Narrower caching scope | Keeps item counts low by caching less | Conflicts with the offline-completeness goal; reduces usefulness |
| Accept current performance | No work | Pain continues to grow with repo size; no search capability added |

**Could the need be met without new code?** Partially

A file-backend optimization (persisted process-level cache or a sidecar index) could reduce repeated-query cost, but it cannot deliver indexed field search or single-file portability, and it leaves the O(n) cold-scan in place.
New storage code is justified if indexed search and large-repo scalability are desired.

## Strategic Alignment

| Criterion | Assessment |
|---|---|
| Aligns with project goals | Yes, directly advances the "fast, offline-capable queries" and low-cost-data-source-for-agents goals |
| Serves core or edge use case | Core (query/list/search performance is the product) |
| Dependency enabler | Unblocks future search, sorting, and analytics features that need indexed access |

**Alignment rating:** Strong

## Verdict

**Overall needs assessment:** Needed

**Rationale:** The feature targets the tool's core value proposition (fast offline queries) and strongly aligns with project goals, especially the growing agent/automation audience that issues many repeated cached reads.
The cost of inaction is moderate and rising with cache size, and no code-free alternative can deliver indexed search or eliminate the cold full-scan.
The single weakest dimension is evidence: the performance gap is asserted and structurally plausible but not yet measured, so the verdict is contingent on confirming the gap is real and material before committing to a full build.

## Conditions to Proceed

- Benchmark the current file-based backend on a repository with a few hundred to a few thousand issues/PRs to confirm that list/search latency is materially slow before committing to a new storage engine.
- Confirm that the alternative of optimizing the file backend (process-level cache or sidecar index) would not meet the need at materially lower cost.

## Open Questions

1. How slow is the current backend in practice (measured), and at what cache size does latency become unacceptable for the target users?
2. Are the target use cases cold-start queries (where an index helps most) or repeated queries within one process (where an in-memory cache might suffice)?
3. Is full-text search across issue/PR bodies a desired capability, or is indexed metadata filtering (state, author, labels) sufficient?
