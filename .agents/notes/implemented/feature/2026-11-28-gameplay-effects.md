# Agent Note: Gameplay effects

Status: implemented

## Problem

Ability results stopped at activation and games repeatedly implemented timed or instant stat changes in ad hoc code.

## Decision

Stats effects accept optional gameplay metadata and `Sheet.ApplyEffect` applies their modifiers through the existing fixed-step sheet lifecycle. Version 1 effect files remain valid when metadata is omitted.

## Alternatives considered

Adding a separate combat-effect format would duplicate stat modifier parsing. Extending the existing effect record keeps persistence and recomputation in one place.

## Consequences

The core can share effect metadata across abilities and sheets. Full tag requirements, periodic event emission and advanced stacking policies remain follow-up layers.
