# mcp

`game/mcp` exposes a running game's scenes, layers, entities, components, fixed-step loop controls, game-coordinate input injection, tunables, debug console, loaded asset groups and hot reload controls to coding agents over MCP through NSGo's `github.com/nsgo-dev/nsgo/automation` package.

NSGo's built-in `nsgo mcp` server sees native views only; a game draws into one `Canvas` or GPU view. Attaching an `mcp.Bridge` to a `core.Scope` registers game-specific tools on the engine's reactive graph so `nsgo mcp` serves them alongside the built-in tools whenever the app runs under `go tool nsgo dev -automation` or `go tool nsgo test`. In ordinary and release builds, `automation.Register` is a no-op and adds no runtime protocol.

```go
import "github.com/nsgo-dev/game/pkg/mcp"
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
	ProjectDir:      g.projectDir,
	StateValues:     func() map[string]any { return map[string]any{"score": g.score} },
	Log:             &mcp.LogConfig{},
})
if err != nil {
	return err
}
```

- When a game replaces its `*scene.Scene`, `*entities.World`, `*game.Loop`, `*gamedebug.Console` or `*physics.World` across level restarts, supply `SceneFunc` / `WorldFunc` / `LoopFunc` / `ConsoleFunc` / `PhysicsFunc` (or call `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop` / `bridge.SetConsole` / `bridge.SetPhysics`).
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
| `game_state_dump` | `{}` | Deterministic state dump (`version`, `rate`, `steps`, `game_time_seconds`, `scene`, `entities`, `values` from `Config.StateValues`, and canonical `statedump` `yaml`). |
| `game_state_capture` | `name` (required), `values` (optional key list), `include_yaml` (optional, default `false`) | Runs `statedump.Capture` over the world, scenes and `Config.StateValues` (optionally filtered to `values`), stores the canonical YAML in memory under `name` (bounded to `MaxStateCaptures` / `MaxStateCaptureTotalBytes` with FIFO eviction), and returns capture summary metadata. |
| `game_state_diff` | `from` (required), `to` (optional; defaults to live state), `paths` (optional filter list) | Returns `statedump.Diff` unified YAML line diff, `equal`, `steps_delta`, `scene_changed`, `changed_entities` / `added_entities` / `removed_entities` / `modified_entities` refs, per-entity `entity_changes`, and `value_changes`. |
| `game_loop_status` | `{}` | Current loop state: `paused`, effective `time_scale`, `step_seconds`, `step_hz`, total `steps`, `game_time_seconds` and `seed_configured`. |
| `game_loop_pause` | `{}` | Pauses the loop (`Loop.SetPaused(true)`) without accumulating a catch-up backlog and returns the updated loop status. |
| `game_loop_resume` | `{}` | Resumes the loop (`Loop.SetPaused(false)`) and returns the updated loop status. |
| `game_loop_step` | `count` (optional, default `1`, `1..3600`) | Synchronously runs `count` fixed updates (`Loop.StepNow`) whether paused or running, returning `steps_advanced`, `total_steps`, `game_time_seconds`, `delta_seconds` and `paused`. Stepping 60 updates at 60 Hz advances exactly `1.0` second of game time. |
| `game_loop_time_scale` | `time_scale` (required, finite `>= 0`) | Updates `Loop.TimeScale` (`0` resets to the default `1x` speed) and returns the updated loop status. |
| `game_loop_seed` | `seed` (required, unsigned 64-bit integer) | Re-seeds `Config.Random`, `Config.RandomStreams` and/or calls `Config.SetSeed`, returning `{seed}`. |
| `game_input_action` | `name` (required), `mode` (`"press"`, `"release"`, `"tap"`, `"axis"`; default `"tap"` when `value` is omitted, `"axis"` when `value` is provided), `value` (optional float in `[-1, 1]`, required when `mode` is `"axis"`) | Injects a digital action hold/release/tap or analog axis value into `input.Actions` and returns the action's current step state. |
| `game_input_axis` | `name` + `value`, `axes` map (`{name: value}`), and/or `clear` (`bool`) | Sets or clears 1D or multi-axis analog values in `[-1, 1]` on `input.Actions`. |
| `game_input_pointer` | `x`, `y` (required), `action` (`"press"`, `"release"`, `"move"`, `"click"`, default `"click"`), `space` (`"game"` or `"world"`, default `"game"`), `button` (`"left"`, `"right"`, `"middle"`) | Maps coordinates through the active scene's `Camera` (when `space="world"`) and `screen.Viewport` into view space, forwards to `input.Actions`, runs `Scene.HitTest` / `Scene.DispatchPress`, invokes `Config.OnPointer`, and returns both game/view coordinates, `in_viewport`, and hit metadata (`hit`, `hit_layer`, `hit_kind`, `hit_frame`, `hit_batch_index`, `hit_sprite_index`, `hit_drawable_index`, `dispatched`). |
| `game_input_actions_list` | `{}` | Lists configured action and axis names on `input.Actions` with their current step state (`pressed`, `just_pressed`, `just_released`, `binding_count`, `value`, `negative`, `positive`). |
| `game_tunables_list` | `prefix` (optional) | Registered `gamedebug.Console` tunables sorted by name with `type`, current `value`, registration `default`, and numeric `min`/`max` bounds. |
| `game_tunable_get` | `name` (required) | One registered tunable's `name`, `type`, current `value`, registration `default`, and numeric `min`/`max` bounds. |
| `game_tunable_set` | `name` (required), `value` (required), `clamp` (optional, default `false`) | Updates a registered tunable (`float`, `int`, `bool` or `string`) with strict type checking and range validation (or clamping when `clamp: true`), returning `old_value`, applied `value`, metadata and `clamped`. |
| `game_vars_list` | `prefix` (optional), `include_self` (optional, default `true`) | Declared `flow.Vars` entries (`name`, `kind`, `value`, `default`, `min`, `max`, `doc`) and active per-object `self` variables. |
| `game_var_get` | `name` (required) | One declared `flow.Vars` entry's `name`, `kind`, current `value`, `default`, numeric `min`/`max` bounds, and `doc`. |
| `game_var_set` | `name` (required), `value` (required), `clamp` (optional, default `false`) | Updates a declared `flow.Vars` variable (`bool`, `int`, `float` or `string`) with strict kind and range validation (or clamping when `clamp: true`), returning `old_value`, applied `value`, metadata and `clamped`. |
| `game_console_commands_list` | `include_builtin` (optional, default `true`) | Built-in and custom `gamedebug.Console` commands sorted by name with `help` text and `builtin` flag. |
| `game_console_exec` | `command` (required) | Runs one command line through `gamedebug.Console`, returning `command`, `ok`, `output`, `lines`, and `error` (when the console command fails). |
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
| `game_perf_zones` | `limit` (optional) | Named timing zones from `gamedebug.Profiler` sorted by recent frame cost (`name`, `last_ms`, `avg_ms`, `max_ms`, `calls`), plus `recording`, `recorded_frames` and `total`. |
| `game_perf_reset` | `{}` | Resets accumulated `gamedebug.Overlay` frame timings and pushed draw stats, `gamedebug.Profiler` zone timings, and `gamedebug.GCMonitor` history. A profiler trace recording started with `StartRecording` keeps running and keeps its events. |
| `game_sound_readout` | `{}` | Live audio voice telemetry matching the debug overlay (`Config.Sound`, `Config.SoundFunc`, or `Overlay.Sound()`): `active_voices`, `fading_voices`, `max_voices`, per-concurrency-group counts and counters (`groups`), and per-bus active voice counts and ducking gains (`buses`). |
| `game_assets_list` | `group` (optional), `state` (`loaded`/`loading`/`unloaded`/`failed`), `kind` (optional) | Deterministically sorted asset groups (`assets.Manager` groups, `resource.Library`, `hotreload.ShaderReloader`, `hotreload.MaterialReloader`, and custom registered assets) with load states, asset keys/kinds/URIs, frame counts, per-group estimated bytes, and `total_bytes`. |
| `game_asset_reload` | `key` (name, URI or relative path), `group` (optional), `all` (boolean, optional) | Reloads a matching asset in place (or all loaded assets when `all: true`), returning `{reloaded, key, group, kind, bytes, duration_ms, revision, items, error}`; each item carries `skipped` when it was left unchanged without an error. Parse and compile errors are returned in the structured result while keeping the previously loaded working asset intact. `all: true` needs an asset manager, reloader or registered asset; `Config.ReloadAsset` only reloads a single key. |
| `game_assets_reload_group` | `group` (required) | Reloads every loaded asset in the named group in place, reporting per-item results, `count` reloaded, `failed`, `skipped`, memory bytes, duration, and any item errors without crashing the running game. |
| `game_screenshot` | `scene`, `width`, `height`, `format` (`"png"`), `path`, `baseline`, `compare_to`, `tolerance` (`0..255`), `max_mismatched_pixels`, `max_mismatched_percent`, `diff_path`, `include_diff_image`, `bounds`, `collision_shapes`, `physics`, `grid`, `grid_step`, `camera`, `labels`, `overlays` (`{bounds, collision_shapes, physics, grid, grid_step, camera, labels}`) | Captures the game view as PNG (`width`, `height`, `format`, `bytes`, `png_base64` or relative `path`, an `overlays_drawn` summary, and `skipped_ops` / `skipped_images` when the software rasterizer left something out), optionally compositing pixel-aligned sprite bounds, 2D physics collision shapes, reference grid, camera frame and labels in the same camera and viewport transforms, saving an in-memory `baseline` and/or returning a `comparison` against a named baseline or project-relative PNG (`changed_pixels`, `exact_changed_pixels`, `changed_percent`, `gametest_match`, `within_tolerance`, `changed_bounds`, per-region `changed_regions`, and optional `diff_path` / `diff_png_base64`). |
| `game_log_read` | `since` (optional cursor), `level` (optional min level), `contains` (optional substring), `limit` (optional `1..1000`, default `100`) | Reads recent structured log entries from the Bridge's bounded `slog.Handler` ring buffer with `next_cursor`, `dropped`, `missed`, `has_more`/`truncated`, plus `last_panic` recorded by `pkg/tooling/gamedebug`. |

