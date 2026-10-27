---
title: "Stats Report Analytics"
status: done
---

# Test plan: Stats Report Analytics

## Scope

Tests cover the `ghx stats` report pipeline: period parsing, report aggregation (author × reviewer matrix, per-author and per-reviewer tables, lead time, contribution, reviewer engagement, merge speed, size buckets, monthly and per-author trends, notable lists), HTML rendering, the cache-fallback data path, and the web UI's reuse of the same report through `serveStatsFunc`.
GraphQL collection of the new fields is covered as supporting evidence by the query-parsing tests in `internal/github` and the mock-server round trips; the `serve` HTTP routing itself belongs to FEAT-p9 and is out of scope here.
Chart.js drawing inside a browser is not tested; the Go tests assert section markers and the JSON payload, not rendered pixels.

## Unit tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | Period parsing: empty is unbounded, date-only from is midnight, date-only to is end of day, RFC3339 accepted, invalid rejected | `parseStatsTime` cases (`cmd/stats_test.go:26`) | Parsed bounds or an error |
| TC-2 | Matrix counts comment responses, ignores self-comments, orders columns, computes cell median and average | Four PRs with mixed comments (`cmd/stats_test.go:64`) | 3 PRs in period, bob before carol, median "3h 0m" |
| TC-3 | Submitted reviews count as reviewer activity; dismissed reviews, author reviews, and bot reviews do not; cell share is row-relative | PRs with APPROVED, DISMISSED, author, and bot reviews (`cmd/stats_test.go:183`) | bob-only column, "50%" share, 1h 30m median, "PRs w/o review" agrees |
| TC-4 | Per-reviewer table lists only reviewers who submitted reviews; engagement keeps comments and reviews apart | Comment-only bob, review-only carol (`cmd/stats_test.go:268`) | carol-only reviewer rows; separate engagement figures |
| TC-5 | Per-author monthly trends bucket opened, merged, and closed by month | PRs across January and February (`cmd/stats_test.go:313`) | One series per author with expected per-month counts |
| TC-6 | Cell share formatting including under 1% and empty cases | `formatReviewPct` table (`cmd/stats_test.go:374`) | "33%", "50%", "100%", "<1%", "" |
| TC-7 | Author, reviewer, state, and include-bots filters narrow the report | Filtered `buildReport` runs (`cmd/stats_test.go:393`) | Only matching PRs and reviewer columns counted |
| TC-8 | Bot detection uses the reported account type, not the login | Bot-typed human-looking login and user-typed `-bot` login (`cmd/stats_test.go:466`) | Bot excluded by default, both kept with `--include-bots` |
| TC-9 | Summary tables across repositories, PR list opt-in, and comment-before-review response ordering | Two repositories, comment then review (`cmd/stats_test.go:503`) | Correct author and reviewer rows; PR list only with `ListPRs` |
| TC-10 | Percentile interpolation between closest ranks | `percentileDuration` table (`cmd/stats_test.go:583`) | p50/p99 for empty, single, even, and odd samples |
| TC-11 | Merge speed windows, percentiles, per-author merge speed, and trend samples; sections omitted without merged PRs | Four merged plus one open PR (`cmd/stats_test.go:605`) | 25%/50%/100% windows, interpolated p50/p90/p99, one sample per merged PR |
| TC-12 | Duration and median helpers | `formatStatDuration` and `medianDuration` tables (`cmd/stats_test.go:689,706`) | "2d 2h" style strings; even and odd medians |
| TC-13 | Lead time and contribution tables: median/average merge, merge rate, no-review count, comments received, additions/deletions, size counts, totals | Merged PR with sizes and a closed unreviewed PR (`cmd/stats_test.go:767`) | "2d 0h" median merge, "100%" rate, +400/-100, m bucket, bob's closed count |
| TC-14 | Size bucket boundaries from changed lines | PRs of 99, 400, 1000, and 2000 changed lines (`cmd/stats_test.go:829`) | One PR each in xs, s, l, and xl; m empty |
| TC-15 | Reviewer engagement counts and open/request/re-request response medians; author-side first review and approve medians | Reviews plus two review-requested events (`cmd/stats_test.go:858`) | Approval and comment-only counts, 3h/2h/2h medians, 3h first review, 8h approve |
| TC-16 | Approval-to-merge excludes post-merge approvals and unapproved merges | Three merged PRs (`cmd/stats_test.go:960`) | Only the pre-merge approval contributes ("1d 0h") |
| TC-17 | Draft time reconstruction: created-as-draft, a request event not hiding the draft period, and a convert-to-draft cycle | Three timeline shapes (`cmd/stats_test.go:1003`) | "2h 0m" median draft in every shape |
| TC-18 | Notable lists rank by metric, respect `Top`, and label awaiting entries | Merged and stale open PRs, Top 1 (`cmd/stats_test.go:1053`) | One entry per list; PR 2 and PR 10 first |
| TC-19 | Rendered HTML carries every analytics section, the Chart.js CDN tag, sidebar anchors, merge-trend controls, escaped titles, and no dangling empty labels | Report with a malicious title (`cmd/stats_test.go:1146`) | All markers present; `<script>` escaped |
| TC-20 | Legacy-cache notice: PRs without analytics data trigger it, data clears it, a small share does not | PR with and without review data (`cmd/stats_test.go:1293`) | Notice HTML present exactly when most PRs lack data |
| TC-21 | PR list and its sidebar link are omitted without `ListPRs` | Report without `ListPRs` (`cmd/stats_test.go:1340`) | No "Pull requests in period" section or anchor |

