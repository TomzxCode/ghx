---
title: "Web UI (serve command)"
status: done
---

# Specification: Web UI (serve command)

## Overview

The web UI is implemented in `internal/webui/` as a plain `net/http` server with a JSON API and `go:embed`-ed frontend assets.
The `ghx serve` CLI command in `cmd/serve.go` resolves the cache store, starts the server, and handles shutdown.
The frontend is a Vue 3 single-page app (loaded from CDN, no build step) styled with daisyUI 5 (also via CDN), with hand-rolled hash routing, a GitHub-style list and detail view, and a Ctrl+P palette.

## Architecture

```
ghx serve ──► runServe()
                  │
                  ├─ store := newStore() ──► cache.Store (file backend)
                  │
                  ├─ webui.New(store, refreshFn) ──► *webui.Server
                  │         │
                  │         ├─ GET  /                 ──► index.html (embedded)
                  │         ├─ GET  /assets/*         ──► embedded FS (app.js, style.css)
                  │         ├─ GET  /api/repos        ──► ListCachedRepos + counts
                  │         ├─ GET  /api/repos/{host}/{owner}/{repo}/issues?state=&q=&sort=&order=
                  │         ├─ GET  /api/repos/{host}/{owner}/{repo}/prs?state=&q=&sort=&order=
                  │         ├─ GET  /api/repos/{host}/{owner}/{repo}/issues/{n}
                  │         ├─ GET  /api/repos/{host}/{owner}/{repo}/prs/{n}
                  │         ├─ POST /api/repos/{host}/{owner}/{repo}/refresh
                  │         └─ GET  /api/search?q=&scope=
                  │
                  └─ http.Server.ListenAndServe + signal-based shutdown
```

The refresh handler calls back into the shared fetch logic extracted from `cmd/cache.go`, so `ghx cache` and the UI refresh button use one implementation of the resumable/delta fetch.

## Data Models

### RepoSummary (GET /api/repos entries)

| Field | Type | Description |
|---|---|---|
| host | string | GitHub host (e.g. `github.com`) |
| owner | string | Repository owner |
| repo | string | Repository name |
| issueCount | int | Number of cached issues |
| prCount | int | Number of cached PRs |
| cachedAt | string/null | RFC3339 timestamp from cache info, null when absent |
| complete | bool | Whether the last fetch completed |

### ItemSummary (list entries)

| Field | Type | Description |
|---|---|---|
| number | int | Issue or PR number |
| title | string | Title |
| state | string | `open`, `closed`, or `merged` (PRs) |
| isDraft | bool | PR draft flag (issues always false) |
| kind | string | `issue` or `pr` |
| labels | array | `{name, color}` entries |
| author | string | Author login |
| commentCount | int | Comment count |
| createdAt | string | RFC3339 creation timestamp |
| updatedAt | string | RFC3339 timestamp |
| url | string | GitHub URL |

### ItemDetail

| Field | Type | Description |
|---|---|---|
| summary | ItemSummary | As above |
| bodyHTML | string | Goldmark-rendered body HTML |
| assignees | array | Actor logins |
| milestone | object/null | `{number, title}` |
| createdAt | string | RFC3339 timestamp |
| closedAt/mergedAt | string/null | RFC3339 timestamps when set |
| baseRefName/headRefName | string | PR branch names (empty for issues) |
| reviewers | array | PR only: `{login, name, state}` entries (`login` for people and bots, `name` for teams), where state is `approved`, `changes_requested`, `commented`, `dismissed`, or `pending`; omitted for issues and PRs with no reviewers |
| comments | array | `{author, createdAt, bodyHTML, url}` entries |
| timeline | array | `{kind, actor, detail, at, count}` entries, oldest first; kind is `opened`, `commented`, `closed`, `merged`, `review`, `ready_for_review`, `converted_to_draft`, or `review_requested`; `count` is present only when the entry stands for several consecutive identical events |

Reviewers combine the PR's cached reviews with its current review requests (`reviewRequests`, including teams): everyone who submitted a review and everyone currently requested appears once, and the PR author is never listed. State is the latest non-comment review (a later comment-only review does not hide an approval), `commented` when they only left comment reviews, and `pending` for anyone in the current review requests (including a re-request). A cache row predating `reviewRequests` falls back to the timeline's user-only review-request events, where teams and request removals are not visible.

The timeline is assembled from the cached record: the opening event, every comment, every submitted review (with `detail` set to its lowercase state), the PR's tracked draft/ready and review-request events, and the merge or close. Merging closes a pull request too, so a merged PR carries a `merged` event and no `closed` event. `detail` carries the requested reviewer's login for `review_requested`. Events with no recorded time are skipped, and ties keep insertion order (the opening event first).

After sorting, runs of consecutive events with the same kind, actor, and detail are condensed into the first event of the run, with `count` set to the number of events it stands for (the UI appends `(xN)`). A different actor, kind, or detail starts a new entry, so a later approval is never folded into an earlier comment review. The condensed entry keeps the first event's timestamp.

