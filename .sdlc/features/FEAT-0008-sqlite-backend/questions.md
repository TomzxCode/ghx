---
feature: FEAT-0008-sqlite-backend
title: "Open questions"
---

# Open Questions

## From needs-assessment

1. How slow is the current backend in practice (measured), and at what cache size does latency become unacceptable for the target users?
2. Are the target use cases cold-start queries (where an index helps most) or repeated queries within one process (where an in-memory cache might suffice)?
3. Is full-text search across issue/PR bodies a desired capability, or is indexed metadata filtering (state, author, labels) sufficient?

## From requirements

1. Should SQLite become the default backend on fresh installs, or require explicit opt-in for the first release?
2. Should the file-based and SQLite backends be kept in sync (dual-write), or is the backend a one-time choice per repository?
3. Is full-text search (FTS5) desired over plain LIKE, given the added schema complexity?
4. Where does the database file live relative to the existing per-repo cache directory, and is one DB per repo or one global DB preferred?

## From existing-solutions

1. Is the binary-size increase from the transpiled modernc.org/sqlite acceptable, or should a smaller-footprint approach be evaluated?
2. Should WAL mode be enabled by default (better read concurrency) given the CLI is mostly single-process?
3. Do we want FTS5 now (FR-06 is Should) or defer it to keep the initial schema simpler, using LIKE until search volume justifies it?

## From codebase-analysis

1. Should the `Store` interface gain query methods (`QueryIssues`/`QueryPRs`) now, or ship v1 with `LoadAll*` backed by SQLite and defer pushed-down predicates until measured (FR-03 strictness)?
2. Is the backend selection global (one choice for all repos) or per-repo (a repo can be migrated independently)?
3. Should the SQLite DB live per-repo (`<repo>/cache.db`) or as one global DB keyed by `(host, owner, repo)` rows?

## From feasibility

1. Is the binary-size increase from `modernc.org/sqlite` acceptable for a tool distributed as pre-built binaries, or does it need evaluation before adoption?
2. Will maintaining two backends long-term be worth it, or should the file backend be deprecated once SQLite is proven (affects default-selection open question)?
