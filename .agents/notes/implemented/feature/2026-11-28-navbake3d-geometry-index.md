# Agent Note: Index geometry for incremental navmesh tile bakes

Status: implemented

## Problem

`BakeTile` scanned every triangle in a level to find its height range and the rasterizer then visited unrelated geometry. Large streamed levels therefore made a local tile rebuild scale with the whole level size (#1719).

## Decision

`Geometry` records each triangle's world-space bounds and indexes it in deterministic 32-unit XZ cells as triangles are added. Tile and bounded mesh bakes query the cells covering their padded bounds, deduplicate candidates, and sort triangle indices before rasterization. The existing append order, solid ranges, layer filtering, and bake output remain unchanged.

## Alternatives considered

- A lazily built BVH: rejected for this incremental change because Add methods would need invalidation and rebuild coordination, while the fixed grid updates in constant work per triangle.
- Scanning sorted triangles by X only: rejected because long XZ levels still visit unrelated rows and the index must cover both horizontal axes.

## Consequences

Local tile bakes avoid examining triangles in distant grid cells and preserve deterministic candidate order. Geometry keeps a small bounds entry and cell references per triangle, and large triangles occupy multiple cells.
