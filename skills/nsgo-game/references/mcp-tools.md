# `game/pkg/mcp` Tool Reference

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

	// Screenshots & diagnostic overlays (#465, #1744)
	Physics:              g.physics,      // or PhysicsFunc
	RigidPhysics:         g.rigidPhysics, // or RigidPhysicsFunc
	DrawFrame:            g.drawFrame,
	CaptureFrame:         g.captureFrame,
	ScreenshotDir:        g.screenshotDir,
	ProjectDir:           g.projectDir, // optional confined root for compare_to PNGs
	ScreenshotBackground: core.RGB(0x0f172a),

	// State values & bounded slog capture (#1744)
	StateValues: func() map[string]any { return map[string]any{"score": g.score} },
	Log:         &mcp.LogConfig{}, // installs tee slog.Handler on slog.Default() while attached

	// Debug tunables & console (#466)
	Console: g.console, // or ConsoleFunc: func() *gamedebug.Console

	// Input recording & replay (#467)
	Replay: g.replayCtrl, // *mcp.ReplayController (implements replay.ActionSource)

	// Performance telemetry (#468)
	Overlay:         g.overlay,  // *gamedebug.Overlay
	Profiler:        g.profiler, // *gamedebug.Profiler
	GC:              g.gcMon,    // *gamedebug.GCMonitor
	Memory:          g.memTrack, // *gamedebug.MemoryTracker
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
| `game_state_dump` | `{}` | Deterministic state dump (`version`, `rate`, `steps`, `game_time_seconds`, `scene`, `entities`, `values` from `Config.StateValues`, and canonical `statedump` `yaml`) |
| `game_state_capture` | `name` (req), `values` (opt key list), `include_yaml` (opt bool, default `false`) | Runs `statedump.Capture` and stores canonical YAML in memory under `name` (bounded count/bytes with FIFO eviction); returns capture summary |
| `game_state_diff` | `from` (req), `to` (opt; defaults to live state), `paths` (opt dot-path prefixes) | Returns `statedump.Diff` unified YAML diff, `equal`, `steps_delta`, `scene_changed`, `changed_entities`/`added_entities`/`removed_entities`/`modified_entities`, per-entity `entity_changes`, and `value_changes` |

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
| `game_input_action` | `name` (req), `mode` (`"press"` \| `"release"` \| `"tap"` \| `"axis"`, default `"tap"` without `value` or `"axis"` with `value`), `value` (opt, `-1..1`; req when `mode` is `"axis"`) | Injects action hold/release/single-step tap or analog axis value into `input.Actions`; returns current step state (`pressed`, `just_pressed`, `just_released`, `axis`) |
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
| `game_screenshot` | `scene` (opt), `width`/`height` (opt pair, `1..8192`), `format` (`"png"`), `path` (opt relative path inside `ScreenshotDir`), `baseline` (opt name), `compare_to` (opt baseline name or relative `.png`), `tolerance` (`0..255`), `max_mismatched_pixels`, `max_mismatched_percent`, `diff_path`, `include_diff_image`, top-level booleans or `overlays` object (`bounds`, `collision_shapes` / `physics`, `grid`, `grid_step`, `camera`, `labels`) | Captures the game view as PNG (`png_base64` or written `path`), with `width`, `height`, `bytes`, `overlays_drawn`, optional saved `baseline`, and optional `comparison` (`changed_pixels`, `exact_changed_pixels`, `changed_percent`, `gametest_match`, `within_tolerance`, `changed_bounds`, `changed_regions`, and optional diff PNG) |

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
| `game_replay_record_start` | `name` (opt), `seed` (opt), `actions` (opt), `axes` (opt), `metadata` (opt), `reset` (opt bool) | Begins recording per-step inputs via `ReplayController` |
| `game_replay_record_stop` | `name` (opt), `save_path` (opt relative path under `ReplayConfig.SaveDir`), `overwrite` (opt bool, default `false`; required to replace an existing recording or save file) | Stops recording, stores it and returns deterministic YAML (`yaml`), frame/event summary and `saved_path` |
| `game_replay_play` | `name` OR `yaml` (one required), `run_to_end` (opt bool), `steps` (opt int, exclusive with `run_to_end`), `reset` (opt bool, default `true` when `ResetFunc` or a seed target is configured) | Replays a stored or inline YAML recording, optionally advancing frames synchronously |
| `game_replay_stop` | `{}` | Stops active replay playback |
| `game_replay_list` | `{}` | Lists stored recordings with `frames`, `seed`, `step_rate`, `events`, `actions`, `axes`, `metadata` |

