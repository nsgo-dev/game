### Added

- `game/pkg/plugin`: add runtime plugin contract (`APIVersion`, `Plugin`, `Host`, `Install`, `Host.OnWorld`, `Host.Plugins`, `Host.Has`, `ModuleVersion`) with pre-flight conflict checks, deterministic ordering, standard scheduler stages (`StageInput`, `StageUpdate`, `StagePhysics`, `StageLate`, `EnsureStages`), and idempotent process-global `pixelfx` ownership (#1272).
- `game/pkg/plugin/devtools`: add separate development-time plugin installer for MCP bridge prefabs (`PrefabSpec`) and playtest bot factories (`playtest.BotFactory`) so shipped game binaries do not link `pkg/mcp` or `pkg/playtest` (#1272).
- `game/pkg/entities`, `game/pkg/datatable`, `game/pkg/mcp`: add `Scheduler.HasSystem`, `Registry.Lookup`, and `Bridge.HasPrefab` inspection helpers, and expose `health.Plugin()` installed by `examples/fieldmedic` (#1272).
