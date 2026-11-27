# `game/mcp` Tool Reference

`github.com/nsgo-dev/game/pkg/mcp` registers game-specific MCP tools on NSGo's reactive graph through `github.com/nsgo-dev/nsgo/automation`. When the game runs under `go tool nsgo dev -automation` (or `go tool nsgo test`), `go tool nsgo mcp` serves these tools alongside NSGo's built-in view tools. In ordinary and release builds, `automation.Register` is a no-op.

## Connecting an MCP client

```sh
# One-time client setup
claude mcp add nsgo -- go tool nsgo mcp

# Start the game with automation enabled
go tool nsgo dev -automation
```

## Wiring `mcp.Bridge` in Go

Call `mcp.Attach(cx, cfg)` (or `mcp.New(cfg)` + `bridge.Attach(cx)`) on the engine goroutine. Every `mcp.Config` field is optional; tools remain registered until `cx` is disposed or `bridge.Detach()` is called. Use `*Func` getters when level restarts replace the active scene, world, loop, input actions, console, physics world, or asset manager.

```go
bridge, err := mcp.Attach(cx, mcp.Config{
	// Scene & entity inspection (#461)
	Scenes:      g.scenes,                                         // *scene.Manager
	Scene:       g.activeScene,                                    // or SceneFunc: func() *scene.Scene
	SceneName:   func(c *scene.Controller) string { return c.ID }, // optional display names
	NamedScenes: map[string]*scene.Scene{"title": g.title},
	WorldFunc:   func() *entities.World { return g.world },
	EntityName:  func(e entities.Entity) string { return g.names[e] },
	EntityTags:  func(e entities.Entity) []string { return g.tags[e] },
	EntityLayer: func(e entities.Entity) string { return g.layers[e] },

	// Fixed-step loop & seeding (#462)
	Loop:          g.loop, // or LoopFunc: func() *game.Loop
	Random:        g.rng,  // *random.Stream
	RandomStreams: g.rngs, // *random.Streams
	SetSeed:       g.resetSeed,

	// Input at game coordinates (#463)
	Actions:      g.actions, // or ActionsFunc: func() *input.Actions
	Viewport:     &g.viewport,
	ViewSizeFunc: func() core.Size { return g.viewSize },
	OnPointer:    g.handlePointer,

	// Prefabs for live spawning (#464)
	Prefabs: map[string]mcp.PrefabSpawner{"slime": g.spawnSlime},

	// Screenshots & diagnostic overlays (#465)
	Physics:              g.physics,      // or PhysicsFunc
	RigidPhysics:         g.rigidPhysics, // or RigidPhysicsFunc
	DrawFrame:            g.drawFrame,
	CaptureFrame:         g.captureFrame,
	ScreenshotDir:        g.screenshotDir,
	ScreenshotBackground: core.RGB(0x0f172a),

	// Debug tunables & console (#466)
	Console: g.console, // or ConsoleFunc: func() *debug.Console

	// Input recording & replay (#467)
	Replay: g.replayCtrl, // *mcp.ReplayController (implements replay.ActionSource)

	// Performance telemetry (#468)
	Overlay:         g.overlay,  // *debug.Overlay
	Profiler:        g.profiler, // *debug.Profiler
	GC:              g.gcMon,    // *debug.GCMonitor
	Memory:          g.memTrack, // *debug.MemoryTracker
	DrawStatsFunc:   g.drawStats,
	RenderStatsFunc: g.render3DStats,

	// Assets & hot reload (#469)
	Assets:      g.assets,    // or AssetsFunc: func() *assets.Manager
	Resources:   g.resources, // *resource.Library
	Shaders:     g.shaders,   // *hotreload.ShaderReloader
	Materials:   g.materials, // *hotreload.MaterialReloader
	ReloadAsset: g.reloadAsset,
})
```

Additional registration helpers on `*mcp.Bridge`:

