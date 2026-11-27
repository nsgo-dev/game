// Package mcp exposes a running NSGo game's state to coding agents over MCP through github.com/nsgo-dev/nsgo/automation.
//
// NSGo's built-in `nsgo mcp` server inspects native views, whereas a game draws into a single Canvas or GPU view. Attaching a [Bridge] to a [core.Scope] registers game-specific tools via [automation.Register] so `nsgo mcp` serves them automatically alongside the built-in tools:
//
//   - game_scenes_list: list active scenes on the scene stack and any registered named scenes, including the current scene and transition progress.
//   - game_layers_list: list layers of a scene in draw order with visibility, z-order, coordinate space, parallax, depth sort and item counts.
//   - game_entities_list: list live entities with stable generational references ("entity:<index>:<generation>") across steps, hierarchy links and component names.
//   - game_entity_inspect: inspect a single live entity by its stable reference, returning metadata, hierarchy links and all readable component values.
//   - game_component_get: read one component's value on a live entity by reference and component name.
//   - game_state_dump: capture a deterministic snapshot of entities, codec components, scene state, loop counters and StateValues as structured JSON and canonical statedump YAML.
//   - game_loop_status: report the game loop's paused state, effective time scale, fixed step rate, total steps, simulated game time and whether random seeding is configured.
//   - game_loop_pause: pause the game loop without accumulating a catch-up backlog.
//   - game_loop_resume: resume a paused game loop.
//   - game_loop_step: synchronously advance the game loop by N fixed updates (1..3600, default 1), advancing exact simulated game time.
//   - game_loop_time_scale: set the game loop's simulation time scale (finite, >= 0; 0 resets to the default 1x speed).
//   - game_loop_seed: set the deterministic random seed via Config.Random, Config.RandomStreams and/or Config.SetSeed.
//   - game_input_action: inject a press, release, tap, or analog axis value for a named action on [input.Actions].
//   - game_input_axis: inject or clear 1D or multi-axis analog values in [-1, 1] on [input.Actions].
//   - game_input_pointer: inject a pointer press, release, move, or click in game (or camera world) coordinates, mapped through [screen.Viewport] and hit-tested/dispatched on the active [scene.Scene].
//   - game_input_actions_list: list configured action and axis names with their current step state.
//   - game_tunables_list: list registered debug console tunables with their type, current value, default and numeric bounds.
//   - game_tunable_get: read one registered tunable's current value and metadata by name.
//   - game_tunable_set: update one registered tunable's value with type checking and range validation or optional clamping.
//   - game_console_commands_list: list built-in and registered debug console commands with their usage descriptions.
//   - game_console_exec: execute a debug console command line and return its structured status, output text, output lines and error.
//   - game_prefabs_list: list registered prefabs available to game_entity_spawn.
//   - game_entity_spawn: spawn a registered prefab or empty entity with optional parent, position and component overrides.
//   - game_entity_destroy: destroy a live entity by stable reference with an optional hierarchy child policy.
//   - game_component_set: set or patch a component's value on a live entity so the change takes effect on the next simulation step.
//   - game_component_remove: remove a component from a live entity.
//   - game_replay_status: report input recording and playback status, frame progress and saved recordings.
//   - game_replay_record_start: start recording per-step inputs into a named recording.
//   - game_replay_record_stop: stop the active recording, store it and return its deterministic YAML text and summary.
//   - game_replay_play: replay a stored or inline YAML recording deterministically, optionally resetting and running to completion.
//   - game_replay_stop: stop an active replay playback cleanly.
//   - game_replay_list: list stored recordings with frame counts, seeds, step rates and metadata.
//   - game_perf_readout: read live frame timing (FPS, frame/update/draw costs and percentiles), draw statistics, allocations per frame, heap/GC counters, asset memory, and optional frame history or profiler zones matching the debug overlay.
//   - game_perf_zones: list named profiler timing zones sorted by recent per-frame cost.
//   - game_perf_reset: reset accumulated debug overlay frame timings, pushed draw statistics, profiler zones and GC monitor history.
//   - game_assets_list: list loaded asset groups, asset keys/types, load states, and estimated memory usage in bytes.
//   - game_asset_reload: reload a named asset, URI, or path (or all loaded assets) in place and report structured reload diagnostics.
//   - game_assets_reload_group: reload every loaded asset in a named asset group in place.
//   - game_screenshot: capture the game view as PNG, optionally with aligned sprite bounds, physics collision shapes, reference grid, camera frame and labels drawn on top.
//
// Components registered on an [entities.World] with [entities.RegisterCodec] are readable and editable automatically. Transient or non-saved components can also be exposed with [RegisterComponent], [RegisterComponentFunc], [RegisterMutableComponent] or [RegisterComponentEditor], and built-in [entities.Transform2D], [entities.WorldTransform2D] and [sceneformat.ObjectRef] components are surfaced automatically when present and not already registered under another name. Prefabs can be registered via [Config.Prefabs], [Bridge.RegisterPrefab], [Bridge.RegisterEntityPrefab] or [Bridge.RegisterScenePrefab].
//
// Like [automation.Register], attachment is active in `nsgo dev -automation` and test builds (`nsgo test`) and is a no-op in ordinary and release builds. Call [Attach], [Bridge.Attach] and [Bridge.Detach] on the engine goroutine.
package mcp
