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
	PhysicsFunc:     func() *physics.World { return g.physics },
	ScreenshotDir:   g.screenshotDir,
})
if err != nil {
	return err
}
```

- When a game replaces its `*scene.Scene`, `*entities.World`, `*game.Loop`, `*debug.Console` or `*physics.World` across level restarts, supply `SceneFunc` / `WorldFunc` / `LoopFunc` / `ConsoleFunc` / `PhysicsFunc` (or call `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop` / `bridge.SetConsole` / `bridge.SetPhysics`).
- To support `game_loop_seed`, configure `Random` (`*random.Stream`), `RandomStreams` (`*random.Streams`) and/or a custom `SetSeed` callback; if none is set, `game_loop_seed` returns an actionable error.
- When a `scene.Manager` controller draws via a custom `Controller.Draw` without setting `Controller.Scene`, `SceneFunc` / `Scene` provides the current scene's layers automatically.
- When `EntityName` or `EntityLayer` is omitted (or returns `""`), `game/mcp` falls back to [`sceneformat.ObjectRef`](editor.md) if the entity carries one.
- For `game_screenshot`, supply `CaptureFrame` (to composite overlays onto an already-rendered `*image.RGBA`), `DrawFrame` (to record custom `core.Drawing` ops), or let `game/mcp` render the active `Scenes`/`Scene` directly under `Viewport` / `ViewportFunc` (with optional `ScreenshotBackground`). Set `ScreenshotDir` so agents can pass a relative `.png` `path` and read the file; otherwise screenshots return `png_base64` inline, which an MCP client receives as JSON text rather than as an image.

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
| `game_asset_reload` | `key` (name, URI or relative path), `group` (optional), `all` (boolean, optional) | Reloads a matching asset in place (or all loaded assets when `all: true`), returning `{reloaded, key, group, kind, bytes, duration_ms, revision, items, error}`; each item carries `skipped` when it was left unchanged without an error. Parse and compile errors are returned in the structured result while keeping the previously loaded working asset intact. `all: true` needs an asset manager, reloader or registered asset; `Config.ReloadAsset` only reloads a single key. |
| `game_assets_reload_group` | `group` (required) | Reloads every loaded asset in the named group in place, reporting per-item results, `count` reloaded, `failed`, `skipped`, memory bytes, duration, and any item errors without crashing the running game. |
| `game_screenshot` | `scene`, `width`, `height`, `format` (`"png"`), `path`, `bounds`, `collision_shapes`, `physics`, `grid`, `grid_step`, `camera`, `labels`, `overlays` (`{bounds, collision_shapes, physics, grid, grid_step, camera, labels}`) | Captures the game view as PNG (`width`, `height`, `format`, `bytes`, `png_base64` or relative `path`, an `overlays_drawn` summary, and `skipped_ops` / `skipped_images` when the software rasterizer left something out), optionally compositing pixel-aligned sprite bounds, 2D physics collision shapes, reference grid, camera frame and labels in the same camera and viewport transforms. |

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
- `game_perf_readout` always includes a `sources` object indicating which telemetry providers are active (`overlay`, `profiler`, `gc`, `memory`, `draw_stats`, `render3d`). When the game annotates a frame spike via `Overlay.NoteFrameCause`, `history` entries include the optional `cause` string.

## Assets and hot reload

Wire `Config.Assets` (or `Config.AssetsFunc` / `bridge.SetAssets`), `Config.Resources`, `Config.Shaders`, `Config.Materials`, and/or `Config.ReloadAsset` to let agents inspect memory usage and trigger live reloads without restarting the game:

- **In-place atlas updates**: Reloading an atlas in `assets.Manager` (by asset name such as `"hero"`, full URI `"asset:sprites/hero.json"`, relative path `"sprites/hero.json"`, or image path `"sprites/hero.png"`) updates the existing `*sprite.Atlas` pointer in place and increments `assets.Manager.Revision()`, so sprites holding the atlas pointer and canvases reading `Revision()` update immediately.
- **Standalone assets and atlases**: Games that hold a `*sprite.Atlas` or custom resource outside `assets.Manager` can register it with `bridge.RegisterAtlas(...)` or `bridge.RegisterAsset(mcp.CustomAsset{...})`.
- **Safe failure handling**: Syntax, schema, or shader compilation errors leave the previous working atlas, shader, material, or resource active and return `reloaded: false` with the diagnostic message in `error`.
- **Skipped assets**: Sounds are not replaced in place, images with no file behind them (such as generated textures) have nothing to reload, and a file at a density the screen does not show (the 1x file on a 2x screen) would change nothing on screen. These items carry `skipped` with the reason (`assets.SkippedSound`, `assets.SkippedNoFile`, `assets.SkippedDensity`) and count neither as reloaded nor as failed. A group reload also skips an image whose file the group's reader cannot find; reloading that image by key reports the read error.
- **Off the engine goroutine**: `Manager.ReloadAsset` and `Manager.ReloadGroup` read files, parse atlases and run custom loaders on another goroutine, as `Group.Load` does, and apply the results on the engine goroutine once they finish. The tool call waits for them, so a custom reader or loader must not wait on the engine goroutine; when the call's context ends first, nothing is replaced and the tool returns the context's error.

## Screenshots and diagnostic overlays

`game_screenshot` renders or captures the current game view and composites requested overlays in the exact same `screen.Viewport` clip/scale/letterbox and `tilemap.Camera` (including parallax and `SpaceScreen` HUD layers) transforms as the game itself:

- **`bounds`**: Draws 1-pixel cyan (`#00e5ff`) bounding outlines around every visible sprite in the rendered scene(s).
- **`collision_shapes` / `physics`**: Draws 2D collision shapes from `Config.Physics` (box, polygon, segment and chain bodies in green `#22c55e`, circle bodies with `+X` orientation tick and capsule bodies in sky blue `#38bdf8`, triggers in pink `#f472b6`, solid tiles in amber `#f59e0b`, one-way tiles in purple `#c084fc`) and `Config.RigidPhysics` (rotated rigid bodies), counted in `overlays_drawn` as `physics_boxes`, `physics_circles`, `physics_polygons`, `physics_capsules`, `physics_lines`, `physics_triggers` and `physics_tiles`. Box, circle, trigger and tile colors match `physics.DefaultDebugStyle()` and `mcp.DefaultOverlayStyle()`.
- **`grid`**: Draws a world-space reference grid spaced by `grid_step` or `overlays.grid_step` (default `32` points).
- **`camera`**: Draws the active camera's visible frame and center crosshair (`#facc15`).
- **`labels`**: Draws compact bitmap labels for sprite frames and tagged physics bodies.
- **`path`**: When set, writes the PNG to a relative `.png` path inside `Config.ScreenshotDir` through `os.Root`, so absolute paths, `..` and symbolic links cannot leave it, and omits `png_base64` from the response. When omitted, returns `png_base64` in the JSON result. An MCP client gets that result as text, so prefer `path` when the agent should look at the image.
- **Size and pixel ratio**: Without `width` and `height`, the screenshot covers the view `game_input_pointer` maps through: `ViewSizeFunc`, else the attached window, else the viewport's virtual size, else the scene camera's size, in points at one pixel per point. A `CaptureFrame` image is taken to cover that view at the display's pixel ratio (an 800x450 frame of a 400x225-point view is 2x), so the viewport is laid out in points as the game does and the overlays are scaled to the image; an image with another aspect ratio is taken as one pixel per point. Inferred sizes are bounded like explicit ones (`8192` pixels a side, `4096x4096` pixels in all).
- **`scene`**: Renders that scene on its own with the software rasterizer; `CaptureFrame` and `DrawFrame` are not used. A scene whose controller draws through a custom `Controller.Draw` without a `scene.Scene` is refused.
- **Partial renders**: The software rasterizer draws rectangles, lines, arcs, quadratic and cubic curves, fills, strokes, rectangular clips, images and atlases whose pixels are in memory (`ImageSource.Pixels` or `Data`, read in the image's points when `Size` is set). It counts what it leaves out: `skipped_ops` by operation kind (text, rounded rectangles, ellipses, gradients, which fall back to their first color, blend modes, dashes, shadows, meshes) and `skipped_images` for images from a URI, an `asset:` file or an `Offscreen` canvas. Supply `CaptureFrame` when those matter. `overlays_drawn.sprite_bounds` counts sprites in layers' `Item.Batch`; sprites a `Drawable` item or a tilemap draws get no outline.

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

## Rendering a drawing to PNG

`mcp.RenderPNG(ops, width, height, background)` plays a recorded `core.Drawing`'s operations into a PNG with the software rasterizer `game_screenshot` uses when it draws a frame itself, and reports in `SkippedOps` and `SkippedImages` what it left out (text, and images that are not in memory as `Pixels` or `Data`). The editor's `viewport_image` uses it, so its picture is the same on every platform and headless.

## Editor tools

The tools above reach a running game. The NSGo Game Editor has an MCP server of its own, package `game/editor/editormcp`, through which an agent reads and edits the project open in the editor: every edit goes through the same `Editor` methods the inspector, scene tree and viewport use, so it shows at once in the tree and the viewport, and the person takes it back with one Undo.

```go
import "github.com/nsgo-dev/game/packages/game/editor/editormcp"
```

### Connecting an agent

`nsgo game editor .` serves the open project on a local endpoint of its own: a Unix domain socket that only the user can open, named after a hash of the project's path, in `nsgo-game/editor` under the user's cache directory (`$NSGO_GAME_EDITOR_ENDPOINTS` when set). It is never a network port. When the editor opens another project, the endpoint follows it; a second editor on the same project leaves the first serving and says so on stderr.

The agent starts `nsgo game mcp` in the project's directory, or in any directory inside it, or names it with `-project`:

```sh
claude mcp add game-editor -- nsgo game mcp
```

`nsgo game mcp` is an MCP server on standard input and output that finds the editor with the project open and forwards every tool call, resource read and subscription to it. It lists the editor's tools, including those other parts of the editor add, and connects again when the editor is closed and opened anew, so the agent's session outlives the editor's. While no editor has the project open, each call answers with an error that says so and how to open it: `no editor has the project open at /path/to/game: open it with nsgo game editor /path/to/game and try again`.

### Tools

| Tool | Action | Arguments | What it does |
| --- | --- | --- | --- |
| `scene_read` | read | `file`, `offset`, `limit` (all optional) | Reads a scene, prefab, data table or other text file of the project, or `game.yaml`, as lines numbered from 1. While the editor holds unsaved edits to the open scene or `game.yaml`, that file is read as the editor holds it, and the reply says so and that its line numbers differ from the file's on disk; otherwise, and for any other file, it is read from disk, comments included, with the line numbers `game_check` reports. Without `file` it reads the open scene. A path must be relative to the project root and stay inside it. |
| `scene_edit` | edit | `edits` (required), `file`, `label` (optional) | Applies the edits, in order, to the scene open in the editor, as one undo step labeled `label` (or one derived from the edits). The edits stay unsaved until the person saves. |
| `selection_get` | read | none | The open scene, the active layer, the selected object with its lines of YAML as `scene_read` numbers them, the file selected in the file browser, the active tool and the view mode. |
| `viewport_image` | read | `scale` (optional, 0.25 to 4) | A PNG of the viewport as it shows now: the scene through the editor's view, with the camera frame, tile grid and selection outline, at the viewport's size in points times `scale`. Text and images not loaded in memory are left out, and the reply counts them. |
| `game_check` | run | none | Runs the Problems panel's check (`assetcheck` with the editor's component registry, and `project.Check`) and returns its problems, each with file, line and message. It reads the files on disk; the reply names the open scene when it has unsaved edits the check did not see. |
| `game_playtest` | run | `file`, `run`, `count`, `seeds`, `out`, `recordings` (all optional) | Validates `playtest.yaml`, runs the configured headless bot playtests with per-seed `.replay.yaml` recordings, writes the YAML report and returns each metric's value, goal status (`met` or `missed`) and heat-map position data. |
| `playtest_define` | edit | `metric` (required), `file`, `rationale`, `replace` (optional) | Validates a proposed playtest metric, generates the canonical `playtest.yaml` diff and queues it as a pending review (`Server.PlaytestReviews` / `ApplyPlaytestReview` / `DismissPlaytestReview`) without modifying `playtest.yaml` on disk until reviewed. |

