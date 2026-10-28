<!-- session_link: <filled by skill> -->
# Infrastructure

<!--
This file captures the project's technology stack, development tooling, CI/CD pipelines,
environments, and deployment procedures. It gives implementation, testing, and deployment
skills the commands and configuration they need without grepping the codebase each time.
Update this file when tooling, environments, or deployment procedures change.
-->

## Technology Stack

| Component | Technology | Version |
|---|---|---|
| Language | Go (module `github.com/tomzxcode/ghx`) | 1.26.0 (from `go.mod`) |
| CLI framework | `spf13/cobra` | v1.10.2 |
| Markdown rendering (web UI) | `yuin/goldmark` | v1.8.6 |
| YAML parsing | `gopkg.in/yaml.v3` | v3.0.1 |
| Database (cache, telemetry) | `modernc.org/sqlite` (pure Go, no CGO) | v1.60.1 |
| Web UI frontend | Vue 3, daisyUI 5, Tailwind v4 browser build, loaded from CDNs, no build step | pinned major versions |
| Documentation | Python + mkdocs + mkdocs-material, managed with uv | Python >= 3.14, mkdocs >= 1.5.0, mkdocs-material >= 9.4.0 |
| Benchmark scripts | `scripts/benchmark.py`, `scripts/benchmark_rpc.py` (standard library only) | n/a |

## Development Tooling

| Tool | Purpose | Command |
|---|---|---|
| make | Build | `make build` (output `ghx`, version stamped via ldflags from `git describe`) |
| make | Install / uninstall locally | `make install` / `make uninstall` (installs to `~/.local/bin`, override with `PREFIX=`) |
| go | Vet | `go vet ./...` |
| go | Tests | `go test -race -cover ./...` |
| gofmt | Formatting | `gofmt -w .` (no other formatter or linter is configured) |
| uv | Docs environment setup | `uv sync --locked --dev` |
| mkdocs | Docs preview | `uv run mkdocs serve` |
| mkdocs | Docs build | `uv run mkdocs build` (outputs to `site/`) |
| python3 | Benchmarks | `uv run python scripts/benchmark.py` / `uv run python scripts/benchmark_rpc.py` (against a mock or live server) |

## Environments

| Environment | Branch | Purpose | URL |
|---|---|---|---|
| Local CLI | any (developed on `main` and feature branches) | Development and daily use; the binary runs against the real GitHub API | n/a (runs in the user's terminal) |
| Local servers | main | Optional web UI (`ghx serve`) and JSON-RPC server (`ghx rpc serve`), loopback by default, token required for non-loopback binds | `http://localhost:<port>` |
| `latest` prerelease | main | Rolling release channel, rebuilt on every push to main | https://github.com/TomzxCode/ghx/releases/tag/latest |
| Tagged builds | `v*` tags | Version-stamped cross-compiled artifacts (workflow artifacts only, no automated release) | workflow run artifacts |
| Docs | main | mkdocs site published to GitHub Pages | https://tomzxcode.github.io/ghx |

## CI/CD Pipelines

| Workflow | Trigger | What it runs |
|---|---|---|
| ci.yml | All pushes and PRs | `go vet ./...`, then `go test -race -cover ./...` with the Go version from `go.mod` |
| build.yml | All pushes, PRs, and `v*` tags | Cross-compiles darwin/amd64, darwin/arm64, linux/amd64, windows/amd64 (`CGO_ENABLED=0`), uploads artifacts; version is the tag name for `v*` builds; on push to main only, deletes and recreates the `latest` prerelease with the four binaries |
| static-docs.yml | Push to main, manual dispatch | Installs uv, syncs the locked dev environment, runs `mkdocs build`, uploads the site, deploys to GitHub Pages |

## Deployment

ghx is a CLI, so deployment means distributing a binary, and there is no server fleet or manual approval step.

- Merging to main automatically updates the `latest` prerelease (build.yml `release-latest` job) and republishes the docs site (static-docs.yml).
- Users install with `go install github.com/tomzxcode/ghx@main`, with a pinned version (`@<tag-or-sha>`), or by downloading a binary from GitHub Releases.
- Local installs use `make install`, which builds and copies the binary to `$PREFIX/bin` (default `~/.local/bin`, no sudo; `DESTDIR` is honored for packaging).

### Rollback

There is no infrastructure to roll back and no automated procedure.

- Pin the previous version: `go install github.com/tomzxcode/ghx@<previous-tag-or-sha>`, or check out the previous commit and run `make install`.
- Re-download a previous `latest` prerelease asset before the next push to main overwrites it (older releases stay on GitHub Releases).
- The only persistent state is the user's local cache and telemetry SQLite database, so a downgrade only risks a local schema mismatch, never a shared outage.

## Health Checks

There are no health check endpoints and nothing monitors ghx after install.

- The CLI exits with a non-zero status on failure, which is the only health signal.
- The local `ghx serve` and `ghx rpc serve` servers have no dedicated health endpoint.

## Smoke Tests

Smoke testing is done by the Go test suite, not by hitting live endpoints.

- `go test -race -cover ./...` runs end-to-end CLI tests in `internal/mockserver/` (`integration_test.go`, `cli_test.go`, `cli_live_test.go`), where the mock GraphQL server (`internal/mockserver`) stands in for GitHub so command behavior is exercised without network access.
- The same mock server can be run by hand (`ghx mock serve`, then point `GH_HOST` or the RPC/Web UI at it) for a manual smoke pass against known scenarios.

## Hosting

GitHub hosts everything; there is no server infrastructure, no regions, and no scaling strategy.

- Binaries: GitHub Releases, under https://github.com/TomzxCode/ghx/releases (the rolling `latest` prerelease plus any tagged releases).
- Documentation: GitHub Pages at https://tomzxcode.github.io/ghx, deployed by `static-docs.yml`.
- There is no infrastructure-as-code; the workflows in `.github/workflows/` are the entire deployment configuration.

## Secrets Management

No secrets are stored in the repository.

- ghx resolves its GitHub token at runtime from `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`; the user supplies whichever credential exists on their machine, and ghx stores nothing itself.
- Cache and telemetry data stay in the user's local SQLite database and are never uploaded.
- CI uses the workflow's built-in `github.token` (scoped to `contents: write`) only for the `latest` release job; the Pages deployment uses the standard GitHub Pages OIDC flow.
