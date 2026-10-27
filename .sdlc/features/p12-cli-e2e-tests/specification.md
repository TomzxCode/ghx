---
title: "CLI end-to-end tests"
status: done
---

# Specification: CLI end-to-end tests

## Overview

Adds two test files to `internal/integration` (package `integration`).
`cli_test.go` drives the built `ghx` binary as a subprocess against the in-process mock server, covering the cache-first read path, filters, JSON output, and error exits.
`cli_live_test.go` runs the same surface against the real GitHub API behind an explicit opt-in (`GHX_LIVE_TEST=1`) and a resolved token.

## Architecture

```
go test ./internal/integration/
  ├─ buildCLI()  go build -o <tmp>/ghx .   (once per test binary, sync.Once)
  ├─ startCountingServer()  httptest reverse proxy in front of mockserver.Server
  │                          counts every GraphQL request
  └─ runCLI()  exec <tmp>/ghx <args>  ──► stdout / stderr / exit code
                     │
                     ├─ fake server: --api-url <proxy URL> --cache-dir <tmp> --telemetry=false
                     └─ real server: GH_TOKEN <resolved> --cache-dir <tmp> --telemetry=false
```

The tests exec the real `ghx` binary rather than re-entering the cobra command in-process, so flag parsing, exit codes, and stream output are exercised exactly as a user sees them.
Building once per test binary keeps the suite fast; the binary goes in a temp directory removed by the last test's cleanup.

## Components

| Component | File | Responsibility |
|---|---|---|
| `buildCLI` | `cli_test.go` | Builds the real binary once, returns its path |
| `runCLI` | `cli_test.go` | Runs the binary with args and extra env, captures streams and exit code |
| `cliBaseArgs` | `cli_test.go` | Shared flags: `--api-url`, `--cache-dir`, `--storage sqlite`, `--telemetry=false`, `--log-level error` |
| `startCountingServer` | `cli_test.go` | Mock server behind a counting reverse proxy |
| `TestCLI_IssueList_MockServer` | `cli_test.go` | Cold list, warm zero-request list, state filter, JSON output |
| `TestCLI_PRList_MockServer` | `cli_test.go` | Open and merged filters, JSON output |
| `TestCLI_View_MockServer` | `cli_test.go` | Item views from cache, comments flag, PR-number redirect, missing-item error |
| `liveTestToken` | `cli_live_test.go` | Token chain: GH_TOKEN, GITHUB_TOKEN, `gh auth token` |
| `TestCLI_LiveGitHub` | `cli_live_test.go` | Opt-in live subtests: issue list (table + JSON), issue view, pr list (JSON) |

## Behavior

### Fake-server flow

Each test starts the mock server with `sampleScenario()` (issues #1-#5, PRs #6-#8).
A cold `issue list` asserts open-issue rows, exit 0, and at least one API request.
`ghx cache` then warms the full cache; a repeat `issue list` must produce the same rows (compared sorted, since the cache query sorts by updatedAt while the API preserves its own order) with zero additional requests.
State filters (`-s closed`, `-s merged`), `--json` output (parsed and field-checked), `issue view`/`pr view` output, the comments flag, the "#6 is a pull request" redirect, and the missing-item error are asserted the same way.

### Live flow

The test skips unless `GHX_LIVE_TEST=1` is set; it then resolves a token and skips if none is found.
Subtests run against `GHX_LIVE_REPO` (default `cli/cli`): table list, JSON list, a view of the first listed issue, and a JSON PR list.
Assertions cover exit codes, JSON parseability, and core fields; exact titles and counts are never asserted because live data changes.

## Error Handling

- A failing subprocess is reported with its exit code and stderr in the assertion message
- The live test skips (never fails) when the opt-in or token is missing, naming what is missing

## Test Plan

- `go test ./internal/integration/ -run TestCLI_ -count=1 -v`: all fake-server CLI tests pass; live test skips
- `GHX_LIVE_TEST=1 go test ./internal/integration/ -run TestCLI_LiveGitHub -count=1 -v`: live subtests pass against real GitHub
- `go test -race -cover ./...`: full suite green with the new files included
