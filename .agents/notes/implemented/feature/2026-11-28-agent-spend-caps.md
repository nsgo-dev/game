# Agent Note: Agent spend caps

Status: implemented

## Problem

Agent usage tracked requests but not token volume or estimated spend, so a long or automated run could exhaust a weekly budget without a visible cost signal.

## Decision

`AgentUsageStats` stores input, cached-input, output token and estimated-cost totals. `AgentSettings` stores optional weekly spend, per-request spend and tool-call caps with non-negative validation. `RecordAgentSpend` updates the week-scoped counters and `AgentSpendCapReached` exposes the weekly guard.

## Alternatives considered

Using request count as a spend proxy was rejected because model calls differ substantially in prompt and output size. Storing API keys or provider billing responses in editor.yaml was rejected because secrets stay in the system keychain and costs are estimates.

## Consequences

Usage screens and runners can enforce spend-aware queueing without changing existing request counters. Wiring every provider's exact pricing and automation scheduling remains follow-up work.
