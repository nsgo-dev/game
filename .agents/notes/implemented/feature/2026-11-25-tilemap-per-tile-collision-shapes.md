# Agent Note: Per-tile collision shapes, slopes and light occluders from tilemaps

Status: implemented

## Problem

Tiled's collision editor stores rectangles, polygons, ellipses and polylines on each tile, the way Godot's TileSet stores physics and occlusion polygons. `game/tilemap` dropped them, so a platformer on a tilemap with slope or half tiles had to hand-place `physics.Slope` segments, and lighting had to build one `lights2d` occluder per solid cell. Issue #846 asks for 45 and 22.5 degree slopes and half tiles walked with no hand-placed segments, a grid that physics takes directly, occluders derived from the same shapes that stay correct when tiles change at runtime, and flips and rotations honoured.

## Decision

- `tilemap.LoadTiled` reads each tile's `objectgroup` into `Tileset.Shapes` (`packages/game/tilemap/shapes.go`): rectangles and polygons as their corners, ellipses as `EllipseShapeSegments` points, object rotation applied, points and tile objects skipped. `TileShape.Flipped` and `Tilemap.TileShapes` apply a GID's flip bits with the same matrix `DrawLayer` uses. `tilemap` still imports neither `physics` nor `lights2d`.
- A new Experimental package `game/tileshape` owns the derived geometry. Its `View` satisfies `physics.TileGrid`. A tile whose shapes fill its cell is a whole cell. A tile with partial shapes reports neither `TileSolid` nor `TileOneWay` (unless `Options.ShapedSolid`), and its upward-facing and vertical edges become slopes (upward-facing only for one-way tiles). Polylines are slopes only: they have no solid side, so they never enter `Outline` or `Occluders` and never make a cell solid.
- Geometry is cached per 16 by 16 chunk. Edges shared by neighbouring cells and shapes are cut, and collinear edges are merged within a chunk.
- The `View` follows the map through the existing pull-based edit log (`Tilemap.TileRevision` / `TileChangesSince`, from [the tile minimap](2026-11-25-worldmap-tile-minimap.md)). This change extends that log: `SetCellFlags` and `ClearCellFlags` record their cell, `SetFlags` records a bulk edit, and calls that change nothing record nothing. There is no second, push-based notification API.
- Tiles larger than a cell are placed bottom-aligned, so their shapes reach into other cells. When it builds its grid, the `View` computes how many cells any tile's shapes reach past their cell under every flip. It widens the neighbourhood for edge cutting, the chunks it rebuilds around an edit, and `AppendSlopesIn`'s region by that reach.
- The `View` implements `physics.TileGridOrigin` from the map's `Origin`, and every segment, slope and box it returns is in world coordinates, so a level placed in a multi-level world collides and casts shadows where it is drawn.
- Unbounded maps (`Tilemap.Infinite`, or a sparse chunked layer) give a `View` with no cells; this is tested and documented.
- `physics.CharacterBody.Move` changes in two places that walking tile slopes needs. Before tile walls are tested, the character is lifted onto a walkable slope it is climbing, so the top of a slope meeting a solid cell is not taken for the cell's side. Right after leaving a slope, it snaps onto a floor tile within `SnapDist`, so walking off a slope's lower end does not leave the ground for a step. Walking off a ledge from flat ground is unchanged.

## Alternatives considered

### A push-based `Tilemap.OnTileChange` callback

The first implementation added `OnTileChange(fn) (stop func())`, which reports a layer and a rectangle for each edit. Its strongest argument is precision: the layer filter skips edits to layers a `View` does not read, and a `FillRect` reports one rectangle instead of one cell per tile. It was dropped because `TileChangesSince` already records the same edits for `worldmap`, and two parallel mechanisms drift apart. The sparse chunked writes (`PutChunk`, `RemoveChunk`, streaming) only fed the log, and a listener needs `Close` to unregister. Pulling from the log costs an edit to an unselected layer a rebuild of the chunks around it, which is cheap.

### Slopes inside `physics.CharacterBody` or `tilemap`

`CharacterBody` could read shapes from the grid itself, or `tilemap` could produce slopes. Either would make a Stable package depend on tile shapes or on `physics` and `lights2d`. A separate package keeps the stability boundaries and gives physics and lighting one shared derivation.

### Ceiling segments for the underside of partial shapes

Adding downward-facing segments to `CharacterBody` would make a half tile's underside stop a jump. That is a new collision primitive across `physics` with its own tests, beyond #846. `ShapedSolid` and whole tiles cover the cases where a ceiling matters, and the docs say so.

### Clipping shapes to their cell instead of widening the neighbourhood

Clipping would keep every edge inside one cell, but a tall tile's shape would no longer match its drawn image. Widening by the measured reach costs nothing on maps with no oversized tiles, where the reach is zero.

## Consequences

- Benefits: slope, 2:1, true 22.5 degree and half tiles drawn in Tiled are walkable with no hand-placed segments. One `View` serves `MoveAndSlide`, `CharacterBody`, the platformer controller, `physics.World` and `lights2d`. On a 256 by 256 map, a `SetTile` plus rebuild costs about 0.3 ms with no allocations.
- Costs: `SetFlags` now forces a full rebuild in every edit-log reader, including `worldmap.TileMinimap`. Editing a tileset's `Shapes` needs `View.InvalidateAll`, because `Invalidate` keeps the per-GID geometry. `Boxes` covers only rectangular shapes, since rigid bodies have no polygon shape. Infinite maps are not supported yet.
- Verification: `packages/game/tileshape/tileshape_test.go` walks `testdata/slopes.tmj` and a true 22.5 degree ramp both ways. It checks that the derived occluders cast the same shadows as per-cell rectangles and per-shape polygons, with flipped GIDs, before and after `SetTile` edits, and that a View updated edit by edit matches one built from scratch across chunk borders. It also covers tall tiles across a chunk border, polylines, a moved map `Origin` and unbounded maps. `packages/game/physics/character_test.go` has regression tests for the slope-top lift and the lower-end snap. `packages/game/tilemap/shapes_test.go` checks that a `.tmx` map reads the same tile shapes as the equivalent `.tmj` map.
