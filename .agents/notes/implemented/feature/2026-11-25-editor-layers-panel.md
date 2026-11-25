# Agent Note: Editor Layers Panel and Undoable Layer Edits

Status: implemented

## Problem

The editor placed objects and painted tiles on `Editor.ActiveLayer`, but nothing in its UI chose another layer except the scene tree's layer rows, and no command created, removed, renamed, reordered, hid or re-spaced a layer, so a scene with a background, a tile layer and an actor layer could not be built in the editor (#862). Layers are named by more than the editor's own state: the scene's connections name the objects on them, an inheriting scene's `layerOverrides` and same-named layers tie them to a base scene, and draw order is the pair of `order` and slice position, so each edit has a contract to keep with the scene format.

## Decision

- **Commands**: `AddLayer`, `RemoveLayer`, `RenameLayer`, `MoveLayer`, `SetLayerVisible` and `SetLayerSpace` in `packages/game/editor/layers.go` are History commands that find their layer by name on every Do and Undo, as the object commands do. Each builds a clone of the document with the edit applied and refuses it unless `sceneformat.Validate` passes and, for a scene that instances or inherits other files, `sceneformat.Resolve` does (`checkLayerEdit`), so nothing unbuildable reaches History.
- **Remove**: `RemoveLayer` removes the layer with its objects and tilemap and the connections from or to its objects (the same `namesSubtree` rule as `DeleteObject`), and Undo reinserts the layer and each connection at its old index, so the saved file is byte for byte what it was.
- **Move**: `MoveLayer(name, index)` takes a position in the draw order `Layers` lists. `planLayerMove` keeps the layer's `order` when that still draws it there and otherwise takes the order of the neighbour it lands next to; it then places the layer in the slice right before the next layer or right after the previous one when their orders tie, since ties draw in slice order. No other layer's order or relative position changes. `FuzzMoveLayer` checks this for arbitrary orders with many ties, and that Undo restores the bytes.
- **Visibility**: hiding writes `visible: false`, showing writes the field unset, so a hide and show of a layer whose field was unset round-trips the file. An explicit `visible: true` is not kept by a show; Undo restores it.
- **Inherited layers**: a layer whose name is a layer of the resolved base scene or a key of `layerOverrides` belongs to the base for its name, order, space and visibility. Those edits on it, and adding or renaming to a base layer's name, are refused with an error pointing at `layerOverrides`, rather than written to `layerOverrides` from the panel. `Layers` reports `Inherited` and the effective space, order and visibility from `Built.Doc`. The base layer names are loaded through the Resolver once per revision (`inheritedLayers`).
- **Lock is session state**: `SetLayerLocked` is a map on the `Editor` like the object locks of the scene tree (`2026-11-25-editor-scene-tree.md`), cleared by `OpenYAML`, renamed with the layer and restored with it by Undo of a removal. `HitTest`, `Pick3D` and `viewportFrozen` skip locked layers, and `HandleCanvasPress` refuses Place, Paint, Erase and Fill on a locked active layer with a status message. API calls such as `PaintTile` are not blocked: the lock guards the viewport, not the document.
- **Active layer**: `afterMutate` keeps `ActiveLayer` naming a layer of the scene (`keepActiveLayer`), so undoing the addition of the active layer falls back to the first one; removing the active layer picks the layer that takes its slice position.
- **UI**: `LayersView` sits under the scene tree in one 220-point column. It is a flat `ui.Tree` with a `ui.Selection` kept equal to `ActiveLayer`, `dragdrop` rows for reordering, a `ui.ContextMenu` per row, and inline rename on double press. It binds no global key: `OnKey` gives a key to its newest binding only, and F2 already renames objects in the scene tree.

## Alternatives considered

### Write visibility and space of inherited layers to layerOverrides

This is what the format offers for changing a base layer, and it would let the panel's eye button work on every row. It was not done because the panel would then show and edit two places for one setting, and clearing an override to get back to the base's value needs its own UI; the refusal says where to make the change, and the scene stays as the author wrote it.

### Renumber every layer's order on a move

Rewriting all orders to 0, 10, 20... in draw order makes moves trivial to reason about, and some editors do it. It was rejected because every move would then rewrite lines of layers the author did not touch, which a review diff should not show, and the scene format's documented tie rule already lets a move change one layer.

### Store visibility in the session, like the lock

Hiding only in the viewport would keep the scene file untouched. The issue asks for the eye to write the layer's `visible` field, which the game reads; the session-only control is the lock.

## Consequences

- **Benefits**: a scene's layers can be built, ordered and toggled in the editor with every edit undoable and the file round-tripping; refused edits never reach History; the status bar shows the active layer and whether it is hidden or locked.
- **Costs**: in an inheriting scene the layers that extend the base cannot be moved, hidden or re-spaced from the panel, and a move of an own layer that the resolved draw order cannot honour is refused. Renaming a layer does not rewrite other scene files that inherit the open one and name it. `inheritedLayers` reads the base scene through the Resolver once per revision, which is a file read per edit for inheriting scenes.
