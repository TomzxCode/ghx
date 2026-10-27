---
title: "Issue Browsing"
status: draft
---

# Requirements: Issue Browsing

## Overview

Provides CLI commands to list and view GitHub issues for a given repository.
Supports extensive filtering (state, author, assignee, labels, milestone, mention, app, search) and serves results from local cache when fresh, falling back to the GitHub GraphQL API otherwise.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| CLI user | Quickly browse and inspect issues without hitting API rate limits |
| AI agent | Bulk issue data via cached JSON for analysis workflows |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide an `issue list` command that displays issues in tabular format |
| FR-02 | Must | The system shall provide an `issue view <number>` command that displays a single issue with metadata |
| FR-03 | Must | The system shall filter issues by state (open, closed, all) |
| FR-04 | Must | The system shall filter issues by author, assignee, and labels |
| FR-05 | Must | The system shall filter issues by milestone (number or title) |
| FR-06 | Must | The system shall filter issues by search query (matching title and body) |
| FR-07 | Must | The system shall serve list results from cache when the cache is fresh |
| FR-08 | Must | The system shall serve view results from cache when the item was written within the cache's configured duration (60 minutes by default), even when the cache as a whole is stale |
| FR-09 | Must | The system shall support `--comments` flag on view to display issue comments |
| FR-10 | Must | The system shall support `--json` output for both list and view |
| FR-11 | Should | The system shall support `--refresh` flag on view to force API fetch |
| FR-12 | Should | The system shall filter by mention and app author (API-only, not cached) |
| FR-13 | Should | The system shall support `--no-truncate` to show full issue titles |
| FR-14 | Should | The system shall support `--limit` to cap the number of results |
| FR-15 | Should | The system shall write issues fetched from the API during `list` and `view` to the local cache in the same form `ghx cache` writes (full record, comments included) |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | Cached list queries shall return in under 100ms for up to 1000 issues |
| NFR-02 | Must | Compatibility | The system shall support GitHub Enterprise Server hosts |

## Constraints

- Filtering by `mention` and `app` cannot be done from cache alone; these filters are only applied when serving from the API
- A cache-fresh view serves the item from the full cache; an item missing from it is fetched and cached rather than reported as missing
- A read command never advances the cache freshness window; only `ghx cache` does

## Acceptance Criteria

- [ ] FR-01: `gh-cached issue list` shows issues in tabular format with number, title, labels, comment count, and date
- [ ] FR-02: `gh-cached issue view 42` shows issue metadata and body
- [ ] FR-03: `--state open|closed|all` filters correctly
- [ ] FR-07: With fresh cache, `issue list` reads from disk without API calls
- [ ] FR-09: `--comments` shows inline comments on view
- [ ] FR-10: `--json` outputs valid JSON
- [ ] FR-15: After `issue list` or `issue view` falls back to the API, the fetched issues are in the cache (comments included)

## Open Questions

1. Should cached filtering support `mention` and `app` by storing additional metadata?