- `mcp.RegisterComponent[T](bridge, name)` / `mcp.RegisterComponentFunc(bridge, name, read)` — expose transient/non-codec components for inspection (built-in `"transform2d"`, `"world_transform2d"`, and `"object_ref"` plus `entities.RegisterCodec` components are readable automatically; `NaN`/`±Inf`/funcs/chans/cycles are safely sanitized).
- `bridge.RegisterPrefab(name, spawner)`, `bridge.RegisterEntityPrefab(p)`, `bridge.RegisterScenePrefab(name, doc)` — register prefabs for `game_entity_spawn`.
- `bridge.RegisterAsset(mcp.CustomAsset{...})`, `bridge.RegisterAtlas(key, group, jsonPath, atlas, readFile)` — expose standalone assets for `game_assets_list` and `game_asset_reload`.

All MCP tools strictly refuse unknown JSON argument fields.

## Tool catalog

### 1. Scene, layer, entity and component inspection

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_scenes_list` | `{}` | Active scene stack (bottom to top), `current`, `stack_depth`, `transitioning`/`progress`, layer counts, camera state and inactive `NamedScenes` |
| `game_layers_list` | `scene` (opt), `include_hidden` (opt, default `true`) | Scene layers in draw order (`index`, `draw_index`, `name`, `space`, `visible`, `order`, `parallax_x`/`parallax_y`, offsets, repeat, `depth_sort`, item/batch/sprite/tilemap/drawable counts) |
| `game_entities_list` | `component`, `layer`, `name` (substring), `tag`, `parent` (ref or `"root"`), `include_components` (default `false`), `offset`, `limit` | Live entities in ascending slot order with `total`, `returned`, stable generational `ref` (`"entity:<index>:<generation>"`), metadata, `parent`, `child_count`, `components` names and optional `values` |
| `game_entity_inspect` | `ref` (req) | One live entity's `ref`, `index`, `generation`, `name`, `layer`, `tags`, `parent`, `children` refs and all readable `components` values |
| `game_component_get` | `ref` (req), `component` (req) | Serialized `value` of one component on a live entity |

Stable entity references use `"entity:<index>:<generation>"` (`mcp.FormatEntityRef` / `mcp.ParseEntityRef`). Stale references to destroyed or recycled slots return a `"not alive"` error.

### 2. Fixed-step loop control and seeding

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_loop_status` | `{}` | `paused`, effective `time_scale`, `step_seconds`, `step_hz`, `steps`, `game_time_seconds`, `seed_configured` |
| `game_loop_pause` | `{}` | Pauses `Loop` without catch-up backlog; returns updated loop status |
| `game_loop_resume` | `{}` | Resumes `Loop`; returns updated loop status |
| `game_loop_step` | `count` (opt, default `1`, `1..3600`) | Synchronously runs `count` fixed updates (`Loop.StepNow`) and one `Draw`; returns `steps_advanced`, `total_steps`, `game_time_seconds`, `delta_seconds`, `paused` (60 steps at 60 Hz advances exactly `1.0` s) |
| `game_loop_time_scale` | `time_scale` (req, finite `>= 0`; `0` resets to `1x`) | Updates `Loop.TimeScale`; returns updated loop status |
| `game_loop_seed` | `seed` (req, uint64) | Re-seeds `Random`, `RandomStreams` and/or `SetSeed`; returns `{seed}` |

