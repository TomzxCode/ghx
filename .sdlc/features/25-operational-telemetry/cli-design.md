---
issue: "#25"
title: "Operational telemetry for API and cache performance"
status: in-review
revision: 1
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# CLI Design: Operational telemetry for API and cache performance

## Design Principles

- Extends the existing `ghx` CLI conventions: cobra commands grouped under a noun, GNU-style long options, short forms only where one is conventional.
- Telemetry is on by default and switchable: `telemetry enable|disable` persist the global setting, `telemetry status` reports it, and `--telemetry=false` / `GHX_TELEMETRY=0` opt out for a single run.
- Read commands are read-only: nothing under `telemetry` mutates the cache, and only `telemetry clear` ever deletes data.
- Output goes to stdout for data and stderr for diagnostics, matching the rest of the CLI.
- Enabling telemetry is orthogonal to the storage backend; `--telemetry` applies to all backends.

## Command Tree

```
ghx
├── telemetry          Inspect and control recorded API and cache timings
│   ├── summary        Counts, duration percentiles, retry rates, and byte totals by operation kind
│   ├── export         Emit recorded events as JSON
│   ├── clear          Delete recorded events (destructive)
│   ├── enable         Turn on recording globally
│   ├── disable        Turn off recording globally
│   └── status         Show the effective on/off state and paths
├── cache              Existing; gains --telemetry and prints a timing summary
├── issue / pr / repo / stats / mock
    └── (existing; all accept the global --telemetry flag)
```

## Commands

### `ghx telemetry summary`

**Description:** Reads the telemetry database and reports, per operation kind, the event count, duration percentiles, retry rate, and total bytes sent and received, plus a total row. Serves FR-7 and NFR-9.

**Synopsis:**

```
ghx telemetry summary [options]
```

**Positional arguments:**

None.

**Options:**

| Short | Long | Value | Default | Description |
|---|---|---|---|---|
| | `--kind` | string | (all) | Restrict to one operation kind, e.g. `pr_full_page` |
| | `--repo` | `[HOST/]OWNER/REPO` | (all) | Restrict to one repository |
| | `--since` | date | (all time) | Only events at or after this date (`YYYY-MM-DD` or RFC3339) |
| | `--json` | bool | `false` | Emit the summary as JSON instead of a table |
| -h | `--help` | | | Show help and exit |

**Examples:**

```bash
ghx telemetry summary
ghx telemetry summary --repo cli/cli --since 2026-09-01
ghx telemetry summary --kind pr_full_page --json
```

**Errors:** Exits 1 with `no telemetry events recorded at <path>` when nothing has ever been recorded.

### `ghx telemetry enable` / `ghx telemetry disable` / `ghx telemetry status`

**Description:** Persist or inspect the global on/off preference, stored in `config.json` beside the telemetry database. Serves FR-5 and NFR-8.

**Synopsis:**

```
ghx telemetry enable
ghx telemetry disable
ghx telemetry status
```

**Positional arguments:**

None.

**Options:**

| Short | Long | Value | Default | Description |
|---|---|---|---|---|
| -h | `--help` | | | Show help and exit |

`enable` and `disable` take no options; `status` takes none. All three honor the global `--telemetry-db`.

**Examples:**

```bash
ghx telemetry disable
ghx telemetry enable
ghx telemetry status
```

**Errors:** Exits 1 with `saving telemetry config: <reason>` when the config file cannot be written.

### `ghx telemetry export`

**Description:** Emits recorded events as machine-readable JSON on stdout, for the benchmark script and external analysis. Serves FR-8.

**Synopsis:**

```
ghx telemetry export [options]
```

**Positional arguments:**

None.

**Options:**

| Short | Long | Value | Default | Description |
|---|---|---|---|---|
| | `--format` | string | `json` | Output format; `json` is the only supported value today |
| | `--kind` | string | (all) | Restrict to one operation kind |
| | `--repo` | `[HOST/]OWNER/REPO` | (all) | Restrict to one repository |
| | `--since` | date | (all time) | Only events at or after this date |
| -o | `--output` | path | stdout | Write to a file instead of stdout |
| -h | `--help` | | | Show help and exit |

**Examples:**

```bash
ghx telemetry export > events.json
ghx telemetry export --kind pr_full_page -o pr-pages.json
```

**Errors:** Exits 1 with `no telemetry events recorded at <path>` when there is nothing to export; exits 2 on an unsupported `--format`.

### `ghx telemetry clear`

**Description:** Deletes recorded events from the telemetry database. Destructive, and the only mutating telemetry command. Serves the retention question in the requirements (Open Question 2).

**Synopsis:**

```
ghx telemetry clear [options]
```

