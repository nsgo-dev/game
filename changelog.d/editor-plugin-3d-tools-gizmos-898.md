### Added

- `game/editor`: add 3D viewport hooks (`Press3D`, `Draw3D`, `OnDragBegin3D`/`OnDragMove3D`/`OnDragEnd3D`/`OnDragCancel3D`, `OnHover3D`, `Projection3D`, `PickRay3D`) to `PluginTool` and per-component 3D gizmos (`Plugin.Gizmos3D`, `ComponentGizmo3D`, `GizmoHandle3D`, `GizmoDrag3D`) with object-space point, line, and box handles that commit as a single undo step and share the same 3D handle layer with the editor's built-in 3D transform and `collider3d`/`trigger3d` handles (#898).
