# Cache

ghx caches GitHub data locally (by default in a single SQLite database) and serves results from cache without hitting the API after an initial fetch.

## Cache location

The default SQLite backend stores everything in one database:

```
~/.cache/ghx/cache/cache.db
```

The legacy file backend stores one JSON file per item:

```
~/.cache/ghx/cache/<host>/<owner>/<repo>/
├── .cache_info.json
├── issues/
│   ├── 1.json
│   ├── 2.json
│   └── ...
└── prs/
    ├── 1.json
    ├── 2.json
    └── ...
```

Override the base directory with `--cache-dir`:

```bash
ghx --cache-dir /tmp/gh-cache cache --repo cli/cli
```

## Storage backends

ghx defaults to an indexed SQLite database and also supports the legacy file backend:

| Backend | Layout | Select |
|---|---|---|
| `sqlite` (default) | A single SQLite database at `<cache-dir>/cache.db` with indexed lookups | `--storage sqlite` or `GHX_STORAGE=sqlite` |
| `file` | One JSON file per issue/PR under `<cache-dir>/<host>/<owner>/<repo>/` | `--storage file` or `GHX_STORAGE=file` |

Select the file backend per invocation with `--storage`, or set `GHX_STORAGE` in your environment:

```bash
ghx --storage file issue list --repo cli/cli --state all
GHX_STORAGE=file ghx pr list --repo cli/cli
```

`--storage` takes precedence over `GHX_STORAGE`. Both backends are keyed by the same repository coordinates, so commands and filters behave identically; only the on-disk representation and query performance differ. On large repositories (hundreds to thousands of cached items), listing, filtering, and searching against SQLite is several times faster because it avoids reading and parsing every item file.

### Migrate an existing file cache to SQLite

The default SQLite backend does not read the file cache. If you have an existing file cache, import it once:

```bash
ghx cache migrate                 # migrate every cached repository
ghx cache migrate --repo cli/cli  # migrate a single repository
```

The file cache is left in place (the migration is non-destructive), and re-running it is safe (idempotent). To keep using the file cache without migrating, pass `--storage file`.

## Populate the cache

```bash
ghx cache
ghx cache --repo cli/cli
ghx cache --cache-duration 120   # record a 2-hour freshness window for reads
ghx cache --if-stale             # do nothing while the cache is still fresh
ghx cache --force                # delete the selected type and re-fetch it
```

Every plain run fills the gaps between the recorded coverage and the current head, even when the cache is still fresh.

Issues and pull requests are fetched concurrently. Within each type, the uncovered ranges are split into segments that run on `--workers` goroutines, so a large backfill does not wait on one range at a time. A failure in a segment leaves that range uncovered for the next run; the ranges that finished stay covered.

On an unchanged repository the work is the catch-up tail since the last run, usually a single light page per type.

The `--cache-duration` window decides when read commands stop trusting the cache; it does not stop `ghx cache` from refreshing.

Use `--if-stale` for scripts that only need the cache warm and want a no-op while it is fresh.

### Refresh one portion only with --type

`--type issues` or `--type prs` refreshes only that portion; `--type both` (the default) refreshes both. An explicit `--type` always fetches, even when the cache is fresh:

```bash
ghx cache --type issues
ghx cache --type prs
ghx cache --type prs --since 2026-09-01   # combine with a date window
ghx cache --type issues --force           # full issue re-fetch only
```

Notes:

- A partial run never marks an incomplete cache complete; run a plain `ghx cache` (or `--force`) to finish fetching the missing portion.
- `--force` with a partial `--type` deletes only that portion's items and coverage.

### Coverage segments

Coverage is stored as half-open `[start, end)` intervals per type. Each completed segment is merged with its neighbours, so a long backfill ends up as one or a few ranges rather than a row per page.

- Segments are independent: each one can start from a synthesized cursor at its own start time.
- `--workers` sets how many run at once (default 2, maximum 16). Full pull request pages are capped at two in flight because GitHub's secondary limits punish wider concurrency.
- The width is chosen from the uncovered duration: issues target four segments per worker, full pull request pages two per slot, clamped between 15 minutes and 7 days. A worker that finishes early takes the next pending segment.
- A catch-up tail from the run's start to the head is processed after the segments, so updates that landed during the run are not missed.

### Ensure coverage from a date with --since

`--since` sets the start of the range the run ensures, instead of starting at the current coverage frontier:

```bash
ghx cache --since 2026-09-01
ghx cache --since 2026-09-01T15:04:05Z
ghx cache --since "2026-09-01 15:04"
```

Accepted formats: `YYYY-MM-DD` (UTC midnight), RFC3339, and naive datetimes (local time).

Notes:

