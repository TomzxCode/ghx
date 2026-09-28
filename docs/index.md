# ghx

A GitHub CLI that calls the GitHub GraphQL API to retrieve issues, pull requests, and comments, caching all results to disk to minimise API calls.

Cache lives at `~/.cache/ghx/cache/` (a single indexed `cache.db` by default; the file backend stores per-repository JSON under `<host>/<owner>/<repo>/`).

## Quick start

```bash
go install github.com/tomzxcode/ghx@main
export GH_TOKEN=ghp_...
ghx cache --repo cli/cli
ghx issue list
ghx pr list
```

See the [User Guide](user-guide/installation.md) for detailed instructions.
