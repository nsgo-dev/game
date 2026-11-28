# pathfind

`game/pathfind` provides deterministic 2D pathfinding stepped cleanly on `game.Loop`: grid `AStar` pathfinding with line-of-sight smoothing and hexagonal or staggered topologies, `HPA` hierarchical pathfinding, `JPS` Jump Point Search, generic `GraphSearch` (A*, Dijkstra, and budgeted movement ranges with paths over hex tilemaps, waypoint graphs, or custom graphs), 2D `NavMesh` navigation with funnel smoothing, off-mesh links, polygon areas and filters, tiled meshes with dynamic obstacles, `NavAgent` path corridors, and bounded, partial, and incremental searches scheduled by a `PathQueue`. [navbake](navbake.md) bakes its navigation meshes from level geometry, and [ai](ai.md) steers agents along its paths and scores tactical positions by path distance.

```go
import "github.com/nsgo-dev/game/pkg/pathfind"
```

Nothing in `pathfind` uses background goroutines or hidden global randomness.

## Grid A* pathfinding

`AStar` searches any `Grid` or weighted `CostGrid` (`BoolGrid`, `CostMap`, or the adapter `GridFunc`), reusing its internal buffers across calls:

```go
grid := pathfind.GridFunc{
	Cols: levelMap.Width,
	Rows: levelMap.Height,
	Walk: func(col, row int) bool {
		return levelMap.FlagsAt(col, row)&tilemap.TileSolid == 0
	},
	Cost: func(col, row int) float64 {
		if isMud(col, row) {
			return 2.5
		}
		return 1
	},
}

astar := pathfind.NewAStar()
astar.Diagonal = true // NoCornerCutting is true by default

path, cost, ok := astar.Find(grid, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 18, Row: 10})
_, _ = path, cost
_ = ok
```

Use `astar.FindInto(dst, grid, start, goal)` to reuse a caller-owned `[]pathfind.Cell` buffer with zero allocations per search. `NewBoolGrid` and `NewCostMap` provide flat-slice grids when you do not already have a tilemap.

### Line-of-sight path smoothing and any-angle search

Tile-grid A* paths step from cell to cell and look jagged when followed with `FollowPath`. Convert a cell path into smooth world-space waypoints with `SmoothGridPath` or the buffer-reusing `SmoothGridPathInto`:

```go
opts := pathfind.GridPathOptions{
	Origin:   core.Point{X: 0, Y: 0},
	CellSize: 16,
	Radius:   6, // keep circular body radius clear of every blocked cell
}

rawCells, _, ok := astar.FindInto(cellBuf, grid, startCell, goalCell)
if ok {
	waypoints = pathfind.SmoothGridPathInto(waypoints, grid, rawCells, opts)
}
```

- **Radius-aware line of sight**: `SmoothGridPath` / `SmoothGridPathInto` drops intermediate cells whenever the straight segment between kept waypoints stays at least `Radius` away from every blocked cell (`!grid.Walkable(col, row)`). Set `CellWidth` / `CellHeight` instead of `CellSize` for rectangular tiles, and call `pathfind.GridLineOfSight(grid, a, b, opts)` or `opts.CellCenter(cell)` directly when needed.
- **Zero-allocation buffer reuse**: `SmoothGridPathInto(dst, grid, cells, opts)` writes into `dst[:0]` with `0` heap allocations once `dst` has enough capacity.
- **Any-angle (Theta*) search**: set `astar.AnyAngle = true` on square grids (`TopologySquare`, `TopologySquare4`, `TopologySquare8`) to relax neighbors directly from an ancestor cell whenever line of sight between cell centers crosses uniform-cost walkable cells (respecting `NoCornerCutting` and avoiding expensive `CostGrid` cells). When smoothing for a positive body `Radius`, prefer standard cell-by-cell `AStar` (`AnyAngle: false`) followed by `SmoothGridPathInto` so corner cells remain available when the body radius cannot clear a corner directly.

### Hexagonal and staggered topologies

Set `astar.Topology` to match any `tilemap.Tilemap` orientation via `pathfind.TopologyFromTilemap(m)` or pick a topology explicitly (`TopologySquare`, `TopologySquare4`, `TopologySquare8`, `TopologyHexPointyOdd`, `TopologyHexPointyEven`, `TopologyHexFlatOdd`, `TopologyHexFlatEven`, `TopologyStaggeredYOdd`, `TopologyStaggeredYEven`, `TopologyStaggeredXOdd`, `TopologyStaggeredXEven`):

