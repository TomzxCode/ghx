---
title: "Comment operations"
status: done
---

# Test Plan: Comment operations

## Scope

This plan documents the tests that exist in the repository for the comment, review-thread, pending-review, and stash functionality.
The feature was reverse-engineered from already-merged code during /sync-sdlc, so this is a coverage record, not a plan written ahead of implementation: most write paths have no automated tests, and that is documented here rather than papered over.
Out of scope: read-side browsing (`pr view`, `issue view` bodies, cache behavior) covered by FEAT-p4 and FEAT-p3, and the mock server's data seeding itself.

## Unit Tests

### cmd/pr_threads_resolve_test.go (3 tests, batch resolution logic)

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | One failing thread does not stop the batch and the error names only the failure | Thread IDs T1, T2, T3 with a stub `set` failing on T2 | All 3 attempted; success lines printed for T1 and T3; error contains "T2: not found" and no other thread |
| TC-2 | Every failure is joined into one error | 3 thread IDs, stub always fails | `set` called 3 times; the error names every thread |
| TC-3 | All-success batch returns nil and prints in order | 2 thread IDs, stub always succeeds | nil error; one success line per thread |

### internal/gh/pr_test.go (4 tests; TC-4 and TC-5 are cases of one table-driven test)

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-4 | `ResolveThread` sends the resolve mutation with the thread ID variable | Thread ID against a stub GraphQL server | Query contains `resolveReviewThread(input: {threadId: $threadId})`, variable carries the ID, no error |
| TC-5 | `UnresolveThread` sends the unresolve mutation | Thread ID against a stub GraphQL server | Query contains `unresolveReviewThread(input: {threadId: $threadId})`, response `isResolved` false, no error |
| TC-6 | A missing thread (null payload) is an error | Response with `thread: null` | Non-nil error |
| TC-7 | A GraphQL errors payload is surfaced with its message | Response with an errors array mentioning an unresolvable node | Error contains the server's message |

The stub server is `withGraphQLServer`: an `httptest.Server` swapped into `graphqlEndpoint` with `GH_TOKEN` set via `t.Setenv` and both restored by `t.Cleanup`.
TC-4 and TC-7 double as the only automated evidence for NFR-1 (requests are GraphQL POSTs carrying a query document).

## Integration Tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-8 | `ghx issue view 1 -c` renders seeded comments end to end through the mock server (internal/integration/cli_test.go) | Mock server seeded with issue comments via `WithIssueComment` | Output contains each comment author and body; exercises the same `internal/gh` issue-comment read used by ID discovery, but not the `--ids` flag |

The mock server implements none of the comment, review, thread, or stash mutations (verified: no `addComment`, `submitPullRequestReview`, `resolveReviewThread`, or similar operation appears in `internal/mockserver`), so no write command can run against it.
`internal/integration/integration_test.go` seeds PR comments and issue comments (`WithPRComment`, `WithIssueComment`) only as input to stats and process reports; those assertions belong to FEAT-p8 and FEAT-p1.

## End-to-End Tests

None.
No test drives any comment, review, thread-resolve, or stash command against a real or mocked GraphQL endpoint through the cobra command layer.
The only command-layer tests for this feature are TC-1 to TC-3, which call `applyThreadState` directly with a stubbed mutation function.

## Edge Cases and Failure Scenarios

Implemented in code but not covered by any test except where noted:

| ID | Scenario | Expected Behavior | Tested |
|---|---|---|---|
| TC-9 | Stash push with no pending review | Error naming the repository and PR | No |
| TC-10 | Stash push when the pending review has no comments | Error "pending review ... has no comments to stash" | No |
| TC-11 | Stash pop with an existing pending review | Error "existing pending review ... found; submit or discard it before popping stash" | No |
| TC-12 | Stash index out of range (pop, drop, append) | Error "stash@{N} does not exist (have M stash entries)" | No |
| TC-13 | Push shifts existing entries up; pop and drop shift the rest down and remove the directory when empty | Dense indices preserved | No |
| TC-14 | List with no stash at all | Error "no stash found for OWNER/REPO#N" | No |
| TC-15 | Line range with start not less than end | Error "invalid line range: start must be less than end" | No |
| TC-16 | Non-numeric `--line` | Error "invalid line number" | No |
| TC-17 | `--line` or `--side` without `--file`, `--pending` or `--stash` without an inline target | Fix-naming guard errors | No |
| TC-18 | Edit or delete of a node that is neither PullRequestReviewComment nor IssueComment | Error "unsupported comment type" plus the type name | No |
| TC-19 | `--body` and `--body-file` both given | Cobra mutual-exclusion error | No |
| TC-20 | Stash restore partially failing after a force-push (thread no longer in diff) | Error names the file; pending review exists with the threads restored so far | No |

## Test Infrastructure

- `withGraphQLServer` (internal/gh/pr_test.go): in-process `httptest.Server` replacing `graphqlEndpoint`, plus `t.Setenv("GH_TOKEN", ...)`, restored via `t.Cleanup`; no fixtures or recorded traffic.
- Stub mutation functions in cmd tests stand in for the GraphQL client, so the command plumbing and the GraphQL documents are never exercised together.
- Integration tests use the project's mock server (internal/mockserver), which covers reads only.
- Gaps: no tests for `internal/gh/stash.go` (all nine exported functions), no tests for `AddTopLevelComment`, `AddReviewThread`, `ReplyToThread`, `GetNodeType` dispatch, `EditReviewComment`, `EditIssueComment`, `DeleteReviewComment`, `DeleteIssueComment`, the pending-review lifecycle functions, or `RestorePendingReview`; no tests for `resolveBodyFlags`, `parseLineRange`, or the `runPRComment` flag guards; no end-to-end coverage for any command in this feature.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-1 | None |
| FR-2 | None |
| FR-3 | None |
| FR-4 | None |
| FR-5 | None |
| FR-6 | None |
| FR-7 | None |
| FR-8 | None |
| FR-9 | None |
| FR-10 | None |
| FR-11 | None |
| FR-12 | None (the `pr threads` list path shares TC-8's read plumbing only indirectly) |
| FR-13 | TC-1, TC-2, TC-3 (batch logic), TC-4, TC-5, TC-6, TC-7 (GraphQL layer) |
| FR-14 | None (only the resolve/unresolve mutations TC-4 to TC-7 touch thread state, not the review lifecycle) |
| FR-15 | None |
| FR-16 | None |
| FR-17 | None |
| FR-18 | None |
| NFR-1 | TC-4, TC-5 (partial: two mutations shown to be GraphQL documents against a stub server) |
| NFR-2 | None (token resolution is exercised implicitly by `withGraphQLServer`, not asserted) |
| NFR-3 | None |
| NFR-4 | None |
| NFR-5 | TC-1 (error naming the failing thread) |
| NFR-6 | None |

Honest summary: only FR-13 (resolve and unresolve, batch behavior and GraphQL layer) is meaningfully covered, by seven unit tests.
Everything else in this feature, including the entire stash store and every comment or review mutation, relies on manual verification.
Raising this would mean: unit tests for the stash store over `t.TempDir()` (TC-9 to TC-14), mutation-document tests in the `withGraphQLServer` style for the eleven operations, guard tests for `runPRComment` (TC-15 to TC-19), and mock-server support for the write mutations to unlock integration coverage.
