---
title: "Repository Listing"
status: done
---

# Test Plan: Repository Listing

## Scope

Covers `repo list` output rendering, the hit-rate computation, the telemetry fallback, and the underlying `ListCachedRepos` store operation on both backends.
Out of scope: the RPC and web UI repo listings, `repo delete` semantics beyond its use of `ListCachedRepos`, and the read commands that record retrieval outcomes.

## Unit Tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | `runRepoList` renders the hit-rate columns and rows | One cached repo, telemetry events for 4 operations | Header `ISSUE-LIST ISSUE-VIEW PR-LIST PR-VIEW`; `67% (3)` for issue list, `100% (1)` for issue view, `0% (1)` for PR list, dash for PR view |
| TC-2 | `recordRetrievalOutcome` writes one hit or miss event per retrieval operation | One hit and one miss for a repo | `CacheHitRates` returns 1 hit / 0 misses for issue list and 0 hits / 1 miss for PR view |
| TC-3 | SQLite `ListCachedRepos` counts items and joins metadata in one query | Repo A with 2 issues, 1 PR, cache metadata; repo B with 1 issue and no metadata | Both repos listed; A has counts 2/1 and non-nil `Info`; B has counts 1/0 and nil `Info` (union of item sources) |
| TC-4 | `repo delete` empties the listing and is idempotent | One cached repo, delete it, delete again | Deletion confirmation then "No cached data found"; `ListCachedRepos` returns 0 repos |
| TC-5 | Listing reports canonical lower-case coordinates after normalization | Issues and PRs saved under mixed-case keys, both backends | `ListCachedRepos` returns one repo with lower-case host, owner, and repo |
| TC-6 | Listing works on a legacy database missing freshness columns | v1 schema without `issues_cached_at` / `prs_cached_at` | Open migrates the schema; `ListCachedRepos` succeeds |

## Integration Tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| (none) | No CLI integration test exercises `repo list` end to end; `internal/integration` covers issue and PR commands only | | |

## End-to-End Tests

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| (none) | No end-to-end test covers `repo list` against the mock server | | |

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior | Tested |
|---|---|---|---|
| TC-7 | Repository has cached items but no cache metadata | Listed with age dash and status "no cache info" (read-through writes, no `cache_meta` row) | Yes, TC-3 |
| TC-8 | Telemetry disabled, database missing, or unreadable | Listing succeeds; every hit-rate column shows a dash | Yes, TC-1 |
| TC-9 | Operation has recorded events but zero total after filtering | Dash, never a division error | Yes, TC-1 (dash branch in `hitRateLabel`) |
| TC-10 | Empty cache | "No cached repositories found." | No, untested |
| TC-11 | Mixed-case repository keys collapse to one listing entry | One lower-case row per repository | Yes, TC-5 |

## Test Infrastructure

- `cmd/repo_test.go` drives `runRepoList` and `runRepoDelete` with a cobra command writing to a buffer, `withCleanStorageFlags`, and `resetTelemetryState`; telemetry points at a `t.TempDir()` database.
- `internal/cache` tests use `t.TempDir()` stores for both backends and helpers named per conventions (`richIssue`, `richPR`).
- Hit rates are seeded directly with `telemetry.Open` + `InsertBatch`, so no GitHub API or mock server is involved.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-1, TC-3, TC-4 |
| FR-02 | TC-1 (columns rendered), TC-3 (counts and metadata); age and fresh/stale strings not asserted directly |
| FR-03 | None directly; `repoLabel` host-prefix formatting is untested |
| FR-04 | TC-1, TC-2 |
| FR-05 | TC-1, TC-2 |
| FR-06 | TC-1, TC-8 |
| FR-07 | TC-1 |
| NFR-01 | None; no benchmark or timing test exists |
| NFR-02 | TC-1, TC-8 |

## Gaps

- No test asserts the `OWNER/REPO` versus `HOST/OWNER/REPO` label (FR-03, `cmd/repo.go:53`).
- No test asserts the cache age and status strings (`formatDuration`, fresh/stale, "no cache info").
- No test covers the empty-cache message (TC-10) or the NFR-01 performance target.
