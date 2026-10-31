---
title: "Cache Management"
status: done
---

# Specification: Cache Management

## Overview

The cache command fetches all issues and PRs (with comments) from GitHub via GraphQL, writing each as a separate JSON file.
Cache metadata tracks the last fetch timestamp and configured duration.
Every plain run delta-refreshes from the last cache write; the freshness window governs read commands, not the fetch itself.
`--if-stale` restores the no-op behavior while the cache is fresh, and `--force` discards resume state for a full re-fetch.
Delta fetches use the `since` parameter on issues and client-side timestamp comparison on PRs to only fetch updated items.
The `cache` command renders one progress row per portion to stderr, driven by the GraphQL connection `totalCount` plus the number of items already cached, so a refresh starts from what the cache already holds; it is suppressed when stderr is not a terminal.

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

### Coverage fill

```
cache command ──► LoadSegments(kind) ──► seed legacy cursor as a prefix
                                         │
                                         ▼
                  Uncovered(start, now) ──► gaps
                                         │
                                         ▼
                  Chunk(gaps, adaptive width) ──► parallel segment walks
                                         │
                                         ▼
                  SaveSegments(segment) after each completed walk
                                         │
                                         ▼
                  catch-up tail [now, now'] ──► SaveSegments
                                         │
                                         ▼
                  SaveCacheInfo() ──► timestamps and cursors
```

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| One file per item | Individual JSON files | Efficient for single-item lookups without loading all data |
| Coverage ranges | Half-open `[start, end)` `updatedAt` intervals per type, merged on adjacency | A contiguous cache needs only a union of ranges, not a ledger of runs. Half-open intervals make boundaries unambiguous. |
| Segment start | Each PR segment starts from a synthesized `(start, 0)` keyset cursor | The PR connection has no date filter; the cursor decodes to `(updatedAt, databaseId)` and GitHub accepts a key that matches no row, so a segment can start anywhere without walking history. |
| Parallel segments | `--workers` goroutines pull chunks from a shared queue; the width targets four chunks per worker for issues and two per effective PR slot, clamped to 15 minutes to 7 days | Enough pending chunks keep early finishers busy without creating a request per item. Full PR pages are capped at two in flight because GitHub secondary limits punish wider concurrency. |
| Catch-up tail | After the planned segments, process `[run head, current head]` | An item updated during the run moves above the run head; the tail covers it so a segment cannot be marked covered while a moved item is missing. |
| Progress window | The number of items in the uncovered gaps, from the search index | GitHub exposes no count for a time range on the connections; a search range supplies it. The cached figure uses the repository total. |
| No cache eviction | Let cache grow | Simplicity; disk usage is typically small |
| Repository key casing | Lower-case host/owner/repo | GitHub names are case-insensitive, so one repository maps to one cache entry |

## Risks and Unknowns

1. Segment starts rely on the undocumented cursor payload; a rejected cursor leaves that segment uncovered and the next run retries it
2. No atomic cache replacement; a failed mid-fetch leaves a partially updated cache, but completed segments stay covered
3. A cache written before key normalization is collapsed on first open: mixed-case rows and directories are lower-cased, keeping the most recent write when two spellings collide
4. A bot review or an edited review comment may not bump `updatedAt`, so discussion state can lag until a later PR update or an on-demand refresh

## Out of Scope

- Cache eviction or TTL-based cleanup
- Compression of cached files
- Shared or distributed cache
