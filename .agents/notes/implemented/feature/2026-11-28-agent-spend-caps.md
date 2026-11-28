# Agent Note: Agent spend tracking and caps

Status: implemented

## Problem

The editor tracked request counts but not token or cost usage, so a long request or automation could consume a weekly budget without visibility.

## Decision

`AgentUsageStats` records weekly input, cached-input, output tokens and estimated USD cost. `AgentSettings` stores optional weekly and per-request spend caps and a tool-call cap. `RecordAgentUsage` resets counters at the weekly boundary and `AgentWeeklySpendLimitReached` provides the gate used by request callers.

## Alternatives considered

Replacing request counts with spend would break existing usage displays and limits. The counters remain and spend is additive, allowing a gradual UI and runner migration.

## Consequences

Callers must report model usage after each response to populate totals. The cap query is side-effect free and can be applied to built-in and automation entry points.

