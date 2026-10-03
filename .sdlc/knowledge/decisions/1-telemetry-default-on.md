---
issue: "#25"
title: "Telemetry enabled by default with a global toggle"
status: Accepted
session_link: "http://localhost:10000/?session=ses_fbbd6ecf9ffeBDsTFy7ccdFJM6"
---

# Decision: Telemetry enabled by default with a global toggle

**Date:** 2026-10-03
**Status:** Accepted
**Deciders:** Tom Rochette

---

## Context

The operational telemetry feature was designed opt-in: recording only happened with `--telemetry` or `GHX_TELEMETRY=1`.
In practice this produced no data, because every measurement required remembering a flag, and an unmeasured CLI cannot answer the questions the feature exists for (which cache-population algorithm is fastest, what latency a simulation should model).

The project is offline-first and previously documented "No observability" (`.sdlc/context/architecture.md`).
Reversing the default therefore needs an explicit, recorded justification rather than a silent behavior change.

## Options Considered

### Option A: Enabled by default, with an explicit global and per-run off switch *(chosen)*

Recording happens unless turned off.
A persisted setting (`config.json` beside the database) is set by `ghx telemetry enable|disable`; a single run opts out with `--telemetry=false` or `GHX_TELEMETRY=0`.
Resolution order is flag, then environment variable, then persisted setting, then default (on).

**Pros:**
- Performance evidence accumulates without anyone remembering a flag.
- The off switch is one command, and per-run opt-out never mutates the global setting.
- Data stays local; nothing about the offline-first guarantee changes.

**Cons:**
- Reverses the documented "no observability" stance and the original opt-in requirement.
- A user who never reads the docs is recorded without having opted in.

### Option B: Keep opt-in, add only the toggle command

Recording stays off until enabled, but `ghx telemetry enable` makes enabling persistent.

**Pros:**
- Preserves the original privacy stance and the "no observability" decision.
- Still solves the "must remember a flag on every run" problem once enabled.

**Cons:**
- Continues to produce no data for anyone who never runs `enable`, which is the observed failure mode.

### Option C: Ask on first run

Prompt once whether to enable, persist the answer.

**Pros:**
- Explicit consent.

**Cons:**
- Interactive prompts in a CLI that is scripted and piped are hostile; adds a first-run special case for a background concern.

## Decision

Option A.
Telemetry is enabled by default; `ghx telemetry disable` turns it off globally, and `--telemetry=false` / `GHX_TELEMETRY=0` opt out per run.
The data remains local-only and is never transmitted, so the offline-first commitment is preserved.
This supersedes the opt-in wording in the feature's original requirements.

## Consequences

**Positive:**
- The feature now produces the evidence it was built for without ceremony.
- The reversal is documented, so the "no observability" note in the context files is no longer silently contradicted.

**Negative:**
- A default-on recorder is a privacy-sensitive default; it is acceptable only because the data never leaves the machine and is trivially switchable.
- Adds a persisted config file, a small piece of state the CLI did not previously have.

**Risks:**
- A user could be surprised by disk growth from the database; the mitigation is that `ghx telemetry status` reports it and `ghx telemetry clear` removes it.
- A future feature could be tempted to transmit the data; the recorded decision and the local-only tests are the guardrail.
