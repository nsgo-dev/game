# Agent Note: Generic graph search, hex tilemap adapter, and waypoint graphs

Status: implemented

## Problem

`game/ai` already supports square, staggered, and hexagonal grid pathfinding on `AStar` (`GridTopology` and `AStar.MovementRange`, see [hex/iso pathfinding and fog](2026-11-25-tilemap-hex-iso-path-fog.md)), but three gaps remained for tactics and non-grid games (#831):

1. `AStar` is coupled to 2D `(Col, Row)` rectangular `Grid` arrays, leaving games with waypoint networks, 3D cell grids, or arbitrary state graphs without a reusable-buffer A*/Dijkstra search.
2. `AStar.MovementRange` returns only `[]ReachableCell` (cell + cost) without preserving a self-contained shortest-path tree to reconstruct the path to each reachable cell for turn-based movement previews.
3. Callers using hexagonal `tilemap.Tilemap` projections had to build their own `GridFunc` wrapper around `FlagsAt` rather than passing a ready-made hex adapter using `HexNeighbors` and `HexDistance` into graph search.

## Decision

Add three focused, composable pieces inside stable `game/ai` (using only stable dependencies `game/tilemap` and `nsgo/core`):

- **Generic graph search (`packages/game/ai/graph.go`)**:
  - `Graph[N comparable]` uses `AppendNeighbors(dst []GraphEdge[N], node N) []GraphEdge[N]` so callers append outgoing edges into a searcher-owned scratch slice with zero allocations.
  - `HeuristicGraph[N]` adds `Heuristic(from, goal N) float64`; optional `ValidatedGraph[N]` (`ValidNode`) rejects out-of-bounds, blocked, or removed endpoints before search; optional `NodeOrder[N]` (`NodeLess`) supplies a domain-native deterministic tie-breaking order. `GraphFunc[N]` wraps ad-hoc callbacks.
  - `GraphSearch[N]` runs A* (`Find` / `FindInto`, `FindWithHeuristic` / `FindWithHeuristicInto`), Dijkstra (`FindDijkstra` / `FindDijkstraInto`), and budgeted range queries (`Range` / `RangeInto`) using dense node slices, an indexed binary min-heap with in-place decrease-key, a `clear`-reused `map[N]int32` index, and a reused neighbor buffer. Tie-breaking matches `AStar` (lowest `f`, lowest `h`, `Less`/`NodeOrder`, then discovery order).
  - `GraphRange[N]` stores reachable `(Node, Cost)` entries in pop order plus parent indices referencing the output slice itself (valid because Dijkstra closes every parent before its child), enabling `Contains`, `Cost`, `Parent`, `Path`, and zero-allocation `PathInto` without re-running search or retaining searcher state.
- **Hex tilemap adapter (`packages/game/ai/hex_graph.go`)**:
  - `HexGrid` (`NewHexGrid(m *tilemap.Tilemap)`) wraps `*tilemap.Tilemap` directly, calling `m.HexNeighbors` and `m.HexDistance` across all four `(StaggerAxis, StaggerIndex)` combinations, with configurable `SolidMask` (default `tilemap.TileSolid`), `Walk`, `Cost`, and directed `StepCost`.
  - Implements `HeuristicGraph[Cell]`, `ValidatedGraph[Cell]`, `NodeOrder[Cell]` (row-major order matching `AStar`), as well as `Grid`, `CostGrid`, and `EdgeCostGrid` so it works identically with both `GraphSearch[Cell]` and `AStar`.
- **Waypoint graph (`packages/game/ai/waypoint.go`)**:
  - `WaypointGraph` (`NewWaypointGraph()`) stores hand-placed 2D (`core.Point`) or 3D (`ai.Vec3`) nodes under stable integer IDs (`Remove` tombstones a slot and strips incident edges without renumbering), with `SetEnabled` toggling, `Connect`/`ConnectUndirected` (live Euclidean distance edges that stay accurate after `SetPos`), explicit `AddEdge`/`AddUndirectedEdge`, deterministic ascending-ID edge storage, `Nearest`/`Nearest3D`, and zero-allocation `PointsInto`/`Points3DInto` path conversion.

## Alternatives considered

1. **Integer-only graph interface (`Graph` over `int` node IDs with fixed `NodeCount()`)**: avoids a `map[N]int32` lookup in `GraphSearch`, but forces every caller with `Cell`, 3D voxel coordinates, or string/struct keys to build and maintain an external bidirectional ID table. Using `clear(s.nodeIndex)` on a Go map keeps repeated `FindInto`, `FindDijkstraInto`, and `RangeInto` calls at `0 allocs/op` while working directly on any comparable node type.
2. **Treat `cost == 0` in `WaypointGraph.AddEdge` as automatic Euclidean distance**: saves two methods, but makes it impossible to represent zero-cost edges (such as teleporters or instant transitions) and hides non-obvious magic behavior. Splitting `Connect`/`ConnectUndirected` (live Euclidean distance) from `AddEdge`/`AddUndirectedEdge` (literal non-negative weight) keeps both cases explicit.
3. **Add `LastMovementPath` onto `AStar` instead of returning `GraphRange[N]`**: mutates and invalidates previous range results on the next `AStar` call. Returning a self-contained `*GraphRange[N]` (with `RangeInto` reuse for zero-allocation hot loops) cleanly separates range queries from subsequent searches and works uniformly across hex grids, waypoints, and custom graphs.

## Consequences

- **Benefits**: Tactics games get an adapter directly over hexagonal `tilemap.Tilemap` (all four stagger configurations) with per-cell/per-edge range queries and instant path reconstruction; non-grid and 3D games get generic A*/Dijkstra/range search and a ready-made 2D/3D `WaypointGraph`. Repeated `FindInto`, `FindDijkstraInto`, `RangeInto`, and `PathInto` calls allocate `0` times after warm-up (`Find` allocates only the result slice), and `HexGrid` matches `AStar` path-for-path on every hex stagger layout.
- **Costs**: `GraphSearch[N]` uses a Go map (`map[N]int32`) per search instead of flat grid arrays, which has higher constant-factor overhead than `AStar` on dense rectangular grids; dense 2D grids should continue using `AStar` unless custom graph features or `GraphRange` paths are needed. `WaypointGraph` keeps tombstoned slots in its node slice after `Remove` to preserve ID stability.
