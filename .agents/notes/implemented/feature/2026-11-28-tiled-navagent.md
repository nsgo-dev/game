# Agent Note: Tiled navigation agent corridor boundary

Status: implemented

## Problem

`TiledNavMesh` already supports streamed tiles, dynamic obstacles, salted polygon references and per-tile path invalidation, but movement agents only consumed a single-piece `NavMesh`. Games had to run tiled searches manually and could not reuse deterministic agent movement or targeted invalidation.

## Decision

`pathfind3d.TiledNavAgent` owns a `TiledNavPathResult`, follows its waypoint path at fixed durations, and registers `WatchPath` for the result's tile footprint. A tile mutation marks that agent for replan through the watcher, while agents on other tiles remain untouched. Replanning replaces the corridor and watcher, and unloading the current tile produces `Unreachable`. The agent exposes corridor, path and replan state and has no goroutines or wall-clock dependency.

The implementation uses the existing `TiledNavMesh.FindPathWithFilterAndOptions` and `NavTileChange` APIs. It keeps the existing plain `NavAgent` constructors unchanged and leaves group budgeting and 2D integration for follow-up work.

## Alternatives considered

- Rebuild every agent on every mesh revision. This is simple, but streamed worlds turn one tile change into an O(agent count) spike.
- Make `TiledNavAgent` depend on rendering or world streaming packages. That would couple navigation to a client runtime and prevent headless simulation.

## Consequences

Tiled navigation now has a reusable deterministic movement boundary and targeted invalidation. The first version provides a focused 3D agent and does not yet add the matching 2D tiled agent or group scheduler integration, so games with thousands of agents should still coordinate budgets at a higher layer.
