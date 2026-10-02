---
date: "2026-10-02"
scope: "FEAT-0002-pr-browsing"
entry: "code"
flags: "fix: false, create-issues: false"
status: complete
---

# Propagation Report

**Date:** 2026-10-02
**Scope:** FEAT-0002-pr-browsing (change set: commit `82941df`, "Add gh-compatible --json field selection to pr list")
**Entry:** code

## Change Set

- `pr list --json <fields>` now takes a comma-separated field list and emits a compact JSON array containing only those fields, with unknown fields rejected; a bare `--json` still selects every field.
- `headRefOid` is added to the `github.PullRequest` model, fetched by the GraphQL queries, persisted by both cache backends, and emitted in JSON.

## Consistency Status

| Feature | Downward updates | Upward drift | IDs | Review order | Status |
|---|---|---|---|---|---|
| FEAT-0002-pr-browsing | 0 needed (tests and docs updated with the change) | 2 findings | clean | 1 inversion | drifted |

## Updates (downward)

| # | Artifact | Edge | Change | Resolution | Status |
|---|---|---|---|---|---|
| 1 | `cmd/pr_json_test.go` | code -> tests | new `--json` field selection and `headRefOid` | added tests for selection, unknown-field rejection, empty array, and real command parsing | applied (in the change commit) |
| 2 | `README.md`, `docs/user-guide/pull-requests.md`, `docs/user-guide/flag-reference.md` | code -> documentation | new flag behavior and field | documented the field list, supported fields, and bare `--json` | applied (in the change commit) |
| 3 | (none) | code -> PR | no PR opened on branch `pr-list-json` | not applicable until a PR is opened | n/a |

No downward rewrite is pending.
The tests and user-facing docs were updated together with the code, so no dependent still describes the old boolean `--json`.

## Questions Raised

| # | Edge | Disagreement | Resolutions | Where recorded |
|---|---|---|---|---|
| 1 | Requirements ↔ Code | `pr list --json <fields>` and `headRefOid` exist in code but no `FR-N` covers them | (a) add a requirement for gh-compatible list JSON field selection, (b) revert the code | report only (no named answerer needed) |
| 2 | Specification ↔ Code | `specification.md` PullRequest omits `headRefOid` and documents no `--json` field-list contract | (a) extend the data model and add the contract, (b) treat the field as out of scope | report only |

## Traceability Matrix (per feature)

### FEAT-0002-pr-browsing

| FR / NFR | Spec section | Plan phase | Task(s) | Test(s) | Code location | Docs | Issue AC |
|---|---|---|---|---|---|---|---|
| FR-01 | Overview / Architecture | (none) | (none) | `cmd/pr_json_test.go`, `internal/integration` | `cmd/pr.go` | README, pull-requests.md | FR-01 |
| FR-02 | Data Models (PullRequest) | (none) | (none) | `internal/integration` | `cmd/pr.go` | pull-requests.md | (none) |
| FR-03 | Overview | (none) | (none) | `internal/cache/filter_test.go` | `internal/github/api.go`, `internal/cache/filter.go` | pull-requests.md | FR-03 |
| FR-04 | Overview | (none) | (none) | `internal/cache/filter_test.go` | `internal/github/api.go` | pull-requests.md | (none) |
| FR-05 | Overview | (none) | (none) | `internal/cache/filter_test.go` | `internal/cache/filter.go` | pull-requests.md | (none) |
| FR-06 | Overview | (none) | (none) | `internal/integration` | `internal/github/api.go` | pull-requests.md | FR-06 |
| FR-07 | Overview | (none) | (none) | (none) | `internal/github/api.go` | pull-requests.md | FR-07 |
| FR-08 | Architecture | (none) | (none) | (none) | `cmd/pr.go` | pull-requests.md | (none) |
| FR-09 | Overview | (none) | (none) | (none) | `cmd/pr.go` | pull-requests.md | FR-09 |
| FR-10 | Architecture | (none) | (none) | `cmd/storage_test.go` | `internal/cache/store.go` | (none) | FR-10 |
| FR-11 | Overview | (none) | (none) | `internal/cache/filter_test.go` | `internal/cache/filter.go` | pull-requests.md | (none) |
| NFR-01 | Technical Decisions | (none) | (none) | `cmd/bench_test.go` | `internal/cache` | (none) | (none) |
| NFR-02 | Technical Decisions | (none) | (none) | `internal/cache/filter_test.go` | `internal/github/api.go` | pull-requests.md | (none) |
| (none) | (none) | (none) | (none) | `cmd/pr_json_test.go` | `cmd/pr_json.go`, `internal/github/types.go` | pull-requests.md | (none) |

