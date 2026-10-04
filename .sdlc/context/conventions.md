# Conventions

## Naming

- **Files:** kebab-case for Go source files (e.g. `detect.go`, `api_test.go`); lowercase single words for packages
- **Variables:** camelCase (e.g. `repoFlag`, `issueListLabels`)
- **Functions / Methods:** camelCase, exported functions are PascalCase (e.g. `NewClient`, `SaveIssue`)
- **Constants:** PascalCase for exported, camelCase for unexported
- **Test helpers:** prefixed with `make` or `new` (e.g. `makeIssues`, `newTempStore`)

## Directory Structure

```
.
├── main.go                  # Entry point
├── cmd/                     # CLI commands (one file per command group)
│   ├── root.go              # Root command, global flags
│   ├── issue.go             # issue list, issue view
│   ├── pr.go                # pr list, pr view
│   ├── cache.go             # cache command, shared fetch logic (fetchRepoData)
│   ├── serve.go             # serve command (local web UI server)
│   ├── mock.go              # mock serve command
│   ├── repo.go              # repo list command
│   └── *_test.go            # Tests co-located with commands
├── internal/                # Private packages
│   ├── cache/               # Cache store (disk I/O)
│   ├── github/              # GitHub API client
│   ├── gitremote/           # Git remote detection
│   ├── mockserver/          # Mock GraphQL server, scenario builder, simulation
│   ├── webui/               # serve command's HTTP server, JSON API, embedded UI assets
│   └── version/             # Version resolution
├── .github/workflows/       # CI configuration
├── .agents/skills/          # Agent skill definitions
├── Makefile                 # Build targets
├── go.mod / go.sum          # Dependencies
└── README.md                # User-facing documentation
```

## Coding Standards

- Use Go standard library where possible; external dependencies are limited to `spf13/cobra` (CLI), `schollz/progressbar/v3` (cache progress bar), `gopkg.in/yaml.v3`, and `yuin/goldmark` (markdown to HTML for the web UI)
- The web UI frontend uses Vue 3, daisyUI 5, and the Tailwind v4 browser build, all loaded from CDNs (pinned major versions) with no build step; ghx-owned assets (HTML, CSS, JS) are embedded via `go:embed` under `internal/webui/assets/`, with `style.css` limited to an override layer (markdown typography, navbar helpers)
- All exported types and functions have godoc comments
- Error wrapping with `fmt.Errorf("context: %w", err)` for error chains
- CLI flags use both short and long forms where conventional (e.g. `-s, --state`)
- In-memory filtering uses case-insensitive comparison (`strings.EqualFold`)
- GraphQL queries are defined as raw string constants at the top of the API file
- Tests use table-driven patterns and `t.TempDir()` for filesystem tests

## Commit Messages

- Imperative mood, no prefix convention enforced
- Examples: `Add versioning`, `Fix Makefile`, `ci: update latest release on merge to main`
- Occasional conventional-commit prefixes (`ci:`, `feat:`, `docs:`)

## Branching

- `main` is the primary branch
- Feature branches named descriptively (e.g. `sdlc`, `smart-incremental-cache-updates`, `no-label-filter`)
- Agent-created branches follow the pattern `claude/<description>-<id>`

## SDLC Documentation Style

- One sentence per line in markdown files for easier diff/review
- Use sentence case for headings
- Prefer bullet lists over prose paragraphs
- Tables for structured data (requirements, API contracts, etc.)
