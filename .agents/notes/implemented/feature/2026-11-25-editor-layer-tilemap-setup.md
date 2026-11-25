# Agent Note: Editor layer tilemap setup and version 3 tilemap offsets

Status: implemented

## Problem

The editor's tile tools (`PaintTile`, `EraseTile`, `FloodFillTile`, and drag strokes) could edit an inline tilemap only after the YAML scene file already declared one, and growing or shrinking a map or switching its tileset required editing `data` by hand (#877). In addition, `sceneformat.TilemapDoc` always anchored cell `(0, 0)` at layer origin `(0, 0)`, so growing a tilemap to the left or top could not keep existing painted tiles at their world coordinates without shifting every other object and camera in the scene.

## Decision

### Tilemap offsets in scene format version 3

`sceneformat.TilemapDoc` gains `OffsetX` and `OffsetY` (`offsetX`, `offsetY`, `float32`, omitted when `0`) as part of unreleased scene format version 3 (`scene.v3.json`), alongside [parallax and repeating layers](2026-11-25-sceneformat-parallax-layers.md):

- **Validation and schemas (`sceneformat/format.go`, `cmd/gameschema/main.go`)**: `offsetX` and `offsetY` are refused in version 1 and version 2 documents, must be finite, and cannot be set on a `ref` tilemap (the top-level entry in `tilemaps` owns the offset so moving between inline and `ref` is lossless and never double-applies an offset). `sceneV3Fields` excludes them from `scene.v1.json` and `scene.v2.json`.
- **Runtime and merge (`sceneformat/build.go`, `sceneformat/merge.go`)**: `buildTilemapBatch` offsets each tile sprite by `(OffsetX, OffsetY)`; layered and external maps set or shift `tilemap.Tilemap.Origin` by `(OffsetX, OffsetY)`, which `tilemap.Tilemap.Draw`, `WorldBounds`, and `tileshape.View` already honor. `sceneformat.Merge` merges `offsetX` and `offsetY` as independent scalar fields.
- **Editor viewport parity (`editor/viewport.go`, `editor/tilestroke.go`, `editor/view2d.go`)**: `CanvasToTile`, `tileAt`, `strokeSegment`, `drawTileGridOverlay`, `drawTileHoverOverlay`, and `FrameAll2D` all apply `(tm.OffsetX, tm.OffsetY)` so negative-coordinate cells added on the left or top edges remain grid-aligned, paintable, pickable, and framed.

### Undoable layer tilemap setup in the Layer Inspector

`packages/game/editor/tilemap_setup.go` and `tilemap_setup_ui.go` add undoable `History` commands and Layer Inspector controls under the **TILEMAP** section (also reachable from **Set Up Tilemap** in an empty `TilePaletteView` and **Inspect Layer** in the layers panel context menu):

- **Add / Remove (`AddLayerTilemap`, `RemoveLayerTilemap`)**: creates an empty single-grid inline tilemap with chosen tileset atlas, tile size, and grid size, refusing non-positive or overflowing dimensions (`> 1 << 24` cells), empty/unresolved atlases, layers that already have a tilemap, and inherited base-owned layers. `ActiveTileID` follows the new tileset via `keepTileInPalette` ([editor tile palette](2026-11-25-editor-tile-palette.md)). Removing a `ref` tilemap removes only the layer's reference so other layers sharing the top-level entry are unaffected.
- **Resize from any edge (`ResizeLayerTilemap`)**: accepts signed `(left, right, top, bottom)` cell deltas for both single and layered inline grids (direct or via `ref`). Existing cells shift by `(+left, +top)` inside the resized grid and `OffsetX`/`OffsetY` adjust by `(-left*TileWidth, -top*TileHeight)` so every surviving painted tile stays at its exact world position. Writing a non-zero offset on a version 1 or version 2 scene raises `SceneDoc.Version` to `CurrentVersion` (3), and Undo restores the exact previous cells, offset, and version.
- **Change tileset atlas and tile size (`SetLayerTilemapTileset`, `SetLayerTilemapTileSize`)**: validates the new atlas resolves and contains frames for every non-zero tile ID painted in `Data`, refusing missing frames with a status message before touching `History`.
- **Move between inline and top-level `ref` (`ExtractLayerTilemap`, `InlineLayerTilemap`)**: moves a layer's tilemap into `SceneDoc.Tilemaps[refName]` (refusing empty or duplicate keys) and replaces the layer's entry with `ref: refName`, or moves it back inline. Moving back inline removes the top-level entry from `SceneDoc.Tilemaps` only when no other layer or `layerOverrides` entry references it, so an extract followed by an inline round-trips the scene byte-for-byte while shared references stay intact.
- **Inspector remount invariant**: `inspectorSchemaKey` includes `layerTilemapSchemaTag` (`none`, `inline:single`, `ref:single`, etc.) so switching storage or adding/removing a tilemap remounts the controls while value edits (resizing, changing tileset, parallax) keep input focus. Unlike parallax and repeat controls, tilemap controls stay enabled on screen-space layers and are disabled only during Play mode or on inherited base-owned layers ([editor layers panel](2026-11-25-editor-layers-panel.md)).

## Alternatives considered

### Shift all scene objects and the camera when growing left or top instead of adding `offsetX`/`offsetY`

Keeping `TilemapDoc` unchanged and translating every object and camera by `(+left*TileWidth, +top*TileHeight)` would avoid a schema addition. Rejected because issue #877 specifically requires growing left and top to keep painted tiles at their *world* positions, translating objects pollutes diffs across unrelated layers and breaks coordinates referenced by scripts or other tilemap layers, and `tilemap.Tilemap` already supports `Origin`.

### Bump scene format to version 4 for tilemap offsets

Opening `scene.v4.json` would isolate `offsetX`/`offsetY` from parallax layers. Rejected because version 3 has not shipped yet (`changelog.d/sceneformat-parallax-layers-885.md` is still unreleased), matching the precedent set when layered tilemaps joined unreleased version 2 ([YAML tilesets and layered tilemaps](2026-11-25-yaml-tilesets-and-layered-scene-tilemaps.md)); version 1 and version 2 schemas remain immutable.

### Always delete the top-level `tilemaps` entry on `RemoveLayerTilemap` or `InlineLayerTilemap`

Deleting `doc.Tilemaps[ref]` unconditionally simplifies cleanup. Rejected because multiple layers or `layerOverrides` may reference the same top-level tilemap, so deleting a shared entry would invalidate the scene; `InlineLayerTilemap` removes the entry only when it has no remaining references, and `RemoveLayerTilemap` leaves top-level entries intact as `RemoveLayer` does.

## Consequences

- **Benefits**: authors can create, paint, resize from any edge (including left and top without shifting world coordinates), re-tileset, share via `ref`, inline, and remove tilemaps entirely in the editor with full Undo/Redo and byte-for-byte `Parse`/`Encode` round-trips.
- **Costs**: growing left or top on a version 1 or version 2 scene upgrades the saved file to `version: 3` while the offset is non-zero; `TilemapDoc` gains two fields (`offsetX`, `offsetY`) that `sceneformat.Build`, `Merge`, and editor viewport coordinate math must maintain.
