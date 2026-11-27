# Agent Note: Runtime plugin contract (`pkg/plugin`) and `APIVersion` policy

Status: implemented

## Problem

Runtime Go plugins in `nsgo-dev/game` had no shared entry point. Extending a game with scene components, ECS systems, world snapshot codecs, pixel effects, data table row schemas, console commands, MCP prefabs, or playtest bots required each game and editor binary to hand-wire every registry individually. The ECS scheduler (`entities.Scheduler`) only ordered systems by explicit `Before`/`After` names, leaving third-party plugins with no standard execution phases to attach to without knowing a host game's system names. Meanwhile, linking `pkg/mcp` or `pkg/playtest` directly from a core plugin package would pull heavy inspection, replay, and UI dependencies into every shipped game binary.

## Decision

Introduce `pkg/plugin` for runtime plugin installation and a sibling package `pkg/plugin/devtools` for development-time MCP and playtest contributions, with a strict compile-time contract enforced by `APIVersion` and pre-flight validation.

Key contract invariants:

- **`APIVersion` policy**: `plugin.APIVersion` starts at `1` and is shared across runtime plugins (`plugin.Plugin.API`), `plugin.yaml` manifests (`api`), devtools plugins (`devtools.Plugin.API`), and editor plugins (`editor.Plugin.API`). `APIVersion` is bumped **only on breaking changes** to the plugin contract (removing or renaming fields, changing callback signatures, or breaking stage/lifecycle semantics). Additive fields, new optional contribution slices/maps, and new helper methods stay on the current `APIVersion`. `Install` rejects any plugin whose `API != plugin.APIVersion` with `ErrAPIMismatch` before registering anything.
- **No `Version` field on `Plugin`**: Following `plugin.yaml`, a Go plugin's version is its Go module version resolved by `go.mod` and read from `runtime/debug.ReadBuildInfo` via `plugin.ModuleVersion(modulePath)` (returning `"(devel)"` for local builds or replace directives without a version, and `""` when absent from build info).
- **Atomic pre-flight validation and deterministic order**: `plugin.Install` validates every plugin's identity, API version, duplicate names, and every contributed component, system, pixel effect, data table pattern, and console command name before mutating any host or process-global registry. On any conflict or validation error, nothing is registered. Valid contributions commit in deterministic order: plugins in argument order, slice fields (`Components`, `Systems`, `Console`) in declaration order, maps (`PixelFX`, `RowTypes`) in sorted key order, and `Plugin.Install` escape-hatch hooks last.
- **Standard scheduler stages**: `plugin.Install` (via `plugin.EnsureStages`) registers four anchor pairs once per `*entities.Scheduler` in fixed order: `StageInput` (`"input"`) -> `StageUpdate` (`"update"`) -> `StagePhysics` (`"physics"`) -> `StageLate` (`"late"`), named `plugin.stage.<stage>.start` and `plugin.stage.<stage>.end`. A `System` with `Stage` set receives `entities.After(stage.Start())` and `entities.Before(stage.End())` in addition to its own `Before`/`After` constraints, and anchor names are reserved against collision.
- **Per-world codec application**: `Host.OnWorld(w)` runs every installed plugin's `Codecs` callback on `w` in installation order so games that create multiple worlds or fresh worlds during save restoration apply plugin codecs consistently.
- **Process-global registry ownership (`pixelfx`, `playtest`)**: Process-global registries are guarded by a package mutex and an owner table keyed by `(Name, Module)`. Re-installing the same plugin in a second test or after an editor reload is an idempotent no-op instead of a panic or silent overwrite, while a different plugin or a collision with a built-in entry fails pre-flight with `ErrConflict`.
- **Link-cost split (`pkg/plugin/devtools`)**: `Prefabs` (`mcp.Bridge`) and `Bots` (`playtest.BotFactory`) live in `pkg/plugin/devtools` rather than `pkg/plugin`. Dependency tests enforce that `pkg/plugin` does not directly or transitively import `pkg/editor`, `pkg/mcp`, or `pkg/playtest`, and `pkg/plugin/devtools` does not import `pkg/editor`.
- **Non-mutating lookup helpers**: `entities.Scheduler.HasSystem`, `datatable.Registry.Lookup`, and `mcp.Bridge.HasPrefab` enable exact pre-flight collision detection without mutating registries.

## Alternatives considered

- **Putting `Prefabs`, `Bots`, and `Host.Bridge` directly in `pkg/plugin` behind build tags**: Build tags would either force every plugin author to split their `Plugin()` literal across build-tagged files or break standard builds when tests omit tags. A dedicated `pkg/plugin/devtools` package keeps standard builds clean and lets shipped games avoid linking `pkg/mcp` and `pkg/playtest` without build-tag friction.
- **Rolling back partial registrations after mutating registries**: `pixelfx.Register`, `playtest.RegisterBot`, `entities.Scheduler.AddSystem`, `datatable.Registry.Register`, and `debug.Console.RegisterCommand` have no unregister/rollback APIs (and `pixelfx.Register` panics on duplicate IDs). Pre-flight validation into temporary registries plus mutex-guarded global checks guarantees zero partial mutation without adding destructive unregister APIs to every subsystem.
- **Running `Scheduler.Build()` inside `plugin.Install`**: Games frequently register their own systems after installing plugins, and plugins may declare `Before`/`After` constraints targeting systems registered later. Deferring `Build()` to the host's first `Step`/`Order`/`Build` preserves forward-reference flexibility while still validating empty targets, self-references, unknown stages, and duplicate names immediately.

## Consequences

- `examples/fieldmedic` now exposes `health.Plugin()` and installs it in `LoadWard`, preserving `ComponentType.Spawn` and registering its `health.Stats` snapshot codec via `Host.OnWorld`.
- Downstream issues (`#1273` editor plugin lifecycle/API check, `#1274` `game plugin sync` generating `plugins_gen.go`, `#1275` `game plugin new` scaffold, `#1290` resource kinds and custom bus effects) build directly on `plugin.APIVersion`, `plugin.Plugin`, `plugin.Host`, `plugin.Install`, and `plugin.ModuleVersion`.
- Any future breaking change to `plugin.Plugin` or `plugin.Host` must increment `plugin.APIVersion` and update `plugin.yaml` validation expectations together.
