# Agent Note: AI smart-object slots

Status: implemented

## Problem

Agents needed a shared way to discover and claim usable world slots such as benches or workstations. Per-game registries allowed two agents to claim the same slot and had no save-friendly occupancy state.

## Decision

`ai.SmartObjects` stores enabled objects with tagged slots and behavior names. Radius queries filter required tags and return stable object/slot order. Claims include the agent ID, reject occupied slots, and are released explicitly or when an object is disabled. Snapshots preserve the complete registry and occupancy.

## Alternatives considered

Tying slots to one behavior-tree action would duplicate search and claim rules across games. Reusing point reservations would lose object tags, behavior metadata and per-slot ownership.

## Consequences

AI systems can share deterministic interaction discovery and save/restore occupancy. Scene component wiring, editor slot placement and 3D variants remain follow-up integration layers.

## Verification

Tests cover deterministic tag queries, exclusive claims, disable callbacks and snapshot restoration. `go tool nsgo test` and `go tool nsgo check` pass for `pkg/ai`.