```go
astar := pathfind.NewAStar()
astar.Topology = pathfind.TopologyFromTilemap(hexMap)

path, cost, ok := astar.Find(grid, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 6, Row: 4})
_, _, _ = path, cost, ok
```

- Hexagonal topologies use 6-way axial adjacency and exact cube-distance heuristics matching `tilemap.Tilemap.HexNeighbors` and `HexDistance`.
- Staggered isometric topologies map offset coordinates onto the underlying diamond lattice (`4-way` edge moves by default; set `Diagonal = true` for `8-way` vertex-diagonal moves with `NoCornerCutting`).
- Implement `EdgeCostGrid` (`EdgeCost(fromCol, fromRow, toCol, toRow) float64`) when transition costs depend on both source and destination cells (elevation steps, rivers, or zone of control); negative, `NaN`, or infinite values forbid an edge.
- Inspect a topology directly with `topo.Neighbors(c)`, `topo.NeighborsInto(dst, c)`, and `topo.Distance(a, b)`.

### Movement-range queries

For tactics games, compute every cell reachable from `start` within a movement-point budget using Dijkstra search under `astar.Topology`:

```go
reachable := astar.MovementRange(costMap, pathfind.Cell{Col: 2, Row: 2}, 4)
for _, rc := range reachable {
	// rc.Cell is reachable at minimum cost rc.Cost <= 4
	_ = rc
}
```

- Results are returned in deterministic order: ascending `Cost`, then row-major cell index.
- Use `astar.MovementRangeInto(dst, grid, start, budget)` for zero-allocation buffer reuse, or `astar.MovementRangeWithCost(dst, grid, start, budget, edgeCost)` / `pathfind.ReachableCells(grid, topo, start, budget, edgeCost)` to supply a custom `func(from, to pathfind.Cell) float64` callback (supporting sub-unit road costs as well as negative blocked edges).

## Hierarchical pathfinding (HPA*) and Jump Point Search (JPS)

For large strategy and open-world tile maps (such as 1024×1024), per-cell `AStar` becomes expensive. `game/pathfind` provides two complementary fast grid searchers that reuse all internal buffers with zero allocations on `FindInto`:

### HPA* (`HPA`)

`HPA` divides any `Grid`, `CostGrid`, or `EdgeCostGrid` into fixed-size square clusters (`ClusterSize`, default `16`), places entrance nodes along shared cluster borders, and precomputes intra-cluster shortest paths between entrances:

```go
hpa := pathfind.NewHPA(grid, pathfind.HPAOptions{
	ClusterSize: 32,
	Diagonal:    true, // AllowCornerCutting is false by default
})

path, cost, ok := hpa.Find(pathfind.Cell{Col: 2, Row: 2}, pathfind.Cell{Col: 1021, Row: 1021})
_, _, _ = path, cost, ok

// When obstacles or terrain costs change, mark only the modified cells or region:
grid.SetWalkable(400, 320, false)
hpa.UpdateCell(400, 320) // or hpa.UpdateRect(minC, minR, maxC, maxR)
hpa.RebuildDirty()       // optional: Find also flushes pending dirty clusters automatically
```

- **Performance & path quality**: on the 1024×1024 multi-room benchmark map (`BenchmarkHPA1024` vs `BenchmarkAStar1024`), `HPA.FindInto` is over two orders of magnitude faster than `AStar.FindInto` (~150× faster, ~780× faster for `FindAbstractInto`) while staying well within 1.15× of optimal A* cost.
- **Incremental updates**: `UpdateCell` and `UpdateRect` mark only the affected cluster (and adjacent cluster(s) when a cell lies on a shared cluster border) dirty. `RebuildDirty` rebuilds only those dirty borders and clusters instead of the whole map; inspect rebuild counts with `Stats()`.
- **Abstract & corridor queries**: use `FindAbstract` / `FindAbstractInto` for coarse entrance waypoints without cell-by-cell refinement, or `FindClusters` / `FindClustersInto` to obtain the traversed `[]ClusterID` corridor (used directly by `rts.SectorFlowField.BuildAlongHPA`).

### Jump Point Search (`JPS`)

`JPS` is a fast 8-way solver for uniform-cost square grids (`Grid`) that skips symmetric open-space cells while guaranteeing exact optimal octile path costs identical to `AStar` (`Diagonal: true`):

