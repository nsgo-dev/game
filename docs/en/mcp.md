# mcp

`game/mcp` exposes a running game's scenes, layers, entities, components, fixed-step loop controls, game-coordinate input injection, tunables, debug console, loaded asset groups and hot reload controls to coding agents over MCP through NSGo's `github.com/nsgo-dev/nsgo/automation` package.

NSGo's built-in `nsgo mcp` server sees native views only; a game draws into one `Canvas` or GPU view. Attaching an `mcp.Bridge` to a `core.Scope` registers game-specific tools on the engine's reactive graph so `nsgo mcp` serves them alongside the built-in tools whenever the app runs under `go tool nsgo dev -automation` or `go tool nsgo test`. In ordinary and release builds, `automation.Register` is a no-op and adds no runtime protocol.

```go
import "github.com/nsgo-dev/game/packages/game/mcp"
```

## Attaching a bridge

Call `mcp.Attach` (or `mcp.New` followed by `bridge.Attach`) on the engine goroutine with the subsystems you want agents to inspect. Every field in `mcp.Config` is optional; tools remain registered until `cx` is disposed or `bridge.Detach()` is called.

```go
bridge, err := mcp.Attach(cx, mcp.Config{
	Scenes:          g.scenes,
	SceneName:       func(c *scene.Controller) string { return g.sceneNames[c] },
	SceneFunc:       func() *scene.Scene { return g.activeScene },
	NamedScenes:     map[string]*scene.Scene{"title": g.titleScene},
	WorldFunc:       func() *entities.World { return g.world },
	EntityName:      func(e entities.Entity) string { return g.names[e] },
	EntityTags:      func(e entities.Entity) []string { return g.tags[e] },
	EntityLayer:     func(e entities.Entity) string { return g.layers[e] },
	Loop:            g.loop,
	RandomStreams:   g.rng,
	Actions:         g.actions,
	Viewport:        &g.viewport,
	ViewSizeFunc:    g.currentViewSize,
	Console:         g.console,
	Overlay:         g.debugOverlay,
	RenderStatsFunc: func() mcp.PerfRender3DStats { return mcp.PerfRender3DStats(g.lastRenderStats) },
	Assets:          g.assets,
	Resources:       g.resources,
	Shaders:         g.shaderReloader,
	Materials:       g.materialReloader,
	ReloadAsset:     g.reloadCustomAsset,
})
if err != nil {
	return err
}
```

- When a game replaces its `*scene.Scene`, `*entities.World`, `*game.Loop` or `*debug.Console` across level restarts, supply `SceneFunc` / `WorldFunc` / `LoopFunc` / `ConsoleFunc` (or call `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop` / `bridge.SetConsole`).
- To support `game_loop_seed`, configure `Random` (`*random.Stream`), `RandomStreams` (`*random.Streams`) and/or a custom `SetSeed` callback; if none is set, `game_loop_seed` returns an actionable error.
- When a `scene.Manager` controller draws via a custom `Controller.Draw` without setting `Controller.Scene`, `SceneFunc` / `Scene` provides the current scene's layers automatically.
- When `EntityName` or `EntityLayer` is omitted (or returns `""`), `game/mcp` falls back to [`sceneformat.ObjectRef`](editor.md) if the entity carries one.

## Inspecting and editing components

Components registered on an [`entities.World`](entities.md) with `entities.RegisterCodec` are readable and editable automatically. In addition:

