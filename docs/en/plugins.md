# Plugins

A Go plugin is a Go module that extends a game's runtime, the scene editor, or the `game` CLI. Because iOS and web targets link all native code at compile time, Go plugins are ordinary Go packages wired into the game project through `go.mod` rather than dynamically loaded shared libraries.

```go
import (
	"github.com/nsgo-dev/game/pkg/gameplugin"
	"github.com/nsgo-dev/game/pkg/gameplugin/manifest"
)
```

## Runtime plugins (`pkg/gameplugin`)

A plugin's runtime Go package exports a constructor returning a `gameplugin.Plugin` (conventionally `func Plugin() gameplugin.Plugin`), and the game installs every active plugin onto a `gameplugin.Host` with `gameplugin.Install`. `pkg/gameplugin` never imports `pkg/editor`, `pkg/mcp`, or `pkg/tooling/playtest`, so shipped binaries link only the runtime packages they use.

```go
func Plugin() gameplugin.Plugin {
	return gameplugin.Plugin{
		Name:   "health",
		Module: "example.com/healthkit",
		API:    gameplugin.APIVersion,
		Components: []sceneformat.ComponentType{
			Type(), // carries Fields, Events, Handlers and optional Spawn
		},
		Systems: []gameplugin.System{
			{Name: "health.regen", Stage: gameplugin.StageUpdate, Fn: regenSystem},
		},
		Codecs: func(w *entities.World) error {
			return entities.RegisterCodec[Stats](w, "health")
		},
	}
}
```

### Installing onto a `Host`

A `gameplugin.Host` groups the registries a game wants plugins to populate; any `nil` field skips that kind of contribution:

- `Host.Components` (`*sceneformat.ComponentRegistry`): merges each plugin's `Components` (preserving `ComponentType.Spawn`) in plugin and slice order.
- `Host.Scheduler` (`*entities.Scheduler`): ensures the four standard stage anchor pairs exist once per scheduler, then registers each plugin's `Systems` in plugin and slice order.
- `Host.DataTables` (`*datatable.Registry`): registers each plugin's `RowTypes` in sorted file-pattern order.
- `Host.Console` (`*gamedebug.Console`): registers each plugin's `Console` commands in slice order, refusing collisions with built-in or previously registered commands.
- `Host.OnWorld(w)` runs every installed plugin's `Codecs` callback on `w` in installation order. Call `OnWorld` on every `entities.World` the game creates (including fresh worlds restored from save snapshots).
- `Host.Plugins()` returns installed plugin metadata (`Name`, `Module`, and `Version` resolved from `runtime/debug.ReadBuildInfo`, returning `"(devel)"` for local builds or replace directives without a version). `Host.Has(name)` reports whether a plugin is installed.

```go
host := &gameplugin.Host{
	Components: sceneformat.NewComponentRegistry(),
	Scheduler:  entities.NewScheduler(),
	DataTables: datatable.NewRegistry(),
	Console:    gamedebug.NewConsole(),
}
if err := gameplugin.Install(host, health.Plugin()); err != nil {
	return err
}

world := entities.NewWorld()
if err := host.OnWorld(world); err != nil {
	return err
}
```

### Validation, determinism and process-global registries

- **Pre-flight checks**: `Install` checks `Plugin.API == gameplugin.APIVersion`, validates plugin names and module paths, refuses duplicate plugin names, and verifies that every component, system, pixel effect, data table pattern, and console command name is free before mutating any registry. On any conflict or validation error, nothing is registered.
- **Deterministic order**: plugins are processed in argument order, slice fields (`Components`, `Systems`, `Console`) in declaration order, and maps (`PixelFX`, `RowTypes`) in sorted key order. `Plugin.Install` (the escape hatch `func(*Host) error`) runs last after all declarative contributions are committed.
- **Process-global idempotency (`PixelFX`)**: `pixelfx` effects live in a process-global registry. `Install` tracks the owning `(Name, Module)` of every plugin-registered effect so re-installing the same plugin in a second test or after an editor reload is a no-op rather than a panic, while a different plugin or collision with a built-in effect fails cleanly before registering anything.
- **No `Version` field**: just like `plugin.yaml`, `gameplugin.Plugin` has no `Version` field. Versions come from `go.mod` via `gameplugin.ModuleVersion(modulePath)`. `gameplugin.APIVersion` is bumped only on breaking changes to the plugin contract.

