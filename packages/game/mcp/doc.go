// Package mcp exposes a running NSGo game's state to coding agents over MCP through github.com/nsgo-dev/nsgo/automation.
//
// NSGo's built-in `nsgo mcp` server inspects native views, whereas a game draws into a single Canvas or GPU view. Attaching a [Bridge] to a [core.Scope] registers game-specific tools via [automation.Register] so `nsgo mcp` serves them automatically alongside the built-in tools:
//
//   - game_scenes_list: list active scenes on the scene stack and any registered named scenes, including the current scene and transition progress.
//   - game_layers_list: list layers of a scene in draw order with visibility, z-order, coordinate space, parallax, depth sort and item counts.
//   - game_entities_list: list live entities with stable generational references ("entity:<index>:<generation>") across steps, hierarchy links and component names.
//   - game_entity_inspect: inspect a single live entity by its stable reference, returning metadata, hierarchy links and all readable component values.
//   - game_component_get: read one component's value on a live entity by reference and component name.
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
//
// Components registered on an [entities.World] with [entities.RegisterCodec] are readable automatically. Transient or non-saved components can also be exposed with [RegisterComponent] or [RegisterComponentFunc], and built-in [entities.Transform2D], [entities.WorldTransform2D] and [sceneformat.ObjectRef] components are surfaced automatically when present and not already registered under another name.
//
// Like [automation.Register], attachment is active in `nsgo dev -automation` and test builds (`nsgo test`) and is a no-op in ordinary and release builds. Call [Attach], [Bridge.Attach] and [Bridge.Detach] on the engine goroutine.
package mcp
