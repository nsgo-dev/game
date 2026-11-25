# Agent Note: Editor plugin 3D viewport tools and shared 3D component gizmos

Status: implemented

## Problem

Editor plugins (`editor.Plugin`, added in #492 and extended with 2D drag/hover hooks in [2026-11-25-editor-shape-handles.md](./2026-11-25-editor-shape-handles.md)) could only add 2D viewport tools: in 3D view, plugin tools left the toolbar, could not draw overlays, and never received presses or camera rays. Games could not ship custom 3D tools (such as spline or spawn-volume tools) or interactive 3D handles for their components, and the editor's built-in 3D transform gizmo and 3D physics colliders had no shared 3D handle drawing/picking layer (#898).

## Decision

### 3D viewport hooks on `PluginTool`

`PluginTool` gains optional 3D hooks alongside its 2D hooks: `Press3D(ed, ray math3d.Ray)`, `Draw3D(ed, d, proj Projection3D)`, `OnDragBegin3D(ed, ray) bool`, `OnDragMove3D(ed, ray)`, `OnDragEnd3D(ed, ray)`, `OnDragCancel3D(ed)`, and `OnHover3D(ed, ray, active)`.

- `Projection3D` exposes the 3D camera, viewport dimensions, `ViewProj` matrix, `Project`, `ProjectSegment`, `ScreenRay`, `GizmoLength`, and drawing helpers (`DrawPoint`, `DrawLine`, `DrawBox`).
- `RegisterPlugin` requires at least one of `Press`, `OnDragBegin`, `Press3D`, or `OnDragBegin3D`.
- `pluginToolsView` filters tools by mode (`supports2D()` in 2D, `supports3D()` in 3D), so 2D-only tools remain absent and idle in 3D while 3D-only tools never widen the 2D `editor-toolbar`. Activating a plugin tool from the Command Palette or desktop menu bar switches view mode only when the tool supports exclusively 2D or exclusively 3D.

### Shared 3D handle engine and per-component 3D gizmos

`packages/game/editor/gizmo3d.go` introduces `GizmoHandle3D` (`GizmoHandlePoint3D`, `GizmoHandleLine3D`, `GizmoHandleBox3D`, optional `DisplayOnly`) and `ComponentGizmo3D` registered via `Plugin.Gizmos3D`:

- **Single undo step via preview-capture transaction**: `History.beginPreview`, `endPreview`, and `commitPreview` let `UpdateComponentGizmoDrag3D` roll back the previous frame's preview commands before invoking `ComponentGizmo3D.Drag(ed, drag)`. Inside `Drag`, plugins read pre-drag values via `Editor.ComponentField` and write fields naturally through `Editor.SetComponentField` or `Editor.History.Execute`. Each move previews live, `Escape` or any state transition rolls back cleanly with zero history entries, and `EndComponentGizmoDrag3D` commits all edits made during the drag as one undo step.
- **Object-space transforms and drag math**: Every handle is declared in object space and transformed through the full ancestor chain (`objectWorldMat3D`), while `GizmoDrag3D` supplies both world-space and object-space pick rays plus `AxisDelta`, `PlanePoint`, `PlaneDelta`, and `ViewPlaneDelta` helpers.
- **Shared built-in handles**: The editor's 3D transform gizmo (`Translate`, `Rotate`, `Scale` in `viewport3d.go`) builds `GizmoHandle3D` primitives and delegates hit-testing and rendering to `pickGizmoHandles3D` and `drawGizmoHandles3D`. Built-in 3D physics components (`collider3d` box/sphere/capsule and `trigger3d`) register directly as `ComponentGizmo3D` instances through the same API, with active plugins in `editor.plugins` order able to override them.

## Alternatives considered

### Requiring plugins to return raw field maps from `Drag` instead of calling `Editor`

Returning `map[string]any` patches from `Drag` would avoid adding preview capture to `History`, but would prevent plugins from using `Editor.SetComponentField` validation/normalization or custom `Editor.History` commands (for example editing multiple fields or related scene state atomically) and would diverge from the plugin contract documented in #492. Intercepting commands with `beginPreview` / `commitPreview` lets plugins use the exact same `Editor` mutation APIs they already use in panels and inspectors while guaranteeing single-step Undo/Redo and clean `Escape` rollback.

### Separate 3D tool type (`PluginTool3D`) instead of extending `PluginTool`

A separate `Plugin.Tools3D` slice would split tools that work in both 2D and 3D across two registrations and duplicate toolbar/palette/menu plumbing. Extending `PluginTool` with 3D hooks and filtering by `supports2D()` / `supports3D()` keeps dual-mode tools unified while preserving exact 2D-only behavior in 3D.

## Consequences

- Plugins can ship custom 3D tools receiving camera pick rays and drawing projected 3D overlays, as well as object-space 3D component gizmos whose handle drags commit as one undo step.
- Built-in `collider3d` and `trigger3d` components gain interactive 3D resize handles for box faces, sphere radii, and capsule radii/heights through the same `ComponentGizmo3D` API, and the built-in 3D transform gizmo shares the same projection/picking/drawing primitives.
- `ComponentField` falls back to `lookupComponentType` so built-in components also resolve registered defaults without an explicit `SetComponentRegistry`.
- Verified by `packages/game/editor/plugin_3d_test.go` alongside the full `packages/game/editor` suite, `docs/en/editor.md`, and `docs/zh/editor.md`.
