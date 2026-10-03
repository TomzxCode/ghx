---
issue: "#25"
title: "Operational telemetry for API and cache performance"
status: in-review
revision: 1
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# Observability: Operational telemetry for API and cache performance

## Overview

This feature adds a local, default-on recording layer rather than a hosted monitoring stack.
The goal is to make per-call latency and retry behavior inspectable after the fact through the `telemetry` read commands and the per-run summary, without adding a metrics endpoint, a tracing backend, or structured logging to the CLI.
This is deliberately weaker than the logging and metrics described in a service-oriented observability plan: ghx has no server, no long-running process, and no collection pipeline, so the SQLite event table *is* the observability store.

## Logging

### Retry and failure notices

| Log Level | Event | Fields | When |
|---|---|---|---|
| WARN | rate limit hit | reason, wait, attempt, max attempts | A GraphQL call is retried after a rate limit or transient error (existing behavior, unchanged) |
| INFO | timing summary | total_ms, calls, retried, per-kind counts | After a `cache` run with telemetry enabled |
| ERROR | no telemetry data | db path | A `telemetry` read command finds no database or no rows |
| ERROR | telemetry write failed | db path, error | A sink write fails and events are dropped (suppressed on the caller path) |

Structured logging (per the Python/structlog convention elsewhere in this workspace) does not apply: ghx is a Go CLI and its convention is human-readable stdout/stderr, which this feature follows.

## Metrics

There is no metrics endpoint.
The equivalent of each metric is a query over the telemetry table.

### `api_call_duration_ms`

| Field | Value |
|---|---|
| Type | Histogram (recorded as individual event rows) |
| Description | Duration of each GraphQL call, including retry backoff |
| Labels | kind, operation, repo, status, retried |
| Source | `internal/github/client.go`, `Query` |

### `api_call_attempts`

| Field | Value |
|---|---|
| Type | Counter |
| Description | Attempts per call; the retry rate is the fraction of calls with attempts greater than 1 |
| Labels | kind, repo |
| Source | `internal/github/client.go`, `Query` |

### `cache_op_duration_ms`

| Field | Value |
|---|---|
| Type | Histogram |
| Description | Duration of each cache store operation |
| Labels | kind, operation, backend, failed |
| Source | `internal/cache`, `Store` decorator |

### `sim_latency_samples`

| Field | Value |
|---|---|
| Type | Gauge |
| Description | Number of samples in a fitted latency model when the mock server starts |
| Labels | kind |
| Source | `internal/mockserver` |

## Tracing

None.
A single-process CLI has no distributed trace to correlate, and ghx does not use a tracing library.
The closest equivalent is the per-call `duration_ms` series, which already attributes time to a named operation kind.

## Health Checks

None.
There is no process to probe; the tool exits when a command finishes.

| Check | Type | Endpoint / Method | Healthy Condition |
|---|---|---|---|
| Telemetry sink usable | Not applicable | `telemetry summary` exits 0 | The database opens and returns at least one row |

## Alerts

No alerting stack is introduced, so no `alerts.yaml` companion is written.

### Sustained retry rate

| Field | Value |
|---|---|
| Condition | `pr_full_page` retry rate above ~2% over a run |
| Severity | Warning |
| For | One cache run |
| Runbook | Inspect `telemetry summary`; if the rate is high, reduce `prBlocksInFlight` in `internal/github/api.go` and re-run |
| Notification | None; surfaced in the run's timing summary |

## SLOs

Not applicable.
There is no service to hold an availability or latency objective, and applying one to a local CLI would be meaningless.
The NFR-1 overhead budget (less than 1 ms median added per call) is the closest proxy and is validated by a benchmark, not by an SLO.

## Infrastructure Requirements

| Requirement | Type | Notes |
|---|---|---|
| Local SQLite event store | Log-equivalent | Reuses `modernc.org/sqlite`; no collection pipeline |
| `telemetry summary` and `telemetry export` read commands | Metric-equivalent | The query surface over the same store |
| Post-`cache` timing summary on stderr | Report | The per-run human-readable signal |
| Mockserver latency injection | Test instrument | Makes simulated latency reproducible in tests and demos |

## Out of Scope

- A metrics endpoint, Prometheus exposition, or a scraping target.
- Distributed tracing or span instrumentation.
- An alerting pipeline (PagerDuty, Slack, or a hosted alert manager).
- SLOs and error budgets for the CLI.
- Any logging that changes the existing human-readable stdout/stderr contract.
