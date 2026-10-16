# Repositories

## List cached repositories

```bash
ghx repo list
```

Lists all repositories that have been cached locally. This command reads the filesystem only and makes no network calls.

Example output:

```
REPO                      ISSUES  PRS  CACHED AGE  STATUS
octocat/hello-world       42      15   2h30m       fresh
alice/another-repo        10      3    1d          stale
```

For GitHub Enterprise hosts, the full `HOST/OWNER/REPO` format is displayed.

The status column shows `fresh` when the cache is within its configured duration, and `stale` otherwise.

Use `repo list` to see which repositories are cached and how many issues and PRs each holds.

## Clean up

Cache data is never automatically removed. To free disk space, delete the SQLite database (the default backend):

```bash
rm ~/.cache/ghx/cache/cache.db
```

With the legacy file backend, delete the per-repository directories under the cache location instead:

```bash
rm -rf ~/.cache/ghx/cache/<host>/<owner>/<repo>
```
