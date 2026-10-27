---
title: "PR Browsing"
status: done
---

# Test Plan: PR Browsing

## Scope

Covers `pr list` filtering (state, author, assignee, labels, base, head, draft, search), the gh-style `--json` field selection, `pr view` cache interaction (full-cache hit, item freshness window, cache-miss backfill, cross-type report, `--refresh`), and the cache backfill that mirrors `ghx cache` records.
Out of scope: the cache backends themselves (FEAT-1), `ghx cache` fetch semantics (FEAT-p5), and the mutation commands (`pr comment`, `pr review`, `pr threads`).

## Unit Tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | `prStates` maps every list state | open, closed, merged, all, "" | OPEN, CLOSED, MERGED, or the full triple (internal/github/api_test.go:23) |
| TC-2 | `buildPRSearchQuery` builds the search qualifiers | state/author/base, merged/draft, head/app, all | `is:pr`, `is:open`, `author:alice`, `base:main`, `is:merged`, `draft:true`, `head:feature-x`, `author:app/ci-bot`; `all` adds no state qualifier (internal/github/api_test.go:79) |
| TC-3 | `resolvePRJSONFields` validates the field list | `number,headRefOid,mergedAt`, duplicates, bare sentinel, `number,bogus` | Field list preserved, duplicates collapse, sentinel selects every field, unknown field errors with the available list (cmd/pr_json_test.go:47) |
| TC-4 | `resolvePRListJSONFields` handles the three `--json` forms | unset, bare `--json`, space form, equals form | nil when unset, all fields when bare, field list taken from args or flag value (cmd/pr_json_test.go:82) |
| TC-5 | `nodeToPR` preserves actor `__typename` | PR node with Bot author, comments, reviews, and a ReviewRequestedEvent | Author and nested actors keep their type; timeline keeps the requested reviewer (internal/github/api_test.go:114) |
| TC-6 | `nodeToPR` carries review requests | User and Team reviewers, missing connection, empty connection | Login for people and bots, Name for teams, nil preserved as nil, empty stays non-nil (internal/github/api_test.go:156) |
| TC-7 | Query selections request actor `__typename` and review requests | listPRsQuery, getPRQuery, searchPRsQuery, fetchAllPRsQuery, prTimelineSelection | Every query contains `author { __typename login }`; getPR and fetchAllPRs select `reviewRequests(first: 100)` with the Team fragment (internal/github/api_test.go:187) |
| TC-8 | `filterPRs` state, author, draft, base, head, and search predicates | Cached PR fixtures | Each predicate keeps only matching PRs, case-insensitively (internal/cache/filter_test.go:155-205) |

## Integration Tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-N1 | `pr list` served from the API backfills the cache | Mock server, empty temp cache | The listed PR is cached with comments, matching a `ghx cache` record (cmd/readcache_test.go:124) |
| TC-N2 | `pr list --json` emits only the selected fields | Mock server, `--json` before and after the field list | Exactly `number`, `headRefOid`, `mergedAt` per PR, including through the real cobra command (cmd/pr_json_test.go:122,165) |
| TC-N3 | `pr list --json` with no matches prints an empty array | Filter matching nothing | Output is `[]` (cmd/pr_json_test.go:200) |
| TC-N4 | `pr view` serves an item within the configured window | Stale full cache, item written 12h ago, 24h duration | Cached item is served with zero API calls (cmd/readcache_test.go:318) |
| TC-N5 | `pr view` fetches an item missing from a fresh full cache | Fresh cache metadata, no cached item | PR is fetched and cached instead of erroring (cmd/readcache_test.go:388) |
| TC-N6 | `pr view` on a number cached as an issue reports the type | Cached issue #1, server closed | Errors with "is an issue, not a pull request" and never queries the API (cmd/readcache_test.go:248) |
| TC-N7 | `QueryPRs` matches `filterPRs` on both backends | Same fixtures through file and SQLite stores | Identical result sets for every PRQuery; SQLite uses the index (internal/cache/query_equiv_test.go:96,225) |
| TC-N8 | Per-type and per-item freshness | Issues-only or PRs-only cache runs, aged items | `IsPRsCacheFresh` is judged independently of issues; `IsItemFresh` uses the repo duration or the 60-minute default (internal/cache/per_type_freshness_test.go:11, item_freshness_test.go:12) |

## End-to-End Tests

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-M1 | `pr list` CLI behavior | Run the built binary against the mock server: default list, `-s merged`, `--json` | Open list excludes the merged PR, merged filter shows it, `--json` parses with the expected items (internal/integration/cli_test.go:185) |
| TC-M2 | `pr view` and cross-type guidance | Warm the cache, then `pr view 7` and `issue view 6` | `pr view 7` prints from cache with zero API calls; `issue view 6` fails and points to `pr view 6` (internal/integration/cli_test.go:234) |
| TC-M3 | `pr list --json` against live GitHub | Skipped without credentials; run `pr list --json` and parse the output | Valid JSON array (internal/integration/cli_live_test.go:124) |

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-P1 | Unknown `--json` field | Error names the field and lists the available ones (cmd/pr_json.go:73) |
| TC-P2 | `--json` with an empty field list | Error "no JSON fields specified" (cmd/pr_json.go:82) |
| TC-P3 | Item older than the configured window | `pr view` refetches and replaces the stale copy, as verified on the issue counterpart (cmd/readcache_test.go:352) |
| TC-P4 | Backfill fetch or save failure for one PR | Warning is logged and the listing still succeeds (cmd/readcache.go:95-102) |
| TC-P5 | A PR number that is not found | GetPR returns "pull request #N not found" (internal/github/api.go:1047) |

## Test Infrastructure

- `internal/mockserver` scenario builder (`NewScenarioBuilder`, `AddPR`, `WithPRState`, `WithPRReview`, `WithPRComment`) provides deterministic PR fixtures (cmd/pr_json_test.go:12, cmd/readcache_test.go:18).
- `withPRListFlags` and `withReadFlags` isolate command globals and point the CLI at a mock server and `t.TempDir()` cache (cmd/pr_json_test.go:24, cmd/readcache_test.go:35).
- `setItemMtime` ages file-backend records so item freshness is tested without sleeping (cmd/readcache_test.go:271).
- End-to-end tests build the binary and drive it via `runCLI` (internal/integration/cli_test.go).

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-M1 (tabular output assertions) |
| FR-02 | TC-M2, TC-N4, TC-N5 |
| FR-03 | TC-1, TC-8, TC-M1 |
| FR-04 | TC-2, TC-8 (assignee shares the issue-tested predicate, internal/cache/filter_test.go:69) |
| FR-05 | TC-8 shares `hasAllLabels` with the issue-tested path (internal/cache/filter_test.go:77); the PR search label qualifier itself is untested |
| FR-06 | TC-2, TC-8 |
| FR-07 | TC-2, TC-8 |
| FR-08 | TC-M2 (`--comments`, cache hit), TC-N1 (`--refresh` path exercised as the API fallback); no test sets `pr view --refresh` explicitly |
| FR-09 | Not directly asserted: `formatReviewDecision` (cmd/pr.go:242) has no unit test and no list or view assertion checks the decision text |
| FR-10 | TC-N4, TC-N7, TC-N8, TC-M2 (zero API calls on a warm cache) |
| FR-11 | TC-2, TC-8 |
| FR-12 | TC-N1, TC-N5 |
| NFR-01 | Not covered by an automated benchmark |
| NFR-02 | TC-1, TC-8, TC-M1 |
