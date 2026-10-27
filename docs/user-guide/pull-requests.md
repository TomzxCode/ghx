# Pull Requests

## List pull requests

```bash
ghx pr list
```

When the cache is fresh, results are served from disk. Otherwise the GitHub API is used.

### Filter by state

```bash
ghx pr list                     # open (default)
ghx pr list --state closed
ghx pr list --state merged
ghx pr list --state all
```

### Filter by author or assignee

```bash
ghx pr list --author alice
ghx pr list --assignee bob
```

### Filter by branch

```bash
ghx pr list --base main
ghx pr list --head feat/dark-mode
```

### Filter by draft status

```bash
ghx pr list --draft
```

### Filter by label

Labels use AND logic. Repeat the flag to require all specified labels:

```bash
ghx pr list --label enhancement
ghx pr list --label enhancement --label reviewed
```

### Search

Case-insensitive substring match against title and body:

```bash
ghx pr list --search "fix crash"
```

### Limit results

```bash
ghx pr list --limit 10
```

When the total exceeds the limit, a log line is printed to stderr:

```
info showing pull requests shown=10 total=30
```

### JSON output

`--json` takes a comma-separated list of fields and emits a compact JSON array
containing only those fields, in the same spirit as `gh pr list --json`:

```bash
ghx pr list --state merged --head feat/dark-mode --json number,headRefOid,mergedAt
```

```json
[{"headRefOid":"2c354efe48616041586de6d5f16bc15f94fc659a","mergedAt":"2026-09-29T10:34:00Z","number":42}]
```

Supported fields: `additions`, `assignees`, `author`, `baseRefName`, `body`,
`checks`, `closedAt`, `comments`, `createdAt`, `deletions`, `headRefName`,
`headRefOid`, `isDraft`, `labels`, `mergedAt`, `milestone`, `number`,
`reviewDecision`, `reviewRequests`, `reviewThreads`, `reviews`, `state`,
`title`, `updatedAt`, `url`. An unknown field is rejected with the list of
valid names.

`checks` is the head commit's rollup (`{"state":"SUCCESS","total":8}`) and
`reviewThreads` the thread counts (`{"total":4,"open":2}`). Both are cached
with the pull request, so reading them from a fresh cache costs no API call.
`reviewThreads.open` counts the first hundred threads, so a busier pull request
undercounts it; `total` is exact.

A list served from the cache carries both. A list that falls back to the API
carries the rollup but not the thread counts, because thread nodes are too
heavy to attach to every row of a list page; run `ghx cache` to refresh the
records first.

Omitting the field list (bare `--json`) outputs every supported field:

```bash
ghx pr list --json
```

### Disable title truncation

By default, titles are truncated to 55 characters. Use `--no-truncate` to show full titles:

```bash
ghx pr list --no-truncate
```

## View a single pull request

```bash
ghx pr view 10
```

### Show comments

```bash
ghx pr view 10 --comments
```

### Force refresh

Bypass the cache and fetch the latest data from GitHub, updating the cache:

```bash
ghx pr view 10 --refresh
```

### JSON output

```bash
ghx pr view 10 --json
```

## Show a pull request's checks

```bash
ghx pr checks 10
```

The cached pull request record carries the head commit's rollup state and how
many checks there are, not the checks themselves. `pr checks` reads them live
and lists each check run and commit status:

```
failing • 2 check(s)

failing   build
passing   ci/unit
```

Two checks can differ in shape: a check run reports a conclusion once it
finishes and a status while it runs, and a commit status reports a state
directly. `pr checks` renders both in the same words.

Pass `--json` for the same data as an object: `state`, `total`, and `checks`
(each with `type`, `name`, and the state fields GitHub reports).

```bash
ghx pr checks 10 --json
```

## Example output

### pr list

```
#10  Add new feature         feature → main    approved          3  2025-01-15
#9   Fix edge case [draft]   fix-bug → dev     review required   1  2025-01-14
```

### pr view

```
#10 Add new feature
OPEN • opened by octocat • 3 comment(s)

Branch:    feature → main
Review:    approved
Checks:    failing
Threads:   2 open of 4
Labels:    enhancement
Assignees: alice, bob
Created:   2025-01-08 11:00
URL:       https://github.com/octocat/hello-world/pull/10

This PR adds a new feature...

── Comment 1 by reviewer1 (2025-01-09 08:30) ──

Looks good, approved!
```
