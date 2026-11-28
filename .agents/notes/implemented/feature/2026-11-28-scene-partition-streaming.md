# Agent Note: Scene partition streaming core

Status: implemented

## Problem

Scene files loaded as a whole and had no shared deterministic cell assignment for large-world streaming.

## Decision

`sceneformat.Partition` validates cell and load/unload radii, assigns objects by bounds to all touched cells, and computes source coverage in sorted coordinate order. Bounds assignment uses the full object rectangle so large objects are not attached only by origin.

## Alternatives considered

Assigning by object origin is cheaper but leaves large bridges or buildings in the wrong cell. Sorting by distance during loading belongs to the runtime scheduler, while this core keeps cell output deterministic.

## Consequences

Build and runtime streamers can share the same cell math and stable output. Data-layer state, background loading and object integration remain separate runtime concerns.

