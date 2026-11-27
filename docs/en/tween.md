# tween

`game/tween` animates numbers, points, colors, custom types, timelines, and `tilemap.Camera` moves deterministically on `game.Loop`'s fixed steps using NSGo's `core.Curve` easing functions and named eases.

```go
import "github.com/nsgo-dev/game/pkg/tween"
```

## Creating and stepping a Tween

Construct a `*Tween` with `Float32`, `Float64`, `Point`, `Color`, or generic `New[T]`, optionally chain `Delay`, `Repeat`, and `OnComplete`, and step it via `Update(dt)` (or a `Group`):

```go
var group tween.Group

popup := tween.Float32(0, 1, 300*time.Millisecond, core.EaseOut, func(v float32) {
	banner.ScaleX, banner.ScaleY = v, v
}).Delay(100 * time.Millisecond).
	OnComplete(func() { bannerReady = true })

bob := tween.Float32(0, -6, 600*time.Millisecond, core.EaseInOut, func(y float32) {
	coin.Y = baseY + y
}).Repeat(tween.RepeatForever, true) // yoyo forever

group.Add(popup, bob)

// Inside game.Loop.Update:
group.Update(dt) // finished tweens are removed automatically
```

- **Easing**: a zero `core.Curve` defaults to `core.Linear`.
- **`Repeat(count, yoyo)`**: runs `count` extra passes after the first (`0` plays once; `tween.RepeatForever` or any negative value repeats forever). When `yoyo` is `true`, alternating passes run back from `to` to `from`.
- **Seeking and resetting**: `tw.Seek(at)` jumps to timestamp `at` and applies that exact value without firing `OnComplete`; `tw.Reset()` rewinds to `0`. Inspect `tw.Done()` and `tw.Duration()`.
- **Custom types**: use `tween.New[T](from, to, d, curve, lerp, set)` for vectors, transforms, or domain structs.

## Named eases, and reading the start value late

`tween.Ease` is a plain easing function with a full named set covering the sine, quad, cubic, quart, quint, expo, circ, back, elastic, bounce and spring families, each in the `In`, `Out`, `InOut` and `OutIn` variants (`tween.BackOut`, `tween.ElasticOut`, ...). The names match the `interp` values of resource curve keys, so clips and code share the same eases.

`To`, `By` and `FromCurrent` read the start value lazily when the step actually runs, not when it is built, so a step always starts from whatever an earlier step left behind:

```go
// Pop the scale up, then drift up by 40 points — no start values written down.
coin := tween.Sequence().
	Then(tween.To(&coin.Scale, 1.4, 80*time.Millisecond, tween.BackOut)).
	With(tween.By(&coin.Y, -40, 300*time.Millisecond, tween.QuadOut, nil, nil)).
	Build()
```

- `tween.To(&v, target, d, ease)` animates `*v` to `target`.
- `tween.By(&v, delta, d, ease, add, lerp)` animates `*v` to its value at step start plus `delta`; pass `nil, nil` for the built-in types (`float32`, `float64`, `core.Point`, `core.Color`) and supply `add` and `lerp` for custom types.
- `tween.FromCurrent(get, set, target, d, ease)` reads the start through `get` for values that are not a plain field.
- `To` and `FromCurrent` take an optional `lerp` argument for custom types; without one they hold the start value and snap to the target at the end, matching `New`.
- Rewinding a timeline (or `Reset` on the tween) clears the captured start so the next run reads it again.

## Chaining steps with Sequence

`Sequence` builds a `Timeline` without computing start times by hand: `Then` appends a step after the previous block, `With` runs a step in parallel with the last one, `Wait` inserts an interval, `Call` schedules a callback at the cursor and `Loops` repeats the run:

```go
seq := tween.Sequence().
	Then(tween.To(&coin.Scale, 1.4, 80*time.Millisecond, tween.BackOut)).
	With(tween.By(&coin.Y, -40, 300*time.Millisecond, tween.QuadOut, nil, nil)). // parallel with the last step
	Wait(100 * time.Millisecond).
	Then(tween.FromCurrent(getGlow, setGlow, 0, 200*time.Millisecond, tween.ElasticOut)).
	Call(func() { score.Add(1) }).
	Loops(3)

var group tween.Group
group.AddTimeline(seq.Build())
group.SetSpeed(0.25) // slow motion
group.SetPaused(true)

// Inside game.Loop.Update:
group.Update(dt)
```

- `With` starts at the same time as the last `Then`'s step; the cursor advances to the end of the longer of the parallel steps.
- `Loops(n)`: `n <= 0` wraps forever, `n == 1` plays once, larger `n` wraps `n-1` more times. A looping run replays every step identically, so it stays deterministic.
- `Timeline.SetLoops(n)` does the same on any timeline, and `Timeline.SetSpeed(s)` scales its steps; `Group.SetSpeed` and `Group.SetPaused` scale or freeze every tween and timeline in the group together (for slow motion or pausing with the game). `Group.Bind(cx)` clears the group when the scope ends.

## Coordinating cutscenes with Timeline

`Timeline` places multiple tweens and callback cues on one deterministic clock with play, pause, reverse, loop, and scrub (`Seek`) support:

```go
tl := tween.NewTimeline().
	Add(0, tween.Float32(0, 1, 400*time.Millisecond, core.EaseOut, setFade)).
	Append(tween.Point(startPos, endPos, 800*time.Millisecond, core.EaseInOut, setPos)).
	Call(600*time.Millisecond, func() { playSfx("whoosh") })

tl.Play()

// Inside game.Loop.Update:
tl.Update(dt)
```

- `Add(at, tw)` places `tw` starting at offset `at`; `Append(tw)` places `tw` at the timeline's current `Duration()`.
- `Call(at, fn)` fires `fn` whenever playback crosses `at` (and never fires during `Seek`).
- Control playback with `Play()`, `Pause()`, `Restart()`, `Reverse(bool)`, `SetLoop(bool)` or `SetLoops(n)` for counted runs, `SetSpeed(s)`, and `Seek(at)` (which places every track — before, during, and after its window — at its exact state at `at`).

## Camera choreography

Built-in camera tween helpers animate a `*tilemap.Camera` directly and clamp to `cam.Bounds` whenever set:

```go
// Pan to a boss arena, zoom in, then smoothly hand back to following the player
intro := tween.NewTimeline().
	Add(0, tween.CameraPan(cam, boss.X, boss.Y, 900*time.Millisecond, core.EaseInOut)).
	Add(200*time.Millisecond, tween.CameraZoom(cam, 1.5, 700*time.Millisecond, core.EaseOut)).
	Append(tween.CameraZoom(cam, 1.0, 500*time.Millisecond, core.EaseInOut)).
	Append(tween.CameraFollowTransition(cam, func() core.Point {
		return player.Pos
	}, 600*time.Millisecond, core.EaseInOut))

intro.Play()
```

Available camera tweens:

| Helper | Animates |
| --- | --- |
| `CameraPan(cam, toX, toY, d, curve)` | `cam.X`, `cam.Y` from start position to `(toX, toY)`. |
| `CameraZoom(cam, toZoom, d, curve)` | `cam.Zoom` (treating `<= 0` as `1`) to `toZoom`. |
| `CameraRotate(cam, toRot, d, curve)` | `cam.Rotation` (in radians) to `toRot`. |
| `CameraMove(cam, toX, toY, toZoom, toRot, d, curve)` | Position, zoom, and rotation simultaneously. |
| `CameraFollowTransition(cam, targetFn, d, curve)` | Blends smoothly from the camera's starting position onto a live moving `targetFn()`, ending directly on `targetFn()`. |