### 3. Input injection at game coordinates

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_input_action` | `name` (req), `mode` (`"press"` \| `"release"` \| `"tap"`, default `"tap"`), `value` (opt, `-1..1`) | Injects action hold/release/single-step tap or analog value into `input.Actions`; returns current step state (`pressed`, `just_pressed`, `just_released`, `axis`) |
| `game_input_axis` | `name` (opt), `value` (`-1..1`), `axes` (map of name -> `-1..1`), `clear` (opt bool) | Sets or clears 1D/2D analog axes in `input.Actions`; returns `{axes, cleared}` |
| `game_input_pointer` | `x` (req), `y` (req), `action` (`"press"` \| `"release"` \| `"move"` \| `"click"`, default `"click"`), `space` (`"game"` \| `"world"`, default `"game"`), `button` (`"left"` \| `"right"` \| `"middle"`, default `"left"`) | Maps coordinates through `screen.Viewport` and active camera, dispatches to `input.Actions`, `scene.Scene.HitTest`/`DispatchPress`, and `Config.OnPointer`; returns mapped coordinates and topmost hit details |
| `game_input_actions_list` | `{}` | `configured`, all registered `actions` (with step states and binding counts) and `axes` |

### 4. Live entity spawning, destruction and component editing

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_prefabs_list` | `{}` | Registered prefabs (`name`, `kind`: `"custom"`, `"entity_prefab"`, `"scene"`) |
| `game_entity_spawn` | `prefab` (opt), `overrides` (opt object), `parent` (opt ref), `x` (opt), `y` (opt), `components` (opt map) | Spawns a registered prefab or empty entity, returns `ref`, metadata, hierarchy links and initial `components` |
| `game_entity_destroy` | `ref` (req), `policy` (opt: `"cascade"` \| `"orphan"` \| `"keep_world"` \| `"reparent"`) | Destroys the entity and reports `destroyed` count |
| `game_component_set` | `ref` (req), `component` (req), `value` (req), `replace` (opt bool, default `false` patches existing fields) | Updates a live component so the next step observes the new value; returns `{ref, component, value}` |
| `game_component_remove` | `ref` (req), `component` (req) | Removes one component from a live entity; returns `{ref, component, removed}` |

### 5. Screenshots and diagnostic overlays

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_screenshot` | `scene` (opt), `width`/`height` (opt pair, `1..8192`), `format` (`"png"`), `path` (opt relative path inside `ScreenshotDir`), top-level booleans or `overlays` object (`bounds`, `collision_shapes` / `physics`, `grid`, `grid_step`, `camera`, `labels`) | Captures the game view as PNG (`png_base64` or written `path`), with `width`, `height`, `bytes` and `overlays_drawn` counts |

### 6. Debug tunables and console commands

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_tunables_list` | `prefix` (opt) | Sorted debug console tunables (`name`, `type`, `value`, `default`, `min`, `max`) |
| `game_tunable_get` | `name` (req) | One tunable's current value, type, default and bounds |
| `game_tunable_set` | `name` (req), `value` (req), `clamp` (opt bool, default `false`) | Validates type/bounds (or clamps), updates tunable and returns `old_value`, `value`, `clamped` |
| `game_console_commands_list` | `include_builtin` (opt, default `true`) | Built-in (`clear`, `get`, `help`, `list`, `set`) and custom debug console commands |
| `game_console_exec` | `command` (req) | Executes a console command line; returns `ok`, `output`, `lines`, `error` |

### 7. Input recording and deterministic replay

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_replay_status` | `{}` | Current recording/playback status, frame progress and `saved_recordings` |
| `game_replay_record_start` | `name` (opt), `seed` (opt), `step_rate` (opt), `actions` (opt), `axes` (opt), `metadata` (opt), `reset` (opt bool) | Begins recording per-step inputs via `ReplayController` |
| `game_replay_record_stop` | `name` (opt), `save_path` (opt relative path under `ReplayConfig.SaveDir`) | Stops recording, stores it and returns deterministic YAML (`yaml`), frame/event summary and `saved_path` |
| `game_replay_play` | `name` OR `yaml` (one required), `run_to_end` (opt bool), `steps` (opt int, exclusive with `run_to_end`), `reset` (opt bool, default `true` when `ResetFunc` is set) | Replays a stored or inline YAML recording, optionally advancing frames synchronously |
| `game_replay_stop` | `{}` | Stops active replay playback |
| `game_replay_list` | `{}` | Lists stored recordings with `frames`, `seed`, `step_rate`, `events`, `actions`, `axes`, `metadata` |

### 8. Performance telemetry

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_perf_readout` | `include_history` (opt), `history_limit` (opt), `include_zones` (opt), `zones_limit` (opt) | `fps`, `frame_ms`/`update_ms`/`draw_ms`, percentiles (`p50`/`p95`/`p99`), `draw_calls`/`sprites`/`batches`/`triangles`, `allocs_per_frame`, heap/GC stats, `render3d`, `asset_memory`, optional `history` and `zones` |
| `game_perf_zones` | `limit` (opt) | Named `debug.Profiler` zones sorted by recent cost (`last_ms`, `avg_ms`, `max_ms`, `calls`) |
| `game_perf_reset` | `{}` | Resets overlay frame stats, draw stats, profiler zones and GC monitor history |

