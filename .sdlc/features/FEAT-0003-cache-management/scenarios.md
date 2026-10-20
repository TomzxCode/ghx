# Cache read and refresh scenarios

Design note for the read paths and `ghx cache` after the agreed change.
It describes the target behavior, not the current release.

Decisions this document records:

- No automatic full crawl from reads. Reads use the API when the cache cannot answer them.
- A list stores its list payload and never fetches per-item detail.
- A view fetches detail only when the requested output needs it.
- `ghx cache` remains the only full crawl and the only detail crawl.

## Definitions

State is tracked per data type, so a repository can have complete issues and incomplete pull requests.

| Term | Meaning |
|---|---|
| Cold | No completed crawl and no items for that type. |
| Partial | Some items are cached for that type, but no crawl reached the end. |
| Complete | A crawl of that type reached the end of the connection. |
| Fresh | A complete type refreshed within the configured duration, 60 minutes by default. |
| Detail record | A record that carries comment bodies, reviews, timeline events, and diff sizes, as written by `ghx cache` or a `view` fetch. |

## What each fetch stores

| Payload | Stored fields | Missing fields |
|---|---|---|
| Issue list record | Every field the list query selects, including the comment count | Comment bodies |
| PR list record | Every field the list query selects, including the comment count | Reviews, timeline events, additions, deletions |
| Detail record | Every field the detail query selects | Nothing the API exposes in one page |

A list record is complete for the default `view` output.
It is not complete for `--comments` or `--json`.

## issue list and pr list

| Scenario | Source | Requests | Writes | Notes |
|---|---|---|---|---|
| Cold | API | One per 100 requested items | List records | No per-item detail fetch. |
| Partial | API | Same as cold | List records, newer `updated_at` wins | A partial cache never answers a list. |
| Complete, fresh | Cache | 0 | None | Filters and the limit are applied in SQLite. |
| Complete, stale | API | Same as cold | None | The complete cache keeps its detail records. Freshness is not advanced, so `ghx cache` is still needed. |

Notes:

- The default `--limit` is 1000, so a cold `--state all` list can be up to 10 requests.
- A list never fetches comment bodies, reviews, timeline events, or diff sizes.
- `--app` and `--mention` bypass the cache and use the API, because the cache cannot represent them.
- A stale complete type is not auto-refreshed. An automatic delta on a stale list is an open option, not part of this document.

## issue view and pr view

| Scenario | Source | Requests | Writes | Notes |
|---|---|---|---|---|
| Cold | API | 1 | Detail record | Fetches comment bodies, reviews, timeline, and diff sizes. |
| Partial, item cached and younger than 60 minutes | Cache | 0 | None | Only when the record satisfies the requested output. |
| Partial, item missing or older | API | 1 | Detail record | |
| Complete, fresh | Cache | 0 | None | Only when the item is present and the record satisfies the requested output. |
| Complete, fresh, item missing | API | 1 | Detail record | A number newer than the crawl is a normal miss, not an error. |
| Complete, stale | Cache when the item is younger than 60 minutes and satisfies the request, else API | 0 or 1 | Detail record on an API fetch | The full crawl timestamp is not advanced. |

## When a view needs a detail fetch

| Command | Needs detail when |
|---|---|
| `issue view` | Never. The list record has every field the default view prints. |
| `issue view --comments` | The record has a comment count above zero and no comment bodies. |
| `issue view --json` | Same as `--comments`, because the JSON includes comments. |
| `pr view` | Never. The list record has every field the default view prints. |
| `pr view --comments` | The record has a comment count above zero and no comment bodies. |
| `pr view --json` | Reviews, timeline events, or diff sizes are missing. |

A detail fetch replaces the stored record.

## ghx cache scenarios

`ghx cache` fetches detail records for whole types.
It writes comments, reviews, timeline events, and diff sizes, which is why it is the only crawl `stats` can use.

### Default invocation

`ghx cache` with no `--force`, `--since`, or `--type`.

| State before | Action | Requests | State after |
|---|---|---|---|
| Cold | Full crawl of both types, oldest first | One per 100 issues, one per 100 pull requests | Both complete and fresh |
| Partial | Resume each incomplete type from its cursor, delta each complete type | Pages until the end for incomplete types, usually one page per complete type | Both complete and fresh |
| Complete, fresh | Delta both types from their cursors; three small requests when nothing changed | 3 | Both complete, window advanced |
| Complete, fresh, `--if-stale` | No-op, prints that the cache is still fresh | 0 | Unchanged |
| Complete, stale | Delta both types from their cursors | Usually one page per type | Both complete and fresh |

### Flags

| Invocation | Effect | Notes |
|---|---|---|
| `ghx cache --force` | Discards the resume state and runs a full crawl | Marks the cache complete when both types are fetched. |
| `ghx cache --since DATE` | Delta from an explicit cutoff instead of the stored cursor | Ignored when the cache is incomplete, which resumes the full fetch instead. Cannot be combined with `--force`. |
| `ghx cache --type issues` | Refreshes issues only | Advances issue freshness only. Pull requests keep their state. |
| `ghx cache --type prs` | Refreshes pull requests only | Advances pull request freshness only. Issues keep their state. |
| `ghx cache --cache-duration N` | Sets the freshness window that reads use | The window does not gate `ghx cache`; only `--if-stale` uses it to skip a run. A value of 0 is not stored. |
| `ghx cache --if-stale` | Does nothing while the cache is fresh | Skips only a complete cache within the window; otherwise it delta-refreshes as usual. |
| `ghx cache --retries N` | Retries transient errors such as rate limits and 5xx responses | Total attempts are N plus one. |

### Partial and interrupted runs

- A run persists each page and the resume cursor before fetching the next page.
- An interrupted run leaves previously written records in place and resumes from the last cursor next time.
- A partial run never marks a type complete unless that type reached the end of its connection.
- `--type issues` or `--type prs` updates only the freshness of the type it fetched.
- When the other type was never fetched, reads for that type still use the API.

### Resulting read behavior

| After | issue list | pr list |
|---|---|---|
| `ghx cache` (both types) | Cache while fresh | Cache while fresh |
| `ghx cache --type issues` | Cache while fresh | API until pull requests are fetched |
| `ghx cache --type prs` | API until issues are fetched | Cache while fresh |
| Interrupted, issues complete | Cache while fresh | API |
| Freshness expired | API until `ghx cache` runs | API until `ghx cache` runs |

## Invariants

- A partial type never answers a list.
- A view answers from cache only when the record exists, is within its freshness window, and has the detail the requested output needs.
- A list fetch never issues per-item detail requests.
- A list record never replaces a newer cached record.
- When a type is complete, list fetches do not write records, so `stats` keeps reading full detail.
- `ghx cache` remains the only full crawl and the only detail crawl.

## Known gaps

- The web UI detail endpoint reads the cache only. After list-only reads it can show an issue with no comments until a view or `ghx cache` fills them.
- A repeated list against a partial cache still calls the API. Serving repeats locally needs the complete mirror or a per-query cache.
- The PR detail check is a heuristic. A pull request with zero diff and no reviews or timeline looks like a list record and is re-fetched. That costs one request and never produces wrong data.
