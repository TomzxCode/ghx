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
ghx cache --force                # discard resume state and full re-fetch
```

Every plain run refreshes the cache with a delta fetch from the last cache write, even when the cache is still fresh.

Issues and pull requests are fetched concurrently, so a run takes about as long as the slower portion instead of the sum of both. While both run, the progress bar counts their items together and shows each portion's figure in its label. A failure in one portion keeps whatever the other portion wrote, and the next run resumes both from their saved positions.

On an unchanged repository that costs three small requests (one issue page, one lightweight PR scan page, and one tiny full-payload page for the boundary PR), and a changed one fetches only what changed.

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
- `--force` with a partial `--type` resets only that portion's resume cursor.

### Refresh a specific window with --since

`--since` refreshes only entries created or updated on or after a given date, instead of the default last-cache-write delta:

```bash
ghx cache --since 2026-09-01
ghx cache --since 2026-09-01T15:04:05Z
ghx cache --since "2026-09-01 15:04"
```

Accepted formats: `YYYY-MM-DD` (UTC midnight), RFC3339, and naive datetimes (local time).

Notes:

- Issues use a server-side filter with exact timestamps. PRs are fetched in two phases: a lightweight newest-first walk locates the exact window (so the progress bar knows the total), then the window is fetched in full-payload pages sized to the number of in-window items. Both avoid the search API.
- `--since` always fetches and cannot be combined with `--force`.
- Using a `--since` date newer than the last cache write intentionally skips items in between; run `ghx cache --force` afterwards if you need to backfill them.

First run fetches everything. Example output:

```
Caching issues for octocat/hello-world...
Caching pull requests for octocat/hello-world...
Cached 42 issue(s).
Cached 15 pull request(s).
Cache updated. Valid for 60 minute(s).
```

Subsequent runs use a delta fetch, only retrieving items updated since the last cache write:

```
Fetching issues updated since 2025-01-15 10:30 for octocat/hello-world...
Fetching pull requests updated since 2025-01-15 10:30 for octocat/hello-world...
Cached 3 issue(s).
Cached 1 pull request(s).
Cache updated. Valid for 60 minute(s).
```

The two `Cached` lines appear in whichever order the portions finish.

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

It does not gate `ghx cache` itself, which always refreshes with a delta fetch unless you pass `--if-stale`.

## Cache behavior per command

| Command | Behavior |
|---|---|
| `cache` | Fetches all issues and PRs (all states, with comments). Each run delta-refreshes from the last cache write; `--if-stale` skips the run while the cache is still within `--cache-duration`. Supports `--since` windowed refresh and `--type issues/prs` partial refresh. |
| `issue list` / `pr list` | Reads the cache and applies filters when the cache is fresh (indexed SQL predicates for the SQLite backend). Falls back to the GitHub API when stale, and writes each entry not already cached to the cache in full (comments, reviews, timeline), so the record is identical to what `ghx cache` writes. |
| `issue view` / `pr view` | Serves the individual cached item if the full cache is fresh, or if it was written less than 60 minutes ago. Otherwise fetches from the API and saves to cache; an item missing from an otherwise-fresh cache is fetched and cached rather than reported as missing. When the number is cached under the other type (a pull request viewed as an issue, or the reverse), it reports that type and exits without querying the API. `--refresh` bypasses the same-type cache checks. |

## Notes

- Repository coordinates are stored lower-case. GitHub owner and repository names are case-insensitive, so a repository has one cache entry however its name is spelled; a cache written before this rule is collapsed to the lower-case key on first open.
- Run a read command with `--log-level debug` to see where its time goes: repository resolution, cache store open, token resolution, the list API call, and the cache backfill (scan, per-item detail fetch, and totals).
- A read command (`list`/`view`) never marks the repository's cache fresh; only `cache` (or `cache --type`) advances the freshness window. Reads fill the cache without claiming the repository is fully cached, so `ghx cache` still does its full or delta fetch afterwards.
- Transient failures (rate limits and GitHub 5xx errors such as an HTML 502 from a proxy) are retried automatically with exponential backoff. Each fetched page is already persisted, so if a run still fails, re-running resumes from the last page written.
- Cache data is never automatically cleaned up. Delete `~/.cache/ghx/cache/cache.db` (SQLite) or the per-repository directories under `~/.cache/ghx/cache/` (file) to free space.
- The `--mention` and `--app` filters cannot be evaluated from cached data and are silently skipped when serving from cache.
- Bulk cache operations fetch up to 100 comments per item.
