# Agent Note: Input behavior configs

Status: implemented

## Problem

Reusable platform, directional, tile, vehicle, drag, turret, line-of-sight and path behaviors existed only as template-specific Go logic or separate packages.

## Decision

`sceneformat.BehaviorConfig` provides a versioned, deterministic configuration vocabulary and validates the supported behavior kinds and numeric constraints. Runtime components can consume this schema without changing scene file structure.

## Alternatives considered

Adding one unrelated component type per behavior would duplicate version and validation rules. A shared config keeps editor and runtime schemas aligned while allowing behavior implementations to evolve independently.

## Consequences

Scene tooling can validate behavior settings before runtime. The current change defines and checks the schema, while per-behavior stepping and physics integration can be added incrementally.

