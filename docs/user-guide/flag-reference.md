# Flag Reference

## Global flags

These flags apply to all commands.

| Flag | Description |
|---|---|
| `--repo [HOST/]OWNER/REPO` | Target repository. When omitted, detected from `git remote origin` in the current directory. |
| `--cache-dir string` | Override the cache directory (default `~/.cache/ghx/cache/`). |
| `--api-url string` | Override the GitHub GraphQL API endpoint (for testing). |
| `--version`, `-v` | Print version. |
| `--help`, `-h` | Show help. |

## cache

| Flag | Default | Description |
|---|---|---|
| `--cache-duration int` | `60` | Minutes before the cache is considered stale |
| `--force` | `false` | Re-fetch even if the cache is still fresh (full fetch, not delta) |

## issue list

| Flag | Short | Default | Description |
|---|---|---|---|
| `--state string` | `-s` | `open` | Filter by state: `open`, `closed`, or `all` |
| `--author string` | `-A` | | Filter by author |
| `--assignee string` | `-a` | | Filter by assignee |
| `--label strings` | `-l` | | Filter by label (repeat for AND logic) |
| `--milestone string` | `-m` | | Filter by milestone number or title |
| `--mention string` | | | Filter by mention |
| `--app string` | | | Filter by GitHub App author |
| `--search string` | `-S` | | Search query (case-insensitive substring match on title and body) |
| `--limit int` | `-L` | `1000` | Maximum number of results |
| `--json` | | `false` | Output as JSON |
| `--no-truncate` | | `false` | Don't truncate long titles |

## issue view

| Flag | Short | Default | Description |
|---|---|---|---|
| `--comments` | `-c` | `false` | Show comments |
| `--ids` | | `false` | Show comment IDs (useful for editing/deleting) |
| `--json` | | `false` | Output as JSON |
| `--refresh` | | `false` | Force fetch from GitHub and update cache |

## issue comment

| Flag | Short | Default | Description |
|---|---|---|---|
| `--body string` | `-b` | | Comment body text |
| `--body-file string` | `-F` | | Read body from file (use "-" for stdin) |

`comment edit <comment-id>` accepts the same `--body` / `--body-file` flags.
`comment delete <comment-id>` takes no flags.

## pr list

| Flag | Short | Default | Description |
|---|---|---|---|
| `--state string` | `-s` | `open` | Filter by state: `open`, `closed`, `merged`, or `all` |
| `--author string` | `-A` | | Filter by author |
| `--assignee string` | `-a` | | Filter by assignee |
| `--label strings` | `-l` | | Filter by label (repeat for AND logic) |
| `--base string` | `-B` | | Filter by base branch name |
| `--head string` | `-H` | | Filter by head branch name |
| `--draft` | `-d` | `false` | Show only draft PRs |
| `--app string` | | | Filter by GitHub App author |
| `--search string` | `-S` | | Search query (case-insensitive substring match on title and body) |
| `--limit int` | `-L` | `1000` | Maximum number of results |
| `--json` | | `false` | Output as JSON |
| `--no-truncate` | | `false` | Don't truncate long titles |

## pr view

| Flag | Short | Default | Description |
|---|---|---|---|
| `--comments` | `-c` | `false` | Show comments |
| `--json` | | `false` | Output as JSON |
| `--refresh` | | `false` | Force fetch from GitHub and update cache |

## pr comment

| Flag | Short | Default | Description |
|---|---|---|---|
| `--body string` | `-b` | | Comment body text |
| `--body-file string` | `-F` | | Read body from file (use "-" for stdin) |
| `--file string` | | | File path for inline comments |
| `--line string` | | | Line number or range (e.g. 42 or 42-45) |
| `--side string` | | `RIGHT` | Diff side: LEFT or RIGHT |
| `--reply-thread string` | | | Thread ID to reply to |
| `--pending` | | `false` | Add to a pending review instead of posting immediately |
| `--stash string` | | | Save to a local stash entry (optional index, default 0) |

`comment edit <comment-id>` accepts `--body` / `--body-file`.
`comment delete <comment-id>` takes no flags.

## pr threads

| Flag | Short | Default | Description |
|---|---|---|---|
| `--thread string` | | | Show a specific thread by ID |
| `--state string` | | `open` | Filter by state: open, resolved, all |
| `--ids` | | `false` | Show comment IDs |

## pr review submit

| Flag | Short | Default | Description |
|---|---|---|---|
| `--event string` | | `COMMENT` | Review event: COMMENT, APPROVE, or REQUEST_CHANGES |
| `--body string` | `-b` | | Review summary body |
| `--body-file string` | `-F` | | Read body from file (use "-" for stdin) |
| `--review string` | | | Specific review ID to submit (defaults to your current pending review) |

`pr review create <number>`, `pr review list <number>`, and `pr review discard <review-id>` take no command-specific flags.

## pr review stash

| Subcommand | Flag | Short | Default | Description |
|---|---|---|---|---|
| `push <number>` | `--message string` | `-m` | | Stash description |
| `pop <number>` | `--stash int` | | `0` | Stash entry index to restore |
| `drop <number>` | `--stash int` | | `0` | Stash entry index to drop |

`pr review stash list <number>` takes no command-specific flags.

## repo list

No command-specific flags. Uses only the global `--cache-dir`.

## stats

Repositories are positional `[HOST/]OWNER/REPO` arguments. When omitted, the repository is resolved from `--repo` or the current directory's git remote.

| Flag | Short | Default | Description |
|---|---|---|---|
| `--author strings` | `-A` | | Only count PRs authored by these users |
| `--reviewer strings` | | | Only count comments by these reviewers |
| `--from string` | | | Period start (`YYYY-MM-DD` or RFC3339) |
| `--to string` | | | Period end, inclusive (`YYYY-MM-DD` or RFC3339) |
| `--state string` | `-s` | `all` | Filter by PR state: `open`, `closed`, `merged`, or `all` |
| `--output string` | `-o` | `stats-report.html` | Output HTML file path |
| `--include-bots` | | `false` | Include bot accounts as reviewers |

## mock serve

| Flag | Default | Description |
|---|---|---|
| `--preset string` | `default` | Config preset: default, small, or none |
| `--repos strings` | | Repositories in "owner/repo" format (comma-separated) |
| `--users int` | | Number of simulated users (0 = use preset) |
| `--history duration` | | Duration of simulated history (e.g. 720h = 30 days) |
| `--issues-per-repo int` | | Issues per repo (0 = use preset) |
| `--prs-per-repo int` | | PRs per repo (0 = use preset) |
| `--comments-per-issue int` | | Max comments per issue (0 = use preset) |
| `--comments-per-pr int` | | Max comments per PR (0 = use preset) |
| `--assignees-per-issue int` | | Max assignees per issue (0 = use preset) |
| `--assignees-per-pr int` | | Max assignees per PR (0 = use preset) |
| `--labels-per-item int` | | Max labels per item (0 = use preset) |
| `--milestones-per-repo int` | | Milestones per repo (0 = use preset) |
| `--close-rate float` | | Fraction of issues closed [0,1] (-1 = use preset) |
| `--merge-rate float` | | Fraction of PRs merged [0,1] (-1 = use preset) |
| `--draft-rate float` | | Fraction of PRs as drafts [0,1] (-1 = use preset) |
| `--review-rate float` | | Fraction of PRs reviewed [0,1] (-1 = use preset) |
| `--activity-bursts int` | | Number of high-activity windows (-1 = use preset) |
| `--seed int` | | RNG seed (0 = use preset) |
| `--stats` | `false` | Print simulation stats and exit without serving |
