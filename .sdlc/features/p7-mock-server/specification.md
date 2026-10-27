---
title: "Mock Server"
status: done
---

# Specification: Mock Server

## Overview

The mock server is implemented in `internal/mockserver/` as an `httptest.Server` backed by an in-memory `Scenario`.
It handles all GraphQL query patterns sent by `github.Client`, using string-based query routing.
The `mock serve` CLI command in `cmd/mock.go` provides a user-friendly interface for starting the server with configurable presets and parameters.

## Architecture

```
mock serve ──► runMockServe()
                    │
                    ├─ buildSimConfig() ──► SimulationConfig
                    │
                    ├─ mockserver.Generate(cfg) ──► Scenario
                    │
                    └─ mockserver.NewServer(scenario)
                              │
                              └─ httptest.Server
                                    │
                                    └─ handleGraphQL() ──► route() ──► {getIssue, getPR, listIssues, fetchAllIssues, listPRs, fetchAllPRs, search}
```

### Scenario construction approaches

```
A. Fluent builder:
   NewScenarioBuilder(owner, repo) ──► AddIssue/AddPR ──► Build()

B. Simulation generator:
   Generate(SimulationConfig) ──► timeline events ──► chronological processing ──► Scenario

C. Time-advance:
   ScenarioBuilder.AdvanceTime(delta) ──► TimeAdvance ──► NewIssue/NewPR/Comment/Merge/Close ──► Build()
```

## Data Models

### SimulationConfig

| Field | Type | Constraints | Description |
|---|---|---|---|
| NumUsers | int | > 0 | Number of simulated GitHub users |
| Repos | []string | >= 1 | Repositories in "owner/repo" format |
| History | time.Duration | > 0 | Duration of simulated history |
| IssuesPerRepo | int | >= 0 | Issues to create per repository |
| PRsPerRepo | int | >= 0 | PRs to create per repository |
| CommentsPerIssue | int | >= 0 | Max comments per issue |
| CommentsPerPR | int | >= 0 | Max comments per PR |
| AssigneesPerIssue | int | >= 0 | Max assignees per issue |
| AssigneesPerPR | int | >= 0 | Max assignees per PR |
| LabelsPerItem | int | >= 0 | Max labels per item |
| MilestonesPerRepo | int | >= 0 | Milestones per repo |
| CloseRate | float64 | [0, 1] | Fraction of issues closed |
| MergeRate | float64 | [0, 1] | Fraction of PRs merged |
| DraftRate | float64 | [0, 1] | Fraction of PRs as drafts |
| ReviewRate | float64 | [0, 1] | Fraction of PRs with review decision |
| Seed | int64 | | RNG seed for deterministic output |
| Now | time.Time | | Reference time (defaults to time.Now()) |
| ActivityBursts | int | >= 0 | Number of high-activity windows |

### Scenario

| Field | Type | Description |
|---|---|---|
| Issues | []ScenarioIssue | All mock issues (embeds github.Issue with owner/repo) |
| PRs | []ScenarioPR | All mock PRs (embeds github.PullRequest with owner/repo) |

### Server

| Field | Type | Description |
|---|---|---|
| mu | sync.RWMutex | Protects scenario for concurrent access |
| scenario | *Scenario | Current scenario data |
| server | *httptest.Server | Underlying HTTP test server |

## API Contracts

The mock server exposes a single `POST /` endpoint that accepts GraphQL requests matching the GitHub API contract.
All 7 query patterns are routed by string matching on the query body.

### Query routing

| Pattern match | Handler | Returns |
|---|---|---|
| `issue(number:` | getIssue | Single issue with full comments |
| `pullRequest(number:` | getPR | Single PR with full comments |
| `issues(first:` + `states: [OPEN, CLOSED]` | fetchAllIssues | All issues with full comments, supports `since` |
| `issues(first:` (other) | listIssues | Filtered issue list with comment counts |
| `pullRequests(first: 100` + `states: [OPEN, CLOSED, MERGED]` | fetchAllPRs | All PRs with full comments |
| `pullRequests(first:` (other) | listPRs | Filtered PR list with comment counts |
| `search(query:` | search | Search results (issues and/or PRs) |

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| httptest.Server | Go's in-process test server | No external dependencies, deterministic, fast |
| String-based query routing | Contains checks on query body | Simple to implement; avoids a full GraphQL parser |
| Shared types | Reuses `github.Issue` and `github.PullRequest` | No translation layer between mock and production code |
| Event-based simulation | Timeline of chronologically sorted events | Produces internally consistent timestamps |
| Weighted time sampling | Activity bursts via distance-weighted probability | Simulates realistic activity patterns (hackathons, release sprints) |
| Concurrency-safe updates | sync.RWMutex on scenario | Allows safe runtime updates while serving requests |

## Risks and Unknowns

1. String-based query routing may break if query formatting changes significantly
2. Search filtering is a subset of GitHub's full search syntax; complex queries may not be accurately simulated
3. No support for GraphQL mutations or subscriptions

## Out of Scope

- Full GraphQL query parsing and validation
- Simulating API rate limits or errors
- Persisting scenarios to disk
- GraphQL subscriptions or mutations
