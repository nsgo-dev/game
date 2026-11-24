# Agent Note: Mid-game controller disconnect, reconnect, and fallback in game/input

Status: implemented

## Problem

[`Players`](./2026-11-24-input-local-multiplayer.md) routes game controllers exclusively to player slots and releases held inputs when a controller disappears, but games still have no built-in way to handle mid-game controller loss:

- Games must diff `Player.GamepadConnected()` every step to detect disconnects, distinguish "never connected yet" from "disconnected mid-game", and manage pause/resume across multiple simultaneous disconnects.
- When a game pauses `game.Loop` after a controller disconnects, `game.Loop.Update` stops running, so calling `Players.Step()` inside `Update` can never observe the controller reconnecting or another controller pressing a button to take over.
- Single-player games that want seamless keyboard or touch fallback on controller loss have to temporarily mutate `AssignKeyboard`/`AssignPointer`, remember prior assignments, and clear held fallback inputs when a controller returns.

## Decision

Add disconnect and reconnect policy handling directly onto `Players` and `Player` in `packages/game/input/disconnect.go` (integrated with `packages/game/input/players.go`), keeping `game/input` decoupled from `game` by using an `OnPause func(paused bool)` callback (matching [`game.Loop.OnPause`](./2026-11-24-sound-pause-with-game.md) and `game.Loop.SetPaused`):

- **Policy and events**: `Players.SetDisconnectPolicy(DisconnectPolicy)` configures `PauseOnDisconnect`, `AllowReassignOnPress`, `FallbackToKeyboardTouch`, `OnPause`, `OnDisconnect`, and `OnReconnect`. `DisconnectEvent` and `ReconnectEvent` report `PlayerIndex`, `Player`, `PreviousGamepadID`, `NewGamepadID`, `Reassigned`, `FallbackActive`, and `ShouldPause`.
- **State queries**: `Player.GamepadDisconnected()`, `Player.FallbackActive()`, `Players.DisconnectedPlayers()`, `Players.PausedByDisconnect()`, and `Players.PollGamepads()` expose disconnect state and explicit polling.

### State transitions and ordering invariants

1. **Initial pre-assignment vs. mid-game disconnect**: Each `Player` tracks `gpWasConnected` separately from `hasGamepad`. A pre-assigned controller that has never appeared in a snapshot **never** emits `OnDisconnect` or pauses gameplay; only a `gpWasConnected -> absent` transition marks `gpDisconnected = true` and queues `DisconnectEvent`. Explicit watch teardown (`WatchGamepads(nil)` or scope disposal) clears held inputs without emitting a mid-game disconnect event.
2. **Three-pass reconciliation order** (`reconcileGamepads`):
   - Pass 1 detects newly absent assigned controllers, marks `gpDisconnected = true`, activates fallback if `FallbackToKeyboardTouch` is enabled, and queues `DisconnectEvent`.
   - Pass 2 detects initial connections and same-ID reconnects, clearing `gpDisconnected`, deactivating fallback if active, and queueing `ReconnectEvent` (`Reassigned: false`).
   - Pass 3 (when `AllowReassignOnPress` is enabled and any slot remains disconnected) checks unassigned connected controllers in ascending ID order for a rising button edge (`nowMask &^ prevMask != 0`) using a reused sorted `[]padButtonState` slice (zero steady-state allocations). A controller with a button already held before the disconnect **must not** steal a slot until released and pressed again. Claiming assigns the controller to the lowest-index disconnected player, deactivates fallback, and queues `ReconnectEvent` (`Reassigned: true`).
3. **Held-input release before callbacks**: In `Players.Step()`, `reconcileGamepads` dispatches empty pad slices to disconnected players and runs every player's `Actions.Step()` **before** `flushDisconnectEvents()` invokes `OnDisconnect`, `OnReconnect`, and `OnPause`. Consequently, held buttons emit `JustReleased == true`, stick axes read `0`, and no player keeps moving on the disconnect tick.
4. **Pause-aware display-frame monitoring**: While `WatchGamepads` has a live scope, at least one player has `gpDisconnected == true`, and disconnect handling is active, `Players` schedules a lightweight `cx.RequestAnimationFrame` callback (`waitRAFFrame`) owned by `gpScope` that calls `PollGamepads()`. Once all disconnected players recover (or `gpScope` is disposed / `WatchGamepads(nil)` is called), the frame callback is cancelled immediately so steady-state gameplay incurs zero extra RAF callbacks and zero heap allocations per step.
5. **Fallback restoration**: `activateFallback` snapshots `preFallbackKeyboard` and `preFallbackPointer` before enabling keyboard (syncing `watchedKeys`/`watchedCodes` when `WatchKeys` is active) and pointer routing. `deactivateFallback` restores only the flags that were temporarily added and clears their held inputs (`clearKeyboardInput`/`clearPointerInput`) so fallback touches or keys never stick when controller play resumes. Explicit `AssignKeyboard`/`UnassignKeyboard`/`AssignPointer`/`UnassignPointer` calls during fallback update the saved pre-fallback flags.

