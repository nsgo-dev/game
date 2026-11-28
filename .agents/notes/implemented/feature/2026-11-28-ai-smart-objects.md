# Agent Note: AI smart objects

Status: implemented

## Problem

World objects had no shared interaction-slot concept, so games duplicated search, claim, release and disabled-object handling for benches, workstations and similar activities.

## Decision

`ai.SmartObjects` registers enabled objects with tagged slots, finds free slots in deterministic ID order, grants exclusive agent claims, releases claims when objects disable, and snapshots claim state.

## Alternatives considered

Using generic attack tokens would not carry object positions, slot tags or behavior names. Per-game registries would duplicate arbitration and make editor/runtime state harder to inspect.

## Consequences

Behavior trees can use claim handles without knowing registry internals. Runtime adapters still need to connect scene components and spatial indexes to the registry.