### Standard scheduler stages

Third-party systems often need to run in a predictable phase without knowing the host game's own system names. `gameplugin.Install` (or `gameplugin.EnsureStages(sched)`) adds four anchor pairs once per `*entities.Scheduler` in fixed order:

1. `gameplugin.StageInput` (`"input"`) — reading input and translating player intent.
2. `gameplugin.StageUpdate` (`"update"`) — general gameplay systems.
3. `gameplugin.StagePhysics` (`"physics"`) — physics, movement, and collision resolution.
4. `gameplugin.StageLate` (`"late"`) — camera follow, transform synchronization, and cleanup.

Setting `System.Stage` constrains the system with `entities.After(stage.Start())` and `entities.Before(stage.End())`. Within the same stage, systems run in registration order unless further ordered with `Before` and `After`. Games can also reference `stage.Start()` and `stage.End()` from their own `Scheduler.AddSystem` calls.

### Development-time contributions (`pkg/gameplugin/devplugin`)

MCP bridge prefabs (`pkg/mcp`) and playtest bots (`pkg/tooling/playtest`) depend on heavier inspection and replay packages that shipped game binaries should not link (see [Package Size Budgets](package-size-budgets.md)). Those contributions live in the separate `github.com/nsgo-dev/game/pkg/gameplugin/devplugin` package:

```go
import "github.com/nsgo-dev/game/pkg/gameplugin/devplugin"

devHost := &devplugin.Host{Bridge: bridge}
if err := devplugin.Install(devHost, devplugin.Plugin{
	Name:   "healthkit",
	Module: "example.com/healthkit",
	API:    gameplugin.APIVersion,
	Prefabs: []devplugin.PrefabSpec{
		{Prefab: patientPrefab},
	},
	Bots: map[string]playtest.BotFactory{
		"healthkit-medic": newMedicBot,
	},
}); err != nil {
	return err
}
```

`devplugin.Install` enforces the same API version check, atomic pre-flight conflict detection, deterministic ordering, and process-global ownership rules for `playtest` bot profiles (preventing collisions with built-in profiles or another plugin while allowing idempotent re-installs of the same plugin), without importing `pkg/editor`.

## The `plugin.yaml` manifest

Every Go plugin declares a `plugin.yaml` file at the root of its Go module. Tooling, the library index, and the editor read `plugin.yaml` before compiling anything to discover which Go packages a plugin provides, which engine versions and platforms it supports, and what other plugin modules it requires or conflicts with.

```yaml
manifestVersion: 1
name: healthkit
module: example.com/healthkit
description: Health component, ward panel and admit tool
license: MIT
authors: [Jane Doe]
repository: https://github.com/example/healthkit
engine: '>=0.4.0 <0.5.0'
api: 1
platforms: [all]
entries:
  runtime: example.com/healthkit/health
  editor: example.com/healthkit/healtheditor
  cli:
    - name: healthkit
      package: example.com/healthkit/cmd/game-healthkit
requires:
  example.com/navkit: ^0.3.0
conflicts: [example.com/oldhealth]
contributes:
  components: [health]
  editor:
    panels: [ward]
    tools: [admit]
    commands: [heal-all]
  agentTools: [plugin.healthkit.heal_all]
```

### Why there is no `version` field

A plugin's version is its Go module version, resolved by the game project's `go.mod` (and recorded in `go.sum`). Writing a second version field inside `plugin.yaml` would inevitably drift from Git tags and `go.mod`, so `plugin.yaml` deliberately omits `version` and refuses it as an unknown field.

Similarly, `requires` and `conflicts` are keyed by Go module path (such as `example.com/navkit`) rather than short plugin names so dependency resolution and conflict checks match the exact modules resolved in `go.mod`.

## Fields

