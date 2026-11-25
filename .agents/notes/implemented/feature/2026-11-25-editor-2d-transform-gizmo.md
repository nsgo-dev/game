# Agent Note: The editor's 2D transform gizmo edits in place and records one step per drag

Status: implemented

## Problem

In the 2D viewport the Move tool put the selected object where the pointer was pressed, and there were no handles for rotation or scale, so an object's rotation and scale could only be typed into the inspector (#863). Godot's and Unity's 2D editors drag objects with their grab offset kept and offer move, rotate and scale handles on the selection.

## Decision

`packages/game/editor/gizmo2d.go` adds a combined 2D gizmo shown on the selected object, nested children included, with the Select or Move tool, outside Play: a free-move square at the origin, X and Y arrows along the object's own (rotated) axes, a rotation ring outside the outline, and corner and edge scale handles on the outline. A drag on any object away from the gizmo selects it and moves it.

- **Edit in place, record at the end.** `BeginDrag2D` snapshots the object's 2D transform (`transform2D`: position, rotation and the two scale pointers), `UpdateDrag2D` writes the document fields directly and rebuilds, and `EndDrag2D` puts the snapshot back and executes one `transform2DCmd` holding before and after. This is the pattern the 3D gizmo already uses (`EndGizmoDrag3D`), so Undo and Redo replay exact values and the scale pointers stay nil where the file leaves them unset.
- **Everything is relative to the grab, in the parent's space.** The grab point is converted to the object's layer space and then, through the inverse of its ancestors' composed transform (`chain2(path[:len-1]).invert()`, as `HandleCanvasPress` does for the Move tool), to its parent's space once, at the start; each update converts the pointer the same way with the view as it is then. A nested child's X, Y, rotation and scale are written in its parent's space, so working there makes a child under a turned or scaled parent follow the pointer. A pan or zoom during a drag therefore keeps the grabbed point under the pointer, and under a rotated or zoomed view or camera a world-space object moves by the world distance the pointer travelled, a screen-space one by the virtual-screen distance.
- **Rotation is unwrapped.** The ring accumulates the signed step between successive pointer angles about the origin, so a drag past half a turn keeps turning rather than jumping by 2π.
- **Scaling is about the origin.** Each axis a handle moves scales by how much farther from the origin, along the object's own axis, the pointer is than the grab point was; Shift uses one factor for both axes (for a corner, the projection of the pointer onto the grab direction). A factor of 1 leaves the axis's pointer alone, so an edge handle never adds the other axis's field. A scale whose magnitude would be below `minScale2D` (0.001) is not taken, since an object collapsed to a point loses its handles and cannot be dragged back. Scales and rotations go through `checkTransformEdit`, which checks the whole tree from the top-level ancestor down, so the gizmo refuses what `ScaleObject` and `RotateObject` refuse: the object keeps its last valid transform and the status bar says why.
- **Frozen objects are left alone.** An object hidden or locked in the scene tree, or under one that is (`viewportFrozen`), has no gizmo and `BeginDrag2D` refuses it, as the 3D gizmo and the Move tool do.
- **Cancelling restores the snapshot.** Escape (`OnKey("escape")`), a cancelled pan, a cancelled touch and a second finger call `CancelDrag2D`. Undo, Redo, `PlayIn`, `SetTool`, `SetPluginTool`, `SetViewMode`, `Save`, `DeleteObject` and `DuplicateObject` cancel a drag first, since keyboard shortcuts can run them while the mouse drags, so no other step is recorded against a half-dragged object; `openYAML` drops it.
- **Gestures.** The viewport's 2D `OnPan` goes through `viewportGestures.pan`. A pan begins only past the platform's slop, so the drag starts at `Position - Translation`, where the pointer went down. Space held, a middle-button drag or a two-finger pan keep navigating as before. Shift comes from `core.KeyHeld`, since a pan event carries no modifiers.
- **Taps are unchanged.** `HandleCanvasPress` keeps its behavior, including the Move tool putting the selection where it is tapped; the gizmo acts only on drags.

### Handle layout

Handle sizes are fixed in canvas points (`gizmoCenterHalf`, `gizmoArrowLen`, `gizmoRingMin`, `gizmoScaleHalf` and the rest), so the gizmo stays usable at any zoom. Where handles overlap a press takes them in this order: the move square, scale handles, the arrows, the ring, then the object's body. Scale handles show only when both outline sides are at least `gizmoScaleMinSide` (24) points on the canvas; below that they would cover the move square and each other, and the author zooms in to scale a small object. A scale handle is also left out when, along an axis it scales, it sits less than `gizmoScaleMinReach` (12) canvas points from the origin: `originX` and `originY` default to 0, so a placed sprite's pivot is its top left corner, and its N, W, NW, NE and SW handles would lie on the origin's lines, where the ratio of the pointer's distance to the grab's is undefined or explodes.

This builds on the editor's own 2D view: [The editor's 2D view is separate from the scene camera](2026-11-25-editor-2d-view-independent-of-camera.md).

## Alternatives considered

### Separate Move, Rotate and Scale tool modes

Godot and Unity switch the gizmo with tool modes, which keeps each gizmo uncluttered. The editor's tool bar already has Select and Move and is crowded; a combined gizmo gives every operation from either tool without new modes, and its fixed pick order resolves overlaps. Modes can still be added later on top of `GizmoHandle2D`.

### Record a command on every pointer move and merge them

Executing a command per update and coalescing would show each intermediate state in History's machinery and needs merge support History does not have. Editing in place and recording once at the end is what the 3D gizmo does and what the issue asks for.

### Scale about the opposite corner or edge

Unity's rect tool keeps the opposite side fixed, which changes the object's position as it scales. Scaling about the origin, as Godot's scale mode does, changes only the scale fields and keeps the pivot the author set with `originX` and `originY`.

### Make a tap on a handle do nothing

A tap on the ring or an arrow could keep the selection rather than select or move as before. It would change the Move tool's tap-to-place behavior near the selection, which existing tests and authors rely on; the gizmo acts only on drags instead.

## Consequences

- Objects move, turn and scale directly in the viewport, each drag one undo step that restores the file exactly.
- Scale handles are hidden on objects smaller than 24 points on the canvas; the inspector or a zoom-in is needed there.
- A sprite pivoted at a corner offers only the three scale handles away from its pivot; the others are reached by moving the origin or with the inspector.
- nsgotest's `Finger.Cancel` (nsgo v0.3.0 `nsgotest/pointer.go:259`) sends no pan cancel for an active pan, unlike a real platform taking the touches, so the viewport also cancels the drag on a touch cancel; the pan-cancel path is tested by calling `viewportGestures.pan` directly.
- Tests: `gizmo2d_test.go` drives drags, Shift and Escape through the mounted editor with nsgotest, including a rotated, zoomed camera and a screen-space layer; `gizmo2d_internal_test.go` covers the pan phases and fuzzes `scaleFromDrag`; `BenchmarkUpdateDrag2D` measures an update.
