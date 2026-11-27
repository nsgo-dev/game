### Added

- `game/pkg/save`: add `Part`, `StagedPart`, `Resetter`, `Game` registry (`Register`, `RegisterMigration`, `Save`, `Load`, `PeekPart`, `EncodeParts`), generic helpers (`ValuePart`, `FuncPart`, `StagedFuncPart`), and `Persistence` for per-scene object state (destroyed, moved, component fields, self switches and vars) (#1759).
- `game/pkg/save/savetest`: add `RoundTrip` and `RoundTripInto` helpers to verify every registered `save.Game` part round-trips through a save slot (#1759).
- `game/pkg/inventory`, `game/pkg/quest`, `game/pkg/stats`, `game/pkg/ability`, `game/pkg/flow`, `game/pkg/entities`: add `SavePart()` adapters for `save.Game`, plus `stats.Sheet.Snapshot` and `stats.Sheet.Restore` (#1759).
- `game/pkg/sceneformat`: add `ApplyPersistence`, `BuildWithPersistence`, `CapturePersistence`, and `SpawnOptions.Persistence`/`ScenePath` to persist destroyed, moved and component-modified scene objects across reloads (#1759).