**Positional arguments:**

None.

**Options:**

| Short | Long | Value | Default | Description |
|---|---|---|---|---|
| | `--before` | date | (all) | Delete only events strictly before this date |
| -y | `--yes` | bool | `false` | Skip the confirmation prompt |
| -h | `--help` | | | Show help and exit |

**Examples:**

```bash
ghx telemetry clear --yes
ghx telemetry clear --before 2026-09-01
```

**Errors:** Exits 1 with `no telemetry data at <path>` when the database is absent.

## Global Options

| Short | Long | Value | Default | Description |
|---|---|---|---|---|
| | `--telemetry` | bool | on | Record API and cache timings for this invocation; pass `--telemetry=false` to opt out for one run |
| | `--telemetry-db` | path | (resolved) | Override the telemetry database path |
| -R | `--repo` | `[HOST/]OWNER/REPO` | (detected) | Existing global option |
| -h | `--help` | | | Show help and exit |

`--telemetry` and `--telemetry-db` are persistent root flags, so they apply to every command that can produce events.

## Environment Variables

| Variable | Used by | Default | Description |
|---|---|---|---|
| `GHX_TELEMETRY` | all | unset | Override the global setting for this run; `1`/`true`/`yes`/`on` enables, `0`/`false`/`no`/`off` disables |
| `GHX_TELEMETRY_DB` | all | `$XDG_DATA_HOME/ghx/telemetry.db` (fallback `~/.local/share/ghx/telemetry.db`) | Telemetry database path |
| `GHX_STORAGE` | all | `sqlite` | Existing; selects the cache backend, independent of telemetry |
| `NO_COLOR` | all | unset | Existing; disables colored output |

## Configuration

- A config file (`config.json`, beside the telemetry database) stores the global on/off preference set by `telemetry enable|disable`. It holds only `{"enabled": bool}`.
- Precedence: CLI flags > environment variables > config file > default (enabled).
- A missing or unreadable config falls back to the default rather than erroring, so configuration never blocks a command.

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success, including a run where telemetry was enabled but recording failed silently |
| 1 | Runtime failure (no telemetry data, unwritable export path, database error) |
| 2 | Usage error (unknown `--format`, invalid `--since`, conflicting flags) |

## Output Behavior

- **stdout:** `telemetry summary` tables or JSON, `telemetry export` JSON, and the existing command output.
- **stderr:** progress bars, the post-`cache` timing summary, retry notices, and errors.
- **Machine-readable:** `telemetry summary --json` emits one object per operation kind with `total_sent_bytes` and `total_received_bytes`; `telemetry export --format json` emits an array of event objects with `timestamp`, `kind`, `operation`, `repo`, `host`, `status`, `duration_ms`, `page_size`, `items_returned`, `request_bytes`, `response_bytes`, `cursor`, `attempts`, `retried`, `rate_remaining`, `backend`, and `items`.
- **TTY behavior:** tables and progress bars render only when stderr is a TTY, matching the existing `progressWriter` behavior; JSON output is never colored.

## Help Text

```
ghx --help

Extended GitHub CLI with local caching

Usage:
  ghx [command]

Available Commands:
  cache       Fetch and cache issues and PRs (including comments)
  issue       Work with GitHub issues
  mock        Mock GitHub API server for testing
  pr          Work with pull requests
  repo        List locally cached repositories
  stats       Generate an HTML report of pull request activity
  telemetry   Inspect and control recorded API and cache timings

Flags:
      --api-url string        Override the GitHub GraphQL API endpoint URL (for testing)
      --cache-dir string      Override the cache directory path
  -h, --help                  help for ghx
  -R, --repo string           Repository in [HOST/]OWNER/REPO format
      --storage string        Cache storage backend: sqlite (default) or file (env GHX_STORAGE)
      --telemetry             Record API and cache timings locally (default enabled; env GHX_TELEMETRY, use --telemetry=false to opt out)
      --telemetry-db string   Telemetry database path (env GHX_TELEMETRY_DB)
  -v, --version               version for ghx
```

## Error Messages

- Format: `error: <message>` followed by an actionable hint when one exists.
- `error: no telemetry events recorded at /home/u/.local/share/ghx/telemetry.db` when nothing has ever been recorded (telemetry is on by default; run a command first, or check `telemetry status`)
- `error: invalid --format "csv"` / `hint: use --format json`
- `error: invalid --since value "last week"` / `hint: use YYYY-MM-DD or RFC3339 (e.g. 2026-09-01T15:04:05Z)`
- A failed telemetry write never produces an error message on the user's path; it is suppressed so NFR-1/NFR-2 hold.

## Interactive Behavior

