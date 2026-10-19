---
title: "JSON-RPC API"
status: draft
---

# Specification: JSON-RPC API

## Overview

Adds `internal/rpc`, a JSON-RPC 2.0 server that implements `http.Handler`, and a client for the command layer.
The server registers typed handlers for the read and cache commands plus `cli.run`, which runs the ghx binary in a child process.
`cmd/rpc.go` wires the server and exposes `rpc serve|call|methods|exec`; `cmd/remote.go` forwards any command with `--remote`; `internal/webui` gains `Mount` so `ghx serve` serves the API at `/rpc` on its existing port.
The protocol uses one HTTP endpoint with no sessions, so any HTTP client can participate.

## Architecture

```
ghx rpc call / --remote ──► rpc.Client ──► POST /rpc ──► rpc.Server.ServeHTTP
                                                             │
                                  ┌──────────────────────────┼──────────────────────────┐
                                  ▼                          ▼                          ▼
                            typed methods              cache.refresh                 cli.run
                                  │                          │                          │
                                  ▼                          ▼                          ▼
                       cache.Store / github.Client   fetchRepoData (mutex)      exec ghx binary
```

```
ghx serve ──► webui.Server.Handler()
                   ├─ /            UI + /api/*
                   └─ /rpc         rpc.Server
```

## Data Models

### Protocol types (`internal/rpc/protocol.go`)

| Type | Fields |
|---|---|
| `Request` | `jsonrpc`, `method`, `params` (raw object), `id` (raw; absent means notification) |
| `Response` | `jsonrpc`, `result` or `error`, `id` (null when absent) |
| `Error` | `code`, `message`, `data`; implements `error` |

Error codes: `-32700` parse, `-32600` invalid request, `-32601` method not found, `-32602` invalid params, `-32603` internal, `-32000` method error.

### Method params and results

| Method | Params | Result |
|---|---|---|
| `rpc.discover` | none | `{name, version, methods:[{name, description, params}]}` |
| `server.info` | none | `{name, version, storeKind, storeLocation, defaultRepo, execEnabled, methods, startedAt}` |
| `repo.list` | none | `{repos:[{host, owner, repo, label, issueCount, prCount, cachedAt, durationMinutes, complete, fresh}]}` |
| `repo.delete` | `{repo?}` | `{repo, deleted}` |
| `issue.list` | `{repo?, state?, author?, assignee?, labels?, milestone?, mention?, search?, app?, limit?}` | `{items:[Issue], total, source}` |
| `issue.view` | `{repo?, number, refresh?}` | `Issue` |
| `pr.list` | `{repo?, state?, author?, assignee?, labels?, base?, head?, draft?, search?, app?, limit?}` | `{items:[PullRequest], total, source}` |
| `pr.view` | `{repo?, number, refresh?}` | `PullRequest` |
| `pr.threads` | `{repo?, number, thread?, state?}` | `{threads:[ReviewThread]}` |
| `cache.refresh` | `{repo?, type?, force?, since?, durationMinutes?, retries?}` | `{repo, issues, prs}` |
| `cli.run` | `{args:[string], stdin?}` | `{stdout, stderr, exitCode}` |

`source` is `cache` or `api`.
`total` is the count before `limit` is applied.
`state` defaults to `open` for list and thread methods.
`type` defaults to `both`; `durationMinutes` defaults to 60 and `retries` to 2; explicit zero is honored.
`force` and `since` are mutually exclusive.

## API Contracts

### HTTP endpoint

```
POST /rpc
Content-Type: application/json
Authorization: Bearer <token>        (optional)

{"jsonrpc":"2.0","id":1,"method":"issue.list","params":{"repo":"cli/cli","state":"open"}}
```

- `200` with one response object, or an array for a batch
- `204` when every request in the batch is a notification, or for a single notification
- `401` with `WWW-Authenticate: Bearer realm="ghx"` when the token is missing or wrong
- `405` for any method other than POST
- Request bodies are limited to 16 MiB

