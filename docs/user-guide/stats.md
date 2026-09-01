# Stats

The `stats` command generates a self-contained HTML report describing pull
request activity for one or more repositories during a period.

## Usage

```bash
ghx stats [REPO...] [flags]
```

Repositories are positional `[HOST/]OWNER/REPO` arguments. When none are given,
the repository is resolved from `--repo` or the current directory's git remote.

```bash
# Report for the current repository over May 2026
ghx stats --from 2026-05-01 --to 2026-05-31

# Report for two repositories, filtered to specific people
ghx stats acme/widget acme/gadget --from 2026-05-01 --to 2026-05-31 \
    --author alice,carol --reviewer alice,bob --output may-report.html
```

Run `ghx cache` first so the report covers full history; stats serves data from
the local cache and only falls back to the GitHub API (capped at 1000 PRs) when
nothing is cached.

## Flags

| Flag | Description |
| ---- | ----------- |
| `--author`, `-A` | Only count PRs authored by these users (comma-separated) |
| `--reviewer` | Only count comments by these reviewers (comma-separated) |
| `--from` | Period start (`YYYY-MM-DD` or RFC3339) |
| `--to` | Period end, inclusive (`YYYY-MM-DD` or RFC3339) |
| `--state`, `-s` | Filter by PR state: `open`, `closed`, `merged`, `all` (default `all`) |
| `--output`, `-o` | Output HTML file path (default `stats-report.html`) |
| `--include-bots` | Include bot accounts (`*[[bot]]`) as reviewers |

## Report contents

- **Summary cards**: total PRs, merged/open/closed counts, author and reviewer
  counts, and the median/average time to first comment across all PRs.
- **Author × reviewer matrix**: authors as rows, reviewers as columns. Each
  cell shows how many PRs the combination appears in, plus the average and
  median time from PR creation to that reviewer's first comment. Cells are
  shaded by count, and a `(no comments)` column tracks PRs that nobody
  reviewed. Two filter fields above the table narrow the matrix to a subset of
  authors (rows) and reviewers (columns) via case-insensitive substring
  matching; the `(no comments)` and `Total` columns always stay visible, and a
  hint reports how many authors and reviewers are shown.
- **Per-author summary**: PRs authored, merged count, and time to first
  comment received.
- **Per-reviewer summary**: PRs reviewed and time to first comment given.
- **Pull requests in period** (only with `--list-prs`): the full list of
  matching PRs with links. Off by default since the table can be very large
  for active repositories.

Reviewers are derived from PR comments written by someone other than the PR
author; bot accounts are excluded unless `--include-bots` is set.

## Themes

The report ships with light and dark themes. It defaults to the browser's
`prefers-color-scheme`, and the **theme toggle** button in the top-right corner
switches between them. The choice is remembered per browser via
`localStorage`, so re-opening the report keeps the selected theme.
