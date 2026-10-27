# Vocabulary

## Domain Terms

| Term | Definition |
|---|---|
| Actor | A GitHub user, bot, or app that creates content such as issues, PRs, comments, or reviews |
| Aging list | The process report's list of every open issue and open PR still on the line, longest waiting first |
| App | A GitHub App (bot) that authored an issue, PR, or comment |
| Assignee | A GitHub user assigned to an issue or PR |
| Comment | A comment on an issue or pull request |
| Cumulative flow diagram | A process report chart showing one stacked band per active station per day |
| Cycle time | The process report's duration from item creation to its completion station (close for issues, merge or discard for PRs) |
| Draft PR | A pull request that is not yet ready for review |
| Flip | An observed draft/ready transition in a PR's move history |
| Issue | A GitHub issue tracked in a repository |
| Label | A coloured tag applied to issues and PRs for categorisation |
| Lead time | The stats report's merged-PR metric: time to merge, with median time in draft, time to first review, time to approve, and approval-to-merge segments |
| Little's law | The queueing relation the process report uses to derive the implied cycle time from average work in process and throughput |
| Mention | A GitHub user referenced in an issue, PR, or comment body |
| Milestone | A named grouping of issues and PRs tied to a target version or deadline |
| Monte Carlo forecast | The process report's simulation of when the currently queued items will clear, based on sampled throughput |
| Pending review | A review started on a PR but not yet submitted; ghx can create, list, and submit pending reviews |
| Production line | The process report's model of a repository's work as two lines (issues and PRs) of stations |
| Pull Request (PR) | A GitHub pull request proposing changes to a repository |
| Queue age | How long the items currently sitting in a station have been waiting there |
| Review comment | A comment anchored to a file and line range of a PR diff |
| Review Decision | The aggregate review status of a PR: APPROVED, CHANGES_REQUESTED, or REVIEW_REQUIRED |
| Review thread | A chain of review comments on one region of a PR diff, which can be resolved or reopened |
| Reviewer engagement | The stats report section aggregating each reviewer's review and comment activity |
| Station | A stage of a production line (e.g. draft, awaiting_review, awaiting_merge, merged) where items queue |
| Throughput | The rate at which items complete a line, shown in the process report as weekly arrivals versus completions |
| Unsplit bucket | The wait-share bucket holding completed PRs whose draft/ready split is unknown (no recorded flip events), reported as one unsplit duration instead of per-station waits |
| Wait share | Each station's fraction of the completed items' total cycle time on a line |
| Work in process (WIP) | The unfinished items on a line; the process report samples station inventory weekly |

## Technical Terms

| Term | Definition |
|---|---|
| Activity burst | A high-activity window in the simulation timeline (e.g. hackathon, release sprint) |
| Cache backfill | The read path (list, view) writing full records (comments, reviews, timeline) for items missing from the cache without advancing its freshness timestamps |
| Cache hit rate | The per-repository, per-operation rate at which read commands served data from the cache instead of falling back to the GitHub API |
| Cache info | Per-repository cache metadata recording when issues and PRs were last populated (`.cache_info.json` in the file backend) |
| Cache migrate | The non-destructive, idempotent copy of an existing file cache into the SQLite backend |
| Canonical repository key | The lower-case host/owner/repo coordinates every cache row is stored and looked up under |
| Cursor | A pagination token used by GitHub's GraphQL API to fetch subsequent pages |
| Delta refresh | Refreshing only the items created or updated since the last cache write, used by `ghx cache` and `--since` |
| Endpoint URL | The GraphQL API URL, auto-detected from the host (Enterprise hosts resolve to `https://<host>/api/graphql`) or overridden with `--api-url` |
| Freshness window | The duration (default 60 minutes) for which a cache write is considered fresh for reads |
| Full fetch | Fetching all items regardless of cache state, triggered by `--force` |
| GraphQL | The query language ghx uses for all GitHub API access: reads in `internal/github`, writes (comments, reviews) in `internal/gh` |
| Host prefix | The optional leading host in `--repo [HOST/]OWNER/REPO` that selects a GitHub Enterprise instance instead of github.com |
| httptest.Server | Go's `net/http/httptest` package used by the mock server for in-process HTTP testing |
| JSON-RPC 2.0 server | The typed rpc server exposing `repo.list`, `repo.delete`, `issue.list`, `issue.view`, `pr.list`, `pr.view`, `pr.threads`, `cache.refresh`, `rpc.discover`, `server.info`, and `cli.run` |
| Latency model | The mock server's per-kind latency and payload-size model fitted from recorded telemetry events |
| Log writer | The stderr writer that clears and redraws the active progress bar so log lines never interleave with it |
| Mock server | The local GraphQL server (`ghx mock serve`) serving in-memory scenarios for testing against |
| Per-type freshness | Separate freshness timestamps for issues (`issues_cached_at`) and PRs (`prs_cached_at`), so one type can refresh without the other |
| Remote mode | Running any ghx command against a remote ghx server with `--remote` |
| Retry rate | The share of telemetry events for an operation kind that needed more than one attempt, reported by `ghx telemetry` |
| Scenario | An in-memory collection of mock issues and PRs served by the mock server |
| Scenario builder | A fluent API for constructing test scenarios with hand-crafted data |
| Search palette | The web UI's palette search across one repository's cached issues and PRs |
| Simulation | A parameterized generator that produces realistic bulk test data from a config |
| Stats report | The `ghx stats` report covering lead time, time in draft, time to first review, time to approve, reviewer engagement, and trends |
| Storage backend | The cache store selected by `--storage`: `sqlite` (default, a single `cache.db`) or `file` (one JSON file per issue/PR) |
| Telemetry event kind | The label grouping telemetry events for analysis (e.g. `pr_full_page`, `issue_list`, `cache_hit`) |
| Time advance | A scenario builder mode that processes events chronologically to simulate evolving activity |
| Token resolution | The chain used to find a GitHub token: GH_TOKEN > GITHUB_TOKEN > gh auth token (host-specific via `gh auth token --hostname` for Enterprise) |
| Web UI | The serve command's local web UI with the search palette, the `/stats` and `/process` report pages, and the `/api/process` JSON endpoint |

## Acronyms and Abbreviations

| Abbreviation | Expansion |
|---|---|
| API | Application Programming Interface |
| CI | Continuous Integration |
| CLI | Command-Line Interface |
| GHES | GitHub Enterprise Server |
| HTML | HyperText Markup Language |
| JSON | JavaScript Object Notation |
| PR | Pull Request |
| RNG | Random Number Generator |
| RPC | Remote Procedure Call |
| SVG | Scalable Vector Graphics |
| WIP | Work In Process |
