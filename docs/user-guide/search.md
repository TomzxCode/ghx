# Search

`ghx search` searches the issues and pull requests already in the local cache.
It never calls the GitHub API, so it answers instantly and works offline; the
results are only as current as the cache.

Two searches are available: `search issues` and `search prs`.
`ghx search` on its own prints the command help.

## Search issues

```bash
ghx search issues login
ghx search issues "memory leak"
ghx search issues --state open --label bug
ghx search issues --include-prs --no-assignee
```

## Search pull requests

```bash
ghx search prs crash
ghx search prs --state merged --base main
ghx search prs --draft
ghx search prs --review-requested @me --state open
```

## How the query is matched

- Every positional argument is one term.
- A term matches when it appears in the title, body, or comments, case-insensitively.
- All terms must match (AND), so `ghx search issues login safari` finds items
  whose title, body, or comments contain both words.
- Quote an argument to require the phrase as written:
  `ghx search issues "login bug"`.
- With no query terms, every cached item that matches the filters is listed.

Use `--match` to widen or narrow where terms are sought:

```bash
ghx search issues reproduce --match comments
ghx search issues login --match title --match body
```

The values are `title`, `body`, and `comments`, and default to all three.
Terms are not parsed as GitHub qualifiers: `is:open` or `label:bug` in the
query are literal text, so use the flags instead.

## Which repositories are searched

Every cached repository is searched by default, and each result shows the
repository it belongs to.

Limit the search to one repository with the global `--repo` flag, or filter by
owner with `--owner`:

```bash
ghx search issues login --repo octocat/hello-world
ghx search issues login --owner octocat
```

Unlike the list and view commands, search does not resolve the current
directory's git remote; that keeps a global cache search predictable and lets it
run outside a repository.

Run `ghx repo list` to see which repositories the cache holds.

## Filters

### Shared

| Flag | Short | Description |
|---|---|---|
| `--state string` | `-s` | Filter by state: issues `open`, `closed`, `all`; PRs also `merged` |
| `--author string` | `-A` | Filter by author (login or `@me`) |
| `--assignee string` | `-a` | Filter by assignee (login or `@me`) |
| `--label strings` | `-l` | Filter by label (repeat for AND logic) |
| `--milestone string` | `-m` | Filter by milestone number or title |
| `--app string` | | Filter by GitHub App author (approximate; see caveats) |
| `--owner strings` | | Filter on repository owner (repeatable) |
| `--no-assignee` | | Items with no assignee |
| `--no-label` | | Items with no label |
| `--no-milestone` | | Items with no milestone |
| `--created date` | | Filter on creation date |
| `--updated date` | | Filter on last update date |
| `--closed date` | | Filter on close date |
| `--comments number` | | Filter on comment count |
| `--commenter user` | | Items with a comment by the user |
| `--mentions user` | | Items whose body or comments mention the user |
| `--team-mentions string` | | Items whose body or comments mention the team (`org/team`) |
| `--involves user` | | Author, assignee, commenter, or mentioned user |

### Issues

| Flag | Description |
|---|---|
| `--include-prs` | Include cached pull requests, marked `[PR]` in the table and `isPullRequest: true` in JSON |

### Pull requests

| Flag | Short | Description |
|---|---|---|
| `--base string` | `-B` | Filter by base branch |
| `--head string` | `-H` | Filter by head branch |
| `--draft` | `-d` | Show only draft PRs |
| `--merged` | | Show only merged PRs |
| `--merged-at date` | | Filter on merge date |
| `--checks string` | | Filter by check status: `pending`, `success`, `failure` |
| `--review string` | | Filter by review status: `none`, `required`, `approved`, `changes_requested` |
| `--review-requested user` | | Filter on a requested reviewer (login, `org/team`, or `@me`) |
| `--reviewed-by user` | | Filter on a user who submitted a review (login or `@me`) |

### Dates

Date flags accept GitHub's qualifier grammar:

| Value | Meaning |
|---|---|
| `2026-01-02` | That calendar day (UTC) |
| `>=2026-01-02` | On or after that day |
| `>2026-01-02` | After that day |
| `<=2026-01-02` | On or before that day |
| `<2026-01-02` | Before that day |
| `2026-01-01..2026-01-31` | Inclusive range |
| `2026-01-01..` / `..2026-01-31` | Open-ended range |

### Counts

`--comments` accepts `N`, `>N`, `>=N`, `<N`, `<=N`, and `START..END`.

### `@me`

