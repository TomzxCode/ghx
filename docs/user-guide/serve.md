# Serve (web UI)

The `serve` command starts a local web server that browses the ghx cache in your browser.
It lists every cached repository, shows issue and pull request lists and details in a GitHub-like layout, and includes a Ctrl+P search palette.

## Start the server

```bash
ghx serve
```

The server prints its URL (default `http://127.0.0.1:8080`) and serves until you stop it with Ctrl+C.

```bash
ghx serve --addr 127.0.0.1:9000 --open
```

`--addr` overrides the listen address and `--open` opens your browser automatically.

## Using the UI

- **Repository switcher**: the dropdown in the top bar lists all cached repositories (as produced by `ghx repo list`) with their issue and PR counts.
- **List view**: tabs for Issues and Pull requests with open/closed/all filters (merged for PRs) and a text filter over titles.
- **Detail view**: click a row to see the state badge, metadata (author, dates, labels, assignees, milestone, branches), the rendered markdown body, and the comment timeline.
- **Search palette**: press <kbd>Ctrl</kbd>+<kbd>P</kbd> (or <kbd>Cmd</kbd>+<kbd>P</kbd>, or `/`) to open the palette.
  Type to fuzzy-search issues and PRs by number or title, use <kbd>↑</kbd>/<kbd>↓</kbd> and <kbd>Enter</kbd> to jump, and <kbd>Tab</kbd> to switch the search scope between the current repository and all cached repositories.
- **Theme**: the sun/moon button in the top bar switches between light and dark themes.
  The choice is remembered in your browser; on first visit the UI follows your system preference.
- **Refresh**: the refresh button in the top bar fetches new issues and PRs for the current repository using the same resumable logic as `ghx cache`.

The UI is read-only against the cache except for the explicit refresh action.

## Loading data

The UI serves whatever is in the cache.
If nothing is cached yet, run the cache command first:

```bash
ghx cache
```

## Markdown rendering

Issue bodies, PR bodies, and comments are rendered from markdown to HTML on the server (GitHub-flavoured tables, task lists, strikethrough, and autolinks).
Raw HTML is removed, not passed through, so untrusted content stays inert.

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
ghx serve --cache-dir /tmp/mock-cache --addr 127.0.0.1:8080
```

## Security

The server binds to a loopback address by default and exposes cached data plus the refresh action.
It has no authentication, so do not expose it beyond your machine; if you must listen on a public interface, put your own protection in front of it.
