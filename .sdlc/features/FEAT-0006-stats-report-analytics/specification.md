---
title: "Stats Report Analytics"
status: draft
---

# Specification: Stats Report Analytics

## Overview

The analytics pipeline has three layers: GraphQL collection (internal/github),
mock parity (internal/mockserver), and report aggregation and rendering
(cmd/stats_report.go).
The HTML report adds a Chart.js dependency loaded from the jsDelivr CDN, next
to the existing Tom Select dependency.

## Data collection

`PullRequest` gains four fields persisted in the cache JSON: `additions`,
`deletions`, `reviews`, and `timeline`.

| Field | Type | Source |
|---|---|---|
| `additions`, `deletions` | int | scalar PR fields |
| `reviews` | `[]PRReview{author, state, submittedAt}` | `reviews(first: 100)` |
| `timeline` | `[]TimelineEvent{kind, actor, requestedReviewer, createdAt}` | `timelineItems(first: 250, itemTypes: ...)` |

Timeline events are normalized to three kinds: `ready_for_review`,
`converted_to_draft`, and `review_requested`.
`PullRequestTimelineItems` and `RequestedReviewer` are GraphQL unions, so the
query selects member fields through inline fragments (`... on User { login }`).
GitHub has no review re-request event type; a re-request is recorded as an
additional `ReviewRequestedEvent`, and the stats command classifies request
events that occur after a reviewer's first review on the PR as re-requests.
Only the cache-facing queries fetch the new fields (`fetchAllPRsQuery`,
`searchPRsForCacheQuery`, `getPRQuery`); `listPRsQuery` stays lightweight.
The mock server mirrors the connection shapes with `reviewNodes` and
`timelineNodes` serializers.

## Metric definitions

| Metric | Definition |
|---|---|
| Time to merge | `createdAt` to `mergedAt` for merged PRs |
| Time in draft | Sum of draft intervals reconstructed from timeline events; a ready-for-review event with no preceding convert-to-draft implies the PR was created as draft, and a still-draft PR counts to `updatedAt` |
| Time to first review | `createdAt` to the earliest submitted review (APPROVED, CHANGES_REQUESTED, or COMMENTED) |
| Time to approve | `createdAt` to the earliest APPROVED review |
| PRs w/o review | PRs with no reviewer comments and no submitted reviews |
| Size bucket | Changed lines (`additions + deletions`): xs <100, s <500, m <1000, l <2000, xl >=2000 |
| Open to review | PR creation to a reviewer's first review, median per reviewer |
| Request to review | First `ReviewRequestedEvent` time to that reviewer's first review at or after the request, median per reviewer; re-requests (later request events after a review) measured identically to the reviewer's next review |

Reviewers are filtered by the same rules as comments: the PR author is never a
reviewer, bots are excluded unless `--include-bots` is set, and an explicit
`--reviewer` filter restricts the set.

## Report structure

New sections, in render order: summary card additions (median time to merge,
reviews submitted), monthly trend chart, lead time table, contribution table,
reviewer engagement table, PR size versus merge time table with size chart,
peak activity chart, notable PR lists, and a legacy-cache notice.
Sections render only when their data exists; unavailable values render as `-`.

Charts share one JSON payload (`chartsPayload`) serialized into a
`var DATA` script variable and rendered by Chart.js; charts are rebuilt on
theme changes and replaced with a note when the CDN is unreachable.

A PR with no reviews, no timeline, and zero changed lines is treated as
possibly pre-analytics, but the `ghx cache --force` notice only appears when
more than a fifth of the period's PRs lack data, since genuinely empty diffs
occur in real repositories.

## Mock server

The simulation generates additions/deletions per PR, a review request event
followed by a review by the requested reviewer, re-request events after
changes-requested reviews, and ready-for-review events for most draft PRs.
Merge, close, and issue-close timestamps capture the event time by value to
avoid loop-variable aliasing.
