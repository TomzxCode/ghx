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
                          tabular output ◄──► JSON output (--json)
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

### RepoJSONRow

The `--json` form emits one object per repository, projected from a row type that carries the repository, its hit rates, and one `now` shared by every row so ages and freshness agree.

| Field | Type | Constraints | Description |
|---|---|---|---|
| nameWithOwner | string | not null | `OWNER/REPO`, or `HOST/OWNER/REPO` when the host is not github.com |
| host | string | not null | GitHub host |
| owner | string | not null | Repository owner |
| name | string | not null | Repository name |
| issueCount | int | not null | Number of cached issues |
| prCount | int | not null | Number of cached PRs |
| status | string | not null | `fresh`, `stale`, or `no cache info` |
| cachedAt | timestamp | nullable | Cache write time as RFC3339 (with fractional seconds); null without cache metadata |
| cacheAge | string | nullable | Human-readable age, as in the table; null without cache metadata |
| cacheDurationMinutes | int | nullable | Configured cache duration; null without cache metadata |
| hitRates | object | not null | Operation → `RepoHitRate`; operations with no retrievals are omitted |

### RepoHitRate

| Field | Type | Constraints | Description |
|---|---|---|---|
| hits | int | not null | Retrievals served from the local cache |
| misses | int | not null | Retrievals that fell back to the GitHub API |
| total | int | not null | `hits + misses` |
| percent | int | not null | `hits / total` rounded to a whole percentage, the same value the table prints |

The operation keys are `issueList`, `issueView`, `prList`, and `prView`.
An unknown requested field is an error that lists the supported names.
A bare `--json` selects every field.

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| File-count based statistics | Count cached issue and PR records | Simple, no index needed beyond the store's existing listing query |
| Duration formatting | Custom `formatDuration` helper | Human-readable output (e.g. "2h30m", "3d") |
| Hit-rate source | Local telemetry database | Reuses the existing local-only recorder; no new cache state or network path |
| Hit-rate dimension | Retrieval operation | Separates list from view so a broad list query does not hide per-item view behaviour |
| Forced refresh | Excluded from the hit rate | `--refresh` deliberately bypasses the cache, so counting it as a miss would understate cache effectiveness |
| Missing telemetry | Dash, never an error | `repo list` must work with telemetry disabled or cleared |
| JSON field selection | Same field-list mechanism as `pr list` | One resolution path keeps the two commands consistent and validates typos once |
| Missing cache metadata in JSON | Null fields plus `no cache info` status | A dash is a table rendering choice, not data; null is the JSON equivalent |
| JSON hit rates | Structured object per operation | Scripts get the numbers instead of parsing the table's `P% (N)` string |
| JSON vocabulary | gh-style `name`/`nameWithOwner`, tri-state `status` | `gh repo list --json` users already know these names, and a repository without cache metadata has no bool equivalent: the RPC `repo.list` shape (`repo`/`label`/`durationMinutes`/`fresh` plus `complete`) is a separate contract and keeps its own names |

## Risks and Unknowns

- The telemetry database can contain events from a different telemetry path than the cache; when no events match the repository, the rates show a dash.

## Out of Scope

- Fetching or refreshing repos from this command
- Displaying per-repo configuration or settings
- Recording hit rates for RPC or web UI retrievals