The edits `scene_edit` takes, each an object with `op`:

| `op` | Fields | Editor method |
| --- | --- | --- |
| `add` | `object` (as the scene file writes it), `layer`, `parent` | `PlaceObject`, then `ReparentObject` under `parent` |
| `remove` | `id` | `DeleteObject` |
| `move` | `id`, `x`, `y` | `MoveObject` |
| `set` | `id`, `fields` | the object's setters: `id`, `name`, `x`, `y`, `z`, `rotation`, `scaleX`, `scaleY`, `alpha`, `originX`, `originY`, `flipX`, `flipY`, `sprite`, `material`, `posZ`, `rotX`, `rotY`, `rotZ`, `scaleZ`, `depth`, `mesh` |
| `set` | `id`, `component`, `fields` | `SetComponentField` for each field |
| `add_component` | `id`, `component`, `fields` | `AddComponent`, then `SetComponentField` |
| `remove_component` | `id`, `component` | `RemoveComponent` |
| `set_property`, `remove_property` | `id`, `key`, `value` | `SetProperty`, `DeleteProperty` |
| `reparent` | `id`, `parent`, `index` | `ReparentObject` |
| `move_to_layer` | `id`, `layer`, `index` | `MoveObjectToLayer` |

An `add`'s object is read by the scene format's own reader with the editor's component registry, so it is refused as opening the file would refuse it. When the editor refuses any edit of a call, none of them applies: the scene, its undo history and its file on disk stay as they were, and the tool error names the file, the edit, the object, the component and field, and the fix, as in `scene_edit: scenes/room.scene.yaml: edit 2 (set) on object "hero" component "body" field "sped": unknown field (registered: friction, layer, solid, speed); use a component and fields the project registers, as the message lists them. Nothing was changed.` The structured result holds the same as an `editormcp.EditError`.

