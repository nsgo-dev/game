# Agent Note: Idle unpaused OnDemand loops via Loop.Idle and input wake hooks

Status: implemented

## Problem

`game.Loop.OnDemand` previously marked a frame dirty whenever at least one fixed `Update` step ran. Because an unpaused loop always runs `Rate` steps per second, `OnDemand: true` at `Rate: 60` on a 60 Hz display ran 60 updates and 60 draws per second even while nothing changed (such as a visual novel line waiting for a tap). Setting `Paused: true` stopped updates, which prevented `input.Actions.Step()` from running and noticing the input that should wake the loop, and nothing in `pkg/input` called `Loop.MarkDirty`.

## Decision

Add explicit idle detection to `game.Loop` and wake callbacks to `pkg/input`, keeping `OnDemand` without `Idle` backward compatible and integrating with display keep-awake management ([2026-11-28-loop-paused-keep-awake.md](2026-11-28-loop-paused-keep-awake.md)):

- **`Loop.Idle func() bool`**: when `OnDemand` is `true`, `Paused` is `false`, and `Idle` returns `true`, `loopRun.frame` skips fixed `Update` steps and `Draw` without accumulating display time (`r.acc = 0`) and counts idle time toward `PausedAwake` release. If `Idle()` becomes `true` mid-frame during a multi-step catch-up, remaining steps on that frame stop and `r.acc` resets to 0.
- **Coalesced wake step on `Loop.MarkDirty`**: calling `MarkDirty()` sets `dirty = true` and, when `OnDemand && !Paused`, sets a boolean `wakeStep = true`. Multiple `MarkDirty()` calls between frames (such as `TouchStart` + `TouchEnd` in a tap) coalesce into a single fixed `Update(step)` and `Draw(0)` on the next display frame. `wakeStep` clears after the frame's update phase so shared helpers calling `MarkDirty()` inside `Update` do not cause a spurious extra wake frame. When `Paused` is `true`, `MarkDirty()` continues to request `Draw` only without running `Update`.
- **Input wake hooks (`Actions.Wake`, `TouchControls.OnWake`, `GestureRecognizer.Wake`)**:
  - `Actions.Wake` fires on touches, mouse button presses/releases/drags, context menu pulses, pointer-lock deltas, key presses/releases (`OnKeyDown`/`OnKeyUp` and `WatchKeys` changes), injected triggers/presses/axes, and gamepad changes (`OnGamepads` and `WatchGamepads`). Idle mouse hover (`OnMouseMove` with no button held), redundant `WatchKeys` startup effects, and gamepad analog drift inside dead zones do not wake the loop.
  - When `WatchGamepads(cx)` has connected controllers in the foreground, a lightweight RAF poller checks button bitmasks and bound dead-zoned stick axes so controller presses wake an idle loop; zero RAF callbacks are scheduled when no controller is connected.
  - `Actions.Step` records `stepLatched` for actions latched since the previous `Step`, and `JustPressed` returns `(curr && !prev) || stepLatched` so consecutive quick taps separated by an idle gap (where no empty intermediate `Step` ran to clear `curr`) both fire `JustPressed` without breaking held-key semantics.
  - `TouchControls` uses `OnWake` (since `TouchControls.Wake()` already exists for resetting idle-fade opacity) and also wakes through a bound `Actions`. `GestureRecognizer` adds `Wake` and likewise falls back to its bound `Actions`.
- **Visual novel template follow-up**: `template/visualnovel/template/go.mod` pins released `github.com/nsgo-dev/game v0.1.0` and CI tests each template module with `GOWORK=off` against its pinned modules. Adopting `Loop.Idle` and `Actions.Wake` in `template/visualnovel/template` (and `internal/gamecli/_templates/projects/visualnovel`) therefore waits on tagging `v0.1.1` alongside the other post-`v0.1.0` template updates (#1665, #1461, #1466).

## Alternatives considered

### Have `Update` signal changes through a `Loop.Changed()` call instead of `Loop.Idle`

Strongest argument: the game decides whether state changed inside `Update` itself without maintaining a separate predicate.

Rejected because if `Loop` must run `Update` on every frame just to learn whether `Changed()` was called, an unpaused loop still runs 60 `Update` calls per second while idle, violating the goal of skipping both `Update` and `Draw` until input arrives (and forcing games to guard `Update` against advancing clocks while idle). A pure `Idle func() bool` predicate lets `Loop` skip both `Update` and `Draw` on idle frames and immediately integrate with `syncAwake`.

### Pause the loop (`Paused: true`) and wake by unpausing on input

Strongest argument: reuses existing `Paused` semantics with no new `Loop` field.

Rejected because `Paused` drives `OnPause` (pausing `sound.Mixer` music and ambience) and `Immersive` (bringing phone system bars back in and out). A visual novel waiting for a tap on a dialogue line is playing, not paused: background audio and immersive bars must remain uninterrupted while the loop idles.

## Consequences

- **Benefit**: Idle `OnDemand` loops execute 0 updates and 0 draws per second with no input, wake cleanly for 1 update and 1 draw on tap/key/controller input, and let the display auto-lock after `PausedAwake`. Existing `OnDemand` loops without `Idle` keep identical behavior.
- **Cost**: `Loop` gains one exported field (`Idle func() bool`) and one unexported flag (`wakeStep`); `pkg/input` adds `Actions.Wake`, `TouchControls.OnWake`, `GestureRecognizer.Wake`, and `Actions`'s `stepLatched` map plus foreground connected-gamepad wake polling. Updating `template/visualnovel/template` to use `Loop.Idle` and `Actions.Wake` is deferred until `v0.1.1` is tagged so `GOWORK=off` pinned-module template CI stays green.

## Verification

- `loop_test.go`: `TestOnDemandIdleSkipsUpdatesAndDrawsAndWakesOnMarkDirty` covers 0 updates / 0 draws over 1s idle, coalesced `MarkDirty` waking for 1 Update + 1 Draw, no catch-up after long idle, mid-frame transition back to idle, `MarkDirty` inside `Update` causing no extra frame, `DrawRate` deferral, and `PausedAwake` grace-period release/re-acquire.
- `pkg/input/wake_test.go`: covers `Actions.Wake` with tap, mouse hover non-wake, consecutive quick taps across idle gaps, `WatchGamepads` idle/dead-zone/press/release/axis wake, `TouchControls.OnWake` and bound `Actions.Wake`, and `GestureRecognizer.Wake`.
- Verified with `gofmt -l .`, `go tool nsgo test`, `go tool nsgo check -target macos_arm64,web -jobs 2`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.
