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

The status column shows `fresh` when the cache is within its configured duration, and `stale` otherwise.

The four hit-rate columns report how often each retrieval operation was served from the local cache instead of calling GitHub, with the total number of recorded retrievals in parentheses.
The counts are read from the local telemetry database and are split by operation: issue list, issue view, PR list, and PR view.
A dash means no retrieval of that kind has been recorded for the repository, for example when telemetry is disabled or was cleared.
Telemetry is local only; see [Telemetry](telemetry.md).

Use `repo list` to see which repositories are cached and how many issues and PRs each holds.

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
