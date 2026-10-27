# Project Overview

## Purpose

ghx is an extended GitHub CLI written in Go (module `github.com/tomzxcode/ghx`).
It caches issues, pull requests, and comments to disk to minimise GitHub API calls, and serves list and view commands from that cache when it is fresh, falling back to the API and backfilling the cache when it is not.
Beyond reads, it adds comment and review operations that the standard `gh` CLI does not offer: inline and line-range PR review comments, thread replies, pending reviews, comment edit and delete for issues and PRs, and local review-comment stashes.
It also ships a local web UI, a JSON-RPC 2.0 server, HTML stats and process reports, a mock GitHub server, local-only performance telemetry, and benchmark scripts.
The project was previously named `gh-cached`; it has since grown from a read-only, file-cache-only tool into the read/write, multi-backend tool described here.

## Key Stakeholders

| Stakeholder | Role | Interest |
|---|---|---|
| Developer / maintainer | Author (Tom Rochette) | Project direction, code quality, feature completeness |
| CLI user | End user | Fast, reliable issue and PR browsing with minimal API usage, plus review-comment workflows beyond `gh` |
| AI agents / automation | Consumer | Cached data and the JSON-RPC server as low-cost data sources and execution paths for agentic workflows |

## Scope

**In scope:**
- Listing and viewing GitHub issues and pull requests, served from the cache when fresh with fallback to the API
- Bulk cache population with delta (incremental) refresh, `--since` windowed refresh, `--type issues/prs` partial refresh, `--if-stale`, and `--force`
- Read-path cache backfill: list and view commands write what they fetch back into the cache
- Cache storage backends: indexed SQLite (`cache.db`, the default) and the legacy per-repository JSON file backend (`--storage file` / `GHX_STORAGE`), plus non-destructive `cache migrate`
- PR and issue comment write operations: top-level and inline/line-range PR review comments, thread replies, thread resolve/unresolve, pending reviews (create, list, submit, discard), local review-comment stashes (push, list, pop, drop), and comment edit/delete for issues and PRs
- Local web UI (`ghx serve`): repository switcher, issue/PR lists and details, Ctrl+P search palette, `/stats` and `/process` report pages, `/api/process` JSON endpoint
- JSON-RPC 2.0 server (`ghx rpc serve`) with typed read/cache methods plus `cli.run`, and `--remote` forwarding for any command
- HTML pull request activity reports (`ghx stats`)
- HTML issue and PR production-line flow reports (`ghx process`)
- Mock GitHub server (`ghx mock serve`) with scenario presets and latency injection fitted from recorded telemetry
- Local-only performance telemetry (`ghx telemetry`), recorded by default, with summary, export, enable/disable, and clear commands
- Benchmark scripts against `gh` and between storage backends (`scripts/benchmark.py`, `scripts/benchmark_rpc.py`, standard-library-only Python)
- Multi-platform binary builds (darwin, linux, windows; amd64 + arm64)
- GitHub Enterprise Server support via the `[HOST/]OWNER/REPO` form of `--repo`

**Out of scope:**
- Creating, updating, or deleting issues or PRs themselves (comment and review operations are in scope; issue/PR creation and edits are not)
- Real-time streaming or webhook-based updates
- Authentication management (delegates to the token resolution chain described under constraints)
- Caching data beyond issues and PRs (e.g. actions, releases, discussions)

## Key Constraints

- Go 1.26 minimum version (`go 1.26.0` in `go.mod`)
- SQLite is the default cache backend, stored under `~/.cache/ghx/cache/`; the legacy file backend stores one JSON file per issue/PR in the same tree
- Performance telemetry is local-only (SQLite at `~/.local/share/ghx/telemetry.db`), recorded by default, never uploaded, and a failure to record never fails or slows a command
- Token resolution chain: `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`
- Local servers (web UI and JSON-RPC) bind to the loopback interface by default (e.g. `127.0.0.1`)
- Single binary with no external runtime dependencies beyond a GitHub token; external Go dependencies are limited to cobra, progressbar/v3, goldmark, yaml.v3, and modernc.org/sqlite
- GitHub API rate limits apply when the cache is cold or stale; cached reads are offline-capable and avoid them
