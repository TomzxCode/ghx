---
title: "Repository Listing"
status: done
---

# Requirements: Repository Listing

## Overview

Provides a `repo list` command that scans the local cache directory and displays all repositories that have been previously cached, along with their issue/PR counts, cache age, freshness status, and a cache hit rate per retrieval operation.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| CLI user | See which repositories are cached and whether they are stale |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide a `repo list` command that displays all locally cached repositories |
| FR-02 | Must | For each repository, the system shall display issue count, PR count, cache age, and freshness status |
| FR-03 | Must | The system shall display the repository as `OWNER/REPO` for github.com hosts and `HOST/OWNER/REPO` for others |
| FR-04 | Must | For each repository, the system shall display a cache hit rate for each retrieval operation: issue list, issue view, PR list, and PR view |
| FR-05 | Must | A hit rate shall be the number of retrievals served from the local cache divided by all recorded retrievals of that operation for the repository |
| FR-06 | Must | A hit rate shall be displayed as a dash when the repository has no recorded retrievals for that operation, including when telemetry is disabled or has no data |
| FR-07 | Must | The system shall display the total number of recorded retrievals beside each hit rate |
| FR-08 | Must | The system shall provide a `repo list --json [FIELDS]` form that emits the repositories as a JSON array, accepting a comma-separated field subset and rejecting unknown fields |
| FR-09 | Must | Each JSON repository object shall carry its cache identity, issue and PR counts, freshness status, and a structured hit rate for each retrieval operation that has recorded retrievals |
| FR-10 | Should | A JSON listing of an empty cache shall emit an empty array |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | Listing shall complete in under 1 second for up to 50 cached repos |
| NFR-02 | Must | Reliability | An unavailable, disabled, or unreadable telemetry database shall not fail the listing; hit-rate columns fall back to a dash |

## Constraints

- Only shows repositories with data on disk; no network calls
- Cache age formatting rounds to minutes
- Hit rates are derived from local telemetry events recorded by list and view commands
- JSON output mirrors the table's data; it adds no network calls
- JSON hit rates are structured rather than pre-formatted strings

## Acceptance Criteria

- [ ] FR-01: `gh-cached repo list` shows all cached repos in tabular format
- [ ] FR-02: Each row shows issue count, PR count, age, and fresh/stale status
- [ ] FR-03: GitHub.com repos omit the host prefix; GHE repos include it
- [ ] FR-04: Each row shows a hit rate for issue list, issue view, PR list, and PR view
- [ ] FR-05: A repository with 2 hits and 1 miss for issue list reports 67%
- [ ] FR-06: An operation with no recorded retrievals shows a dash
- [ ] FR-07: A row with 2 hits and 1 miss shows `67% (3)`
- [ ] FR-08: `repo list --json nameWithOwner,issueCount` emits one object per repository with exactly those keys; a bare `--json` emits every field; an unknown field fails with the valid names listed
- [ ] FR-09: An issue list hit rate of 2 hits and 1 miss appears as `{"hits":2,"misses":1,"total":3,"percent":67}`
- [ ] FR-10: An empty cache emits `[]` in JSON mode
- [ ] NFR-02: Listing succeeds with a dash in every hit-rate column when no telemetry database exists

## Open Questions

(none)