`--author`, `--assignee`, `--commenter`, `--mentions`, `--involves`,
`--review-requested`, and `--reviewed-by` accept `@me`.
It resolves to the login gh stores locally for the repository's host
(`gh config get -h <host> user`), so no API call is made.
Because a search can span hosts, it resolves once per host.
When gh has no stored user, the command fails with an error instead of silently
matching nothing; run `gh auth login` or pass the login.

## Sort, order, and limit

```bash
ghx search issues login --sort comments --order desc
ghx search prs crash --sort created --order asc --limit 10
```

`--sort` accepts `updated` (default), `created`, or `comments`.
`--order` accepts `asc` or `desc`.
GitHub's server-side orderings (`best-match`, `reactions`, `interactions`) have
no local equivalent and are rejected.

`--limit` caps how many rows are shown and defaults to 30; `--limit 0` shows all.
When the cap hides results, a log line reports the shown and total counts on
stderr.

## Display

```
octocat/hello-world#42  Fix login bug   open    2026-10-30
octocat/hello-world#41  Update README   closed  2026-10-29
```

Titles are truncated to 60 characters; pass `--no-truncate` to show them in full.
Rows added by `--include-prs` are prefixed with `[PR]`.

## JSON output

`--json` emits a pretty-printed JSON array of the cached records.
Bare `--json` selects every supported field; `--json=field1,field2` selects a
subset (use the `=` form so a following word is still read as a query term):

```bash
ghx search issues login --json
ghx search issues login --json=number,title,repository,isPullRequest
```

Each row carries the issue or pull request fields plus a `repository` object:

```json
[
  {
    "assignees": [],
    "author": { "login": "octocat", "type": "User" },
    "body": "Login fails on Safari",
    "closedAt": null,
    "comments": [],
    "commentsCount": 3,
    "createdAt": "2026-10-01T10:00:00Z",
    "isPullRequest": false,
    "labels": [],
    "milestone": null,
    "number": 42,
    "repository": {
      "host": "github.com",
      "name": "hello-world",
      "nameWithOwner": "octocat/hello-world",
      "owner": "octocat"
    },
    "state": "OPEN",
    "title": "Fix login bug",
    "updatedAt": "2026-10-30T09:00:00Z",
    "url": "https://github.com/octocat/hello-world/issues/42"
  }
]
```

`search issues` supports the fields above; `search prs` additionally supports
`additions`, `baseRefName`, `checks`, `deletions`, `headRefName`, `headRefOid`,
`isDraft`, `mergedAt`, `reviewDecision`, `reviewRequests`, `reviewThreads`,
`reviews`, and `timeline`.
Unknown field names are rejected with the list of valid ones.

### Templates

`--template` renders each result with a Go template over the same data, using
lowercase JSON keys. It runs once per result and appends a newline when the
template does not end with one:

```bash
ghx search issues login --template '{{.repository.nameWithOwner}}#{{.number}} {{.title}}'
```

```
octocat/hello-world#42 Fix login bug
```

## Not supported

These `gh search` flags are not available, because the cache does not hold the
data or the filtering happens on GitHub's side:

| Flags | Why |
|---|---|
| `--archived`, `--visibility`, `--language` | Repository metadata is not cached |
| `--locked` | `isLocked` is not fetched or stored |
| `--reactions`, `--interactions` | Reaction counts are not stored |
| `--project`, `--no-project` | Projects v2 data is not stored |
| `--search-type semantic`, `--search-type hybrid` | Server-side relevance ranking |
| `--jq` | No jq dependency in the binary |
| `--web` | Opens GitHub in a browser rather than answering from the cache |

## Caveats

- The cache stores at most the first 100 comments, 100 reviews, and 100 review
  requests per item, so `--commenter`, `--mentions`, `--team-mentions`,
  `--involves`, `--reviewed-by`, and `--review-requested` see cached pages only.
- Older cache rows may lack `checks`, `reviewRequests`, or `reviewDecision`
  until they are refetched; those rows do not match the corresponding filters.
- `--mentions`, `--team-mentions`, and the mention part of `--involves` scan the
  cached body and comments for `@login` / `@org/team`, which can differ from
  GitHub's mention index.
- `--app` is approximate: the cache records the actor type, and GitHub Apps
  appear as bots, so it matches a bot whose login is `<app>` or `<app>[bot]`.
- `--review none` matches rows whose cached review decision is empty, which
  includes rows where GitHub reports no review requirement.

## Relationship to the list commands

`issue list --search` and `pr list --search` filter one repository, can fall
back to the API when its cache is stale, and match a single substring.
`search issues` and `search prs` cover every cached repository, never call the
API, and treat multiple words as separate AND terms with a much larger filter
set.
