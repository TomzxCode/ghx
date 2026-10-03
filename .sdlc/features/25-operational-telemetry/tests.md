---
issue: "#25"
title: "Operational telemetry for API and cache performance"
status: in-review
revision: 1
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# Test Plan: Operational telemetry for API and cache performance

## Scope

Covers the recorder, both instrumentation seams (GraphQL and cache store), the SQLite sink, the `telemetry` read commands, the post-`cache` timing summary, and the mock server latency model.
The tests exercise the enabled and disabled paths, the failure-isolation guarantees, and the forward-compatibility of the stored and exported shapes.
Out of scope: measuring absolute GitHub latency, and any test that requires real network access.

## Unit Tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | Event encoding captures all GraphQL fields | A recorded API event with kind, host, repo, status 200, page size 25, cursor, attempts 2, retried, rate remaining | Encoded row has every field set and `retried` derived from attempts |
| TC-2 | Duration measurement covers all attempts and backoff | A client with a sleep stub that fails once then succeeds | One event whose duration includes the stubbed backoff wait and attempts = 2 |
| TC-3 | Cache operation event carries backend and item count | A decorated store query returning 10 items on the sqlite backend | One event with `backend = sqlite` and `items = 10` |
| TC-4 | Path resolution precedence | Flag, env var, and default all set | Flag wins; without the flag the env var wins; without either the default path is used |
| TC-5 | Plain run records by default; explicit off records nothing | A run with no flag, env, or config, then a run with the flag explicitly false | The first records; the second writes no database |
| TC-6 | Summary percentiles are computed correctly | A known duration list | p50 and p95 match the expected values, retry rate equals retried/total |
| TC-6b | Summary totals bytes sent and received per kind | Events with known request/response sizes across two kinds | Per-kind totals and the grand total equal the summed bytes |
| TC-7 | Export JSON is well formed and open-ended | Events with a kind not known to the writer | Output parses as JSON and preserves the unknown kind verbatim |
| TC-8 | Timeout classification is reported as an error field, not an exception | A store operation returning an error | Event still recorded, with the outcome marked as failed |
| TC-9 | Latency model samples from the recorded distribution | A distribution with a heavy tail | Sampled durations include values from the tail, and the same seed reproduces the same sequence |
| TC-10 | Schema creation is idempotent and additive | Opening an existing database twice, once with an extra nullable column | No error, existing rows preserved, new column nullable |
| TC-10b | An older database migrates additively | A database built with the pre-size schema holding one row | Opening it preserves the row, reads sizes as zero, and accepts new-field writes |
| TC-32 | Global on/off preference overrides, and is overridden by, flag and env | A disabled config with the flag on; an enabled config with env off | Flag beats config; env beats config; a corrupt config falls back to enabled |

## Integration Tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-11 | One GraphQL call produces exactly one event | Telemetry enabled, mock server serving one page | Exactly one event row for that call |
| TC-12 | Retried call records a single call with both attempts | Mock server fails the first request with a retryable status | One row, attempts 2, retried true, no duplicate row |
| TC-13 | A full cache run records separable operation kinds | Telemetry enabled, mock repository with issues and PRs | Events include the issue fetch, the PR scan, and the PR full-page kinds |
| TC-14 | Both storage backends are instrumented | The same run with `--storage sqlite` and `--storage file` | Both produce cache events tagged with their backend kind |
| TC-15 | Concurrent PR page fetches do not lose events | Mock server with enough PRs to trigger parallel chains | Recorded full-page events equal the number of pages fetched |
| TC-16 | `telemetry summary` reports per-kind rows | A populated database with three kinds | Three rows with counts, median, p95, and retry rate |
| TC-17 | `telemetry export` filters compose | A database with two repos and two dates | The `--repo` and `--since` filters return only matching events |
| TC-18 | `cache` prints a timing summary when telemetry is enabled | A cache run against the mock server | stderr contains per-kind counts, durations, retry count, and total elapsed time |
| TC-19 | Flush at command exit persists pending events | A run producing events right before exit | The database contains every event after the process exits |
| TC-20 | Mock server injects recorded latency | A fitted latency model for one kind | Response timestamps show delays consistent with the model, other kinds unaffected |

## End-to-End Tests

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-21 | Default-on, record, inspect, export | Run `ghx cache` against the mock server with no flag, then `ghx telemetry summary`, then `ghx telemetry export` | Summary shows the run's kinds; export emits valid JSON containing those events |
| TC-22 | Explicit off leaves no trace | Run a full cache run with `--telemetry=false` (and again with a `disable` config) | No telemetry database is created and no telemetry output appears |
| TC-23 | Clear is scoped and confirmed | Populate events across two dates, run `telemetry clear --before <date> --yes` | Only older events are removed and the command exits 0 |
| TC-33 | Toggle lifecycle end to end | `telemetry status` (default), a recording run, `telemetry disable`, another run, `status` | First status is enabled; event count rises; after disable the count is unchanged and status reports disabled |

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-24 | Telemetry database path is unwritable | Command completes with normal output and exit 0; no error is printed |
| TC-25 | Database is locked by another process | Events are dropped for the duration of the lock; command is unaffected and exit 0 |
| TC-26 | Token appears in configuration | No stored field contains the token or an authorization header |
| TC-27 | Telemetry database has no rows | `telemetry summary` exits 1 with `no telemetry events recorded at <path>` |
| TC-28 | Unsupported export format | Exits 2 with `invalid --format`, and no file is written |
| TC-29 | Corrupt telemetry database file | Read commands exit 1 with a clear message naming the path; recording drops events without affecting the command |
| TC-30 | Unknown event kind in an exported file | The simulation fitter ignores it rather than failing |
| TC-31 | Instrumentation overhead budget | Median instrumented duration exceeds the uninstrumented median by less than 1 ms |

## Test Infrastructure

- `internal/mockserver` provides the GraphQL endpoint; extend it with a per-kind delay and a deterministic transient-failure trigger for TC-2, TC-12, TC-15, and TC-20.
- A temp-file SQLite telemetry database per test (`t.TempDir()`), never the user's real path.
- A fake recorder recording events in memory, to assert seams emit the right events without touching disk.
- The existing `github.Client` sleep hook (`c.sleep`) is reused to make backoff deterministic in tests.
- `go test ./...` is the runner; no external dependency is added.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-1 | TC-1, TC-2, TC-11, TC-12 |
| FR-2 | TC-3, TC-14 |
| FR-3 | TC-13 |
| FR-4 | TC-4 |
| FR-5 | TC-5, TC-22, TC-32, TC-33 |
| FR-6 | TC-6, TC-18 |
| FR-7 | TC-16, TC-27 |
| FR-8 | TC-7, TC-17, TC-28 |
| FR-9 | TC-9, TC-20, TC-30 |
| FR-10 | TC-18 |
| FR-11 | TC-1 |
| NFR-1 | TC-31 |
| NFR-2 | TC-24, TC-25, TC-29 |
| NFR-3 | TC-26 |
| NFR-4 | TC-22 |
| NFR-5 | TC-10, TC-10b |
| NFR-6 | TC-14 |
| NFR-7 | TC-21 (help text asserted in the same e2e run) |
| NFR-8 | TC-32, TC-33 |
| NFR-9 | TC-6b |