```go
jps := pathfind.NewJPS() // NoCornerCutting is true by default
path, cost, ok := jps.Find(grid, start, goal)
waypoints, _, _ := jps.FindJumpPoints(grid, start, goal) // turning points only
_, _, _ = path, cost, waypoints
```

- Use `jps.FindInto(dst, grid, start, goal)` or `jps.FindJumpPointsInto(dst, grid, start, goal)` for zero-allocation buffer reuse.
- `JPS` assumes uniform traversal cost `1` per walkable cell; use `AStar` or `HPA` for weighted `CostGrid` / `EdgeCostGrid` maps.

## Generic graph search, hex adapters, and waypoint graphs

When pathfinding over hexagonal tilemaps, hand-placed waypoints, 3D cell grids, or custom domain graphs, use `GraphSearch[N]` over any `Graph[N]` (`AppendNeighbors(dst []GraphEdge[N], node N) []GraphEdge[N]`) or `HeuristicGraph[N]` (adding `Heuristic(from, goal N) float64`):

- **A* and Dijkstra with buffer reuse**: `Find` / `FindInto`, `FindWithHeuristic` / `FindWithHeuristicInto`, and `FindDijkstra` / `FindDijkstraInto` reuse internal node, heap, map, and neighbor buffers across calls (`FindInto` and `FindDijkstraInto` allocate `0` times after warm-up; `Find` allocates only the returned path slice). Ties break deterministically like `AStar`: lowest `f`, lowest `h`, `GraphSearch.Less` (or optional `NodeOrder[N]`), then discovery order.
- **Range queries with reconstructed paths**: `Range(g, start, budget)` and buffer-reusing `RangeInto(dst, g, start, budget)` run Dijkstra within `budget` and return a `*GraphRange[N]` containing every reachable node and cost (`Nodes()`, `Contains(n)`, `Cost(n)`, `Parent(n)`) plus direct path reconstruction (`Path(n)` and zero-allocation `PathInto(dst, n)`).
- **Custom callback graphs**: wrap quick ad-hoc graphs with `GraphFunc[N]{Neighbors, Estimate, Valid, Less}` or implement optional `ValidatedGraph[N]` to reject invalid/blocked endpoints before searching.

### Hex grid adapter over `tilemap.Tilemap`

`NewHexGrid(m)` wraps any hexagonal `*tilemap.Tilemap` across all stagger axes (`StaggerX`, `StaggerY`) and indices (`StaggerOdd`, `StaggerEven`), delegating directly to `m.HexNeighbors` and `m.HexDistance`:

```go
hex := pathfind.NewHexGrid(hexMap)
hex.Cost = func(col, row int) float64 {
	if isForest(col, row) {
		return 2
	}
	return 1
}

search := pathfind.NewGraphSearch[pathfind.Cell]()
path, cost, ok := search.Find(hex, start, goal)

// Movement range for turn-based tactics, including path preview to any cell in range:
moveRange := search.Range(hex, start, 4)
if preview, reachable := moveRange.Path(hoveredCell); reachable {
	_, _, _, _ = path, cost, ok, preview
}
```

Set `SolidMask` (defaults to `tilemap.TileSolid`), `Walk`, `Cost`, or directed `StepCost` to customize passability and per-cell or per-edge terrain costs. `HexGrid` also satisfies `Grid`, `CostGrid`, and `EdgeCostGrid`.

### Waypoint graphs

`NewWaypointGraph()` stores hand-placed 2D (`Add`, `SetPos`, `Pos`, `Nearest`, `Points` / `PointsInto`) or 3D (`Add3D`, `SetPos3D`, `Pos3D`, `Nearest3D`, `Points3D` / `Points3DInto`) waypoints with stable integer IDs, live Euclidean-distance edges (`Connect`, `ConnectUndirected`), explicit weighted directed/undirected edges (`AddEdge`, `AddUndirectedEdge`), temporary node toggling (`SetEnabled`), and deletion (`Remove`):