- Built-in `entities.Transform2D`, `entities.WorldTransform2D` and `sceneformat.ObjectRef` components are surfaced automatically as `"transform2d"`, `"world_transform2d"` and `"object_ref"` unless that type or name is already registered. `"transform2d"` and `"object_ref"` are editable directly (with `"transform2d"` propagating `Hierarchy2D` dirty flags automatically); `"world_transform2d"` is derived from the hierarchy and read-only.
- Transient or non-saved runtime components can be exposed read-only with `mcp.RegisterComponent[T](bridge, name)` or `mcp.RegisterComponentFunc(bridge, name, read)`, or exposed for both reading and editing with `mcp.RegisterMutableComponent[T](bridge, name)` or `mcp.RegisterComponentEditor[T](bridge, name, apply)`. `RegisterComponentEditor` can add editing to an earlier `RegisterComponent[T]` registration of the same type, but not to a `RegisterComponentFunc` reader, whose value need not be stored as `T`.
- `game_component_set` decodes values strictly with `DisallowUnknownFields`, rejects non-finite floats (`NaN`, `±Inf`) and invalid nulls, and patches existing fields by default (preserving omitted fields unless `"replace": true` is passed) without leaving partially mutated state on error. A patch keeps every field it does not change exactly as it was, including unexported fields, fields tagged `json:"-"` such as callbacks and handles, and the identity of pointers, maps and slices; a field it changes gets a freshly allocated value, so an error or an editor that rejects the value leaves the live component untouched. A codec component whose decoded value cannot be encoded back, such as a `NaN` produced by a custom `UnmarshalJSON`, is refused before it is stored.
- Component values are serialized with `encoding/json`; if a runtime value contains non-finite floats (`NaN`, `+Inf`, `-Inf`), callback fields, channels or cyclic pointers, `game/mcp` falls back to a safe sanitizer (`"NaN"`, `"+Inf"`, `"-Inf"`, `"<func>"`, `"<chan>"`, `"<cycle>"`) so an agent can still diagnose broken runtime state.

```go
if err := mcp.RegisterMutableComponent[AIState](bridge, "ai"); err != nil {
	return err
}
if err := mcp.RegisterComponentEditor[Health](bridge, "health", func(w *entities.World, e entities.Entity, h Health) error {
	if h.HP < 0 {
		return errors.New("hp cannot be negative")
	}
	entities.Set(w, e, h)
	return nil
}); err != nil {
	return err
}
```

## Registering prefabs

Prefabs can be registered on `mcp.Config.Prefabs` or on the `*mcp.Bridge` so agents can discover them with `game_prefabs_list` and spawn them with `game_entity_spawn`:

- `bridge.RegisterPrefab(name, spawn)` (or `Config.Prefabs`) registers a custom `mcp.PrefabSpawner` (`func(w *entities.World, overrides map[string]any) (entities.Entity, error)`).
- `bridge.RegisterEntityPrefab(p)` registers an [`*entities.Prefab`](entities.md) under `p.Name()`.
- `bridge.RegisterScenePrefab(name, doc)` registers a validated, self-contained [`*sceneformat.SceneDoc`](editor.md) template spawned via `sceneformat.SpawnEntities`. The document must hold exactly one top-level object, the spawned root; nest the rest under it. Every spawn attaches the document's `sceneformat.ObjectRef` IDs unchanged, so repeated spawns produce entities with the same object IDs, which `game_entities_list` also shows as their names unless `EntityName` says otherwise.

```go
if err := bridge.RegisterEntityPrefab(coinPrefab); err != nil {
	return err
}
if err := bridge.RegisterScenePrefab("turret", turretDoc); err != nil {
	return err
}
```

If `game_entity_spawn` fails while applying `parent` or `components`, or while reading the new entity's components back, the newly spawned entity (and any scene-prefab children) are rolled back automatically. `x` and `y` are validated before anything is spawned.

## Registered tools

