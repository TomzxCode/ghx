# Architecture

## System Overview

```
User ──► CLI (cobra) ──► Cache Store (disk)
                 │              │
                 │              ▼
                 │       Fresh cache? ──Yes──► Filter in-memory ──► Output
                 │              │
                 │              No
                 │              ▼
                 └──────► GitHub GraphQL Client ──► Output
                                  │
                                  ▼
                           Save to cache
```

The tool is a single Go binary with four layers: CLI commands, business logic (filtering, display), cache store, and GitHub API client.

## Key Components

| Component | Responsibility | Technology |
|---|---|---|
| `cmd/` (root, issue, pr, cache, repo, mock) | CLI interface, flag parsing, command handlers | cobra |
| `internal/cache/` | On-disk JSON cache read/write, freshness checks | Go standard library |
| `internal/github/` | GitHub GraphQL API client, query building, response parsing | Go net/http |
| `internal/gitremote/` | Git remote URL detection and parsing | Go os/exec |
| `internal/mockserver/` | In-process mock GitHub GraphQL server, scenario builder, simulation generator, optional latency injection | Go net/http/httptest |
| `internal/telemetry/` | Opt-in local SQLite recorder for API and cache timings, with a disabled no-op path | Go standard library + modernc.org/sqlite |
| `internal/version/` | Build version resolution from ldflags or Go build info | Go runtime/debug |

## Data Flow

1. User invokes a CLI command (e.g. `gh-cached issue list`).
2. The command handler resolves the target repository from `--repo` flag or git remote.
3. A `cache.Store` checks whether the full cache is fresh for that repo.
4. If fresh, cached JSON files are loaded into memory and filtered according to CLI flags.
5. If stale or missing, a `github.Client` makes GraphQL API calls, saves results to cache, and returns them.
6. Results are formatted (tabular or JSON) and printed to stdout.

## Infrastructure

- **CI/CD:** GitHub Actions (`.github/workflows/build.yml`)
  - Builds for 4 platforms: darwin-amd64, darwin-arm64, linux-amd64, windows-amd64
  - On push to main: creates/updates a `latest` prerelease with all binaries
- **Distribution:** `go install` from source, or pre-built binaries from GitHub Releases
- **Testing:** Mock server (`internal/mockserver`) provides an `httptest.Server` that mirrors the GitHub GraphQL API contract for integration tests without network access
- **No observability:** The tool is a CLI with no runtime metrics, logging, or tracing; performance data is available only through the local-only telemetry recorder (`internal/telemetry/`), which is on by default, switchable with `ghx telemetry enable|disable`, and never transmits anything (see `.sdlc/knowledge/decisions/1-telemetry-default-on.md`)
- **No hosting:** Fully client-side, no server component (mock server is for testing only)

## Architecture Decisions

- GraphQL-only API access (no REST endpoints used), documented in code and README
- File-per-item cache layout (`issues/42.json`, `prs/10.json`) for efficient individual lookups
- Token resolution chain: `GH_TOKEN` > `GITHUB_TOKEN` > `gh auth token`
- Authoritative cache mode: when full cache is fresh, `view` commands serve from cache without API fallback
- Mock server uses `httptest.Server` with route-based query matching (string contains) for simplicity
- Simulation generator uses an event-based timeline with weighted time sampling for realistic activity bursts
