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
| `--top` | Number of pull requests per notable-PRs list (default `5`) |
| `--list-prs` | Include the table of individual pull requests (can be large) |

## Report contents

- **Summary cards**: total PRs, merged/open/closed counts, author and reviewer
  counts, median/average time to first comment, median time to merge, and the
  total number of submitted reviews.
- **Monthly trend chart**: PRs opened and merged per month plus the median
  time from opening to merge (Chart.js, loaded from the jsDelivr CDN).
- **Author × reviewer matrix**: authors as rows, reviewers as columns. Each
  cell shows how many PRs the combination appears in, plus the average and
  median time from PR creation to that reviewer's first comment. Cells are
  shaded by count, and a `(no comments)` column tracks PRs that nobody
  reviewed. Two dropdown filters above the table (Tom Select, loaded from the
  jsDelivr CDN) narrow the matrix to a subset of authors (rows) and reviewers
  (columns) with type-ahead autocompletion and multi-select; the
  `(no comments)` and `Total` columns always stay visible, and a hint reports
  how many authors and reviewers are shown.
- **Per-author summary**: PRs authored, merged count, and time to first
  comment received.
- **Lead time** (merged PRs only): per author, the median and average time
  from opening to merge, plus median time in draft, median time to first
  review, and median time to approve.
- **Contribution**: per author, opened/merged/closed counts, merge rate, PRs
  without any review activity, comments received, total additions/deletions,
  and the distribution of PR sizes (xs/s/m/l/xl by changed lines: xs <100,
  s <500, m <1000, l <2000, xl >=2000).
- **Per-reviewer summary**: PRs reviewed and time to first comment given.
- **Reviewer engagement**: per reviewer, comment and review counts, approvals,
  changes requested, comment-only reviews, median time from PR opening to the
  reviewer's first review, and median time from a review request (and
  re-request) to that reviewer's review.
- **PR size vs merge time**: per size bucket, how many PRs were opened and
  merged and the median time to merge, plus a bar chart of the size
  distribution.
- **Peak activity**: a bar chart of when PRs were opened by hour of day.
- **Notable pull requests**: the longest PRs awaiting a first review, the
  longest PRs to merge, and the most discussed PRs (`--top` entries each).
- **Pull requests in period** (only with `--list-prs`): the full list of
  matching PRs with links. Off by default since the table can be very large
  for active repositories.

Reviewers are derived from PR comments and reviews written by someone other
than the PR author; bot accounts are excluded unless `--include-bots` is set.

## Cache data requirements

The lead time, contribution, engagement, and size sections rely on review,
timeline, and change-size data that older cache entries do not contain.
When the report detects PRs cached before this data was collected it shows a
notice; run `ghx cache --force` for the affected repositories to collect the
full data set. Sections still render with a `-` for PRs where the underlying
events were never recorded (for example PRs that never received a formal
review).

## Themes

The report ships with light and dark themes. It defaults to the browser's
`prefers-color-scheme`, and the **theme toggle** button in the top-right corner
switches between them. The choice is remembered per browser via
`localStorage`, so re-opening the report keeps the selected theme.
