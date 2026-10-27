---
title: "Repository Deletion"
status: done
---

# Requirements: Repository Deletion

## Overview

Provides a `repo delete` command that removes the locally cached data for one repository.
Today the only way to free a single repository's cache is to delete the whole database by hand, which also removes every other cached repository.
The command makes per-repository cleanup a first-class, backend-independent operation.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| CLI user | Remove stale or unwanted cache data for one repository without losing the rest |
| AI agent | Reset a repository's cache before a controlled refetch |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-01 | Must | The system shall provide a `repo delete [REPO]` command that removes all cached issues, pull requests, and cache metadata for the specified repository |
| FR-02 | Must | The system shall accept the repository as an optional positional `[HOST/]OWNER/REPO` argument, falling back to `--repo` or the current directory's git remote |
| FR-03 | Must | The system shall remove only the specified repository's data, leaving other cached repositories untouched |
| FR-04 | Must | The system shall report whether data was deleted or nothing was cached for the repository |
| FR-05 | Should | The system shall support both the SQLite and file cache backends through the selected `--storage` backend |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-01 | Must | Performance | The operation shall not call the GitHub API; it reads and writes local cache storage only |
| NFR-02 | Must | Reliability | A deletion shall leave no partial state: either the repository's data is fully removed or the operation fails without changing other repositories |
| NFR-03 | Must | Usability | The command shall reject specifying both a positional repository and `--repo` instead of silently preferring one |

## Constraints

- Deletion is limited to the active cache backend
- The command cannot undelete; the next cache, list, or view run refetches from GitHub

## Acceptance Criteria

- [ ] FR-01: `ghx repo delete octocat/hello-world` removes that repository's issues, PRs, and cache metadata
- [ ] FR-02: `ghx repo delete` with no argument uses `--repo` or the git remote; passing both an argument and `--repo` errors
- [ ] FR-03: Other repositories in the same SQLite database or file cache remain readable and listed
- [ ] FR-04: A second deletion prints that no cached data was found
- [ ] NFR-01: The command makes no network calls

## Open Questions

(none)
