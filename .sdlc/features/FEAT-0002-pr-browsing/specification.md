---
title: "PR Browsing"
status: draft
---

# Specification: PR Browsing

## Overview

PR browsing mirrors the issue browsing architecture with two Cobra subcommands (`pr list`, `pr view`) in `cmd/pr.go`.
It adds PR-specific fields: branch names, draft status, merge/closed timestamps, and review decision.
The filtering and display logic follows the same cache-first, API-fallback pattern.

## Architecture

```
pr list ──► runPRList()
                 │
                 ├─ Cache fresh? ──Yes──► QueryPRs() ──► printPRList()
                 │
                 └─ No ──► NewClient() ──► ListPRs() ──► backfillPRDetails() ──► printPRList()

pr view N ──► runPRView()
                 │
                 ├─ Cache fresh? ──Yes──► LoadPR(N) ──► printPRView()
                 │                            │ (missing) ──► fall through to API
                 │
                 ├─ Individual item within the cache duration? ──Yes──► printPRView()
                 │
                 └─ API ──► GetPR(N) ──► SavePR() ──► printPRView()
```

`backfillPRDetails` writes each listed pull request the repository does not already have cached, re-fetching it with `getPRQuery` because a list payload never carries the detail (`additions`, `deletions`, reviews, timeline) a cache record does.
Read commands never advance the cache freshness window.

## Data Models

### PullRequest

| Field | Type | Constraints | Description |
|---|---|---|---|
| number | int | not null | PR number |
| title | string | not null | PR title |
| state | string | not null | OPEN, CLOSED, or MERGED |
| isDraft | bool | not null | Whether the PR is a draft |
| author | Actor | not null | PR author |
| assignees | []Actor | | Assigned users |
| labels | []Label | | PR labels |
| milestone | *Milestone | nullable | Associated milestone |
| baseRefName | string | not null | Target branch |
| headRefName | string | not null | Source branch |
| createdAt | time.Time | not null | Creation timestamp |
| updatedAt | time.Time | not null | Last update timestamp |
| mergedAt | *time.Time | nullable | Merge timestamp |
| closedAt | *time.Time | nullable | Closure timestamp |
| url | string | not null | GitHub URL |
| body | string | | PR body text |
| commentCount | int | | Total comment count |
| comments | []Comment | | Inline comments |
| reviewDecision | string | | APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED |
| reviews | []PRReview | | Submitted reviews (`author`, `state`, `submittedAt`) |
| timeline | []TimelineEvent | | Draft/ready flips and review-request events |
| reviewRequests | []ReviewRequest | nil = unknown | Current pending review requests (`type`, `login` for people/bots, `name` for teams) |

## API Contracts

### GraphQL: `listPRsQuery`

Direct listing with server-side filters (states, labels, baseRefName, headRefName).
Used when no author/assignee/app/draft/search filters are active.

### GraphQL: `searchPRsQuery`

Used when author, assignee, app, draft, or search filters are provided.
Builds a GitHub search query string.

### GraphQL: `getPRQuery`

Fetches a single PR with up to 100 inline comments.

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Merged as distinct state | `MERGED` is a separate PullRequestState in GraphQL | Allows filtering merged PRs separately from closed |
| Review decision formatting | Human-readable strings in output | `APPROVED` becomes "approved" for tabular display |
| Read-path caching | `list` and `view` write fetched PRs to the cache | Browsing warms the cache without a separate `ghx cache` run |
| Full record for cached items | Re-fetch with `getPRQuery` for every new entry | Cached records are identical to `ghx cache` output |

## Risks and Unknowns

1. `app` filter cannot be verified from cached data
2. Backfilling a large list issues one detail request per uncached entry, bounded to a small worker pool

## Out of Scope

- Creating, updating, or merging PRs
- Review comment threads (only top-level issue-style comments)
- CI/check status on PRs
