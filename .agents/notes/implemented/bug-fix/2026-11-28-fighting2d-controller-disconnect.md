# Agent Note: Keep fighting2d controller assignments sticky and pause versus on disconnect

Status: implemented

## Problem

`template/fighting2d/template/controls.go` originally owned two bare `*input.Actions` and reassigned game controllers on every fixed step by sorted ID position (`pads[0]` to player 1, `pads[1]` to player 2). In a local versus match with two controllers, disconnecting player 1's controller dropped it from the sorted list so player 2's controller shifted into `pads[0]` and immediately took over player 1 while the match kept running with no pause or prompt (`nsgo-dev/game#1567`). Connecting a controller with a lower ID mid-match likewise shifted existing fighters onto different controllers.

This supersedes the original input alternative in [the fighting2d template note](../feature/2026-11-25-template-fighting2d.md), which avoided `input.Players` under the assumption that it assigned the keyboard to only one player. `input.Players` supports `AssignKeyboard` on multiple player slots sharing disjoint key sets alongside exclusive per-player gamepad routing and `DisconnectPolicy` ([mid-game controller disconnect, reconnect, and fallback](../feature/2026-11-24-input-controller-disconnect-reconnect.md)).

## Decision

Move `template/fighting2d/template/controls.go` onto `input.NewPlayers(2)` and manage versus disconnects through `input.DisconnectPolicy`:

- **Shared keyboard + exclusive controllers**: Both slots bind `game.yaml`'s full input map and call `AssignKeyboard(0)` / `AssignKeyboard(1)`, while slot 0 also calls `AssignPointer(0)` and binds the on-screen `VirtualPad`. `Players` routes each assigned controller exclusively to its slot and preserves assignments across `Players.Reset()` on screen changes.
- **Sticky slot assignment**: Open slots claim unassigned connected controllers in ascending ID order only on the title screen and at `startMatch` (`assignFreePads(true)`), which also drops stale disconnected assignments before a new match. During a match (`screenFight`, `screenPaused`, `screenReconnect`), `step(false)` never reassigns controllers by position, so a newly connected controller (even with a lower ID) never steals an already-assigned fighter's slot.
- **Versus disconnect pause and reconnect panel**: Starting a versus match enables `DisconnectPolicy{PauseOnDisconnect: true, AllowReassignOnPress: true}` with callbacks wired to `fightingGame`. Disconnecting an assigned controller during `screenFight` releases held inputs immediately, pauses audio, and switches to `screenReconnect`, displaying `"Player N: reconnect your controller"` (or `"Players 1 & 2: reconnect your controllers"` when both drop). The fight resumes when the original controller reconnects, when a free controller newly presses any button (muted across `Reset()` so pressing Start to claim does not open the pause menu), or when the disconnected player continues on keyboard via the `"Continue on keyboard"` button or their keyboard Start key (`Enter` / `Right Shift`), which calls `UnassignGamepad`.
- **Manual pause preservation**: If a controller disconnects while already on `screenPaused`, the game stays on `screenPaused` and does not auto-unpause on reconnect; clicking Resume while a controller is still disconnected transitions to `screenReconnect`.

## Alternatives considered

- **Keep two bare `input.Actions` and reimplement sticky IDs, disconnect tracking, and press-to-reassign in `controls.go`**: avoids switching `controls` to `input.Players`, and was weighed in `#1567`. Rejected because it duplicates `pkg/input/disconnect.go`'s rising-edge button tracking (`AllowReassignOnPress`), held-input release ordering, and `Players.Reset()` preservation, and leaves the fighting template out of sync with the engine's canonical local-multiplayer API (already shipped in the pinned `github.com/nsgo-dev/game v0.1.0`).
- **Pause `game.Loop` directly (`OnPause: g.loop.SetPaused`) on disconnect**: matches the minimal `docs/en/input.md` snippet, where `Players`'s `waitRAFFrame` still polls controllers while paused. Rejected here because `fighting2d` already drives pause menus (`screenPaused`) with `g.loop` running so keyboard bindings (`Enter` / `Right Shift`) can operate menu actions while `fightStep()` is skipped; keeping `g.loop` running on `screenReconnect` lets disconnected players press their keyboard Start key to continue on keyboard without adding separate `core.View` key listeners.

## Consequences

- **Benefits**: Disconnecting player 1's controller in versus immediately releases held inputs, leaves player 2's controller on player 2, pauses gameplay with an actionable reconnect panel, and restores player 1 on reconnect, free-pad press, or keyboard continuation; connecting a lower-ID controller mid-match never swaps fighters; `controls.go` drops manual `gamepad.Pads` sorting and works cleanly against the pinned `v0.1.0` module without changing `pkg/input`.
- **Costs**: Against the CPU (`modeCPU`), player 1 now reads its assigned controller slot (or keyboard/touch) rather than every connected controller simultaneously, matching the title-screen assignment rules; single-player disconnect prompts remain tracked separately in `#1568`.
