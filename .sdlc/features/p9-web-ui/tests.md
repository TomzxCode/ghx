---
title: "Web UI (serve command)"
status: done
---

# Test plan: Web UI (serve command)

## Scope

Covered: the `ghx serve` command and its injected callbacks (`cmd/serve.go`), the HTTP server and JSON API (`internal/webui/server.go`), the markdown renderer and label color sanitizer (`internal/webui/render.go`), the stats and process report pages served at `/stats` and `/process` with their JSON endpoint `/api/process` (`cmd/serve_stats.go`, `cmd/serve_process.go`, `internal/webui/process.go`), and the embedded assets served from `go:embed`.
Out of scope: the browser-side JavaScript (`internal/webui/assets/app.js` has no automated tests), the JSON-RPC surface mounted at `/rpc` (covered by FEAT-p11's suite and `cmd/rpc_test.go`), the stats and process report computation itself (FEAT-p8 and the process-flow feature), and the cache fetch logic shared with `ghx cache` (FEAT-p5).

## Unit Tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | `paletteScore` precedence | Exact number, `#number`, title word prefix, title subsequence, no match, empty query | Scores 3, 3, 2, 1, 0, 0; case-insensitive (`TestPaletteScore`, `internal/webui/server_test.go`) |
| TC-2 | Markdown rendering with GFM extensions | Bold/italic, table, strikethrough, task list, bare URL, raw `<script>`, HTML in inline code, `javascript:` link, whitespace-only input | Each feature renders; raw HTML removed; `javascript:` href neutralized; empty input renders empty (`TestRenderMarkdown`, `internal/webui/render_test.go`) |
| TC-3 | Label color sanitization | `d73a4a`, `D73A4A`, `f0f`, empty, `red`, `#d73a4a`, `<script>`, 5 digits | Safe hex values normalize to lowercase `#rrggbb`/`#rgb`; everything else returns `""` (`TestSanitizeColor`) |

## Integration Tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-4 | `/api/repos` lists every cached repository | Two seeded repos, alpha and zeta | Sorted by owner then repo name, with issue/PR counts, `complete`, and `cachedAt` (`TestReposEndpoint`, `internal/webui/server_test.go`) |
| TC-5 | Issue list filtering | Issues in mixed states, `state` open/all, `q=docs` | Default is open; `all` returns everything sorted by `updatedAt` desc; query narrows; `state=merged` for issues is 400; unknown repo is 404 (`TestListIssuesFiltering`) |
| TC-6 | PR list state normalization | Open, merged (CLOSED with `MergedAt`), and draft PRs | Merged PR reports `merged`; draft flag surfaces; `state=merged` returns one; `merged` rejected for issues (`TestListPRsMergedState`) |
| TC-7 | Issue detail | Issue with markdown body, `<script>` tag, and one comment | `bodyHTML` rendered, script tag absent, comment rendered with author, `cachedAt` recent and RFC3339; missing number is 404 (`TestDetailIssue`) |
| TC-8 | PR detail | Merged PR with branch names | `kind`/`state`, `baseRefName`/`headRefName`, and `mergedAt` present; `cachedAt` recent (`TestDetailPR`) |
| TC-9 | PR reviewer combination | Reviews (approval hidden by later comment, changes requested, comment-only), current requests (user, team, re-request, author), stale timeline event | Reviewer states approved/changes_requested/pending/commented; team appears by name; author never listed; stale request absent (`TestDetailPRReviewers`) |
| TC-10 | PR reviewers from a legacy cache row | PR with `ReviewRequests` nil and timeline request events | Reviewers come from user-only timeline events; author excluded (`TestDetailPRReviewersLegacyTimeline`) |
| TC-11 | Repo refresh concurrency and method | Blocking stub refresh callback | First POST 200 with `{"issues","prs"}` counts and data written; concurrent POST 409; GET 405 (`TestRefreshEndpoint`) |
| TC-12 | Repository cached only by view | Items saved without cache metadata | List and detail still serve; refresh runs a cold fetch; a repo with nothing at all stays 404 (`TestRefreshRepoCachedOnlyByView`) |
| TC-13 | Item refresh endpoint | Two items seeded, blocking callback | Same-item concurrent POST 409; different item runs; records rewritten; GET 405; missing item 404; unknown sub-resource 404; no callback 501; unknown repo 404 (`TestItemRefreshEndpoint`) |
| TC-14 | Palette search endpoint | Issues across two repos, queries by number, prefix, subsequence | Empty query returns recent items; number wins; prefix and subsequence ranked; `scope=all` fills host/owner/repo; missing repo params 400; bad scope 400 (`TestSearchScoring`) |
| TC-15 | Embedded assets | GET `/`, `/assets/app.js`, `/assets/missing.js`, `/nope` | index.html served as HTML and references app.js; JS served with the right type; missing asset and unknown path 404 (`TestStaticAssets`) |
| TC-16 | `/stats` query mapping and defaults | Full query string and bare `/stats` | All filters map onto `StatsQuery`; defaults state=all and top=5 (`TestStatsEndpoint`) |
| TC-17 | `/stats` guard rails | Bad state, bad top, failing renderer, nil renderer, POST | 400, 400, 500 with the JSON error body, 501, 405 (`TestStatsEndpoint_ValidationAndAvailability`) |
| TC-18 | `/process` query mapping | repo, days, from, to | `ProcessQuery` carries every value; HTML content type (`TestProcessEndpoint`, `internal/webui/process_test.go`) |
| TC-19 | `/process` guard rails | No renderer, bad days, bad from, to before from, zero days, POST | 501, 400 on each validation, 405 (`TestProcessEndpoint_ValidationAndAvailability`) |
| TC-20 | `/api/process` computed from cache | Open issue, merged PR, draft PR, days=30 | Station counts, flow samples, and the WIP list match the fixtures (`TestAPIProcess`) |
| TC-21 | `/api/process` explicit range | Issue closed inside a from/to window, another created after it | Window bounds inclusive of end-of-day; completions count; later arrivals excluded; bad bounds 400 (`TestAPIProcess_Range`) |
| TC-22 | `/api/process` all-history default and repo defaults | Old closed issue, no params; two seeded repos; uncached repo name | Window starts at the earliest item; empty repo list selects every cached repo; uncached name 400 (`TestAPIProcess_DefaultsToAllHistory`, `TestAPIProcess_DefaultsToAllCachedRepos`) |
| TC-23 | Stats renderer adapter | Cached PRs via `serveStatsFunc` | Report rendered from cache with repos, authors, reviewers; period and author filters apply; interactive period form preserves repo and bounds (`TestServeStatsFunc_*`, `cmd/serve_stats_test.go`) |
| TC-24 | CLI-written stats report stays static | `buildReport` + `renderReport` without the interactive flag | No period form unless `Interactive` is set; form inputs carry repo, from, to, top, state (`TestRenderReport_PeriodFormIsInteractiveOnly`) |
| TC-25 | Stats renderer error paths | Empty cache, two-part repo name, uncached repo, bad date, to before from | Each returns an error the endpoint turns into a 500 (`TestServeStatsFunc_Errors`) |
| TC-26 | Process renderer adapter | Cached open issue and draft PR via `serveProcessFunc` | Report names active items in the aging list; period form preserves repos and bounds (`TestServeProcessFunc_RendersReport`) |
| TC-27 | Process renderer range and defaults | Ranged issue; old issue with no bounds | Bounds pre-filled and named; all-history default says so and leaves inputs empty (`TestServeProcessFunc_Range`, `TestServeProcessFunc_DefaultsToAllHistory`) |
| TC-28 | Process renderer error paths | Empty cache, uncached repo | Errors instead of empty reports (`TestServeProcessFunc_RejectsUncachedRepo`) |
| TC-29 | Non-loopback bind refusal | `serveHost` 0.0.0.0 with no token | `runServe` refuses to start; `isLoopbackAddr` classified correctly (`cmd/rpc_test.go:82-101`) |

## End-to-End Tests

| ID | Description | Steps | Expected Outcome |
|---|---|---|---|
| TC-30 | Repo refresh through the mock GitHub API | Mock server with one issue and one PR; POST the refresh endpoint via the real `fetchRepoData` path | 200 with counts 1/1; cache info complete; the list endpoint serves the new rows; the detail endpoint renders the fetched markdown (`TestServeRefreshWithMockServer`, `cmd/serve_test.go`) |
| TC-31 | Single-item refresh bypasses freshness | Warm full cache, change the mock scenario, POST the item refresh endpoints | Both items refetched and rewritten even though the repo cache is fresh (`TestServeItemRefreshWithMockServer`) |

## Edge Cases and Failure Scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-P1 | PR closed with `MergedAt` set | Reported as `merged` in lists and details (TC-6, TC-8) |
| TC-P2 | `state=merged` on an issue list | 400, message names the valid states (TC-5) |
| TC-P3 | Unknown repository on any repo-scoped endpoint | 404, including refresh and item refresh (TC-5, TC-13) |
| TC-P4 | Missing item number | 404 on detail and item refresh (TC-7, TC-13) |
| TC-P5 | Concurrent refresh of the same repo or item | Second request 409; a different item's refresh runs in parallel (TC-11, TC-13) |
| TC-P6 | Repository with items but no cache metadata | Listed, viewable, and refreshable with a cold fetch (TC-12) |
| TC-P7 | Refresh/detail callback not injected | Repo refresh 500, item refresh and report pages 501 (TC-13, TC-17) |
| TC-P8 | Later comment-only review after an approval | Approval still shown; a later decision replaces an earlier one (TC-9) |
| TC-P9 | Cache row predating `reviewRequests` | Timeline review-request events used as a user-only fallback (TC-10) |
| TC-P10 | Team review request and re-request | Team appears by name as pending; re-requested reviewer flips to pending (TC-9) |
| TC-P11 | Raw HTML and unsafe URL schemes in markdown | Removed/neutralized; inline code HTML escaped (TC-2, TC-7) |
| TC-P12 | Unsafe label colors | Empty color string, neutral styling in the UI (TC-3) |
| TC-P13 | Invalid query parameters | 400 for bad state, top, days, from, to, and to before from (TC-5, TC-17, TC-19, TC-21) |
| TC-P14 | Search with no repo params or bad scope | 400; empty query returns the most recent items (TC-14) |
| TC-P15 | `/api/process` with no bounds and no window | Report covers the entire available history (TC-22) |

## Test Infrastructure

- `internal/mockserver` provides the fake GitHub GraphQL server; the refresh end-to-end tests point `fetchRepoData` at it and mutate the scenario between calls (`UpdateScenario`).
- `internal/webui` tests use real stores backed by `t.TempDir()` through `cache.NewStoreWithPath`, seed fixtures with `makeIssue`/`makePR`/`seedRepo`, and exercise handlers through `httptest` against `Server.Handler()` without binding a port.
- Blocking stub callbacks plus goroutines and channels create a deterministic in-flight refresh for the 409 cases.
- The stats and process renderers are tested as injected `webui.StatsFunc`/`webui.ProcessFunc` adapters in `cmd`, keeping the report builders decoupled from the HTTP layer.
- Run everything with `go test -race -cover ./...`.

## Coverage Matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-29 (loopback guard and refusal); the printed URL, binding, and signal shutdown are untested |
| FR-02 | TC-4 |
| FR-03 | TC-4, TC-5, TC-6, TC-3 |
| FR-04 | TC-7, TC-8, TC-9, TC-10 |
| FR-05 | TC-5, TC-6, TC-P2 |
| FR-06 | TC-1, TC-14 (server side only); palette keybindings and jump behavior in app.js are untested |
| FR-07 | TC-2, TC-7 |
| FR-08 | TC-11, TC-12, TC-13, TC-30, TC-31 |
| FR-09 | TC-15 |
| FR-10 | TC-29 (`--host`/`--port` exercised through the refusal test); `--open` has no test |
| FR-11 | TC-14 (`scope=all`) |
| FR-12 | TC-4, TC-5, TC-6, TC-7, TC-8, TC-14, TC-20 (every endpoint test parses JSON) |
| FR-13 | None (frontend empty state) |
| FR-14 | None (frontend theme toggle) |
| NFR-01 | TC-29, TC-2, TC-7 |
| NFR-02 | TC-11, TC-13 |
| NFR-03 | TC-30 |
| NFR-04 | TC-5, TC-6 (handler tests serve from the store with no network) |
| NFR-05 | TC-15 (embedded assets; the CDN tags themselves are unasserted) |

Coverage gaps: the frontend JavaScript has no automated tests, so FR-13, FR-14, and the client half of FR-06 are uncovered; `runServe` is never tested with a live listener, leaving FR-01's printed URL and clean shutdown and FR-10's `--open` unverified; the `Mount` mechanism for `/rpc` is exercised only by the JSON-RPC suite.
