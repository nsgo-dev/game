# Agent Note: Captioned positional sounds and cutscene dialogue

Status: implemented

## Problem

`a11y.CaptionTrack` only showed what a game pushed into it by hand, so every positional sound needed a second `ShowSFXAt` call with a pan re-derived beside the mixer's, and cutscene dialogue from `cutscene.Player.Dialogue` never reached `CaptionsView` and its caption settings. Repeated effects such as gunfire would also flood the caption area if captioned on every play. Issue #1774 (part of #1479); this change covers the sound hook, the debouncing emitter and the cutscene bridge, not the YAML `caption` field, `vn`/flow bridges or the sound indicator view.

## Decision

- `sound.Effect.SetCaption(label, callback)` stores a label and a `func(label string, pan float32)`. `PlayAtWith` and `PlayAt3DWith` call it after `playInternal` reports a playback, with the pan they gave the voice (`Listener2D.Voice`'s pan, `Listener3D.Spatialize`'s pan), before the mixer's mono and balance settings, so the caption keeps the true direction even in mono. Hooking the `With` forms covers `PlayAt`, `PlayAt3D` and the `Cue` positional plays, which all go through them. Plain `Play` has no position and does not call it.
- `a11y.CaptionEmitter` holds a track and a `MinInterval` (default 250 ms) and drops a label shown less than `MinInterval` ago. It reads the track's logical clock, which `CaptionTrack.Update` advances on the fixed loop step, rather than the wall clock, so the debounce is deterministic in tests and replays and pauses with the track.
- `cutscene.CaptionBridge.Update`, called after `Player.Step`, shows a newly active dialogue line with `ShowDialogue` and an hour-long duration, and dismisses that caption when the line ends or another replaces it. The timeline's `DialogueLine` carries no clip length, so the bridge ends the caption from the line's state instead of guessing a duration; the track's default 3 s would cut longer lines short.

## Alternatives considered

- Importing `a11y` from `sound`: rejected, `a11y` imports `input`, which imports `sound`, so it would be an import cycle; a callback also lets a game route sound captions through its own policy.
- Debouncing on `time.Now`: rejected, it made the emitter depend on wall time, so a test or replay at a different speed would show different captions, and a paused game would still age the interval.
- Leaving the cutscene caption to expire on the track's default duration: rejected, it either cuts a long line short or outlives a short one.

## Consequences

Games opt in per effect and per player, so no existing behavior changes. The emitter's per-label map grows with the number of distinct labels, which is bounded by the game's sound set. The YAML `caption` field on sound definitions, `vn.Presenter` and flow line bridges, and `a11y.SoundIndicatorView` from #1774 remain open.
