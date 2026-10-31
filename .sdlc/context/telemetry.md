# Telemetry

<!--
This file captures ghx's telemetry infrastructure. Despite the name, ghx telemetry
is a local-only performance instrumentation platform, not product analytics: no
third-party service receives any data. It gives create-telemetry the context it
needs to define new events consistently with the existing taxonomy.
System health monitoring (metrics, logs, traces) lives in observability.md.
Update this file when the event conventions or recording behavior change.
-->

## Analytics Platform

ghx has no third-party analytics platform.
This is a deliberate design choice: "telemetry" is a local-only performance recorder built into the CLI (`internal/telemetry`), and events are never uploaded.
The default-on behavior and its justification are recorded in `.sdlc/knowledge/decisions/1-telemetry-default-on.md`.

| Property | Value |
|---|---|
| Platform | None (no third-party analytics; no network egress) |
| SDK | None; Go standard library plus `modernc.org/sqlite` for the local sink |
| Integration | In-process recorder in `internal/telemetry`, writing to a local SQLite database |
| API key location | Not applicable (no external service) |
| Database location | `~/.local/share/ghx/telemetry.db` by default (honors `XDG_DATA_HOME`); overridable with `--telemetry-db` or `GHX_TELEMETRY_DB` |
| Persisted setting | `config.json` beside the database, set by `ghx telemetry enable\|disable` |

Recording is on by default (decision record `1-telemetry-default-on.md`).
Resolution order is flag, then environment variable (`GHX_TELEMETRY`), then persisted setting, then default (on).
A disabled recorder is a no-op object (`telemetry.Nop`), so callers record unconditionally.
Recording is best-effort: a failure never fails, slows, or changes the exit code of the command that produced the event.
Events flow through a bounded in-process queue to a batched background writer, so callers never block on disk I/O.

## Event Naming Conventions

- An event is one measured call, grouped by its `kind` for analysis.
- Kinds are snake_case operation-kind strings named after the measured operation (for example `pr_full_page`, `cache_load_pr`).
- Kinds are the unit of analysis: durations are grouped by kind so the cost of a PR full page is not averaged with a scan page.
- New kinds are additive; readers must tolerate unknown values.
- The reserved kind `migrate` exists but is unused by the API client; `unknown` is the fallback kind.
- Cache retrieval outcomes use the kinds `cache_hit` and `cache_miss`, and carry an `operation` label naming the command-level retrieval (`issue_list`, `issue_view`, `pr_list`, `pr_view`), not the store method.
- Properties are snake_case columns shared by all kinds; fields that do not apply to a kind are stored as NULL.
- Prohibited properties: any user identity, machine identifier, token, or content payload (see Identity Resolution).

## Existing Event Taxonomy

All events are rows in one append-only SQLite table, `telemetry_events`.
The schema is additive: new fields become nullable columns, and migration is idempotent so a newer binary can read an older database and vice versa.

| Event (kind) | Trigger | Key properties |
|---|---|---|
| `issue_list` | Issue list API call | duration_ms, page_size, items_returned, status, attempts, retried |
| `issue_get` | Single issue fetch | duration_ms, status, attempts, retried |
| `issue_full_fetch` | Full issue window fetch | duration_ms, page_size, items_returned, request_bytes, response_bytes |
| `pr_list` | PR list API call | duration_ms, page_size, items_returned, status |
| `pr_get` | Single PR fetch | duration_ms, status, attempts, retried |
| `pr_full_fetch` | Full PR window fetch | duration_ms, page_size, items_returned, request_bytes, response_bytes |
| `pr_scan` | Phase 1 of the read-only PR window walk used by `stats` | duration_ms, page_size, items_returned, cursor |
| `pr_full_page` | A full-payload page of the read-only stats window walk | duration_ms, page_size, items_returned, request_bytes, response_bytes |
| `issue_search` | Issue search API call | duration_ms, items_returned |
| `pr_search` | PR search API call | duration_ms, items_returned |
| `cache_hit` | Read served from the local cache | operation, repo, backend, items, duration_ms |
| `cache_miss` | Read that fell back to the GitHub API | operation, repo, duration_ms |
| `cache_query_issues`, `cache_query_prs` | Cache query operations | items, duration_ms, backend |
| `cache_load_issue`, `cache_load_pr` | Single-record cache reads | items, duration_ms, backend |
| `cache_save_issue`, `cache_save_pr` | Cache writes | items, duration_ms, backend |
| `cache_load_all` | Bulk cache load | items, duration_ms, backend |
| `cache_info` | Cache metadata read | duration_ms, backend |
| `cache_other` | Unclassified cache operation | duration_ms, backend |

Shared columns recorded on every event: `timestamp`, `kind`, `operation`, `host`, `repo`, `status`, `duration_ms`, `page_size`, `items_returned`, `request_bytes`, `response_bytes`, `cursor`, `attempts`, `retried`, `rate_remaining`, `backend`, `items`, `failed`.
Non-applicable columns are NULL.
`attempts` is at least 1, and `retried` is true when a call took more than one attempt.

## Identity Resolution

None.
No user ID, device ID, session ID, account name, or IP address is recorded or persisted, and there is no identity merge behavior.
The identifying-adjacent fields are `host` (API host) and `repo` (owner/repo name), which exist so hit rates and latencies can be broken down per repository, and which never leave the machine.
The absence of identity is a design consequence, not an omission: the platform measures this machine's API and cache costs, so there is nothing to resolve a user across sessions for.

## Privacy and Compliance

- Local-only by design: records are never uploaded, and no code path transmits them.
- User-controlled: `ghx telemetry enable|disable` persists a global toggle, and `--telemetry=false` or `GHX_TELEMETRY=0` opts out a single run without changing the setting.
- The database grows without limit until the user acts: there is no automatic pruning; `ghx telemetry status` reports its size, and `ghx telemetry clear` (optionally `--before <date>`) deletes events.
- Recorded content is limited to timing, size, status, and repository naming metadata; do not add request or response payloads, tokens, or identity fields to events.
- Retention policy: user-managed deletion only; treat the database as user data under the user's home directory.
- Compliance posture: because data never leaves the machine and contains no identity, no consent, transfer, or disclosure mechanism applies; the default-on choice remains privacy-sensitive and is accepted only because of the local-only guarantee and the trivial opt-out (decision record `1-telemetry-default-on.md`).
- Guardrail: any future change that transmits recorded data would reverse a recorded decision and must not be added silently.

## Funnels and Key Metrics

There are no conversion funnels; the platform measures performance, not user journeys.
Analysis replaces funnels:

| Analysis | What it answers | Defined in |
|---|---|---|
| `ghx telemetry summary` | Counts, p50/p95 durations, retry rates, and byte totals per operation kind | `cmd/telemetry.go` |
| Cache hit rates | Hit/miss counts per repository and retrieval operation | `telemetry.Store.CacheHitRates` |
| Per-run timing summary | In-run percentiles by kind without re-reading the database | `telemetry.Aggregator` |
| `ghx telemetry export` | Raw events as JSON for offline analysis | `cmd/telemetry.go` |
| Mock latency fitting | Per-kind response latency for `ghx mock serve --latency-from`, fitted from recorded events or an export | `cmd/mock_latency.go` |
