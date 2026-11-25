# Agent Note: Editor Scene Tree and Nested-Object Editing

Status: implemented

## Problem

The editor (`packages/game/editor`) listed and edited only the top-level objects of each layer: `FindObject` searched `LayerDoc.Objects` alone, so a nested child (`ObjectDoc.Children`) could be drawn by `sceneformat.Build` but not selected, inspected, moved or renamed. Issue #861 asks for a scene tree panel, like Godot's Scene dock and Unity's Hierarchy, that selects, reparents, reorders and renames nested objects, and moving an object in the tree must leave it where it is in the world. Opening the whole tree to editing changes the contract of every edit command and of both viewports, and the world-transform rule has to be chosen for scenes whose objects carry 2D and 3D fields at once.

## Decision

- **Lookup contract**: `FindObject`, `SelectObject` and `DeleteObject` find an object by ID anywhere in the tree, and `FindObject`'s index is the index among the object's siblings. Every edit command goes through `FindObject`, so every edit and the inspector work on nested children, with the transform in the child's local space (its parent's space).
- **Moves**: `ReparentObject`, `MoveObjectToLayer`, `ReorderObject` and `DropObject` all go through `relocate` in `hierarchy.go`, which refuses a cycle, nesting past `sceneformat.MaxObjectDepth`, and any result `sceneformat.Validate` refuses on a cloned document, and records one `relocateCmd` holding both places and both transforms, so Undo writes back the exact fields, nil scales included.
- **World transform kept**: `keepWorldTransform` recomputes the local transform under the new parent in 2D (`x`, `y`, `rotation`, `scaleX`, `scaleY`) whenever it can. When the object or an old or new ancestor sets a 3D field, the depth (`posZ`, `scaleZ`) is kept too, read straight off the 3D matrix since no 3D rotation mixes it with x and y. When one of them sets a 3D rotation, the 2D view (which ignores `rotX`/`rotY`/`rotZ`) and the 3D view (which ignores `rotation`) no longer agree on where x and y lead, and the 3D world transform is kept in full. Decomposition puts a mirror back on the axis the object had negative before (`scaleX: -1` stays `scaleX: -1`), so a reparent does not churn the YAML.
- **Shear checks see ancestors**: `RotateObject`, `ScaleObject`, `ScaleObject3D` and a committed 3D gizmo drag check the whole tree from the object's top-level ancestor (`checkTransformEdit`) with `sceneformat.CheckObjectTree`, because a nested sprite is sheared by its ancestors' scales and rotations, not only by its own subtree.
- **IDs**: `SetObjectID` is one undoable step that rewrites the open scene's connections and override keys naming the object (and `old/...` paths into a scene it instances), the open clip's target and track targets, the selection and the session toggles. It does not rewrite other scene files.
- **Hide and lock belong to the session**: `SetObjectHidden` and `SetObjectLocked` are maps on the `Editor`, not scene fields and not History commands; `OpenYAML` clears them. A hidden or locked object (or one under such an ancestor) is neither picked nor manipulated in a viewport: no gizmo, no Move-tool drag. The scene tree and the inspector still edit it. Layer locks follow the same rule ([editor layers panel](2026-11-25-editor-layers-panel.md)).
- **Linear per-frame cost**: the 3D draw and pick walks hand each parent's composed `aff3` down the tree instead of looking up every object's ancestors, and the scene tree's rows look objects up through `lookupObject`, an index rebuilt once per editor revision.

## Alternatives considered

### Keep the local fields and let the object jump

Leaving `x`/`y`/`rotation` as written when the parent changes is what a text edit of the YAML does, and it needs no matrix code. It was rejected because the issue asks that an object keep its place, and both reference editors do so by default; an object that jumps on every drag makes the tree unusable for arranging a scene.

### Always keep the 3D world transform when any 3D field is set

The first implementation recomputed x and y from the 3D chain whenever a 3D field was present. It is simpler and keeps the 3D viewport exact, but a scene that only uses `posZ` for depth alongside a 2D `rotation` saw the object move in the 2D viewport, which is where such scenes are edited. Keeping 2D first and only the depth from 3D fixes that case; a full 3D keep is used only when 3D rotations make the two views disagree anyway.

### Store hidden and locked in the scene file

Unity keeps scene visibility and pickability in editor state, Godot keeps a node's `visible` in the scene. Writing them to the YAML would make them survive a reopen, but every toggle would then be a diff in review and an Undo step, and the scene format would carry editor-only fields the game must ignore. Session state keeps the scene file what the game reads.

### Project-wide ID rewrite

`SetObjectID` could rewrite every scene that instances or inherits the open one, as `MoveFile` rewrites file references. It was not done here because Undo would then have to restore files on disk; the docs state the limit and point to `game mv` and manual edits.

## Consequences

- **Benefits**: every edit, both viewports and the inspector work on nested children; tree moves keep objects in place and Undo restores fields byte for byte in the saved YAML; edits that would leave an unbuildable scene are refused before they reach History.
- **Costs**: in a scene that uses 3D rotations a tree move keeps the 3D placement and may move the object in the 2D view. Renaming an object that other scene files override by ID leaves those files pointing at the old ID. The `lookupObject` index is rebuilt on every revision, which is linear in the scene but paid even for edits that do not touch the tree.
