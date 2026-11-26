# Agent Note: Editor 3D transform gizmos in World and Local space with quaternion rotation, plane handles, uniform scale, and snapping

Status: implemented

## Problem

The editor's 3D transform gizmo (`packages/game/editor/viewport3d.go` and `gizmo3d.go`) previously constructed its own world-axis-only line segments and applied rotations by adding the dragged angle directly to `RotX`, `RotY`, or `RotZ`. Because `sceneformat` composes 3D rotations in extrinsic XYZ order (`Rz · Ry · Rx`), dragging the X or Y ring on an object already rotated about two axes—or nested under a rotated parent—turned it about an axis other than the ring drawn. There was no Local/World space toggle, no planar (XY/XZ/YZ) or screen-space center translate handles, no uniform scale center handle, no live drag delta readout in the status bar, and 3D gizmo shortcuts used `W`/`R`/`S` instead of `W`/`E`/`R` (#892).

## Decision

Reuse `render3d.Gizmo` directly as the single geometry and picking engine for the editor's 3D transform gizmos, operating in an oriented frame (`GizmoSpaceGlobal` or `GizmoSpaceLocal`):

- **Exact rotation about the drawn axis (`composeEulerRotation3D`)**: Every 3D rotation drag computes the signed angle around the drawn ring's axis `uWorld = orient * uLocal` in float64, composes `RWorldNew = RAxisAngle(uWorld, angle) * RParent * RStart`, removes the Gram-Schmidt orthonormalized parent rotation via `RLocalNew = RParentᵀ * RWorldNew`, and converts `RLocalNew` back to the scene's extrinsic Euler XYZ (`Rz · Ry · Rx`) representation. Zero-angle drags preserve the original Euler fields verbatim. Both single-object and multi-object drags use this exact path, and non-finite ancestor rotations are skipped safely.
- **Global / Local space orientation (`GizmoSpace3D`)**: Unified on `Editor.GizmoSpace3D` (`GizmoSpaceGlobal` default, `GizmoSpaceLocal`), `SetGizmoSpace3D` (guarded against Play mode), `ToggleGizmoSpace3D`, `GizmoOrientation3D`, `CmdGizmoSpace3D` (`"gizmo.3d.space"`, shortcut `t` in `ScopeViewport3D`), the 3D bottom bar `view3d-space-local`/`view3d-space-global` toggle, `View > 3D Gizmo`, and the status bar's ` · Global` / ` · Local` indicator. In Local mode, `gizmo3DAxes` / `gizmo3DOrientation` use Gram-Schmidt orthonormalization on the full ancestor-to-self chain (respecting active animation clip poses and non-uniform/mirrored parents), converting the orthonormal basis to `quat64` via `quat64FromMat3`.
- **Reusing `render3d.Gizmo` for plane, center, and uniform scale handles**: `render3d.Gizmo` gains a center screen-space translate cube/pick sphere (`AxisAll`), depth-aware plane-vs-center/shaft resolution in `pickTranslate`, ring-segment distance checks in `pickRotate`, and grazing-angle plane guards (`|dot(dir, normal)| >= 0.02`). The editor transforms screen rays into the gizmo's origin-centered oriented frame (`toGizmoRay3D`) and calls `render3d.Gizmo.PickAxis` and `AppendSegments`, while performing float64 constrained drag math with grazing-ray rejection (`minGizmoPlaneCos3D = 0.02`), finite displacement bounds (`maxGizmoJump3D`), and consistent `minScale2D` clamping across single- and multi-object drags. Visual fills and highlights use `Editor.SpecPalette` (`spec.TextPrimary` center ring and `spec.Warning.Fg` active highlight) so handles meet WCAG 2.2 3:1 contrast in both Spec Dark and Spec Light themes. `GizmoAxis3D` extends with `Axis3DXY`, `Axis3DXZ`, `Axis3DYZ`, and `Axis3DAll`. Uniform scale (`Axis3DAll`) projects view-plane drag onto camera right + up so dragging inward or outward scales all three axes proportionally from any click inside the center cube.
- **Snapping and modifier inversion**: Integrated with existing [editor snapping](./2026-11-25-editor-snapping.md) (`SnapSettings`, `SetSnapInvert`, `Ctrl`/`Cmd` inversion). In Global space (and screen-space `Axis3DAll`), translate snapping snaps active world coordinates to multiples of `EffectiveGridSize` (or `1` for pixel snap); in Local space on axis/plane handles, translate snapping snaps displacement components along the active local axes so movement never drifts off the drawn handle. Rotation snaps the drag angle to `EffectiveRotationSnapDeg` radians before composition; uniform scale snaps the uniform factor via `SnapScaleFactor` to preserve axis proportions.
- **Scoped `W`/`E`/`R` shortcuts and live status bar drag delta**: Updating `CmdGizmoRotate` to `e` and `CmdGizmoScale` to `r` in `ScopeViewport3D` (building on [editor shortcuts](./2026-11-25-editor-shortcuts-menu-palette.md)) gives standard `W`/`E`/`R` gizmo switching in 3D without conflicting with 2D `ToolEraseTile` (`e` in `ScopeTile`). `Editor.GizmoDragDelta3D()` formats live `dx`/`dy`/`dz`, degree `°`, or scale `x` deltas and renders them in `editor-status-message` during drags.

