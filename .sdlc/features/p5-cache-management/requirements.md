---
title: "Cache Management"
status: done
---

# Requirements: Cache Management

## Overview

Provides the `cache` command to pre-populate the local disk cache with all issues and PRs for a repository, including their comments.
Supports incremental (delta) updates, configurable freshness duration, and forced full re-fetches.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| CLI user | Warm the cache once, then browse offline or with minimal API usage |
| AI agent | Bulk-cache data for downstream analysis without repeated API calls |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide a `cache` command that fetches all issues and PRs with comments |
| FR-02 | Must | The system shall store each issue and PR as an individual JSON file under `~/.cache/gh-cached/<host>/<owner>/<repo>/` |
| FR-03 | Must | The system shall write a `.cache_info.json` metadata file tracking cache timestamp and duration |
| FR-04 | Must | The system shall support `--cache-duration` to configure freshness in minutes (default 60) |
| FR-05 | Must | The system shall support `--force` to re-fetch even when cache is fresh |
| FR-06 | Must | The system shall perform delta fetches when cache exists and `--force` is not used |
| FR-07 | Should | The system shall report progress during caching (items fetched, cache status) |
| FR-08 | Should | The system shall key the cache on lower-case host/owner/repo, so a repository has one entry regardless of the casing it is named with |
| FR-09 | Should | The system shall support `--if-stale` to skip a run when the cache is complete and still fresh |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | Full cache of 1000+ items shall complete within GitHub API rate limits |
| NFR-02 | Must | Reliability | Partial cache writes shall not corrupt existing cache files |

## Constraints

- Cache directory is fixed at `~/.cache/gh-cached/`
- No cache eviction or cleanup mechanism; cache grows indefinitely
- No compression of cached JSON files

## Acceptance Criteria

- [ ] FR-01: `gh-cached cache --repo owner/repo` fetches and saves all issues and PRs
- [ ] FR-04: `--cache-duration 120` sets freshness to 2 hours
- [ ] FR-05: `--force` triggers a full re-fetch regardless of freshness
- [ ] FR-06: Second `cache` run without `--force` performs delta fetch
- [ ] FR-09: `--if-stale` on a complete, fresh cache performs no requests

## Open Questions

1. Should the cache support automatic eviction of stale entries?
2. Should cache files be compressed to save disk space?
