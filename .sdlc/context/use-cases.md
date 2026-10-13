<!-- session_link: http://localhost:10000/?session=ses_ecc7bc0a9ffe3UBlAhXU5o6k4Y -->
# Use Cases

Use cases describe what people and systems do with ghx.
Each entry names the actor and the goal, and how ghx serves it today.
Entries under "Wanted" are not supported yet.

## Supported

| ID | Use case | Actor | Description |
|---|---|---|---|
| UC-1 | Agentic browsing | AI agent, automation | Search issues and pull requests and read item details (body, labels, assignees, comments) as structured JSON, repeatedly and cheaply, to drive agent workflows. |
| UC-2 | Offline and low-quota browsing | Developer, AI agent | List, filter, and view issues, PRs, and comments from the local cache with no network calls while it is fresh, staying within GitHub API rate limits. |
| UC-3 | Fast repeated queries on one repository | Developer | Run many list, filter, and view queries against the same repository without re-fetching; delta and windowed refresh keep the cache current. |
| UC-4 | Backlog triage and filtering | Developer, maintainer | Filter issues and PRs in-memory by state, author, assignee, label, milestone, mention, app, base/head branch, and draft, plus free-text search, with indexed SQLite predicates on large repositories. |
| UC-5 | Scripting and pipelines | Developer, automation | Consume JSON output with gh-compatible field selection and branch on structured results. |
| UC-6 | Cache inventory | Developer, AI agent | See which repositories are cached, with issue/PR counts, age, and freshness, to decide what to refresh. |
| UC-7 | Pull request review from the CLI | Reviewer | Post top-level, inline, line-range, and file-level review comments and reply to existing threads without leaving the terminal. |
| UC-8 | Draft and batch review comments | Reviewer, AI agent | Compose comments into a pending review or a local stash, then submit, pop, or drop them later; a stash records comments without calling the API. |
| UC-9 | Inspect review threads | Reviewer, AI agent | List open or resolved review threads, inspect one thread, and surface comment IDs for edit or delete. |
| UC-10 | Maintain comments | Comment author | Edit or delete pull request and issue comments. |
| UC-11 | Comment on issues | Anyone with repository access | Post and maintain comments on issues. |
| UC-12 | Browse the cache in a web UI | Developer | Browse cached issues and PRs in a GitHub-like local UI with a repository switcher, list and detail views, and a Ctrl+P search palette. |
| UC-13 | Report on pull request activity | Engineering manager, maintainer | Generate an HTML report of lead times, review engagement, PR sizes, and trends for one or more repositories over a date range. |
| UC-14 | Inspect local performance | Maintainer | Record API and cache timings, review p50/p95 and retry rates by operation kind, and export or clear events, all locally. |
| UC-15 | Benchmark against gh | Maintainer | Time equivalent read commands for ghx and gh across warm, cold, and forced-refresh scenarios, and compare storage backends. |
| UC-16 | Develop and test without GitHub | Contributor, CI | Run a mock GraphQL server with a scenario builder and simulation generator, optionally injecting recorded latencies, for tests and offline development. |
| UC-17 | Target GitHub Enterprise Server | Enterprise developer | Point ghx at a self-hosted GitHub instance with the `[HOST/]OWNER/REPO` syntax. |

## Wanted

| ID | Use case | Actor | Description | Signal |
|---|---|---|---|---|
| UC-18 | Read review threads offline | AI agent, reviewer | Serve inline pull request review comments (review threads) from the cache so whole review discussions can be read without API calls. | Issue #15 |

## Out of scope

Documented as out of scope, so not wanted at present:

- Creating, updating, or deleting issues and pull requests (ghx is read-only except for comments).
- Real-time streaming or webhook-based updates.
- Authentication management (delegates to `GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth`).
- Caching data beyond issues and pull requests, for example actions, releases, and discussions.