## Integration tests

Run in `cmd` against `internal/mockserver` with a temporary cache directory.

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-22 | Empty-cache stats falls back to the GitHub API and keeps comment authors for reviewer metrics | Mock server PR with comments (`cmd/stats_test.go:720`) | Fetched PR carries bob's comment |
| TC-23 | Cache round trip preserves additions, deletions, reviews, and timeline events | Mock server PR with analytics data (`cmd/stats_test.go:1090`) | Saved PR has +120/-30, bob's approval, and the review-requested event |
| TC-24 | Web UI stats renders the report from cached PRs of the requested repositories | Seeded store (`cmd/serve_stats_test.go:33`) | HTML names the repository, author, and reviewer |
| TC-25 | Empty repository list serves every cached repository and the period filter drops out-of-period PRs | PRs across the period boundary (`cmd/serve_stats_test.go:59`) | Only the in-period PR listed |
| TC-26 | Query filters map onto the report | Seeded store, author filter (`cmd/serve_stats_test.go:79`) | bob excluded from the HTML |
| TC-27 | Interactive period form carries from/to/repo/state/top values and never renders in CLI output; all-repos reports omit repo inputs | `buildReport` and `renderReport` variants (`cmd/serve_stats_test.go:97,132`) | Form present only when `Interactive` |
| TC-28 | Serve stats error paths: empty cache, malformed repo names, uncached repository, invalid or inverted period | Error cases (`cmd/serve_stats_test.go:184`) | Each returns an error naming the fix |

## End-to-end tests

No compiled-CLI end-to-end tests cover `stats`; `internal/integration` does not exercise this command.
The command-level mock-server tests above (TC-22 to TC-28) are the outermost automated layer.

## Edge cases and failure scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-29 | PR cached before analytics data existed | Counts toward the legacy-cache notice; unavailable metrics render as `-` (TC-20, TC-19) |
| TC-30 | A small share of PRs without analytics data, such as genuinely empty diffs | Notice suppressed; only more than a fifth of PRs lacking data triggers it (TC-20) |
| TC-31 | Post-merge approval, unapproved merge, never-reviewed merge | Excluded from approval-to-merge; the lead row still renders (TC-16, TC-15) |
| TC-32 | Dismissed or pending review states, author or bot reviews | Never counted as reviewer activity (TC-3) |
| TC-33 | Out-of-period or wrong-state PRs | Excluded before any aggregation (TC-2, TC-7) |
| TC-34 | Report with no merged PRs | Merge speed sections and lead rows omitted entirely (TC-11, TC-13) |
| TC-35 | PR title containing HTML | Escaped in the rendered report (TC-19) |
| TC-36 | Chart.js CDN unreachable | Chart boxes are replaced with a note by the guard in the template; asserted by inspection only (`cmd/stats_report.go:2410`) |

## Test infrastructure

- `internal/mockserver` serves scenario PRs with fixed timestamps for the fallback and cache round-trip tests (`cmd/stats_test.go:720,1090`).
- Command tests save and restore the package-level `apiURLFlag` and `cacheDir` flags and point the store at `t.TempDir()` (`cmd/stats_test.go:1117`).
- Report tests build in-memory fixtures with the `statsTestPR` helper and call `buildReport` and `renderReport` directly; the serve tests seed a SQLite store with `seedStatsRepo` (`cmd/serve_stats_test.go:16`).
- Supporting collection evidence: timeline parsing including the requested-reviewer Bot inline fragment (`internal/github/api_test.go:123,203`).

## Coverage matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-23, TC-22 (reviews, timeline, and size survive the cache; comment fallback) |
| FR-02 | TC-13, TC-15, TC-16, TC-17 (median/average merge, draft, first review, approve, approval-to-merge) |
| FR-03 | TC-13, TC-3 (opened/merged/closed, merge rate, PRs w/o review, comments received, +/-, size distribution) |
| FR-04 | TC-15, TC-4 (comments, reviews, approvals, changes requested, comment-only, open-to-review, request-to-review) |
| FR-05 | TC-18 (awaiting first review, longest merge, most discussed, `--top`) |
| FR-06 | TC-14, TC-13 (xs/s/m/l/xl boundaries; size versus merge time rows) |
| FR-07 | TC-19, TC-5, TC-11 (trend, per-author, size, and peak-activity charts plus the CDN tag) |
| FR-08 | TC-20, TC-19 (legacy-cache notice and `-` rendering; CDN-failure guard inspected only, TC-36) |
| FR-09 | TC-18 (`Top` bounds each notable list) |
| FR-10 | TC-17 (created-as-draft and later convert-to-draft cycles) |
| FR-11 | TC-23 (mock server serves and the cache preserves the generated fields) |
| NFR-01 | TC-20, TC-29 (old entries readable, `-` instead of failure) |
| NFR-02 | TC-23 (collection through the shared query with the fixed pagination budgets, verified by inspection of `internal/github/api.go:782,821,868`) |