## Alternatives considered

### Importing `game.Loop` directly into `game/input` (`PauseLoop(*game.Loop)`)

Strongest argument: accepting `*game.Loop` directly would let `Players` inspect `loop.Paused` and call `loop.SetPaused` without a callback field.

Rejected for the same reason [`game/sound`](./2026-11-24-sound-pause-with-game.md) chose a callback: `game/input` is a `Stable` package with a minimal dependency footprint (`game/loaderr` only within this module). Using `OnPause func(paused bool)` accepts `loop.SetPaused` directly as a method value, keeps `game/input` independent of `game`, and works identically with custom pause managers, pause menus, or UI state signals.

### Requiring callers to invoke `Players.PollGamepads()` manually from `Loop.Draw` or `Loop.FrameTime`

Strongest argument: avoiding an internal `RequestAnimationFrame` monitor while paused would keep `Players` purely pull-driven.

Rejected because `game.Loop` stops invoking `Update` whenever `Paused` is `true`, and games following the documented pattern (`players.Step()` in `Loop.Update`, `OnPause: loop.SetPaused`) would deadlock on disconnect unless every caller remembered to wire a second polling call in `Draw` or `FrameTime`. Scheduling `waitRAFFrame` only while a player is disconnected and `WatchGamepads` has a live scope makes `PauseOnDisconnect` + `OnPause: loop.SetPaused` work out of the box with zero overhead during normal play, while `PollGamepads()` remains available for custom loops.

### Tracking disconnect policy on `Actions` instead of `Players`

Strongest argument: single-player games that use `Actions` directly would not need `NewPlayers(1)` to get disconnect events.

Rejected because `Actions` reads all controllers by default (`SelectAnyGamepad`) and has no concept of device-exclusive routing (it cannot temporarily disable keyboard/touch while a controller is active and re-enable them on fallback without breaking existing `Actions` semantics where all bound inputs are simultaneously active). `NewPlayers(1)` already models single-player device ownership cleanly and reuses the exact same `Actions` bindings and persistence.

## Consequences

- **Benefits**: Single-player and local multiplayer games get mid-game disconnect detection, automatic `game.Loop` pause/resume, same-pad reconnect, press-any-button reassignment, and non-pausing keyboard/touch fallback with one `SetDisconnectPolicy` call; steady-state `BenchmarkPlayersStepGamepads` remains `0 allocs/op`; `game/input` adds zero new package imports.
- **Costs**: Single-player games wanting managed disconnect/fallback behavior use `input.NewPlayers(1)` instead of bare `input.NewActions()`; press-to-reassign uses the button press's natural state on the next step unless the caller invokes `Players.Reset()` inside `OnReconnect` to mute held buttons until released.

## Verification

- `packages/game/input/disconnect_test.go` verifies under `nsgotest` and deterministic `OnGamepads` stepping:
  - Disconnecting player 1 emits `DisconnectEvent`, clears held buttons/axes immediately, pauses `game.Loop`, blocks `Update` while paused, and reconnecting the same pad restores bindings and resumes.
  - Pressing a button on another unassigned pad claims player 1's slot (while already-held buttons do not steal before release+press), restores bindings, emits `ReconnectEvent{Reassigned: true}`, and resumes; `AllowReassignOnPress: false` ignores other pads.
  - `FallbackToKeyboardTouch: true` continues without pausing, routes keyboard (`WatchKeys`) and touch immediately, clears temporary fallback inputs on recovery, and preserves pre-existing keyboard/pointer assignments.
  - Multi-player simultaneous disconnects pause once, keep pausing through partial recovery (`ReconnectEvent.ShouldPause == true`), and resume only once every disconnected player recovers.
  - Manual `AssignGamepad`, `UnassignGamepad`, `Unassign`, mid-disconnect `SetDisconnectPolicy` toggles, `WatchGamepads` scope disposal (no false disconnect), and nil/zero safety.
- Checked with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./input`, `go tool nsgo test -run TestStabilityTable .`, `go tool nsgo test -bench=BenchmarkPlayersStepGamepads ./input` (`0 allocs/op`), `go run ./tools/changelog check`, and `bun run verify-notes`.
