# Agent Note: Re-capture camera tween start state on Reset while preserving deterministic Seek

Status: implemented

## Problem

All five camera helpers in `pkg/tween/camera.go` (`CameraPan`, `CameraZoom`, `CameraRotate`, `CameraMove`, and `CameraFollowTransition`) capture the camera's starting position, zoom, or rotation lazily inside `Tween.onStart`, which `Tween.eval` runs only while `!t.started`. Previously `Tween.Reset()` cleared `elapsed`, `done`, and `onRewind`, but left `started` set to `true`. Replaying a camera tween after `Reset()` therefore skipped `onStart` and snapped the camera back to the origin captured during its very first run instead of animating from the camera's current state (`nsgo-dev/game#1589`).

## Decision

Clear `t.started = false` inside `Tween.Reset()`, while keeping `Tween.Seek(0)` and `Tween.seekBefore()` unchanged for `onStart`-based camera tweens:

- **`Tween.Reset()` (`pkg/tween/tween.go`)**: sets `elapsed = 0`, `done = false`, `started = false`, and runs `onRewind` if present. On the next `Update` (after any `Delay`) or `Seek`, `eval` runs `onStart` again so every camera tween captures the camera's current `X`, `Y`, `effectiveZoom(Zoom)`, and `Rotation`. This matches the existing `Reset` contract of `To`, `By`, and `FromCurrent` (see [tween sequences and lazy start values](../feature/2026-11-27-tween-sequences-eases.md)).
- **`Tween.Seek(0)` stays deterministic**: `Seek(at)` does not clear `started` (or call `onRewind`), so scrubbing back to `0` on a standalone tween or calling `Timeline.Seek(0)` / `Timeline.Restart()` / looping a `Timeline` restores the already-captured start instead of overwriting it with whatever mid-run state the camera currently holds.
- **`Tween.seekBefore()` keeps backward scrubbing**: for `onStart` tweens that have already started, `seekBefore()` still applies `eval(0)` when a timeline scrubs earlier than the track's start window (restoring independent properties such as a delayed `CameraZoom` track to its pre-track zoom), while unstarted or freshly `Reset` tracks still return early so they do not capture prematurely.

## Alternatives considered

- **Move camera tweens onto `onRewind` like `To`/`By`/`FromCurrent`**: unifies lazy capture under one hook, but `seekBefore()` invokes `onRewind()` and returns without calling `eval(0)`. For a timeline with an independent later track (for example `CameraZoom` starting at 200 ms alongside a pan), scrubbing backward to 100 ms would leave `cam.Zoom` at its mid-zoom value instead of restoring the pre-track zoom, breaking existing timeline scrubbing semantics.
- **Also clear `started` in `Tween.Seek(0)`**: would make `Seek(0)` behave like `Reset`, but breaks deterministic scrubbing (`Seek(0)` after `Update(500ms)` would capture the 500 ms position as the new start) and breaks `Timeline.Restart()` and looping timelines, which call `Seek(0)` on tracks starting at offset 0.

## Consequences

- **Benefits**: Reusing camera tweens via `Reset()` (standalone, inside `Group`, or when resetting tracks before `Sequence`/`Timeline.Restart()`) starts smoothly from the camera's current pose without snapping back to stale coordinates, while `Seek`, `Timeline.Restart`, and looping timelines remain deterministic.
- **Costs**: `Tween` retains two distinct lazy-start lifecycle paths (`onStart`/`started` for camera tweens that restore `eval(0)` on backward timeline seeks, and `onRewind` for `To`/`By`/`FromCurrent` that leave the target untouched before their window). Callers that want to replay a camera move from a fixed origin after `Reset` must restore the camera or animate via explicit value tweens.
