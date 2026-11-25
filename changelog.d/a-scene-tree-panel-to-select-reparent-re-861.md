### Added

- `game/editor`: a scene tree panel (`SceneTreeView`) lists the scene's layers and nested objects, selects them in step with the viewports, and reparents, reorders, renames and moves them between layers by drag or context menu, keeping their world transform; `ReparentObject`, `MoveObjectToLayer`, `ReorderObject`, `DropObject` and `SetObjectID` (which rewrites the connections that name the object) do the same as undoable commands, and per-row eye and lock toggles (`SetObjectHidden`, `SetObjectLocked`) hide or lock objects in the viewports for the session without touching the scene file (#861).

### Changed

- `game/editor`: `FindObject`, `SelectObject` and every object edit work on nested children, with transforms in the child's local space, and both viewports draw and pick nested children; `Objects3D` lists them with a new `Object3DRef.ParentID` (#861).
