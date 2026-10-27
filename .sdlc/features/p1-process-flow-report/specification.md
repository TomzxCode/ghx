---
title: "Process Flow Report"
status: done
---

# Specification: Process Flow Report

## Architecture

Three pieces, mirroring the stats report's layering:

```
internal/processflow    pure computation: items + window -> Report (JSON-tagged structs)
internal/processreport  HTML rendering of a Report (server-side SVG, no external assets)
cmd/process.go          ghx process command (cache-first loading, API fallback)
cmd/serve_process.go    webui.ProcessFunc adapter (cache-only loading)
internal/webui/process.go  /process HTML endpoint + /api/process JSON endpoint
```

`processflow` knows nothing about GitHub clients or the cache. The GitHub
mapping lives in its `github.go` adapter (`FromIssue`, `FromPR`), so the web
UI and the CLI convert the same way.

## Data model

An `Item` reduces one issue or PR to what the flow computation needs:
`ID`, `Kind` (issue/pr), `Title`, `URL`, `CreatedAt`, `Station`, `ExitedAt`
(the close/merge/discard time, nil while active or unknown), and `Moves`
(draft/ready flips from cached timeline events and review turns from cached
reviews, oldest first).

Stations: `Open`, `Draft`, `AwaitingReview`, `AwaitingAuthor`, and
`AwaitingMerge` are active; `Closed`, `Merged` are completions; `Discarded`
is scrap. The `Line` type orders a kind's stations (issues: Open, Closed;
PRs: Draft, Awaiting review, Waiting on author, Waiting on merge, Merged,
Discarded), names them, and names the completion station. PRs opened
directly as ready start in the review-wait station.

## Timeline walk

`Item.Timeline` walks creation to now: every item starts at its line's first
station (issues Open, PRs Draft); a move applies only when it changes the
current station and happens after the previous entry (duplicate or stale
timeline events cannot corrupt the walk); a recorded `ExitedAt` is appended as
the final move so merged/closed items show their real finish moment; and a
walk that ends off the current station is corrected to it, entering at the
last known move. This one rule
set backs inventory ages, per-day station lookups (cumulative flow, weekly
WIP), and station waits.

## Report computation

All computations take the window `[end-windowDays, end]`; the window end
doubles as "now" so a report for a past window stays reproducible.

- **Completions**: line items whose `ExitedAt` (at the completion station)
  falls inside the window; cycle time is creation to `ExitedAt`. Discarded PRs
  never count as completions.
- **Dwell and wait shares**: for each completed PR with recorded flips, its
  timeline segments clamped at the completion moment give per-station waits;
  shares divide each station's total by the grand total. Completed PRs
  without flip history (automation merges, caches without timeline events)
  land in one unsplit bucket instead of being attributed to a station; all
  shares, including the unsplit one, sum to one. The issues line has one
  active station, so its shares are trivially 100% and the page says so.
- **Little's law**: average WIP is the exact time integral of active-station
  membership over the window; throughput is completions per day; implied cycle
  time is WIP divided by throughput.
- **Queue pressure**: arrivals are creations (into the first station) plus
  moves into the station; departures are moves out (including exits to
  Merged/Closed/Discarded). Net gives the trend. Weekly WIP samples inventory
  at each week's end from the timelines.
- **Cumulative flow**: one sample per day (inclusive) of open issues, the
  three PR review-wait stations (draft, reviewer, author, merge), and
  cumulative completions (closed issues plus merged PRs).
- **Forecast**: 500 simulations draw one week's completions at a time from the
  window's weekly history (with replacement, `rand.PCG` seeded 0, 520-week
  cap) until the active count is exhausted; p50/p85 become weeks and dates.
  No completions in the window means no forecast (nil quantiles).
- **Outcomes**: window arrivals split by current disposition.

## HTML rendering

`processreport.Render` flattens the report into a view and renders one
template. Charts are server-side SVG strings (stacked area, grouped weekly
bars, histogram, sparklines, wait-share bar) and the station diagram is
server-side HTML, so the template only places them. The page is styled with
CSS variables and `prefers-color-scheme` dark mode; no JavaScript, no external
assets. The aging table is capped at ten rows with a note; the JSON keeps
everything.

## CLI command

`ghx process [REPO...] --days 90 --output process-report.html` reuses the
stats loaders' shape: fresh cache served as-is, stale cache served with a
warning, empty cache falling back to `FetchAllIssues`/`FetchAllPRs`.

