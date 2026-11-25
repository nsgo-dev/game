# Agent Note: Tiled 2D and 3D navigation meshes with runtime dynamic obstacles

Status: implemented

## Problem

Static `ai.NavMesh` (2D) and `world3d.NavMesh` (3D) previously kept all walkable polygons in a single flat slice and required rebuilding the entire mesh whenever doors, destructible walls, placed buildings, or streamed open-world chunks changed walkable space (nsgo-dev/game#826). Large streamed levels also needed navmesh tiles to load and unload in lockstep with `world3d.ChunkStreamer` without stalling frame budgets or invalidating unrelated paths across the map.

## Decision

Implement symmetric tiled navigation meshes with dynamic obstacle carving/marking, two-level spatial indexing, and per-tile path invalidation in `packages/game/ai` (2D, Stable) and `packages/game/world3d` (3D, Experimental), preserving the package stability boundary established in [spatial queries and zero-allocation path reuse](2026-11-25-ai-navmesh-queries.md) and adding baker bridges in `packages/game/navbake` and `packages/game/navbake3d` ([3D navigation mesh baking](2026-11-25-navbake-recast-baking.md)):

1. **Independent fixed-size tiles with exact border seams**:
   - `ai.TiledNavMesh` (`NavTileCoord{X, Y}`) and `world3d.TiledNavMesh` (`NavTileCoord = ChunkCoord{X, Z}`) store each tile independently in a `map[coord]*tileSlot` backed by reusable slot IDs and monotonic generation salts (`NavPolyRef{Tile, Poly, Salt}`).
   - Tile boundaries compute `min = origin + float64(coord)*tileSize` and `max = origin + float64(coord+1)*tileSize` in `float64` so adjacent tiles share identical seam coordinates. `BakeNavTile` is pure and goroutine-safe, clipping polygons to exact tile bounds (and preserving exact 3D supporting planes for sloped ramps and multi-level bridges).
   - Adding, replacing, rebuilding, or removing a tile unlinks and re-links border portals only with its up to four cardinal neighbors (using per-tile border polygon lists and exact 2D/3D shared-edge detection), never touching unrelated tiles.

2. **Dynamic obstacle carving and marking on touched tiles only**:
   - Dynamic obstacles (`NavBoxObstacle`, `NavOrientedBoxObstacle`, `NavCircleObstacle` / `NavCylinderObstacle`, `NavPolygonObstacle`) decompose into CCW convex cutters (using circumscribed polygons for circles/cylinders, ear-clipping for concave polygons, and optional outward `Margin` inflation).
   - Each tile keeps its uncarved base polygons; when an obstacle is added, updated, or removed (or batched via `Batch`), only the tiles whose bounds overlap the old or new obstacle footprint rebuild via iterative half-plane convex subtraction (`carveConvexPoly2D` / `carveConvexPolyXZ`) followed by lightweight co-area/coplanar convex merging.
   - `NavObstacleCarve` removes the obstacle footprint so agents detour around it and restores the exact original polygons on removal; `NavObstacleMark` keeps the clipped interior polygon with `NavFlagDisabled` (default) or custom `Area`/`Flags`. In 3D, obstacles check vertical `[minY, maxY]` overlap so ground-floor obstacles never carve bridges overhead.

3. **Two-level spatial index and funnel-aware sparse A\***:
   - `FindPolygon` and small-extent `NearestPoint` compute tile coordinates in O(1) and query only the relevant tile's flat pointer-free AABB BVH (`navBVHNode2D` / `navBVHNode3D`). Unbounded `NearestPolygon` and large-extent queries use branch-and-bound over a lazily rebuilt coarse tile BVH descending into per-tile BVHs, never scanning the whole mesh.
   - Pathfinding runs sparse portal-graph A\* keyed by `(slotID, portalIdx)` (touching only visited portals, with deterministic `(coord, poly)` heap tie-breaking independent of slot reuse order), tracks funnel apex state across uniform-cost coplanar portals so subdivided tile borders do not distort `f`-scores, and runs uniform-cost surface tightening + collinear simplification so paths across tile borders match an untiled `NavMesh` for the same geometry.

4. **Per-tile path invalidation and `world3d` chunk streaming**:
   - Every coordinate maintains a monotonic `tileVersions[coord]` counter (retained even after tile removal). `TiledNavPathResult.Tiles` stamps every crossed tile's version for O(crossed tiles) `IsPathValid` checks.
   - `WatchPath` indexes one-shot invalidation callbacks in `tileWatches[coord]` so rebuilding or unloading a tile notifies only paths that crossed that tile, in deterministic watch ID order; `AddTileListener` exposes global tile events.
   - `world3d.NavTileStreamer` wraps `ChunkStreamer[*NavTile]` with `ChunkSize == Mesh.TileSize()` (mirroring `TilemapStreamer`), baking tiles in `Loader` workers, installing them on `OnLoad` (automatically applying pre-existing active obstacles touching the loaded chunk), and removing them on `OnUnload`.
   - `navbake.Mesh.TiledNavMesh`, `navbake3d.Mesh.TiledNavMesh`, and `navbake3d.BakeTile` (grid-aligned erosion-bordered Recast tile bake) bridge offline and chunk-level bakers to `TiledNavMesh`.

## Alternatives considered

1. **Rebuild a flat global `NavMesh` behind the scenes on every tile or obstacle edit**:
   - Strongest argument: reuses `ai.NavMesh` / `world3d.NavMesh` queries and `NavAgent` verbatim with zero new pathfinding code.
   - Rejected because rebuilding a global flat polygon array, global BVH, and global portal graph is O(total polygons in the world), violating the single-tile desktop frame budget on large open worlds and breaking O(1) tile streaming.

2. **Rasterize obstacles onto a heightfield/voxel grid at runtime instead of analytic convex polygon carving**:
   - Strongest argument: closely mirrors Detour TileCache's compressed heightfield approach for arbitrary 3D geometry.
   - Rejected as the primary runtime obstacle mechanism because analytic half-plane convex subtraction operates directly on each tile's clipped walkable polygons in ~4.5 µs per tile (no voxel quantization error, works identically in 2D and 3D including sloped ramps and stacked floors, and restores exact original polygons when obstacles are removed), while `navbake3d.BakeTile` still provides full Recast voxel baking per chunk when level geometry itself changes.

3. **Standard portal-midpoint A\* without funnel-aware `f`-scoring or surface tightening**:
   - Strongest argument: simpler A\* transition matching untiled `NavMesh`.
   - Rejected because subdividing large open polygons into fixed-size tiles introduces 4-way interior tile-grid corners where greedy single-point portal estimates (`bestPortalPoint`) can pick a sub-optimal side of an interior vertex and produce kinks at tile corners, violating untiled path parity. Combining funnel-aware `f`-score comparisons with uniform-cost coplanar raycast tightening preserves exact path parity across tile borders.

## Consequences

- **Benefits**:
  - Rebuilding one tile of a 400-tile (1,600-polygon) mesh with a dynamic box obstacle runs in ~4.5 µs/op in both 2D and 3D (`BenchmarkTiledNavMeshRebuildSingleTile`, `BenchmarkTiledNavMesh3DRebuildSingleTile`), well within a 16.6 ms desktop frame budget.
  - `FindPolygon` on a 400-tile mesh runs in ~35 ns/op (2D) and ~48 ns/op (3D) with `0 B/op, 0 allocs/op`.
  - Paths across tile borders match untiled `ai.NavMesh` and `world3d.NavMesh` across flat rooms, pillars, L-corridors, borders/corners, and 3D sloped ramps; multi-level bridges stay isolated from ground-floor obstacles; `world3d.NavTileStreamer` streams navmesh tiles in lockstep with `ChunkStreamer`.
- **Costs**:
  - `TiledNavMesh` is a dedicated type alongside untiled `NavMesh` (carrying `NavPolyRef` rather than flat `int` indices so tile replacement never invalidates unrelated polygon handles), and 2D/3D implementations mirror each other across `game/ai` and `game/world3d` to preserve the Stable/Experimental dependency rule.
  - Highly fragmented tiles with dozens of overlapping curved obstacles increase per-tile polygon count before merging; `mergeTilePolys` bounds merge work to tiles with <= 256 polygons to keep worst-case rebuilds bounded.
- **Verification**:
  - Unit tests in `packages/game/ai/tiled_navmesh_test.go`, `packages/game/world3d/tiled_navmesh_test.go`, `packages/game/navbake/tiled_test.go`, and `packages/game/navbake3d/tiled_test.go` verify box obstacle detour and exact path restoration, cross-tile path parity against untiled meshes (2D and 3D ramps/pillars), multi-level vertical separation, circle/cylinder/concave-polygon obstacles, carve and mark modes, per-tile path invalidation and watchers, `Batch`, determinism independent of tile insertion order, synchronous and async `NavTileStreamer`, `navbake3d.BakeTile`, and single-tile rebuild budgets and benchmarks.
  - Checked with `go tool nsgo check` (desktop and `-target ios`), `go tool nsgo test`, `go run ./tools/changelog check`, and `bun run verify-notes`.
