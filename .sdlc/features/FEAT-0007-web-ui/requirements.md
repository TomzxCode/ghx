---
title: "Web UI (serve command)"
status: draft
---

# Requirements: Web UI (serve command)

## Overview

Provides a `ghx serve` CLI command that starts a local web server exposing a GitHub-like UI over the on-disk cache.
The UI lists and views issues and pull requests for all cached repositories, renders markdown bodies and comments, offers a Ctrl+P fuzzy palette for navigation, and includes a cache refresh action that reuses the `cache` command's fetch logic.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| Developer | Browse cached issues and PRs in a browser instead of paging through `issue list`/`pr view` output |
| AI agent | Read cached data programmatically via the serve JSON API |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide a `ghx serve` CLI command that starts an HTTP server on a loopback address and prints its URL |
| FR-02 | Must | The serve UI shall list all cached repositories (via the cache store's repository listing) and allow switching between them |
| FR-03 | Must | The serve UI shall display a list of issues and a list of pull requests for the selected repository with number, title, state, labels, author, comment count, and updated date |
| FR-04 | Must | The serve UI shall provide a detail view for each issue and pull request showing state badge, metadata (author, dates, labels, assignees, milestone), the rendered markdown body, and the comment thread |
| FR-05 | Must | The serve UI shall filter issue and PR lists by state (open, closed, all; merged for PRs) and by a free-text query |
| FR-06 | Must | The serve UI shall provide a Ctrl+P (Cmd+P on macOS) palette that fuzzy-searches issues and PRs by number and title and jumps to the selected item |
| FR-07 | Must | The serve API shall render issue and PR bodies and comments from markdown to HTML using GFM extensions (tables, task lists, strikethrough, autolinks) without allowing raw HTML injection |
| FR-08 | Must | The serve UI shall provide a refresh action that fetches new issues and PRs for the selected repository using the same resumable fetch logic as the `cache` command |
| FR-09 | Must | All ghx-owned UI assets (HTML, CSS, JS) shall be embedded in the binary via `go:embed`; the frontend framework (Vue 3) and CSS framework (daisyUI) load from CDN |
| FR-10 | Should | The `serve` command shall accept `--addr` to override the listen address and `--open` to open the browser automatically |
| FR-11 | Should | The palette shall support jumping across all cached repositories, not only the currently selected one |
| FR-12 | Should | The serve API shall support machine-readable JSON responses for repos, issue/PR lists, issue/PR details, and search |
| FR-13 | May | The UI shall show a friendly empty state with `ghx cache` instructions when no repositories are cached |
| FR-14 | Should | The UI shall let the user switch between light and dark themes, remember the choice across visits, and follow the system preference on first load |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Security | The server shall bind to a loopback address by default and shall remove raw HTML from rendered markdown |
| NFR-02 | Must | Reliability | Concurrent refresh requests for the same repository shall not run in parallel; a second request receives an explicit conflict response |
| NFR-03 | Must | Consistency | The refresh action shall reuse the `cache` command's fetch and resume cursor logic rather than duplicating it |
| NFR-04 | Should | Performance | Issue and PR list responses shall render from cache without network access |
| NFR-05 | Should | Portability | The frontend framework (Vue 3) and CSS framework (daisyUI) shall load via CDN with no build step; ghx's own assets remain embedded |

## Constraints

- The UI is read-only against the cache except for the explicit refresh action
- The frontend framework (Vue 3) and CSS framework (daisyUI) are loaded from a CDN, so the first page load requires internet access
- Markdown rendering happens server-side with goldmark; the frontend renders pre-rendered HTML

## Acceptance Criteria

- [ ] FR-01: `ghx serve` starts a server, prints its URL, and shuts down cleanly on SIGINT/SIGTERM
- [ ] FR-02: All repositories found by the cache store appear in the repo dropdown
- [ ] FR-03: Selecting a repository and tab shows the correct issue/PR rows with all listed columns
- [ ] FR-04: Opening an item shows the state badge, metadata, rendered markdown body, and comments
- [ ] FR-05: State filter and free-text query narrow the visible rows
- [ ] FR-06: Ctrl+P opens the palette, typing narrows matches, Enter jumps to the item view
- [ ] FR-07: Markdown bodies render with GFM features; a `<script>` tag in a body is removed
- [ ] FR-08: The refresh action fetches from GitHub (or a mock server) and updates the cached rows
- [ ] FR-09: The binary serves the UI with no external ghx-owned asset files
- [ ] FR-10: `--addr` and `--open` behave as documented
- [ ] FR-12: `/api/repos` and the list/detail endpoints return valid JSON

## Open Questions

1. Should the palette support searching inside bodies (not just titles) at a later stage?
