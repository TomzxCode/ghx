# Conventions

## Naming

- **Files:** lowercase Go source files, words separated by underscores when a name needs more than one word (e.g. `readcache.go`, `pr_review.go`, `serve_process.go`, `cache_since_test.go`); lowercase single words for packages (e.g. `cache`, `webui`, `processreport`)
- **Variables:** camelCase (e.g. `repoFlag`, `issueListLabels`)
- **Functions / Methods:** camelCase unexported, PascalCase exported (e.g. `NewClient`, `SaveIssue`)
- **Constants:** PascalCase for exported, camelCase for unexported
- **Cobra commands:** package-level vars named `<cmd>Cmd` (e.g. `issueListCmd`), run functions named `run<Command>` (e.g. `runIssueList`), flag variables grouped in one `var (...)` block per subcommand
- **CLI flags:** kebab-case long forms (e.g. `--no-truncate`, `--cache-duration`); short single-letter forms only where conventional (e.g. `-s, --state`, `-R, --repo`)
- **Test helpers:** prefixed with `new`, `make`, or `sample` (e.g. `newTempStore`, `makeIssues`, `sampleIssue`)

## Directory Structure

```
.
├── main.go                  # Entry point
├── cmd/                     # CLI commands (one file per command or flag group)
│   ├── root.go              # Root command, global flags
│   ├── issue.go, pr.go      # Read/browse commands (issue, pr, repo, cache, stats)
│   ├── pr_comment.go, ...   # One file per mutation command group (comment, review, threads)
│   ├── serve.go, ...        # serve command and per-page files (serve_stats.go, serve_process.go)
│   ├── rpc.go, remote.go    # JSON-RPC server mode and --remote forwarding
│   └── *_test.go            # Tests co-located with commands
├── internal/                # Private packages
│   ├── cache/               # Cache store (SQLite + file backends, schema.sql, filters)
│   ├── gh/                  # GraphQL mutation client (comment, review, thread writes)
│   ├── github/              # GraphQL query client (reads)
│   ├── gitremote/           # Git remote detection
│   ├── integration/         # End-to-end CLI tests
│   ├── logging/             # Leveled logging to stderr
│   ├── mockserver/          # Mock GraphQL server, scenarios, simulation
│   ├── processflow/         # PR/issue flow analysis
│   ├── processreport/       # Process report rendering (templates, SVG charts)
│   ├── rpc/                 # JSON-RPC 2.0 server/client and typed methods
│   ├── telemetry/           # Local timing aggregation and store
│   ├── webui/               # serve HTTP server, JSON API, embedded UI assets
│   └── version/             # Version resolution
├── docs/                    # mkdocs sources; user-guide/*.md, one page per command area
├── scripts/                 # Python benchmark scripts
├── .agents/skills/          # Project agent skills (ghx, mock-server, test-ghx)
├── .github/workflows/       # ci.yml (vet + test), build.yml, static-docs.yml (Pages deploy)
├── Makefile                 # build / install / uninstall (per-user PREFIX by default)
├── mkdocs.yml, pyproject.toml, uv.lock  # Docs site, managed with uv
├── go.mod / go.sum          # Dependencies
└── README.md                # User-facing documentation
```

## Coding Standards

- Use the Go standard library where possible; external dependencies are limited to `spf13/cobra` (CLI), `schollz/progressbar/v3` (progress bar), `gopkg.in/yaml.v3`, `yuin/goldmark` (markdown to HTML for the web UI), and `modernc.org/sqlite` (default cache backend)
- Read and write API calls are split by package: `internal/github` holds GraphQL queries (reads), `internal/gh` holds GraphQL mutations (comment, review, and thread writes)
- Commands use `RunE` and return errors; wrap chains with `fmt.Errorf("context: %w", err)`; user-facing errors are lowercase sentences that name the fix (e.g. `#5 is a pull request, not an issue; use "ghx pr view 5"`)
- Env var fallbacks exist for some flags (`GHX_STORAGE`, `GHX_RPC_URL`, `GHX_TELEMETRY`); the flag takes precedence and the help text says both
- In-memory filtering uses case-insensitive comparison (`strings.EqualFold`)
- GraphQL queries are defined as raw string constants at the top of the API file
- The web UI frontend uses Vue 3, daisyUI 5, and the Tailwind v4 browser build, all loaded from CDNs (pinned major versions) with no build step; ghx-owned assets are embedded via `go:embed` under `internal/webui/assets/`, with `style.css` limited to an override layer
- Tests use table-driven patterns, `t.TempDir()` for filesystem tests, and helpers that call `t.Helper()`; end-to-end CLI tests live in `internal/integration`
- Run `go test -race -cover ./...` before committing; CI also runs `go vet ./...`
- Formatting is gofmt only; there is no golangci-lint config, no `.editorconfig`, and no formatter config beyond gofmt
- All exported types and functions have godoc comments

## Commit Messages

- Short imperative subjects with no Conventional Commits prefixes (e.g. `Split the PR review wait into reviewer, author, and merge stations`, `Add resolve and unresolve review thread commands`)
- Commits that only reformat are titled `gofmt`
- Prefixes (`docs:`, `ci:`, `build:`) appear only in older history and dependabot bumps; do not add them to new commits

## Branching

- `main` is the primary branch
- Feature branches are named descriptively (e.g. `sdlc`, `json-rpc`, `stats`, `telemetry`)
- Agent-created branches follow the pattern `issue-<number>-<descriptor>` (e.g. `issue-1-kinetic-mongoose`)

## SDLC Documentation Style

- One sentence per line in markdown files (README, docs, and SDLC artifacts) for easier diff and review
- Use sentence case for headings
- Prefer bullet lists over prose paragraphs
- Use tables for structured data (requirements, flag references, API contracts)
- `docs/user-guide/` has one page per command area; `flag-reference.md` mirrors the binary's `--help` output and is updated in the same change that adds or changes flags
- Documentation is built with mkdocs via `uv`; run `uv run mkdocs serve` to preview
