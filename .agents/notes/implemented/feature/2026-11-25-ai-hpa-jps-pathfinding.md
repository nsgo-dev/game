# Agent Note: HPA* hierarchical pathfinding, Jump Point Search, and sector-limited flow fields

Status: implemented

## Problem

Large strategy and open-world tile maps (such as 1024×1024 grids) make flat per-cell `ai.AStar` searches and whole-grid `rts.FlowField` Dijkstra integrations too slow for interactive fixed-step game loops. `game/ai` previously offered only flat `AStar` and 2D polygon `NavMesh`, and `game/rts` could only integrate flow fields over the entire grid.

Adding large-map pathfinding must satisfy five constraints simultaneously:

1. Keep `game/ai` **Stable** (`TestStabilityTable` forbids importing Experimental packages such as `game/rts`).
2. Deliver at least an order-of-magnitude speedup on 1024×1024 cross-map searches over `ai.AStar` while keeping concrete refined paths valid, deterministic, and within a tight bound of optimal cost.
3. Update only affected clusters (and shared borders) when grid cells are blocked, unblocked, or re-weighted, without rebuilding the entire map.
4. Provide an exact optimal Jump Point Search (`JPS`) solver for uniform-cost 8-way grids supporting both `NoCornerCutting: true` and `false`.
5. Keep repeated searches and sector flow-field rebuilds allocation-free after buffer warm-up.

## Decision

Implement `HPA` in `packages/game/ai/hpa.go`, `JPS` in `packages/game/ai/jps.go`, and `SectorFlowField` in `packages/game/rts/sector_flowfield.go`, importing only the Go standard library in `game/ai` and keeping the `game/rts -> game/ai` dependency direction intact.

### 1. HPA* cluster graph and incremental dirty rebuilds

`HPA` partitions a square `Grid` (and `CostGrid` / `EdgeCostGrid`) into `ClusterSize × ClusterSize` clusters (default `16`) and treats shared cluster borders (`vBorders`, `hBorders`, plus diagonal `corners` when `Diagonal && AllowCornerCutting`) as first-class objects:

- **Entrance placement**: contiguous walkable border runs of length `<= MaxEntranceSpan` (default `6`) get one midpoint entrance (preferring the lowest-cost cell on weighted grids); longer runs get endpoint entrances (`segStart`, `segEnd`, plus an interior lowest-cost cell if strictly cheaper on weighted grids). Entrance nodes are deduplicated by `Cell` within each cluster and reference-counted (`refCount`) across border links so cluster corners shared by two borders are stored once and freed cleanly without dangling edges.
- **Directed intra/inter edges**: inter-cluster edges store directed forward/reverse costs; intra-cluster edges are computed via bounded cluster-local Dijkstra (symmetric early-stop for uniform grids, directed per-entrance for `CostGrid`/`EdgeCostGrid`).
- **Incremental dirty invalidation**: `UpdateCell` and `UpdateRect` enqueue only dirty clusters and dirty shared borders. Interior cell edits dirty only 1 cluster (recomputing intra-cluster edges without touching any border or neighbor cluster); non-corner border edits dirty 1 shared border and 2 clusters; corner border edits dirty the touched borders and 3–4 clusters. `Find`/`FindAbstract`/`FindClusters` flush pending dirty clusters automatically via `RebuildDirty`.
- **Zero-mutation abstract search and deterministic tie-breaking**: start and goal connect via two temporary virtual node IDs (`len(nodes)` and `len(nodes)+1`) using one forward local Dijkstra from `start` and one reverse local Dijkstra from `goal` (recording entrance-to-goal costs in a generation-stamped `goalEdgeGen`/`goalEdgeCost` array so the persistent graph is never mutated). Abstract A* orders its heap by `(f, h, rowMajorCellID, id)` so results remain deterministic regardless of freelist node-ID reuse. Concrete refinement runs cluster-local A* over L1-sized `clusterSize × clusterSize` generation-stamped buffers.

### 2. Iterative Jump Point Search (`JPS`)

`JPS` implements Harabor & Grastien's 8-way uniform-cost Jump Point Search with iterative `jumpHoriz`, `jumpVert`, and `jumpDiag` loops (avoiding recursion depth limits on huge open maps) and a direct `*BoolGrid` fast path:

- Under `NoCornerCutting: true` (the `NewJPS` default), diagonal moves require both adjacent cardinal cells to be walkable and produce zero diagonal forced neighbors, while cardinal moves check behind-side obstacles (`!walkable(x-dx, y±1) && walkable(x, y±1)`) and emit both the forced cardinal turn and (when the forward cell is walkable) the forced diagonal turn.
- Under `NoCornerCutting: false`, both cardinal and diagonal forced neighbors follow classic corner-cutting JPS pruning.
- `FindInto` expands the jump-point chain cell-by-cell into caller memory with zero heap allocations after warm-up; `FindJumpPointsInto` exposes the raw turning waypoints.