All tools reject unknown argument fields.

## Deterministic loop control

Agents can drive reproducible test scenarios by pausing the loop, setting a deterministic seed, stepping an exact number of fixed updates, and inspecting entity state immediately afterward:

- `game_loop_step` runs `Update` synchronously on the engine goroutine and calls `Draw` once afterward, so subsequent `game_entity_inspect` or `game_component_get` calls observe the updated state immediately without waiting for the next display frame.
- Simulated time is accumulated per fixed-rate segment (`steps / rate`), so stepping 60 updates at 60 Hz advances `game_time_seconds` and `delta_seconds` by exactly `1.0` with no integer-nanosecond drift.
- `game_loop_step` caps each call at `MaxLoopSteps` (`3600`, or one minute at 60 Hz) to guard against accidental hangs.

## Input injection and viewport mapping

When `Actions`, `Viewport` / `ViewportFunc`, `ViewSizeFunc`, or `OnPointer` are set on `mcp.Config` (or updated via `bridge.SetActions` / `bridge.SetViewport`), agents can drive gameplay directly in virtual game coordinates without computing window letterbox offsets:

- **Actions and axes**: `game_input_action` drives `Actions.Press`, `Actions.Release`, `Actions.Trigger` and `Actions.SetAxis` (`mode` defaults to `"tap"` when `value` is omitted and `"axis"` when `value` is provided; `"axis"` requires `value`); `game_input_axis` sets or clears one or more analog axes in `[-1, 1]`. All injected inputs follow `Actions.Step()` snapshot semantics just like physical inputs, and `Actions.Reset()` clears them cleanly.
- **Pointer coordinate pipeline**: `game_input_pointer` takes `(x, y)` in `"game"` space (virtual canvas points) or `"world"` space (converted to game space through the active scene's `Camera.WorldToView`), then maps game coordinates through `screen.Viewport.GameToView` using `ViewSizeFunc` (falling back to the attached scope's `Env().Window()` and then the viewport's virtual size).
- **Delivery**: Mapped view coordinates are forwarded to `input.Actions` (so `Actions.MapPoint = viewport.ViewToGame` recovers the exact game position for `MouseRegion` bindings). When `in_viewport` is true, `Scene.HitTest` and primary `Scene.DispatchPress` run in game coordinates so pressing at a sprite's position hits that sprite and triggers its `OnPress` callback. `Config.OnPointer` receives every injected `PointerEvent` (a `"click"` emits `PointerPress` followed by `PointerRelease`).

## Tunables and debug console

Pass a [`*gamedebug.Console`](gamedebug.md) via `Config.Console`, `Config.ConsoleFunc`, or `bridge.SetConsole` so agents can inspect and adjust live game variables and run custom debug commands:

```go
console := gamedebug.NewConsole()
console.RegisterFloat("physics.gravity", 0, 2000, &g.gravity)
console.RegisterInt("player.max_jumps", 1, 5, &g.maxJumps)
console.RegisterBool("player.god_mode", &g.godMode)
console.RegisterString("level.name", &g.levelName)
console.RegisterCommand("spawn", "spawn <kind> [count]", g.handleSpawn)
```

- `game_tunables_list` and `game_tunable_get` read live values directly without adding entries to the console's interactive command history or output log.
- `game_tunable_set` enforces each tunable's registered type (`float`, `int`, `bool` or `string`) and numeric `[min, max]` bounds by default without mutating the variable on error. Passing `"clamp": true` clamps out-of-range numeric values into `[min, max]` and sets `"clamped": true` in the response.
- `game_console_exec` executes built-in commands (`help`, `list`, `get`, `set`, `clear`) and game-registered commands through `gamedebug.Console.Exec`, returning `{command, ok, output, lines, error}`. Invalid MCP arguments or a missing `Console` return a tool error, while command execution failures (such as an unknown command name or invalid command arguments) return `ok: false` with the command's `error` message.
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

Pass your [`gamedebug.Overlay`](gamedebug.md) via `Config.Overlay` to expose the exact same frame timings, draw statistics (`gamedebug.Options.DrawStats` or `Overlay.SetDrawStats`), GC/allocation counters (`gamedebug.GCMonitor`), asset memory (`gamedebug.MemoryTracker`) and timing scopes (`gamedebug.Profiler`) shown by the on-screen debug HUD.

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
- **`baseline` and `compare_to`**: Passing `baseline` stores the PNG-space frame in memory under that name (bounded to `MaxScreenshotBaselines` / `MaxScreenshotBaselineBytes`, evicting oldest baselines FIFO). Passing `compare_to` compares the captured frame against a stored baseline name or a relative `.png` file read confined via `os.Root` under `Config.ProjectDir` (falling back to `Config.ScreenshotDir`). The `comparison` object reports `changed_pixels`, `exact_changed_pixels`, `changed_percent`, `max_channel_diff`, `mean_channel_diff`, `gametest_match` (exact `gametest.DiffImage` match), `within_tolerance` (evaluated against optional `tolerance`, `max_mismatched_pixels`, and `max_mismatched_percent`, always `false` on `dimension_mismatch`), overall `changed_bounds`, and up to 32 8-connected `changed_regions`. Pass `diff_path` (confined to `Config.ScreenshotDir`) or `include_diff_image: true` to output a magenta-on-dimmed-grayscale diff PNG matching `gametest.DiffImage`.

## State captures and diffing

Use `game_state_capture` and `game_state_diff` to verify what an action, edit, or simulation run changed in one call:

- `game_state_capture` runs [`statedump.Capture`](statedump.md) over `World`, `Scenes`, `Loop` counters and `Config.StateValues` (optionally filtered to `values` keys) and stores the canonical YAML in memory under `name` (bounded to `MaxStateCaptures` captures and `MaxStateCaptureTotalBytes` bytes).
- `game_state_diff` compares `from` against `to` (or against live state when `to` is omitted), returning `statedump.Diff` output, `changed_entities` (plus `added_entities`, `removed_entities`, `modified_entities`), per-entity `entity_changes` with field-level `before`/`after` values, `value_changes`, `scene_changed` and `steps_delta`. Pass `paths` (such as `["entities"]`, `["entity:0:1.pos"]`, or `["values.score"]`) to scope both the YAML diff and structured summary.

## Logs and recovered panics

Set `Config.Log = &mcp.LogConfig{}` to enable `game_log_read`:

- On `Bridge.Attach`, the bridge installs a tee `slog.Handler` around `slog.Default()` so existing log output continues uninterrupted while records are also captured into a concurrency-safe bounded ring buffer (`DefaultLogCapacity` = 512 entries, dropping the oldest when full). `Bridge.Detach` (or scope disposal) restores the previous `slog.Default()`. Only `slog` records are captured: output of the standard `log` package (`log.Print` and friends) keeps its previous destination and does not reach the buffer.
- Games using a dedicated `*slog.Logger` can pass a pre-created `*mcp.LogBuffer` (`mcp.NewLogBuffer(...)`) in `Config.Log.Buffer`, or set `NoDefault: true` and wire `bridge.LogBuffer()`.
- `game_log_read` supports cursor polling (`since` using the previous response's `next_cursor`, reporting `missed` if lines rolled off the ring buffer and `has_more` when paginating forward with `limit`), tail reads (`since` omitted with `limit`), minimum `level` filtering, and case-insensitive `contains` matching across message, source and attributes.
- Whenever the game records a recovered panic via `gamedebug.Recover`, `gamedebug.RecoverFunc`, or `gamedebug.RecordPanic` (or supplies `Config.Log.LastPanic`), `game_log_read` returns it in `last_panic` (even when `Config.Log` is nil, in which case `configured` is `false`).
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
import "github.com/nsgo-dev/game/pkg/editor/editormcp"
```

### Connecting an agent

`go tool game editor .` serves the open project on a local endpoint of its own: a Unix domain socket that only the user can open, named after a hash of the project's path, in `nsgo-game/editor` under the user's cache directory (`$NSGO_GAME_EDITOR_ENDPOINTS` when set). It is never a network port. When the editor opens another project, the endpoint follows it; a second editor on the same project leaves the first serving and says so on stderr.

Run `go tool game mcp install` once in the project folder to write the project-level MCP configurations that both Claude Code (`.mcp.json`) and Codex (`.codex/config.toml`) read automatically:

```sh
go tool game mcp install
```

`go tool game mcp install` writes or updates the `nsgo-game-editor` entry plus every MCP server enabled in this user's **Settings › Tools**, and removes project entries named like servers disabled there. It copies each enabled server's command and arguments from the per-user editor settings into these project files, so review them before committing. Unrelated MCP servers and other Codex settings are preserved, as are existing file permissions. Installation is idempotent: identical files stay untouched byte for byte, and the command prints what it wrote or left unchanged. You can also pass `-project <dir>` from outside the project directory, or register `go tool game mcp` manually:

```sh
claude mcp add game-editor -- go tool game mcp
```

`go tool game mcp` is an MCP server on standard input and output that finds the editor with the project open and forwards every tool call, resource read and subscription to it, reporting the client's name and transport (such as `Claude Code over stdio`) to the editor's Connect dialog and status chip. It lists the editor's tools, including those other parts of the editor add, and connects again when the editor is closed and opened anew, so the agent's session outlives the editor's. While no editor has the project open, each call answers with an error that says so and how to open it: ``editormcp: no editor has the project open at <root>: open it with `go tool game editor <root>` and try again``.

### Tools

| Tool | Action | Arguments | What it does |
| --- | --- | --- | --- |
| `scene_read` | read | `file`, `offset`, `limit` (all optional) | Reads a scene, prefab, data table or other text file of the project, or `game.yaml`, as lines numbered from 1. While the editor holds unsaved edits to the open scene or `game.yaml`, or staged in-memory edits from a pending agent turn, that file is read as the editor holds it, and the reply says so and that its line numbers differ from the file's on disk; otherwise, and for any other file, it is read from disk, comments included, with the line numbers `game_check` reports. Without `file` it reads the open scene. A path must be relative to the project root and stay inside it. |
| `scene_edit` | edit | `edits` (required), `file`, `label`, `why` (optional; each edit may also carry `why`) | Applies the edits, in order, to the open scene (or, while an agent turn is active, to any scene or prefab in the project in memory) without touching disk. Inside an active turn the edits join that single pending turn and record any `why` explanation on each derived change; outside a turn they record one ordinary undo step labeled `label`. The edits stay unsaved until kept and saved. |
| `turn_start` | edit | `prompt`, `step` (optional) | Opens a pending agent turn so subsequent `scene_edit`, `object_set_field`, and `file_edit` calls across multiple files accumulate into one reviewable turn and one undo step when the person keeps it. |
| `turn_end` | edit | `summary`, `step` (optional) | Marks the active working turn ready for the person's review with an optional completion summary (and optional progress step), returning the current derived turn snapshot. |
| `turn_status` | read | none | Read-only snapshot of the current agent turn: status (`working`, `pending`, `stopped`, `kept`, `reverted`), prompt, progress steps, summary, per-file line counts (`+added`/`−removed`), and derived structural changes (with `kind`, `title`, `detail`, `file`, 1-based `line`, `why`, and `reverted`). There is intentionally no MCP tool that keeps or reverts a turn; only the person decides. |
| `file_edit` | edit | `file` (required), `yaml` (required), `why`, `label` (optional) | Validates and applies a multi-file in-memory edit to `game.yaml`, a data table (`data/*.yaml` / `*.table.yaml`), an animation clip (`*.anim.yaml` / `*.clip.yaml`), a prefab (`*.prefab.yaml`), or a scene (`*.scene.yaml`) inside the active agent turn without writing to disk until kept and saved (enforcing symlink containment, You-only boundaries, `.git/**` refusal, and a 2 MiB size limit). |
| `selection_get` | read | none | The open scene, the active layer, the selected object with its lines of YAML as `scene_read` numbers them, `selectedIds` for every selected object in multi-selection, the file selected in the file browser, the active tool and the view mode. |
| `viewport_image` | read | `scale` (optional, 0.25 to 4), `viewport_overlays` (optional string array) | A PNG of the viewport as it shows now: the scene through the editor's view, with the camera frame, tile grid, selection outline and viewport overlays, at the viewport's size in points times `scale`. Omitting `viewport_overlays` uses the editor's active overlay set; passing overlay IDs (`collision_shapes`, `triggers`, `connections`, `patrol_paths`, `navigation`, `camera_bounds`, `labels`, or active plugin overlay IDs) overrides them for this image, and `[]` draws no overlays. Overlay labels (such as `"pulled → open"` on connection arrows) are rendered into the PNG; images not loaded in memory are left out and counted. |
| `game_check` | run | none | Runs the Problems panel's check (`assetcheck` with the editor's component registry, and `project.Check`, including open unsaved documents in the editor) and returns its visible problems, each with file, 1-based line, 1-based column (where known), and message. |
| `problem_propose_fix` | read | `problem` (required), `explanation` (required), `edit` (required) | Validates a proposed fix for a live problem on a cloned scene, computes the exact `-`/`+` YAML diff, and shows the review card (with Apply fix, Show me, Ignore, and Ask about this problem…) in the editor's Agent panel. Never applies the fix or modifies any file — no MCP tool applies a fix. |
| `history_list` | read | `author` (`all`, `you`, `agent`), `limit` (1–500, default 50) (both optional) | Reads the editor's unified History timeline of manual edits, agent turns, external Git reloads, and saved points (newest first), with author, summary, detail, touched files, timestamps, change/kept counts, unsaved status, and active preview ID. |
| `checkpoint_list` | read | `filter` (`all`, `you`, `agent`; default `all`), `limit` (1–200, default 50) (both optional) | Lists recent project history and persistent turn checkpoints stored in `.nsgo/checkpoints`, newest first, including each entry's actor, kind, title, detail, files, and whether it is persistent and restorable. |
| `checkpoint_restore` | edit | `id` (required) | Restores every file recorded in a persistent checkpoint to its pre-turn bytes as one undoable step; Undo puts the turn back. |
| `review_status` | read | none | Returns the active or most recent change-by-change agent turn review state: whether it is open or pending, the quoted request, round state, file summaries, every change's seen and reverted flags, overlapping edit conflicts and the person's merge choice (`mine`, `agent`, `both`), in-review YAML hand edits tagged `"you"` with edited fields and lines (preserved after Keep for round 2), active blockers (including YAML parse errors), and the last keep/revert outcome. Unsent draft notes are never exposed, privacy policies (`SceneAndData` and `GoSource`) are enforced, and keeping or reverting remains the person's decision. |
| `review_notes_get` | read | none | Reads the active agent turn review's round state, changes, and the change-scoped notes explicitly sent by the author via **Send N notes** or **Revert and redo with this note** (`change_id`, `file`, `object`, `field`, `kind`, `note`, `revert_first`). Unsent draft notes and notes removed before sending are never returned. |
| `review_redo` | edit | `changes` (required non-empty array of `{change_id, detail?, x?, y?, component?, fields?, file_content?, old_value?, new_value?, delta?, yaml_lines?}`) | Redoes only changes the author left notes on in the active review, advancing the same turn to Round 2+ while keeping earlier rounds intact and read-only. Un-noted changes (and any un-noted items in a supplied `file_content`) are refused atomically; redone changes become unseen again with `"Redone from your note: …"` as their Why, and **Keep** still applies the latest round as one undo step. |
| `game_playtest` | run | `file`, `run`, `count`, `seeds`, `out`, `recordings` (all optional) | Validates `playtest.yaml`, runs the configured headless bot playtests with per-seed `.replay.yaml` recordings, writes the YAML report and returns each metric's value, goal status (`met` or `missed`) and heat-map position data. |
| `playtest_define` | edit | `metric` (required), `file`, `rationale`, `replace` (optional) | Validates a proposed playtest metric, generates the canonical `playtest.yaml` diff and queues it as a pending review (`Server.PlaytestReviews` / `ApplyPlaytestReview` / `DismissPlaytestReview`) without modifying `playtest.yaml` on disk until reviewed. |
| `playtest_report` | edit | `file`, `open`, `read`, `suggestions`, `watchRun` (all optional) | Opens or inspects the editor's Playtest Report screen, loads a `playtest.report.yaml` file, updates the Agent's read summary and actionable suggestions, or opens a 1-based run's saved `.replay.yaml` recording in Play mode. |
| `merge_status` | read | `open`, `file` (both optional) | Inspects the active object-by-object scene conflict session, or detects unmerged scene files in the open project's Git repository; with `open: true`, opens `file` or the first detected conflicted scene when no session is open. |
| `merge_resolve` | edit | `id`, `choice` (`agent`, `mine`, `theirs`, `custom`, `unresolved`), `yaml`, `allAgent`, `focus`, `suggest` (`meta`, `yaml`, `reason`, `deleted`) (all optional) | Focuses a conflicted object, updates the agent's suggestion, picks a resolution or custom YAML, or takes the agent's suggestion for every conflict with `allAgent: true`. |
| `merge_finish` | run | none | Once every conflict is resolved, writes the resolved scene, runs `game check` and a short playtest, and stages and finishes the active Git operation (commit for merge, `--continue` for rebase/cherry-pick/revert, or stage-only for stash). |
| `merge_abort` | run | none | Aborts the active Git merge, rebase, cherry-pick, revert, or stash conflict, restores the branch and working tree, and closes the conflict resolver. |
| `play_frozen_context` | read | `includeImage` (optional, default `true`) | Reads the frozen Play mode context: the captured viewport frame PNG (when `includeImage` is true), the author's question, the saved or 60 s rolling input replay (version 2 YAML) with `windowStartFrame`, live entity states and changes, Play marks (`LastMarksThread`), any detected Output panel panic report, and the current Agent stepping log. |
| `play_replay_step` | run | `from_frame`, `to_frame`, `steps` (1–3600, mutually exclusive with `to_frame`), `replay_file` (optional project-relative `.replay.yaml`), `note`, `includeImage` (all optional) | Steps Play mode across `from_frame..to_frame` (up to 3,600 frames per call) using the saved or rolling input replay while keeping Play paused, updating the viewport agent pill (`"agent stepping frames A–B"`) and Agent tab step log (`"Stepped frames A–B with game_loop_step…"`), and optionally returning the resulting paused frame PNG. |
| `code_review_propose` | code | `files` (required array of `{path, content, delete?, note?}`), `prompt`, `round`, `summary`, `failure_explanation`, `suspect_file`, `suspect_line`, `play_impact`, `open` (default `true`), `run_checks` (default `true`) | Stages pending Go files (and optional companion YAML/prefab files) in the editor's file-by-file Go review without touching disk, updates an active review in place as Round 2 inside the same turn, and runs `nsgo check` and `nsgo test` against the pending Go build overlay off the UI goroutine. |
| `code_review_status` | read | none | Returns the active Go code review state: round, pending YAML and Go files with line deltas and confirmation badges, `nsgo check` / `nsgo test` status and failures, `fixRequested` flag, and any unconfirmed Go files waiting after Keep. |
| `code_review_run_checks` | run | `failure_explanation`, `suspect_file`, `suspect_line` (all optional) | Reruns `nsgo check` and `nsgo test` against the pending Go build overlay without modifying files on disk and returns the updated structured check/test status. |
| `states_write` | edit | `file` (required), `yaml` (required), `target` (optional), `open` (optional, default `true`) | Validates a `*.states.yaml` sprite state machine against `animclip.ParseStateMachine` (refusing unknown fields, missing target states, or bad expressions without touching the file), writes canonical YAML to `file`, and optionally opens it in the editor's State Machine graph panel on `target`. |
| `states_test` | run | `file` (optional), `durationSeconds` (optional, default `8`), `rate` (optional, default `60`), `script` (optional) | Drives a `*.states.yaml` state machine with scripted input through `animclip.SimulateStateMachine` on fixed steps and returns the state transition trace, visited states, stuck-state check and summary (such as `Played 8 s of scripted input · no stuck states`). |
| `cutscene_write` | edit | `file` (required), `yaml` (required), `prompt` (optional), `review` (optional), `open` (optional, default `true`) | Validates a `*.cutscene.yaml` file against `cutscene.Parse` (refusing invalid YAML without modifying disk or editor) and opens an interactive Before/After review on the cutscene timeline without overwriting disk until kept (or writes canonical YAML directly when `review: false`). |
| `cutscene_scrub` | read | `file` (optional), `timeSeconds` (optional), `view` (optional: `"before"` or `"after"`) | Opens (when `file` is provided) and scrubs the cutscene timeline playhead to `timeSeconds`, returning the posed camera, actor positions/states, active localized dialogue subtitle, and active review counts. |
| `tileset_read` | read | `file` (optional) | Reads the open YAML tileset (`*.tileset.yaml`, or `file` relative to the project root, falling back to the active layer's `tilesetFile`) as canonical YAML and structured tile rules (collision flags, shapes, animation, terrain, name, weight, custom properties). |
| `tileset_edit` | edit | `ops` (required), `file`, `save` (optional) | Applies tileset operations (`set_tile` for `name`/`solid`/`oneWay`/`flags`/`weight`/`shapes`/`animation`/`terrain`/`neighbors`, `set_property`, `remove_property`, `add_terrain`, `remove_terrain`) to the YAML tileset in one atomic undo step; edits stay unsaved unless `save: true` is passed. |
| `refs_find` | read | `file`, `open` (optional) | Finds every usage of a prefab, scene, or project file grouped across scenes (with override counts), prefab variants (`inherits`), data tables (rows), and Go code (`go/parser` string constants marked `notRewritten`), and optionally opens the References screen in the editor. |
| `refactor_rename` | edit | `from`, `to` (required), `apply` (optional) | Prepares the safe YAML-only rename review in the editor with exact `-`/`+` line diffs and non-rewritten Go warnings without writing to disk until Keep (or applies through `MoveFile` as one undo step when `apply: true`). |
| `tile_query` | read | `layer`, `tileLayer`, `empty`, `tile`, `tiles`, `tileName`, `tileNames`, `rect`, `minCells`, `diagonal`, `includeCells` (all optional) | Queries cells and connected regions or empty pockets on an inline tilemap layer (such as `"an 11 × 5 pocket at columns 2–12, rows 12–16"`), returning bounding rectangles, cell counts, summaries, and coordinates. |
| `tile_paint` | edit | `layer`, `tileLayer`, `tile`, `tileName`, `flipH`/`flipV`/`flipD`, `weighted`, `seed`, `noAdjacentDuplicates`, `rect`, `rects`, `cells`, `label`, `review` (default `true`) | Paints explicit cells, inclusive rectangles, or deterministic seeded weighted random tile picks on an inline tilemap layer as one undoable edit, tracked in the active region-based tile edit review by default. |
| `tile_autotile` | edit | `layer`, `tileLayer`, `terrain`, `rect`, `rects`, `cells`, `place`, `label`, `review` (default `true`) | Re-edges (or places and re-edges) a region on a layered tilemap using a YAML tileset terrain set's 4-bit cardinal or 8-bit blob autotile rules in one undoable edit, tracked in the active tile edit review by default. |
| `tile_erase` | edit | `layer`, `tileLayer`, `rect`, `rects`, `cells`, `label`, `review` (default `true`) | Erases explicit cells or inclusive rectangles on an inline tilemap layer as one undoable edit, tracked in the active tile edit review by default. |
| `tile_review_status` | read | `view` (optional: `"before"` or `"after"`), `focus` (optional region ID) | Returns the active tile edit review's grouped regions (`painted`, `replaced`, `erased`), cell counts, bounding rectangles, revert flags, agent steps, and Before/After state; optionally switches view or focuses a region. |
| `tile_review_revert_region` | edit | `id` or `index` | Marks one region in the active tile edit review as reverted and updates the live 2D viewport. |
| `tile_review_restore_region` | edit | `id` or `index` | Restores a reverted region in the active tile edit review and updates the live 2D viewport. |
| `tile_review_keep` | edit | none | Commits all non-reverted regions of the active tile edit review cleanly as one undoable step in `History` and closes the review. |
| `tile_review_revert_all` | edit | none | Reverts all pending tile review changes back to the pre-review tilemap and closes the review. |
| `object_set_field` | edit | `field`, `value` (required), `id`, `component`, `property`, `label`, `why` (optional) | Sets one inspector field, component field (`component`) or custom property (`property: true`) on the object `id` through the inspector's own `Editor` methods, as one undo step outside a turn. Without `id`, see [Edit targets](#edit-targets). |
| `object_locate_yaml` | read | `id`, `open` (both optional) | Returns an object's 1-based line and YAML snippet in the open scene, defaulting to the selected object; `open: true` also opens the scene at that line through `Editor.OnOpenFileAtLine`. |
| `object_copy_yaml` | read | `id` (optional) | Serializes an object (local or resolved) in the open scene as canonical `sceneformat` YAML, copies it to the editor clipboard, and shows the copy toast. |
| `object_paste_yaml` | edit | `yaml`, `layer` (optional) | Parses canonical `sceneformat` object YAML (or the editor's current clipboard when `yaml` is omitted, capped at 4 MiB) and places a copy with fresh unique IDs on `layer` as one undo step, reporting the layer as `layer`. Without `layer`, see [Edit targets](#edit-targets). |
| `object_duplicate` | edit | `id` (optional) | Clones the object `id` (and its children) on the same layer with fresh unique IDs as one undo step, reporting it as `sourceId`. Without `id`, see [Edit targets](#edit-targets). |
| `object_group` | edit | `ids`, `name` (optional) | Creates a new parent group object and reparents the objects `ids` under it in one undo step while preserving world transforms, reporting them as `children`. Without `ids`, see [Edit targets](#edit-targets). |
| `object_move_under` | edit | `id`, `parent`, `listCandidates` (optional) | Reparents the object `id` under `parent` (or to the top level of its layer when `parent` is empty) in one undo step while preserving world transform, or lists valid candidate parents without modifying the scene when `listCandidates: true`, which alone defaults `id` to the selected object. Without `id` for a move, see [Edit targets](#edit-targets). |
| `object_select_same_prefab` | read | `id` (optional) | Selects every object in the open scene sharing the same `prefab` or instanced `scene` reference as the target object and returns `selectedIds`. |
| `object_visibility` | edit | `id`, `hidden`, `locked` (optional) | Sets the local or resolved object `id`'s session-only hidden and/or locked state in the editor viewports without modifying the scene file. Without `id`, see [Edit targets](#edit-targets). |
| `object_find_usages` | read | `id` (optional) | Finds project-wide references or search matches for an object's instanced scene, prefab, or ID in the open project (opening the Find Usages screen for scene/prefab targets or the Search panel for plain IDs). |
| `release_notes_draft` | edit | `markdown`, `kept_reviews` (optional) | Drafts player-facing Markdown release notes from git commits since the last release tag (or initial commit), kept review summaries, and `changelog.d` fragments (or saves validated `markdown`, capped at 256 KiB), writing atomically and exclusively to the file read by `game release -release-notes`. |
| `release_review_uploads` | run | none | Opens the Build and Release screen's **Review uploads** confirmation dialog listing every artifact, size, store destination (`itch.io`, `Google Play · internal testing`, `TestFlight`), and version. Never starts an upload: store uploads always require the person's explicit confirmation, whatever the permission profile. |
| `agent_report_step` | read | `text` (required), `objects`, `status` (`running` or `done`, default `done`) (optional) | Reports a live progress step in the Agent panel, optionally naming the scene object IDs it touches, and returns any follow-up instructions the person added to the running request. |
| `agent_turn_get` | read | `turnId`, `includeScene`, `includeImage` (optional) | Reads the latest (or specified) agent turn submitted from the `⌘L` in-place composer or Agent panel, returning the prompt, `skipPlan` flag, selected object IDs and names, open scene path and YAML, and the captured viewport PNG as an image. |
| `agent_turn_reply` | edit | `turnId`, `reply`, `status` (optional) | Updates an agent turn's status (`pending`, `running`, `done`, `stopped`) and/or appends the agent's reply text in the editor's Agent panel thread. |
| `tasks_list` | read | `include_dismissed` (optional, default `false`) | Lists background agent tasks across Needs you, Running, Ready to review, Queued, and Failed (plus dismissed/completed tasks when `include_dismissed` is `true`), along with active per-object holds and `TaskSummary` badge counts (`dockBadge = needsYou + readyToReview + failed`). |
| `tasks_submit` | edit | `prompt` (required), `title`, `mode` (`"queue"` or `"parallel"`, default `"queue"`), `scene` (optional) | Submits a background agent request to the editor's task list: `"queue"` waits behind the active or unreviewed turn and starts only after it is kept or reverted; `"parallel"` starts immediately (up to the concurrency cap) as a separate session with its own numbered overlay. |
| `tasks_control` | edit | `action` (required: `"keep"`, `"revert"`, `"dismiss"`, `"stop"`, `"retry"`, `"open"`, `"focus"`, `"allow"`, or `"deny"`), `task_id`, `task_ids` (for bulk dismiss) | Controls background agent tasks: safely reverts or stops the calling session's own task (a revert preserves unrelated parallel/kept edits), dismisses Failed tasks, retries a task, or focuses/opens a task's scene (confined to project root). Keeping a task, dismissing a task that is Ready to review, and answering a permission prompt are the person's decisions: `keep`, `allow`, and `deny` are always refused (`ErrTaskDecisionYouOnly`), as are `revert` and `stop` of a task bound to another session, `stop` of a Ready to review task, and `dismiss` of a Ready to review task. |
| `project_context` | read | none | Returns the shared project context the agent reads before every request: `brief.yaml` (canonical YAML, parsed fields, missing sections/gaps, and structured playtest targets) and `AGENTS.md` (plain Markdown project rules and rule count), plus any pending document reviews. |
| `brief_edit` | edit | `yaml` or structured fields (`pitch`, `genre`, `tags`, `platforms`, `artDirection`, `toneAndAudio`, `feel`), `summary`, `rationale` | Strictly validates a proposed `brief.yaml` edit (refusing unknown fields with line numbers) and queues a reviewable `DocReview` diff without touching disk until the person keeps it. |
| `rules_edit` | edit | `markdown` or `appendRule`, `summary`, `rationale` | Proposes an edit or appended bullet rule for `AGENTS.md` as a reviewable `DocReview` diff without touching disk until the person keeps it. |
| `permissions_draft` | read | `request` (optional), `rules` (required) | Opens a You-only permissions draft card with nothing ticked by default; never modifies `permissions.yaml` directly. |
| `game_delete_file` | delete | `file` (required), `reason` (optional) | Turned off by default in **Settings › Tools** (not offered to connected clients until enabled). When enabled, proposes deleting a project file in the Review changes panel with its line count and scene usages without touching disk until confirmed (refusing You-only files, symlinks, `.git/**`, `game.yaml`, open scene tabs, and >1 MiB files). |
| `datatable_review_propose` | edit | `files` (required), `prompt`, `checks` (optional) | Validates one or more version-1 YAML data table files against `datatable.ParseDocument` (and any registered Go row type) and opens the row-by-row Data Table review screen with old → new, `Δ%`, per-record Revert/Restore, and optional named agent check figures without modifying disk until Keep. |
| `datatable_review_checks` | edit | `checks` (required) | Attaches or refreshes named agent check figures (`label`, `value`, `before`, `basis`) on the active Data Table review and clears `checksOutOfDate` after the person reverts, edits, or reorders records. |
| `datatable_review_status` | read | `file` (optional) | Reports the active Data Table review's files, kept/reverted/user-edited counts, active record states, attached check figures, and whether `checksOutOfDate` is true. |
| `sketch_read` | read | `includeImage`, `scale` | Reads the active or sent viewport sketch marks (Pen, Arrow, Box, Text) with sequential badge numbers, exact scene coordinates, labels, prompt, hold state, and an optional rendered PNG of the viewport with numbered marks overlaid. |
| `sketch_reading` | edit | `summary`, `items`, `assumptions` | Posts the agent's restatement card (`"I read your sketch as: …"`) and assumptions for a sent viewport sketch; while a sent sketch awaits confirmation, `scene_edit` and other mutating scene tools hold cleanly until the person chooses **Go ahead**, **Correct a mark**, or replies in words. |
| `plan_propose` | read | `title`, `steps` (required); `id`, `turnId`, `prompt`, `summary`, `estimate`, `reason`, `files`, `questions`, `wait` (optional) | Proposes a structured plan with clarifying questions (with visual previews and ghost/title overrides), editable numbered steps, planned Scene Tree nodes, and agent-violet viewport ghosts before modifying any scene or file. Waits (when `wait` is not `false`) until the person approves (`Approve` or `Just do it`) or stops, returning the structured `AgentPlanDecision` with answers, edited step counts/notes, and removed steps. |
| `plan_step_update` | read | `step`, `status`, `complete` (optional) | Updates an approved plan step's progress (`pending`, `running`, `done`, `failed`, `skipped`) or completes the plan so the turn's review lists every step and marks which were done, and reports whether the person pressed Stop. |
| `phone_sessions_list` | read | `{}` | Lists Play-mode web build sessions connected to the editor's LAN QR MCP bridge, including server status, LAN URL, and each connected phone/browser session. |
| `phone_call_tool` | run | `tool` (required); `session_id`, `arguments`, `timeout_ms` (optional) | Forwards a game MCP tool call (such as `game_state_dump`, `game_scenes_list`, `game_entities_list`, `game_loop_step`, `game_input_action`, `game_screenshot`) to a connected phone/browser session via `Editor.CallWebBridgeTool`, governed by `permissions.yaml` (`playtests: phone/<tool>`) and privacy rules. |

Every tool registered on the editor's MCP server can be turned on or off per computer in **Settings › Tools** (`Editor.SetMCPToolEnabled`): disabling a tool removes it from the server's advertised tool list (`tools/list_changed`) and returns a readable refusal if called. Outbound content categories in **Settings › Usage and privacy** (**WHAT MAY LEAVE THIS COMPUTER**: Scene and data files, Viewport images, Go source code, Art and audio files) are enforced for every connected agent across all tools (including symlink-resolved file paths) and all resources (`editor://scene`, `editor://selection`, `editor://agent/turn`, `editor://brief`, `editor://rules`, `editor://context`), refusing calls with the reason whenever the requested content kind is switched off. Additional MCP servers configured in **Settings › Tools** (starting with `nsgo-game` for the running game via `go tool nsgo mcp`) are synced into `.mcp.json` and `.codex/config.toml` via `editormcp.InstallWithServers` for agents you start yourself. When the editor starts Claude Code or Codex from the composer, it passes the editor's server and these servers inline and ignores the project's `.mcp.json`, `.codex/config.toml`, project settings and hooks (see "Local agent launch policy" in the editor guide).

The edits `scene_edit` takes, each an object with `op`:

| `op` | Fields | Editor method |
| --- | --- | --- |
| `add` | `object` (as the scene file writes it), `layer`, `parent` | `PlaceObject`, then `ReparentObject` under `parent` |
| `remove` | `id` | `DeleteObject` |
| `move` | `id`, `x`, `y` | `MoveObject` |
| `set` | `id`, `fields` | the object's setters: `id`, `name`, `x`, `y`, `z`, `rotation`, `scaleX`, `scaleY`, `alpha`, `originX`, `originY`, `flipX`, `flipY`, `sprite`, `material`, `particles`, `posZ`, `rotX`, `rotY`, `rotZ`, `scaleZ`, `depth`, `mesh`, `model`, `gridMap`, `occluder`, `light`, `camera` |
| `set` | `id`, `component`, `fields` | `SetComponentField` for each field |
| `add_component` | `id`, `component`, `fields` | `AddComponent`, then `SetComponentField` |
| `remove_component` | `id`, `component` | `RemoveComponent` |
| `set_property`, `remove_property` | `id`, `key`, `value` | `SetProperty`, `DeleteProperty` |
| `reparent` | `id`, `parent`, `index` | `ReparentObject` |
| `move_to_layer` | `id`, `layer`, `index` | `MoveObjectToLayer` |

An `add`'s object is read by the scene format's own reader with the editor's component registry, so it is refused as opening the file would refuse it. When the editor refuses any edit of a call, none of them applies: the scene, its undo history and its file on disk stay as they were, and the tool error names the file, the edit, the object, the component and field, and the fix, as in `scene_edit: scenes/room.scene.yaml: edit 2 (set) on object "hero" component "body" field "sped": unknown field (registered: friction, layer, solid, speed); use a component and fields the project registers, as the message lists them. Nothing was changed.` The structured result holds the same as an `editormcp.EditError`.

When an agent turn is active (`turn_start` through `turn_end`, or started from the editor's Agent panel), every edit made through `scene_edit`, `object_set_field`, and `file_edit` across scenes, prefabs, data tables, `game.yaml`, and clips is staged in memory into that one pending turn without writing to disk. The editor derives the turn's changes structurally by diffing each touched file against its pre-turn state (carrying any `why` explanation supplied on the tool call), lets the person revert or restore individual changes or whole files while pending, and commits all remaining changes across every touched file as one `AuthorAgent` undo step on **Keep** (or restores every touched file byte-for-byte on **Revert**). `turn_status` reports the derived changes and which ones the person reverted; by design there is no MCP tool that keeps or reverts an agent turn. Outside a turn, each `scene_edit` or `object_set_field` call remains one ordinary undo step on the open scene.

#### Edit targets

The selection is context, never a write target: the person and the agent share one editor, and a click while the agent works must not redirect its write. `object_set_field`, `object_duplicate`, `object_group`, `object_move_under` and `object_visibility` resolve the objects they edit once, before they hold them for a parallel task, and hold, write and report those same objects:

- With `id` (or `ids`), they edit the objects named.
- Without it, inside an open agent turn, they edit the objects the person's request pinned when it was sent, the `selectionIds` `agent_turn_get` returns, whatever is selected now. A tool that edits one object refuses a pin of several and asks for `id`.
- Without it, outside a turn, they refuse with an `EditError` whose fix reads `pass id; selection_get shows the selected object`, and change nothing; in a turn whose request pinned nothing, the fix points at `agent_turn_get` instead.
- When a pinned object is no longer in the open scene, they refuse with the problem `the object the request was about was removed or renamed` and the fix to read the scene again with `scene_read`, distinct from the error for an unknown `id`.

`object_paste_yaml` resolves its destination layer the same way, once, before it holds the layer (`layer:<name>`), and holds, pastes onto and reports (`layer`) that same layer:

- With `layer`, it pastes onto the layer named.
- Without it, inside an open agent turn whose request pinned objects, it pastes onto their layer, whatever is selected now and whichever layer is active. A pin whose objects are on several layers is refused and asks for `layer`; a pinned object that is no longer in the open scene is refused with the same removed-or-renamed problem.
- Otherwise, it pastes onto the layer a copied scene YAML (as `object_copy_yaml` writes) names, when the open scene has that layer.
- Otherwise, it refuses with an `EditError` whose fix reads `pass layer; scene_read lists the layers`, and changes nothing. It never falls back to the selected object's layer or the active layer.

The read-only `object_locate_yaml`, `object_copy_yaml` and `object_find_usages` still default to the selected object, since they cannot write to the wrong place.

While the editor's automatic saving is failing (for example into a read-only folder or a full disk), every tool result returned by `editormcp` appends a warning stating that edits are not on disk along with the failure reason (and attaches `nsgo.dev/editsNotOnDisk`, `nsgo.dev/saveError`, and `nsgo.dev/saveWarning` in `_meta`, plus `SaveWarning` on `Result` / `AutomationResult`) until a save succeeds, so the agent does not build on edits that have not landed on disk.

### The change stream

The server exposes six resources: `editor://scene` (the open scene's YAML as the editor holds it), `editor://selection` (the selection as `selection_get` reports it in JSON), `editor://agent/turn` (the latest request started from the `⌘L` in-place composer or Agent panel: JSON metadata including `selectionIds`, `scenePath`, `sceneYaml`, and `skipPlan`, plus the captured `image/png` viewport blob), `editor://brief` (the open project's `brief.yaml` as canonical YAML), `editor://rules` (the open project's `AGENTS.md` as plain Markdown), and `editor://context` (the combined JSON project context returned by `project_context`). A client that subscribes to them is notified of each change, and the notice's `_meta` says who made it, `"nsgo.dev/by": "agent"` for a tool call and `"person"` for the editor's own controls, with the editor's revision under `"nsgo.dev/revision"` (and `"nsgo.dev/turnId"` / `"nsgo.dev/skipPlan"` on `editor://agent/turn` notices). A burst of changes may come as one notice.

### Adding tools

The tools are a registry that other parts of the editor join with `Server.AddTool`, so that every action with a button or a shortcut has a tool. A `Tool` has a name, a description, a JSON Schema for its arguments, an `Action` (`ActionRead`, `ActionEdit`, `ActionCreate`, `ActionDelete`, `ActionRun`, or `ActionCode`, which the editor's permission rules decide on), an optional `Target` resolver, and a handler. The handler runs on one of the server's goroutines and reaches the editor with `Call.Editor`, which runs a function on the editor's goroutine and waits for it, so a tool may wait for work done elsewhere and return an image. Clients are told the tool list changed.

### Permissions enforcement

Before running any mutating or running tool call (every action other than `ActionRead`), `editormcp.Server` evaluates the call against the open project's `permissions.yaml` (`editor.PermissionsFile`, defaulting to the `Balanced` profile when the file is absent) from top to bottom using first-match-wins rules:

- **`allow`**: runs the tool immediately.
- **`ask`**: invokes `Options.OnPermissionAsk` when configured, or queues a pending permission request on `Editor` (`PendingPermissionRequests`) and returns a structured `*editormcp.PermissionError` with `decision: "ask"` without running the tool. Answering **Always** (`PermissionAnswerAllowAlways` / `Editor.ApproveAlwaysPermission`) inserts an `allow` rule ahead of the matching rule, persists `permissions.yaml`, and shows the `"Always permission rule saved"` toast with **Show** and **Undo**.
- **`deny`**: records a blocked step on `Editor` (`BlockedPermissions`), returns a structured `*editormcp.PermissionError` naming the exact rule index, rule summary, base profile, and Settings link (for example `"Settings › Permissions · rule 7 of 7"`), and never calls the underlying tool handler.

Enforcement is fail-closed: `Server.AddTool` refuses empty or unrecognized `Action` values; a corrupt or unreadable `permissions.yaml` denies mutating/running calls until fixed; file targets are canonically cleaned and symlink-resolved against the project root before rule matching (refusing absolute paths, `..` traversal, and symlink escapes, and matching case-insensitively on macOS/Windows); and multi-target tools (`refactor_rename` checking `from`, destination `to`, and rewritten referencing files when `apply: true`; `merge_finish` checking both the resolved scene edit and `git merge` run) apply the strictest verdict (`deny > ask > allow`).

Rules govern the editor's MCP server tools only. Files modified directly by an external agent outside MCP continue to surface through the editor's disk watcher.

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

`Options.Run` hands functions to the editor's goroutine (`cx.Post` for an editor in a window); without it the server routes calls and session notifications through the editor's single headless executor (`Editor.PostOrRunHeadless`), serialized with `Editor.Post` and `Editor.RunHeadless` and drained by `Editor.AttachScope`, for an editor no window drives. Callers driving the same scope-less `Editor` directly from a test or script while the server is active must wrap their editor calls in `Editor.RunHeadless`. `Server.Connect` serves one session on any MCP transport, as a test does with `mcp.NewInMemoryTransports`.

### Why its own server

NSGo's `automation` registers app tools only in `nsgo dev -automation` and test builds, and `nsgo mcp` reaches only the app that `nsgo dev -automation` runs; an editor is an ordinary build. An app tool's handler also runs synchronously on the graph goroutine (nsgo-dev/nsgo#2342), which cannot wait for a project check without holding the editor, and returns JSON only, so a screenshot reaches the agent as base64 text rather than an image (nsgo-dev/nsgo#2343). The editor's server uses the official MCP Go SDK, which NSGo's own `nsgo mcp` is built on, so its tools wait off the editor's goroutine and `viewport_image` returns a real image. `Server.RegisterAutomation` still offers the same tools to `nsgo mcp` for an editor run under `go tool nsgo dev -automation`, each running on the graph goroutine from start to end and returning an `AutomationResult` with the PNG in base64.

## Editor MCP You-only rule and proposed deletions (#1015)

In `pkg/editor/editormcp`, the editor's MCP server enforces two non-bypassable trust boundaries regardless of the active permission profile (`Cautious`, `Balanced`, or `Autonomous`):

- **You-only refusal (`*editormcp.YouOnlyError`)**: Every non-read tool call (built-in or registered via `Server.AddTool`) is checked before its handler runs across explicit argument paths, derived targets (including bare-stem `refactor_rename` and `release_notes_draft`), and open document paths, and refused if any target resolves to `permissions.yaml`, `.nsgo/permissions.yaml`, `tools.yaml`, `mcp.yaml`, `models.yaml`, `usage.yaml`, or `Editor.SettingsPath`.
- **`permissions_draft`**: Submits a You-only draft card (`Editor.SubmitPermissionsDraft`) with zero rules ticked by default. It never modifies `permissions.yaml` directly; only explicit person ticks + **Apply** can persist changes.
- **`game_delete_file`**: Refuses You-only files, symlinks, `.git/**`, `game.yaml`, open scene tabs, and >1 MiB files; for ordinary project files it proposes a deletion in the Review changes panel with line count and scene usages (`Editor.ProposeFileDeletion`) without deleting the file on disk. **Keep all** never deletes pending files; confirming deletes via a checkpoint command that `⌘Z` restores byte-for-byte.
