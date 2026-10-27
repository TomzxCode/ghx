---
title: "Process Flow Report"
status: done
---

# Test plan: Process Flow Report

## Scope

Tests cover the process flow report pipeline: the station mapping from cached issues and PRs (`FromIssue`, `FromPR`), the timeline walk, the pure report computation (inventory, cycle times, weekly flow, Little's law, queue pressure, station flows, daily samples, the aging list, the Monte Carlo forecast, outcomes, and explicit `BuildRange` windows), the HTML rendering (sections, window form, range meta, SVG line diagrams, aging-list cap, empty states), the cache-only serve path (`serveProcessFunc` against a real store), and the `/process` and `/api/process` HTTP handlers.
The `ghx process` CLI command itself is a thin composition of the tested loaders, `Build`, and `Render`; it has no direct test, and the serve command's routing belongs to the web UI feature.
Browser rendering of the page is not tested; the Go tests assert section markers, SVG presence, and the JSON payload, not pixels.

## Unit tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | Quantile interpolation between order statistics, empty and single samples | `Quantile` table (`internal/processflow/processflow_test.go:57`) | Interpolated quantiles; nil for an empty sample |
| TC-2 | Issue mapping: open/closed stations, case-insensitive state enums, close time kept or nil, nil input | Issues with `open`/`CLOSED`/missing close time (`internal/processflow/processflow_test.go:628`) | Station and `ExitedAt` per state; zero item for nil |
| TC-3 | PR mapping: merged, discarded, draft, ready-from-birth stations; draft→ready move recorded; nil input | PRs with `MergedAt`, closed-unmerged, `IsDraft`, no history (`internal/processflow/processflow_test.go:659`) | Station, `ExitedAt`, and moves per state; zero item for nil |
| TC-4 | Timeline walks forward, rejects duplicate/stale moves, corrects missing history to the current station | PRs with one and duplicate flips, a closed issue with no moves (`internal/processflow/processflow_test.go:76`) | Two-entry timeline; duplicate dropped; one-entry corrected timeline |
| TC-5 | A recorded exit joins the walk as the final move, so historical lookups predate it | Merged PR with a flip (`internal/processflow/processflow_test.go:98`) | Three entries; day-35 lookup says awaiting review |
| TC-6 | Station inventory counts and oldest/median/newest wait ages | Ten-item scenario built with `Build` (`internal/processflow/processflow_test.go:116`) | Per-station counts and hour ages as asserted |
| TC-7 | Cycle time distribution (mean, median, p10, p90, fastest, slowest) and fixed histogram buckets | Closed/merged items in the scenario (`internal/processflow/processflow_test.go:150`) | Distribution values and bucket counts as asserted |
| TC-8 | Review turns drive stations: approval → waiting on merge, changes requested → waiting on author | PRs with cached reviews (`internal/processflow/processflow_test.go:184`) | Moves and final stations follow the review turns |
| TC-9 | A PR with no flip history at all stays unsplit (opened directly as ready) | PR with no timeline events (`internal/processflow/processflow_test.go:234`) | Station is the state-implied one with no moves |
| TC-10 | Wait shares rank the bottleneck across draft/reviewer/author/merge stations | Completed PRs with flip history (`internal/processflow/processflow_test.go:253`) | Shares per station, summing to one, ranked as asserted |
| TC-11 | Completed PRs without flip history land in the separate unsplit bucket, keeping shares summing to one | Flipless merged PRs mixed with split ones (`internal/processflow/processflow_test.go:280`) | Unsplit share holds the flipless trips' whole time |
| TC-12 | Weekly arrivals and completions anchored at the window start | Items created/closed across weeks (`internal/processflow/processflow_test.go:315`) | Per-week counts and totals as asserted |
| TC-13 | Little's law: average WIP as the time integral, throughput per day, implied cycle time | Active items over the window (`internal/processflow/processflow_test.go:342`) | WIP, throughput, and projected hours as asserted |
| TC-14 | Queue pressure: arrivals, departures, net, trend verdicts, weekly WIP samples | Items moving between stations (`internal/processflow/processflow_test.go:359`) | Filling/draining/steady trends and weekly sparkline counts |
| TC-15 | Station flows count observed moves, including exits attributed to the station left | Items with moves and exits (`internal/processflow/processflow_test.go:394`) | From→to counts as asserted |
| TC-16 | One daily sample per active inventory plus cumulative completions | Items over a multi-day window (`internal/processflow/processflow_test.go:421`) | Daily `FlowSample` values as asserted |
| TC-17 | Aging list sorts longest wait first | Mixed open items (`internal/processflow/processflow_test.go:448`) | WIP ordered by descending age |
| TC-18 | Forecast resamples weekly completion history; deterministic seed; nil quantiles with no completions; empty line is 0 weeks now | Weekly histories including all-zero (`internal/processflow/processflow_test.go:463`) | Exact seeded p50/p85 weeks and dates; nil quantiles otherwise |
| TC-19 | A flipless merged PR counts as review wait for flows but reports its trip unsplit in wait shares | Merged PR with no moves (`internal/processflow/processflow_test.go:486`) | Exit flow from awaiting review; unsplit share = 1 |
| TC-20 | Outcomes split the window's arrivals into done/discarded/active, window-bounded | Scenario and a narrow 14-day window (`internal/processflow/processflow_test.go:527`) | Created/done/discarded/active per line as asserted |
| TC-21 | `Build` defaults a non-positive window to 90 days | `Build(..., 0)` (`internal/processflow/processflow_test.go:543`) | `WindowDays` = 90 |
| TC-22 | `BuildRange` covers an explicit window: ages measured at the end, later arrivals excluded | Four items around a four-day range (`internal/processflow/processflow_test.go:550`) | Window fields, arrivals, completions, inventory, WIP as asserted |
| TC-23 | `BuildRange` counts items as of the end: a later merge stays open work, not a completion | PR merged after the range end (`internal/processflow/processflow_test.go:589`) | Station inventory and completions as-of the end |
| TC-24 | A zero start clamps to the earliest item and keeps daily sampling bounded; empty input yields one sample | Two items, then nil (`internal/processflow/processflow_test.go:609`) | `WindowStart` at the first item; 55 daily samples; single sample when empty |
| TC-25 | Render produces every section from the computed numbers; the static document carries no form | Built report rendered with CLI meta (`internal/processreport/report_test.go:19`) | All section markers, inline SVG, no `<form` |
| TC-26 | The interactive report renders the window form, preserving repositories and echoing bounds | Interactive meta with repo ids and days (`internal/processreport/report_test.go:62`) | Form action `/process`, hidden repo inputs, days/from/to inputs; no pinned repos for all-repo mode |
| TC-27 | A ranged report names the covered span and pre-fills the date inputs, not "last N days" | `BuildRange` report with range meta (`internal/processreport/report_test.go:103`) | "Mar 1 – Mar 31, 2026" meta; date inputs carry the bounds |
| TC-28 | The PR line diagram draws every move, including station-skipping ones, one arrow per direction on two-way edges | `LineReport` with mixed flows (`internal/processreport/report_test.go:131`) | SVG paths and per-direction count labels as asserted |
| TC-29 | The issue line diagram carries the close count on its single edge | Issue `LineReport` (`internal/processreport/report_test.go:180`) | SVG with the close count label |
| TC-30 | The rendered aging list caps at ten rows, notes the remainder, and keeps the oldest visible | Thirteen open issues (`internal/processreport/report_test.go:205`) | Ten rows, "and 3 more items on the line", oldest shown, newest hidden |
| TC-31 | An empty report renders empty-state notes instead of failing | `Build(nil, ...)` rendered with empty meta (`internal/processreport/report_test.go:236`) | "(no repository)" and "No items in the window." |

## Integration tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-32 | `serveProcessFunc` builds the report from cached issues and PRs and renders the form with the requested repositories | Seeded store, `ProcessQuery` with repo and days (`cmd/serve_process_test.go:39`) | HTML names the items and carries the preserved repo and window inputs |
| TC-33 | An explicit from/to query renders that range, names it, and rejects inverted or unparseable bounds | Seeded store with a closed issue (`cmd/serve_process_test.go:71`) | Bounds echoed in the inputs; no "last 90 days"; errors otherwise |
| TC-34 | With no bounds and no window the report covers all history and leaves the form inputs unset | Store with a 2025 issue (`cmd/serve_process_test.go:106`) | Meta names the span from the first item; empty from/to/days inputs |
| TC-35 | An uncached named repository and an empty cache are errors, never GitHub calls | Empty store, then a seeded one (`cmd/serve_process_test.go:137`) | Errors for empty cache and unknown repo |
| TC-36 | `/process` parses repo, days, and from/to into `ProcessQuery` and serves the renderer's HTML | Injected renderer capturing the query (`internal/webui/process_test.go:17`) | 200, text/html, query fields as requested |
| TC-37 | `/process` guard rails: 501 without a renderer, 400 on bad days or bounds, 405 on POST | Server with and without a renderer (`internal/webui/process_test.go:41`) | Status codes as asserted |
| TC-38 | `/api/process` computes the JSON report from the cache: stations, samples, aging list | Seeded store, 30-day window (`internal/webui/process_test.go:81`) | Decodable `Report` with expected station counts, samples, WIP |
| TC-39 | `/api/process` honors from/to bounds with an inclusive end-of-day `to`, and rejects bad bounds | Seeded store, March range (`internal/webui/process_test.go:127`) | Window fields as requested; 400 on bad or inverted bounds |
| TC-40 | `/api/process` with no bounds and no window covers all available history | Store with a 2025 closed issue (`internal/webui/process_test.go:179`) | Window starts at the earliest item; all arrivals count |
| TC-41 | `/api/process` without repo selects every cached repository; an uncached repo is 400 | Two seeded repositories (`internal/webui/process_test.go:213`) | Report merges both; 400 for the unknown repo |

## End-to-end tests

None.
The `ghx process` and `ghx serve` commands have no `internal/integration` coverage; the command is a thin composition of the tested loaders, computation, and renderer, and the HTTP endpoints are covered at the handler level above.

## Edge cases and failure scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| EC-1 | Nil issue or PR handed to the mapper | Zero item, no panic (TC-2, TC-3) |
| EC-2 | Closed issue without a recorded close time | Counts toward inventory and outcomes, not cycle times or throughput (TC-2, TC-7) |
| EC-3 | Cache rows storing GraphQL state enums verbatim (`CLOSED`, `OPEN`) | Case-insensitive comparison still maps the station (TC-2) |
| EC-4 | Duplicate or stale timeline moves | Walk ignores them; history cannot corrupt the timeline (TC-4) |
| EC-5 | No recorded history at all | Timeline collapses to the current station entered at creation (TC-4) |
| EC-6 | Discarded PR without a recorded close time | Exit time falls back to the last activity, so discards still count in the window |
| EC-7 | Flipless merged PRs (automation merges, caches without timeline/review data) | Reported as one unsplit wait-share bucket, not attributed by assumption (TC-11, TC-19) |
| EC-8 | A completed PR with only review turns and no flips | Split normally from the review turns; not unsplit (TC-8) |
| EC-9 | `to` bound before `from`, on both endpoints | 400 / error (TC-33, TC-37, TC-39) |
| EC-10 | Malformed `days`, `from`, or `to` values | 400 on both endpoints (TC-37, TC-39) |
| EC-11 | `to` date-only bound | Inclusive: end-of-day 23:59:59.999999999 (TC-39) |
| EC-12 | Zero-length or empty window (no items, zero start) | One sample, empty-state rendering, no division by zero (TC-24, TC-31) |
| EC-13 | Window with no completions | Forecast quantiles stay nil; empty line forecasts 0 weeks dated now (TC-18) |
| EC-14 | Items created after a range end, or finished after it | Excluded, or counted as still-open work as of the end (TC-22, TC-23) |
| EC-15 | More than ten aging items | HTML caps at ten with a remainder note; JSON keeps every item (TC-30) |
| EC-16 | Missing process renderer on the server | 501, not a panic (TC-37) |

## Test infrastructure

- `internal/processflow/processflow_test.go` builds items directly with `issueItem`/`prItem` helpers around a fixed "now"; no store, no client, exercising the pure package.
- `internal/processreport/report_test.go` renders built reports in memory and asserts on HTML strings.
- `cmd/serve_process_test.go` seeds a real store via `cache.NewStoreWithPath(t.TempDir())` and `SaveIssue`/`SavePR`/`SaveCacheInfo`.
- `internal/webui/process_test.go` drives `httptest` requests through `Server.Handler()` with the renderer injected via `SetProcessFunc` and stores seeded by the shared webui test helpers (`seedRepo`, `makeIssue`, `makePR`).
- No network access anywhere: all tests run offline against in-memory or temp-dir stores.

## Coverage matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-2, TC-3, TC-4, TC-8, TC-9 |
| FR-02 | TC-6, TC-22 |
| FR-03 | TC-7 |
| FR-04 | TC-12 |
| FR-05 | TC-13 |
| FR-06 | TC-14 |
| FR-07 | TC-16 |
| FR-08 | TC-17, TC-30 |
| FR-09 | TC-18 |
| FR-10 | TC-20 |
| FR-11 | TC-8, TC-10, TC-11, TC-19 |
| FR-12 | TC-25 (rendering); command composition untested directly (loaders covered by the stats feature's tests) |
| FR-13 | TC-32, TC-35 (serve-side cache-only path); CLI fallback loaders shared with stats |
| FR-14 | TC-32, TC-36, TC-37 |
| FR-15 | TC-38, TC-39, TC-40, TC-41 |
| FR-16 | TC-4, TC-5 |
| FR-17 | TC-30 |
| FR-18 | TC-2, EC-2 |
| FR-19 | TC-20 |
| FR-20 | TC-27, TC-33, TC-36, TC-39 |
| FR-21 | TC-26, TC-32, TC-34 |
| FR-22 | TC-34, TC-40 |
| NFR-01 | TC-1..TC-24 run the pure package with no store or client |
| NFR-02 | TC-25 (inline SVG asserted), TC-31 |
| NFR-03 | TC-18 (exact seeded quantiles) |
