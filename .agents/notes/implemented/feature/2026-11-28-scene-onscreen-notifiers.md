# Agent Note: On-screen notifiers in scene files (on_screen component)

Status: implemented

## Problem

Issue #1369 asked for the Godot `VisibleOnScreenNotifier2D` / `VisibleOnScreenEnabler2D` pair: an object should report when its rectangle enters or leaves the camera view, and expensive parts of it (a particle emitter, a skeletal rig) should stop updating while it is off-screen. `BuiltScene.Update` advanced every emitter and rig unconditionally and had no camera input at all, and no signal path existed for visibility changes.

## Decision

Two pieces share one semantics:

- `scene.ScreenNotifier` (`pkg/scene/screennotify.go`) is the code-only form: a world `Rect`, a `Margin`, `OnEnter`/`OnExit` callbacks, and `Step(views ...*tilemap.Camera) bool`. It starts off-screen, so the first `Step` that finds the rectangle inside a view fires `OnEnter`, matching Godot. `Margin` is a one-directional hysteresis band: an on-screen notifier exits only after leaving a view by more than the margin (the view's bounds are expanded), while an off-screen notifier enters only by touching the view itself, so a rectangle hovering near an edge does not flap. A notifier is on screen when any view sees it (pass every split-screen viewport's camera); `Step` with no views (or only nil cameras) treats it as on-screen.
- The built-in `on_screen` component (`pkg/sceneformat/onscreen.go`) declares `version`, `x`, `y`, `width`, `height`, `margin` and a `pause` string list (`particles`, `skeleton`), plus `entered`/`exited` events. `Build` extracts runtime `sceneformat.ScreenNotifier` values (embedding `scene.ScreenNotifier`) into `BuiltScene.Notifiers` in document order. `BuiltScene.Update` steps them against the scene's own camera view before advancing layers, skips `Emitter.Update`/`Skeleton.Update` for paused parts while off-screen, and `BuiltScene.UpdateWithViews(dt, views...)` adds extra views for split-screen games. Events flow through the scene's `connections`: the caller sets `BuiltScene.ScreenSignals` to the `Signals` that `Connect` returned, and the notifier callbacks emit `on_screen.entered` / `on_screen.exited`.

Rectangles stay axis-aligned on both sides: an object's local rectangle is composed through its world transform and covered by the AABB of its transformed corners, and a rotated or zoomed camera is tested through `tilemap.Camera.VisibleBounds`, which is already the AABB of the view's corners. This keeps every test a cheap `math2d.Rect.Overlaps`.

## Alternatives considered

- **A spatial grid for the per-step tests**, as the issue suggests. Measured first: 5000 notifiers cost ~0.15 ms per `Update` step (`BenchmarkOnScreenUpdate5000`), roughly 30 ns per notifier per view, which is far under a frame budget. A grid would add build, resize and invalidation bookkeeping (objects move every frame) for no measurable win at any realistic notifier count, so the straight scan stays. If profiling a real game ever shows otherwise, the grid slots behind `Update` without changing the component schema or the `ScreenNotifier` API.
- **Emitting events through a callback field on `BuiltScene`** instead of a `*Signals`. Rejected: the scene format already has `connections` and `Signals.Emit` with declared-event checking (the `trigger2d` pattern), and wiring `entered`/`exited` through it gives YAML authors the same handler mechanism as every other component event. `ScreenSignals` is nil by default so `Build` alone never panics on unconnected scenes.
- **Changing `Update(dt)`'s signature to take views.** Rejected: it would break every existing caller for a feature most scenes do not use. `Update` keeps its signature and uses the scene's own camera view; `UpdateWithViews` is the opt-in for split-screen.
- **Testing the exact rotated view polygon** instead of its AABB. Rejected: `VisibleBounds` already defines "what the camera sees" for culling elsewhere in the engine, and matching it keeps notifier behavior consistent with drawing. The cost is that a rectangle near a rotated view's corner counts as on-screen slightly early, which is the safe direction for pausing.

## Consequences

- `BuiltScene` grows `Notifiers`, `ScreenSignals` and the unexported `notifierByObject` index; `Update` is unchanged for scenes without `on_screen` components (the notifier block is skipped when the slice is empty).
- Pausing skips `Update` entirely rather than freezing output, so an emitter or rig resumes exactly where it stopped (covered by `TestOnScreenUpdatePausesEmitter`, which checks particle ages across a pause/resume cycle).
- Tests cover enter/exit order, first-step-inside enter, margin hysteresis in both directions, zoomed and rotated cameras, multiple views with any-view-wins, empty rectangles, and connection-delivered events; the 5000-notifier benchmark guards the no-grid decision.
- Docs: `docs/en/scene.md` and `docs/zh/scene.md` gain an "On-screen notifiers in scene files" section; `from-godot.md` maps `VisibleOnScreenNotifier2D`/`VisibleOnScreenEnabler2D` to the component.
