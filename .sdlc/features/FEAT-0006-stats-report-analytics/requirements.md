---
title: "Stats Report Analytics"
status: draft
---

# Requirements: Stats Report Analytics

## Overview

Extends the `stats` HTML report with the metric families offered by PR analytics
tools such as pull-request-analytics-action: lead times, contribution, PR size,
reviewer engagement, review-request response times, monthly trends, peak
activity, and notable PR lists.
Requires the cache to collect review, timeline, and change-size data that was
previously not fetched.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| Developer | Understand review bottlenecks and team workload from cached repository history |
| Team lead | Compare author output and reviewer responsiveness without reading every PR |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall cache per-PR additions, deletions, submitted reviews (author, state, submitted time), and timeline events (ready for review, convert to draft, review requested; re-requests arrive as additional review-requested events) |
| FR-02 | Must | The report shall show per-author lead times for merged PRs: median and average time to merge, median time in draft, median time to first review, and median time to approve |
| FR-03 | Must | The report shall show a per-author contribution table: opened, merged, closed counts, merge rate, PRs without review activity, comments received, additions/deletions, and PR size distribution |
| FR-04 | Must | The report shall show a per-reviewer engagement table: comments conducted, reviews submitted, approvals, changes requested, comment-only reviews, median time from opening to review, and median time from review request to review |
| FR-05 | Must | The report shall show notable PR lists: longest awaiting first review, longest time to merge, and most discussed |
| FR-06 | Must | The report shall show a PR size versus merge time table with size buckets xs/s/m/l/xl based on changed lines (xs <100, s <500, m <1000, l <2000, xl >=2000) |
| FR-07 | Must | The report shall render charts (monthly trend, PR size distribution, peak activity by hour) using Chart.js loaded from a CDN |
| FR-08 | Must | The report shall degrade gracefully when Chart.js cannot load or when cached PRs lack the new data, showing a notice suggesting `ghx cache --force` |
| FR-09 | Must | The `stats` command shall accept `--top` to control the number of entries per notable list (default 5) |
| FR-10 | Should | Draft time shall be reconstructed from timeline events, including PRs created as draft (ready-for-review without a preceding convert-to-draft) and later convert-to-draft cycles |
| FR-11 | Should | The simulation generator shall produce the new fields so the mock server exercises the analytics metrics |

## Non-Functional Requirements

| ID | Priority | Requirement |
|---|---|---|
| NFR-01 | Must | Old cache entries remain readable; missing analytics data renders as `-` rather than failing the report |
| NFR-02 | Must | New GraphQL fields use the existing pagination budgets (reviews first 100, timelineItems first 250) |