## Alternatives considered

### Keeping bespoke editor-only gizmo geometry and picking instead of reusing `render3d.Gizmo`

Maintaining a parallel gizmo segment builder and screen-space picker in `packages/game/editor/gizmo3d.go` would avoid touching `render3d.Gizmo`, but duplicates planar square bounds, cube handles, arrowheads, and ray-drag math across two packages and risks drift between what `render3d.Gizmo` draws and picks versus the editor. Reusing `render3d.Gizmo` via an origin-centered oriented frame (`toGizmoRay3D`) keeps one canonical implementation and improves `render3d.Gizmo` for downstream games too.

### Snapping each axis independently during uniform scale

Applying `SnapScaleFactor(base * factor)` independently to `ScaleX`, `ScaleY`, and `ScaleZ` on `Axis3DAll` would ensure each final axis lands on a multiple of `ScaleSnapStep`, but breaks the object's aspect ratio whenever its starting scales are not equal (for example `(2.0, 1.0, 0.5)` or `(1.1, 2.2, 3.3)`). Snapping the uniform `factor` preserves exact proportionality across all three axes while still landing on exact multiples of the step for unit or step-aligned scales.

### Keeping `W`/`R`/`S` instead of switching to `W`/`E`/`R`

Keeping `W`/`R`/`S` would avoid updating `commands_test.go`, but contradicts #892's explicit requirement for `W`/`E`/`R` and was only chosen in #920 because scoped shortcuts (`#1061`) did not yet exist to separate `ScopeViewport3D` from `ScopeTile`.

## Consequences

- Benefits:
  - Dragging any rotation ring rotates strictly about the exact axis drawn in both World and Local space, including objects with compound Euler rotations and nested children under rotated parents.
  - Authors gain planar translate handles, screen-space free translate, proportional uniform scale, World/Local orientation toggle, snapping with modifier inversion across all 3D gizmo modes, standard `W`/`E`/`R` shortcuts, and live drag deltas in the status bar.
  - Every gizmo drag commits as one undoable step and round-trips byte-for-byte through scene YAML.
- Costs:
  - `render3d.Gizmo.Segments(..., GizmoTranslate)` now emits 39 segments (adding the 12-edge center cube for `AxisAll`, still fitting inside the stack buffer with 0 allocations).
  - Clicking the exact origin of the Translate gizmo in orthographic axis views now picks `Axis3DAll` (the screen-space center handle) rather than the foreshortened axis pointing at the eye.
- Verification:
  - `packages/game/render3d/render3d_test.go` and `audit546_test.go` verify zero-allocation `AppendSegments`, planar handle alignment, `AxisAll` translate picking, and canvas draw counts.
  - `packages/game/editor/gizmo3d_transform_test.go` verifies quaternion rotation about drawn X/Y/Z rings on 2-axis rotated objects in World and Local space, nested children under rotated parents, XY/XZ/YZ plane handles, screen-space center translate, proportional uniform scale, snapping with modifier inversion, `W`/`E`/`R`/`T` scoped shortcuts, status bar readouts, single-step Undo/Redo, and YAML round-trips.
