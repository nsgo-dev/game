# Agent Note: Deterministic entity-system cadence

Status: implemented

## Problem

The entity scheduler ran every system every fixed step, forcing expensive crowd systems to pay full-rate cost even when their logic could run less often.

## Decision

`entities.Every(n)` adds an explicit per-system cadence. The scheduler runs the first invocation immediately, then skips until each `n`th scheduler step while preserving the existing topological order. The counter is scheduler state and does not depend on entity iteration order or randomness.

## Alternatives considered

Letting each system keep its own modulo counter duplicates cadence logic and makes scheduling behavior harder to inspect. Randomly selecting frames would break deterministic replays.

## Consequences

Systems can opt into deterministic reduced cadence without changing existing systems. Entity filters, LOD components, accumulated per-entity dt, and slice-aware query iteration remain follow-up work.
