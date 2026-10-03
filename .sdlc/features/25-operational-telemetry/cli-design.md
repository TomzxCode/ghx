---
issue: "#25"
title: "Operational telemetry for API and cache performance"
status: draft
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# CLI Design: Operational telemetry for API and cache performance

## Design Principles

- Extends the existing `ghx` CLI conventions: cobra commands grouped under a noun, GNU-style long options, short forms only where one is conventional.
- Telemetry is opt-in and never on by default: the surface exists to turn recording on, inspect what was recorded, and turn it off.
- Read commands are read-only: nothing under `telemetry` mutates the cache, and only `telemetry clear` ever deletes data.
- Output goes to stdout for data and stderr for diagnostics, matching the rest of the CLI.
- Enabling telemetry is orthogonal to the storage backend; `--telemetry` applies to all backends.

## Command Tree

```
ghx
├── telemetry          Inspect and export recorded API and cache timings
│   ├── summary        Counts, duration percentiles, and retry rates by operation kind
│   ├── export         Emit recorded events as JSON
│   └── clear          Delete recorded events (destructive)
├── cache              Existing; gains --telemetry and prints a timing summary
├── issue / pr / repo / stats / mock
    └── (existing; all accept the global --telemetry flag)
```

## Commands

### `ghx telemetry summary`

**Description:** Reads the telemetry database and reports, per operation kind, the event count, duration percentiles, and retry rate. Serves FR-7.

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

**Errors:** Exits 1 with `no telemetry data at <path>` when the database is absent or empty.

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

**Errors:** Exits 1 with `no telemetry data at <path>` when there is nothing to export; exits 2 on an unsupported `--format`.

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
| | `--telemetry` | bool | `false` | Enable recording of API and cache timings for this invocation |
| | `--telemetry-db` | path | (resolved) | Override the telemetry database path |
| -R | `--repo` | `[HOST/]OWNER/REPO` | (detected) | Existing global option |
| -h | `--help` | | | Show help and exit |

`--telemetry` and `--telemetry-db` are persistent root flags, so they apply to every command that can produce events.

## Environment Variables

| Variable | Used by | Default | Description |
|---|---|---|---|
| `GHX_TELEMETRY` | all | unset | Opt in to telemetry; `1` or `true` enables it, matching `GHX_STORAGE` as the existing env-var pattern |
| `GHX_TELEMETRY_DB` | all | `$XDG_DATA_HOME/ghx/telemetry.db` (fallback `~/.local/share/ghx/telemetry.db`) | Telemetry database path |
| `GHX_STORAGE` | all | `sqlite` | Existing; selects the cache backend, independent of telemetry |
| `NO_COLOR` | all | unset | Existing; disables colored output |

## Configuration

- No configuration file is introduced; the existing CLI has none, and adding one is out of this feature's scope (requirements Open Question 1).
- Precedence: CLI flags > environment variables > defaults.

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success, including a run where telemetry was enabled but recording failed silently |
| 1 | Runtime failure (no telemetry data, unwritable export path, database error) |
| 2 | Usage error (unknown `--format`, invalid `--since`, conflicting flags) |

## Output Behavior

- **stdout:** `telemetry summary` tables or JSON, `telemetry export` JSON, and the existing command output.
- **stderr:** progress bars, the post-`cache` timing summary, retry notices, and errors.
- **Machine-readable:** `telemetry summary --json` emits one object per operation kind; `telemetry export --format json` emits an array of event objects with `timestamp`, `kind`, `operation`, `repo`, `host`, `status`, `duration_ms`, `page_size`, `cursor`, `attempts`, `retried`, `backend`, and `items`.
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
  telemetry   Inspect and export recorded API and cache timings

Flags:
      --api-url string        Override the GitHub GraphQL API endpoint URL (for testing)
      --cache-dir string      Override the cache directory path
  -h, --help                  help for ghx
  -R, --repo string           Repository in [HOST/]OWNER/REPO format
      --storage string        Cache storage backend: sqlite (default) or file (env GHX_STORAGE)
      --telemetry             Record API and cache timings locally (env GHX_TELEMETRY)
      --telemetry-db string   Telemetry database path (env GHX_TELEMETRY_DB)
  -v, --version               version for ghx
