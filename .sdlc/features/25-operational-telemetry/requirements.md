---
issue: "#25"
title: "Operational telemetry for API and cache performance"
status: in-review
revision: 1
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# Requirements: Operational telemetry for API and cache performance

## Overview

ghx performs every unit of work over an unnamed operation: a GraphQL page, a cache query, a save batch.
Today nothing records how long any of them takes, so performance work and the mock server's simulation generator both rely on assumed costs rather than measured ones.
This feature adds local, default-on, failure-isolated telemetry that records per-call durations and call metadata for GraphQL operations and cache backend operations, reports a per-run summary, and makes the recorded distributions available to the simulation generator so generated scenarios can model real latency.
The immediate consumer is the question "what is the optimal algorithm to pull issues and PRs into cache", which cannot be answered without a per-call cost series; the same series tells us how long a REST page or a GraphQL query actually takes across many executions.

## Stakeholders

| Stakeholder | Interest |
|---|---|
| Developer / maintainer | Per-call latency data to choose a cache-population algorithm and detect regressions |
| Simulation author | Real measured latency distributions to feed realistic `mockserver` scenarios |
| CLI user | Visibility into where cache time goes, without any behavior or privacy change |
| Security / privacy reviewer | Assurance the tool stays offline-first, default-on but switchable, and never phones home |

## Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Requirement |
|---|---|---|
| FR-1 | Must | The system shall record one telemetry event per GraphQL API call, capturing operation name, host and repository, HTTP status, duration, page size, request and response payload sizes, returned node count, whether a pagination cursor was supplied, attempt count, and whether the call was retried |
| FR-2 | Must | The system shall record one telemetry event per cache backend operation, capturing backend kind, operation name, repository, duration, and the number of items read or written |
| FR-3 | Must | The system shall label each GraphQL call with the caller's operation kind (at minimum: issue list, issue get, issue full fetch, PR list, PR get, PR full fetch, PR scan, PR full page) so per-kind distributions are separable |
| FR-4 | Must | The system shall write telemetry events to a local SQLite database whose path resolves from an explicit flag, then an environment variable, then a default under the user data directory |
| FR-5 | Must | The system shall enable recording by default, keep it local-only, and provide a global command to turn it off (`ghx telemetry disable`) plus per-run opt-outs (`--telemetry=false`, `GHX_TELEMETRY=0`) |
| FR-6 | Must | The system shall emit a per-run summary after a `cache` run, reporting per-kind call counts, duration totals and percentiles, retry count, and total elapsed time |
| FR-7 | Should | The system shall provide a `telemetry` command that reads the database back and reports counts, duration percentiles, and retry rates grouped by operation kind |
| FR-8 | Should | The system shall provide a machine-readable export of recorded events as JSON, so the benchmark script and external analysis can consume it |
| FR-9 | Should | The system shall expose the recorded duration distribution per operation kind to the mock server simulation generator, which can inject per-kind latency when configured to do so |
| FR-10 | Should | The `cache` command shall report total elapsed time and a per-page timing breakdown on completion |
| FR-11 | May | The system shall record the GraphQL rate-limit remaining value per call when the response carries it, so point consumption can be modelled |

## Non-Functional Requirements

Order rows by priority: Must first, then Should, then May.

| ID | Priority | Category | Requirement |
|---|---|---|---|
| NFR-1 | Must | Performance | Telemetry recording shall add no more than 1 ms of median overhead per instrumented call when enabled, and operations shall never block on the telemetry write path |
| NFR-2 | Must | Reliability | A telemetry failure (unwritable path, locked database, serialization error) shall never fail, slow, or alter the exit code of the command that triggered it |
| NFR-3 | Must | Security | Recorded events shall contain no tokens, authorization headers, request bodies, or response bodies |
| NFR-4 | Must | Privacy | No event shall leave the machine; the feature shall not add any network egress path |
| NFR-8 | Should | Usability | The global on/off state shall be settable and inspectable without editing a file by hand (`ghx telemetry enable|disable|status`) |
| NFR-5 | Should | Operability | The telemetry database shall be a single standard SQLite file, inspectable with ordinary SQLite tooling, with an append-only schema designed for additive evolution |
| NFR-9 | Should | Operability | The summary shall report total bytes sent and received per operation kind, so payload volume is visible alongside latency |
| NFR-6 | Should | Compatibility | Telemetry shall be available for both storage backends (SQLite and file) and shall not require a new external dependency |
| NFR-7 | Should | Usability | Enabling and disabling telemetry shall be discoverable from the command help of the commands it affects |