### `rpc.Client`

```
Call(ctx, method, params) (json.RawMessage, error)
CallInto(ctx, method, params, out) error
Discover(ctx) (DiscoverResult, error)
Exec(ctx, args, stdin) (ExecResult, error)
```

The client normalizes `http://host:port` to `http://host:port/rpc`, sends the token as a bearer header, and returns the JSON-RPC `error` object as a Go error.

### CLI surface

| Command | Behavior |
|---|---|
| `rpc serve` | Runs the server; `--addr` default `127.0.0.1:8090`, `--token`, `--allow-exec` default true |
| `rpc call <method> [params]` | Calls one method and prints indented JSON; params may be a literal, `-`, or `@path` |
| `rpc methods` | Calls `rpc.discover` and prints a method/description table |
| `rpc exec [--url] [--token] [--] <args...>` | Calls `cli.run` and mirrors the output and exit code |
| `--remote [URL] <command...>` | Intercepted before cobra; forwards every argument through `cli.run` |

## Sequences

### Typed read

```
Client → POST /rpc issue.list
Server → resolve repo: params, else --repo or the server's git remote
       → store.IsIssuesCacheFresh
           fresh   → QueryIssues, sort by updatedAt desc, apply limit → source=cache
           stale   → client.ListIssues → BackfillIssues → source=api
Server → 200 {items, total, source}
```

### Run a command

```
Client → POST /rpc cli.run {args, stdin}
Server → exec.CommandContext(os.Executable(), baseArgs + args)
       → capture stdout/stderr, map *exec.ExitError to exitCode
Server → 200 {stdout, stderr, exitCode}
Client → write stdout/stderr, exit with exitCode
```

### Transparent remote mode

```
argv → takeRemoteArgs: strip --remote[=URL] and --rpc-token, keep the rest
     → readPipedStdin (only when stdin is not a terminal)
     → rpc.Client.Exec
     → writeRemoteOutput → os.Exit(result.ExitCode)
```

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Transport | JSON-RPC 2.0 over HTTP POST | One endpoint, standard error and batch semantics, every language has a client |
| Coverage | Typed methods for reads/cache, `cli.run` for the rest | Structured results for common operations without reimplementing every write command over RPC |
| Command parity | Child process running the same binary | Avoids duplicating argument parsing, output formatting, and exit-code semantics |
| Refresh serialization | `sync.Mutex` around `cache.refresh` | The file backend has no cross-goroutine write coordination |
| Authentication | Optional bearer token, required for non-loopback binds | Loopback stays frictionless; remote exposure is never unauthenticated |
| Serve integration | `webui.Mount("/rpc", ...)` | One process and port for the UI and the API |
| Remote flag handling | Pre-scan in `cmd.Execute` before cobra | Unknown flags and commands reach the server unchanged, so client and server versions may differ |
| Optional defaults | Pointer fields (`*int`) in params | Distinguishes omitted from explicit zero for limits, duration, and retries |

## Risks and Unknowns

1. `cli.run` is a trust boundary: a client with the token can run any ghx command, including comment and thread writes. The token is the only control.
2. There is no TLS, so a token sent over a non-loopback connection is only as private as the network.
3. `cache.refresh` checks for cancellation before it starts but the fetch path does not take a context, so a refresh that has started runs to completion even if the client disconnects.
4. `pr.threads` delegates to `gh.ListThreads`, which uses the local git and gh configuration rather than the server's `--repo`; it therefore targets the server working directory's repository when `repo` is omitted.
5. `--remote` pulls `--rpc-token` from the argument list anywhere it appears, including after `--`.

## Out of Scope

- WebSocket or streaming subscriptions
- TLS termination and multi-user access control
- Typed methods for commands beyond the read and cache surface
- Pagination or cursors beyond the CLI's existing limits
- Persisting RPC sessions or request history
