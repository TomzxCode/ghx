# Repositories

## List cached repositories

```bash
ghx repo list
```

Lists all repositories that have been cached locally. This command reads the filesystem only and makes no network calls.

Example output:

```
REPO                      ISSUES  PRS  CACHED AGE  STATUS  ISSUE-LIST  ISSUE-VIEW  PR-LIST  PR-VIEW
octocat/hello-world       42      15   2h30m       fresh   92% (13)    80% (5)     75% (8)  60% (5)
alice/another-repo        10      3    1d          stale   —           —           —        —
```

For GitHub Enterprise hosts, the full `HOST/OWNER/REPO` format is displayed.

The status column shows `fresh` when the cache is within its configured duration, `stale` otherwise, and `no cache info` when the repository has no cache metadata.

The four hit-rate columns report how often each retrieval operation was served from the local cache instead of calling GitHub, with the total number of recorded retrievals in parentheses.
The counts are read from the local telemetry database and are split by operation: issue list, issue view, PR list, and PR view.
A dash means no retrieval of that kind has been recorded for the repository, for example when telemetry is disabled or was cleared.
Telemetry is local only; see [Telemetry](telemetry.md).

Use `repo list` to see which repositories are cached and how many issues and PRs each holds.

## JSON output

`--json` takes an optional comma-separated list of fields and emits a compact JSON array, in the same spirit as `gh pr list --json`:

```bash
ghx repo list --json nameWithOwner,issueCount,prCount,status,hitRates
```

```json
[{"hitRates":{"issueList":{"hits":12,"misses":1,"percent":92,"total":13},"issueView":{"hits":4,"misses":1,"percent":80,"total":5},"prList":{"hits":6,"misses":2,"percent":75,"total":8},"prView":{"hits":3,"misses":2,"percent":60,"total":5}},"issueCount":42,"nameWithOwner":"octocat/hello-world","prCount":15,"status":"fresh"}]
```

Supported fields: `cacheAge`, `cacheDurationMinutes`, `cachedAt`, `hitRates`, `host`, `issueCount`, `name`, `nameWithOwner`, `owner`, `prCount`, `status`.
An unknown field is rejected with the list of valid names.

- `nameWithOwner` is `OWNER/REPO` on github.com and `HOST/OWNER/REPO` elsewhere, the same label the table shows.
  `host`, `owner`, and `name` expose the parts.
- `status` is `fresh` within the repository's configured cache duration, `stale` past it, and `no cache info` when the repository has no cache metadata.
- `cachedAt` is an RFC3339 timestamp (`null` without cache metadata), `cacheAge` the human-readable age, and `cacheDurationMinutes` the configured window.
- `hitRates` maps `issueList`, `issueView`, `prList`, and `prView` to `{"hits":n,"misses":n,"total":n,"percent":n}`.
  An operation with no recorded retrievals is omitted, so an empty object means no hit-rate data at all.

Omitting the field list (bare `--json`) outputs every supported field:

```bash
ghx repo list --json
```

An empty cache prints `[]` rather than the plain-text "No cached repositories found." message.

## Repository identity

Owner and repository names are matched case-insensitively, as GitHub does. `ghx repo delete TomzxCode/ai-text-editor` and `ghx repo delete tomzxcode/ai-text-editor` address the same cache entry, and a cache written under a mixed-case spelling is collapsed to one entry on first open. Coordinates are stored lower-case, so `repo list` shows that canonical form.

## Delete a cached repository

```bash
ghx repo delete octocat/hello-world
```

Removes the cached issues, pull requests, and cache metadata for one repository. With the SQLite backend (the default), only that repository's rows are removed from the shared database. With `--storage file`, the per-repository directory is removed.

The repository can also be resolved from `--repo` or the current directory's git remote:

```bash
ghx repo delete
ghx repo delete -R octocat/hello-world
```

Deleting cached data is local only; the next `cache`, `list`, or `view` command fetches it from GitHub again. The command reports when nothing is cached for the repository.

## Clean up

Cache data is never automatically removed. To remove a single repository, use `repo delete` above. To remove every cached repository, delete the SQLite database (the default backend):

```bash
rm ~/.cache/ghx/cache/cache.db
```

With the legacy file backend, delete the per-repository directories under the cache location instead:

```bash
rm -rf ~/.cache/ghx/cache/<host>/<owner>/<repo>
```
