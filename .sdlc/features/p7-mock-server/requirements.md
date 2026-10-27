---
title: "Mock Server"
status: done
---

# Requirements: Mock Server

## Overview

Provides an in-process mock GitHub GraphQL server and a `mock serve` CLI command for testing gh-cached without real GitHub API access.
The mock server serves deterministic data from an in-memory scenario and supports three modes of scenario construction: fluent builder, parameterized simulation generator, and time-advance workflow.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| Developer | Test gh-cached commands and integration without network access, tokens, or rate limits |
| AI agent | Use the mock server to validate code changes against reproducible test data |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide a `mock serve` CLI command that starts an HTTP server implementing the GitHub GraphQL API contract |
| FR-02 | Must | The mock server shall handle all 7 GraphQL query patterns used by the GitHub client (get issue, get PR, list issues, fetch all issues, list PRs, fetch all PRs, search) |
| FR-03 | Must | The mock server shall support pagination with `pageInfo` (`hasNextPage`, `endCursor`) on all list and search endpoints |
| FR-04 | Must | The mock server shall require an `Authorization` header on every request |
| FR-05 | Must | The system shall provide a fluent scenario builder (`ScenarioBuilder`) for hand-crafted test data with options for state, assignees, labels, milestones, and comments |
| FR-06 | Must | The system shall provide a simulation generator (`Generate`) that produces realistic bulk data from a parameterized config |
| FR-07 | Must | The simulation generator shall support configurable: user count, repo count, issue/PR counts, comment counts, assignee/label/milestone counts, close/merge/draft/review rates, RNG seed, and activity bursts |
| FR-08 | Must | The system shall provide a time-advance mode that processes events chronologically to simulate evolving activity |
| FR-09 | Must | The mock server shall support runtime scenario updates via a concurrency-safe `UpdateScenario` method |
| FR-10 | Must | The `mock serve` command shall accept `--preset` (default, small, none) and allow individual overrides for all simulation parameters |
| FR-11 | Must | The `mock serve` command shall support `--stats` to print simulation statistics without starting the server |
| FR-12 | Should | The mock server shall support the `since` parameter for delta fetches on issues |
| FR-13 | Should | The mock server search endpoint shall support filtering by repo, author, label, state, draft, base branch, and head branch |
| FR-14 | Should | The `mock serve` command shall print usage examples on startup showing how to connect gh-cached to the mock server |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | The mock server shall respond to queries in under 1ms for scenarios with up to 1000 items |
| NFR-02 | Must | Reliability | Scenario updates shall be concurrency-safe via read/write mutex |
| NFR-03 | Must | Determinism | The simulation generator shall produce identical output for the same seed and config |

## Constraints

- Mock server is for testing only; it does not replicate all GitHub API behaviours
- Query routing is based on string matching (contains), not a full GraphQL parser
- Search filtering in the mock is a subset of GitHub's full search syntax

## Acceptance Criteria

- [ ] FR-01: `gh-cached mock serve --repos acme/testrepo` starts a server and prints its URL
- [ ] FR-02: All 7 client query patterns return valid responses with correct data shapes
- [ ] FR-05: Scenario builder produces deterministic scenarios with `WithSeed`
- [ ] FR-06: `Generate(DefaultConfig())` produces a scenario with issues and PRs across multiple repos
- [ ] FR-10: `--preset small` applies the small preset; individual flags override preset values
- [ ] FR-11: `--stats` prints config summary without starting the server
- [ ] FR-12: Delta fetches with `since` parameter return only items updated after the given time

## Open Questions

1. Should the mock server support GraphQL subscriptions or mutations in the future?