### 9. Assets and live hot reload

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_assets_list` | `group` (opt), `state` (`"loaded"` \| `"loading"` \| `"unloaded"` \| `"failed"`), `kind` (opt) | Asset groups, states, asset entries (`key`, `kind`, `uri`, `loaded`, `bytes`, `frames`) and `total_bytes` |
| `game_asset_reload` | `key` (asset name/URI/path, req unless `all: true`), `group` (opt), `all` (opt bool) | Reloads one asset (or all loaded assets) in place; surfaces parse/compile errors cleanly |
| `game_assets_reload_group` | `group` (req) | Reloads every loaded asset in a group and reports per-asset results and errors |

## Headless verification in `nsgotest`

Call registered MCP tools directly inside Go tests with `automation.Call`:

```go
var (
	raw json.RawMessage
	err error
)
app.Post(func() {
	raw, err = automation.Call(cx, t.Context(), mcp.ToolLoopStep, json.RawMessage(`{"count":60}`))
})
app.Settle()
```

Every tool above is always registered once the Bridge is attached. A tool whose subsystem is not configured in `mcp.Config` returns an error naming what is missing, such as `mcp: no game loop configured` or `mcp: no debug console configured`, so wire the matching `Config` field before calling it.

## The editor's own server

The tools above reach a running game. The NSGo Game Editor has its own MCP server, `game/editor/editormcp`, for editing the project it has open: an agent reads and edits through the same commands as the inspector, scene tree and viewport, so each edit is an undo step the person sees. Connect it with `claude mcp add game-editor -- nsgo game mcp` in the project directory, with the editor open on the project (`nsgo game editor .`). Its tools:

- `scene_read` `{file?, offset?, limit?}`: a scene, prefab, data or project file with line numbers; the open scene and `game.yaml` as the editor holds them while they have unsaved edits (line numbers then differ from disk), otherwise every file from disk, comments included.
- `scene_edit` `{file?, label?, edits}`: edits of the open scene (`add`, `remove`, `move`, `set`, `add_component`, `remove_component`, `set_property`, `remove_property`, `reparent`, `move_to_layer`), applied as one undo step, or none when the editor refuses one; the error names the file, the edit, the object, the field and the fix.
- `selection_get` `{}`: the open scene, the selected object with its YAML lines, the active layer, tool and view.
- `viewport_image` `{scale?}`: a PNG of the viewport.
- `game_check` `{}`: the Problems panel's problems, each with file, line and message.
- `game_playtest` `{file?, run?, count?, seeds?, out?, recordings?}`: validates `playtest.yaml`, runs headless bot playtests with per-seed `.replay.yaml` recordings, writes the YAML report and returns metric values, goal statuses and heat-map points.
- `playtest_define` `{metric, file?, rationale?, replace?}`: validates a proposed playtest metric and queues its canonical `playtest.yaml` diff as a pending review without modifying disk until accepted.

Resources `editor://scene` and `editor://selection` carry the change stream: subscribe for a notice on each change, whose `_meta` says whether the person or the agent made it.