## Constraints

- The tool is offline-first and must never phone home; this is an existing architectural decision (`.sdlc/context/architecture.md`, `.sdlc/features/FEAT-0008-sqlite-backend/telemetry.md`), and it forbids any remote analytics backend.
- `github.Client.Query` (`internal/github/client.go:151`) is the single funnel for every GraphQL call and is the natural instrumentation seam; `internal/cache.Store` is the single interface for cache operations.
- The GraphQL client's operation name is not available at the seam today, so callers must supply it.
- Recording must be safe under the concurrency used by `FetchPRsUpdated` (`prBlocksInFlight` parallel chains), so event writes need to tolerate concurrent callers.
- The simulation generator's `SimulationConfig` (`internal/mockserver/simulation.go:16`) currently models volume only; latency is additive scope, not a change to the volume model.
- External dependencies stay minimal; the preferred implementation reuses the existing `modernc.org/sqlite` dependency.

## Acceptance Criteria

Every FR and NFR shall have at least one acceptance criterion.

Order criteria by FRs first (sorted by ID), then NFRs (sorted by ID).

Acceptance criteria verify how a requirement is proven done, they do not restate it.
Write concrete, scenario-based criteria (happy path, edge cases and error states where applicable).
Write each criterion as a fenced `gherkin` block with a tag matching its requirement ID, so the criteria are parseable and later executable via BDD tooling (pytest-bdd, cucumber).
Multiple scenarios per requirement are allowed; tag each with the requirement ID.

- [ ] **FR-1**

    ```gherkin
    @FR-1
    Scenario: A successful GraphQL call is recorded
      Given telemetry is enabled and pointed at a temporary database
      When an operation issues one GraphQL query that succeeds
      Then the database contains one event with the operation name, host, repository, HTTP 200 status, a non-zero duration, the page size, the request and response sizes, the number of returned nodes, and attempt count 1
    ```

    ```gherkin
    @FR-1
    Scenario: A retried call records both attempts
      Given telemetry is enabled and the server fails the first attempt with a retryable response
      When an operation issues one GraphQL query that eventually succeeds
      Then a single call event is recorded with attempt count 2 and retried marked true
    ```

- [ ] **FR-2**

    ```gherkin
    @FR-2
    Scenario: A cache query is recorded with its result size
      Given telemetry is enabled and a repository cache holds 10 issues
      When a query filtered to a subset of those issues is executed
      Then the database contains one cache event naming the operation, the backend kind, and the number of items returned
    ```

- [ ] **FR-3**

    ```gherkin
    @FR-3
    Scenario: Operation kinds are separable
      Given telemetry is enabled
      When a cache run fetches issues and PRs
      Then the recorded events distinguish the issue fetch, the PR scan walk, and the PR full-page fetch by their operation kind
    ```

- [ ] **FR-4**

    ```gherkin
    @FR-4
    Scenario: Path resolution precedence
      Given both an explicit telemetry path flag and a telemetry path environment variable are set
      When telemetry is enabled
      Then events are written to the path from the flag, not the environment variable
    ```

- [ ] **FR-5**

    ```gherkin
    @FR-5
    Scenario: A plain run records by default
      Given neither a telemetry flag nor a telemetry environment variable is set
      And no global preference has been saved
      When any command runs
      Then a telemetry database is created and written to
    ```

    ```gherkin
    @FR-5
    Scenario: The global switch disables recording
      Given the global preference was set with the telemetry disable command
      When any command runs without an explicit override
      Then no telemetry database is created or written to
    ```

    ```gherkin
    @FR-5
    Scenario: A single run can opt out without changing the global setting
      Given telemetry is enabled globally
      When a command runs with the telemetry flag explicitly false
      Then no events are recorded for that run
      And the global setting remains enabled
    ```

    ```gherkin
    @FR-5
    Scenario: Recording makes no network calls
      Given telemetry is enabled and no network is reachable except the mock server
      When a command runs end to end
      Then no request is made to any host other than the configured API endpoint
    ```

- [ ] **FR-6**

    ```gherkin
    @FR-6
    Scenario: A cache run prints a timing summary
      Given telemetry is enabled
      When a cache run completes against a mock repository
      Then the output reports per-kind call counts, total and percentile durations, retry count, and total elapsed time
    ```

