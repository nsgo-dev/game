# Agent Note: Preserve stacked LDtk tiles and apply entity pivots in pkg/tilemap

Status: implemented

## Problem

`pkg/tilemap/ldtk.go` dropped data that LDtk projects routinely emit (`nsgo-dev/game#1583`):

- **Stacked tiles overwritten**: `buildLDtkTileData` wrote one GID per cell into a single dense `[]uint32` slice (`data[row*lw+col] = gid`). Auto-layer rules (ground plus edge/overlay tiles) and stacked `Tiles` layers (`gridTiles`) regularly emit multiple tile instances at the same `px`; every tile before the last one in that cell was overwritten and disappeared when drawn.
- **Entity pivot ignored**: `buildLDtkLevel` copied `ent.Px` directly into `Object.X` and `Object.Y` without reading `__pivot`. LDtk stores an entity's pivot point in `px`, whereas `ObjectRect` consumers (including `Object.PhysicsShape` and `ObjectLayer.PhysicsBodies`) treat `(X, Y)` as the top-left corner. Any entity with a non-top-left pivot such as LDtk's common bottom-center `(0.5, 1)` was shifted by `(pivotX*width, pivotY*height)`.

## Decision

Update `pkg/tilemap/ldtk.go` and `pkg/tilemap/tilemap.go` so stacked LDtk tiles and entity pivots map cleanly onto existing `Tilemap` APIs without breaking `Stable` callers:

- **Additional `TileLayer`s per stack depth**: `buildLDtkLevel` counts each tile-bearing layer's maximum per-cell occupancy (`countLDtkTileStackDepth`) and emits one `TileLayer` per stack depth in LDtk's array draw order: `<layer>` for depth 0, `<layer>#2` for depth 1, `<layer>#3` for depth 2, and so on, inheriting `OffsetX`, `OffsetY`, `ParallaxX`/`ParallaxY` (`1, 1`, `HasParallax: true`), `Opacity`, and `Visible`. Layers without stacked cells emit a single `<layer>` identically to before. This applies uniformly to `Tiles`, `AutoLayer`, and `IntGrid` layers carrying `autoLayerTiles`.
- **Upfront stacked-tile budget enforcement**: before allocating any dense `[]uint32` buffer, `buildLDtkLevel` counts stack depth with a sparse per-cell map sized to `len(tiles)` (not `lw*lh`) and charges `cells * depth` against `maxLDtkTotalTiles` (`64 << 20` cells), preserving the allocation-safety invariants from [bound LDtk layer dimensions and tile allocations](2026-11-27-tilemap-ldtk-layer-bounds.md).
- **Top-left entity conversion with `Object.PivotX`/`PivotY`**: `buildLDtkLevel` resolves an entity's normalized pivot from `entityInstance.__pivot`, falling back to `defs.entities` (`pivotX`/`pivotY` matched by `defUid`), or `(0, 0)` when both are absent. It computes `Object.X = px[0] - pivotX*width` and `Object.Y = px[1] - pivotY*height` so rects and `PhysicsShape`/`PhysicsBodies` align with the entity's visual bounds, and stores the normalized pivot in `Object.PivotX` and `Object.PivotY` (zero-defaulted to `(0, 0)` for top-left). Callers needing the original anchor recover `(o.X + o.PivotX*o.Width, o.Y + o.PivotY*o.Height)`.

## Alternatives considered

- **Add a per-cell overflow slice on `TileLayer` (for example `Overflow map[int][]uint32`)**: keeps one `TileLayer` per LDtk layer, including for layers with `Opacity < 1` where separate layers composite alpha independently rather than as a group. Rejected because `DrawLayer`, `CacheStaticLayer`, `AppendYSortTiles`, `TileAt`, `SetTile`, `FlagsAt`, `TilesInRect`, `Raycast`, and `EncodeTiled` would all need new stacked-cell code paths and an expanded `Stable` `TileLayer` surface; emitting `<layer>#2`, `<layer>#3`, … reuses every existing API, query, cache, and fuzzer invariant unchanged.
- **Store entity pivot in `Object.Properties` (for example `"pivot_x"` / `"pivot_y"` or `"__pivot"`)**: avoids adding fields to `Object`, but injects synthetic keys into `Object.Properties` (which otherwise contains only authored `fieldInstances`), risks colliding with user-defined entity fields, and forces string-keyed lookups. Adding `PivotX, PivotY float32` directly to `Object` has zero value `(0, 0)` matching top-left Tiled objects and avoids colliding with custom fields.

## Consequences

- **Benefits**: LDtk auto-layers, stacked `Tiles` layers, and `IntGrid` auto-layers draw every stacked tile in exact LDtk order across all existing draw, query, and caching paths; LDtk entities with any pivot produce accurate rects and `physics.Box` shapes out of the box while keeping the normalized anchor directly accessible; stacked-layer memory remains bounded by `maxLDtkTotalTiles` before allocation.
- **Costs**: a translucent (`opacity < 1`) LDtk layer with stacked cells composites each stack layer's alpha separately rather than into an offscreen group; callers inspecting `len(m.Layers)` on a map with stacked LDtk cells will see the emitted `<layer>#2`, … layers in draw order.
