---
title: "CLI end-to-end tests"
status: done
---

# Test Plan: CLI end-to-end tests

## Scope

This feature is itself a test suite, so this plan maps the suite's coverage of the assembled `ghx` CLI.
Covered: `issue list`, `pr list`, `issue view`, `pr view`, and `cache` run as a subprocess, including the cache-first read path, state filters, JSON output, and error exits against the in-process mock server, plus opt-in read-only live checks against real GitHub.
Out of scope: write commands (comment, review, and thread mutations are never exercised), and the library-level `github.Client` and `cache.Store` tests in the same package (`integration_test.go`), which belong to the features they test.

## Unit Tests

None; the feature adds no production code and every test drives the CLI as a subprocess asserting exit code, stdout, and stderr.

## End-to-End Tests

### Fake-server flow (internal/integration/cli_test.go)

Every invocation passes `--api-url` pointing at a counting reverse proxy in front of the mock server, a temporary cache directory, `--storage sqlite`, `--telemetry=false`, and `--log-level error`.
The mock scenario has issues #1-#5 (issue #1 closed) and PRs #6-#8 (PR #6 merged).

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-1 | Cold `issue list` fetches from the API | `issue list -R acme/myproject` on an empty cache | Exit 0; open issues #2 and #5 shown; closed #1 absent; at least one API request |
| TC-2 | A fresh full cache serves a repeat list with zero requests | `ghx cache`, then repeat `issue list` | Exit 0; rows identical to the cold list (compared sorted); 0 additional API requests |
| TC-3 | State filter | `issue list -s closed` | Exit 0; only closed issue #1 shown |
| TC-4 | JSON list output parses with core fields | `issue list --json` | Exit 0; array of 4 open issues, each with non-empty number, title, and state |
| TC-5 | `pr list` default open filter | `pr list -R acme/myproject` | Exit 0; PRs #7 and #8 shown; merged PR #6 absent |
| TC-6 | Merged filter | `pr list -s merged` | Exit 0; PR #6 shown |
| TC-7 | PR JSON output | `pr list --json` | Exit 0; array of 2 open PRs with core fields |
| TC-8 | `issue view` is served from the warm cache | `cache`, then `issue view 1` | Exit 0; title, state, and URL shown; 0 additional API requests |
| TC-9 | Comments flag | `issue view 1 -c` | Exit 0; authors alice and bob and their comment bodies shown |
| TC-10 | `pr view` renders a pull request | `pr view 7` | Exit 0; title and OPEN state shown |
| TC-11 | A PR number passed to `issue view` redirects with guidance | `issue view 6` (PR #6) | Non-zero exit; stderr names "pull request" and `pr view 6` |
| TC-12 | A missing item fails with guidance | `issue view 999` | Non-zero exit; stderr carries an "Error:" message |

### Live flow (internal/integration/cli_live_test.go)

The test binary re-executes itself as the CLI child, so live runs resolve the token through the documented chain (GH_TOKEN, GITHUB_TOKEN, `gh auth token`) and target `GHX_LIVE_REPO` (default `cli/cli`).

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-13 | Live table list | `GHX_LIVE_TEST=1`, token set; `issue list -R <repo> -L 5` | Exit 0; at least 3 rows |
| TC-14 | Live JSON list | `issue list -L 5 --json` | Exit 0; parseable non-empty array with core fields |
| TC-15 | Live view of a listed issue | list 1 issue as JSON, then `issue view <number>` | Exit 0; output starts with `#<number>` and carries a URL line |
| TC-16 | Live PR JSON list | `pr list -L 5 --json` | Exit 0; parseable array with core fields; zero open PRs tolerated for a custom repo |
| TC-17 | Opt-in and token gating | run without `GHX_LIVE_TEST=1`, then with it but without a token | Test skips both times; each skip message names what is missing |

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-18 | Issue number belongs to a pull request | Non-zero exit with `pr view` guidance on stderr (TC-11) |
| TC-19 | Item absent from the mock data | Non-zero exit with an error message on stderr (TC-12) |
| TC-20 | Row-order difference between API and cache queries | Cold and warm list rows compared sorted, so ordering alone never fails the cache-first assertion (TC-2) |
| TC-21 | Live repo with no open pull requests | Only payload shape asserted; empty result logged, not failed (TC-16) |

## Test Infrastructure

- The child CLI is the test binary re-executed: `TestMain` runs the real `cmd.Execute()` cobra tree when `GHX_TEST_CLI_CHILD=1`, giving real flag parsing, exit codes, and stream output without a separate build step.
- `runCLI` execs the child with extra environment and captures stdout, stderr, and exit code.
- `cliBaseArgs` supplies the shared flags (`--api-url`, `--cache-dir`, `--storage sqlite`, `--telemetry=false`, `--log-level error`).
- `startCountingServer` fronts `mockserver.Server` with an `httptest` reverse proxy that counts GraphQL requests, enabling the zero-request assertions.
- `sampleScenario()` (integration_test.go) builds the deterministic fixture: issues #1-#5, PRs #6-#8, seed 42.
- Every test writes its cache to `t.TempDir()`, so the suite needs no network, credentials, or shared state by default; the live tests run only when explicitly opted in.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-1 to TC-16 (every case asserts subprocess exit code, stdout, and stderr) |
| FR-02 | TC-1 to TC-12 |
| FR-03 | TC-2, TC-8 |
| FR-04 | TC-4, TC-7, TC-11, TC-12, TC-14, TC-16 |
| FR-05 | TC-17 |
| FR-06 | TC-13 to TC-16 |
| FR-07 | TC-17 |
| NFR-01 | TC-1 to TC-12 (offline; live tests skip without opt-in, so the default `go test ./...` run needs no network or credentials) |
| NFR-02 | TC-1 to TC-16 (temporary cache directories, telemetry disabled in every child invocation) |