- **Prompts:** `telemetry clear` prompts `Delete all recorded events from <path>? [y/N]` unless `--yes` is passed.
- **Destructive actions:** `telemetry clear` is the only destructive command; `--yes` skips confirmation, and there is no `--force`.
- **Dry run:** `telemetry clear --before <date>` with an empty result exits 0 and prints `Nothing to delete.`.

## Example Sessions

### Recording and inspecting a cache run

```console
$ ghx --repo cli/cli cache
Caching issues for cli/cli...
Cached 4213 issue(s).
Caching pull requests for cli/cli...
Cached 987 pull request(s).
Cache updated. Valid for 60 minute(s).
Timings (38.2s total, 214 calls, 3 retried, sent 44.1KB, received 812.7MB):
  issue_full_fetch   43 calls  p50  412ms  p95 1180ms  sent   1.1KB  recv  6.2MB  p50 99 items
  pr_scan            12 calls  p50  260ms  p95  495ms  sent   0.5KB  recv  5.1KB  p50 100 items
  pr_full_page      159 calls  p50  186ms  p95  940ms  sent   2.8KB  recv  1.2MB  p50 50 items
```

### Summarizing recorded timings

```console
$ ghx telemetry summary --repo cli/cli
kind              count  p50_ms  p95_ms  retry_rate  sent   received  p50_size  p50_items
issue_full_fetch   43     412    1180        0.00        48.3KB 268.4MB   6.2MB     99
pr_scan            12     260     495        0.00        6.0KB  61.2KB    5.1KB     100
pr_full_page      159     186     940        0.02        445KB  190.8MB   1.2MB     50
TOTAL             214                                  499KB  459.3MB
```

### Enabling and disabling globally

```console
$ ghx telemetry disable
Telemetry disabled (/home/u/.local/share/ghx/config.json).
$ ghx telemetry status
Telemetry:   disabled
Database:    /home/u/.local/share/ghx/telemetry.db
Config:      /home/u/.local/share/ghx/config.json
Events:      1637
```

### Clearing recorded data

```console
$ ghx telemetry clear --before 2026-09-01
Delete 2,418 recorded event(s) older than 2026-09-01 from /home/u/.local/share/ghx/telemetry.db? [y/N] y
Deleted 2,418 event(s).
```

## Requirements Traceability

| Requirement | Command(s) / Option(s) | Notes |
|---|---|---|
| FR-1 | `--telemetry` (global) | Enables per-GraphQL-call recording for every command that calls the API |
| FR-2 | `--telemetry` (global) | Enables per-cache-operation recording for both backends |
| FR-3 | `telemetry summary --kind` | The `kind` label is the operation-kind vocabulary surfaced by the read commands |
| FR-4 | `--telemetry-db`, `GHX_TELEMETRY_DB` | Flag > env > default resolution |
| FR-5 | `telemetry enable\|disable\|status`, `--telemetry`, `GHX_TELEMETRY` | On by default with a persisted global switch and per-run opt-outs |
| FR-6 | `cache` (summary on stderr) | Post-run timing summary after `cache` |
| FR-7 | `telemetry summary` | Counts, percentiles, retry rate, and byte totals by kind |
| FR-8 | `telemetry export --format json` | Machine-readable JSON for scripts |
| FR-9 | `mock serve --latency-from` | Latency injection configured on the mock command, not the telemetry surface |
| FR-10 | `cache` | Elapsed-time and per-page breakdown in the summary |
| FR-11 | `--telemetry` (global) | Rate-limit remaining is captured per call when the response carries it |
| NFR-2 | `--telemetry` (global) | A failed write never reaches stdout/stderr and never changes the exit code |
| NFR-7 | `ghx --help`, `ghx cache --help` | The telemetry flag, env fallback, and default state are documented in both help texts |
| NFR-8 | `telemetry enable\|disable\|status` | The global switch is settable and inspectable without editing a file |
| NFR-9 | `telemetry summary` | Per-kind total bytes sent and received, plus a total row |

## Out of Scope

- Any remote analytics backend or upload command (forbidden by the offline-first constraint).
- A general-purpose configuration file beyond the telemetry on/off preference.
- Retention automation: `telemetry clear --before` is manual; automatic pruning is not designed here.
- The exact mock-server latency-injection flags; they live on the `mock serve` surface (`--latency-from`).

## Open Questions

1. Should `--telemetry` be a persistent root flag only, or also accepted per-command where it matters most (`cache`)?
2. Should `telemetry summary` default to the current repository, or to all repositories, when `--repo` is omitted?
3. Is `--json` on `telemetry summary` needed at all, given `telemetry export` already produces machine-readable output?
