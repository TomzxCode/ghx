---
title: "Issue Browsing"
status: done
---

# Test plan: Issue Browsing

## Scope

Covered: the `issue list` and `issue view` commands (`cmd/issue.go`), the shared filter predicates (`internal/cache/filter.go`), the per-item freshness window (`internal/cache/store.go`), the GraphQL issue queries and search-query construction (`internal/github/api.go`), and the cache backfill performed by read commands (`cmd/readcache.go`).
Out of scope: pull request browsing, the `cache` command, the web UI, and the JSON-RPC server; they have their own features, and the RPC tests that re-exercise issue list/view are listed as related coverage.

## Unit Tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | `filterIssues` state filter (open, closed, all) | Three fixture issues, `IssueQuery.State` open/closed/all | 2, 1, and 3 matches (`TestFilterIssues_State`, `internal/cache/filter_test.go`) |
| TC-2 | `filterIssues` author filter | `Author: "alice"` | Only alice's issues (`TestFilterIssues_Author`) |
| TC-3 | `filterIssues` assignee filter | `Assignee: "bob"` | Only the assigned issue (`TestFilterIssues_Assignee`) |
| TC-4 | `filterIssues` label filter with AND logic | `Labels: ["bug"]` then `["bug", "p1"]` | Subset then both-labels-only matches (`TestFilterIssues_Labels`) |
| TC-5 | `filterIssues` milestone by title and by number | `Milestone: "v1.0"` and `"1"` | Same issue matched both ways (`TestFilterIssues_Milestone`) |
| TC-6 | `filterIssues` search, case-insensitive over title and body | `Search: "memory"` | Matches titles differing in case (`TestFilterIssues_Search`) |
| TC-7 | `issueStates` mapping | open, closed, all, empty | `OPEN`, `CLOSED`, `OPEN,CLOSED`, `OPEN,CLOSED` (`TestIssueStates`, `internal/github/api_test.go`) |
| TC-8 | `buildIssueSearchQuery` covers state, author, assignee, labels, milestone, mention, app, and raw search | One `IssueListOptions` case per filter | Query string contains each qualifier; state=all adds none (`TestBuildIssueSearchQuery`) |
| TC-9 | Issue GraphQL queries request actor `__typename` | `listIssuesQuery`, `getIssueQuery`, `fetchAllIssuesQuery`, `searchIssuesQuery` | Every author selection includes `__typename` (`TestQueriesRequestActorTypename`) |
| TC-10 | `IsItemFresh` window on both backends | Item age vs default 60m, configured 1440m, and zero duration; with and without cache metadata | Hits inside the window, misses outside; zero duration never serves; no metadata falls back to the default (`TestIsItemFresh`, `internal/cache/item_freshness_test.go`) |

## Integration Tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-11 | `issue list` backfills the cache with full records, comments included | Mock server, empty cache | Both listed issues cached; the commented issue carries its 2 comments (`TestIssueList_CachesFetchedIssues`, `cmd/readcache_test.go`) |
| TC-12 | List never overwrites an existing cache entry | Issue #1 pre-cached with a sentinel title | Sentinel title survives the list (`TestIssueList_KeepsExistingCache`) |
| TC-13 | List emits debug timings for the API and backfill stages | Debug logging enabled | Log contains "fetched from API", backfill scan/done, and durations (`TestIssueList_EmitsDebugTimings`) |
| TC-14 | View fetches an item missing from a fresh cache and caches it | Fresh cache metadata, no cached items | View succeeds and the issue lands in the cache (`TestIssueView_FreshCacheMissFetches`) |
| TC-15 | View on a number cached as a pull request errors without an API call | PR cached, server closed | Error names the type and points at `ghx pr view` (`TestIssueView_CachedPRReportsOtherType`) |
| TC-16 | View serves an item within the configured duration even when the full cache is stale | Item 12h old, cache metadata 5 days old with 1440m duration, server closed | Cached title is printed with no API access (`TestIssueView_ServesItemWithinConfiguredDuration`) |
| TC-17 | View refetches an item past the configured duration | Item 25h old, 1440m duration | Stale title absent, fresh API title printed (`TestIssueView_RefetchesItemPastConfiguredDuration`) |
| TC-18 | SQLite issue query matches the file backend across 15 query combinations | Same fixture seeded in both backends | Identical result sets for every `IssueQuery` (`TestQueryEquivalence`, `internal/cache/query_equiv_test.go`) |
| TC-19 | SQLite issue query plan uses an index | Seeded SQLite store | Query avoids a full table scan (`TestSQLiteQueryUsesIndex`) |
| TC-20 | Related: issue list and view over the JSON-RPC surface re-exercise the same behavior | Mock server | `TestIssueListAndViewFromAPI`, `TestIssueListFromCache`, and `TestIssueViewCachedPRReportsOtherType` in `internal/rpc/methods_test.go` |

