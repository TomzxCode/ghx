---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Telemetry: SQLite backend for issue/PR storage

## Context

gh-cached is an offline-first local CLI with no remote analytics backend and no user-tracking (per `.sdlc/context/architecture.md`: "No observability").
No analytics events are collected or transmitted; the tool must remain functional fully offline and must not phone home.
Success is therefore measured through local, runnable signals (benchmarks and behavior) rather than emitted events.

## Success Metrics

| Metric | Target | How measured |
|---|---|---|
| Large-repo list/filter latency (NFR-01) | SQLite path completes in < 50% of file-backend wall-clock on >=500 items (>=2x speedup) | `go test -bench` benchmark over a generated large fixture, recording both timings |
| Result-set equivalence (NFR-03, FR-06) | 100% match vs. in-memory `filterIssues`/`filterPRs` on identical fixtures | Differential test asserting SQLite query output equals file-backend output |
| Migration correctness (FR-05) | Zero field loss; idempotent re-run | Round-trip test (file -> SQLite -> compare) plus re-run idempotency test |
| Build portability (NFR-04) | All four release targets build CGO-free | CI release build matrix passes |

## Funnel

Not applicable.
There is no product funnel (sign-up, activation, retention) for a local CLI; adoption is inferred indirectly from release downloads and issue activity, which are outside this feature's scope.

## Events

None.
No analytics events are emitted by the feature.

## Deferred / Out of Scope

- Remote usage telemetry (deliberately excluded; conflicts with offline-first and privacy).
- Opt-in anonymous usage reporting (could be a future, separately-scoped feature with explicit user consent).
