# Vocabulary

## Domain Terms

| Term | Definition |
|---|---|
| Issue | A GitHub issue tracked in a repository |
| Pull Request (PR) | A GitHub pull request proposing changes to a repository |
| Comment | A comment on an issue or pull request |
| Label | A coloured tag applied to issues and PRs for categorisation |
| Milestone | A named grouping of issues/PRs tied to a target version or deadline |
| Actor | A GitHub user, bot, or app that creates content (issues, comments, etc.) |
| Review Decision | The aggregate review status of a PR: APPROVED, CHANGES_REQUESTED, or REVIEW_REQUIRED |
| Draft PR | A pull request that is not yet ready for review |
| Assignee | A GitHub user assigned to an issue or PR |
| Mention | A GitHub user referenced in an issue or PR body or comment |
| App | A GitHub App (bot) that authored an issue, PR, or comment |

## Technical Terms

| Term | Definition |
|---|---|
| Cache freshness | Whether the cache is within its configured duration (default 60 minutes) |
| Delta fetch | Fetching only items updated since the last cache timestamp |
| Full fetch | Fetching all items regardless of cache state, triggered by `--force` |
| Cache info | Metadata file (`.cache_info.json`) tracking when the cache was last populated and its validity duration |
| GraphQL | The query language used exclusively to communicate with the GitHub API |
| Cursor | A pagination token used by GitHub's GraphQL API to fetch subsequent pages |
| GHE | GitHub Enterprise Server, a self-hosted GitHub instance |
| httptest.Server | Go's `net/http/httptest` package used by the mock server for in-process HTTP testing |
| Scenario | An in-memory collection of mock issues and PRs served by the mock server |
| Scenario builder | A fluent API for constructing test scenarios with hand-crafted data |
| Simulation | A parameterized generator that produces realistic bulk test data from a config |
| Activity burst | A high-activity window in the simulation timeline (e.g. hackathon, release sprint) |
| Time advance | A scenario builder mode that processes events chronologically to simulate evolving activity |
| Endpoint URL | The GraphQL API URL, auto-detected from the host or overridden via `--api-url` |
| Token resolution | The chain used to find a GitHub token: GH_TOKEN > GITHUB_TOKEN > gh auth token |
| Authoritative cache | When the full cache is fresh, `view` commands serve from it; an item missing from it is fetched and cached instead of erroring |

## Acronyms

| Acronym | Expansion |
|---|---|
| PR | Pull Request |
| GHE | GitHub Enterprise Server |
| CI | Continuous Integration |
| CLI | Command-Line Interface |
| API | Application Programming Interface |
| JSON | JavaScript Object Notation |
| RNG | Random Number Generator |
