# Agent Note: Fixed-step gameplay effects

Status: implemented

## Problem

The repository's stats effects already provided atomic application, stacking, refresh and fixed-step expiry, but the ability documentation did not identify them as the deterministic gameplay-effect boundary.

## Decision

Abilities emit deterministic events and games apply named `stats.Effect` values on the same fixed-step loop. Existing `Apply`, `Refresh`, `Step`, `Snapshot` and `Restore` contracts remain unchanged while richer tags and periodic policies remain future extensions.

## Alternatives considered

Adding a second effect runtime in `ability` would duplicate stats lifetime and snapshot logic. Moving timed modifiers to wall-clock durations would break pause and replay determinism.

## Consequences

Current buffs and debuffs can be reused by ability implementations with deterministic replay behavior. Full gameplay-effect tags, immunity and periodic damage require a later schema extension.

## Verification

Existing stats tests cover effect stacking, refresh net changes, fixed-step expiry and snapshots. Ability and stats docs now state the shared boundary. Targeted tests and `go tool nsgo check` pass.
