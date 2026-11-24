# mcp

`game/mcp` exposes a running game's scenes, layers, entities, components and fixed-step loop controls to coding agents over MCP through NSGo's `github.com/nsgo-dev/nsgo/automation` package.

NSGo's built-in `nsgo mcp` server sees native views only; a game draws into one `Canvas` or GPU view. Attaching an `mcp.Bridge` to a `core.Scope` registers game-specific tools on the engine's reactive graph so `nsgo mcp` serves them alongside the built-in tools whenever the app runs under `go tool nsgo dev -automation` or `go tool nsgo test`. In ordinary and release builds, `automation.Register` is a no-op and adds no runtime protocol.

```go
import "github.com/nsgo-dev/game/packages/game/mcp"
```

## Attaching a bridge

Call `mcp.Attach` (or `mcp.New` followed by `bridge.Attach`) on the engine goroutine with the subsystems you want agents to inspect. Every field in `mcp.Config` is optional; tools remain registered until `cx` is disposed or `bridge.Detach()` is called.

```go
bridge, err := mcp.Attach(cx, mcp.Config{
	Scenes:        g.scenes,
	SceneName:     func(c *scene.Controller) string { return g.sceneNames[c] },
	SceneFunc:     func() *scene.Scene { return g.activeScene },
	NamedScenes:   map[string]*scene.Scene{"title": g.titleScene},
	WorldFunc:     func() *entities.World { return g.world },
	EntityName:    func(e entities.Entity) string { return g.names[e] },
	EntityTags:    func(e entities.Entity) []string { return g.tags[e] },
	EntityLayer:   func(e entities.Entity) string { return g.layers[e] },
	Loop:          g.loop,
	RandomStreams: g.rng,
})
if err != nil {
	return err
}
```

- When a game replaces its `*scene.Scene`, `*entities.World` or `*game.Loop` across level restarts, supply `SceneFunc` / `WorldFunc` / `LoopFunc` (or call `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop`).
- To support `game_loop_seed`, configure `Random` (`*random.Stream`), `RandomStreams` (`*random.Streams`) and/or a custom `SetSeed` callback; if none is set, `game_loop_seed` returns an actionable error.
- When a `scene.Manager` controller draws via a custom `Controller.Draw` without setting `Controller.Scene`, `SceneFunc` / `Scene` provides the current scene's layers automatically.
- When `EntityName` or `EntityLayer` is omitted (or returns `""`), `game/mcp` falls back to [`sceneformat.ObjectRef`](editor.md) if the entity carries one.

## Inspecting components

Components registered on an [`entities.World`](entities.md) with `entities.RegisterCodec` are readable automatically. In addition:

- Built-in `entities.Transform2D`, `entities.WorldTransform2D` and `sceneformat.ObjectRef` components are surfaced automatically as `"transform2d"`, `"world_transform2d"` and `"object_ref"` unless that type or name is already registered.
- Transient or non-saved runtime components can be exposed with `mcp.RegisterComponent[T](bridge, name)` or a custom reader via `mcp.RegisterComponentFunc(bridge, name, read)`.
- Component values are serialized with `encoding/json`; if a runtime value contains non-finite floats (`NaN`, `+Inf`, `-Inf`), callback fields, channels or cyclic pointers, `game/mcp` falls back to a safe sanitizer (`"NaN"`, `"+Inf"`, `"-Inf"`, `"<func>"`, `"<chan>"`, `"<cycle>"`) so an agent can still diagnose broken runtime state.

```go
if err := mcp.RegisterComponent[AIState](bridge, "ai"); err != nil {
	return err
}
```

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

All tools reject unknown argument fields.

## Deterministic loop control

Agents can drive reproducible test scenarios by pausing the loop, setting a deterministic seed, stepping an exact number of fixed updates, and inspecting entity state immediately afterward:

- `game_loop_step` runs `Update` synchronously on the engine goroutine and calls `Draw` once afterward, so subsequent `game_entity_inspect` or `game_component_get` calls observe the updated state immediately without waiting for the next display frame.
- Simulated time is accumulated per fixed-rate segment (`steps / rate`), so stepping 60 updates at 60 Hz advances `game_time_seconds` and `delta_seconds` by exactly `1.0` with no integer-nanosecond drift.
- `game_loop_step` caps each call at `MaxLoopSteps` (`3600`, or one minute at 60 Hz) to guard against accidental hangs.

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
