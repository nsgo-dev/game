# Agent Note: Editor tile palette and the active tile it keeps

Status: implemented

## Problem

The editor's Paint and Fill tools write `Editor.ActiveTileID`, but nothing in the UI let a person choose it, and nothing kept it valid when the active layer changed (#874). A tile ID that the active layer's tileset does not have is not harmless: painting it writes a GID that `sceneformat.Build` cannot find (`tileset has no frame "N"`), so the scene stops building.

## Decision

`packages/game/editor/tilepalette.go` adds `Editor.TilePalette()` and the `TilePaletteView` panel, and `ActiveTileID` becomes state the editor keeps consistent with the active layer:

- **Tile IDs are canonical frame names.** A palette tile is a frame of the layer's tileset atlas whose name is a positive integer written without leading zeros. `sceneformat` looks a tile up by `strconv.Itoa(gid)` (`sceneformat/build.go`), so a frame named `"05"`, `"0"`, `"-1"` or `"corner"` can never be painted and must not be offered. The palette parses each name with `strconv.Atoi` and keeps it only when `strconv.Itoa` gives the name back. This is the rule for a single grid; a layered grid's palette lists the tiles of its YAML tileset instead, numbered from 1 ([YAML tilesets and layered tilemaps](2026-11-25-yaml-tilesets-and-layered-scene-tilemaps.md)).
- **The active tile follows the layer.** `keepTileInPalette` runs in `New`, `openYAML` and `SetActiveLayer`: when the new active layer's palette has tiles and none of them is `ActiveTileID`, the first tile in ID order is selected. When the palette has no tiles (a layer without an inline tilemap, an external Tiled map, an unresolved tileset), the ID is left alone, so moving to such a layer and back keeps the person's choice where the tileset still has it. Every place that changes the active layer or replaces the document must call it.
- **Picking a tile is view state.** `SetActiveTileID` does not go through `History`; only the paint that follows is an undoable command.
- **The palette is a `base.RadioGroup` of `base.Radio` tiles.** Exactly one tile is selected, which is what a radio group means, and NSGo gives it the accessibility role, the per-tile label ("Tile N") and arrow-key movement for free. Its arrow keys step through the tiles in ID order (Left/Up previous, Right/Down next), not by rows of the wrapped grid; the docs say so.
- **The status bar shows `Tile: N` only for Paint and Fill, and only when the palette lists N.** Erase writes no tile, and a layer without tiles keeps an ID the panel does not show.

### Layout invariant

NSGo lays a node's border and padding inside its box. The panel's tiles wrap in `paletteInner` (the 224pt panel less 10pt padding on each side and its 1pt left border), and each tile's ring (2pt) and padding (2pt) sit inside the tile's box, so a picture wider than `paletteTileLimit` is scaled down to it. `TestTilePaletteFitsAWideTileInThePanel` checks that a wide tile's box is no wider than its grid.

## Alternatives considered

### Clamp or reject the tile ID at paint time instead of following the layer

Validating `ActiveTileID` in `PaintTile`/`FloodFillTile` would catch every bad ID in one place. Rejected because it turns a layer switch into a paint that silently does nothing or errors, while the palette would show no tile selected; keeping the ID valid when the layer changes means the palette always shows what the next paint writes.

### A custom grid with row-wise arrow keys

A hand-built grid could move Up/Down by a row, as Tiled's and Godot's palettes do. Rejected for now: it needs its own focus, selection and accessibility handling that `base.RadioGroup` already provides, and the row length depends on the laid-out width. The ID-order behaviour is documented and tested so a later change is deliberate.

## Consequences

- **Benefit:** Paint and Fill always write a tile the active layer's tileset has, including right after `New` or `OpenYAML` of a scene whose first layer's tileset starts above 1.
- **Benefit:** The palette reuses NSGo's radio semantics, so screen readers and keyboard users get a labelled single-choice group.
- **Cost:** `SetActiveLayer`, `New` and `openYAML` now rewrite `ActiveTileID` as a side effect; code that sets the field directly and then changes layer must expect it to move.
- **Cost:** Down moves to the next tile rather than the tile below it.
