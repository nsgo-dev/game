# Agent Note: Deterministic room state deltas

Status: implemented

## Problem

Non-entity room state needed a common versioned delta and visibility contract instead of hand-written messages (#1693).

## Decision

`roomstate.State` stores JSON-compatible fields, increments a monotonic version on every mutation, and emits sorted dirty-field deltas. Owner and team views filter values at encode time, while deletes emit null markers.

## Alternatives considered

- Send the entire state every tick: rejected because it wastes bandwidth and makes hidden fields harder to audit.
- Depend on map iteration order: rejected because callbacks and encoded bytes must be deterministic.

## Consequences

Games get a small transport-neutral delta core that can be generated from a schema later. Callers retain control over transport epochs, acknowledgements, and schema-specific typed wrappers.