```go
wg := pathfind.NewWaypointGraph()
a := wg.Add(core.Point{X: 0, Y: 0})
b := wg.Add(core.Point{X: 80, Y: 0})
c := wg.Add(core.Point{X: 80, Y: 60})
wg.ConnectUndirected(a, b) // Euclidean distance cost (80)
wg.ConnectUndirected(b, c) // Euclidean distance cost (60)
wg.AddEdge(a, c, 120)      // explicit directed cost

search := pathfind.NewGraphSearch[int]()
nodePath, totalCost, ok := search.Find(wg, a, c)
waypoints := wg.Points(nodePath) // ready for ai.FollowPath
_, _, _ = totalCost, ok, waypoints
```

`WaypointGraph.Heuristic` returns Euclidean distance between waypoints (admissible whenever every edge costs at least Euclidean distance); call `search.FindDijkstra` when a graph includes shortcut or portal edges cheaper than straight-line distance.
## 2D NavMesh with funnel smoothing

For open arenas or non-grid worlds, build a `NavMesh` from convex 2D polygons. `BuildNavMesh` normalizes polygon winding, automatically connects adjacent polygons along shared edges (including partial collinear overlaps), and `FindPath` runs portal-graph A* followed by Simple Stupid Funnel Algorithm string-pulling:

```go
mesh := pathfind.BuildNavMesh([][]core.Point{
	{{X: 0, Y: 0}, {X: 120, Y: 0}, {X: 120, Y: 80}, {X: 0, Y: 80}},
	{{X: 120, Y: 20}, {X: 240, Y: 20}, {X: 240, Y: 100}, {X: 120, Y: 100}},
})

waypoints, ok := mesh.FindPath(core.Point{X: 16, Y: 16}, core.Point{X: 220, Y: 85})
_ = waypoints
_ = ok
```

Inspect mesh geometry with `Contains(p)`, `FindPolygon(p)`, `NearestPolygon(p)`, `Neighbors(index)`, `Polygon(index)`, `PolygonCount()`, and `FindCorridor(start, goal)`. Register off-mesh connections with `AddOffMeshLink(NavOffMeshLink{...})`, inspect or clear them with `OffMeshLinks()` and `ClearOffMeshLinks()`, and flag shared edges with `SetEdgeFlags(fromPoly, toPoly, flags)`.

To cut a level into these polygons, with paths kept an agent's radius away from the walls, bake the mesh from its outline and obstacles or from a tilemap with [navbake](navbake.md).

### Off-mesh links (jumps, ladders, and teleports)

Connect polygons that do not share an edge — such as one-way ledge drops, gap jumps, bidirectional ladders, doors, or teleporters — with `AddOffMeshLink`:

```go
mesh.AddOffMeshLink(pathfind.NavOffMeshLink{
	Start:         core.Point{X: 90, Y: 40},
	End:           core.Point{X: 140, Y: 40},
	Radius:        4, // snap endpoints within 4 units of a polygon edge and trigger agent entry
	Bidirectional: false,
	Cost:          12, // <= 0 uses Euclidean distance
	Area:          pathfind.NavAreaJump,
	Flags:         uint32(pathfind.NavFlagJump),
	ID:            1,
	UserData:      "drop",
})

res := mesh.FindPathWithOptions(start, goal, pathfind.PathOptions{})
if res.Found {
	for i := 0; i+1 < len(res.Path); i++ {
		if link, ok := res.LinkAt(i); ok {
			// Segment Path[link.StartIndex] -> Path[link.EndIndex] traverses an off-mesh link.
			_ = link.UserData
		}
	}
}
```

- **Path segment metadata**: `NavPathResult.Links` lists every off-mesh link traversed in path order (`StartIndex`, `EndIndex == StartIndex + 1`, `FromPoly`, `ToPoly`, `Start`, `End`, `Radius`, `Area`, `Flags`, `LinkID`, `UserData`), with `HasLinks()` and `LinkAt(segmentIndex)` helpers. Collinear simplification never collapses link entry or exit points.
- **Shortcut-aware optimal search & filtering**: A\* treats off-mesh links as directed edges (or two directed edges when `Bidirectional` is true), filters them by `NavFilter` (`Area` and `Flags`, including `NavAreaJump`, `NavAreaClimb`, `NavAreaTeleport`, `NavFlagJump`, `NavFlagClimb`, `NavFlagTeleport`), scales traversal cost by `filter.AreaCost(link.Area)`, and adjusts its heuristic when a link's effective cost is cheaper than straight-line distance so teleporters are used only when strictly cheaper than walking.
- **YAML storage and scene placement**: store 2D off-mesh links in baked YAML files via `navbake.Source.Links` / `navbake.Mesh.Links` (applied automatically by `navbake.Mesh.NavMesh`), or place them on scene objects using the built-in `"nav_link"` component (`sceneformat.RegisterNavLink`, `sceneformat.ExtractNavLinks2D`, `sceneformat.ExtractBuiltNavLinks2D`).

