# Performance telemetry

ghx can record how long each GitHub API call and each cache operation takes, so
performance work and the mock server's simulation generator can rely on measured
costs instead of assumed ones.

Recording is **off by default**, **local-only**, and **never uploaded**. A
failure to record never fails, slows, or changes the exit code of the command
that produced the event.

## Enabling recording

Recording is **on by default**. Each command records while it runs; turn it off
globally with `ghx telemetry disable`, or for a single run with
`--telemetry=false` / `GHX_TELEMETRY=0`:

```bash
ghx cache --repo cli/cli                  # records by default
ghx --telemetry=false cache --repo cli/cli   # one run, no recording
ghx telemetry disable                     # globally off
ghx telemetry enable                      # globally on
ghx telemetry status                      # show the effective state
```

Recording stays **local-only** and is **never uploaded**. A failure to record
never fails, slows, or changes the exit code of the command that produced the
event.

## Reading the per-run summary

When telemetry is enabled, `ghx cache` prints a timing breakdown to stderr when
it finishes:

```
Timings (337.6s total, 1637 call(s), 7 retried, sent 3.6KB, received 752.9KB):
  pr_full_page          56 call(s)  p50  7832ms  p95 65970ms  sent    2.8KB  recv  562.5KB  p50 50 items
  pr_scan               15 call(s)  p50   336ms  p95   448ms  sent    0.5KB  recv    5.1KB  p50 100 items
```

## Inspecting across runs

```bash
ghx telemetry summary --repo cli/cli --since 2026-09-01
ghx telemetry summary --kind pr_full_page --json
```

`summary` reports the event count, p50 and p95 durations, the retry rate, the
total bytes sent and received, and the payload-size percentiles per operation
kind. A `TOTAL` row sums the counts and bytes across every kind.

```
kind              count  p50_ms  p95_ms  retry_rate  sent   received  p50_size  p50_items
pr_full_fetch     2      4468    6345    0.00        2.8KB  562.5KB   255.2KB   36
issue_full_fetch  1      2225    2225    0.00        801B   190.4KB   190.4KB   99
TOTAL             3                                  3.6KB  752.9KB
```

## Exporting events

```bash
ghx telemetry export > events.json
ghx telemetry export --kind pr_full_page -o pages.json
```

`export` emits the stored events as JSON: `timestamp`, `kind`, `operation`,
`host`, `repo`, `status`, `duration_ms`, `page_size`, `items_returned`,
`request_bytes`, `response_bytes`, `cursor`, `attempts`, `retried`,
`rate_remaining`, `backend`, `items`, and `failed`.
Treat both the field set and the `kind` set as open: new values are additive.

`page_size` is what was requested; `items_returned` is what the response actually
carried, so the two differ at the end of a pagination window. `request_bytes` and
`response_bytes` are the payload sizes, which explain most of the latency
variance: a `pr_full_page` carrying 1.2 MB is slow for reasons a count alone does
not reveal.

## Simulating measured latency

The mock server can inject per-kind response latency drawn from recorded events,
so a generated scenario reproduces measured cost rather than an assumed one:

```bash
ghx mock serve --preset default --latency-from ~/.local/share/ghx/telemetry.db
ghx mock serve --preset default --latency-from events.json   # an exported file
```

Delays are sampled from the recorded distribution, not from an average, so the
long tail (502s, rate-limit waits) survives into the scenario.

## Clearing recorded data

```bash
ghx telemetry clear --before 2026-09-01
ghx telemetry clear --yes
```

`clear` is the only destructive telemetry command and prompts unless `--yes` is
given.

## Operation kinds

| Kind | Recorded for |
|---|---|
| `issue_list`, `issue_get`, `issue_full_fetch`, `issue_search` | Issue listing, view, full cache fetch, search fallback |
| `pr_list`, `pr_get`, `pr_full_fetch`, `pr_search` | The PR equivalents |
| `pr_scan` | Phase 1 of a PR delta fetch (the light newest-first walk) |
| `pr_full_page` | Phase 2 of a PR delta fetch (a full-payload page) |
| `cache_query_issues`, `cache_query_prs` | Cache-backed list queries |
| `cache_load_issue`, `cache_load_pr`, `cache_load_all` | Individual and bulk cache reads |
| `cache_save_issue`, `cache_save_pr` | Cache writes |
| `cache_info` | Cache metadata read/write and repository listing |
