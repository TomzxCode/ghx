---
title: "JSON-RPC API"
status: done
---

# Test Plan: JSON-RPC API

## Scope

The JSON-RPC 2.0 protocol handling (`internal/rpc/protocol.go`), the typed methods and `cli.run` (`internal/rpc/methods_read.go`, `methods_cache.go`, `server.go`), the client (`internal/rpc/client.go`), and the command-layer wiring (`cmd/rpc.go`, `cmd/remote.go`).
Out of scope: the web UI server itself and any end-to-end CLI run of `ghx --remote` against a live server, which has no automated coverage (`internal/integration` contains no RPC or remote tests).

## Unit Tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | `takeRemoteArgs` strips `--remote` with a separate URL and keeps the remaining arguments | `["--remote", "http://host:9000", "issue", "list"]` | URL set, rest `["issue", "list"]`, ok |
| TC-2 | `--rpc-token` is stripped only when `--remote` is present | `["--rpc-token", "secret", "issue", "list"]` | Not remote, arguments untouched |
| TC-3 | `--remote` without a URL falls back to the environment default | `["--remote", "issue", "list"]` | URL from `GHX_RPC_URL`, command forwarded |
| TC-4 | `isLoopbackAddr` classifies loopback and non-loopback hosts | `127.0.0.1:8090`, `localhost:8090`, `[::1]:8090` vs `0.0.0.0:8090`, `:8090`, `192.168.1.10:8090` | True for loopback, false otherwise |
| TC-5 | `ghx serve` refuses a non-loopback bind without a token | host `0.0.0.0`, empty token | `runServe` returns an error before listening |
| TC-6 | `ghx rpc serve` refuses a non-loopback bind without a token | host `0.0.0.0`, empty token | `runRPCServe` returns an error before listening |
| TC-7 | `splitRPCExecArgs` accepts leading `--url`/`--token` and stops forwarding at `--` | `["--url", "http://host:9000", "--token", "secret", "--", "issue", "list", "--state", "open"]` | URL, token, and forwarded args separated |
| TC-8 | Unknown method returns method-not-found with the echoed id | `{"method":"nope","id":1}` | Error code `-32601`, id `1` |
| TC-9 | Malformed JSON returns a parse error | `{not json` | Error code `-32700` |
| TC-10 | Wrong `jsonrpc` version is an invalid request | `"jsonrpc":"1.0"` | Error code `-32600` |
| TC-11 | A notification (no id) produces no response body | request without id | HTTP `204`, empty body |
| TC-12 | A batch returns one response per non-notification request | batch of discover, notification, unknown method | Two-element response array, error only for the unknown method |
| TC-13 | An empty batch is invalid | `[]` | Error code `-32600` |
| TC-14 | A configured bearer token is required and accepted | request with and without `Authorization: Bearer secret` | `401` without the token, `200` with it |
| TC-15 | `rpc.discover` reports version and every typed method, and hides `cli.run` when disabled | discover on a server with `AllowExec` false | All eight typed methods plus core listed, no `cli.run` |
| TC-16 | `cli.run` is absent when `AllowExec` is false | `cli.run` call on a restricted server | Error code `-32601` |
| TC-17 | `cli.run` captures stdout, stderr, and the exit code | `/bin/sh -c "printf hello; exit 3"` | stdout `hello`, exit code `3` |