### Navigation agents and path corridors (`NavAgent`, `NavAgentGroup`)

`NavAgent` maintains an active polygon corridor over a `pathfind.NavMesh`, computes the next funnel corner (`NextCorner`) directly from its current position, steers smoothly toward it, slows inside `ArrivalRadius` to stop cleanly within `StoppingDistance`, and exposes explicit state (`NavAgentIdle`, `NavAgentMoving`, `NavAgentArrived`, `NavAgentUnreachable`):

```go
agent := pathfind.NewNavAgent(mesh, core.Point{X: 16, Y: 16}, pathfind.NavAgentConfig{
	MaxSpeed:         120,
	MaxAcceleration:  480,
	Radius:           10,
	ArrivalRadius:    40,
	StoppingDistance: 8,
	ReplanInterval:   100 * time.Millisecond,
	OnLink: func(a *pathfind.NavAgent, ev pathfind.NavLinkEvent, dt time.Duration) bool {
		// Return true when custom off-mesh or flagged-edge traversal finishes.
		return true
	},
})
agent.SetTarget(playerPos)

// Inside game.Loop.Update:
agent.SetTarget(playerPos)
agent.Step(dt)
```

- **Cheap local corridor repair**: when the agent advances, gets pushed off its corridor onto an adjacent polygon (`SetPosition`), or chases a target moving onto a neighboring polygon, `NavAgent` repairs its corridor locally via bounded adjacency bridging (up to `MaxRepairDepth`, default 2) and loop truncation without running a full A* replan. A full replan runs only when the corridor breaks beyond `MaxRepairDepth`, after sustained tail drift, or when the mesh's `Revision()` changes (`AddOffMeshLink`, `SetEdgeFlags`, `BumpRevision`, or `SetMesh`).
- **Throttled and staggered group replanning**: manage many chasing agents with `NewNavAgentGroup(NavAgentGroupConfig{MaxReplansPerStep: 4, ReplanInterval: 100 * time.Millisecond, StaggerStride: 1})` and call `group.Step(dt)`. Full A* replans are strictly bounded to `MaxReplansPerStep` per step in deterministic round-robin order (optionally staggered by `StaggerStride`) so large crowds never spike a single frame.
- **External avoidance velocity**: for local avoidance, read `DesiredVelocity()` (the velocity `Step` would steer toward, zero when idle, unreachable, arrived or traversing a link), pass it through a solver such as `ai.ORCASolver`, and call `StepWithVelocity(v, dt)` instead of `Step(dt)`. The agent reaches `v` under `MaxAcceleration`, clamped to `MaxSpeed`, and moves with the same corridor repair, wall sliding, link traversal and arrival handling as `Step`; feeding `DesiredVelocity()` back unchanged moves it as `Step` would. It is meant for standalone agents: a `NavAgentGroup` moves its agents in `group.Step`.
- **Off-mesh and flagged link callbacks**: `OnLink` is invoked each step while traversing an off-mesh link (`NavLinkOffMesh`) or crossing a flagged shared-edge portal (`SetEdgeFlags`); return `false` while driving a custom jump/door transition and `true` to resume normal corridor steering. When `OnLink` is `nil`, off-mesh links traverse linearly at `MaxSpeed`.
- **Deterministic stepping**: `Step(dt)` uses no background goroutines, no map iteration, and no hidden randomness; replaying identical inputs produces bit-identical positions every step.

### Polygon areas, runtime flags, and query filters

Tag convex polygons at build time with `BuildNavMeshWithAreas` using `NavPolygonInput{Vertices, Area, Flags}` (`NavAreaDefault`, `NavAreaRoad`, `NavAreaGrass`, `NavAreaMud`, `NavAreaWater`, `NavAreaDoor`, `NavAreaHazard`, or any custom `uint8` `NavArea`; `NavFlagWalk`, `NavFlagSwim`, `NavFlagDoor`, `NavFlagJump`, `NavFlagDisabled`, `NavFlagAll` — zero `Flags` defaults to `NavFlagWalk`):