## End-to-End Tests

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-21 | Cold list serves open issues from the API | Run the built binary `issue list` against the mock server | Table output lists open issues only; at least one API request counted (`TestCLI_IssueList_MockServer`, `internal/integration/cli_test.go`) |
| TC-22 | Warm list serves identical rows with zero API calls | Run `ghx cache`, then `issue list` again | Row sets match; request counter unchanged (same test) |
| TC-23 | State filter via `-s closed` | Closed-state list | Only the closed issue appears (same test) |
| TC-24 | List `--json` output | `issue list --json` | Parses as a JSON array; every item has number, title, and state (same test) |
| TC-25 | View shows item metadata from cache | Warm cache, then `issue view 1` | Title, state, and URL printed; zero API requests (`TestCLI_View_MockServer`) |
| TC-26 | View `-c` shows comments | `issue view 1 -c` | Comment authors and bodies printed (same test) |
| TC-27 | View on a pull request number guides to `pr view` | `issue view 6` where #6 is a cached PR | Non-zero exit; stderr suggests `pr view 6` (same test) |
| TC-28 | View on a missing issue fails cleanly | `issue view 999` | Non-zero exit with an error on stderr (same test) |

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-P1 | Item missing from an otherwise fresh full cache | Fetched and cached, not reported as missing (TC-14) |
| TC-P2 | Number cached as a pull request | Type error pointing at `ghx pr view`, no API call (TC-15, TC-27) |
| TC-P3 | Item inside vs outside the configured per-item window | Served from cache vs refetched (TC-16, TC-17) |
| TC-P4 | Repository with no cache metadata | Default 60-minute item window applies (TC-10) |
| TC-P5 | Zero cache duration | No item is ever served from cache (TC-10) |
| TC-P6 | Milestone filter by title (case-insensitive) or number | Both match the same milestone (TC-5) |
| TC-P7 | Multiple labels required at once | AND semantics, not OR (TC-4) |
| TC-P8 | Case-insensitive search hit | Lowercase query matches capitalized title (TC-6) |
| TC-P9 | state=all in a search query | No `is:open`/`is:closed` qualifier injected (TC-8) |

## Test Infrastructure

- `internal/mockserver` provides the fake GitHub GraphQL server and the scenario builder (`AddIssue`, `WithIssueComment`); its own behavior is verified in `internal/mockserver/server_test.go` (`TestGetIssue`, `TestListIssues`, `TestListIssues_FilterState`, `TestSearchIssues`, `TestFetchAllIssues`).
- `cmd` tests redirect the command globals to a temp cache directory and the mock server with `withReadFlags`, forcing the file backend; `internal/integration` builds the binary with the SQLite backend and counts GraphQL requests through an `httptest` reverse proxy.
- Run everything with `go test -race -cover ./...`.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-11, TC-21 |
| FR-02 | TC-25 |
| FR-03 | TC-1, TC-7, TC-18, TC-23 |
| FR-04 | TC-2, TC-3, TC-4, TC-18 |
| FR-05 | TC-5, TC-18 |
| FR-06 | TC-6, TC-8, TC-18 |
| FR-07 | TC-22 |
| FR-08 | TC-10, TC-16, TC-17 |
| FR-09 | TC-26 |
| FR-10 | TC-24 (list); view `--json` is covered only at the RPC surface (TC-20) |
| FR-11 | None directly; TC-17 covers the refetch path `--refresh` forces |
| FR-12 | TC-8 (search-query construction only); no test asserts the cached path ignores mention/app |
| FR-13 | None |
| FR-14 | None |
| FR-15 | TC-11, TC-12, TC-14 |
| NFR-01 | None (no performance assertion) |
| NFR-02 | None (the mock server always uses the github.com host shape) |

Coverage gaps: `--refresh` (FR-11), `--no-truncate` (FR-13), and `--limit` (FR-14) have no direct tests, view `--json` (FR-10) has no CLI-level test, the cache-path exclusion of mention/app (FR-12) is untested, and no test measures the NFR-01 latency budget.
