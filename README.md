# ghx

An extended GitHub CLI. It caches issues, pull requests, and comments to disk to minimise API calls, and provides PR/issue comment operations beyond the standard `gh` CLI: inline review comments, line-range comments, thread replies, pending reviews, and local comment stashes.

Issue/PR cache lives at `~/.cache/ghx/cache/<host>/<owner>/<repo>`. Review-comment stashes live at `~/.cache/ghx/stash/<owner>/<repo>/<pr>`.

## Installation

```bash
go install github.com/tomzxcode/ghx@main
```

Or download a pre-built binary from the [releases page](https://github.com/TomzxCode/ghx/releases/tag/latest).

## Authentication

Set a GitHub personal access token in your environment:

```bash
export GH_TOKEN=ghp_...
```

`GITHUB_TOKEN` is also supported and checked as a fallback after `GH_TOKEN`.

Alternatively, if you have the [GitHub CLI](https://cli.github.com) installed and authenticated, `ghx` will use it as a fallback (`gh auth token`).

## Usage

All commands accept `--repo [HOST/]OWNER/REPO`. When omitted, the repository is detected from the `origin` remote of the current directory.

### Cache

Pre-populate the local cache with all issues and PRs (including comments):

```bash
ghx cache
ghx cache --repo cli/cli
ghx cache --cache-duration 120   # treat cache as fresh for 2 hours
ghx cache --cache-duration 0     # always re-fetch (delta)
ghx cache --since 2026-09-01     # refresh entries created/updated since a date
ghx cache --type issues            # refresh only issues (or prs)
ghx cache --force                  # force full re-fetch
```

Example output (first-time run):

```
Caching issues for octocat/hello-world...
Cached 42 issue(s).
Caching pull requests for octocat/hello-world...
Cached 15 pull request(s).
Cache updated. Valid for 60 minute(s).
```

Example output (subsequent run, delta fetch):

```
Fetching issues updated since 2025-01-15 10:30 for octocat/hello-world...
Cached 3 issue(s).
Fetching PRs updated since 2025-01-15 10:30 for octocat/hello-world...
Cached 1 pull request(s).
Cache updated. Valid for 60 minute(s).
```

List and view commands serve from the cache when it is fresh, and fall back to the GitHub API otherwise.

### Issues

```bash
# List open issues (default)
ghx issue list

# Filtering
ghx issue list --state all
ghx issue list --state closed
ghx issue list --author alice
ghx issue list --assignee bob
ghx issue list --label bug
ghx issue list --label bug --label p1   # AND logic
ghx issue list --milestone v2.0
ghx issue list --mention carol
ghx issue list --app dependabot
ghx issue list --search "memory leak"
ghx issue list --limit 10

# View a single issue
ghx issue view 42
ghx issue view 42 --comments
ghx issue view 42 --refresh   # force fetch from GitHub and update cache
```

Example output (`issue list`):

```
#42  Fix login bug                          bug, priority     5  2025-01-15
#41  Update README                          documentation     2  2025-01-14
#40  Very long title that will be truncat…  enhancement       0  2025-01-13
```

Example output (`issue view 42 --comments`):

```
#42 Fix login bug
OPEN • opened by octocat • 5 comment(s)

Labels:    bug, priority
Assignees: alice
Milestone: v1.0
Created:   2025-01-10 14:30
URL:       https://github.com/octocat/hello-world/issues/42

The login page crashes when...

── Comment 1 by reviewer1 (2025-01-12 09:15) ──

I can reproduce this on Firefox.

── Comment 2 by octocat (2025-01-12 10:30) ──

Thanks, I'll look into it.
```

### Pull Requests

```bash
# List open PRs (default)
ghx pr list

# Filtering
ghx pr list --state all
ghx pr list --state merged
ghx pr list --author alice
ghx pr list --assignee bob
ghx pr list --label enhancement
ghx pr list --base main
ghx pr list --head feat/dark-mode
ghx pr list --draft
ghx pr list --app dependabot
ghx pr list --search "fix crash"
ghx pr list --limit 10

# View a single PR
ghx pr view 10
ghx pr view 10 --comments
ghx pr view 10 --refresh      # force fetch from GitHub and update cache
```

Example output (`pr list`):

```
#10  Add new feature         feature → main    approved          3  2025-01-15
#9   Fix edge case [draft]   fix-bug → dev     review required   1  2025-01-14
```

Example output (`pr view 10 --comments`):

```
#10 Add new feature
OPEN • opened by octocat • 3 comment(s)

Branch:    feature → main
Review:    approved
Labels:    enhancement
Assignees: alice, bob
Created:   2025-01-08 11:00
URL:       https://github.com/octocat/hello-world/pull/10

This PR adds a new feature...

── Comment 1 by reviewer1 (2025-01-09 08:30) ──

Looks good, approved!

── Comment 2 by octocat (2025-01-09 09:00) ──

Thanks for the review.
```

### Repositories

List locally cached repositories:

```bash
ghx repo list
```

Example output:

```
REPO                      ISSUES  PRS  CACHED AGE  STATUS
octocat/hello-world       42      15   2h30m       fresh
alice/another-repo        10      3    1d          stale
```

### PR comments (inline, replies, pending, stash)

```bash
# Top-level comment
ghx pr comment 10 --body "Looks good"

# Inline comment on a line or range
ghx pr comment 10 --file src/main.go --line 42 --body "Nit"
ghx pr comment 10 --file src/main.go --line 42-45 --body "Consider extracting"

# File-level comment (no --line)
ghx pr comment 10 --file src/main.go --body "Overall looks clean"

# Reply to an existing review thread
ghx pr comment 10 --reply-thread <thread-id> --body "Agreed"

# Add to a pending review instead of posting immediately
ghx pr comment 10 --file src/main.go --line 42 --body "Nit" --pending

# Save to a local stash entry instead of calling the API
ghx pr comment 10 --file src/main.go --line 42 --body "Nit" --stash

# Body from stdin or a file
ghx pr comment 10 --body-file -
ghx pr comment 10 --body-file comment.txt
```

### Edit / delete comments

```bash
ghx pr comment edit   <comment-id> --body "Updated text"
ghx pr comment delete <comment-id>
ghx issue comment edit   <comment-id> --body "Updated text"
ghx issue comment delete <comment-id>
```

Use `ghx pr threads <number> --ids` or `ghx issue view <number> --ids` to find comment IDs.

### Review threads

```bash
ghx pr threads 10                       # open threads, with comments
ghx pr threads 10 --thread <thread-id>  # show one thread
ghx pr threads 10 --ids                 # include comment IDs
ghx pr threads 10 --state all
ghx pr threads 10 --state resolved
```

### Pending reviews

```bash
ghx pr review create  10
ghx pr review list    10
ghx pr review submit  10 --event COMMENT
ghx pr review submit  10 --event APPROVE --body "LGTM"
ghx pr review submit  10 --event REQUEST_CHANGES --body "See comments"
ghx pr review discard <review-id>
```

### Review-comment stashes

```bash
ghx pr review stash push 10 -m "nit comments"   # save pending -> stash@{0}
ghx pr review stash list 10                     # list stash entries
ghx pr review stash pop  10                     # restore stash@{0} into a pending review
ghx pr review stash pop  10 --stash 1           # restore stash@{1}
ghx pr review stash drop 10                     # discard stash@{0}
```

### Issue comments

```bash
ghx issue comment 42 --body "Fixed in #50"
ghx issue comment 42 --body-file -
ghx issue view 42 --ids                 # show comment IDs
```

### Stats

Generate an HTML report of pull request activity (lead times, review
engagement, PR sizes, trends) for one or more repositories:

```bash
ghx cache                                            # populate the cache first
ghx stats --from 2026-05-01 --to 2026-05-31          # current repository
ghx stats acme/widget acme/gadget --from 2026-05-01 --to 2026-05-31 \
    --author alice,carol --reviewer alice,bob -o may-report.html
```

The report includes summary cards, a monthly trend chart, an author × reviewer
matrix, per-author lead times (time to merge, time in draft, time to first
review, time to approve), contribution and PR size breakdowns, reviewer
engagement with review-request response times, a PR size versus merge time
table, peak-activity and size-distribution charts, notable PR lists, and an
optional appendix of individual PRs (`--list-prs`).
Run `ghx cache --force` once after upgrading so reports can use the review,
timeline and size data collected during caching.

## How caching works

| Command | Cache behaviour |
|---|---|
| `cache` | Fetches everything (all states, with comments) and writes one JSON file per issue/PR. Skips if cache is younger than `--cache-duration`. Supports delta fetch, `--since` windowed refresh, and `--type issues/prs` partial refresh. |
| `issue list` / `pr list` | Reads all cached files and filters in-memory when cache is fresh; falls back to the GitHub API with server-side filters otherwise. |
| `issue view` / `pr view` | Serves from the individual cached file when it is less than 60 minutes old; fetches from the API and updates the cache otherwise. `--refresh` bypasses all cache checks, fetches from the API, and updates the cache. |

## Flag reference

### Global

| Flag | Description |
|---|---|
| `-R, --repo [HOST/]OWNER/REPO` | Target repository (default: detected from `git remote origin`) |
| `--api-url string` | Override the GitHub GraphQL API endpoint URL (for testing) |
| `--cache-dir string` | Override the cache directory path |

### `cache`

| Flag | Default | Description |
|---|---|---|
| `--cache-duration int` | `60` | Minutes before the cache is considered stale |
| `--force` | `false` | Re-fetch even if the cache is still fresh (full fetch, not delta) |
| `--since string` | | Only refresh entries created or updated since this date (`YYYY-MM-DD`, RFC3339, or `YYYY-MM-DD HH:MM`) |
| `--type string` | `both` | Which entries to refresh: `issues`, `prs`, or `both` |
| `--retries int` | `2` | Retries on transient errors (rate limits, 5xx); `0` means a single attempt. Total attempts = retries + 1 |

### `issue list`

| Flag | Description |
|---|---|
| `--app string` | Filter by GitHub App author |
| `-a, --assignee string` | Filter by assignee |
| `-A, --author string` | Filter by author |
| `--json` | Output as JSON |
| `-l, --label strings` | Filter by label (repeat for AND logic) |
| `-L, --limit int` | Maximum results (default 1000) |
| `--mention string` | Filter by mention |
| `-m, --milestone string` | Filter by milestone number or title |
| `--no-truncate` | Don't truncate long titles |
| `-S, --search string` | Search query |
| `-s, --state string` | `open` \| `closed` \| `all` (default `open`) |

### `issue view`

| Flag | Description |
|---|---|
| `-c, --comments` | Show comments |
| `--ids` | Show comment IDs (useful for editing/deleting) |
| `--json` | Output as JSON |
| `--refresh` | Force fetch from GitHub and update cache |

### `pr list`

| Flag | Description |
|---|---|
| `--app string` | Filter by GitHub App author |
| `-a, --assignee string` | Filter by assignee |
| `-A, --author string` | Filter by author |
| `-B, --base string` | Filter by base branch |
| `-d, --draft` | Show only draft PRs |
| `-H, --head string` | Filter by head branch |
| `--json` | Output as JSON |
| `-l, --label strings` | Filter by label (repeat for AND logic) |
| `-L, --limit int` | Maximum results (default 1000) |
| `--no-truncate` | Don't truncate long titles |
| `-S, --search string` | Search query |
| `-s, --state string` | `open` \| `closed` \| `merged` \| `all` (default `open`) |

### `pr view`

| Flag | Description |
|---|---|
| `-c, --comments` | Show comments |
| `--json` | Output as JSON |
| `--refresh` | Force fetch from GitHub and update cache |

### `stats`

| Flag | Default | Description |
|---|---|---|
| `-A, --author strings` | | Only count PRs authored by these users |
| `--reviewer strings` | | Only count comments by these reviewers |
| `--from string` | | Period start (`YYYY-MM-DD` or RFC3339) |
| `--to string` | | Period end, inclusive (`YYYY-MM-DD` or RFC3339) |
| `-s, --state string` | `all` | `open` \| `closed` \| `merged` \| `all` |
| `-o, --output string` | `stats-report.html` | Output HTML file path |
| `--include-bots` | `false` | Include bot accounts as reviewers |
| `--top int` | `5` | Number of pull requests per notable-PRs list |
| `--list-prs` | `false` | Include the table of individual pull requests (can be large) |

## Related projects

- [gitcrawl](https://github.com/openclaw/gitcrawl)