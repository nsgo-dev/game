# Agent Note: Net peer bandwidth budget

Status: implemented

## Problem

Replication sent full snapshots or deltas without a per-peer rate limit, allowing one joining or slow peer to consume the entire transport.

## Decision

`ServerWorld.SetPeerBudget` adds a per-peer token budget replenished at a fixed 60 Hz and `ReplicationStats.BytesDeferred` records payloads deferred by the budget. A priority callback is stored as the scheduling hook for per-entity prioritization.

## Alternatives considered

Dropping complete snapshots would leave joining peers permanently unsynchronized. Deferring payloads preserves ordering and lets later entity-level scheduling consume the same budget.

## Consequences

The current budget bounds complete payload transmission and exposes deferred pressure. Per-entity baselines and priority packing remain follow-up work for the full wire-format migration.