## API Contracts

### GET /api/repos

Returns `{"repos": [RepoSummary...]}` sorted by owner then repo name.

### GET /api/repos/{host}/{owner}/{repo}/issues|prs?state=&q=&sort=&order=

- `state`: `open` (default), `closed`, `all`; PRs additionally accept `merged`
- `q`: case-insensitive substring match on title and body
- `sort`: `updated` (default), `created`, `number`, `comments`, or `title`
- `order`: `asc` or `desc`; defaults to the sort key's natural direction (descending for every key except `title`, which is ascending)
- Returns `{"items": [ItemSummary...]}` sorted by the requested key and direction, defaulting to `updatedAt` descending; ties fall back to the higher number, then the newer update
- An unknown `sort` or `order` returns 400
- Unknown repository returns 404

### GET /api/repos/{host}/{owner}/{repo}/issues|prs/{number}

Returns `ItemDetail` as JSON; 404 when the item is not cached.

### POST /api/repos/{host}/{owner}/{repo}/refresh

- Triggers a forced (resumable) fetch of issues and PRs for the repository
- Returns 200 with `{"issues": n, "prs": m}` on success
- Returns 409 while another refresh for the same repository is in flight
- Requires a GitHub client (honours `--api-url` for testing)

### GET /api/search?q=&scope=

- `scope`: `repo` (default, current repo) or `all`
- Returns `{"items": [ItemSummary + repo fields...]}` for palette consumption; titles only, capped results

## Frontend

### Routes (hash-based)

| Route | View |
|---|---|
| `#/` | Empty state or first cached repo |
| `#/{owner}/{repo}` | List view (tab, filter, sort, and grouping in query: `#/owner/repo?tab=issues&state=open&sort=title&order=asc&group=state`) |
| `#/{owner}/{repo}/issues/{n}` | Issue detail |
| `#/{owner}/{repo}/pulls/{n}` | PR detail |

### Components

| Component | Responsibility |
|---|---|
| Root app | Reactive store: repos, current repo, tab, state filter, sort, order, grouping, items, detail, theme, refreshing map; hash parsing |
| RepoSelect | Dropdown of cached repositories with issue/PR counts |
| ItemList | GitHub-style rows: state icon (open green, closed purple for issues, merged purple, draft grey for PRs), number, title, colored label chips, author, comment count, updated date (creation date when sorting by creation). A toolbar sets the state filter, sort key and direction, grouping, and the title filter. Grouping buckets rows by state, author, or label with collapsible header rows that show a count; label grouping lists an item once per label it carries |
| ItemDetail | State badge, title, meta line, horizontal event timeline with an absolute/relative time toggle (persisted), sidebar (reviewers for PRs, labels, assignees, milestone, GitHub link), rendered markdown body, comment list |
| ThemeToggle | Switches daisyUI's `data-theme` between light and dark; persists to localStorage; defaults to the system preference |
| Palette | Overlay triggered by Ctrl+P/Cmd+P or `/`; fuzzy filter over `#N title` entries; arrow keys + Enter to jump, Esc to close |

### Fuzzy matching

Palette matching scores entries by: exact number match, prefix match on title words, and subsequence match, in that order of precedence; ties break by `updatedAt` descending.

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Framework | Vue 3 via CDN (`vue.global.prod.js`, pinned major) | No build step, reactive state for list/detail/palette, user preference |
| CSS | daisyUI 5 via CDN (`daisyui.css`) plus the Tailwind v4 browser build for utility classes | Component and utility classes without a build step; ghx-owned `style.css` is a small override layer (markdown typography, navbar helpers, palette rows) |
| Router | Hand-rolled hash parsing | Only 3 route shapes; avoids a second CDN dependency |
| Markdown | goldmark with GFM extensions, raw HTML disabled | Server-side rendering keeps the frontend dumb; safe by default |
| Assets | `go:embed` for ghx-owned files | Single-binary distribution; only Vue, daisyUI, and the Tailwind browser build load from CDN |
| Refresh | Shared fetch function with per-repo mutex | One implementation of resume logic (NFR-03); prevents parallel fetch clobbering (NFR-02) |
| List sorting | Server-side `sort` and `order` parameters | Keeps ordering in the documented API contract, so the UI and other API consumers share one implementation |
| List grouping | Client-side buckets over the sorted list | Presentation-only layout; label grouping needs one item in several buckets |
| Binding | Loopback by default | The API exposes cached data and a mutation trigger; no auth layer is planned |

## Risks and Unknowns

1. CDN availability at first page load; mitigated by an inline "framework failed to load" message and a JSON API that works without the UI
2. Refresh against the real API is slow for large repositories; the request is long-running by design and guarded against concurrency
3. The Tailwind browser build generates utility classes at runtime, so there can be a brief unstyled flash on first load

## Out of Scope

- Creating, editing, or commenting on issues and PRs from the UI
- GitHub search syntax in the palette (substring/fuzzy matching only)
- Live updates (websockets or polling)
- Authentication or TLS on the serve endpoint
