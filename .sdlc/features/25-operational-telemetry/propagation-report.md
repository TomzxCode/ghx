---
date: "2026-10-03"
scope: "FEAT-25 (25-operational-telemetry)"
entry: "code"
flags: "fix: true, create-issues: false"
status: complete
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# Propagation Report

**Date:** 2026-10-03
**Scope:** 25-operational-telemetry (FEAT-25, issue #25)
**Entry:** code (the change committed as `b55f03e`)

The change entered at `code` and consists of the whole telemetry feature as implemented: the recorder and sink, both instrumentation seams, the `telemetry` command family, the post-cache summary, mock-server latency injection, the default-on switch, the byte and item-count fields, and the byte totals in the summary.
The artifacts that describe it were written across several earlier revisions and still carried the pre-default-on, pre-byte-capture, and pre-toggle wording, so the downward walk had real drift to resolve.

## Consistency Status

| Feature | Downward updates | Upward drift | IDs | Review order | Status |
|---|---|---|---|---|---|
| 25-operational-telemetry | 12 applied | 1 finding | clean | no reviews recorded | drifted, now in sync |

## Updates (downward)

| # | Artifact | Edge | Change | Resolution | Status |
|---|---|---|---|---|---|
| 1 | cli-design.md | code -> docs | `telemetry enable/disable/status` exist but were undocumented | added the command section and tree entries | applied |
| 2 | cli-design.md | code -> docs | telemetry was documented as opt-in | rewrote the design principle and FR-5 trace row to default-on with a switch | applied |
| 3 | cli-design.md | code -> docs | `--telemetry` default described as `false` | corrected the global-options table and help text | applied |
| 4 | cli-design.md | code -> docs | "no config file" out of scope | replaced with the real `config.json` and precedence chain | applied |
| 5 | cli-design.md | code -> docs | export field list omitted sizes and item counts | added `items_returned`, `request_bytes`, `response_bytes`, `rate_remaining` | applied |
| 6 | cli-design.md | code -> docs | example sessions pre-dated sizes and the toggle | refreshed the cache-run, summary, and added a toggle session | applied |
| 7 | cli-design.md | code -> docs | traceability omitted NFR-8, NFR-9 | added both rows and corrected FR-5 to FR-11 | applied |
| 8 | specification.md | code -> spec | overview said "disabled unless opted in" | rewrote to default-on with the switch and opt-outs | applied |
| 9 | specification.md | code -> spec | `LatencyModel` table listed a stale field set | replaced with `Samples []int64`, `ResponseBytes`, and both sample methods | applied |
| 10 | specification.md | code -> spec | enable sequence used `ghx --telemetry cache` | changed to `ghx cache` (recording on by default) | applied |
| 11 | tests.md | code -> tests | TC-5, TC-21, TC-22 encoded the opt-in behavior | rewrote for default-on and per-run opt-out; added toggle and byte-total cases | applied |
| 12 | requirements.md, telemetry.md, observability.md | code -> artifact | "opt-in" wording and the funnel step | corrected to default-on with opt-out | applied |

## Questions Raised

None.
Every drift had a single resolution that followed directly from the change, so nothing required escalation.

## Traceability Matrix

### 25-operational-telemetry

| FR / NFR | Spec section | Task(s) | Test(s) | Code location | Docs | Issue AC |
|---|---|---|---|---|---|---|
| FR-1 | telemetry_event; QueryCtx | (n/a) | TC-1, TC-2, TC-11, TC-12 | internal/github/telemetry.go | docs/user-guide/telemetry.md | telemetry recorded |
| FR-2 | telemetry_event; Store decorator | (n/a) | TC-3, TC-14 | internal/cache/instrumented.go | flag-reference.md | cache ops recorded |
| FR-3 | operation kind | (n/a) | TC-13 | internal/github/api.go | telemetry.md (kinds table) | kinds separable |
| FR-4 | path resolution | (n/a) | TC-4 | internal/telemetry/store.go | telemetry.md | db path |
| FR-5 | default + switch | (n/a) | TC-5, TC-22, TC-32, TC-33 | internal/telemetry/config.go; cmd/telemetry_wiring.go | telemetry.md | default state |
| FR-6 | per-run summary | (n/a) | TC-6, TC-18 | cmd/cache.go | telemetry.md | run summary |
| FR-7 | read surface | (n/a) | TC-16, TC-27 | cmd/telemetry.go | telemetry.md | summary command |
| FR-8 | exported JSON | (n/a) | TC-7, TC-17, TC-28 | cmd/telemetry.go | telemetry.md | export |
| FR-9 | LatencyModel | (n/a) | TC-9, TC-20, TC-30 | internal/mockserver/latency.go | telemetry.md | latency injection |
| FR-10 | per-run summary | (n/a) | TC-18 | cmd/cache.go | telemetry.md | elapsed time |
| FR-11 | rate_remaining | (n/a) | TC-1 | internal/github/client.go | telemetry.md | rate remaining |
| NFR-1 | write path | (n/a) | TC-31 | internal/telemetry/async.go | observability.md | overhead |
| NFR-2 | failure handling | (n/a) | TC-24, TC-25, TC-29 | internal/telemetry/async.go | observability.md | failure isolation |
| NFR-3 | (no secrets) | (n/a) | TC-26 | internal/github/telemetry.go | telemetry.md | privacy |
| NFR-4 | local-only | (n/a) | TC-22 | internal/telemetry/config.go | telemetry.md | no egress |
| NFR-5 | migration | (n/a) | TC-10, TC-10b | internal/telemetry/store.go | specification.md | additive schema |
| NFR-6 | Store decorator | (n/a) | TC-14 | internal/cache/instrumented.go | telemetry.md | both backends |
| NFR-7 | help text | (n/a) | TC-21 | cmd/root.go | flag-reference.md | discoverability |
| NFR-8 | global switch | (n/a) | TC-32, TC-33 | cmd/telemetry_toggle.go | telemetry.md | enable/disable |
| NFR-9 | byte totals | (n/a) | TC-6b | cmd/telemetry.go | telemetry.md | sent/received totals |

No task files exist for this feature (it was implemented directly rather than through a task decomposition), so the Task(s) column is `(n/a)` throughout.

## Summary

| Class | Critical | High | Medium | Low | Total |
|---|---|---|---|---|---|
| Orphan upstream | 0 | 0 | 0 | 0 | 0 |
| Orphan downstream | 0 | 0 | 1 | 0 | 1 |
| Broken reference | 0 | 0 | 0 | 0 | 0 |
| Status inversion | 0 | 0 | 1 | 0 | 1 |
| **Total** | **0** | **0** | **2** | **0** | **2** |

## Findings

### 1. Test plan described the opt-in behavior the code no longer had

**Feature:** 25-operational-telemetry
**Class:** Orphan downstream
**Severity:** Medium
**Direction:** Downward
**Edge(s):** code -> tests
**Location:** `.sdlc/features/25-operational-telemetry/tests.md` (TC-5, TC-21, TC-22)
**Finding:** Three test cases asserted the pre-default-on behavior: TC-5 "disabled recorder is a no-op", TC-21 "opt-in, record, inspect, export", and TC-22 "no opt-in leaves no trace". The implemented behavior is default-on with opt-out, so those cases described a contract the code does not satisfy.
**Impact:** A reader would have concluded the feature was opt-in and written tests against the wrong expectation.
**Recommendation:** Rewrote the three cases for default-on and per-run opt-out, and added TC-10b, TC-6b, TC-32, and TC-33 for the migration, byte totals, and toggle paths that the implementation actually added.
**Status:** Applied.

### 2. No review findings recorded for any artifact

**Feature:** 25-operational-telemetry
**Class:** Status inversion
**Severity:** Medium
**Direction:** Upward
**Edge(s):** every review-* edge in the feature
**Location:** `.sdlc/features/25-operational-telemetry/` (no `review-*.md` present)
**Finding:** The feature was created and implemented without running the matching `review-*` skills, so no `review-requirements.md`, `review-specification.md`, `review-telemetry.md`, `review-observability.md`, `review-tests.md`, or `review-implementation.md` exists. The artifacts carry `status: draft` and were nonetheless implemented and merged.
**Impact:** Review-approval monotonicity is inverted: code exists downstream of artifacts with no approved review, so there is no independent check that the requirements, spec, and test plan are internally sound.
**Recommendation:** Run the review chain on this feature when convenient, starting with `review-requirements` and `review-specification`. This is a process gap, not a correctness defect, and it does not block the merged change.
**Status:** Reported, not fixed (running review skills is a separate step, not a propagation rewrite).

## Fixes Applied

| Finding / Update | Fix applied | Files changed |
|---|---|---|
| cli-design drift (items 1-7) | rewrites applied (`status: in-review`) | `.sdlc/features/25-operational-telemetry/cli-design.md` |
| specification drift (items 8-10) | rewrites applied (`status: in-review`) | `.sdlc/features/25-operational-telemetry/specification.md` |
| tests drift (finding 1) | rewrites applied (`status: in-review`) | `.sdlc/features/25-operational-telemetry/tests.md` |
| opt-in wording (item 12) | rewrites applied (`status: in-review`) | `.sdlc/features/25-operational-telemetry/requirements.md`, `telemetry.md`, `observability.md` |
| No review files (finding 2) | none; requires running review skills | (none) |

Upward review regressions were not applicable: no `review-*.md` files exist for this feature to regress.

## Recommended Actions

| Priority | Action | Owner decision |
|---|---|---|
| Medium | Run `review-requirements`, `review-specification`, `review-telemetry`, `review-observability`, and `review-tests` on FEAT-25, then `review-implementation` against the merged code | run the review chain |
| Low | Add the completed feature to `.sdlc/context/architecture.md` feature references if a feature index is maintained | confirm convention |
