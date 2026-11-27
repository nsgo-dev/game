### Added

- `game/sceneformat`: `ComponentType.Spawn`, `SpawnOptions`, and `SpawnEntitiesWith` turn YAML scene components into typed ECS components in `entities.World`, resolving registered defaults up front, running in deterministic depth-first and sorted component-name order, and rolling back created entities on error (#1282).

### Changed

- `game/sceneformat`: `SpawnConnected` now forwards its `ComponentRegistry` to `SpawnEntitiesWith`, attaching typed ECS components for connected scenes while preserving existing behavior for registries without `Spawn` (#1282).
- `examples/fieldmedic`: `health.Type` now provides a `Spawn` factory that populates a typed `health.Stats` ECS store, and `LoadWard` spawns patients through `sceneformat.SpawnEntitiesWith` instead of reading `ObjectDoc` components by hand (#1282).
