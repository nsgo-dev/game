# Agent Note: Queue autoplay-blocked music tracks on NotAllowed and retry via Mixer.Unblock

Status: implemented

## Problem

On the web, browsers reject audio output until the page has received a trusted user gesture. NSGo v0.3.4 reports that refusal on `audio.Player` as `audio.NotAllowed` (`core.AudioNotAllowed`) through `OnError`. Previously `sound.Music` treated every `OnError` identically—marking the track `done = true` and never retrying it—so title or ambient music started during setup (as in `examples/lantern3d`) remained silent even after the player clicked or pressed a key, until the game happened to call `Music.Play` again (`nsgo-dev/game#1481`).

## Decision

Keep autoplay-refusal recovery in `pkg/sound` and wire automatic gesture retries through `pkg/input.Actions`:

- **Distinguish `audio.NotAllowed` in `pkg/sound` (`sound.go`, `sections.go`)**: `track` carries a `blocked` flag alongside `playing` and `done`. When `OnError` receives an error matching `errors.Is(err, audio.NotAllowed)`, `Music` clears `playing`, sets `blocked = true`, leaves `done = false`, and preserves the track as `current` (or in `out`) at position `0` with its group volume, crossfade level, loop settings (`Loop`, `LoopStart`), and any armed `PlaySections` loop continuation intact. While `blocked` is `true`, `track.apply` still updates muted/volume state so crossfades and volume sliders stay in sync, without calling `Play` again on every animation frame. Every other error (such as `audio.Decode`) continues to mark the track `done = true` and cancel any armed `PlaySections` continuation.
- **Reactive `Music.Blocked()` and `Mixer.Unblock()`**: `Mixer.Music` creates a `*core.Signal[bool]` mirrored from `mu.current != nil && mu.current.blocked` so reactive views (`core.Show`, `ContentFunc`) can display a "tap for sound" hint without polling. `Mixer.Unblock()` clears `blocked` across every track on every `Music` owned by the mixer (including fading-out tracks and sectioned tracks) and re-applies mixer state: audible tracks call `p.Play(nil)` immediately at their current volume/fade level, while muted, paused, suspended, or backgrounded tracks clear `blocked` and start as soon as gain becomes positive. Calling `Unblock()` when no track is blocked is a no-op.
- **Gesture hook on `pkg/input.Actions` (`Actions.Mixer`)**: `Actions` exposes `Mixer *sound.Mixer` (both `pkg/input` and `pkg/sound` are Stable, with no import cycle) and calls `Mixer.Unblock()` on pointer press (`OnPressDown`, `OnContextMenu`), touch (`OnTouch` on `TouchStart`/`TouchEnd` and `TouchControls.Attach`), and key press (`OnKeyDown`, `WatchKeys`) until NSGo signals audio unlock directly (`nsgo-dev/nsgo#2675`). Calling `Unblock()` on every gesture rather than latching after the first single event ensures recovery if an early event (such as `touchstart` on iOS Safari) fails and a subsequent `touchend` or click succeeds, at zero allocation cost once unblocked.
- **Lantern 3D wiring (`examples/lantern3d`)**: Lantern 3D assigns `actions.Mixer = aud.Mixer` and also invokes `Audio.Unblock()` from its 3D viewport mouse/touch handlers and HUD button helpers so whichever element the player clicks first unblocks the ambient track.

## Alternatives considered

- **Global implicit registry between `pkg/sound` and `pkg/input`**: would let `input.Actions` unblock every `Mixer` without setting `Actions.Mixer`, but `Actions` does not own a `*core.Scope`, so a global registry cannot tell which engine goroutine owns a mixer in multi-app or parallel `nsgotest` runs and would risk cross-goroutine races or deferring via `cx.Post` outside the synchronous browser user-gesture turn. Explicit `Actions.Mixer` matches `scene.Manager.Actions`, `cutscene.Player.Mixer`, and `screen.Video.Loop`.
- **Keep advising authors to start music inside a button press**: avoids code changes, but breaks the standard pattern of starting title/ambient music during setup that already works on every native target and leaves web builds silently broken whenever authors test on desktop.

## Consequences

- **Benefits**: Music started at startup plays automatically after the first click, tap, or key press on the web with group volume, crossfade, loop points, and sectioned intro handoffs intact; decode and other permanent errors still terminate tracks cleanly; games can bind reactive "tap for sound" hints directly to `Music.Blocked()`.
- **Costs**: `pkg/input` now imports `pkg/sound` (both Stable), and games that use standalone press handlers outside `input.Actions` must call `Mixer.Unblock()` from those handlers until `nsgo-dev/nsgo#2675` lands.
