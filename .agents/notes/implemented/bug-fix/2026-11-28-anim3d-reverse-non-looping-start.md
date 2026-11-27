# Agent Note: Start reversed non-looping anim3d clips at Duration

Status: implemented

## Problem

`StateMachine` initialized every state's `currentTime` to `0` on initial registration, immediate transitions, crossfades, and restarts (`pkg/anim3d/statemachine.go`), and `Player.Play` / `Player.Crossfade` reset `p.time` to `0` when switching to a different animation index (`pkg/anim3d/playback.go`). Because `Clip.NormalizeTime` and `advancePlayerTime` clamp negative times back to `0` when `Loop` is `false`, any non-looping clip played with negative speed started at `0`, stepped below `0`, and clamped back to `0` forever — never animating past frame 0 and never firing its clip events.

## Decision

Non-looping clips played with negative speed start at `Duration` and play backward to `0`, while looping clips keep starting at `0` (where modular wraparound already plays backward through the full cycle):

- **`StateMachine` entry paths (`pkg/anim3d/statemachine.go`)**: `initialStateTime(st)` returns `st.Clip.Duration` when `st` resolves to a clip (`Blend1D`, `Blend2D`, and `Sampler` are all nil), `!st.Clip.Loop`, `st.Clip.Duration > 0`, and `st.Speed < 0`; otherwise `0`. Both `beginTransition` (covering `SetState`, `Restart`, and conditional `Transition`s, immediate and crossfaded) and first-time registration of the active state in `AddState` initialize `sm.currentTime` from `initialStateTime`.
- **`Player` entry paths (`pkg/anim3d/playback.go`)**: `Player.startTime(animIndex)` returns `p.animDurations[animIndex]` when `!p.loop && p.speed < 0`, and `0` otherwise. `Play` (on animation index switch) and `Crossfade` (both immediate and crossfaded paths) initialize `p.time` via `startTime`.
- **Speed sign flips at clamped ends**: when an already-active non-looping clip's effective speed changes sign while parked at the end clamped by the new direction (`t <= 0` when flipping negative, or `t >= Duration` when flipping positive), playback jumps to the opposite end so it plays instead of remaining stuck. This applies in `StateMachine.AddState` when replacing `sm.current`, and in `Player.SetSpeed` when `p.playing && !p.loop` (which also re-evaluates the pose). Mid-clip speed flips keep the current timestamp for continuous reversal, and same-sign speed changes at a clamped end do not spuriously restart a finished clip.
- **Reverse clip events**: `StateMachine.advanceState` already passes unwrapped `(curr, curr + scaled)` into `collectDirected` (`pkg/anim3d/events.go`), which mirrors time (`duration - t`) and delegates to `CollectClipEvents`. Starting at `Duration` therefore fires boundary (`Duration` and `0`) and interior events in reverse order strictly once with the same seam semantics as forward playback.

## Alternatives considered

### Start looping clips at `Duration` when speed is negative

Starting looping clips at `Duration` would give looping and non-looping clips the same numeric start timestamp. Rejected because looping clip time lives in `[0, Duration)`, `NormalizeTime(Duration)` wraps back to `0`, and starting at `0` already wraps smoothly into the end of the cycle on the first negative step without breaking existing callers and tests (`TestReversePlaybackFiresCrossedEvents`, `TestPlayerControlsPauseStopSpeedSeekAndNonLooping`).

### Leave speed sign flips at clamped ends unchanged

Requiring callers to call `Restart` or `Seek(Duration)` whenever they flip a running state's or `Player`'s speed sign would keep `AddState` and `SetSpeed` free of time adjustments. Rejected because a very common idiom is `p.SetLoop(false); p.Play(idx); p.SetSpeed(-1)` (or replacing a state's `Speed` before stepping), which would silently stay clamped at `0` and immediately stop on the first step. Limiting the jump strictly to sign flips while parked at the newly clamped end avoids disturbing mid-clip reversals or already-finished clips on same-sign speed changes.

## Consequences

- **Benefits**: Reversed one-shot animations (such as closing doors or retracting weapons) work out of the box in `StateMachine` (initial states, transitions, crossfades, `Restart`, and `AddState` speed flips) and `Player` (`Play`, `Crossfade`, and `SetSpeed`), and fire clip events in reverse order.
- **Costs**: Callers that previously inspected `CurrentTime()` / `Time()` right after entering a reversed non-looping clip now observe `Duration` instead of `0`, and `docs/en/anim3d.md` / `docs/zh/anim3d.md` document this contract. `Player.SetLoop(false)` intentionally does not jump `Time()` if a looping reverse clip happens to be at `0`, so switching off looping mid-cycle finishes cleanly rather than replaying an extra cycle.
