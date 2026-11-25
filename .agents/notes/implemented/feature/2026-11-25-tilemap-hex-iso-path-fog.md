# Agent Note: Hex, isometric and staggered pathfinding, movement range and fog of war

Status: implemented

## Problem

`game/tilemap` already projects and renders orthogonal, diamond isometric, staggered isometric and pointy-top/flat-top hexagonal maps (`Orientation`, `StaggerAxis`, `StaggerIndex`, `HexSideLength`, `HexNeighbors`, `HexDistance`), but `game/ai` (`AStar`) and `game/fog` assumed square 4/8-way adjacency and axis-aligned rectangular tiles. As a result, A* paths on hexagonal or staggered maps walked across non-adjacent cells with an inadmissible heuristic, tactics games lacked a bounded movement-range Dijkstra query, and fog of war neither computed hex/staggered line of sight nor aligned its drawn cells to hex or diamond tile polygons (#854).

## Decision

Extend `game/ai` and `game/fog` in place without breaking existing orthogonal defaults, preserving package stability (`game/ai` and `game/tilemap` are both Stable; `game/fog` and `game/gametest` are Experimental):

- **`game/ai` grid topologies (`packages/game/ai/topology.go`, minimal `Topology` hook in `packages/game/ai/astar.go`)**:
  - `GridTopology` enumerates `TopologySquare` (zero value, preserves `AStar.Diagonal` compatibility), `TopologySquare4`, `TopologySquare8`, four hex offset layouts (`TopologyHexPointyOdd`, `TopologyHexPointyEven`, `TopologyHexFlatOdd`, `TopologyHexFlatEven`), and four staggered isometric layouts (`TopologyStaggeredYOdd`, `TopologyStaggeredYEven`, `TopologyStaggeredXOdd`, `TopologyStaggeredXEven`).
  - `TopologyFromTilemap(*tilemap.Tilemap)` maps a tilemap's projection to its `GridTopology` directly (`ai` imports `tilemap`, both Stable, with no import cycle).
  - Hexagonal neighbor generation and cube-distance heuristics use the exact offset-to-axial formulas in `tilemap/projection.go`.
  - Staggered isometric maps transform offset coordinates bijectively onto their underlying orthogonal diamond lattice `(u, v)`, giving exact 4-way edge neighbors (Manhattan heuristic in `(u, v)`) and optional 8-way vertex-diagonal neighbors with `NoCornerCutting` (octile heuristic in `(u, v)`).
  - `EdgeCostGrid` adds optional `(from, to)` transition costs, respected by both `AStar.FindInto` (clamping positive costs below 1 to keep heuristics admissible) and movement-range searches (accepting any finite non-negative cost).
  - `AStar.MovementRange`, `AStar.MovementRangeInto`, `AStar.MovementRangeWithCost` and `ReachableCells` run Dijkstra using `AStar`'s reusable buffers (zero allocations per call with `MovementRangeInto`), returning `[]ReachableCell` ordered by ascending cost then row-major index. For generic graphs, `HexGrid` over `tilemap.Tilemap`, waypoint graphs, and range queries with per-node path reconstruction, see [generic graph search, hex tilemap adapter, and waypoint graphs](2026-11-25-ai-generic-graph-hex-waypoint-search.md).
- **`game/fog` hex/staggered visibility and projected drawing (`packages/game/fog/hex.go`, `fog.go`, `draw.go`, `snapshot.go`)**:
  - `Fog` carries `Orientation`, `StaggerAxis`, `StaggerIndex` and `HexSideLength` (zero values preserve orthogonal behavior; `NewFromTilemap` copies them automatically).
  - Hexagonal visibility uses `HexDistance <= Radius` on open ground (`hexDisc`) and 6-sextant symmetric shadowcasting with exact integer slopes in axial/cube coordinates (`hexShadowcast`), guaranteeing exact A-sees-B symmetry and bit-for-bit determinism independent of floating-point rounding.
  - Staggered isometric visibility maps `(col, row)` bijectively into diamond-lattice `(u, v)` coordinates and runs 4-quadrant symmetric integer-slope shadowcasting there.
  - `CellAt` delegates non-orthogonal world-to-cell queries to `tilemap.Tilemap.WorldToTile` through a stack-allocated projection header.
  - `Draw` keeps horizontal rectangle runs on orthogonal maps and batches closed 4-vertex diamond subpaths (isometric/staggered) or 6-vertex hex subpaths (pointy-top/flat-top hexagonal) into one filled path per state (`Unseen`, `Explored`) with tight camera frustum culling.
  - JSON snapshots record `orientation`, `staggerAxis`, `staggerIndex` and `hexSideLength` with `omitempty` (omitted for orthogonal grids so existing v1 snapshots are byte-for-byte unchanged).
- **`game/gametest` polygon path rasterization (`packages/game/gametest/golden.go`)**:
  - `RenderOps` rasterizes `DrawMoveTo`, `DrawLineTo` and `DrawClosePath` subpaths with a single-pass scanline winding fill so multi-tile polygon paths fill seamlessly without cracks or double-blended seams, enabling deterministic golden PNG verification for hex, isometric and staggered fog overlays.

## Alternatives considered

1. **Keep `ai` free of a `tilemap` import and pass an orientation struct**: avoids an import edge, but forces every caller to translate `tilemap.Orientation` / `StaggerAxis` / `StaggerIndex` manually and invites drift. Because `game/tilemap` is Stable and does not depend on `game/ai`, importing `tilemap` directly lets `TopologyFromTilemap(m)` stay one call and still satisfies `TestStabilityTable`.
2. **Hex line-of-sight via cube-lerp ray marching (`hexLine`)**: simpler to write, but cube rounding along half-way edges is asymmetric unless perturbed with floating-point epsilons and still misses grazing visibility compared to shadowcasting. 6-sextant symmetric integer-slope shadowcasting matches the package's existing orthogonal shadowcasting contract: pure integer arithmetic, proven symmetric (`A` sees `B` iff `B` sees `A`), and exact agreement with `hexDisc` on open ground.
3. **Bump `fog.FormatVersion` to 2 for projection fields**: unnecessary churn for orthogonal saves; using optional `omitempty` v1 fields preserves byte-identical orthogonal snapshots while validating unknown strings and negative `hexSideLength` with `ErrInvalidSnapshot`.
4. **Render hex/isometric fog via axis-aligned bounding boxes or pre-baked tile masks**: bounding boxes spill outside hex/diamond silhouettes and overlap neighboring tiles. Emitting exact tile polygons in one path per fog state matches `tilemap`'s world projection with zero overdraw and zero external textures.

## Consequences

- **Benefits**: Hex and staggered isometric strategy/tactics games get exact shortest paths (`len(path)-1 == HexDistance` on open hex grids), brute-force-verified movement-range queries with custom edge costs, symmetric lockstep-safe hex/staggered fog of war, and seam-free polygon fog rendering backed by golden PNG tests. All orthogonal APIs, draw op sequences, and snapshots remain 100% backward compatible, and `FindInto` / `MovementRangeInto` remain zero-allocation after warm-up.
- **Costs**: `game/ai` now imports `game/tilemap` (both Stable); `fog/draw.go` emits polygon subpaths rather than merged horizontal runs on non-orthogonal grids (one `MoveTo` + 4–6 `LineTo` + `ClosePath` per culled fogged tile, still only two `Fill` calls per frame); `gametest.RenderOps` carries ~140 lines of scanline polygon fill/stroke rasterization.
