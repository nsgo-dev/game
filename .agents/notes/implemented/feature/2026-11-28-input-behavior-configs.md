# Agent Note: Input behavior configs

Status: implemented

## Problem

Reusable platform, directional, tile, vehicle, drag, turret, line-of-sight and path behaviors (#1752) exist only as template-specific Go logic or separate packages, with no scene-side settings vocabulary.

## Decision

`sceneformat.BehaviorConfig` (`pkg/sceneformat/behaviors.go`) is a versioned (`version: 1`) settings schema with a `kind` discriminator for the eight behaviors. `ParseBehaviorConfig` decodes it with `KnownFields(true)`, refuses a trailing document and runs `Validate`, which checks the version, the kind, non-negative numeric settings, `eight_direction` directions of 4 or 8, and a positive `tile_movement` tile size. Field order is fixed by the struct, so marshaling is deterministic and a parse and save round trip changes nothing.

## Alternatives considered

Adding one unrelated component type per behavior would duplicate version and validation rules. A shared config keeps editor and runtime schemas aligned while allowing behavior implementations to evolve independently.

An `enabled` flag was dropped: with `omitempty` its zero value would have meant every behavior that omits it is disabled, and nothing reads it yet.

## Consequences

This is a schema only, part of #1752. It is not registered in a `ComponentRegistry`, no `BuiltScene.Step` integration, input-action binding, events, handlers or `Snapshot`/`Restore` exist, and the issue's per-kind fields (`config`, `steerSpeed`, `coneOfView`, `tilemapLayer` and others) are not in the schema yet. Those land with the runtime components, which may reshape the schema under a new version.
