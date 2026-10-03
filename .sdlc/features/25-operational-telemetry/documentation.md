---
issue: "#25"
title: "Operational telemetry for API and cache performance"
status: in-review
revision: 1
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# Documentation: Operational telemetry for API and cache performance

## How-to: Measure where a cache run spends its time

Run any command and read the per-kind breakdown it prints at the end.

```bash
ghx cache --repo cli/cli
```

Recording is on by default. Turn it off globally with `ghx telemetry disable`,
or for a single run with `--telemetry=false` / `GHX_TELEMETRY=0`.

After the run, the command prints a summary to stderr showing each operation kind, its call count, its p50/p95 duration, and the total bytes sent and received, for example:

```
Timings (13.2s total, 193 call(s), 0 retried, sent 3.6KB, received 752.9KB):
  pr_full_fetch          2 call(s)  p50   4468ms  p95   6345ms  sent    2.8KB  recv  562.5KB  p50 36 items
  issue_full_fetch       1 call(s)  p50   2225ms  p95   2225ms  sent     801B  recv  190.4KB  p50 99 items
```

A high retry count is the signal to look at: a `pr_full_page` retry rate around or above 2% means GitHub's secondary rate limit is being tripped by the concurrency settings, and the right response is to revisit `prBlocksInFlight` in `internal/github/api.go` rather than to optimize on top of a degraded API.

## How-to: Compare runs across days

The per-run summary is ephemeral; the events persist in the telemetry database, so you can aggregate over time.

```bash
ghx telemetry summary --repo cli/cli --since 2026-09-01
```

Export the raw events when you need a plot or a custom analysis:

```bash
ghx telemetry export --kind pr_full_page --since 2026-09-01 -o pages.json
```

## How-to: Make a simulation use real latency

The mock server can inject per-kind response latency drawn from recorded events, so a generated scenario reproduces measured cost instead of an assumed one.

```bash
# 1. Record a real run (recording is on by default).
ghx cache --repo cli/cli

# 2. Start the mock server with latency fitted from what you recorded.
ghx mock serve --preset default --latency-from ~/.local/share/ghx/telemetry.db
```

`--latency-from` accepts either the telemetry database or a JSON file previously produced by `ghx telemetry export`. The server draws each delay from the recorded distribution (not an average), so the long tail (502s, rate-limit waits) survives into the scenario.

## Reference: `ghx telemetry`

### `ghx telemetry summary`

```bash
ghx telemetry summary [--kind <kind>] [--repo <owner/repo>] [--since <date>] [--json]
```

Prints, per operation kind, the event count, p50 and p95 duration in milliseconds, the retry rate, the total bytes sent and received, and the payload-size percentiles. A `TOTAL` row sums counts and bytes across all kinds. `--json` emits the same data as objects (with `total_sent_bytes` and `total_received_bytes`). Exits 1 with `no telemetry events recorded at <path>` when nothing has ever been recorded.

### `ghx telemetry export`

```bash
ghx telemetry export [--format json] [--kind <kind>] [--repo <owner/repo>] [--since <date>] [-o <path>]
```

Emits stored events as JSON. Without `-o`, the array goes to stdout.

Each event has: `timestamp` (UTC RFC3339), `kind`, `operation`, `host`, `repo`, `status` (API calls only), `duration_ms`, `page_size`, `cursor`, `attempts`, `retried`, `rate_remaining`, `backend` (cache operations only), `items`, and `failed`. Consumers should tolerate unknown fields and unknown `kind` values.

### `ghx telemetry clear`

```bash
ghx telemetry clear [--before <date>] [--yes]
```

Deletes events, all of them or only those before a cutoff. It is the only destructive telemetry command and prompts unless `--yes` is passed.

## Reference: global flags and environment variables

| Flag | Env | Default | Description |
|---|---|---|---|
| `--telemetry` | `GHX_TELEMETRY` | on | Enable local recording; pass `--telemetry=false` or `GHX_TELEMETRY=0` to opt out for a run |
| `--telemetry-db` | `GHX_TELEMETRY_DB` | `$XDG_DATA_HOME/ghx/telemetry.db` | Telemetry database path |
| | `GHX_TELEMETRY` accepts `1`/`true`/`yes`/`on` or `0`/`false`/`no`/`off` | | |

Flag beats environment variable beats the persisted setting (`ghx telemetry enable|disable`) beats the default (on).

## Reference: operation kinds

| Kind | Recorded for |
|---|---|
| `issue_list`, `issue_get`, `issue_full_fetch`, `issue_search` | Issue listing, single-issue view, full cache fetch, search fallback |
| `pr_list`, `pr_get`, `pr_full_fetch`, `pr_search` | PR equivalents |
| `pr_scan` | Phase 1 of a PR delta fetch: the light newest-first walk |
| `pr_full_page` | Phase 2 of a PR delta fetch: a full-payload page |
| `cache_query_issues`, `cache_query_prs` | Cache-backed list queries |
| `cache_load_issue`, `cache_load_pr`, `cache_load_all` | Individual and bulk cache reads |
| `cache_save_issue`, `cache_save_pr` | Cache writes |
| `cache_info` | Cache metadata read/write and repository listing |

New kinds are additive; readers must tolerate values they do not recognize.

## Explanation: why this is local, on by default, and switchable

ghx is offline-first by design: it never phones home, and the project deliberately rejected remote usage telemetry.
This feature is the local counterpart, not a reversal of that decision: everything is written to a SQLite file on the machine and nothing is transmitted.
Recording is on by default so performance evidence accumulates without anyone having to remember to opt in, and `ghx telemetry disable` turns it off globally (with `--telemetry=false` / `GHX_TELEMETRY=0` for a single run).
Everything is written to a SQLite file on the machine, nothing is transmitted, and recording is off until you ask for it.
The data is what makes "fast" falsifiable, so it is recorded by default: it is
the input both for choosing a cache-population algorithm and for making the
simulation generator model reality rather than an imagined scenario.
Records stay on the machine.

Recording is also failure-isolated: a broken or unwritable database never fails,
slows, or changes the exit code of the command that produced the event.
Turning recording off does not delete anything; `ghx telemetry clear` does that
explicitly.

## Where the state lives

| What | Path |
|---|---|
| Database | `$XDG_DATA_HOME/ghx/telemetry.db` (fallback `~/.local/share/ghx/telemetry.db`), overridable with `--telemetry-db` / `GHX_TELEMETRY_DB` |
| Global on/off setting | `config.json` beside the database |
| Precedence | `--telemetry` flag, then `GHX_TELEMETRY`, then the global setting, then the default (on) |

Because the setting sits beside the database, pointing `--telemetry-db` at a
project-local path also moves its config, which is useful for keeping separate
environments entirely apart.
