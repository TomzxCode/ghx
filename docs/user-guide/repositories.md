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
