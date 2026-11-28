# Agent Note: Index geometry for incremental navmesh tile bakes

Status: implemented

## Problem

`BakeTile` scanned every triangle in a level to find its height range and the rasterizer then visited unrelated geometry. Large streamed levels therefore made a local tile rebuild scale with the whole level size (#1719).

## Decision

`Geometry` records each triangle's world-space bounds and indexes it in deterministic 32-unit XZ cells as triangles are added. Tile and bounded mesh bakes query the cells covering their padded bounds, deduplicate candidates, and sort triangle indices before rasterization. The existing append order, solid ranges, layer filtering, and bake output remain unchanged: the candidate filter is the same 3D bounds test the rasterizer already applied, so a triangle it drops would have been rejected there.

A triangle whose bounds span more than 64 cells, such as one quad for a whole ground plane, is kept in a separate list checked by bounds on every query instead of being registered in each cell, so adding it stays constant work and memory. A query covering more cells than the grid holds, such as a whole-level `Bake` or a huge `tileSize`, scans the triangle bounds linearly instead of visiting empty cells. Cell coordinates are clamped to int32 so float32 coordinates near their limits cannot overflow the cell loop.

## Alternatives considered

- A lazily built BVH: rejected for this incremental change because Add methods would need invalidation and rebuild coordination, while the fixed grid updates in constant work per triangle.
- Scanning sorted triangles by X only: rejected because long XZ levels still visit unrelated rows and the index must cover both horizontal axes.
- Registering every triangle in all the cells it overlaps: rejected because a single large ground triangle would add millions of map entries.

## Consequences

Local tile bakes avoid examining triangles in distant grid cells and preserve deterministic candidate order. Geometry keeps a bounds entry and cell references per triangle. `TestBakeTileWithIndexMatchesFullScan` pins that an indexed tile bake equals the linear-scan bake. Removing or replacing a source, `TilesAffected`, the run-time rebaker queue, and the benchmark gate from #1719 are not part of this change.
