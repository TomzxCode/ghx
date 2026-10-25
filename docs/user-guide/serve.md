# Serve (web UI)

The `serve` command starts a local web server that browses the ghx cache in your browser.
It lists every cached repository, shows issue and pull request lists and details in a GitHub-like layout, and includes a Ctrl+P search palette.

## Start the server

```bash
ghx serve
```

The server prints its URL (default `http://127.0.0.1:8080`) and serves until you stop it with Ctrl+C.

```bash
ghx serve --host 127.0.0.1 --port 9000 --open
```

`--host` and `--port` override the listen address and `--open` opens your browser automatically.

## Using the UI

- **Repository switcher**: the dropdown in the top bar lists all cached repositories (as produced by `ghx repo list`) with their issue and PR counts.
- **List view**: tabs for Issues and Pull requests with open/closed/all filters (merged for PRs) and a text filter over titles.
- **Detail view**: click a row to see the state badge, metadata (author, dates, labels, assignees, milestone, branches), the rendered markdown body, and the comment timeline.
- **Search palette**: press <kbd>Ctrl</kbd>+<kbd>P</kbd> (or <kbd>Cmd</kbd>+<kbd>P</kbd>, or `/`) to open the palette.
  Type to fuzzy-search issues and PRs by number or title, use <kbd>↑</kbd>/<kbd>↓</kbd> and <kbd>Enter</kbd> to jump, and <kbd>Tab</kbd> to switch the search scope between the current repository and all cached repositories.
- **Theme**: the sun/moon button in the top bar switches between light and dark themes.
  The choice is remembered in your browser; on first visit the UI follows your system preference.
- **Refresh**: the refresh button in the top bar fetches new issues and PRs for the current repository using the same resumable logic as `ghx cache`.
- **Stats**: the Stats button in the top bar opens the pull request activity report (the same report `ghx stats` writes to a file) for the current repository, or for every cached repository from the home page.

The UI is read-only against the cache except for the explicit refresh action; the stats report is computed from cached pull requests only.

## Loading data

The UI serves whatever is in the cache.
If nothing is cached yet, run the cache command first:

```bash
ghx cache
```

## Markdown rendering

Issue bodies, PR bodies, and comments are rendered from markdown to HTML on the server (GitHub-flavoured tables, task lists, strikethrough, and autolinks).
Raw HTML is removed, not passed through, so untrusted content stays inert.

## Stats report

`/stats` serves the repository activity report described in the [stats guide](stats.md), rendered from the cache on each request.
The Stats button in the top bar links there; the URL accepts the same filters as the `stats` command flags:

```bash
curl -s "http://127.0.0.1:8080/stats"
curl -s "http://127.0.0.1:8080/stats?repo=github.com/octocat/hello-world&state=merged&from=2026-01-01&to=2026-03-31"
curl -s "http://127.0.0.1:8080/stats?list-prs=true&include-bots=true&top=10"
```

| Parameter | Meaning |
| --- | --- |
| `repo` | `host/owner/repo` to report on; repeat for several repositories, omit for all cached ones |
| `from`, `to` | Period boundaries (`YYYY-MM-DD` or RFC3339); `to` is inclusive |
| `state` | `open`, `closed`, `merged`, or `all` (default `all`) |
| `author`, `reviewer` | Restrict to these logins; repeatable |
| `include-bots` | `true` or `1` to count bot accounts as reviewers |
| `list-prs` | `true` or `1` to append the table of individual pull requests |
| `top` | Number of pull requests per notable-PRs list (default `5`) |

An invalid state, date, or `top` value answers `400`; a repository that is not cached fails with `500`.

## Frontend note

The UI is built with Vue 3 and styled with daisyUI (Tailwind CSS), both loaded from CDNs on first page load, so serving the UI requires internet access.
The JSON API under `/api/` works without the frontend and without internet access, which makes it usable for scripts.

Example API calls:

```bash
curl -s http://127.0.0.1:8080/api/repos
curl -s "http://127.0.0.1:8080/api/repos/github.com/octocat/hello-world/issues?state=all"
curl -s http://127.0.0.1:8080/api/repos/github.com/octocat/hello-world/issues/42
curl -s "http://127.0.0.1:8080/api/search?q=login&scope=all"
curl -X POST http://127.0.0.1:8080/api/repos/github.com/octocat/hello-world/refresh
```

## Testing with the mock server

Combine `mock serve` with a throwaway cache directory to try the UI on synthetic data:

```bash
ghx mock serve --repos octocat/hello-world
ghx --api-url http://127.0.0.1:PORT --cache-dir /tmp/mock-cache --repo octocat/hello-world cache
ghx serve --cache-dir /tmp/mock-cache --host 127.0.0.1 --port 8080
```

## Security

The server binds to a loopback address by default and exposes cached data plus the refresh action.
The JSON-RPC endpoint at `/rpc` also exposes `cli.run` and `repo.delete` by default, which can change the cache and GitHub.
Because of that, binding a non-loopback address requires `--rpc-token` (or `GHX_RPC_TOKEN`), which JSON-RPC clients then send as a bearer token.
The web UI itself has no authentication, so do not expose it beyond your machine; if you must listen on a public interface, put your own protection in front of it.

The JSON-RPC API is documented in the [JSON-RPC guide](json-rpc.md).
