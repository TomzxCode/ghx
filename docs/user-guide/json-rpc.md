# JSON-RPC API

`ghx rpc serve` runs a local JSON-RPC 2.0 server over HTTP.
Clients can call the CLI's read and cache operations with structured JSON results instead of spawning `ghx` processes, and `cli.run` gives full command parity for everything else.

## Start the server

```bash
ghx rpc serve
```

The server listens on `127.0.0.1:8090` and answers `POST /rpc`.

```bash
ghx rpc serve --addr 127.0.0.1:9000
```

The server uses the same cache and GitHub configuration as the CLI.
Pass the global flags at startup so `cli.run` children use them too:

```bash
ghx rpc serve --storage sqlite --cache-dir ~/.cache/ghx/cache --repo cli/cli
```

`ghx serve` also mounts the JSON-RPC API at `/rpc` on its web UI port, so one process can serve both.
It refuses a non-loopback `--addr` unless `--rpc-token` is set.

## Call methods

```bash
ghx rpc methods
ghx rpc call server.info
ghx rpc call repo.list
ghx rpc call issue.list '{"repo":"cli/cli","state":"open","limit":20}'
ghx rpc call issue.view '{"repo":"cli/cli","number":42}'
ghx rpc call pr.list '{"repo":"cli/cli","state":"merged","limit":10}'
ghx rpc call pr.view '{"repo":"cli/cli","number":1234,"refresh":true}'
ghx rpc call cache.refresh '{"repo":"cli/cli","type":"issues"}'
```

The params argument is a JSON object.
Use `-` to read it from stdin or `@path` to read it from a file.
Results are printed as indented JSON.

Any client can speak the protocol directly:

```bash
curl -s http://127.0.0.1:8090/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"issue.list","params":{"repo":"cli/cli","state":"open"}}'
```

## Run any CLI command

The `cli.run` method executes a ghx command on the server and returns its captured output and exit code:

```bash
ghx rpc call cli.run '{"args":["repo","list"]}'
ghx rpc call cli.run '{"args":["pr","comment","10","--body","Looks good"]}'
```

Output shape:

```json
{"stdout":"...","stderr":"","exitCode":0}
```

`ghx rpc exec` is a convenient wrapper:

```bash
ghx rpc exec -- repo list
ghx rpc exec -- pr view 10 --comments
```

Because flag parsing is disabled for `rpc exec`, put remote flags after `--`.
For convenience, leading `--url` and `--token` options are recognised before the command:

```bash
ghx rpc exec --url http://127.0.0.1:9000 -- repo list
```

Disable command execution with `ghx rpc serve --allow-exec=false` (or `ghx serve --rpc-allow-exec=false`).
The typed methods remain available.

## Transparent remote mode

Prepend `--remote URL` to any command to run it on a server instead of locally:

```bash
ghx --remote http://127.0.0.1:8090 issue list --state open
ghx --remote http://127.0.0.1:8090 repo list
```

Output and the exit status are forwarded, so scripts behave the same way.
Piped stdin is forwarded too, so commands such as `--body-file -` work through the server.

The URL can also come from `GHX_RPC_URL`:

```bash
export GHX_RPC_URL=http://127.0.0.1:8090
ghx --remote issue list
```

## Methods

| Method | Description | Main params |
|---|---|---|
| `rpc.discover` | List the methods the server exposes | |
| `server.info` | Version, store backend, default repository | |
| `repo.list` | Cached repositories with counts and freshness | |
| `repo.delete` | Remove one repository's cached data | `repo` |
| `issue.list` | List issues | `repo`, `state`, `author`, `assignee`, `labels`, `milestone`, `mention`, `search`, `app`, `limit` |
| `issue.view` | View one issue | `repo`, `number`, `refresh` |
| `pr.list` | List pull requests | `repo`, `state`, `author`, `assignee`, `labels`, `base`, `head`, `draft`, `search`, `app`, `limit` |
| `pr.view` | View one pull request | `repo`, `number`, `refresh` |
| `pr.threads` | List review threads | `repo`, `number`, `thread`, `state` |
| `cache.refresh` | Fetch into the cache, like `ghx cache` | `repo`, `type`, `force`, `since`, `durationMinutes`, `retries` |
| `cli.run` | Run a ghx command | `args`, `stdin` |

`repo` is `[HOST/]OWNER/REPO`.
When omitted, the server uses its `--repo` flag or the git remote of its working directory.
The server processes requests concurrently; `cache.refresh` runs one refresh at a time.

The read methods mirror the CLI's cache behavior: a fresh full cache is served locally, otherwise the method calls the GitHub API and backfills the cache.
`issue.list` and `pr.list` include `source` (`cache` or `api`) and `total` (before the limit is applied) in their results.

## Errors

Errors use standard JSON-RPC codes:

| Code | Meaning |
|---|---|
| `-32700` | Parse error |
| `-32600` | Invalid request |
| `-32601` | Method not found |
| `-32602` | Invalid params |
| `-32000` | Method error (GitHub, cache, or CLI failure) |

The server accepts batches and notifications.
A notification (a request without an `id`) is executed and produces no response body.

## Authentication and security

The server binds to loopback by default and the typed read methods only expose cached data.
`cache.refresh`, `repo.delete`, and `cli.run` can change the cache or GitHub, so binding a non-loopback address requires a token:

```bash
ghx rpc serve --addr 0.0.0.0:8090 --token "$GHX_RPC_TOKEN"
ghx rpc call repo.list --url http://host:8090 --token "$GHX_RPC_TOKEN"
```

Clients send the token as `Authorization: Bearer <token>` or `X-GHX-Token`.
`GHX_RPC_TOKEN` supplies it to the CLI commands.

The server has no TLS.
When exposing it beyond your machine, put a TLS-terminating proxy in front of it.
