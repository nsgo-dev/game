# Agent Note: Local multiplayer input routing in game/input

Status: implemented

## Problem

`game/input` provides `Actions` with `SelectGamepad(id)`, which lets a single `Actions` listen to one controller ID, but building local multiplayer directly out of raw `Actions` instances has several gaps:

- Every player must wire its own `WatchGamepads`, `WatchKeys`, and `Attach` callbacks, polling `gamepad.Pads` once per player and duplicating event plumbing.
- `Actions` has no "no gamepad" mode: an unassigned or keyboard-only player whose bindings share a common keyboard-and-gamepad template reads every connected controller unless callers manually omit `WatchGamepads` and `OnGamepads` on that instance.
- Exclusive controller ownership across player slots (preventing two players from reading the same controller, releasing held buttons on reassignment, and querying unassigned controllers for lobbies or disconnect recovery) is left to every game to reimplement.

## Decision

Add `Players` and `Player` in `packages/game/input/players.go` as a thin coordinator over per-player `*Actions` instances rather than duplicating the binding, axis, or storage APIs.

### Ownership and routing contract

- Each slot (`0..Len()-1`) owns a dedicated `*Actions` (`Player.Actions()`) and independent device assignment flags: at most one exclusive gamepad ID (`AssignGamepad` / `UnassignGamepad`), optional keyboard routing (`AssignKeyboard` / `UnassignKeyboard`), and optional pointer/touch routing (`AssignPointer` / `UnassignPointer`).
- **Gamepad exclusivity**: assigning `padID` to player B unassigns `padID` from any previous owner A and pushes an empty pad slice to A so held buttons release cleanly on the next `Step()`. Unassigned players always receive an empty pad slice, so shared binding templates never leak controller input into keyboard-only players.
- **Shared keyboard and pointer**: multiple players may be assigned to the keyboard (disjoint key sets on one keyboard) or pointer (split-screen touch regions) simultaneously, and a single player may combine devices (such as keyboard + mouse, or keyboard + gamepad). Unassigning keyboard or pointer clears that device's held state on the player's `Actions` immediately so no input sticks.
- **Single-poll stepping**: `Players.WatchGamepads(cx)` polls `gamepad.Pads(cx)` once per `Players.Step()` via `core.Untracked`, sorts by ID, and dispatches per-player slices referencing `m.gpPads` (zero steady-state heap allocations). `Players.WatchKeys(cx)` installs one reactive `cx.Effect` and registers `cx.Cleanup` so disposing the scope releases watched keys across all keyboard players.
- **Slot stability**: slots are fixed-index (`NewPlayers(n)` and `Add()`); leaving a game clears devices via `Unassign(index)` rather than removing and shifting subsequent players' indices.
- **Disconnect readiness (#316)**: `AssignGamepad` remembers `padID` even when that controller is not currently present; `Player.GamepadConnected()` and `Player.Gamepad()` reflect live presence in the latest snapshot, and `UnassignedGamepads()` exposes unclaimed controllers so future disconnect/reconnect and reassignment workflows layer directly onto `Players` without breaking signatures.

## Alternatives considered

### Putting a player-index map inside `Actions` directly

Adding a player parameter to `Actions.Pressed(player, name)` and `Actions.Bind(player, ...)` would keep a single top-level type for single-player and multiplayer games. It was rejected because it would bloat `Actions` (which is already the primary single-player type used by `project.File.BindInput`, `replay`, and existing examples), force callers either to pass player `0` everywhere or duplicate every method, and complicate per-player binding persistence (`Actions.Save` / `Actions.Load`). Wrapping one `*Actions` per `Player` reuses every existing binding, axis, virtual pad, pointer-lock, and storage feature with zero duplication.

### Mutually exclusive device enum per player (`DeviceNone | DeviceKeyboard | DeviceGamepad`)

Representing a player's device as a single enum value would prevent a player from accidentally staying bound to keyboard after switching to a gamepad. It was rejected because local multiplayer games routinely combine keyboard + mouse for one player while another uses a gamepad, and single-player / disconnect fallback modes (#316) need keyboard or touch active alongside a remembered gamepad ID. Independent `AssignGamepad`, `AssignKeyboard`, and `AssignPointer` toggles plus `Unassign(index)` cover both exclusive and hybrid setups cleanly.

### Supporting index-shifting `Remove(index)` for player leave

Allowing callers to delete a slot from the middle of `Players` would keep `Len()` equal to the number of active players. It was rejected because shifting player 2's index from `1` to `0` when player 1 leaves causes subtle bugs in HUD colors, scores, and entity-to-slot references. Fixed slot indices with `Unassign(index)` and `NextUnassigned()` keep player identities stable.

## Consequences

- **Benefits**: Local multiplayer games wire `WatchGamepads`, `WatchKeys`, `Attach`, `Step`, and `Reset` once on `Players`; two gamepads, split keyboards, mixed keyboard + gamepad, and split-screen touch all work with the same per-player `Actions` API and per-player `Save`/`Load`; steady-state `Players.Step()` performs zero allocations per step.
- **Costs**: Callers must remember to call `WatchGamepads` / `WatchKeys` / `Attach` / `Step` on `Players` rather than on individual `p.Actions()` instances, and switching a player from keyboard to gamepad-only requires calling both `UnassignKeyboard(i)` (or `Unassign(i)`) and `AssignGamepad(i, id)`.
- **Known limit**: `Players` does not yet emit disconnect/reconnect events, auto-pause on controller loss, or perform automatic press-any-button reassignment; those are scoped to issue #316 and build directly on `GamepadID()`, `GamepadConnected()`, and `UnassignedGamepads()`.

## Verification

- `packages/game/input/players_test.go` verifies under `nsgotest`: two gamepads driving two players' buttons, triggers, and sticks separately (including disconnect and reconnect), split keyboard key sets via both `WatchKeys` and focused `Attach`, mixed keyboard + gamepad isolation with a shared binding template, gamepad exclusivity on reassignment, `AssignUnassignedGamepads` and drop-in join, split touch / pointer routing, `Reset` muting, scope disposal, `AccessGamepad` denial, nil/zero safety, per-player `Save`/`Load`, and `BenchmarkPlayersStepGamepads` (`0 allocs/op`).
- Verified via `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./input/...`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
