# Agent Note: Terrain chunk LOD with crack-free stitched seams

Status: implemented

## Problem

`world3d.Heightmap.BuildMesh` always builds full-resolution chunk geometry (vertex stride 1). On large terrains such as a 4096×4096 heightmap (4095×4095 cells = 33,538,050 triangles), even frustum-culled views submit millions of distant horizon triangles that mobile and web GPU budgets cannot draw at frame rate. Lowering per-chunk resolution with distance introduces cracks and T-junctions along borders where neighboring chunks select different LOD levels, and distance-based switching must avoid boundary thrashing, respect game-wide quality presets, and remain invariant under `FloatingOrigin` rebasing and `ChunkStreamer` load/unload cycles.

## Decision

Add chunked terrain LOD management and stitched LOD meshing in `packages/game/world3d/terrain_lod.go` while keeping `world3d` free of `render3d` dependencies (consistent with [voxel chunks and meshing](2026-11-24-voxel-chunks-and-meshing.md)) and preserving `Heightmap.BuildMesh` unchanged:

- **Power-of-two per-chunk vertex strides (`Heightmap.BuildMeshLOD`)**: LOD `k` samples grid coordinates at stride `1 << k` (`1, 2, 4, ...`) while always including the chunk's final cell offset (`nx`, `nz`). Because adjacent chunks share the exact cell span along their shared border, the coarser neighbor's border vertices are guaranteed to be an exact subset of the finer chunk's border vertices at indices `{m * ratio < N-1} ∪ {N-1}` for every power-of-two stride pair and every positive chunk span (including partial border chunks such as the 63-cell edge chunks of a 4096×4096 heightmap).
- **Trapezoidal border-strip stitching (zero cracks, zero T-junctions)**: When a chunk with at least 3×3 sampled vertices borders one or more strictly coarser neighbors, `BuildMeshLOD` partitions the chunk into an interior regular grid `[1..w-2] × [1..h-2]` and four independent trapezoidal border strips meeting along the four corner diagonals `(0,0)-(1,1)`, `(w-1,0)-(w-2,1)`, `(0,h-1)-(1,h-2)`, and `(w-1,h-1)-(w-2,h-2)`. Each border strip fans between the coarser neighbor's outer anchors and the inner ring vertices, making all four borders independent of corner combinations and emitting only referenced vertices with strict `+Y` counter-clockwise winding. Thin 2-vertex strips use a two-pointer ribbon triangulation between opposite borders.
- **Distance selection with hysteresis and quality preset bias (`TerrainLOD`)**: `NewTerrainLOD` precomputes `float64` per-chunk AABBs and geometric distance thresholds (default starting at `1.0 * ChunkWorldSize`, doubling per LOD level up to `MaxLOD`). Each `Update` evaluates nearest-point distance from the camera to each active chunk AABB with a configurable relative hysteresis deadband (default `0.10`, clamped to `[0, 0.5]`, disabled when negative). `TerrainLODBiasFor(quality.Level)` maps `quality.Low` to `+1.0`, `quality.Medium` to `+0.5`, and `quality.High` to `0.0`, scaling thresholds by `2^(-bias)` (`SetQualityLevel` / `SetLODBias` reset per-chunk hysteresis memory so preset changes take effect immediately).
- **Frustum culling, caching, `FloatingOrigin`, and `ChunkStreamer` integration**: `UpdateWithOrigin` converts the local camera to `WorldPos64`, computes LOD distances in `float64` world space (so floating-origin rebasing never triggers LOD pops or mesh rebuilds), transforms chunk AABBs into local space via `float64` subtraction before frustum culling, and emits a per-frame translation `Transform` alongside cached `*TerrainMesh` pointers and monotonic `Revision` counters. When `StreamManaged` is enabled, `ChunkStreamerConfig.OnLoad` / `OnUnload` toggle active chunks through `SetChunkActive`, freeing unloaded meshes and re-stitching adjacent seams automatically.

## Alternatives considered

- **Vertical skirts along chunk edges instead of stitched border triangulation** — Skirts avoid neighbor-dependent mesh rebuilds when an adjacent chunk changes LOD, so each chunk's mesh depends only on its own LOD. Rejected because skirts add extra perimeter triangles on every chunk, still leave visible shading/shadow-acne creases on steep slopes and silhouettes, and require tuning skirt drop depth to terrain steepness; trapezoidal edge stitching guarantees exact edge-for-edge manifold joining with zero T-junctions and zero normal seams at shared vertices, and steady-state neighbor-driven rebuilds only occur when a border crosses a LOD boundary.
- **Restricting neighboring chunk LOD deltas to `<= 1` via iterative relaxation** — Simplifies transition tables to 2:1 ratios only, as in classic quadtree terrain schemes. Rejected because trapezoid strip stitching naturally handles arbitrary power-of-two stride ratios (`2, 4, 8, ..., 64`) in the same code path with no extra pass, so custom aggressive distance tables or sparse streamed chunks can never crack.
- **Vertex-height snapping without re-triangulation** — Snapping intermediate fine boundary vertices onto the coarse neighbor's linear segment eliminates geometric height cracks while keeping regular grid indices. Rejected because leaving intermediate vertices on the coarse edge creates topological T-junctions that still produce 1-pixel rasterization pinholes on GPUs.

## Consequences

- **Benefits**:
  - Reduces a 4096×4096 heightmap (33,538,050 full-resolution triangles across 4,096 chunks) to ~51,916 visible triangles on `quality.Medium` (>640× reduction), fitting comfortably within mid-range mobile and web triangle budgets.
  - Steady-state `Update` over all 4,096 chunks incurs `0 B/op` and `0 allocs/op` (~2.86 ms/op even under `-race`, `BenchmarkTerrainLOD4096Update`); cold-building all 3,890 visible stitched meshes takes ~8.87 ms/op under `-race` (`BenchmarkTerrainLOD4096ColdBuild`).
  - Watertight manifold tests prove zero gaps, zero T-junctions, exact XZ area conservation, matching shared-vertex normals, and upward `+Y` winding across every `(lodA, lodB)` pair on all four edges for full power-of-two chunks, partial non-power-of-two border chunks, thin edge strips, and mixed 4-neighbor corners.
- **Costs and known limits**:
  - `NewTerrainLOD` scans all heightmap samples once up front to compute tight per-chunk Y bounds; games that mutate `Heightmap.Heights` in place must call `InvalidateChunk` or `InvalidateAll` to refresh bounds and cached meshes.
  - When a chunk's coarser neighbor changes LOD, the finer chunk's cached `TerrainMesh` is rebuilt (incrementing `Revision` and setting `Changed: true`) so the caller re-uploads that chunk's GPU buffers.

## Verification

Run from `/Users/e-hu/worktrees/agy-game-858`:

- `(cd packages/game && env -u GOROOT go tool nsgo test ./world3d/...)` — runs exhaustive `(lodA, lodB)` seam tests across North/South/East/West for full, partial, thin-strip, and mixed-corner chunks, hysteresis deadband tests, quality preset bias tests, frustum + `FloatingOrigin` + `ChunkStreamer` integration (including `0 allocs/op`), and 4096×4096 triangle budget assertions.
- `(cd packages/game && env -u GOROOT go tool nsgo test -bench=BenchmarkTerrainLOD ./world3d/...)` — verifies `BenchmarkTerrainLOD4096Update` (51,916 visible tris, `0 allocs/op`) and `BenchmarkTerrainLOD4096ColdBuild` (~8.87 ms/op under `-race`).
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
