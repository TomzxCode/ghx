---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Observability: SQLite backend for issue/PR storage

## Context

gh-cached is a local CLI with no runtime metrics, logging, tracing, or alerting, and no deployed service to monitor (per `.sdlc/context/architecture.md`).
There are therefore no SLOs, no metrics pipeline, and no tracing.
Operational health is delivered to the user through clear, actionable error output on stderr and conventional non-zero exit codes, consistent with the existing command behavior.

## Failure Modes and User-Facing Handling

| Failure | Detection | User-facing behavior | Exit code |
|---|---|---|---|
| DB file corrupt / unreadable | `sql.Open` or query returns error | Clear stderr message naming the DB path and suggesting deletion/re-migration | non-zero |
| DB locked by another process | `SQLITE_BUSY` | Retry briefly, then a message advising to close other `ghx` instances | non-zero |
| Migration error mid-run | Transaction error | Transaction rolls back; message reports partial state and that the DB is valid and re-runnable (NFR-02) | non-zero |
| Schema version newer than binary | `PRAGMA user_version` check | Message stating the cache was written by a newer `ghx` and must be upgraded | non-zero |
| Query/argument error | Flag validation / SQL prepare error | Existing cobra error output | non-zero |

## Logging

No structured logging is added.
The feature follows the existing convention of printing human-readable status to stdout (e.g. `Cached N issue(s).`) and errors to stderr, with `fmt.Errorf("context: %w", err)` wrapping for diagnostic clarity.

## Metrics, Tracing, Alerts, SLOs

Not applicable.
No production service, no collection pipeline. The NFR-01 benchmark serves as the performance health check during development rather than a runtime metric.

## Out of Scope

- Structured logging, distributed tracing, or a metrics endpoint (no service to host them).
- Health-check endpoint or uptime monitoring (CLI, not a server).
