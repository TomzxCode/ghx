---
title: "PR Browsing"
status: draft
---

# Requirements: PR Browsing

## Overview

Provides CLI commands to list and view GitHub pull requests for a given repository.
Supports filtering by state (open, closed, merged, all), author, assignee, labels, base/head branch, draft status, and search query.
Serves results from local cache when fresh, falling back to the GitHub GraphQL API.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| CLI user | Browse and inspect PRs quickly with review status visibility |
| AI agent | Cached PR data for code review and analysis workflows |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide a `pr list` command that displays PRs in tabular format with branch info and review decision |
| FR-02 | Must | The system shall provide a `pr view <number>` command with metadata, branch info, and review status |
| FR-03 | Must | The system shall filter PRs by state (open, closed, merged, all) |
| FR-04 | Must | The system shall filter PRs by author and assignee |
| FR-05 | Must | The system shall filter PRs by labels (AND logic) |
| FR-06 | Must | The system shall filter PRs by base and head branch name |
| FR-07 | Must | The system shall filter PRs by draft status |
| FR-08 | Must | The system shall support `--comments`, `--json`, and `--refresh` on view |
| FR-09 | Must | The system shall display review decision (approved, changes requested, review required) |
| FR-10 | Must | The system shall serve results from cache when fresh |
| FR-11 | Should | The system shall filter by search query on title and body |
| FR-12 | Should | The system shall write pull requests fetched from the API during `list` and `view` to the local cache in the same form `ghx cache` writes (full record, reviews and timeline included) |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | Cached list queries shall return in under 100ms for up to 1000 PRs |
| NFR-02 | Must | Compatibility | The system shall support merged state as a distinct filter (not just closed) |

## Constraints

- The `app` filter cannot be evaluated from cached data; it is silently skipped when serving from cache
- Draft PR filtering is only fully reliable when serving from the API
- A cache-fresh view serves the item from the full cache; an item missing from it is fetched and cached rather than reported as missing
- A read command never advances the cache freshness window; only `ghx cache` does

## Acceptance Criteria

- [ ] FR-01: `gh-cached pr list` shows PRs with number, title, branches, review status, and date
- [ ] FR-03: `--state open|closed|merged|all` filters correctly
- [ ] FR-06: `--base main --head feat/login` filters by branch names
- [ ] FR-07: `--draft` shows only draft PRs
- [ ] FR-09: Review decision is displayed in human-readable form
- [ ] FR-10: With fresh cache, `pr list` reads from disk without API calls
- [ ] FR-12: After `pr list` or `pr view` falls back to the API, the fetched PRs are in the cache (reviews and timeline included)

## Open Questions

1. Should the `app` filter be supported from cached data by storing author type information?
