# Agent Note: Tween sequences, lazy start values, loop counts, speed scale and named eases

Status: implemented

## Problem

`pkg/tween` could interpolate an explicit `from`/`to` pair and place tweens at absolute times on a `Timeline`, but game-feel code — scale up, wobble, fade while a coin flies to the HUD — needs chained steps without computing start times by hand, parallel steps, waits, starts read from whatever a value currently is, relative amounts, counted loops, a playback speed scale, and the elastic/bounce/back eases that `core.Curve` (a cubic Bézier) cannot express. Issue #1411 asks for all of these in one idiom, on top of the existing `Tween`/`Timeline`/`Group` API, keeping determinism on fixed steps.

## Decision

The change lands in three new capabilities on the existing types plus one new builder, all in `pkg/tween`.

- `Ease` is the package's easing function type (an alias of the existing `EaseFunc`), with the full named set (sine, quad, cubic, quart, quint, expo, circ, back, elastic, bounce, spring; in, out, in-out, out-in) as package-level values in `ease.go`, implemented with the standard Penner formulas and pinned exact endpoints. `Tween` internally evaluates easing through a `func(u float64) float64` field instead of holding a `core.Curve`, so named eases and Bézier curves share one path; `core.Curve` constructors keep working unchanged.
- `To`, `By` and `FromCurrent` (`sequence.go`) capture the start value lazily at the first application of the step, so a step always starts from whatever an earlier step left behind. A `Tween.onRewind` hook clears the capture on `Reset` and on the timeline's pre-window rewind (`seekBefore`), so a scrubbed timeline re-reads the start when it enters the window again; `seekBefore` applies nothing for a lazy tween because its pre-start state is "leave the current value alone". Looping a timeline does not rewind tracks, so loops replay identically and stay deterministic.
- `Sequence` is a builder over `Timeline`: `Then` appends at the cursor, `With` places a step at the last `Then`'s start (parallel), `Wait` advances the cursor, `Call` adds a cue at the cursor, `Loops` and `Build` configure the timeline.
- `Timeline` loop state moves from a `loop bool` to a run counter (`loops int`: 0 = play once, -1 = forever, N = runs remaining counting the current pass); `SetLoop` maps onto it and `SetLoops(n)` counts runs exactly. `Timeline.SetSpeed` and `Group.SetSpeed`/`Group.SetPaused` scale or freeze steps; a zero speed stops advancement and negative/NaN scales fall back to 1. `Group` also gains `AddTimeline` and `Bind(cx)`, which clears the group through `core.Scope.Cleanup`.

Steps sharing a target keep the timeline's existing conflict resolution: past tracks resolve in forward order and the active track wins, so parallel steps on one target take the later track's value while both run and the later track's end value afterwards.

## Alternatives considered

- Share the named-ease implementation with `resource.Interp` from issue #1342. Rejected for now: #1342 is still open and unmerged, so there is no implementation to share yet; the tween values are written to the same formulas and names so a later change can fold them into one shared package without breaking callers.
- Make `Ease` a new float64 function type instead of aliasing `EaseFunc`. Rejected: two easing function types in one package would duplicate adapters (`EaseFromCurve`) and confuse the PathFollower options; float32 easing output is more than precise enough for animation values.
- Have `By` take a variadic option like `To`. Rejected: `By` needs both an adder and an interpolator for custom types and Go allows one variadic, so it takes `add` and `lerp` explicitly with `nil` meaning "use the defaults for the built-in types"; `To` and `FromCurrent` keep the shorter variadic form because they need only a lerp.

## Consequences

- The API matches Godot's `Tween` ergonomics (`Then`/`With`/`as_relative`/`from_current`/`set_loops`/`set_speed_scale`) while staying deterministic on fixed steps; game-feel snippets no longer hand-compute start times or start values.
- The named eases are also usable in `FollowerOptions.Ease` since `Ease` aliases `EaseFunc`, so path followers get elastic and bounce for free.
- Each ease is tested at 0, 0.5 and 1 against reference values, the sequences, parallel steps, loops, speed scale, pause, seek and lazy/relative starts are covered in `sequence_test.go`, and both `docs/en/tween.md` and `docs/zh/tween.md` document the new API.
- Costs: `By` has a noisier signature than `To` because custom types need an adder as well as a lerp; lazy start capture adds the `onRewind` contract to `Tween`, which a future tween type must observe if it also reads state late; and `Group` now tracks two collections (tweens and timelines), so `Len` intentionally reports tweens only.
