---
title: "Mock Server"
status: done
---

# Test Plan: Mock Server

## Scope

Covers the `mock serve` CLI configuration (presets, flag overrides, validation), the scenario builder and time-advance workflow, the simulation generator, the GraphQL query routing and response shapes, and the telemetry latency model.
The mock server is also used as a fixture by many tests outside this feature (`internal/integration`, `cmd/readcache_test.go`, `cmd/cache_fresh_test.go`, `cmd/storage_test.go`, `cmd/stats_test.go`); those tests are listed here only when they exercise mock-server behavior end to end.
NFR-01 (sub-1ms responses) and NFR-02 (concurrent scenario updates) have no dedicated tests; see the coverage matrix.

## Unit Tests

### CLI configuration (cmd/mock_test.go)

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | Default preset applies `DefaultConfig` values | `--preset default` | NumUsers and Seed match `DefaultConfig()` |
| TC-2 | Small preset applies `SmallConfig` values | `--preset small` | NumUsers matches `SmallConfig()` |
| TC-3 | Unknown preset is rejected | `--preset bogus` | Error naming the valid presets |
| TC-4 | Every simulation flag overrides the preset value | all 17 flags set | Each config field equals the flag value |
| TC-5 | Flags not passed keep preset values | `--users 12 --seed 99` on default preset | Overridden fields change, others stay default |
| TC-6 | `none` preset works when required flags are given | `--preset none --users 3 --repos org/test` | Config uses the flag values |
| TC-7 | `none` preset without `--repos` fails | `--preset none --users 3` | Error requiring at least one repo |
| TC-8 | `none` preset without `--users` fails | `--preset none --repos org/test` | Error requiring users > 0 |
| TC-9 | `--stats` parses as a boolean flag | `--preset small --stats` | Flag reads true |

### Simulation generator (internal/mockserver/simulation_test.go)

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-10 | SmallConfig produces the configured item counts | SmallConfig | Issues = IssuesPerRepo × repos, PRs = PRsPerRepo × repos |
| TC-11 | Every configured repo receives issues and PRs | SmallConfig | No repo has zero issues or zero PRs |
| TC-12 | Comments appear when comment counts are set | CommentsPerIssue/PerPR = 3 | At least one comment on issues and PRs |
| TC-13 | CloseRate bounds behave | CloseRate 1.0 / 0.0 | All issues closed / all issues open |
| TC-14 | MergeRate bounds behave | MergeRate 1.0 / 0.0 | All PRs merged / no PR merged |
| TC-15 | Merges happen after creation with distinct times | MergeRate 1.0, 90-day history | No mergedAt before createdAt; ≥ 2 distinct times |
| TC-16 | DraftRate produces drafts and ready-for-review events | DraftRate 1.0 / 0.0 | Drafts or ReadyForReview timeline events appear / no drafts |
| TC-17 | Timestamps are internally consistent | SmallConfig | createdAt ≤ updatedAt; closedAt/mergedAt ≥ createdAt; comments ≥ createdAt |
| TC-18 | Timestamps stay inside the simulated window | fixed Now, 30-day history | All createdAt in [now − history, now] |
| TC-19 | Same seed reproduces identical output | same SmallConfig twice | Identical issue/PR numbers and titles |
| TC-20 | Different seeds produce different output | Seed 1 vs Seed 2 | At least one title or count differs |
| TC-21 | Multiple repos each receive data | 3 repos, per-repo counts | 9 issues, 6 PRs across 3 distinct repos |
| TC-22 | ReviewRate 1.0 gives every PR a review decision | ReviewRate 1.0 | Every PR has a non-empty reviewDecision |
| TC-23 | Labels, assignees, and milestones appear when configured | counts set to 3 | Each appears on at least one item |
| TC-24 | Generated URLs match owner/repo/number | SmallConfig | Issue and PR URLs match the GitHub URL shape |
| TC-25 | Merged PRs never stay drafts | MergeRate 1.0, DraftRate 1.0 | No MERGED PR has isDraft true |
| TC-26 | Merged PRs and closed issues carry timestamps | rates 1.0 | mergedAt/closedAt set on merged PRs; closedAt on closed issues |
| TC-27 | Comments are in chronological order | 5 comments per item | Comment createdAt is non-decreasing |
| TC-28 | Activity bursts still generate data | ActivityBursts 3 | At least one issue exists |
| TC-29 | Large config generates quickly | 4 repos, 50/40 per repo, 180-day history | Under 5 s, exact issue/PR counts |
| TC-30 | `SimulationStats` renders a summary | DefaultConfig | Non-empty stats string |
| TC-31 | Zero comment counts produce no comments | CommentsPerIssue/PerPR = 0 | Every issue and PR has zero comments |
| TC-32 | PR head branches are set and base is main | SmallConfig | HeadRefName non-empty, BaseRefName `main` |
| TC-33 | CommentCount matches the comment slice | 3/2 comments | CommentCount equals len(Comments) everywhere |

### Latency model and query-kind mapping (internal/mockserver/latency_test.go)

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-34 | Model keeps per-kind samples, ignores empty kinds and zero durations, and samples the distribution tail including a 5000 ms outlier | mixed telemetry events | Kinds and sample counts correct; tail reached; missing kind delays 0 |
| TC-35 | Same seed reproduces the same sample sequence | two RNGs with seed 7 | Identical sample sequences |
| TC-36 | Query shapes map to the kinds the client records | 8 query strings | `routeKind` returns the matching telemetry kind, including PRFullPage for `first: $first` |

## Integration Tests

