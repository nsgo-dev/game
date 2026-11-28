# Agent Note: Loop display frame-rate requests

Status: implemented

## Problem

`Loop.DrawRate` limited drawing work while `core.Scope.RequestAnimationFrame` continued to wake the app at the panel's full refresh rate. Paused and idle on-demand loops had the same wakeup cost, which reduced the battery benefit of those modes.

## Decision

`loopRun.syncFrameRate` updates the owning scope's `core.FrameRateRange` whenever a run starts, each frame observes changed loop fields, or pause and wake methods change state. A positive `DrawRate` becomes the preferred display rate. `MatchDisplayToRate` opts uncapped loops into the effective fixed update rate. Paused and idle `OnDemand` loops request a conservative 60 Hz idle rate, or their active preference when one is configured, until pending steps or dirty work wake them. Stopping or backgrounding restores a zero range so the host follows its normal display rate.

The existing draw accumulator remains the correctness fallback when a host ignores or rounds the requested rate. The host can select another supported rate within its platform constraints.

## Alternatives considered

- **Keep requesting every display frame and only skip `Draw`**: preserves the old behavior but leaves the battery and thermal cost that this issue targets.
- **Always match `Rate`**: lowers wakeups for uncapped games but changes the default behavior of games that intentionally render at the panel rate, so matching is opt-in through `MatchDisplayToRate`.

## Consequences

Games using `DrawRate` now reduce both draw calls and display callbacks where the platform honors preferred rates. Paused and idle loops continue receiving callbacks at a conservative 60 Hz for state changes and wakeups. Existing callers that leave `DrawRate` and `MatchDisplayToRate` at their zero values retain the usual cadence on 60 Hz displays. The 60 Hz idle value is a policy constant and may be revisited if platform power measurements show a better trade-off.

## Verification

`TestDrawRateRetunesTheDisplayFrames`, `TestPausedLoopRequestsAnIdleDisplayRateAndRestoresIt`, and `TestMatchDisplayToRateRequestsTheSimulationRate` exercise preferred-rate behavior through `nsgotest`'s frame scheduler. Repository checks run with `go tool nsgo check` and `go tool nsgo test`.
