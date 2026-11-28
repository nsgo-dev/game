# Agent Note: RPG battle runner

Status: implemented

## Problem

Turn-based combat rules lived in templates and lacked deterministic replay, stable turn order and presentation-independent events.

## Decision

`pkg/battle` stores party and enemy fighters, resolves commands in stable agility order, uses `random.Stream` for hit rolls, emits compact events, and snapshots all mutable state including RNG state.

## Alternatives considered

Embedding presentation callbacks in the runner would couple battle rules to a UI timing model. The runner emits events instead, so templates and replay tools can animate or inspect them independently.

## Consequences

The core API is deterministic and reusable, while richer states, formulas, AI and reward integration can be layered on the RPG data package without changing event delivery.