## Integration Tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-18 | `issue.list` falls back to the API and `issue.view` caches the record | Mock GitHub server, empty cache | `source: "api"`, issue returned, record present in the store afterwards |
| TC-19 | A fresh full cache serves `issue.list` without the API | Cached issue plus fresh cache info | `source: "cache"`, cached issue returned |
| TC-20 | `pr.list` and `pr.view` return pull requests | Mock GitHub server | PR list with one item, `pr.view` returns its title |
| TC-21 | `issue.view` on a number cached as a pull request reports the type | Cached PR number 2 | Error message naming the other type, no GitHub call |
| TC-22 | `pr.view` on a number cached as an issue reports the type | Cached issue number 1 | Error message naming the other type, no GitHub call |
| TC-23 | `repo.list` and `repo.delete` resolve the default repository | Cache info saved, `DefaultRepo` configured | Label `mock/acme/testrepo`, delete reports `deleted: true` |
| TC-24 | `cache.refresh` forwards type and since, and rejects `force` plus `since` | Injected refresh and since parsers | Counts forwarded; combined flags return an error |
| TC-25 | Invalid params, numbers, and states return invalid params | Array params, number 0, state `bogus` | Error code `-32602` with actionable messages |
| TC-26 | A missing repository without a server default returns invalid params | No `DefaultRepo`, empty `repo` param | Error code `-32602` |
| TC-27 | Views serve items within the configured duration even when the cache is old | Aged item files, valid cache info, mock server closed | Cached issue and PR returned without the API |
| TC-28 | `rpc.Client` round-trips over real HTTP and surfaces JSON-RPC errors | httptest server wrapping the RPC server | `Discover` returns name `ghx`; unknown method returns an error |

## End-to-End Tests

None.
`internal/integration` has no scenario that starts the binary, serves the RPC API, and drives `ghx rpc call`, `ghx rpc methods`, `ghx rpc exec`, or `ghx --remote` against it.

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-29 | `cli.run` with empty `args` | Not covered; code returns `-32602` (`server.go:294-296`) |
| TC-30 | Negative `limit`, `durationMinutes`, or `retries` | Not covered; code returns `-32602` (`methods_read.go:208-210`, `methods_cache.go:56-65`) |
| TC-31 | A batch of only notifications | Not covered; code returns `204` (`protocol.go:121-123`) |
| TC-32 | A queued `cache.refresh` whose client has gone away | Not covered; code re-checks `ctx.Err()` before starting (`methods_cache.go:87-89`) |
| TC-33 | `rpc call` params via `-` (stdin) or `@path` | Not covered; `readRPCParams` (`cmd/rpc.go:182-208`) has no test |
| TC-34 | The `X-GHX-Token` alternative auth header | Not covered; accepted by `authorized` (`server.go:140-143`) but undocumented in the specification |
| TC-35 | `ghx serve` mounting the API at `/rpc` | Not covered; `internal/webui/server_test.go` has no `Mount` test |

## Test Infrastructure

- `internal/mockserver`: `NewScenarioBuilder` builds a mock GitHub GraphQL server with one issue and one pull request; `github.NewClientWithURL` points the server's client factory at it.
- `cache.NewStoreWithPath(t.TempDir())` provides a disposable cache store per test.
- `httptest` request recorders exercise the handler in-process (`postRPC`), and `httptest.NewServer` provides a real HTTP round trip for the client (`TestClientRoundTrip`).
- `internal/rpc` test helpers: `newTestServer`, `call`, `requestForTest`; `cmd` tests swap package-level flag variables and restore them with `defer`.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-4, TC-6, TC-8, TC-14, TC-28 |
| FR-02 | TC-15, TC-18, TC-19, TC-20, TC-21, TC-22, TC-23, TC-24 |
| FR-03 | TC-16, TC-17 |
| FR-04 | Not automated (no CLI-level test); underlying transport covered by TC-28 |
| FR-05 | Not automated (TC-33 notes the gap) |
| FR-06 | TC-1, TC-2, TC-3 (argument handling only; no remote round trip) |
| FR-07 | TC-23, TC-26 |
| FR-08 | Not automated (TC-35 notes the gap) |
| FR-09 | TC-11, TC-12, TC-13 |
| FR-10 | TC-15, TC-28 |
| FR-11 | TC-18, TC-19 (source; `total` before limit is not asserted) |
| FR-12 | TC-7 |
| NFR-01 | TC-5, TC-6 |
| NFR-02 | TC-14 (auth behavior; the constant-time property itself is not asserted) |
| NFR-03 | Not covered (no concurrent-refresh test) |
| NFR-04 | TC-19, TC-27 |
| NFR-05 | n/a (stdlib-only build constraint, verified by inspection) |
| NFR-06 | Not covered (TC-32 notes the gap) |
| NFR-07 | TC-21, TC-22, TC-25, TC-26 |