| Tool | Arguments | What it returns |
| --- | --- | --- |
| `game_scenes_list` | `{}` | Active scene stack (bottom to top), `current` scene name, `stack_depth`, transition state and `progress`, layer counts, camera state, and any inactive `NamedScenes`. |
| `game_layers_list` | `scene` (optional), `include_hidden` (optional, default `true`) | Layers of the selected (or current active) scene in draw order (`draw_index`, slice `index`, `name`, `space`, `visible`, `order`, effective `parallax_x`/`parallax_y`, offsets, repeat periods, `depth_sort`, and item/batch/sprite/tilemap/drawable counts). |
| `game_entities_list` | `component`, `layer`, `name` (substring), `tag`, `parent` (ref or `"root"`), `include_components` (default `false`), `offset`, `limit` | Live entities in ascending slot order with `total`, `returned`, stable `ref`, `index`, `generation`, metadata, `parent`, `child_count`, sorted `components` names, and optional inline `values`. |
| `game_entity_inspect` | `ref` (required) | Full inspection of one live entity: `ref`, `index`, `generation`, metadata, `parent`, `children` refs, and every readable component value keyed by component name. |
| `game_component_get` | `ref` (required), `component` (required) | One component's serialized `value` on a live entity. |
| `game_loop_status` | `{}` | Current loop state: `paused`, effective `time_scale`, `step_seconds`, `step_hz`, total `steps`, `game_time_seconds` and `seed_configured`. |
| `game_loop_pause` | `{}` | Pauses the loop (`Loop.SetPaused(true)`) without accumulating a catch-up backlog and returns the updated loop status. |
| `game_loop_resume` | `{}` | Resumes the loop (`Loop.SetPaused(false)`) and returns the updated loop status. |
| `game_loop_step` | `count` (optional, default `1`, `1..3600`) | Synchronously runs `count` fixed updates (`Loop.StepNow`) whether paused or running, returning `steps_advanced`, `total_steps`, `game_time_seconds`, `delta_seconds` and `paused`. Stepping 60 updates at 60 Hz advances exactly `1.0` second of game time. |
| `game_loop_time_scale` | `time_scale` (required, finite `>= 0`) | Updates `Loop.TimeScale` (`0` resets to the default `1x` speed) and returns the updated loop status. |
| `game_loop_seed` | `seed` (required, unsigned 64-bit integer) | Re-seeds `Config.Random`, `Config.RandomStreams` and/or calls `Config.SetSeed`, returning `{seed}`. |
| `game_input_action` | `name` (required), `mode` (`"press"`, `"release"`, `"tap"`, default `"tap"`), `value` (optional float in `[-1, 1]`) | Injects a digital action hold/release/tap (or analog value) into `input.Actions` and returns the action's current step state. |
| `game_input_axis` | `name` + `value`, `axes` map (`{name: value}`), and/or `clear` (`bool`) | Sets or clears 1D or multi-axis analog values in `[-1, 1]` on `input.Actions`. |
| `game_input_pointer` | `x`, `y` (required), `action` (`"press"`, `"release"`, `"move"`, `"click"`, default `"click"`), `space` (`"game"` or `"world"`, default `"game"`), `button` (`"left"`, `"right"`, `"middle"`) | Maps coordinates through the active scene's `Camera` (when `space="world"`) and `screen.Viewport` into view space, forwards to `input.Actions`, runs `Scene.HitTest` / `Scene.DispatchPress`, invokes `Config.OnPointer`, and returns both game/view coordinates, `in_viewport`, and hit metadata (`hit`, `hit_layer`, `hit_kind`, `hit_frame`, `hit_batch_index`, `hit_sprite_index`, `hit_drawable_index`, `dispatched`). |
| `game_input_actions_list` | `{}` | Lists configured action and axis names on `input.Actions` with their current step state (`pressed`, `just_pressed`, `just_released`, `binding_count`, `value`, `negative`, `positive`). |
| `game_tunables_list` | `prefix` (optional) | Registered `debug.Console` tunables sorted by name with `type`, current `value`, registration `default`, and numeric `min`/`max` bounds. |
| `game_tunable_get` | `name` (required) | One registered tunable's `name`, `type`, current `value`, registration `default`, and numeric `min`/`max` bounds. |
| `game_tunable_set` | `name` (required), `value` (required), `clamp` (optional, default `false`) | Updates a registered tunable (`float`, `int`, `bool` or `string`) with strict type checking and range validation (or clamping when `clamp: true`), returning `old_value`, applied `value`, metadata and `clamped`. |
| `game_console_commands_list` | `include_builtin` (optional, default `true`) | Built-in and custom `debug.Console` commands sorted by name with `help` text and `builtin` flag. |
| `game_console_exec` | `command` (required) | Runs one command line through `debug.Console`, returning `command`, `ok`, `output`, `lines`, and `error` (when the console command fails). |
| `game_prefabs_list` | `{}` | Sorted list of registered prefabs (`name` and `kind`: `"custom"`, `"entity_prefab"`, or `"scene"`). |
| `game_entity_spawn` | `prefab`, `overrides`, `parent`, `x`, `y`, `components` (all optional) | Newly spawned live entity's stable `ref`, `index`, `generation`, `prefab`, metadata, `parent`, `children`, and initial `components` snapshot. |
| `game_entity_destroy` | `ref` (required), `policy` (optional: `"cascade"`, `"orphan"`, `"keep_world"`, or `"reparent"`) | Confirmation of destruction with `ref`, the `policy` applied to children (the hierarchy's default, usually `"orphan"`, when omitted; `"none"` when omitted and the entity had no children), and count of `destroyed` entities. `"reparent"` keeps the children's world transforms. |
| `game_component_set` | `ref` (required), `component` (required), `value` (required), `replace` (optional, default `false`) | Updated component `value` read back from the live entity; edits take effect on the next simulation step. If the write succeeded but the read-back failed, `value` is `null` and `read_error` says why; the edit still stands. |
| `game_component_remove` | `ref` (required), `component` (required) | Whether the component was present and `removed` from the live entity. |
| `game_replay_status` | `{}` | Current recording and playback state (`recording`, `replaying`, `replay_done`, `frames_recorded`, `replay_frame`, `replay_total_frames`), active names/metadata, and sorted `saved_recordings`. |
| `game_replay_record_start` | `name`, `seed`, `actions`, `axes`, `metadata`, `reset` (all optional; `reset` defaults to `true` when `ResetFunc` or a seed target is configured) | Resets to `seed` (see below) and starts recording per-step inputs via `replay.Recorder` at the game's fixed step rate, returning the resolved recording name, seed, step rate, watched names and metadata. |
| `game_replay_record_stop` | `name` (optional), `save_path` (optional, relative to `ReplayConfig.SaveDir`), `overwrite` (default `false`) | Stops the active recording, stores it in memory (and optionally writes it under `SaveDir`), and returns its summary plus deterministic version-2 YAML text (`yaml`). An existing stored name or file is refused unless `overwrite: true`, and the recording continues. |
| `game_replay_play` | `name` or `yaml` (mutually exclusive), `run_to_end` (default `false`), `steps` (`1..3600`), `reset` (default `true` when `ResetFunc` or a seed target is configured) | Resets to the recording's seed and starts deterministic playback via `replay.Player`, optionally advancing all (`run_to_end`, recordings up to `3600` steps) or `steps` frames synchronously via `StepFunc` or the game loop's `StepNow`. A recording whose `rate` differs from the game's fixed step rate is refused. |
| `game_replay_stop` | `{}` | Stops an active replay playback cleanly and reports how many frames were played. |
| `game_replay_list` | `{}` | Lists stored recordings in sorted name order with `frames`, `seed`, `step_rate`, `events`, `actions`, `axes` and `metadata`. |
| `game_perf_readout` | `include_history` (optional, default `false`), `history_limit` (optional), `include_zones` (optional, default `false`), `zones_limit` (optional) | Live performance readout matching the debug overlay: `fps`, rolling average and latest `frame_ms`/`update_ms`/`draw_ms`, min/max/p50/p95/p99 frame times, draw stats (`draw_calls`, `sprites`, `batches`, `triangles`, `vertices`), per-frame heap allocations (`allocs_per_frame`, `alloc_bytes_per_frame`, `last_frame_allocs`, `last_frame_alloc_bytes`), `heap_bytes`, `gc_count`, GC pause/hitch metrics, optional `render3d` and `asset_memory` breakdowns, and optional chronological `history` and profiler `zones`. |
| `game_perf_zones` | `limit` (optional) | Named timing zones from `debug.Profiler` sorted by recent frame cost (`name`, `last_ms`, `avg_ms`, `max_ms`, `calls`), plus `recording`, `recorded_frames` and `total`. |
| `game_perf_reset` | `{}` | Resets accumulated `debug.Overlay` frame timings and pushed draw stats, `debug.Profiler` zone timings, and `debug.GCMonitor` history. A profiler trace recording started with `StartRecording` keeps running and keeps its events. |
| `game_assets_list` | `group` (optional), `state` (`loaded`/`loading`/`unloaded`/`failed`), `kind` (optional) | Deterministically sorted asset groups (`assets.Manager` groups, `resource.Library`, `hotreload.ShaderReloader`, `hotreload.MaterialReloader`, and custom registered assets) with load states, asset keys/kinds/URIs, frame counts, per-group estimated bytes, and `total_bytes`. |
| `game_asset_reload` | `key` (name, URI or relative path), `group` (optional), `all` (boolean, optional) | Reloads a matching asset in place (or all loaded assets when `all: true`), returning `{reloaded, key, group, kind, bytes, duration_ms, revision, items, error}`. Parse and compile errors are returned in the structured result while keeping the previously loaded working asset intact. |
| `game_assets_reload_group` | `group` (required) | Reloads every loaded asset in the named group in place, reporting per-item results, memory bytes, duration, and any item errors without crashing the running game. |

All tools reject unknown argument fields.

## Deterministic loop control

Agents can drive reproducible test scenarios by pausing the loop, setting a deterministic seed, stepping an exact number of fixed updates, and inspecting entity state immediately afterward:

- `game_loop_step` runs `Update` synchronously on the engine goroutine and calls `Draw` once afterward, so subsequent `game_entity_inspect` or `game_component_get` calls observe the updated state immediately without waiting for the next display frame.
- Simulated time is accumulated per fixed-rate segment (`steps / rate`), so stepping 60 updates at 60 Hz advances `game_time_seconds` and `delta_seconds` by exactly `1.0` with no integer-nanosecond drift.
- `game_loop_step` caps each call at `MaxLoopSteps` (`3600`, or one minute at 60 Hz) to guard against accidental hangs.

## Input injection and viewport mapping

When `Actions`, `Viewport` / `ViewportFunc`, `ViewSizeFunc`, or `OnPointer` are set on `mcp.Config` (or updated via `bridge.SetActions` / `bridge.SetViewport`), agents can drive gameplay directly in virtual game coordinates without computing window letterbox offsets:

- **Actions and axes**: `game_input_action` drives `Actions.Press`, `Actions.Release`, `Actions.Trigger` and `Actions.SetAxis`; `game_input_axis` sets or clears one or more analog axes in `[-1, 1]`. All injected inputs follow `Actions.Step()` snapshot semantics just like physical inputs, and `Actions.Reset()` clears them cleanly.
- **Pointer coordinate pipeline**: `game_input_pointer` takes `(x, y)` in `"game"` space (virtual canvas points) or `"world"` space (converted to game space through the active scene's `Camera.WorldToView`), then maps game coordinates through `screen.Viewport.GameToView` using `ViewSizeFunc` (falling back to the attached scope's `Env().Window()` and then the viewport's virtual size).
- **Delivery**: Mapped view coordinates are forwarded to `input.Actions` (so `Actions.MapPoint = viewport.ViewToGame` recovers the exact game position for `MouseRegion` bindings). When `in_viewport` is true, `Scene.HitTest` and primary `Scene.DispatchPress` run in game coordinates so pressing at a sprite's position hits that sprite and triggers its `OnPress` callback. `Config.OnPointer` receives every injected `PointerEvent` (a `"click"` emits `PointerPress` followed by `PointerRelease`).

## Tunables and debug console

Pass a [`*debug.Console`](debug.md) via `Config.Console`, `Config.ConsoleFunc`, or `bridge.SetConsole` so agents can inspect and adjust live game variables and run custom debug commands:

```go
console := debug.NewConsole()
console.RegisterFloat("physics.gravity", 0, 2000, &g.gravity)
console.RegisterInt("player.max_jumps", 1, 5, &g.maxJumps)
console.RegisterBool("player.god_mode", &g.godMode)
console.RegisterString("level.name", &g.levelName)
console.RegisterCommand("spawn", "spawn <kind> [count]", g.handleSpawn)
```

- `game_tunables_list` and `game_tunable_get` read live values directly without adding entries to the console's interactive command history or output log.
- `game_tunable_set` enforces each tunable's registered type (`float`, `int`, `bool` or `string`) and numeric `[min, max]` bounds by default without mutating the variable on error. Passing `"clamp": true` clamps out-of-range numeric values into `[min, max]` and sets `"clamped": true` in the response.
- `game_console_exec` executes built-in commands (`help`, `list`, `get`, `set`, `clear`) and game-registered commands through `debug.Console.Exec`, returning `{command, ok, output, lines, error}`. Invalid MCP arguments or a missing `Console` return a tool error, while command execution failures (such as an unknown command name or invalid command arguments) return `ok: false` with the command's `error` message.
## Recording and replaying inputs

Create a `*mcp.ReplayController` with `mcp.NewReplayController`, pass it to your simulation as its [`replay.ActionSource`](replay.md), and attach it on `mcp.Config.Replay`:

```go
var ctrl *mcp.ReplayController
ctrl = mcp.NewReplayController(mcp.ReplayConfig{
	Source:   g.actions,
	Actions:  []string{"left", "right", "jump", "dash"},
	Axes:     []string{"move_x"},
	StepRate: 60,
	SeedFunc: func() uint64 { return g.seed },
	ResetFunc: func(seed uint64) error {
		g.actions.Reset()
		g.Reset(seed)
		return nil
	},
	StepFunc: func(dt time.Duration) {
		g.Step(ctrl, dt)
	},
})

bridge, err := mcp.Attach(cx, mcp.Config{
	WorldFunc: func() *entities.World { return g.world },
	Replay:    ctrl,
})
```

- Because `ReplayController` implements `replay.ActionSource`, your game loop reads input from `ctrl` identically during live play (forwarded to `Source`), recording (`replay.Recorder`) and playback (`replay.Player`). `Source` is required. It is still stepped during playback, so its previous-step snapshot is current when live input resumes.
- Recording and playback share one reset: the Bridge's seed targets (`Config.Random`, `Config.RandomStreams`, `Config.SetSeed`, the same ones `game_loop_seed` uses) are re-seeded with the seed, then `ResetFunc(seed)` runs. `reset` defaults to `true` on both tools whenever there is something to reset, so a recording started mid-game with default arguments replays to the same state. Passing `reset: false` to `game_replay_record_start` records from the current state, and that recording only reproduces from the same state.
- The recording's `rate` is the game's fixed step rate: the Bridge's loop (`Config.Loop` / `Config.LoopFunc`) `StepRate()` when there is one, otherwise `ReplayConfig.StepRate` (default `60`). `game_replay_play` refuses a recording made at another rate, because each step would get a different `dt`.
- Recordings stopped via `game_replay_record_stop` are stored in memory under their name and returned as canonical deterministic [`game/replay`](replay.md) v2 YAML text. Set `ReplayConfig.SaveDir` if you also want agents to write recordings to relative paths on disk via `save_path`; the write goes through `os.Root`, so absolute paths, `..` and symbolic links cannot leave `SaveDir`. `metadata` stays only in the in-memory store: it is not part of the YAML, so a saved file or an inline `yaml` replay does not carry it.
- `game_replay_play` accepts either a stored `name` or inline `yaml` text and replays either synchronously (`run_to_end: true` or `steps: N`) or frame by frame as the game loop advances. Without `StepFunc`, synchronous playback advances the Bridge's loop with `Loop.StepNow`, so `game_loop_status` counts the replayed steps and game time. Like `game_loop_step`, a single call advances at most `MaxLoopSteps` (`3600`) steps.
- After the final recorded step, the next `ctrl.Step()` is a trailing playback step that releases actions still held at the end of the recording (firing `JustReleased` once); the step after it returns to live input and calls `OnReplayStop`.

## Performance readouts

Pass your [`debug.Overlay`](debug.md) via `Config.Overlay` to expose the exact same frame timings, draw statistics (`debug.Options.DrawStats` or `Overlay.SetDrawStats`), GC/allocation counters (`debug.GCMonitor`), asset memory (`debug.MemoryTracker`) and timing scopes (`debug.Profiler`) shown by the on-screen debug HUD.

- `Config.Profiler`, `Config.GC`, `Config.Memory` and `Config.DrawStatsFunc` override (or work standalone without) `Config.Overlay`.
- `Config.RenderStatsFunc` attaches detailed 3D culling, batching, state-switch and GPU memory metrics from [`render3d.RenderStats`](render3d.md) under `render3d`, and populates top-level `draw_calls`, `batches`, `triangles` and `vertices` automatically when no 2D draw-stats provider is set. `mcp.PerfRender3DStats` has the same fields as `render3d.RenderStats`, so `mcp.PerfRender3DStats(stats)` converts one; `game/mcp` does not import `game/render3d`, so a 2D game that attaches the bridge does not link the 3D renderer.
- `game_perf_readout` always includes a `sources` object indicating which telemetry providers are active (`overlay`, `profiler`, `gc`, `memory`, `draw_stats`, `render3d`).

## Assets and hot reload

Wire `Config.Assets` (or `Config.AssetsFunc` / `bridge.SetAssets`), `Config.Resources`, `Config.Shaders`, `Config.Materials`, and/or `Config.ReloadAsset` to let agents inspect memory usage and trigger live reloads without restarting the game:

- **In-place atlas updates**: Reloading an atlas in `assets.Manager` (by asset name such as `"hero"`, full URI `"asset:sprites/hero.json"`, relative path `"sprites/hero.json"`, or image path `"sprites/hero.png"`) updates the existing `*sprite.Atlas` pointer in place and increments `assets.Manager.Revision()`, so sprites holding the atlas pointer and canvases reading `Revision()` update immediately.
- **Standalone assets and atlases**: Games that hold a `*sprite.Atlas` or custom resource outside `assets.Manager` can register it with `bridge.RegisterAtlas(...)` or `bridge.RegisterAsset(mcp.CustomAsset{...})`.
- **Safe failure handling**: Syntax, schema, or shader compilation errors leave the previous working atlas, shader, material, or resource active and return `reloaded: false` with the diagnostic message in `error`.

## Stable entity references

Every live entity receives a generational reference formatted as `"entity:<index>:<generation>"` (such as `"entity:0:1"`), produced by `FormatEntityRef` and parsed by `ParseEntityRef`:

- A surviving entity keeps the exact same `ref` across simulation steps, even while other entities are spawned and destroyed.
- When an entity is destroyed and its slot is later recycled, the new entity receives an incremented generation (for example `"entity:0:2"`). Calling `game_entity_inspect` or `game_component_get` with the stale `"entity:0:1"` returns a `"not alive"` error rather than silently reading the recycled entity.

## Testing with `nsgotest`

In tests, attach the bridge inside `nsgotest.Start` and invoke tools through `automation.Call` inside `app.Post`:

```go
var raw json.RawMessage
var callErr error
app.Post(func() {
	raw, callErr = automation.Call(cx, t.Context(), mcp.ToolEntitiesList, nil)
})
app.Settle()
```
