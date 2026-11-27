### Added

- `game/tween`: `tween.Ease`, a named easing function covering the sine, quad, cubic, quart, quint, expo, circ, back, elastic, bounce and spring families, each in the in, out, in-out and out-in variants, sharing its names with the `interp` values of resource curve keys so clips and code use the same eases (#1411).
- `game/tween`: `To`, `By` and `FromCurrent` read the start value when the step runs, not when it is built, so steps compose from whatever an earlier step left behind; rewinding the timeline reads the start again (#1411).
- `game/tween`: `Sequence` builds a `Timeline` without computing start times by hand — `Then` appends steps, `With` runs a step in parallel with the last one, `Wait` inserts an interval, `Call` schedules a callback and `Loops` repeats the run — and `Timeline.SetLoops(n)` counts runs exactly on any timeline (#1411).
- `game/tween`: `Timeline.SetSpeed`, `Group.SetSpeed` and `Group.SetPaused` scale or freeze playback for slow motion and game pauses, `Group.AddTimeline` steps timelines alongside tweens, and `Group.Bind(cx)` clears the group when the scope ends (#1411).