| Field | Required | Description |
| --- | --- | --- |
| `manifestVersion` | Yes | Schema version of `plugin.yaml`. Must be written as the plain integer `1` (`1.0` and `0x1` are refused). |
| `name` | Yes | Short plugin identifier: 1 to 64 lower-case ASCII letters, digits and `-`, starting with a letter or digit (the same rule as library pack names). |
| `module` | Yes | Go module path of the plugin; must equal the `module` line of the plugin's `go.mod`. |
| `description` | No | Single-line summary of what the plugin provides, without control characters. |
| `license` | No | Single-line license identifier such as `MIT` or `Apache-2.0`. |
| `authors` | No | List of non-empty, single-line author names without duplicates. |
| `repository` | No | `http://` or `https://` URL of the plugin's source repository. |
| `engine` | Yes | Semantic version constraint on `github.com/nsgo-dev/game`, such as `>=0.4.0 <0.5.0` or `^0.4.0`. |
| `api` | Yes | Positive integer `gameplugin.APIVersion` the plugin targets (for example, `1`). |
| `platforms` | No | Either `[all]` alone, or a subset of `macos`, `windows`, `linux`, `ios`, `android` and `web`. When omitted, every platform is supported. |
| `entries` | Yes | Go package import paths inside `module`. At least one of `entries.runtime`, `entries.editor`, or `entries.cli` must be set. |
| `entries.runtime` | No | Go package providing the plugin's runtime entry (`func Plugin() gameplugin.Plugin`). Must equal `module` or be a subpackage under `module/`. |
| `entries.editor` | No | Go package providing the plugin's editor entry (`func Plugin() editor.Plugin`). Must equal `module` or be a subpackage under `module/`. |
| `entries.cli` | No | List of `{name, package}` entries extending the `game` CLI via `go.mod` `tool` directives. Each `name` follows the plugin name rule and must be unique; each `package` must equal `module` or be under `module/`. |
| `requires` | No | Map of required plugin Go module paths to semantic version constraints (such as `^0.3.0`). Keys must be valid Go module paths, cannot name `module` itself, and must be disjoint from `conflicts`. |
| `conflicts` | No | List of Go module paths that cannot be installed alongside this plugin. Entries must be valid Go module paths, unique, cannot name `module` itself, and must be disjoint from `requires`. |
| `contributes` | No | Index of symbols the plugin registers (`components`, `editor.panels`, `editor.tools`, `editor.commands`, and `agentTools`) for directory indexing and comparison against the compiled code. |

## Strict parsing and canonical encoding

`pkg/gameplugin/manifest` has no dependency on `pkg/editor` and can be imported by tooling, runtime code, and indexers alike:

- `manifest.Parse(data)` decodes one YAML document with unknown fields and duplicate keys refused, requires `manifestVersion` and `api` to be plain decimal integers, and validates every field. Errors match `manifest.ErrInvalid` with `errors.Is` and are `*loaderr.Error` values giving the file, line, field, and fix.
- `manifest.ParseFS(fsys, dir)` reads `plugin.yaml` from `dir` in `fsys`, parses it, and attaches the file path to any `*loaderr.Error`.
- `manifest.Encode(m)` validates `m` (filling `ManifestVersion: 1` when left `0`) and writes deterministic 2-space YAML with every list and the `requires` map sorted in byte order, without mutating `m`'s slices.
- `(Manifest).Validate()` checks all validation rules on a manifest constructed in code.
- `(Manifest).SupportsPlatform(platform)` reports whether the manifest supports a concrete platform (`macos`, `windows`, `linux`, `ios`, `android`, `web`).

```go
m, err := manifest.ParseFS(os.DirFS(pluginDir), ".")
if err != nil {
	return err
}
if !m.SupportsPlatform("web") {
	return fmt.Errorf("plugin %s does not support web", m.Name)
}
```

## JSON Schema

The published JSON Schema for `plugin.yaml` is at `https://game.nsgo.dev/schemas/plugin.v1.json` (`manifest.SchemaURL`). Add a schema comment on the first line of `plugin.yaml` (ignored by `manifest.Parse`) or map `plugin.yaml` in your editor's YAML settings for completion and inline validation:

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/plugin.v1.json
```
