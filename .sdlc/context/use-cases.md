<!-- session_link: http://localhost:10000/?session=ses_ecc7bc0a9ffe3UBlAhXU5o6k4Y -->
# Use Cases

Use cases describe why actors use ghx.
Each entry names the actor, the goal, and how ghx serves it today.
Entries under "Wanted" are not supported yet.

## Supported

| ID | Use case | Actor | Description |
|---|---|---|---|
| UC-1 | Feed an agent workflow with repository data | AI agent, automation | Search issues and pull requests and read item details (body, labels, assignees, comments) as structured JSON, repeatedly and cheaply, without spending API calls on every lookup. |
| UC-2 | Keep browsing when GitHub is unreachable or rate-limited | Developer, AI agent | List, filter, and view issues, PRs, and comments from the local cache with no network calls while it is fresh, on github.com or a self-hosted instance. |
| UC-3 | Re-check one repository without fetching everything again | Developer | Repeated list, filter, and view queries serve from the cache, and delta or windowed refresh keeps it current. |
| UC-4 | Work through a large issue and pull request backlog | Developer, maintainer | Filter issues and PRs in-memory by state, author, assignee, label, milestone, mention, app, branch, and draft, plus free-text search, with indexed predicates on large repositories. |
| UC-5 | Script against issues and pull requests | Developer, automation | Consume JSON output with gh-compatible field selection and branch on structured results. |
| UC-6 | Review a pull request from the terminal | Reviewer, AI agent | Inspect review threads and post inline, line-range, and file-level comments or replies, composing them in a pending review or a local stash before posting. |
| UC-7 | Take part in a discussion | Comment author | Post comments on issues and pull requests, and edit or delete them afterwards. |
| UC-8 | Browse issues and pull requests in a local UI | Developer | A GitHub-like local UI with a repository switcher, list and detail views, and a Ctrl+P search palette. |
| UC-9 | Understand pull request activity and review load | Engineering manager, maintainer | An HTML report of lead times, review engagement, PR sizes, and trends for one or more repositories over a date range. |
| UC-10 | Find out where ghx spends time | Maintainer | Local recording of API and cache timings, with p50/p95 and retry rates by operation kind, exportable or clearable. |
| UC-11 | Compare ghx against gh and across storage backends | Maintainer | Timing of equivalent read commands across warm, cold, and forced-refresh scenarios, plus a backend comparison. |
| UC-12 | Develop and test without hitting GitHub | Contributor, CI | A mock GraphQL server with a scenario builder and simulation generator, optionally injecting recorded latencies. |

## Wanted

| ID | Use case | Actor | Description | Signal |
|---|---|---|---|---|
| UC-13 | Read review threads offline | AI agent, reviewer | Serve inline pull request review comments (review threads) from the cache so whole review discussions can be read without API calls. | Issue #15 |

## Out of scope

Documented as out of scope, so not wanted at present:

- Creating, updating, or deleting issues and pull requests (ghx is read-only except for comments).
- Real-time streaming or webhook-based updates.
- Authentication management (delegates to `GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth`).
- Caching data beyond issues and pull requests, for example actions, releases, and discussions.
