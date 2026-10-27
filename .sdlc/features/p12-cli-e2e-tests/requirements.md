---
title: "CLI end-to-end tests"
status: done
---

# Requirements: CLI end-to-end tests

## Overview

Adds tests that drive the assembled `ghx` CLI (real command surface: flags, exit codes, stdout and stderr) instead of only the library layer.
Two targets are covered: a fake GitHub server (the in-process mock) and the real GitHub API.
Today `internal/integration` exercises `github.Client` and `cache.Store` directly, so CLI-level regressions (flag wiring, output formatting, exit codes, cache-first behavior through the commands) have no coverage; the codebase improvement reports list live-credential coverage as a known gap.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| Maintainer | Confidence that the shipped binary behaves correctly against real GitHub responses |
| AI agent | A red/green signal for CLI behavior changes without manual runs |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall have tests that invoke the `ghx` CLI as a subprocess with user-visible arguments, asserting exit code, stdout, and stderr |
| FR-02 | Must | The fake-server tests shall drive `issue list`, `pr list`, `issue view`, and `pr view` through the `--api-url` override against the in-process mock server |
| FR-03 | Must | The fake-server tests shall assert the cache-first read path: after `ghx cache`, a repeat list or view is served with zero API requests |
| FR-04 | Must | The fake-server tests shall assert JSON output parses with core fields and that error paths (missing item, item is a pull request) exit non-zero with guidance on stderr |
| FR-05 | Must | The real-server tests shall be opt-in and skipped by default: they run only with `GHX_LIVE_TEST=1` set and a token resolving through the documented chain (GH_TOKEN, GITHUB_TOKEN, `gh auth token`) |
| FR-06 | Should | The real-server tests shall be read-only (list and view only) and target a configurable repository (`GHX_LIVE_REPO`, default `cli/cli`) |
| FR-07 | Should | The real-server tests shall skip with a clear message when either the opt-in or the token is missing |

## Non-Functional Requirements

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | The default `go test ./...` run (CI and local) must not require network access or credentials; the whole fake-server CLI suite stays under ~20s |
| NFR-02 | Should | Isolation | CLI tests write only to temporary cache directories and run with telemetry disabled |

## Constraints

- The real-server tests depend on live data (titles and counts change), so they assert payload shape and exit codes, not exact content
- Comment and review write commands stay out of scope: the tests never mutate anything

## Acceptance Criteria

- [x] FR-01: `go test ./internal/integration/ -run TestCLI_ -v` spawns the ghx CLI as a subprocess and asserts on its output and exit codes
- [x] FR-02: list and view commands run against the mock server through `--api-url`
- [x] FR-03: a warm `ghx cache` makes the next list and view request-free
- [x] FR-04: `--json` output parses; `issue view 999` and `issue view <PR number>` exit non-zero with guidance
- [x] FR-05: without `GHX_LIVE_TEST=1` the live test skips; with it and a token it runs
- [x] FR-06: live subtests are list/view only and honor `GHX_LIVE_REPO`
- [x] FR-07: skip messages name the missing opt-in or token

## Open Questions

(none)
