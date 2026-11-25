# Agent Note: Editor terrain painting with autotile rules and tileset editor

Status: implemented

## Problem

`game/tilemap` already parses terrain sets from `*.tileset.yaml` (`TilesetDoc.Terrains` and `TileDoc.Terrain`/`Neighbors`) and runs 4-bit cardinal and 8-bit corner-gated blob autotiling (`ApplyAutotile`, `SetAutotile`), but terrains had no explicit `default` fallback field apart from mask `0`, and `game/editor` could only paint individual tile IDs (#879). Authors could neither author terrain bitmasks visually in the editor nor paint/erase autotiled terrain blobs as single undo steps.

## Decision

### Shared terrain resolution and explicit default in `game/tilemap`

- `TerrainDoc` gains an optional `Default *int` (`default`, validated to `0..doc.Count()-1` in `ValidateTileset`, reflected in `apps/website/public/schemas/tileset.v1.json`). When set, `buildDocTerrains` assigns `firstGID + uint32(*Default)` to `Terrain.Autotile.Default`, allowing partial or filled-interior fallback tiles distinct from mask `0`. When omitted, the tile with empty `neighbors` (mask `0`) remains the default, preserving backward compatibility with existing version 1 tilesets.
- `TilesetDoc.Terrain(name, firstGID)` shares `buildDocTerrains` with `Tilemap.AddTilesetDoc`, and `Tilemap.ApplyTerrain` / `Tilemap.SetTerrainTile` expose named terrain operations directly on runtime maps.
- `ValidTerrainMask`, `ToggleTerrainMask`, `TerrainMaskNeighbors`, and `TerrainNeighborsMask` provide one canonical implementation of 4-bit (`cardinal`, 16 masks) and 8-bit corner-gated (`blob`, 47 masks) bitmask semantics. `ToggleTerrainMask` guarantees validity on every click: enabling a corner automatically enables its two adjacent cardinal edges, and disabling an edge automatically clears dependent corners.

### Single-undo terrain strokes in `game/editor`

- `ToolTerrain` (shortcut `t`, toolbar button `tool-terrain`, `View > Tools > Terrain`) paints with `TileStrokeTerrain` on primary drag and erases with `TileStrokeEraseTerrain` on secondary (right-button) drag, using `applyTerrainCell` over the layered tilemap grid (`tileGrid`).
- `applyTerrainCell` mirrors `Tilemap.SetAutotile` with a 5x5 post-edit membership snapshot (`terrain.Contains`) and `terrain.Autotile.ComputeMask` / `Tile`, so painting any blob in the editor produces identical tile IDs to runtime `ApplyAutotile` with the same terrain set.
- `tileStroke` merges multiple updates to the same cell index during a single stroke through `changeIndex` (`recordChange`), keeping the original pre-stroke `oldGID` while updating `newGID` as adjacent cells are crossed. At `EndTileStroke`, no-op net changes (`oldGID == newGID`) are dropped and the remaining changes (both painted/erased cells and all affected neighbors) are committed as one `tileEditCmd` in `History`. `CancelTileStroke` restores every touched cell back to its original `oldGID`.

### Tileset Editor with conflict-safe edge/corner clicking

- `TilesetEditorPanelView` (`tileset_editor.go`, `tileset_editor_ui.go`) opens any `*.tileset.yaml` from the active layer's tile palette (`Edit Tileset`), the file browser (`Edit Tileset`), disk (`OpenTilesetFile`), or YAML bytes (`OpenTilesetYAML`).
- Editing terrain sets, modes, defaults, and tile masks goes through `e.History` (`tilesetDocCmd`), validating every change with `tilemap.ValidateTileset`. While a tileset is open in the panel, `Editor.loadTilesetDoc` serves the live in-memory document to `TilePalette` and `ToolTerrain` so edited masks take effect immediately before saving.
- Clicking edges and corners in the 3x3 mask editor updates `editMask` via `ToggleTerrainMask`. If the resulting mask is free (or owned by the selected tile), it commits immediately as an undoable edit. If another tile in the same terrain already owns that intermediate mask (for example clicking `east` on the way to `east + south`), the editor leaves the other tile untouched and flags `TilesetMaskConflictTile` with a `Reassign` button so the user can either keep clicking toward another mask or explicitly reassign ownership in one undo step.

This builds on [YAML tilesets and layered tilemaps](2026-11-25-yaml-tilesets-and-layered-scene-tilemaps.md) and [Editor tile palette](2026-11-25-editor-tile-palette.md).

## Alternatives considered

### Separate `terrains[].tiles` map in YAML instead of `TileDoc.Terrain`/`Neighbors`

A dedicated mask-to-tile map inside `TerrainDoc` would place all masks under one block, matching `AutotileSet.Tiles`. Rejected because `TilesetDoc` (version 1, already shipped in #847) already represents terrain membership on `TileDoc.Terrain` and `TileDoc.Neighbors` with canonical direction words (`north`..`northwest`), avoiding duplicate representations of the same terrain rules across two fields in version 1.

### Silently stealing or blocking colliding masks on edge/corner clicks

Silently stealing an existing tile's mask on every edge click would destroy already-configured tiles whenever a user builds a multi-bit mask (clicking `east` before `south` would wipe the `east` tile). Refusing the click outright would make multi-bit masks unreachable once single-edge tiles exist. Tracking `editMask` plus `conflictTile` with immediate commit when free and an explicit `Reassign` action avoids both failure modes while keeping `TilesetDoc` valid at all times.

### Recording one undo command per cell crossed in a terrain stroke

Delegating each cell in a drag to `PaintTerrain` would reuse one code path, but would pollute `History` with dozens of steps per drag, breaking the acceptance requirement that one Undo restores a terrain stroke. Merging per-cell changes in `tileStroke` keeps the entire stroke (including neighbor updates) atomic in one undo step.

## Consequences

- **Benefits**: Authors can define 4-bit and 8-bit terrain sets with explicit defaults in `*.tileset.yaml`, edit masks visually by clicking edges and corners, and paint/erase terrain blobs with exact parity against runtime `ApplyAutotile` and single-step Undo/Redo.
- **Costs**: `Editor.loadTilesetDoc` and `Editor.liveResolver()` prefer the open tileset editor document when its path matches the active layer's `TilesetFile`, so unsaved tileset edits affect terrain painting, viewport previews and Play mode immediately until saved or closed (extended in [Editor tileset collision, animation, custom data and live tile preview](2026-11-25-editor-tileset-collision-animation-data.md)). Terrain painting targets YAML tileset grids (`TilesetFile`) rather than legacy single-atlas inline grids (`Tileset`).
- **Verification**: `packages/game/tilemap/tileset_doc_test.go` (`TestTilesetFormatConformance`, `TestTerrainDocDefaultAndRuntimeHelpers`, `TestTerrainMaskHelpers`), `packages/game/editor/terrain_test.go` (`TestTerrainBrushMatchesApplyAutotileBlobAndCardinal`, `TestTerrainEraseFixesNeighborsAndOneUndoRestoresStroke`, `TestTerrainStrokeCancelAndLockedLayer`, `TestTilesetEditorAssignsEdgesAndCornersAndSaves`), `packages/game/cmd/gameschema` (`-check`).
