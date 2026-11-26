# Agent Note: Nested children, hierarchical picking, and instance overrides in the 3D viewport

Status: implemented

## Problem

Scene objects nest up to 64 levels (`sceneformat.MaxObjectDepth`), and instanced or inherited scenes expand into children of the instancing object or merged layers in `Editor.Built.Doc`. Several 3D viewport paths still read `e.Doc` or `e.FindObject` instead of the resolved scene document, leaving instanced and inherited children without a 3D transform gizmo (`gizmo3DOrigin` returned false), missing their 3D light/camera/probe icons, physics debug wireframes, occluder fallbacks, and component gizmos. In addition, non-geometry box picking only compared entry ray distance `t` (so a scaled or enclosing parent box beat its nested child), and there was no way in the 3D viewport to step selection from a picked child up to its parent or owning scene instance root via a second press or modifier.

## Decision

- **Resolved-tree 3D traversal and exact `BuildNodes3D` composition**: all 3D viewport drawing, icon picking, occluder fallback, physics debug, component gizmo, and transform gizmo origin/drag paths read `e.resolvedDoc()` / `e.FindResolvedObject` / `e.resolvedObjectPath` instead of `e.Doc` / `e.FindObject` / `objectPath(e.Doc, ...)`. Non-geometry wireframe corners in `objectCorners3DUnder` compose `parent.mul(local3(o))` identically to `sceneformat.ObjectLocalTransform3D` and `sceneformat.BuildNodes3D`.
- **Deepest-hit picking with hierarchical step-up and `Alt`/Option modifier**:
  - `rayGeometry3D` breaks equal-distance triangle ties (within `1e-4`) in favor of the deeper object in `resolvedObjectPath`.
  - `Pick3D` collects ray-hit non-geometry boxes with their ancestor chain, picks the nearest hit (breaking ties on deeper nesting), and descends along hit ancestor-descendant chains as long as no strictly nearer unrelated object sits in front of the descendant.
  - `Select3DAtWithModifiers`, `HandleViewport3DPressWithModifiers`, and `HandleCanvasPressWithModifiers` add `alt bool` while preserving the existing `Select3DAt*` / `Handle*Press*` signatures. A plain second press on an already-selected child (or on a child whose ancestor is the sole selection) walks selection up one level to its parent—reaching the owning scene instance root and cycling back to the deepest hit from the top-level ancestor. `Alt`/Option+click jumps directly to the owning scene instance root (`ObjectOverrideInfo.InstanceID`) for an instanced child or the immediate parent for a local child, and composes with `Shift` (add) and `Mod` (toggle).
  - `HandleViewport3DPressWithModifiers` lets a hierarchical cycle press bypass the `PickGizmo3D` early-return guard so a second click near the selected child's origin is not absorbed by the gizmo shafts, while clicks on gizmo arms over empty space still preserve selection. `viewport3DGestures.pan` sets `suppressPress = true` when a transform gizmo or 3D handle drag ends so releasing a drag never triggers a press.
- **Parent-space gizmo drags and minimal single-step overrides**: `gizmo3DOrigin` and multi-selection gizmo drag targets (`drag3DTarget`) resolve objects via `FindResolvedObject` and snapshot override state per target. During drags of instanced or inherited objects, `applyTransform3DOverride` writes only the local transform fields that changed from drag start into the instance's `overrides` (or `SceneDoc.Overrides`), validates through `checkCandidateScene`, and commits as a single `objectOverrideCmd` (or single `History.Group` for multi-selection) without touching the source scene file.

This builds on [the GPU 3D viewport](2026-11-25-editor-3d-viewport-on-gpu.md) without changing the GPU upload or `lookKey3D` caching contract.

## Alternatives considered

- **Always pick the instance root on the first press, requiring double-click to enter the instance**: familiar from some group workflows, but inconsistent with the 2D viewport (`overrides_test.go` already picks instanced children directly) and with #894's requirement that a press picks the deepest object hit while a modifier or second press selects its parent or instance root.
- **Use `Shift` as the parent/instance-root pick modifier**: rejected because `Shift`-click already adds to multi-selection (`AddToSelection`) across both 2D and 3D viewports, whereas `Alt`/Option is free on 3D object clicks and already read via `ev.Modifiers.Alt || altHeld()`.
- **Drop all ancestors unconditionally whenever any descendant is hit in `Pick3D`**: simpler, but if an unrelated wall sits between a front parent marker and an offset rear child marker, dropping the front parent would pick the child behind the wall. Checking `h.t <= minUnrelatedT` keeps enclosing-parent picking accurate while respecting occlusion by unrelated objects.

## Consequences

- Benefits: nested objects up to 64 levels and children of instanced/inherited scenes render, pick, show gizmos, and drag accurately in parent space; editing instanced children writes minimal overrides that round-trip through YAML and undo in one step without modifying source scene files; both second-press cycling and `Alt`-click make parent/instance selection fast without leaving the 3D viewport.
- Costs: `Pick3D` allocates a small slice of hit candidates per raycast when hitting non-geometry boxes (typically a handful of entries) and clones ancestor slices only for hit boxes. Multi-select drags touching instanced objects re-resolve via `Rebuild` during live preview, matching single-object override drags.

## Testing

- `packages/game/editor/viewport3d_nested_test.go`: verifies `ObjectCorners3D`, `Scene3D()`, and `DrawViewport3D` against `sceneformat.BuildNodes3D` for a rotated, non-uniformly scaled parent and for 64-level nesting; verifies deepest-hit picking, second-press walk-up and wrap-around (including over the gizmo origin), and `Alt`-click parent/instance-root selection (including `Shift+Alt`); verifies single- and multi-selection 3D gizmo drags on a rotated/scaled scene instance's nested children write minimal `overrides`, leave the source scene file untouched, round-trip through YAML and a fresh `Editor`, cancel cleanly, and undo in one step.
