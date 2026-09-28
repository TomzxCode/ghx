---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Requirements: SQLite backend for issue/PR storage

## Overview

Introduces a SQLite-backed storage backend for cached issues and pull requests as an alternative to the file-per-item JSON cache, so that listing, searching, and filtering on large cached repositories use indexed SQL queries instead of full directory scans.
The file-based backend remains available and selectable, and existing caches can be migrated to SQLite without data loss.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| CLI user | Fast list/search/filter on large cached repos |
| AI agent / automation | Low-latency repeated reads of cached data for batch workflows |
| Developer / maintainer | A scalable storage layer that preserves portability and cross-platform builds |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide a SQLite storage backend that persists cached issues and pull requests |
| FR-02 | Must | The SQLite schema shall cover every field persisted by the file-based backend (issue/PR metadata, body, labels, assignees, milestone, comments, review decision, and all timestamp fields) |
| FR-03 | Must | The system shall serve list, filter, and search operations from indexed SQL queries rather than reading and parsing every item file |
| FR-04 | Must | The system shall default to the SQLite storage backend, with the file-based backend selectable as an alternative |
| FR-05 | Must | The system shall provide a migration path from an existing file-based cache to SQLite, either as a one-shot import or lazy migration, with no field loss |
| FR-06 | Should | The SQLite backend shall support searching across issue/PR titles and bodies via SQL (LIKE or full-text) |
| FR-07 | Should | The system shall report storage backend type and database location to the user (e.g. via the existing cache info / status output) |
| FR-08 | May | The system shall support lazy (on-demand) migration of individual items from file cache to SQLite on read |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | On a cache of >=500 issues/PRs, list/filter/search via SQLite shall complete in less than half the wall-clock time of the file-based backend (>=2x speedup), with both baseline and SQLite timings recorded |
| NFR-02 | Must | Reliability | Partial writes and interrupted migrations shall not corrupt the SQLite database (transactional writes) |
| NFR-03 | Must | Data integrity | Every field round-trips through SQLite identically to the file-based backend (migration is lossless) |
| NFR-04 | Must | Compatibility | The SQLite driver shall not require CGO, so existing cross-platform builds (darwin/linux/windows, amd64+arm64) continue to work |
| NFR-05 | Should | Portability | The database shall remain a single inspectable file openable by standard SQLite tooling |
| NFR-06 | Should | Security | The database file shall inherit filesystem permissions consistent with the existing cache directory |

## Constraints

- Cache root is `~/.cache/ghx/cache/<host>/<owner>/<repo>/` (per `internal/cache/store.go:34`)
- Go 1.21 minimum version
- Existing file-based backend must continue to function unchanged when SQLite is not selected
- External dependencies are kept minimal; a CGO-free SQLite driver is required for the cross-compilation pipeline
- No schema changes to the in-memory `github.Issue` / `github.PullRequest` types (SQLite is a storage alternative, not a model change)

## Acceptance Criteria

- [ ] FR-01: A SQLite backend writes and reads back a cached issue and PR round-trip-correctly
- [ ] FR-02 (fields): Migrating a file cache and reading back yields identical `github.Issue`/`github.PullRequest` values for all fields, including comments, labels, assignees, milestone, and nullable timestamps (`closedAt`, `mergedAt`)
- [ ] FR-03 (indexed queries): `issue list` / `pr list` filtered by state, author, assignee, and label compile to indexed SQL queries (verified by query plan or behavior at scale), not a full table scan followed by in-memory filtering
- [ ] FR-04 (selectable, SQLite default): With no backend configured, data is read from and written to the SQLite database; with `--storage file`, behavior matches the file backend; switching back reads the file cache
- [ ] FR-05 (migration): A one-shot `migrate` from a populated file cache produces a SQLite database whose contents equal the source, and re-running it is idempotent
- [ ] FR-05 (error): An interrupted migration leaves the destination database valid and usable (no partial/corrupt rows) and can be re-run to completion
- [ ] FR-06 (search): Searching titles/bodies returns the same result set as the current in-memory search on the same cached data
- [ ] FR-07 (backend reporting): The active backend type and database file path are surfaced in the cache info/status output when SQLite is selected
- [ ] NFR-01 (benchmark): A benchmark on a repository with >=500 issues/PRs records file-based and SQLite timings and shows the SQLite path meeting the >=2x speedup target
- [ ] NFR-04 (CGO-free): `go build` and the release cross-compilation succeed without CGO enabled

## Conflicts

None identified yet.

## Open Questions

1. Resolved: SQLite is the default backend (maintainer decision); the file backend remains selectable.
2. Should the file-based and SQLite backends be kept in sync (dual-write), or is the backend a one-time choice per repository?
3. Is full-text search (FTS5) desired over plain LIKE, given the added schema complexity?
4. Where does the database file live relative to the existing per-repo cache directory, and is one DB per repo or one global DB preferred?