The last row is the new behavior: it exists in code and tests with no requirement or spec section behind it.

## Summary

| Class | Critical | High | Medium | Low | Total |
|---|---|---|---|---|---|
| Orphan upstream | 0 | 0 | 0 | 0 | 0 |
| Orphan downstream | 0 | 0 | 2 | 0 | 2 |
| Broken reference | 0 | 0 | 0 | 0 | 0 |
| Status inversion | 0 | 0 | 1 | 0 | 1 |
| **Total** | **0** | **0** | **3** | **0** | **3** |

## Findings

### 1. `pr list --json <fields>` has no requirement
**Feature:** FEAT-0002-pr-browsing
**Class:** Orphan downstream (code without an upstream requirement)
**Severity:** Medium
**Direction:** Upward
**Edge(s):** Requirements ↔ Code
**Location:** `cmd/pr_json.go`, `cmd/pr.go`
**Finding:** The gh-compatible `--json` field-list behavior, including the `headRefOid` field it exposes, is implemented and tested but no functional requirement describes it; FEAT-0002 `FR-08` covers `--json` on `view` only.
**Impact:** A future reader of the requirements would not know list JSON field selection is in scope, and a re-derivation from requirements would drop it.
**Recommendation:** Add an `FR-N` (for example `FR-12`) requiring gh-compatible `pr list --json <fields>` output, with the supported-field list or a reference to it.

### 2. Specification PullRequest omits `headRefOid` and the `--json` contract
**Feature:** FEAT-0002-pr-browsing
**Class:** Orphan downstream (code without spec backing)
**Severity:** Medium
**Direction:** Upward
**Edge(s):** Specification ↔ Requirements, Code ↔ Specification
**Location:** `specification.md` §Data Models (PullRequest); §API Contracts
**Finding:** The `PullRequest` data model table has no `headRefOid` row, and the spec has no contract for the `--json` field list or its unknown-field error.
**Impact:** The spec no longer describes a field the code persists in both backends and emits; a re-implementation from the spec would omit it.
**Recommendation:** Add a `headRefOid | string | not null | Head commit SHA` row and document the `--json` field-selection contract.

### 3. Requirements and specification are unreviewed drafts while the feature is implemented
**Feature:** FEAT-0002-pr-browsing
**Class:** Status inversion (review-approval monotonicity)
**Severity:** Medium
**Direction:** Upward
**Edge(s):** Requirements ↔ Code
**Location:** `requirements.md` (`status: draft`), `specification.md` (`status: draft`), no `review-requirements.md` / `review-specification.md`
**Finding:** Both upstream artifacts are `draft` with no approved review, yet the feature is code-complete, so the review pipeline never ran for this feature.
**Impact:** The requirements and spec were never validated against the implemented intent; the gap in finding 1 is a symptom.
**Recommendation:** Run `review-requirements` and `review-specification` after the artifacts are updated, so the feature's upstream chain reaches an approved state.

## Fixes Applied

None.
This was a report-only run (`--fix` not set).

## Recommended Actions

| Priority | Action | Owner decision |
|---|---|---|
| Medium | Add an `FR-N` for gh-compatible `pr list --json <fields>` (finding 1) | add requirement |
| Medium | Add `headRefOid` and the `--json` contract to `specification.md` (finding 2) | update spec |
| Medium | Review `requirements.md` and `specification.md` to clear the status inversion (finding 3) | run review skills |
