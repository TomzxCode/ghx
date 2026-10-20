---
title: "Cache Management"
status: draft
---

# Specification: Cache Management

## Overview

The cache command fetches all issues and PRs (with comments) from GitHub via GraphQL, writing each as a separate JSON file.
Cache metadata tracks the last fetch timestamp and configured duration.
Every plain run delta-refreshes from the last cache write; the freshness window governs read commands, not the fetch itself.
`--if-stale` restores the no-op behavior while the cache is fresh, and `--force` discards resume state for a full re-fetch.
Delta fetches use the `since` parameter on issues and client-side timestamp comparison on PRs to only fetch updated items.
A progress bar (`schollz/progressbar/v3`) renders to stderr during each fetch, driven by the GraphQL connection `totalCount`; it is suppressed when stderr is not a terminal.

## Architecture

```
cache command ──► runCache()
                      │
                      ├─ --if-stale & fresh? ──Yes──► Print "still fresh", exit
                      │
                      └─ Otherwise (always, for a plain run)
                           │
                           ├─ Determine delta cutoff (previous cache timestamp)
                           │
                           ├─ FetchAllIssues(owner, repo, since)
                           │     └─ Save each issue to issues/<number>.json
                           │
                           ├─ FetchAllPRs(owner, repo, since)
                           │     └─ Save each PR to prs/<number>.json
                           │
                           └─ SaveCacheInfo(duration)
```

## Data Models

### CacheInfo

| Field | Type | Constraints | Description |
|---|---|---|---|
| cachedAt | time.Time | not null | Timestamp of last full cache |
| duration | int | not null | Freshness duration in minutes |

### Cache file layout

```
~/.cache/gh-cached/
└── <host>/
    └── <owner>/
        └── <repo>/
            ├── .cache_info.json
            ├── issues/
            │   ├── 1.json
            │   ├── 2.json
            │   └── ...
            └── prs/
                ├── 1.json
                ├── 2.json
                └── ...
```

## Sequences

### Delta cache update

```
cache command ──► LoadCacheInfo() ──► get cachedAt timestamp
                                         │
                                         ▼
                  FetchAllIssues(since=cachedAt) ──► save updated issues
                                         │
                                         ▼
                  FetchAllPRs(since=cachedAt) ──► save updated PRs
                                         │
                                         ▼
                  SaveCacheInfo() ──► update timestamp
```

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| One file per item | Individual JSON files | Efficient for single-item lookups without loading all data |
| Delta via `since` param | Issues use GraphQL `since` filter | Server-side filtering reduces data transfer |
| Delta via client-side comparison | PRs check `updatedAt` client-side | No `since` filter available on PR GraphQL connection |
| Progress via `totalCount` | Determinate bar for full fetches and issue deltas; spinner for PR deltas | Issues' `totalCount` is accurate (server-filtered); PR `totalCount` counts all PRs so a spinner is shown during delta scans |
| No cache eviction | Let cache grow | Simplicity; disk usage is typically small |
| Repository key casing | Lower-case host/owner/repo | GitHub names are case-insensitive, so one repository maps to one cache entry |

## Risks and Unknowns

1. PR delta fetch relies on ordering by `updatedAt DESC`; if ordering changes, some updates may be missed
2. No atomic cache replacement; a failed mid-fetch leaves a partially updated cache
3. A cache written before key normalization is collapsed on first open: mixed-case rows and directories are lower-cased, keeping the most recent write when two spellings collide

## Out of Scope

- Cache eviction or TTL-based cleanup
- Compression of cached files
- Shared or distributed cache