```go
mesh := pathfind.BuildNavMeshWithAreas([]pathfind.NavPolygonInput{
	{Vertices: grassPoly, Area: pathfind.NavAreaGrass},
	{Vertices: roadPoly, Area: pathfind.NavAreaRoad},
	{Vertices: waterPoly, Area: pathfind.NavAreaWater, Flags: pathfind.NavFlagSwim},
	{Vertices: doorPoly, Area: pathfind.NavAreaDoor, Flags: pathfind.NavFlagWalk | pathfind.NavFlagDoor},
})

filter := pathfind.DefaultNavFilter()
filter.SetAreaCost(pathfind.NavAreaRoad, 0.5)   // roads cost half as much per unit distance
filter.SetAreaCost(pathfind.NavAreaMud, 2.5)
filter.ExcludeFlags |= pathfind.NavFlagSwim     // land agents route around water

path, ok := mesh.FindPathWithFilter(start, goal, filter)

// Close or reopen a doorway at runtime in O(1) without rebuilding the mesh:
mesh.AddPolygonFlags(doorIndex, pathfind.NavFlagDisabled)
detour, ok := mesh.FindPathWithFilter(start, goal, filter)
mesh.ClearPolygonFlags(doorIndex, pathfind.NavFlagDisabled)
_, _, _ = path, detour, ok
```

- **Default compatibility**: `BuildNavMesh` assigns `NavAreaDefault` and `NavFlagWalk` to every polygon, and `FindPath` runs with `DefaultNavFilter()` (`IncludeFlags: NavFlagAll`, `ExcludeFlags: NavFlagDisabled`, cost `1.0` per area).
- **Admissible weighted A\***: `FindPathWithFilter` weights distance inside each polygon by `filter.AreaCost(area)` and scales the A\* distance heuristic by the minimum allowed area cost across the mesh so sub-unit road costs remain optimal. Non-positive, `NaN`, or infinite area costs forbid traversal.
- **Runtime updates without rebuild**: inspect or mutate polygons in O(1) via `PolygonArea`, `SetPolygonArea`, `PolygonFlags`, `SetPolygonFlags`, `AddPolygonFlags`, and `ClearPolygonFlags`. Filtered spatial queries (`FindPolygonWithFilter`, `ContainsWithFilter`, `NearestPolygonWithFilter`, `NearestPointWithFilter`, `RaycastWithFilter`, `MoveAlongSurfaceWithFilter`, `RandomPointWithFilter`, `RandomPointAroundWithFilter`) and filtered bounded/incremental searches (`FindPathWithFilterAndOptions`, `StartSearchWithFilter`) honor the same `NavFilter`.

### Spatial queries, surface movement, and zero-allocation path reuse

Every `pathfind.NavMesh` builds a flat 2D AABB BVH spatial index at construction time so `FindPolygon`, `NearestPolygon`, `NearestPoint`, and pathfinding endpoint lookups scale logarithmically on large meshes instead of scanning every polygon:

```go
rng := random.New(42)

// Snap a click or spawn position onto the nearest allowed polygon within half-extents:
snapped, poly, ok := mesh.NearestPoint(clickPos, core.Point{X: 32, Y: 32})

// Test straight-line walkability along the mesh surface:
hit, obstructed := mesh.Raycast(agentPos, targetPos)

// Slide a moving agent toward desiredPos while clamping cleanly to walkable polygons:
moved, onMesh := mesh.MoveAlongSurface(agentPos, desiredPos)

// Pick deterministic, area-weighted random points (anywhere or reachable within radius):
patrolPt, _, _ := mesh.RandomPoint(rng)
wanderPt, _, _ := mesh.RandomPointAround(agentPos, 96, rng)

// Reuse caller and query buffers for zero-allocation path and corridor searches:
query := pathfind.NewNavMeshQuery(mesh)
pathBuf, ok = query.FindPathInto(pathBuf, snapped, wanderPt)
corridorBuf, _ = query.FindCorridorInto(corridorBuf, snapped, wanderPt)
_, _, _, _, _, _, _ = poly, hit, obstructed, moved, onMesh, patrolPt, ok
```