- [ ] **FR-7**

    ```gherkin
    @FR-7
    Scenario: The telemetry command summarizes a populated database
      Given a telemetry database holding events for three operation kinds
      When the telemetry summary command runs against it
      Then the output lists each operation kind with its count, median duration, and retry rate
    ```

- [ ] **FR-8**

    ```gherkin
    @FR-8
    Scenario: Events export as JSON
      Given a telemetry database holding events
      When the telemetry export command runs with JSON output selected
      Then stdout is valid JSON containing every recorded event with its fields
    ```

- [ ] **FR-9**

    ```gherkin
    @FR-9
    Scenario: Simulation injects recorded latency
      Given a telemetry database with a recorded duration distribution for the PR full-page kind
      When the mock server is started with latency injection enabled from that database
      Then it delays each simulated PR full-page response in a way consistent with the recorded distribution
    ```

- [ ] **FR-10**

    ```gherkin
    @FR-10
    Scenario: Cache reports elapsed time and per-page breakdown
      Given telemetry is enabled
      When a cache run completes
      Then the output reports total elapsed time and a per-page timing breakdown for issues and PRs
    ```

- [ ] **FR-11**

    ```gherkin
    @FR-11
    Scenario: Rate limit remaining is captured when present
      Given telemetry is enabled and the server returns a rate-limit remaining header
      When a GraphQL call completes
      Then the recorded event carries the remaining value
    ```

- [ ] **NFR-1**

    ```gherkin
    @NFR-1
    Scenario: Instrumentation overhead stays bounded
      Given a benchmark that runs an instrumented operation with telemetry enabled and with telemetry disabled
      When the median durations of both are compared
      Then the enabled median exceeds the disabled median by less than 1 ms
    ```

- [ ] **NFR-2**

    ```gherkin
    @NFR-2
    Scenario: A broken telemetry sink does not break the command
      Given telemetry is enabled and points at an unwritable path
      When a cache run executes
      Then the command completes successfully with its normal output and exit code 0
    ```

- [ ] **NFR-3**

    ```gherkin
    @NFR-3
    Scenario: Secrets never reach the database
      Given telemetry is enabled and a token is configured
      When any instrumented command runs
      Then no stored event field contains the token value or an authorization header
    ```

- [ ] **NFR-4**

    ```gherkin
    @NFR-4
    Scenario: Telemetry adds no egress
      Given telemetry is enabled and a request-recording mock server is the only reachable host
      When a full command runs
      Then the mock server received only the API queries it already expected
    ```

- [ ] **NFR-5**

    ```gherkin
    @NFR-5
    Scenario: The database is standard and append-only
      Given a telemetry database created by ghx
      When it is opened with standard SQLite tooling and a new event is inserted by the tool
      Then existing rows are unchanged and the new row is readable without a schema migration
    ```

- [ ] **NFR-6**

    ```gherkin
    @NFR-6
    Scenario: Both storage backends are instrumented
      Given telemetry is enabled
      When the same cache run is executed once with the SQLite backend and once with the file backend
      Then both runs produce cache events tagged with their respective backend kind
    ```

- [ ] **NFR-7**

    ```gherkin
    @NFR-7
    Scenario: Help surfaces the telemetry controls
      Given the CLI is built
      When help is requested for the cache command and the root command
      Then the telemetry flag, its environment variable fallback, and its default state are documented in the output
    ```

- [ ] **NFR-8**

    ```gherkin
    @NFR-8
    Scenario: The global switch is settable and inspectable
      Given the CLI is built
      When the telemetry disable command runs, then the status command runs
      Then the status output reports telemetry as disabled and names the config path
    ```

- [ ] **NFR-9**

    ```gherkin
    @NFR-9
    Scenario: The summary totals payload volume per kind
      Given a telemetry database with recorded response sizes for two operation kinds
      When the telemetry summary command runs
      Then each kind row reports total bytes sent and received
      And a total row sums those across kinds
    ```

## Conflicts

None identified yet.

## Open Questions

1. Resolved: telemetry is enabled by default, with a persisted global switch (`ghx telemetry disable`) and per-run opt-outs (`--telemetry=false`, `GHX_TELEMETRY=0`); see `.sdlc/knowledge/decisions/1-telemetry-default-on.md`.
2. Should event retention be bounded (for example, events older than N days pruned on write), or is unbounded growth acceptable for a local developer tool?
3. Should the simulation latency injection require an explicit exported distribution file, or read the telemetry database directly?
4. Should the benchmark script (`scripts/benchmark.py`) consume the exported JSON in this feature, or in a follow-up?
