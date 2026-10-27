---
title: "Repository Deletion"
status: done
---

# Specification: Repository Deletion

## Overview

Adds a `DeleteRepo(host, owner, repo) (bool, error)` operation to the `cache.Store` interface and exposes it as `ghx repo delete [REPO]`.
Each backend implements the operation against its own storage: the file backend removes the repository directory, and the SQLite backend deletes the repository's rows from the shared database in a transaction.
The boolean result distinguishes a deletion from "nothing cached", so the command can report either outcome without a prior read.

## Architecture

```
repo delete ──► runRepoDelete()
                     │
                     ├─ resolveRepoArg(args) ──► gitremote.ParseRepo / getRepo
                     │
                     └─ Store.DeleteRepo(host, owner, repo) ──► bool, error
                          ├─ fileStore:   os.RemoveAll(<baseDir>/<host>/<owner>/<repo>)
                          ├─ sqliteStore: DELETE FROM issues/pull_requests/cache_meta (one transaction)
                          └─ InstrumentedStore: delegate + record a cache_other telemetry event
```

## Data Models

No schema changes.
Both backends already key all data by `(host, owner, repo)`.

## API Contracts

### `cache.Store.DeleteRepo`

```
DeleteRepo(host, owner, repo string) (deleted bool, err error)
```

- `deleted` is true when at least one cached item or cache metadata row existed and was removed.
- `deleted` is false with a nil error when the repository has nothing cached.
- Errors are returned unwrapped from the backend; the command adds repository context.

## Sequences

### Delete a cached repository

```
User → CLI → resolveRepoArg → Store.DeleteRepo
                                   ├─ file: stat repo dir → RemoveAll
                                   └─ sqlite: BEGIN → DELETE issues → DELETE pull_requests
                                              → DELETE cache_meta → COMMIT
         CLI ← deleted
         CLI → "Deleted cached data for OWNER/REPO." or "No cached data found for OWNER/REPO."
```

## Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Interface result | `(bool, error)` | Lets the command distinguish deleted from absent without a second query or scan |
| SQLite deletion | One transaction over the three tables | Prevents an interrupted call from leaving items without metadata, or the reverse |
| Repository resolution | Optional positional argument, then `--repo`, then git remote | Matches `stats`, while an explicit conflict error avoids ambiguous precedence |
| Output label | `OWNER/REPO` on github.com, `HOST/OWNER/REPO` elsewhere | Matches `repo list` |

## Risks and Unknowns

1. Empty owner/host directories can remain under the file backend; they are ignored by `repo list` and harmless.

## Out of Scope

- Deleting every cached repository (delete the database or cache directory)
- Deleting the telemetry database
- Resetting or migrating data between backends
