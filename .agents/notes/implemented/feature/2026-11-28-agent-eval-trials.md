# Agent Note: Agent evaluation trials

Status: implemented

## Problem

The agent evaluation suite (`tools/agenteval`) ran each task once, so a live agent's normal run-to-run variance showed up as pass or fail flips, and the report had no statistic that separated a flaky task from a regression (#1750).

## Decision

`-trials N` (default 1, refused below 1) makes `RunSuite` in `tools/agenteval/runner.go` call `runSingleTask` N times per task. Each call stages its own temporary workspace, so trials are independent. `AggregateTaskReports` in `tools/agenteval/report.go` folds them into one `TaskReport` with `trials`, `pass_rate`, `mean_score` and the population `stddev`; `score` becomes the rounded mean, and `passed` holds only when every trial passed, which keeps the existing exit-code gate at least as strict as a single run.

The aggregated report takes its `criteria`, `agent_output` and `agent_error` from the first failing trial (the first trial when all pass), so a failed task always shows the evidence of a failure rather than a passing trial's criteria next to a FAIL status. Duration, turns, tool calls and tokens are summed, because they measure what the whole run cost. The Markdown summary prints the statistics only when there is more than one trial, so single-trial summaries are unchanged.

## Alternatives considered

Repeating grading in one workspace would be faster but would let one trial's edits and build caches influence the next; fresh staging keeps the evidence independent at the cost of runtime.

Keeping every trial's full report in the JSON would preserve more evidence, but it changes the report's shape for every consumer of `tasks[]`; the single aggregated entry keeps the shape and adds four fields.

A pass-rate threshold gate (pass when `pass_rate` is at or above a value) is what #1750 proposes; it is left for a later change, together with report stamping, history and `-compare`.

## Consequences

The scheduled job can trade runtime for confidence by passing `-trials`. Reports gain four always-present fields; with one trial they are `trials: 1`, a pass rate of 0 or 1, the score as the mean and a deviation of 0.