Grouping one request of the agent into one turn the person keeps or reverts, and drawing the agent's edits live, build on this server in their own changes; until then each `scene_edit` call is one ordinary undo step.

### The change stream

The server has two resources: `editor://scene`, the open scene's YAML as the editor holds it, and `editor://selection`, the selection as `selection_get` reports it in JSON. A client that subscribes to them is notified of each change, and the notice's `_meta` says who made it, `"nsgo.dev/by": "agent"` for a tool call and `"person"` for the editor's own controls, with the editor's revision under `"nsgo.dev/revision"`. A burst of changes may come as one notice.

### Adding tools

The tools are a registry that other parts of the editor join with `Server.AddTool`, so that every action with a button or a shortcut has a tool. A `Tool` has a name, a description, a JSON Schema for its arguments, an `Action` (`ActionRead`, `ActionEdit` or `ActionRun`, which the editor's permission rules decide on) and a handler. The handler runs on one of the server's goroutines and reaches the editor with `Call.Editor`, which runs a function on the editor's goroutine and waits for it, so a tool may wait for work done elsewhere and return an image. Clients are told the tool list changed.

```go
s := editormcp.New(ed, editormcp.Options{Run: cx.Post})
_ = s.AddTool(editormcp.Tool{
	Name:   "scene_name",
	Action: editormcp.ActionRead,
	Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
		var name string
		err := c.Editor(ctx, func(ed *editor.Editor) error { name = ed.Doc.Name; return nil })
		return &editormcp.Result{Text: name}, err
	},
})
_ = s.Open()
```

`Options.Run` hands functions to the editor's goroutine (`cx.Post` for an editor in a window); without it the server runs them on the caller's goroutine one at a time, for an editor no window drives. `Server.Connect` serves one session on any MCP transport, as a test does with `mcp.NewInMemoryTransports`.

### Why its own server

NSGo's `automation` registers app tools only in `nsgo dev -automation` and test builds, and `nsgo mcp` reaches only the app that `nsgo dev -automation` runs; an editor is an ordinary build. An app tool's handler also runs synchronously on the graph goroutine (nsgo-dev/nsgo#2342), which cannot wait for a project check without holding the editor, and returns JSON only, so a screenshot reaches the agent as base64 text rather than an image (nsgo-dev/nsgo#2343). The editor's server uses the official MCP Go SDK, which NSGo's own `nsgo mcp` is built on, so its tools wait off the editor's goroutine and `viewport_image` returns a real image. `Server.RegisterAutomation` still offers the same tools to `nsgo mcp` for an editor run under `go tool nsgo dev -automation`, each running on the graph goroutine from start to end and returning an `AutomationResult` with the PNG in base64.
