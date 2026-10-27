---
title: "JSON-RPC API"
status: draft
---

# Requirements: JSON-RPC API

## Overview

Exposes a running ghx instance over JSON-RPC 2.0 so clients can call the CLI's operations without spawning a process per call.
Typed methods cover the read and cache commands with structured results, and a generic `cli.run` method gives full command parity for everything else.
A client CLI (`ghx rpc`), a transparent `--remote` mode, and a mount on the existing web UI server make the API usable without writing a protocol client.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| AI agent / automation | Call ghx repeatedly from one process instead of paying process startup and token resolution per call |
| CLI user | Run commands against a warm server from another shell or machine on the local network |
| Server operator | Control who can read the cache and who can run commands that change GitHub |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall serve JSON-RPC 2.0 over HTTP at `POST /rpc` on a configurable address |
| FR-02 | Must | The server shall expose typed methods `repo.list`, `repo.delete`, `issue.list`, `issue.view`, `pr.list`, `pr.view`, `pr.checks`, `pr.threads`, and `cache.refresh` |
| FR-03 | Must | The server shall expose `cli.run`, which executes any ghx command and returns its stdout, stderr, and exit code, and shall allow disabling it |
| FR-04 | Must | The system shall provide `ghx rpc serve`, `ghx rpc call`, `ghx rpc methods`, and `ghx rpc exec` |
| FR-05 | Must | `ghx rpc call` shall accept params as a JSON argument, read them from stdin with `-`, or read them from a file with `@path` |
| FR-06 | Must | The system shall forward any command to a server with `--remote [URL]`, preserving stdout, stderr, exit status, and piped stdin |
| FR-07 | Must | Every method that targets a repository shall accept an optional `[HOST/]OWNER/REPO` and fall back to the server's `--repo` or git remote |
| FR-08 | Must | `ghx serve` shall expose the same JSON-RPC API at `/rpc` on its web UI port |
| FR-09 | Must | The server shall support JSON-RPC batches and notifications |
| FR-10 | Should | The server shall answer `rpc.discover` with its method list and `server.info` with its version, store, and default repository |
| FR-11 | Should | Typed list methods shall report whether the result came from the cache or the API, plus the total before the limit is applied |
| FR-12 | May | `ghx rpc exec` shall accept leading `--url` and `--token` options before the forwarded command |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Security | `rpc serve` and `serve` shall refuse a non-loopback bind unless a bearer token is configured |
| NFR-02 | Must | Security | Token checks shall use a constant-time comparison |
| NFR-03 | Must | Reliability | `cache.refresh` shall run one refresh at a time, since the file backend has no cross-goroutine write coordination |
| NFR-04 | Must | Performance | A typed read with a fresh full cache shall answer without calling the GitHub API |
| NFR-05 | Must | Compatibility | The implementation shall use the Go standard library only, adding no dependency |
| NFR-06 | Should | Usability | A queued `cache.refresh` whose client has gone away shall not start |
| NFR-07 | Must | Usability | Invalid params, states, limits, and repository references shall return the JSON-RPC invalid-params code with an actionable message |

## Constraints

- The server has no TLS; encryption is the responsibility of a proxy in front of it
- `cli.run` executes the ghx binary with the server's environment, so it inherits the server's token and working directory
- Typed methods mirror the CLI's cache-first behavior, including freshness and backfill
- The API is stateless between requests; there is no session or per-client state

## Acceptance Criteria

- [ ] FR-01: `ghx rpc serve --host 127.0.0.1 --port 9000` answers a JSON-RPC request at `http://127.0.0.1:9000/rpc`
- [ ] FR-02: Each typed method returns its documented result for a cached repository
- [ ] FR-03: `cli.run` returns stdout, stderr, and the command's exit code; `--allow-exec=false` removes the method
- [ ] FR-04: `rpc methods` lists the exposed methods, and `rpc call` prints an indented JSON result
- [ ] FR-05: `rpc call issue.list -` reads params from stdin and `rpc call issue.list @params.json` reads them from a file
- [ ] FR-06: `ghx --remote http://127.0.0.1:8090 issue list` prints the remote output and exits with the remote status
- [ ] FR-07: Omitting `repo` uses the server default; an invalid repository returns `-32602`
- [ ] FR-08: `ghx serve` answers JSON-RPC at `/rpc` on its UI port
- [ ] FR-09: A batch returns one response per request, and a notification returns no body
- [ ] NFR-01: `rpc serve --host 0.0.0.0 --port 8090` without a token fails before listening
- [ ] NFR-03: Two concurrent `cache.refresh` calls complete one after the other
- [ ] NFR-04: `issue.list` against a fresh cache reports `source: "cache"`

## Open Questions

(none)
