---
title: "Repository Deletion"
status: done
session_link: "sync-sdlc 2026-10-27"
---

# Test plan: Repository Deletion

## Scope

Covers `ghx repo delete`, the `cache.Store.DeleteRepo` operation on both backends, and the `repo.delete` JSON-RPC method.
Out of scope: deleting every cached repository, deleting the telemetry database, and resetting data between backends (out of scope in the specification).

## Unit tests

| ID | Description | Input | Expected Output |
|---|---|---|---|
| TC-1 | `repo delete` removes the cached repository and reports the deletion, on the default SQLite backend and the file backend | One saved issue for `acme/widget`; `runRepoDelete` with the argument `acme/widget` | Output contains `Deleted cached data for acme/widget.` and `ListCachedRepos` afterwards returns nothing (`cmd/repo_test.go` `TestRepoDelete`) |
| TC-2 | A second deletion of the same repository reports nothing cached | `runRepoDelete` repeated after TC-1 | Output contains `No cached data found for acme/widget.` (`cmd/repo_test.go` `TestRepoDelete`) |
| TC-3 | The command rejects an argument combined with `--repo` | `runRepoDelete` with the argument `acme/widget` and `repoFlag` set to `acme/other` | An error is returned (`cmd/repo_test.go` `TestRepoDelete_RejectsArgumentWithRepoFlag`) |
| TC-4 | The file backend removes the repository directory and reports `false` when nothing is cached | Issue, PR, and cache metadata saved under `github.com/owner/repo`; `DeleteRepo` called twice | First call returns `true` and the repository directory is gone; second call returns `false` (`internal/cache/store_test.go` `TestFileStoreDeleteRepo`) |
| TC-5 | The SQLite backend deletes only the target repository's rows and metadata | Repositories `acme/alpha` and `acme/beta` seeded with issues, PRs, and metadata; `DeleteRepo(acme/alpha)` | `alpha` issues, PRs, and cache metadata are gone; `ListCachedRepos` returns only `beta`; second call returns `false` (`internal/cache/sqlite_store_test.go` `TestSQLiteDeleteRepo`) |

## Integration tests

| ID | Description | Preconditions | Expected Outcome |
|---|---|---|---|
| TC-6 | The `repo.delete` JSON-RPC method deletes through the server's store | JSON-RPC server with a seeded cache; `repo.list` then `repo.delete` with no params | `repo.delete` returns `Deleted: true` (`internal/rpc/methods_test.go` `TestRepoListAndDelete`) |

## End-to-end tests

No end-to-end CLI tests in `internal/integration` cover `repo delete`.

## Edge cases and failure scenarios

| ID | Scenario | Expected Behavior |
|---|---|---|
| TC-7 | Deleting a repository with nothing cached | `DeleteRepo` returns `false` with a nil error and the command prints the none-found message (TC-2, TC-4, TC-5) |
| TC-8 | Other repositories in the same SQLite database | Remain readable and listed after another repository is deleted (TC-5) |
| TC-9 | Mixed-case repository coordinates | `normalizeStore` canonicalizes host, owner, and repo to lower-case before the backend delete (`internal/cache/normalize.go:139-143`); not covered by a direct test |
| TC-10 | Interrupted SQLite deletion | The three table deletes run in one transaction, so no partial state is possible (`internal/cache/sqlite_store.go:813-840`); the rollback path is structural and not exercised by a test |
| TC-11 | Empty `host/owner` directories left behind by the file backend | Accepted per the specification's risks; ignored by `repo list` |

## Test infrastructure

- `withCleanStorageFlags`, `resetTelemetryState`, and per-test temp storage and telemetry flags isolate the command tests (`cmd/repo_test.go`)
- `newTempStore`, `sampleIssue`, and `samplePR` provide a throwaway file backend; `NewSQLiteStore(t.TempDir())` provides a throwaway SQLite backend (`internal/cache` tests)
- The RPC test uses the mock GraphQL server harness with a real store (`internal/rpc/methods_test.go`)

## Coverage matrix

| Requirement | Test Cases |
|---|---|
| FR-01 | TC-1, TC-4, TC-5 |
| FR-02 | TC-1 (positional argument) and TC-3 (conflict rejection); the no-argument fallback through `--repo` or the git remote (`cmd/root.go:104` `getRepo`) is not exercised by a delete test |
| FR-03 | TC-5, TC-8 |
| FR-04 | TC-1, TC-2, TC-4, TC-5 |
| FR-05 | TC-1, TC-4, TC-5 |
| NFR-01 | TC-1 (local stores only; no network client in the delete path) |
| NFR-02 | TC-5, TC-10 |
| NFR-03 | TC-3 |