```

## Error Messages

- Format: `error: <message>` followed by an actionable hint when one exists.
- `error: no telemetry data at /home/u/.local/share/ghx/telemetry.db` / `hint: enable recording with --telemetry (or GHX_TELEMETRY=1), then run a command`
- `error: invalid --format "csv"` / `hint: use --format json`
- `error: invalid --since value "last week"` / `hint: use YYYY-MM-DD or RFC3339 (e.g. 2026-09-01T15:04:05Z)`
- A failed telemetry write never produces an error message on the user's path; it is suppressed so FR-2/NFR-2 hold.

## Interactive Behavior

- **Prompts:** `telemetry clear` prompts `Delete all recorded events from <path>? [y/N]` unless `--yes` is passed.
- **Destructive actions:** `telemetry clear` is the only destructive command; `--yes` skips confirmation, and there is no `--force`.
- **Dry run:** `telemetry clear --before <date>` with an empty result exits 0 and prints `Nothing to delete.`.

## Example Sessions

### Recording and inspecting a cache run

```console
$ ghx --telemetry --repo cli/cli cache
Caching issues for cli/cli...
Cached 4213 issue(s).
Caching pull requests for cli/cli...
Cached 987 pull request(s).
Cache updated. Valid for 60 minute(s).
Timings (38.2s total, 214 calls, 3 retried):
  issue_full_fetch   43 calls  p50  412ms  p95 1180ms
  pr_scan            12 calls  p50  260ms  p95  495ms
  pr_full_page      159 calls  p50  186ms  p95  940ms
```

### Summarizing recorded timings

```console
$ ghx telemetry summary --repo cli/cli
kind                count  p50_ms  p95_ms  retry_rate
issue_full_fetch      43     412    1180        0.00
pr_scan               12     260     495        0.00
pr_full_page         159     186     940        0.02
```

### Exporting for analysis

```console
$ ghx telemetry export --kind pr_full_page -o pr-pages.json
Exported 159 event(s) to pr-pages.json
```

### No data yet

```console
$ ghx telemetry summary
error: no telemetry data at /home/u/.local/share/ghx/telemetry.db
hint: enable recording with --telemetry (or GHX_TELEMETRY=1), then run a command
$ echo $?
1
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
| FR-5 | `--telemetry`, `GHX_TELEMETRY` | Off by default; recording starts only when one of them is set |
| FR-6 | `cache` (summary on stderr) | Post-run timing summary after `cache` |
| FR-7 | `telemetry summary` | Counts, percentiles, retry rate by kind |
| FR-8 | `telemetry export --format json` | Machine-readable JSON for scripts |
| FR-9 | `mock serve` (latency injection) | Configuration of injection lives in the mock command's own flags, not the telemetry surface |
| FR-10 | `cache` | `--telemetry` makes the elapsed-time and per-page breakdown available in the summary |
| FR-11 | `--telemetry` (global) | Rate-limit remaining is captured per call when the response carries it |
| NFR-2 | `--telemetry` (global) | A failed write never reaches stdout/stderr and never changes the exit code |
| NFR-7 | `ghx --help`, `ghx cache --help` | The telemetry controls are documented in both help texts |

## Out of Scope

- Any remote analytics backend or upload command (forbidden by the offline-first constraint).
- A configuration file for telemetry defaults (requirements Open Question 1).
- Retention automation: `telemetry clear --before` is manual; automatic pruning is not designed here.
- The exact mock-server latency-injection flags; they belong to the `mock serve` surface and are deferred to that command's design.

## Open Questions

1. Should `--telemetry` be a persistent root flag only, or also accepted per-command where it matters most (`cache`)?
2. Should `telemetry summary` default to the current repository, or to all repositories, when `--repo` is omitted?
3. Is `--json` on `telemetry summary` needed at all, given `telemetry export` already produces machine-readable output?
