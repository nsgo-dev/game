# Agent Note: Editor multi-object selection, marquee, and shared-field inspector

Status: implemented

## Problem

The scene editor tracked only one selected object ID (`Editor.SelectedID`), so Shift/Mod clicks replaced the selection, there was no 2D marquee box selection, moving or deleting several objects had to be repeated one object at a time, and editing a shared component field across multiple objects required editing each instance separately.

## Decision

The editor now maintains an ordered multi-object selection set (`selectedIDs []string`) alongside `Editor.SelectedID` as the primary object ID:

- **Backwards-compatible selection state** (`packages/game/editor/selection.go`, `state.go`): `Editor.SelectedID` remains the primary selected object ID for existing callers, plugins, and `editormcp`, while `SelectedIDs()`, `SelectionCount()`, `IsSelected(id)`, `SelectedObjects()`, `SelectObjects(ids, primary)`, `AddToSelection`, `RemoveFromSelection`, `ToggleSelection`, and `ClearSelection` manage the full set. `afterMutate`, `objectIDCmd.rename`, `placeObjectCmd`, and `deleteObjectCmd` keep the set pruned, renamed, and restored across Undo/Redo.
- **Viewport modifier clicks, 2D marquee, and Escape** (`marquee.go`, `viewport.go`, `viewport3d.go`): `HandleCanvasPressWith` and `HandleViewport3DPressWith` add on Shift and toggle on Mod (Cmd/Ctrl). Dragging on empty 2D canvas draws a marquee rectangle (`BeginMarquee2DWith`, `UpdateMarquee2D`, `EndMarquee2D`, `CancelMarquee2D`, `SelectMarquee2D`) that selects enclosed objects by default or touching objects when Alt is held; Shift adds and Mod toggles marquee matches. Escape cancels any active drag/marquee/stroke/visible tile selection first (without clearing selection), and clears selection when at rest under `KeyNotEditing` so inline text fields, command palette, modals, and connection picking keep their own Escape handling.
- **Scene tree synchronization** (`scenetree.go`): `SceneTreeView` switches `ui.NewSelection(cx, true)` to multi-select mode and bidirectionally syncs the selected object set and primary cursor between the tree and `Editor`, expanding ancestors of selected objects and showing a hint under the tree.
- **Common-center transforms and single-step History groups** (`gizmo2d.go`, `viewport3d.go`, `commands.go`, `view2d.go`): `topSelectedIDs()` filters out descendants whose ancestor is also selected so parent+child multi-selections never double-move or double-delete. 2D and 3D gizmos center on the combined bounding box of draggable selected objects; Move, Rotate, Scale, Delete (`DeleteSelected`), and Duplicate (`DuplicateSelected`) each commit through `History.Group` as one undo step. `FrameSelection2D` and `frameSelection3D` frame the combined bounds of all selected objects.
- **Multi-object inspector** (`inspector_multi.go`, `inspector.go`): When `SelectionCount() > 1`, `InspectorView` renders `"N selected"`, lists only the sections shared by every selected object (Transform, Sprite when all have sprites, intersection of Properties by key+kind, intersection of Components), displays `"Mixed"` placeholders (or indeterminate checkboxes) where values differ, and commits edits through `EditSelected` / `History.Group` so one Undo restores each object's own prior value.

## Alternatives considered

- **Replace `Editor.SelectedID` with a slice field**: Breaking `Editor.SelectedID` would force every external caller, plugin, test, and `editormcp` consumer to migrate at once. Keeping `SelectedID` as the primary object inside `selectedIDs` preserves 100% API compatibility while giving multi-select callers full set access.
- **Use `ui.NumberInput` for mixed numeric inspector fields**: `ui.NumberInput` formats a `float64` directly and has no placeholder state to distinguish a mixed field from `0`. Using `ui.Input` with `PlaceholderFunc` (`"Mixed"`) and commit-on-submit/blur keeps mixed values visually distinct and prevents accidental blur from clobbering differing values across objects.
- **Transform every selected ID independently without ancestor filtering**: Moving or deleting both a parent and its selected child would double-apply the offset to the child or fail on deleting an already-removed child. Filtering through `topSelectedIDs()` avoids both bugs cleanly.

## Consequences

- **Benefits**: Authors can marquee-select or Shift/Mod-select groups of objects in the 2D/3D viewport or scene tree, transform or delete them together in one undo step, and batch-edit shared transform, property, and component fields with exact per-object Undo restoration.
- **Costs**: Multi-select numeric fields use commit-on-Enter/blur text inputs rather than stepper buttons (`ui.NumberInput`), and 2D component-specific handles (lighting/shape handles) and 3D collider handles are suppressed while multiple objects are selected to keep the shared transform gizmo unambiguous.
- **Verification**: Covered by `packages/game/editor/selection_test.go` (marquee-selecting 5 objects and dragging in one undo step, setting a shared component field on 3 objects with different values and restoring each on one Undo, Shift/Mod clicks, enclose vs Alt-touch marquee, Escape priority, 2D/3D multi-rotate/scale/delete, parent+child filtering, and bidirectional scene tree multi-selection).
