# Agent Note: Agent evaluation trials

Status: implemented

## Problem

The agent evaluation suite ran each task once, so run-to-run variance could look like a regression and reports had no aggregate reliability statistics.

## Decision

`agenteval -trials N` runs each selected task in a fresh staged workspace and aggregates the task report with pass rate, mean score and population standard deviation. The default remains one trial for compatibility.

## Alternatives considered

Repeating grading in the same workspace would be faster but would allow one trial's edits and caches to influence the next. Fresh staging preserves independent evidence at the cost of runtime.

## Consequences

Scheduled jobs can trade runtime for confidence by setting `-trials`. Reports carry the evaluated commit and staged docs/skill hash, and `CompareSuiteReports`/`-compare` flags changed criteria only for matching mode and model. History-branch publishing and drop alerts remain workflow follow-up work.

