---
title: "Comment operations"
status: done
---

# Requirements: Comment operations

## Overview

ghx adds comment and review management beyond the standard gh CLI.
A reviewer can post top-level PR and issue comments, inline comments on a line or line range, file-level comments, and replies to existing review threads, then edit or delete any comment by its node ID regardless of its type.
Review threads can be listed with their comment IDs and resolved or unresolved in batches.
The pending-review lifecycle (create, add comments, list, submit as COMMENT, APPROVE, or REQUEST_CHANGES, discard) lets comments be batched into one review event, and a local, git-stash-style stash saves pending comments to disk so drafting can continue offline or across context switches.
Everything is a CLI surface over the GitHub GraphQL API; the JSON-RPC server exposes none of these operations.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| Code reviewer | Post inline and file-level comments, reply in threads, resolve threads, and batch comments into a pending review |
| AI agents / automation | Scriptable comment posting, editing, and thread resolution by node ID with predictable, fix-naming errors |
| Developer switching context | Stash pending review comments locally and restore them later without losing drafted text |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-1 | Must | The system shall add a top-level comment to a pull request with `ghx pr comment <number>` taking the body from `--body` or `--body-file` (with `-` reading stdin); the two flags are mutually exclusive and giving neither is an error |
| FR-2 | Must | The system shall add an inline comment via `--file` with `--line` giving a single line number or a `START-END` range where start must be less than end; a range maps to the GraphQL `startLine` (start) and `line` (end) fields |
| FR-3 | Must | The system shall add a file-level comment when `--file` is given without `--line`, by sending the GraphQL `subjectType` FILE |
| FR-4 | Must | The system shall place inline comments on the diff side given by `--side` (`LEFT` or `RIGHT`, default `RIGHT`); `--side` requires `--file` and `--line` requires `--file` |
| FR-5 | Must | The system shall reply to an existing review thread via `--reply-thread` with a thread ID; the flag is mutually exclusive with `--file` and `--line` |
| FR-6 | Must | The system shall stage a comment in a pending review instead of posting it immediately when `--pending` is given, reusing the current pending review or creating one; `--pending` requires `--file` or `--reply-thread` and is mutually exclusive with `--stash` |
| FR-7 | Must | The system shall save a comment to a local stash entry instead of calling the API when `--stash` (entry 0) or `--stash=N` (entry N, a non-negative integer) is given; `--stash` requires `--file` |
| FR-8 | Must | The system shall preserve an existing pending review when an immediate inline or reply comment is posted: the pending comments are saved to a stash entry, the pending review is deleted, the comment is posted, and the stashed comments are restored into a new pending review, submitting any leftover pending review as COMMENT first; restore failures are reported as warnings, not command errors |
| FR-9 | Must | The system shall add a top-level comment to an issue with `ghx issue comment <number>` taking the body from `--body` or `--body-file` under the same rules as FR-1 |
| FR-10 | Must | The system shall edit and delete a PR comment by node ID with `ghx pr comment edit` and `ghx pr comment delete`, detecting from the node `__typename` whether the comment is an inline review comment (PullRequestReviewComment) or a top-level issue comment (IssueComment) and dispatching to the matching mutation; any other type is an error |
| FR-11 | Must | The system shall edit and delete an issue comment by node ID with `ghx issue comment edit` and `ghx issue comment delete` |
| FR-12 | Must | The system shall list the review threads of a pull request with `ghx pr threads <number>` showing each thread's location, state (open, outdated, resolved), and comments, filtered by `--state` (`open` default, `resolved`, `all`), narrowed to one thread by `--thread`, and extended with comment node IDs by `--ids` |
| FR-13 | Must | The system shall resolve and unresolve review threads with `ghx pr threads resolve` and `ghx pr threads unresolve` accepting one or more thread IDs, attempting every thread even when one fails, printing each success, and joining the failures into one error that names each failing thread before exiting non-zero |
| FR-14 | Must | The system shall manage the pending-review lifecycle with `ghx pr review create <number>`, `list <number>` (showing only the current user's pending reviews), `submit <number>` (`--event` limited to COMMENT, APPROVE, and REQUEST_CHANGES with COMMENT as default, optional `--body` or `--body-file` summary, `--review` to target a specific review, error when none is pending), and `discard <review-id>` |
| FR-15 | Must | The system shall save the current pending review's comments to a new stash entry with `ghx pr review stash push <number>` (optional `--message`), shifting existing entries so the new one is stash@{0}, then deleting the pending review; a missing pending review or a pending review with no comments is an error |
| FR-16 | Must | The system shall restore a stash entry into a new pending review with `ghx pr review stash pop <number>` (`--stash` selects the entry, default 0) by recreating each saved thread (first body as the thread root, remaining bodies as replies) and removing the entry afterwards; an existing pending review on the PR is an error, and a stash removal failure after a successful restore is a warning |
| FR-17 | Must | The system shall list stash entries with `ghx pr review stash list <number>` (message, thread and comment counts, per-thread location) and remove an entry without restoring it with `ghx pr review stash drop <number>`; an out-of-range index or a PR with no stash is an error |
| FR-18 | Should | The system shall make comment node IDs discoverable for editing and deleting via `ghx pr threads <number> --ids` and `ghx issue view <number> --ids` |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-1 | Must | Architecture | The system shall perform all comment, review, thread, and stash-restore writes through the GitHub GraphQL v4 endpoint, with no REST calls |
| NFR-2 | Must | Security | The system shall resolve the API token from `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`, and fail with an error naming the fix when none is found |
| NFR-3 | Should | Performance | The GraphQL HTTP client shall time out after 30 seconds |
| NFR-4 | Should | Privacy | Stash data shall stay on local disk under the user cache directory (`~/.cache/ghx/stash/<owner>/<repo>/<pr>/` as YAML files, mode 0644 in directories of mode 0755) and never be uploaded |
| NFR-5 | Should | Usability | User-facing errors shall be lowercase sentences that name the fix (for example `--line requires --file`, `required: --body or --body-file`) |
| NFR-6 | May | Usability | The target repository shall default to the current checkout's `origin` remote and be overridable with `-R/--repo OWNER/REPO` |

## Constraints

- GitHub makes only the authenticated user's own pending reviews visible, so pending-review operations filter by the viewer's login and operate on the first match.
- `addPullRequestReviewThread` only accepts paths and lines inside the PR diff; an out-of-diff comment surfaces as "failed to create review thread: file or line may not be in the PR diff".
- Stash indices are dense integers starting at 0 with git-stash semantics: push shifts existing entries up, pop and drop remove one entry and shift the rest down.

## Acceptance Criteria

Every FR and NFR shall have at least one acceptance criterion.
Order criteria by FRs first (sorted by ID), then NFRs (sorted by ID).
Acceptance criteria verify how a requirement is proven done, they do not restate it.
The boxes stay unchecked: this feature was reverse-engineered from existing code during /sync-sdlc and the criteria were documented afterwards, so most were never executed as automated checks; tests.md records the coverage that actually exists.

- [ ] **FR-1**

    ```gherkin
    @FR-1
    Scenario: Post a top-level PR comment from a file
      Given a pull request numbered 10 in the current repository
      When I run "ghx pr comment 10 --body-file -" with the body on stdin
      Then the comment is created on PR #10 and its node ID is printed
    ```

    ```gherkin
    @FR-1
    Scenario: Body flags are mandatory and exclusive
      Given any pull request number
      When I run "ghx pr comment 10" with neither --body nor --body-file
      Then the command fails with "required: --body or --body-file"
      When I run "ghx pr comment 10 --body a --body-file b"
      Then the command fails because the flags are mutually exclusive
    ```

- [ ] **FR-2**

    ```gherkin
    @FR-2
    Scenario: Inline comment on a line range
      Given PR #10 contains src/main.go in its diff
      When I run "ghx pr comment 10 --file src/main.go --line 42-45 --body Nit"
      Then a thread is created anchored at line 45 with startLine 42
      When I run "ghx pr comment 10 --file src/main.go --line 45-42 --body Nit"
      Then the command fails with "invalid line range: start must be less than end"
    ```

- [ ] **FR-3**

    ```gherkin
    @FR-3
    Scenario: File-level comment
      Given PR #10 contains src/main.go in its diff
      When I run "ghx pr comment 10 --file src/main.go --body Looks clean"
      Then a file-level comment (subjectType FILE) is created on src/main.go
    ```

- [ ] **FR-4**

    ```gherkin
    @FR-4
    Scenario: Side flag requires a file
      Given any PR number
      When I run "ghx pr comment 10 --side LEFT --body Before"
      Then the command fails with "--side requires --file"
    ```

- [ ] **FR-5**

    ```gherkin
    @FR-5
    Scenario: Reply to an existing review thread
      Given thread PRRT_1 exists on PR #10
      When I run "ghx pr comment 10 --reply-thread PRRT_1 --body Agreed"
      Then a reply comment is added to thread PRRT_1 and its ID is printed
    ```

- [ ] **FR-6**

    ```gherkin
    @FR-6
    Scenario: Stage a comment in a pending review
      Given no pending review exists on PR #10
      When I run "ghx pr comment 10 --file src/main.go --line 42 --body Nit --pending"
      Then a pending review is created and the comment is added to it without being submitted
      When I run "ghx pr comment 10 --pending --body Nit"
      Then the command fails because --pending requires --file or --reply-thread
    ```

- [ ] **FR-7**

    ```gherkin
    @FR-7
    Scenario: Save a comment straight to a stash entry
      Given no API call should happen
      When I run "ghx pr comment 10 --file src/main.go --line 42 --body Nit --stash"
      Then the comment is appended to stash@{0} on local disk and the new thread count is printed
    ```

- [ ] **FR-8**

    ```gherkin
    @FR-8
    Scenario: Immediate comment preserves a pending review
      Given PR #10 has a pending review with two comment threads
      When I run "ghx pr comment 10 --file src/a.go --line 1 --body Now"
      Then the pending threads are stashed, the pending review is deleted, the comment is posted, and the threads are restored into a new pending review
    ```

- [ ] **FR-9**

    ```gherkin
    @FR-9
    Scenario: Comment on an issue
      Given issue #42 in the current repository
      When I run "ghx issue comment 42 --body Fixed in #50"
      Then a top-level comment is created on issue #42 and its node ID is printed
    ```

- [ ] **FR-10**

    ```gherkin
    @FR-10
    Scenario: Edit dispatches by comment type
      Given comment IC_1 is an IssueComment and comment PRRC_1 is a PullRequestReviewComment
      When I run "ghx pr comment edit IC_1 --body Updated"
      Then the issue-comment mutation is called
      When I run "ghx pr comment edit PRRC_1 --body Updated"
      Then the review-comment mutation is called
      When I run "ghx pr comment delete X" where X is a node that is not a comment, such as a pull request node
      Then the command fails with "unsupported comment type" followed by the node's type name
    ```

- [ ] **FR-11**

    ```gherkin
    @FR-11
    Scenario: Edit and delete an issue comment
      Given issue comment IC_1 exists
      When I run "ghx issue comment edit IC_1 --body Updated" and then "ghx issue comment delete IC_1"
      Then the comment body is updated and then the comment is deleted
    ```

- [ ] **FR-12**

    ```gherkin
    @FR-12
    Scenario: List threads with IDs and state filter
      Given PR #10 has one resolved thread and one open thread
      When I run "ghx pr threads 10 --ids"
      Then only the open thread is listed with its comment node IDs
      When I run "ghx pr threads 10 --state resolved"
      Then only the resolved thread is listed
    ```

- [ ] **FR-13**

    ```gherkin
    @FR-13
    Scenario: Resolve several threads when one fails
      Given threads T1 and T3 resolve successfully and T2 fails
      When I run "ghx pr threads resolve T1 T2 T3"
      Then all three threads are attempted, T1 and T3 are reported as resolved, and the command exits non-zero with an error naming only T2
    ```

- [ ] **FR-14**

    ```gherkin
    @FR-14
    Scenario: Submit a pending review
      Given a pending review exists on PR #10
      When I run "ghx pr review submit 10"
      Then the pending review is submitted with event COMMENT
      When I run "ghx pr review submit 10 --event CLOSE"
      Then the command fails naming the valid events
      When I run "ghx pr review submit 20" and PR #20 has no pending review
      Then the command fails with "no pending review found for PR #20"
    ```

- [ ] **FR-15**

    ```gherkin
    @FR-15
    Scenario: Push a pending review into the stash
      Given PR #10 has a pending review with two threads
      When I run "ghx pr review stash push 10 -m nit comments"
      Then a stash entry stash@{0} with the two threads is written locally and the pending review is deleted from GitHub
    ```

- [ ] **FR-16**

    ```gherkin
    @FR-16
    Scenario: Pop a stash back into a pending review
      Given PR #10 has stash@{0} and no pending review
      When I run "ghx pr review stash pop 10"
      Then a new pending review is created with all stashed threads and replies, and stash@{0} is removed
      When a pending review already exists on the PR
      Then popping fails with "existing pending review ... found; submit or discard it before popping stash"
    ```

- [ ] **FR-17**

    ```gherkin
    @FR-17
    Scenario: List and drop stash entries
      Given PR #10 has two stash entries
      When I run "ghx pr review stash list 10"
      Then both entries are listed with their thread and comment counts
      When I run "ghx pr review stash drop 10 --stash 5"
      Then the command fails with "stash@{5} does not exist (have 2 stash entries)"
    ```

- [ ] **FR-18**

    ```gherkin
    @FR-18
    Scenario: Discover comment IDs
      Given PR #10 and issue #42 have comments
      When I run "ghx pr threads 10 --ids" and "ghx issue view 42 --ids"
      Then the comment node IDs suitable for edit and delete are printed
    ```

- [ ] **NFR-1**

    ```gherkin
    @NFR-1
    Scenario: Writes go to the GraphQL endpoint only
      Given a local GraphQL test server records every request
      When any comment, review, thread, or stash-restore command runs against it
      Then every request is a POST to the GraphQL endpoint carrying a query document and no REST path is ever requested
    ```

- [ ] **NFR-2**

    ```gherkin
    @NFR-2
    Scenario: Missing token names the fix
      Given GH_TOKEN and GITHUB_TOKEN are unset and gh is not authenticated
      When any comment command runs
      Then it fails with an error that says to set GH_TOKEN or GITHUB_TOKEN or run gh auth login
    ```

- [ ] **NFR-3**

    ```gherkin
    @NFR-3
    Scenario: GraphQL client timeout
      Given a GraphQL server that never answers
      When a comment mutation is sent
      Then the client gives up after 30 seconds with a timeout error
    ```

- [ ] **NFR-4**

    ```gherkin
    @NFR-4
    Scenario: Stash stays local
      Given a stash push for acme/myproject PR #10
      When the stash is written
      Then the file lives under the user cache directory at ghx/stash/acme/myproject/10/0.yaml and nothing is sent to GitHub
    ```

- [ ] **NFR-5**

    ```gherkin
    @NFR-5
    Scenario: Errors name the fix
      Given any comment command
      When a required flag combination is missing
      Then the error is a lowercase sentence naming what to change, for example "--line requires --file"
    ```

- [ ] **NFR-6**

    ```gherkin
    @NFR-6
    Scenario: Repository resolution
      Given a checkout whose origin remote points at acme/myproject
      When I run "ghx pr comment 10 --body hi"
      Then the comment targets acme/myproject, and "-R other/repo" redirects it
    ```

## Conflicts

None identified.

## Open Questions

None.
