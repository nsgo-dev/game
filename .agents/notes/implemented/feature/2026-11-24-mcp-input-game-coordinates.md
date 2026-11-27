# Agent Note: MCP input injection at game coordinates

Status: implemented

## Problem

With [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md) in place, agents can read live scenes, layers, and entities, but cannot drive gameplay through MCP. Native NSGo pointer automation targets raw window coordinates, forcing agents to reverse-engineer `screen.Viewport` letterbox bars, scaling factors, and `tilemap.Camera` offsets just to click a sprite or trigger an input action.

## Decision

Extend `packages/game/mcp` with a dedicated input tool group (`packages/game/mcp/input.go`) and add minimal, pure additive programmatic injection and introspection methods on `packages/game/input.Actions`.

## Input and coordinate contract

- **Additive `game/input` primitives**: `Actions` gains `Press`, `Release`, `SetAxis`, `ClearAxis`, `ActionNames`, and `AxisNames` with zero experimental imports (preserving `game/input`'s Stable status). `Press` holds an action across subsequent `Step()` calls until `Release` or `Reset` and latches a rising edge on transition so a quick `Press`+`Release` between steps registers for one step, matching physical input semantics. `SetAxis` stores injected analog contributions in `[-1, 1]` that `Step()` snapshots into `Axis()` alongside existing bindings and `Reset()` clears.
- **MCP input tools**: Four `snake_case` tools append to `Bridge.Tools()` via `b.inputTools()` with strict `DisallowUnknownFields()` decoding:
  - `game_input_action`: injects `press`, `release`, `tap` (default when `value` is omitted), or `axis` (default when `value` is provided; requires `value` in `[-1, 1]`) on `input.Actions`.
  - `game_input_axis`: sets or clears 1D (`name` + `value`) or multi-axis (`axes` map) analog values in `[-1, 1]`.
  - `game_input_pointer`: accepts `(x, y)` in `"game"` space (default, virtual canvas points) or `"world"` space (converted via the active scene's `Camera.WorldToView`), maps game coordinates through `screen.Viewport.GameToView` using `ViewSizeFunc` (falling back to the attached scope's `Env().Window()` and then the viewport's virtual size), and computes `in_viewport` via `Viewport.ViewToGame`.
  - `game_input_actions_list`: lists configured action and axis names with their current step state (`configured: false` with empty lists when `Actions` is unset).
- **Dual-space pointer delivery**: `game_input_pointer` forwards mapped view coordinates to `input.Actions` (`OnPressDown`, `OnPressUp`, `OnMouseMove`), matching how `core.Canvas` delivers view coordinates to `Actions.MapPoint = viewport.ViewToGame`. When `in_viewport` is true, it tests `Scene.HitTest(gamePt)` and invokes `Scene.DispatchPress` on primary press/click in virtual game coordinates (matching how `Scene.Draw` runs inside `Viewport.Draw`), then invokes `Config.OnPointer` with `PointerEvent` (including `GamePoint`, `ViewPoint`, `WorldPoint`, `InViewport`, `Hit`, and `Dispatched`).

## Alternatives considered

### Injecting input solely through synthetic NSGo view events

Routing all MCP input through `core.View` keyboard/mouse events would avoid adding `Press`/`Release`/`SetAxis` to `input.Actions`. It was rejected because games bind actions to varied physical inputs (keys, touch regions, virtual pads, gamepads) or unmapped action names; requiring agents to discover and synthesize physical bindings couples automation to control layouts instead of named game actions, and cannot inject arbitrary analog axis values cleanly.

### Running `Scene.HitTest` and `Scene.DispatchPress` in view coordinates instead of game coordinates

Passing `viewPt` directly to `Scene.HitTest` would match games that draw a `Scene` directly without `screen.Viewport`. It was rejected because games using `screen.Viewport.Draw` render `Scene` inside the viewport's translated/scaled virtual coordinate space (`Camera.Width/Height == virtual size`), so passing raw view coordinates would miss every sprite whenever letterboxing or scaling is active. Using `gamePt` for `Scene` (which equals `viewPt` when no `Viewport` is configured) works accurately in both viewport-scaled and direct-canvas games.

### Suppressing `Scene.DispatchPress` whenever `Config.OnPointer` is set

Skipping automatic `Scene.DispatchPress` when `Config.OnPointer` is non-nil would prevent callers who manually call `DispatchPress` inside `OnPointer` from dispatching twice. It was rejected because attaching `OnPointer` alongside a `Scene` to observe pointer events or handle unhandled clicks is a common pattern; exposing `PointerEvent.Dispatched` lets `OnPointer` inspect whether a scene target already handled the press while keeping zero-boilerplate sprite hit dispatch active by default.

## Consequences

- **Benefits**: Agents can press sprites directly by game or world position regardless of window size or letterbox bars, verify hits immediately from structured response metadata (`hit_layer`, `hit_frame`, `hit_sprite_index`, `dispatched`), and drive digital actions and analog axes deterministically across simulation steps. Minimal additive edits in `mcp.go` keep parallel MCP PRs conflict-free.
- **Costs**: `game/input.Actions` carries six additional exported methods and three small maps for injected state that must stay consistent with `Step()` and `Reset()`. Games drawing into a sub-rect smaller than the window must provide `ViewSizeFunc` so `Viewport.GameToView` matches the canvas bounds rather than `Env().Window()`.

## Verification

Verified with `go tool nsgo test ./mcp ./input` and `go tool nsgo test -run TestStabilityTable .`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