### 8. Performance telemetry

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_perf_readout` | `include_history` (opt), `history_limit` (opt), `include_zones` (opt), `zones_limit` (opt) | `fps`, `frame_ms`/`update_ms`/`draw_ms`, percentiles (`p50`/`p95`/`p99`), `draw_calls`/`sprites`/`batches`/`triangles`, `allocs_per_frame`, heap/GC stats, `render3d`, `asset_memory`, optional `history` and `zones` |
| `game_perf_zones` | `limit` (opt) | Named `gamedebug.Profiler` zones sorted by recent cost (`last_ms`, `avg_ms`, `max_ms`, `calls`) |
| `game_perf_reset` | `{}` | Resets overlay frame stats, draw stats, profiler zones and GC monitor history |

### 9. Assets and live hot reload

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_assets_list` | `group` (opt), `state` (`"loaded"` \| `"loading"` \| `"unloaded"` \| `"failed"`), `kind` (opt) | Asset groups, states, asset entries (`key`, `kind`, `uri`, `loaded`, `bytes`, `frames`) and `total_bytes` |
| `game_asset_reload` | `key` (asset name/URI/path, req unless `all: true`), `group` (opt), `all` (opt bool) | Reloads one asset (or all loaded assets) in place; surfaces parse/compile errors cleanly |
| `game_assets_reload_group` | `group` (req) | Reloads every loaded asset in a group and reports per-asset results and errors |

### 10. Logs and recovered panics

| Tool | Arguments | Returns |
| --- | --- | --- |
| `game_log_read` | `since` (opt cursor), `level` (opt min level), `contains` (opt substring), `limit` (opt `1..1000`, default `100`) | Bounded ring-buffer `slog` entries (`seq`, `time`, `level`, `message`, `attrs`, `source`), `next_cursor`, `dropped`, `missed`, `has_more`/`truncated`, and `last_panic` recorded by `pkg/tooling/gamedebug` |

## Recipe: check your change

After editing gameplay code, scenes, or tunables, verify both intended and unintended side effects in five calls:

1. **Capture baseline:** call `game_loop_pause`, `game_state_capture` (`{"name": "before"}`), `game_screenshot` (`{"baseline": "before"}`), and note `next_cursor` from `game_log_read` (`{}`).
2. **Act:** inject input (`game_input_action`, `game_input_axis`, `game_input_pointer`) or mutate a tunable/component.
3. **Step:** advance an exact step count with `game_loop_step` (`{"count": 10}`).
4. **Capture after:** call `game_state_capture` (`{"name": "after"}`).
5. **Diff & inspect logs:**
   - Call `game_state_diff` (`{"from": "before", "to": "after"}`, or with `"paths": ["entities"]`) to verify only the expected entities and fields changed.
   - Call `game_screenshot` (`{"compare_to": "before", "diff_path": "diff.png"}`) to check `changed_pixels`, `changed_regions`, `gametest_match`, and `within_tolerance`.
   - Call `game_log_read` (`{"since": <cursor>, "level": "warn"}`) to catch any new warnings, errors, or `last_panic`.

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

The tools above reach a running game. The NSGo Game Editor has its own MCP server, `game/pkg/editor/editormcp`, for editing the project it has open: an agent reads and edits through the same commands as the inspector, scene tree and viewport, so each edit is reviewable by the person. Connect it with `go tool game mcp install` (or `claude mcp add game-editor -- go tool game mcp`) in the project directory, with the editor open on the project (`go tool game editor .`). The [complete editor tool reference](https://game.nsgo.dev/docs/mcp/#editor-tools) catalogs the tool groups:

- Project, scene and file reads and edits (`scene_read`, `scene_edit`, `file_edit`), object and inspector operations, selection, viewport images, references and safe renames.
- Reviewable agent turns (`turn_start`, `turn_end`, `turn_status`), change reviews and notes, unified history and checkpoints.
- Project checks, playtests and Play-mode replay, Go code review, tile and tileset editing, data tables, animation state machines and cutscenes.
- Background tasks, plans, permissions, merges, releases and project context.

Resources `editor://scene`, `editor://selection`, `editor://agent/turn`, `editor://brief`, `editor://rules` and `editor://context` carry the change stream: subscribe for a notice on each change, whose `_meta` says whether the person or the agent made it.