## Serve endpoints

`/process` (HTML) and `/api/process` (JSON) share query parsing: `repo`
(repeatable `host/owner/repo`; omitted means every cached repository; an
explicit unknown repository is an error) and `days` (1-730, default 90; a bad
value is 400). The web UI computes cache-only, matching its read-only
contract: `serveProcessFunc` resolves repositories against the cache, loads
issues and PRs from the store, builds, and renders. The server wires the
renderer through `SetProcessFunc`, exactly like `SetStatsFunc`; a nil renderer
answers 501.

## Explicit ranges

`BuildRange(items, start, end)` computes any explicit window; `Build` is the
trailing-window special case. A zero start means all history and clamps to
the earliest item's creation, keeping the daily sampling bounded; end doubles
as "current time" for ages and forecasts, and the report records
`WindowStart`/`WindowEnd` alongside the derived `WindowDays`.

The serve endpoints resolve the window the same way: explicit bounds win,
then an explicit days value, and with neither the report covers all
available history (`BuildRange` with a zero start and end now).

Before computing, BuildRange projects every item to its state at the window
end (`itemAsOf`): the station it sat in then, moves recorded up to then, and
its exit only if it finished by then. Items created after the end are
excluded and items that finished later count as still-open work, so an
explicit range describes the line as it stood at the range end, with station
inventory, the aging list, and outcomes all as-of rather than current. The serve page
parses `from`/`to` (date-only values or RFC3339; `to` gets end-of-day so the
bound is inclusive), treats either bound as range mode with the days value
ignored, and the form's date inputs pre-fill with the covered bounds.

## Test map

| Requirement | Test |
|---|---|
| FR-01, FR-16 | `TestFromIssueMapsStates`, `TestFromPRMapsStatesAndMoves`, `TestTimelineWalksForwardAndCorrectsMissingHistory`, `TestTimelineIncludesRecordedExit` |
| FR-02 | `TestStageInventoryCountsAndAges` |
| FR-03 | `TestCycleTimeDistributionAndHistogram` |
| FR-04 | `TestWeeklyFlowSplitsArrivalsAndCompletions` |
| FR-05 | `TestLittlesLawIntegratesWorkInProcess` |
| FR-06 | `TestQueuePressureReadsNetFlow` |
| FR-07 | `TestFlowSamplesSnapshotEveryDay` |
| FR-08 | `TestWipItemsSortLongestWaitFirst` |
| FR-09 | `TestForecastResamplesWeeklyHistory` |
| FR-10 | `TestOutcomesSplitWindowArrivalsByDisposition` |
| FR-11 | `TestWaitSharesRankTheBottleneck`, `TestMergedPRWithoutFlipHistoryCountsAsReviewWait`, `TestWaitSharesBucketFliplessTripsSeparately`, `TestFromPRReviewTurnsDriveStations`, `TestFromPRWithoutAnyHistoryStaysUnsplit` |
| FR-12, FR-13 | command covered by the loaders' reuse of the tested stats loaders; report rendering by `TestRenderProducesTheSections` |
| FR-14, FR-15 | `TestProcessEndpoint`, `TestProcessEndpoint_ValidationAndAvailability`, `TestAPIProcess`, `TestAPIProcess_DefaultsToAllCachedRepos`, `TestServeProcessFunc_RendersReport`, `TestServeProcessFunc_RejectsUncachedRepo` |
| FR-17 | `TestRenderProducesTheSections` (capped table renders), cap constant unit-enforced |
| FR-18 | `TestFromIssueMapsStates` (closed without close time), completions skip nil exits |
| FR-19 | `TestOutcomesSplitWindowArrivalsByDisposition` |
| NFR-01 | package imports: `processflow` imports only stdlib plus the github types adapter |
| NFR-03 | `TestForecastResamplesWeeklyHistory` asserts exact seeded quantiles |
| FR-22 (all-history default) | `TestAPIProcess_DefaultsToAllHistory`, `TestServeProcessFunc_DefaultsToAllHistory` |
| FR-20, FR-21 | `TestBuildRangeCoversAnExplicitWindow`, `TestBuildRangeZeroStartStartsAtTheFirstItem`, `TestBuildRangeCountsItemsAsOfTheEnd`, `TestRenderRangeMeta`, `TestProcessEndpoint` (bounds parsed), `TestProcessEndpoint_ValidationAndAvailability` (bad bounds), `TestAPIProcess_Range`, `TestServeProcessFunc_Range` |