- **Extent-bounded snapping**: `NearestPoint(p, extent)` and `NearestPointWithFilter(p, extent, filter)` find the closest point inside `[p - extent, p + extent]` on any allowed polygon (clipping overlapping polygons against the search box when needed).
- **Surface raycast and sliding**: `Raycast` / `RaycastWithFilter` walk shared-edge portals and return `NavRaycastHit{Point, Normal, Poly, T, Hit}` (with unit inward edge normal on obstruction); `MoveAlongSurface` / `MoveAlongSurfaceWithFilter` slide along obstructing boundaries across polygons and return `NavMoveResult{Point, Poly, Visited, HitBoundary}`.
- **Deterministic area-weighted sampling**: `RandomPoint` / `RandomPointWithFilter` sample uniformly by polygon area across allowed polygons; `RandomPointAround` / `RandomPointAroundWithFilter` restrict sampling to connected allowed polygons reachable from `center` within `radius` (never returning disconnected islands). Given the same `*random.Stream` seed, results are bit-identical across runs and architectures.
- **Zero-allocation queries and concurrency**: `mesh.FindPathInto`, `mesh.FindPathWithFilterInto`, `mesh.FindPathWithFilterAndOptionsInto`, and `mesh.FindCorridorInto` reuse generation-stamped scratch buffers for `0` heap allocations after warm-up. Create one `NewNavMeshQuery(mesh)` per worker or goroutine when querying a shared read-only `NavMesh` concurrently (also exposing `MoveAlongSurfaceInto` and buffer-reusing `RandomPointAround`).
- **Path lengths and sample points**: `query.PathLength(start, goal)` returns the length of the funnel-smoothed path between two points without allocating after warm-up, and `mesh.AppendSamples(dst, center, maxRadius, spacing)` appends the points of a regular grid that lie inside the mesh (or each polygon's centroid when `spacing <= 0`), optionally within `maxRadius` of `center`. [ai](ai.md)'s `PathDistanceTest` and `NavMeshGenerator` are built on them.

### Tiled 2D NavMesh and dynamic obstacles (`TiledNavMesh`)

When doors, placed buildings, destructible walls, or streamed regions change walkable areas at runtime, use `TiledNavMesh`: it partitions 2D polygons into fixed-size square tiles (`NavTileCoord{X, Y}`) that can be baked (`BakeNavTile` / `BakeTile`), added (`AddTile`), replaced (`ReplaceTile` / `SetTile`), removed (`RemoveTile`), and rebuilt (`RebuildTile`) independently, automatically linking polygons across tile borders so paths match an untiled `NavMesh`:

```go
tm := pathfind.BuildTiledNavMesh(corridorPolys, 32) // or pathfind.NewTiledNavMesh(pathfind.TiledNavMeshConfig{TileSize: 32})

res := tm.FindPathWithOptions(start, goal, pathfind.PathOptions{})
watchID := tm.WatchPath(res.TileCoords(), func(ev pathfind.NavTileChange) {
	// Only paths crossing ev.Coord are notified when that tile changes.
	replanPath()
})

// Carve a box, circle, or (convex/concave) polygon obstacle — rebuilds ONLY the touched tiles:
obsID, _ := tm.AddObstacle(pathfind.NavBoxObstacle(minPt, maxPt))
detour, _ := tm.FindPath(start, goal) // detours around the box; tm.IsPathValid(res) is now false

// Remove the obstacle to restore the original polygons and path:
tm.RemoveObstacle(obsID)
tm.UnwatchPath(watchID)
_, _ = detour, watchID
```

- **Dynamic obstacle shapes and modes**: `NavBoxObstacle`, `NavOrientedBoxObstacle`, `NavCircleObstacle`, and `NavPolygonObstacle` (supporting both convex and concave footprints plus optional `Margin` inflation) default to `NavObstacleCarve` (exact convex subtraction so paths detour cleanly around obstacles) or can use `NavObstacleMark` to split polygons along the obstacle footprint and apply `NavFlagDisabled` or a custom `Area`/`Flags` (such as `NavAreaMud`). Wrap multi-obstacle frame updates in `tm.Batch(func() { ... })` so each touched tile rebuilds at most once.
- **Two-level spatial index**: `FindPolygon`, `NearestPoint`, `NearestPolygon`, and `Raycast` use an O(1) tile coordinate lookup (or branch-and-bound over a coarse tile BVH for unbounded queries) plus per-tile flat AABB BVHs, avoiding whole-mesh scans.
- **Per-tile path invalidation**: `TiledNavPathResult.Tiles` stamps every crossed tile's version so `tm.IsPathValid(res)` detects invalidation in O(crossed tiles); `WatchPath` indexes watchers per tile and notifies only paths whose corridor crossed a modified or removed tile (`AddTileListener` observes all tile events).
- **Off-mesh links across tiles**: register one-way or bidirectional jumps, drops, ladders, and teleports on `TiledNavMesh` with `AddOffMeshLink(NavOffMeshLink)` (`OffMeshLinks`, `ClearOffMeshLinks`), supporting endpoint `Radius` snapping, `NavFilter` area/flag filtering and cost scaling, shortcut-aware A\*, and explicit `TiledNavPathResult.Links` (`HasLinks`, `LinkAt`) with `NavPolyRef` endpoints. Links stay indexed by their candidate endpoint tiles so they automatically detach when covered or unloaded and re-attach whenever touched tiles are replaced, rebuilt, or restored after dynamic obstacle carving.
- **Baked bridge**: convert any 2D baked mesh directly into a tiled mesh with `navbake.Mesh.TiledNavMesh(tileSize)`, which registers all baked `Links` automatically.

## Bounded, partial, and incremental path searches

Clicking an unreachable target should still move a unit as close as possible, and dozens of simultaneous long searches should never stall a frame. `AStar`, 2D `NavMesh`, and [pathfind3d](pathfind3d.md)'s 3D `NavMesh` share `PathOptions{MaxExpansions, AllowPartial}` and the `IncrementalSearch` interface:

```go
// Single-shot bounded search with partial-path fallback:
res := astar.FindWithOptions(grid, start, goal, pathfind.PathOptions{
	MaxExpansions: 128,
	AllowPartial:  true,
})
if res.Found {
	// When res.Partial is true, res.Path ends at the closest explored walkable cell.
	// res.Exhausted reports whether MaxExpansions stopped the search early.
	follow(res.Path)
}

// 2D NavMesh bounded/partial search (same PathOptions):
navRes := mesh.FindPathWithOptions(startPt, goalPt, pathfind.PathOptions{
	MaxExpansions: 64,
	AllowPartial:  true,
})
_ = navRes
```

- **Partial paths**: when `AllowPartial` is `true` and the goal is walled off, blocked, outside the mesh, or beyond `MaxExpansions`, `FindWithOptions` / `FindWithOptionsInto` (zero-allocation after warm-up) and `NavMesh.FindPathWithOptions` return a valid path to the closest explored cell or closest point on an explored polygon, with `Partial: true`. Setting `AStar.MaxExpansions > 0` also caps `Find` / `FindInto` and acts as the default budget when `PathOptions.MaxExpansions == 0` (pass a negative `PathOptions.MaxExpansions` to override to unlimited).
- **Deterministic incremental stepping**: start a multi-frame search with `astar.StartSearch(grid, start, goal, opts)` (`*AStarSearch`, reusable via `Reset`) or `mesh.StartSearch(start, goal, opts)` (`*NavMeshSearch`) and advance it with `Step(maxExpansions)`. Splitting a search across any positive step sizes (even 1 expansion per step) produces the exact same path, cost, partial flag, and expansion count as a single-shot call.
- **Budgeted request queue (`PathQueue`)**: service many grid, 2D `NavMesh`, and `pathfind3d.NavMesh` searches within one strict per-step expansion budget using `NewPathQueue`, `Submit` / `SubmitGrid` / `SubmitNavMesh`, `Cancel`, and `Step(budget)` / `StepDefault()`:

```go
queue := pathfind.NewPathQueue(pathfind.PathQueueConfig{
	DefaultBudget: 64,                     // strict max expansions per StepDefault
	Policy:        pathfind.PathQueueRoundRobin, // or pathfind.PathQueueFIFO (default)
	SliceSize:     16,                     // max expansions per turn (0 = even split)
})

reqID, search := queue.SubmitGrid(astar, grid, start, goal, pathfind.PathOptions{
	AllowPartial: true,
}, 1, func(id pathfind.PathRequestID, res pathfind.GridPathResult) {
	assignUnitPath(res.Path, res.Partial)
})

// Inside game.Loop.Update:
_ = queue.StepDefault() // never exceeds DefaultBudget expansions
_ = search.Status()     // poll directly, or cancel with queue.Cancel(reqID)
_ = reqID
```

Higher-priority requests run first, ties break deterministically in FIFO or round-robin rotation order within each priority tier, and canceled requests are never stepped again or delivered to callbacks.
