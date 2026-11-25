# Agent Note: 2D and 3D navigation mesh spatial queries and zero-allocation path reuse

Status: implemented

## Problem

`ai.NavMesh` (2D) and `world3d.NavMesh` (3D) previously exposed only full-mesh linear scans for polygon lookups (`FindPolygon`, `NearestPolygon`) and allocating one-shot `FindPath` / `FindCorridor` searches. Games lacked Detour/Godot/Unity-style agent queries: snapping positions onto the mesh within bounded search extents (especially separating stacked floors in 3D), raycasting along the walkable surface with edge normals, sliding moving agents along boundaries without leaving the mesh, picking deterministic area-weighted random points (anywhere or reachable within a radius), and running zero-allocation path/corridor queries on large meshes.

## Decision

Implement symmetric spatial indexing, surface queries, deterministic random sampling, and reusable zero-allocation query buffers in `packages/game/ai/navmesh_query.go` (2D, Stable) and `packages/game/world3d/navmesh_query.go` (3D, Experimental), respecting the package stability boundary and sharing `ai.NavFilter` as established in [area costs and query filters](2026-11-25-ai-navmesh-areas-filters.md) and [2D and 3D navmesh path-corridor agents](2026-11-25-ai-navmesh-agents.md):

1. **Flat pointer-free AABB BVH spatial index**:
   - Both `ai.NavMesh` and `world3d.NavMesh` construct a flat slice-backed BVH (`bvhNodes`, `bvhOrder`, leaf size 4, max depth 56) during `BuildNavMeshWithAreas` using deterministic midpoint/centroid-median splits (breaking ties by polygon index).
   - Shared-edge portal linking during build, `FindPolygon` / `FindPolygonWithFilter`, `NearestPolygon` / `NearestPolygonWithFilter`, `NearestPoint` / `NearestPointWithFilter`, and pathfinding endpoint lookups traverse the BVH using fixed `[64]` stack arrays (zero heap allocations).
   - `NearestPoint` / `NearestPointWithFilter` use branch-and-bound BVH traversal pushing the closer child first; when a polygon overlaps the search box `[p - extent, p + extent]` while its unconstrained closest point falls outside a narrow extent axis (such as a sloped 3D ramp with tight `extent.Y`), the polygon is clipped against the extent AABB in stack buffers to find the exact closest point inside the box.

2. **Portal-walking `Raycast` and float64 `MoveAlongSurface`**:
   - `Raycast` / `RaycastWithFilter` walk shared-edge portals across allowed polygons (using a stack-allocated visited buffer and a local 32-poly corner bridge for shared-vertex grazing) and return `NavRaycastHit{Point, Normal, Poly, T, Hit}`. On corner hits where a portal edge ties with a boundary or disallowed edge, the obstructing non-portal edge normal is selected regardless of polygon vertex cyclic order. In 3D, only shared 3D polygon edges are crossed, so stacked floors and overpasses never falsely connect, and `Normal` is the unit 3D surface-tangent inward edge normal.
   - `MoveAlongSurface` / `MoveAlongSurfaceWithFilter` (and buffer-reusing `NavMeshQuery.MoveAlongSurfaceInto`) iteratively raycast and project remaining displacement onto the hit wall's tangent in `float64` (avoiding `float32` rounding drift on diagonal walls), requiring monotonic progress toward the target before clamping strictly onto the final polygon.

3. **FMA-free cross-platform deterministic `RandomPoint` and `RandomPointAround`**:
   - Both `RandomPoint` and `RandomPointAround` take an explicit `*random.Stream` and weight polygons by true surface area (exact 2D polygon area and exact 3D triangle-fan surface area, not XZ projection).
   - `RandomPointAround` runs a bounded BFS over connected allowed polygons intersecting the radius sphere/circle (never returning disconnected islands or unconnected stacked floors), clips each reachable polygon to the radius AABB, and performs exact area-weighted rejection sampling against the Euclidean radius with a deterministic convex binary-search fallback for degenerate slivers.
   - Area, clipping, distance, and barycentric interpolation wrap products in explicit `float64(...)` conversions (disabling platform-specific FMA contraction per the Go specification) and avoid transcendental functions so results are bit-identical across x86-64, arm64, and WebAssembly for the same seed.