- `--since` sets the start of the range the run ensures. Covered ranges are skipped; uncovered ranges are split into segments.
- Repeating the same `--since` over a covered range fetches nothing.
- A lower `--since` fills only the gap below the existing coverage.
- `--force --since` deletes the selected type and rebuilds only the window from the date.
- Anything below the earliest covered range is out of scope until a later run starts from an earlier date.

First run fetches everything. Example output:

```
Caching issues for octocat/hello-world...
Caching pull requests for octocat/hello-world...
Cached 42 issue(s).
Cached 15 pull request(s).
Cache updated. Valid for 60 minute(s).
```

Subsequent runs fill only the gap between the recorded coverage and the head:

```
Fetching issues updated since 2025-01-15 10:30 for octocat/hello-world...
Fetching pull requests updated since 2025-01-15 10:30 for octocat/hello-world...
Cached 3 issue(s).
Cached 0 pull request(s).
Cache updated. Valid for 60 minute(s).
```

The two `Cached` lines appear in whichever order the portions finish.
On a terminal, each portion gets a row from the start of the run, stacked one above the other.
Each row's description names its state (`starting`, `caching`).
A row shows two counts: `<done>/<total> updated` for the current run and `<cached>/<repository total> cached` for the cache as a whole.
The bar and ETA follow the current update, so a refresh starts empty rather than part filled.
The cached figure uses the repository total, not the size the cache will reach when this run ends.
A portion with nothing to fetch shows `0/0 updated, <n>/<n> cached, up to date`.
Once it is caching, the row shows the item rate and estimated time left.
A finished portion keeps its row while the other one still runs.
The remaining GitHub API points appear on a line of their own below the rows.

## How freshness works

The cache metadata (the `.cache_info.json` file for the file backend, the `cache_meta` table for SQLite) tracks when the cache was last written and the configured duration. The file-backend form:

```json
{
  "cachedAt": "2025-01-15T10:30:00Z",
  "duration": 60
}
```

A cache is considered **fresh** when `time.Since(cachedAt) < duration × 1 minute`.

The window decides how read commands behave: when the cache is fresh, `list` and `view` serve entirely from the cache with no API calls; when stale, they fall back to the GitHub API and write what they fetch back into the cache.

The same duration is the per-item window for `view`: an item written within it is served without an API call even when the repository's cache as a whole is stale. A repository with no cache metadata (populated only by `view` and `list` reads) falls back to 60 minutes.

It does not gate `ghx cache` itself, which always fills uncovered ranges unless you pass `--if-stale`.

## Cache behavior per command

| Command | Behavior |
|---|---|
| `cache` | Fetches all issues and PRs (all states, with comments). Each run fills the uncovered ranges between the recorded coverage and the head; `--if-stale` skips the run while the cache is still within `--cache-duration`. Supports `--since`, `--workers`, and `--type issues/prs`. |
| `issue list` / `pr list` | Reads the cache and applies filters when the cache is fresh (indexed SQL predicates for the SQLite backend). Falls back to the GitHub API when stale, and writes each entry not already cached to the cache in full (comments, reviews, timeline), so the record is identical to what `ghx cache` writes. |
| `issue view` / `pr view` | Serves the individual cached item if the full cache is fresh, or if it was written within the cache's configured duration (60 minutes by default). Otherwise fetches from the API and saves to cache; an item missing from an otherwise-fresh cache is fetched and cached rather than reported as missing. When the number is cached under the other type (a pull request viewed as an issue, or the reverse), it reports that type and exits without querying the API. `--refresh` bypasses the same-type cache checks. |

## Notes

- Repository coordinates are stored lower-case. GitHub owner and repository names are case-insensitive, so a repository has one cache entry however its name is spelled; a cache written before this rule is collapsed to the lower-case key on first open.
- Run a read command with `--log-level debug` to see where its time goes: repository resolution, cache store open, token resolution, the list API call, and the cache backfill (scan, per-item detail fetch, and totals).
- A read command (`list`/`view`) never marks the repository's cache fresh; only `cache` (or `cache --type`) advances the freshness window. Reads fill the cache without claiming the repository is fully cached, so `ghx cache` still fills the uncovered ranges afterwards.
- Transient failures (rate limits and GitHub 5xx errors such as an HTML 502 from a proxy) are retried automatically with exponential backoff. Each fetched page is already persisted, so if a run still fails, re-running resumes from the last page written.
- Cache data is never automatically cleaned up. Delete `~/.cache/ghx/cache/cache.db` (SQLite) or the per-repository directories under `~/.cache/ghx/cache/` (file) to free space.
- The `--mention` and `--app` filters cannot be evaluated from cached data and are silently skipped when serving from cache.
- Bulk cache operations fetch up to 100 comments per item.