### 3. Sector-limited flow fields (`rts.SectorFlowField`)

`SectorFlowField` stores integration costs, step costs, and direction vectors only for active `sectorSize × sectorSize` sectors mapped through a compact `sectorIndex []int32` table (`-1` for inactive sectors). `BuildAlongHPA(hpa, start, goal, padding)` queries `hpa.FindClustersInto`, optionally expands the corridor by `padding` cluster rings, and runs Dijkstra + direction computation strictly inside those active sectors while exposing the same `Cost`, `Reachable`, `Direction`, `Sample`, `CellAt`, and `CellCenter` queries as `FlowField`.

## Alternatives considered

### Multi-level HPA* (2+ abstraction levels)

Building a recursive pyramid of super-clusters above level-1 clusters can shrink abstract graphs on multi-million-cell maps. Rejected because a single abstraction level with `ClusterSize = 32` already reduces a 1024×1024 map (1,048,576 cells) to 1,024 clusters (~4k–8k abstract nodes), delivering >150× speedup (~3 ms full refined path, ~0.6 ms abstract path vs ~460 ms `AStar`) while keeping incremental dirty rebuilds simple, predictable, and O(affected clusters) without cascading multi-level invalidation.

### Putting sector flow-field integration inside `rts.FlowField` with a full-grid dense array

Adding a `BuildSectors` method directly to `FlowField` while keeping its dense `cols × rows` slices would avoid a second type in `game/rts`, but resetting or indexing 1,048,576 cells on every build still incurs whole-map memory footprint (~20 MB per field) and poor cache locality. A sparse sector-indexed `SectorFlowField` keeps memory proportional to the active corridor and reuses buffers with zero allocations per rebuild.

### Integrating JPS as a boolean flag on `ai.AStar`

Adding `UseJPS bool` to `AStar` would avoid a new type, but `AStar` supports `CostGrid`, `EdgeCostGrid`, hex/staggered `GridTopology`, 4-way movement, and `MovementRange`, whereas JPS is mathematically valid only for 8-way uniform-cost square grids. A dedicated `JPS` struct makes those preconditions explicit in the type and keeps `AStar` free of silent fallback surprises.

## Consequences

- **Benefits**: `game/ai` stays Stable with stdlib-only additions; 1024×1024 cross-map `HPA.FindInto` is ~150× faster than `ai.AStar` (~780× for `FindAbstractInto`) with <1.15× suboptimality and 0 allocs/op; `UpdateCell`/`UpdateRect` rebuild only touched clusters; `JPS` matches `AStar` optimal cost across all randomized obstacle grids (both `NoCornerCutting` modes) with 0 allocs/op; `rts.SectorFlowField` builds flow fields restricted to HPA* cluster corridors with 0 allocs/op after warm-up.
- **Costs**: `HPA` incurs an upfront hierarchy build over the grid (~100 ms on 1024×1024) and stores cluster entrance graphs in memory; `HPA` paths are near-optimal rather than guaranteed globally optimal when start/goal lie far from cluster entrances on short paths; `HPA` and `JPS` target square grids (hex and staggered topologies continue to use `AStar`).

## Verification

- `packages/game/ai/hpa_test.go`: tests 4-way, 8-way (`AllowCornerCutting` false/true), same-cluster detour through neighboring cluster, diagonal corner-squeeze completeness, weighted `CostMap`, asymmetric one-way `EdgeCostGrid`, incremental dirty rebuild counters (interior = 1 cluster, non-corner border = 2 clusters, corner border = 3 clusters, partial wall detour, sealed wall unreachable, newly opened door), abstract/corridor queries, determinism, zero-allocation `FindInto`, edge cases, and a 1024×1024 speedup (>10×) + optimality bound (<=1.15×) test plus `BenchmarkAStar1024`, `BenchmarkHPA1024`, and `BenchmarkHPAAbstract1024`.
- `packages/game/ai/jps_test.go`: compares `JPS` against `AStar` across 300 randomized obstacle grids (densities 0%–45%, both `NoCornerCutting` settings) for exact optimal cost parity, plus `GridFunc`, corner cases, `FindJumpPoints`, determinism, zero-allocation `FindInto`, and `BenchmarkJPS1024`.
- `packages/game/rts/sector_flowfield_test.go`: verifies `BuildAlongHPA` descent to goal on weighted grids, inactive sector isolation, `Sample` blending, corridor padding, zero-allocation rebuilds, edge cases, and `BenchmarkSectorFlowField1024` vs `BenchmarkWholeFlowField1024`.
- Verified with `gofmt`, `go tool nsgo test ./ai/...`, `go tool nsgo test ./rts/...`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