HTTP-level tests against the in-process server (internal/mockserver/server_test.go and simulation_test.go).

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-37 | Get issue returns a full node with comments | sample scenario | Correct number, title, state, author, 2 comments |
| TC-38 | Get a missing issue returns a GraphQL error | sample scenario | Errors list non-empty |
| TC-39 | Get PR returns state, draft, review decision, branches, comments | sample scenario | MERGED, not draft, APPROVED, main ← fix/crash, 1 comment |
| TC-40 | List issues returns pageInfo and nodes | sample scenario | 3 issues, no next page |
| TC-41 | List issues filters by state | states [OPEN] | Only OPEN issues returned |
| TC-42 | fetchAllIssues returns totalCount and full comments | sample scenario | totalCount 3, issue #1 has 2 comments |
| TC-43 | Search issues honors is:issue and is:open | `repo:… is:issue is:open` | 2 results with `__typename` Issue |
| TC-44 | Search PRs honors is:pr and is:open; drafts count as open | `repo:… is:pr is:open` | 2 results (OPEN + DRAFT) |
| TC-45 | List PRs returns all states | states OPEN/CLOSED/MERGED | 3 PRs |
| TC-46 | Missing Authorization header is rejected | no auth header | `unauthorized` error |
| TC-47 | `GenerateRealistic` builder serves its scenario | realistic builder scenario | 8 issues listed |
| TC-48 | Time-advance flow produces issues, comments, a PR, and a merge | AdvanceTime sequence | Issue #1 commented, PR #3 MERGED, non-empty summary |
| TC-49 | fetchAllPRs returns totalCount and comments | sample scenario | totalCount 3, PR #4 has 1 comment |
| TC-50 | Generated scenarios serve correctly through the server | SmallConfig | list issues succeeds with no errors |
| TC-51 | A raw client-shaped query parses the full issue node | client-shaped query | Issue parsed with number, state, comment total |

## End-to-End Tests

Real `github.Client` and CLI paths against the mock server (internal/integration).

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-52 | Client bulk fetches issues and PRs | FetchAllIssues/FetchAllPRs against a mock, with progress callbacks and oldest-first ordering | All items fetched, callbacks fire |
| TC-53 | Client single-item and list fetches | GetIssue, GetPR, ListIssues, ListPRs, NotFound path | Correct data; missing item errors |
| TC-54 | Delta fetch with `since` returns only newer issues | FetchAllIssues with since 1 h ago | No issue older than since |
| TC-55 | Fetch and cache end to end | fetch through the client into a cache store | Cache populated from mock data |
| TC-56 | Large generated scenario fetches fully | Generate large config, fetch all | Exact counts fetched |
| TC-57 | PR delta/resume fetch windows, pages, stops early, and resumes after interruption | FetchPRsUpdated variants and TestCache_ResumeAfterInterruption | Correct pagination, early stop, parallel pages, resume |
| TC-58 | CLI `issue list` against the mock server | run the CLI with `--api-url` on a counting mock | Correct issue table; request counts as expected |
| TC-59 | CLI `pr list` against the mock server | run the CLI against a mock | Correct PR table |
| TC-60 | CLI view against the mock server | run the CLI view path against a mock | Item renders from mock data |

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-61 | Missing issue number | GraphQL `issue #N not found` error (TC-38) |
| TC-62 | Missing Authorization header | `unauthorized` error (TC-46) |
| TC-63 | Unknown preset or missing required flags | Config error naming the fix (TC-3, TC-7, TC-8) |
| TC-64 | Zero comments configured | Empty comment lists, CommentCount 0 (TC-31) |
| TC-65 | Draft PR merged or closed | isDraft cleared on merge/close (TC-25) |
| TC-66 | Latency outlier in telemetry (a 502 retry) | Tail survives into sampling; unknown kinds ignored (TC-34) |
| TC-67 | Unrecognized GraphQL query shape | `unrecognized query` error (no dedicated test) |
| TC-68 | Runtime `UpdateScenario` while serving | Atomic replacement under RWMutex (no dedicated test) |
| TC-69 | Multi-page cursor walk beyond page 1 | Not exercised; pageInfo asserted only on single-page results |

## Test Infrastructure

- The server runs on `httptest` in-process, so tests need no network, tokens, or external processes.
- Builder fixtures: `sampleScenario()` in server_test.go, `readCacheScenario()` in cmd/readcache_test.go, and the integration suite's own `sampleScenario()`.
- Determinism comes from seeds (`WithSeed`, `SimulationConfig.Seed`, `LatencyModel.Seed`).
- The latency model tests note stale TC ids in latency_test.go:11 (`TC-9/TC-30` from a superseded plan numbering); the latency cases are TC-34 to TC-36 here.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-1 to TC-9, TC-58 to TC-60 (partial: the `runMockServe` startup loop itself is untested) |
| FR-02 | TC-37, TC-39 to TC-45, TC-49, TC-52, TC-53 |
| FR-03 | TC-40, TC-42, TC-43, TC-49 (partial: no multi-page cursor walk) |
| FR-04 | TC-46 |
| FR-05 | TC-47, TC-48, TC-51 (partial: `WithSeed` determinism not asserted for the builder) |
| FR-06 | TC-10, TC-11, TC-21 |
| FR-07 | TC-12 to TC-28, TC-31 to TC-33 |
| FR-08 | TC-48 |
| FR-09 | — (gap: no test calls `UpdateScenario`) |
| FR-10 | TC-1 to TC-8 |
| FR-11 | TC-9, TC-30 |
| FR-12 | TC-54 |
| FR-13 | TC-43, TC-44 (partial: author, label, base, head, and draft qualifiers not exercised directly) |
| FR-14 | — (gap: startup usage output untested) |
| NFR-01 | — (gap: no response-latency measurement; TC-29 measures generation time only) |
| NFR-02 | — (gap: RWMutex present in code but no concurrent-access test) |
| NFR-03 | TC-19, TC-20, TC-35 |
