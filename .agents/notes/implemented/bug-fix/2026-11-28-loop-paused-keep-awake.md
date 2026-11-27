# Agent Note: Release Loop display keep-awake hold while paused or OnDemand-idle

Status: implemented

## Problem

`game.Loop.Run` documented that it keeps the display awake (`core.Scope.KeepAwake`) while running in the foreground and releases the hold when paused, stopped, or disposed. In practice, `loopRun` acquired the hold in `resume()` and only released it in `pause()` (window background or `stop()`); setting `Loop.Paused` or calling `Loop.SetPaused(true)` kept display frames running without ever releasing `releaseAwake`. Any game pausing for a pause menu, title screen, or game-over screen — including every template that sets `Paused = true` on background and shows a menu on return — prevented iOS and Android auto-lock indefinitely.

## Decision

Manage `loopRun.releaseAwake` through a single `loopRun.syncAwake(active bool)` helper driven by pause state, pending steps, `OnDemand` dirty draws, and an optional `Loop.PausedAwake time.Duration` grace period:

- **Default immediate release (`PausedAwake <= 0`)**: releasing the keep-awake hold does not turn the screen off immediately; it lets the OS's own idle auto-lock timer run. Starting with `Paused: true` (or returning from background while `Paused: true`) never acquires a hold when `PausedAwake <= 0`.
- **Optional grace period (`PausedAwake > 0`)**: `loopRun.awakeIdle` accumulates clamped frame `dt` while the foreground loop is paused with no pending `Step` updates and (when `OnDemand` is set) no `MarkDirty` draw. Once `awakeIdle >= PausedAwake`, `syncAwake` releases the hold. Redundant `SetPaused(true)` calls leave `awakeIdle` untouched; unpausing, `Step(n)`, `StepNow(n)`, `MarkDirty()` (when `OnDemand` is true), or returning to foreground resets `awakeIdle = 0`.
- **Synchronous vs. next-frame transitions**: `SetPaused`, `Step`, and `MarkDirty` call `syncAwake` immediately (after `OnPause` in `syncPause` so `OnPause` overrides or `stop()` take precedence). Direct edits to `Loop.Paused` or `Loop.PausedAwake` apply on the next frame (or in the same frame when `Update` pauses the loop). Every `KeepAwake` acquisition runs inside `r.cx.Run(...)` so `r.cx` owns the hold and scope disposal always releases it.
- **Stepping and `OnDemand` draws**: while `pendingSteps` run or a paused `OnDemand` loop has a pending `MarkDirty` draw (including frames deferred by `DrawRate`), `syncAwake(true)` keeps the hold through `Update` and `Draw`, then `syncAwake(false)` re-evaluates after `Draw` (before early `canDraw` returns too, so idle `OnDemand` loops still advance `awakeIdle` and release). Unpaused `OnDemand` loops keep a single continuous hold across sub-step frames that skip `Draw`.
- **Unchanged boundaries**: `LifecycleBackground`, `stop()`, and scope disposal still release immediately regardless of `PausedAwake`, and `cutscene.Player`'s independent `"cutscene"` display hold (`pkg/cutscene/player.go`) stays untouched.

## Alternatives considered

### Default `PausedAwake` to 30 s instead of 0

Strongest argument: a non-zero default keeps the screen bright for 30 s when a player pauses briefly to glance at a menu before unpausing, avoiding premature dimming on phones with very short OS auto-lock timeouts.

Rejected because releasing `KeepAwake` already delegates to the OS's configured idle auto-lock timeout (typically 30 s–5 min), so a 30 s default would double the shortest lock delay, contradict `Loop.Run`'s documented contract ("releasing the hold when paused"), and still hold an unnecessary wake lock on title screens or pause-on-background returns. Games that want extra menu grace time can opt in explicitly via `PausedAwake`.

### Treat every `OnDemand` frame with zero updates as idle even when `!Paused`

Strongest argument: checking `steps == 0 && !dirty` uniformly for `OnDemand` loops regardless of `Paused` would cover hypothetical unpaused loops with very low `Rate` or tiny `TimeScale`.

Rejected because an unpaused `OnDemand` loop whose `Rate` is below the display refresh rate (such as 30 Hz on a 60 Hz display or 60 Hz on a 120 Hz display) runs zero updates on alternating frames. Releasing on `steps == 0` with default `PausedAwake == 0` would acquire and release OS power assertions 30–60 times per second. In `game.Loop`, `Rate <= 0` defaults to 60 and `TimeScale <= 0` defaults to 1, so unpaused loops always step continuously and idle `OnDemand` loops use `Paused: true`.

## Consequences

- **Benefit**: Paused games, pause-on-background menus, title screens, and idle `OnDemand` loops no longer block OS auto-lock indefinitely, while `PausedAwake`, `Step`, `MarkDirty`, and `cutscene.Player` remain supported without wake-lock churn.
- **Cost**: `Loop` gains one additive exported field (`PausedAwake time.Duration`, zero-value default) and `loopRun` tracks an extra `awakeIdle` duration.

## Verification

- `loop_test.go`:
  - `TestPausedReleasesDisplayKeepAwakeAndUnpauseReacquires` covers `SetPaused(true)` immediate release, 10-minute `FastForward` repro, redundant `SetPaused(true)`, `SetPaused(false)` immediate re-acquire, direct `Paused` mutation, starting paused, returning from background paused, pausing inside `Update`, and stop-then-unpause.
  - `TestPausedAwakeGracePeriodAndDynamicChanges` covers `PausedAwake` countdown, non-reset on redundant `SetPaused(true)`, direct `Paused` mutation, negative and dynamically changed `PausedAwake`, and immediate release on background/stop during grace.
  - `TestPendingStepsKeepDisplayAwakeWhileStepping` covers holding across `Step(n)` `Update` and `Draw`, background deferral, and grace-period reset.
  - `TestOnDemandIdleReleasesDisplayKeepAwake` covers idle `OnDemand` release, `MarkDirty` hold through `DrawRate`-deferred draws, grace-period reset on `MarkDirty`, and zero wake-lock churn on an unpaused 30 Hz `OnDemand` loop.
- Verified with `gofmt -l *.go`, `go tool nsgo test .`, `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 .`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.
