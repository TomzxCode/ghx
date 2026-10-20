---
title: "Repository Listing"
status: draft
---

# Specification: Repository Listing

## Overview

The `repo list` command walks the cache storage (`~/.cache/ghx/cache/`) and aggregates per-repository statistics.
It makes no network calls; repository data comes from the cache backend, and hit rates come from the local telemetry database.

## Architecture

```
repo list ──► runRepoList()
                   │
                   ├─ Store.ListCachedRepos()
                   │    │
                   │    ├─ Enumerate cached repositories
                   │    ├─ For each repo: LoadCacheInfo(), count issues/PRs
                   │    └─ Return []CachedRepo
                   │
                   └─ telemetry.CacheHitRates()
                        │
                        └─ Aggregate cache_hit and cache_miss events by
                           repository and retrieval operation
                                          │
                                          ▼
                                   tabular output
```

List and view commands record one `cache_hit` or `cache_miss` telemetry event per retrieval.
The operation label (`issue_list`, `issue_view`, `pr_list`, `pr_view`) identifies the dimension the rate is split by.

## Data Models

### CachedRepo

| Field | Type | Constraints | Description |
|---|---|---|---|
| host | string | not null | GitHub host (e.g. github.com) |
| owner | string | not null | Repository owner |
| repo | string | not null | Repository name |
| info | *CacheInfo | nullable | Cache metadata (nil if no .cache_info.json) |
| issueCount | int | not null | Number of cached issue files |
| prCount | int | not null | Number of cached PRs |

### CacheHitRate

| Field | Type | Constraints | Description |
|---|---|---|---|
| host | string | not null | GitHub host |
| repo | string | not null | `owner/repo`, lower-case |
| operation | string | not null | `issue_list`, `issue_view`, `pr_list`, or `pr_view` |
| hits | int | not null | Retrievals served from the local cache |
| misses | int | not null | Retrievals that fell back to the GitHub API |

A row's hit rate is `hits / (hits + misses)`, rounded to a whole percentage and shown as `P% (N)`, where `N` is the total number of recorded retrievals.
An operation with no row renders as a dash.

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| File-count based statistics | Count cached issue and PR records | Simple, no index needed beyond the store's existing listing query |
| Duration formatting | Custom `formatDuration` helper | Human-readable output (e.g. "2h30m", "3d") |
| Hit-rate source | Local telemetry database | Reuses the existing local-only recorder; no new cache state or network path |
| Hit-rate dimension | Retrieval operation | Separates list from view so a broad list query does not hide per-item view behaviour |
| Forced refresh | Excluded from the hit rate | `--refresh` deliberately bypasses the cache, so counting it as a miss would understate cache effectiveness |
| Missing telemetry | Dash, never an error | `repo list` must work with telemetry disabled or cleared |

## Risks and Unknowns

- The telemetry database can contain events from a different telemetry path than the cache; when no events match the repository, the rates show a dash.

## Out of Scope

- Fetching or refreshing repos from this command
- Displaying per-repo configuration or settings
- Recording hit rates for RPC or web UI retrievals
