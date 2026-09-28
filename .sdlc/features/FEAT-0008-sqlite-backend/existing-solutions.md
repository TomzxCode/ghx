---
issue: "#1"
title: "SQLite backend for issue/PR storage"
status: approved
---

# Existing Solutions: SQLite backend for issue/PR storage

## Overview

Surveyed Go SQLite drivers, schema/migration tooling, and reference patterns for an embedded document-style database.
The codebase has no existing database or indexing code (only the file-per-item `cache.Store`), so a storage abstraction and a new SQLite implementation are needed.
Recommended direction is **hybrid**: build a storage interface around the standard library `database/sql`, adopt the pure-Go `modernc.org/sqlite` driver (to satisfy the CGO-free cross-compilation constraint), and keep schema management simple with an embedded, versioned SQL file rather than a migration framework.

## Search Scope

| Source | Searched | Notes |
|---|---|---|
| Internal codebase | Yes | `rg` for `sqlite\|database/sql\|sql.Open\|FTS\|index` across `internal/` and `cmd/`; only the file-based `cache.Store` and unrelated `stashIndex` matches found; no DB or indexing code exists |
| Open-source | Yes | Go SQLite drivers (modernc.org/sqlite, mattn/go-sqlite3, zombiezen.com/go/sqlite, glebarez/sqlite); query/migration tooling (sqlc, golang-migrate, goose, squirrel) |
| Commercial / SaaS | Yes | Not applicable; the cache is local-first and offline, so no hosted DB product fits |
| Standards / protocols | Yes | SQLite database file format; `database/sql` driver interface; FTS5 full-text search module |
| Reference material | Yes | SQLite "application file format" guidance; WAL mode for read-while-writing; transactional bulk-insert patterns |

## Candidate Solutions

| Solution | Type | License | Maturity | Covers | Gaps |
|---|---|---|---|---|---|
| modernc.org/sqlite | Library (driver) | BSD-3-Clause | Mature / active | FR-01, FR-03, NFR-04 (CGO-free), NFR-05, FR-06 (FTS5) | No higher-level ORM/migration; schema and queries are our responsibility |
| mattn/go-sqlite3 | Library (driver) | MIT | Mature / active | FR-01, FR-03, NFR-05, FR-06 | Violates NFR-04 (requires CGO; breaks cross-compilation) |
| zombiezen.com/go/sqlite | Library (high-level) | BSD-3-Clause | Active | FR-01, FR-03, NFR-04 | Extra abstraction over modernc; non-standard API diverges from `database/sql` |
| glebarez/sqlite (+ GORM) | Library (ORM) | MIT | Active | FR-01, FR-03, NFR-04 | Pulls in GORM ORM (overkill); conflicts with the minimal-dependency constraint |
| sqlc | Tool (codegen) | MIT | Active | FR-03 (type-safe queries) | Adds a codegen build step; limited benefit for this query volume |
| golang-migrate / goose | Tool (migrations) | MIT | Active | Schema versioning | Overkill for a single embedded DB; an embedded versioned schema suffices |
| Internal `cache.Store` | Internal | n/a | n/a | Reference for the data model and the interface contract | File-based; no indexing or search |

## Evaluation

### modernc.org/sqlite

- **Strengths:** Pure Go (transpiled from SQLite C), registers as a `database/sql` driver, no CGO, ships FTS5, single dependency, well-maintained.
- **Weaknesses:** Larger compiled binary than CGO builds; slightly slower than `mattn/go-sqlite3` in micro-benchmarks; transpiled code is hard to debug.
- **Integration effort:** Low-medium. Add the module, open a DB with `sql.Open("sqlite", path)`, and write SQL; no build-pipeline changes beyond `go get`.
- **Cost:** None (permissive license, no runtime cost).
- **Risks:** Low. License is permissive; maintenance is active; the transpiled upstream tracks SQLite releases.
- **Forward compatibility:** Tracks upstream SQLite via semver-tagged releases; additive SQL features are backward-compatible; pinning a major version keeps upgrades safe.

### mattn/go-sqlite3

- **Strengths:** Most widely used Go SQLite driver; fastest; smallest binary; battle-tested.
- **Weaknesses:** Requires CGO, which breaks the existing cross-compilation pipeline (darwin/linux/windows, amd64+arm64) unless CGO is cross-compiled per target.
- **Integration effort:** Medium-high due to per-target CGO toolchain setup for releases.
- **Cost:** None.
- **Risks:** Medium. CGO cross-compilation is fragile and would complicate CI for every release.
- **Forward compatibility:** Stable, but the CGO requirement is the disqualifier regardless of version.

### golang-migrate / goose

- **Strengths:** Mature schema versioning with up/down migrations.
- **Weaknesses:** Adds a runtime dependency and migration files for what is a single embedded database with a known, small schema.
- **Integration effort:** Low, but unnecessary complexity for the schema size.
- **Recommendation:** Skip in favor of an embedded, versioned `schema.sql` plus a `user_version` pragma.

## Recommendation

**Direction:** Hybrid (build the abstraction, adopt modernc.org/sqlite, embed schema)

Adopt `modernc.org/sqlite` as the SQLite driver because it is the only mature option that satisfies NFR-04 (CGO-free) while providing FTS5 for FR-06.
Build a storage `interface` with two implementations, the existing file-based `cache.Store` and a new SQLite store, so FR-04 (selectable backend) and FR-05 (migration) are first-class rather than bolted on.
Use the standard library `database/sql` directly (no ORM) to honor the minimal-dependency constraint, and manage the schema with an embedded, versioned SQL file rather than a migration framework.
Borrow the SQLite "application file format" pattern (one inspectable DB file) and WAL mode + transactional batch inserts to meet NFR-02 (no corruption on interrupted writes) and NFR-05 (portability).

## Sources of Information

- modernc.org/sqlite: `database/sql` driver registration pattern; FTS5 virtual-table usage for title/body search.
- SQLite documentation: WAL journal mode for concurrent readers during writes; `PRAGMA user_version` for lightweight schema versioning; transaction-per-batch for fast, atomic bulk inserts.
- Internal `cache.Store`: the exact field set (`github.Issue`/`github.PullRequest`/`Comment`/`Actor`/`Label`/`Milestone`) the schema and migration must round-trip, and the `CacheInfo` metadata shape.

## Open Questions

1. Is the binary-size increase from the transpiled modernc.org/sqlite acceptable, or should a smaller-footprint approach be evaluated?
2. Should WAL mode be enabled by default (better read concurrency) given the CLI is mostly single-process?
3. Do we want FTS5 now (FR-06 is Should) or defer it to keep the initial schema simpler, using LIKE until search volume justifies it?
