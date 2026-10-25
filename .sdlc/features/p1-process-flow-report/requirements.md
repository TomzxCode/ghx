---
title: "Process Flow Report"
status: draft
---

# Requirements: Process Flow Report

## Overview

Adds a production-line report of issue and PR flow, modeled on the process
view used by the agentic project's SDLC monitoring: work items sit in
stations, move between them, and the report answers how much work sits where,
how long it takes to flow through, and whether the queues are growing. The
computation is a shared internal package exposed twice: a `ghx process` HTML
report command and `ghx serve` endpoints (`/process` page and `/api/process`
JSON).

## Stakeholders

| Stakeholder | Interest |
|---|---|
| Developer / maintainer | See where issues and PRs pile up, how long they take to flow, and whether queues are growing |
| AI agents / automation | Read the flow numbers as JSON from the serve endpoint without rendering HTML |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall map issues onto a line of stations (Open, Closed) and pull requests onto a line (Draft, Ready, Merged, Discarded), with closed-unmerged PRs counting as discarded rather than completed |
| FR-02 | Must | The system shall report each station's inventory (count) and the waiting time of the items currently there (oldest, median, newest) |
| FR-03 | Must | The system shall report, per line, cycle time from creation to the completion station (close for issues, merge for PRs) as a distribution (mean, median, p10, p90, fastest, slowest) over the window's completions, with a fixed hour-bucket histogram |
| FR-04 | Must | The system shall report weekly arrivals and completions per line, anchored at the window start, plus total arrivals and completions |
| FR-05 | Must | The system shall report Little's law per line: average work in process (time integral over active stations), throughput (completions per day), and the implied cycle time |
| FR-06 | Must | The system shall report queue pressure per active station: arrivals, departures, net flow, a filling/draining/steady trend, and a weekly WIP sparkline |
| FR-07 | Must | The system shall report a cumulative flow diagram: one daily sample per active station inventory plus cumulative completions |
| FR-08 | Must | The system shall report an aging work-in-progress list of every open issue and open PR, longest wait first, with item id, title, URL, station, and age |
| FR-09 | Must | The system shall report a Monte Carlo forecast per line of when the items currently on the line clear, resampling the window's weekly completion counts (500 simulations, deterministic seed), with p50/p85 as weeks and dates |
| FR-10 | Must | The system shall report where the window's arrivals stand now per line (done, discarded, still active) |
| FR-11 | Must | The system shall report where completed PR cycle time went as station wait shares (draft versus ready), computed from cached draft/ready timeline events |
| FR-12 | Must | The system shall accept `[HOST/]OWNER/REPO]` arguments (default current repository or --repo), a `--days` window (1-730, default 90), and write a self-contained HTML report (inline CSS/SVG, no external assets) to `--output` |
| FR-13 | Must | The system shall serve the report from the cache, warning on stale cache and falling back to the GitHub API when nothing is cached (CLI command), matching the stats loader |
| FR-14 | Must | The system shall serve the same report in the web UI at `/process` (cache-only, repo and days query params, repeatable repo, omit repo for all cached repositories) |
| FR-15 | Must | The system shall expose the report as JSON at `/api/process` with the same parameters, computed cache-only |
| FR-16 | Must | The timeline walk shall correct missing history: an item sits in its current station since its last known move, and a recorded finish time participates as the final move |
| FR-17 | Should | The rendered aging list shall cap its table (showing the oldest items and a note about the remainder) while the JSON keeps every item |
| FR-18 | Should | A finished item whose finish time GitHub did not record shall count toward inventory and outcomes but not toward cycle times or throughput |
| FR-19 | May | The report shall include the outcomes breakdown per line beyond the Must sections (created/done/discarded/active) |
| FR-20 | Must | The serve page and `/api/process` shall accept optional `from`/`to` bounds (YYYY-MM-DD or RFC3339, `to` inclusive): when either is set the report covers that explicit range instead of the trailing days window, an empty `from` starts at the first item, an empty `to` ends now, and a `to` before `from` is an error |
| FR-21 | Must | The served page's period form shall expose From and To date inputs alongside the days shortcut, pre-filled with the covered bounds, and preserve the requested repositories on resubmit |

## Non-Functional Requirements

| ID | Priority | Requirement |
|---|---|---|
| NFR-01 | Must | The flow computation shall be a pure internal package taking items plus a window, with no GitHub or cache dependencies, so both surfaces share one implementation |
| NFR-02 | Must | Reports shall render without network access (no CDN scripts or fonts) |
| NFR-03 | Should | The forecast shall be deterministic for identical inputs (seeded PRNG) |