4. **Reusable `NavMeshQuery` and zero-allocation `FindPathInto` / `FindCorridorInto`**:
   - `NavMeshQuery` (created via `NewNavMeshQuery(mesh)`) holds generation-stamped A\*, funnel/corridor, and BFS scratch slices (`seen`, `closed`, `goalMark`, `bfsVisited`), clearing full slice capacity on `uint32` generation wrap-around.
   - `FindPathInto`, `FindPathWithFilterInto`, `FindPathWithFilterAndOptionsInto`, and `FindCorridorInto` / `FindCorridorWithFilterInto` live on both `NavMeshQuery` (concurrency-safe when each goroutine owns its own `NavMeshQuery` over a shared read-only `NavMesh`) and `NavMesh` (delegating to an embedded `defaultQuery` for single-threaded ergonomics matching `ai.AStar.FindInto`).
   - `minFilterCost` computes the admissible heuristic lower bound in O(`len(filter.AreaCosts)`) instead of scanning all polygons per search.

## Alternatives considered

1. **Uniform spatial grid hash instead of a flat AABB BVH**:
   - Strongest argument: O(1) cell lookup on uniformly sized tiles with minimal traversal code.
   - Rejected because real navmeshes combine tiny doorway slivers with large open arena polygons and multi-story 3D stacks; choosing a single grid cell size either bloats memory with duplicate polygon references across many cells or degrades into linear scans inside huge cells. A flat AABB BVH adapts automatically to heterogeneous polygon sizes and stacked 3D levels with zero tuning parameters and pointer-free cache locality.

2. **Approximate `NearestPoint` by discarding polygons whose unconstrained closest point lies outside `extent`**:
   - Strongest argument: avoids AABB polygon clipping on the slow path.
   - Rejected because sloped 3D ramps and diagonal 2D polygons commonly intersect a narrow search box (for example tight `extent.Y` separating stacked floors) while their unconstrained orthogonal projection lies slightly outside the narrow axis, causing false misses. Fast-pathing the common in-box case and clipping only on boundary straddlers preserves speed while staying exact.

3. **Keep `FindPathInto` allocating per-query `allowed`/`polyCost` slices of length `len(polys)`**:
   - Strongest argument: reuses `NewNavMeshSearchWithFilter` directly with less code.
   - Rejected because issue `#827` requires zero allocations on repeated `FindPathInto` calls and O(log N) / local work on 10k-polygon meshes rather than an O(N) scan per query. Generation-stamped slices plus `minFilterCost` achieve `0 allocs/op` and touch only explored portals.

## Consequences

- **Benefits**:
  - Polygon lookup on a 10,000-polygon mesh drops from linear scans to sub-microsecond BVH lookups: `BenchmarkNavMesh2D10kFindPolygon` runs in `~887 ns/op` (`0 allocs/op`) vs `~130 µs/op` linear (~146× faster), and `BenchmarkNavMesh3D10kFindPolygon` runs in `~627 ns/op` (`0 allocs/op`) vs `~733 µs/op` linear (~1,168× faster).
  - Repeated `FindPathInto`, `FindCorridorInto`, `FindPolygon`, `NearestPoint`, `Raycast`, `NavMeshQuery.MoveAlongSurfaceInto`, and `NavMeshQuery.RandomPointAround` perform `0 B/op, 0 allocs/op` after warm-up in both 2D and 3D.
  - Stacked 3D floors, sloped ramps, dynamic area/flag filters, and seeded cross-platform determinism work uniformly across all queries.
- **Costs**:
  - `NavMesh` carries flat BVH arrays (~32–48 bytes per node plus one `int` per polygon) and `world3d.NavMesh` maintains a flat CSR portal index rebuilt on `AddOffMeshLink` / `SetEdgeFlags`.
  - 2D and 3D query implementations mirror each other across `packages/game/ai/navmesh_query.go` and `packages/game/world3d/navmesh_query.go` to preserve the `game/ai` Stable vs `game/world3d` Experimental package boundary.
- **Verification**:
  - `packages/game/ai/navmesh_query_test.go` and `packages/game/world3d/navmesh_query_test.go` cover extent-bounded `NearestPoint` (including narrow-extent clipping and 3D stacked floors), `Raycast` (holes, corners, ramps, stacked overpasses, filters), `MoveAlongSurface` (axis-aligned and 45-degree diagonal wall sliding, ramp elevation tracking), seeded `RandomPoint` / `RandomPointAround` (determinism, surface-area distribution, disconnected islands and stacked-floor reachability), `FindPathInto` / `FindCorridorInto` parity, `testing.AllocsPerRun == 0` assertions, concurrent goroutine queries, and 10k-polygon benchmarks.
  - Verified with `go tool nsgo test ./ai`, `go tool nsgo test ./world3d`, `go tool nsgo test . -- -run 'TestFormatsPage|TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
