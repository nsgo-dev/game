# pathfind3d

`game/pathfind3d` provides deterministic 3D navigation on [math3d](math3d.md): multi-level navigation meshes over ramps, bridges and stacked floors with off-mesh links, polygon areas and filters, reusable zero-allocation queries and incremental searches, `NavAgent` path corridors with agent groups, and tiled meshes with dynamic obstacles that stream in and out with [world3d](world3d.md)'s chunks. It shares `NavArea`, `NavFlags` and `NavFilter` with [pathfind](pathfind.md), its 2D counterpart, and [navbake3d](navbake3d.md) bakes its meshes from level geometry.

```go
import "github.com/nsgo-dev/game/pkg/pathfind3d"
```

## Multi-level 3D NavMesh

Build a 3D `NavMesh` from convex walkable polygons (ramps, bridges, and stacked floors). `BuildNavMesh` filters polygons steeper than `maxSlopeRad`, links shared edges in 3D, and runs portal A* with 3D string-pulling in `FindPath`:

```go
nav := pathfind3d.BuildNavMesh(walkablePolys, math3d.DegToRad(45))
if path, ok := nav.FindPath(startPos, goalPos); ok {
	follow3DWaypoints(path)
}
```

Use `nav.FindPolygon(p)` to locate the closest walkable polygon vertically within `VerticalTolerance` (supporting stacked floors and overpasses), `Contains(p)` and `NearestPolygon(p)` for membership and closest-surface queries, `FindCorridor(start, goal)` to query polygon corridors, `AddOffMeshLink(NavOffMeshLink{...})` / `OffMeshLinks()` / `ClearOffMeshLinks()` for jumps, ladders, and teleports, and `SetEdgeFlags(fromPoly, toPoly, flags)` for special portal edges.

## Off-mesh links (jumps, ladders, and teleports)

Connect vertically separated ledges, disconnected islands, ladders, or teleporters across a 3D `NavMesh` with `AddOffMeshLink`:

```go
nav.AddOffMeshLink(pathfind3d.NavOffMeshLink{
	Start:         math3d.Vec3{X: 9, Y: 4, Z: 5},
	End:           math3d.Vec3{X: 13, Y: 0, Z: 5},
	Radius:        0.5, // snaps off-edge endpoints within Radius while respecting stacked levels
	Bidirectional: false,
	Cost:          0, // <= 0 uses 3D Euclidean distance
	Area:          pathfind3d.NavAreaJump,
	Flags:         uint32(pathfind3d.NavFlagJump),
	ID:            7,
	UserData:      "drop",
})

res := nav.FindPathWithOptions(startPos, goalPos, pathfind.PathOptions{})
if res.Found {
	for i := 0; i+1 < len(res.Path); i++ {
		if link, ok := res.LinkAt(i); ok {
			// Segment Path[link.StartIndex] -> Path[link.EndIndex] traverses a 3D off-mesh link.
			_ = link.UserData
		}
	}
}
```

- **Path segment metadata**: `NavPathResult.Links` records every traversed 3D off-mesh link (`StartIndex`, `EndIndex == StartIndex + 1`, `FromPoly`, `ToPoly`, `Start`, `End`, `Radius`, `Area`, `Flags`, `LinkID`, `UserData`), with `HasLinks()` and `LinkAt(segmentIndex)`. `NavAgent` delivers the same `Radius`, `Area`, and `UserData` in `NavLinkEvent` and triggers link entry within `Radius`.
- **Shortcut-aware 3D search & filtering**: A\* filters off-mesh links by `NavFilter` (`Area` and `Flags`), scales traversal cost by `filter.AreaCost(link.Area)`, and uses a shortcut-aware admissible 3D heuristic so cheap teleporters are chosen only when strictly cheaper than walking.
- **YAML storage and scene placement**: place `"nav_link"` components on scene objects (`sceneformat.RegisterNavLink`, `sceneformat.ExtractNavLinks3D`, `sceneformat.ExtractBuiltNavLinks3D`); `navbake3d.Geometry.AddSceneFile` bakes them into `meshes[].links` in `*.navmesh.yaml`, and `navbake3d.Mesh.NavMesh` restores them automatically.

## Polygon areas, runtime flags, and query filters

Tag 3D polygons at build time with `BuildNavMeshWithAreas` using `NavPolygonInput{Vertices, Area, Flags}`. `pathfind3d.NavArea`, `pathfind3d.NavFlags`, and `pathfind3d.NavFilter` alias the types in [pathfind](pathfind.md), so the same filter and area/flag constants work seamlessly across 2D and 3D navigation meshes:

```go
nav := pathfind3d.BuildNavMeshWithAreas([]pathfind3d.NavPolygonInput{
	{Vertices: grassQuad, Area: pathfind3d.NavAreaGrass},
	{Vertices: roadQuad, Area: pathfind3d.NavAreaRoad},
	{Vertices: waterQuad, Area: pathfind3d.NavAreaWater, Flags: pathfind3d.NavFlagSwim},
	{Vertices: gateQuad, Area: pathfind3d.NavAreaDoor, Flags: pathfind3d.NavFlagWalk | pathfind3d.NavFlagDoor},
}, math3d.DegToRad(45))

filter := pathfind3d.DefaultNavFilter()
filter.SetAreaCost(pathfind3d.NavAreaRoad, 0.5)
filter.ExcludeFlags |= pathfind3d.NavFlagSwim

path, ok := nav.FindPathWithFilter(startPos, goalPos, filter)

// Toggle a gate or drawbridge polygon at runtime without rebuilding:
nav.AddPolygonFlags(gatePolyIndex, pathfind3d.NavFlagDisabled)
detour, ok := nav.FindPathWithFilter(startPos, goalPos, filter)
nav.ClearPolygonFlags(gatePolyIndex, pathfind3d.NavFlagDisabled)
_, _, _ = path, detour, ok
```

- **Default compatibility**: `BuildNavMesh` assigns `NavAreaDefault` and `NavFlagWalk` to every walkable `NavPoly`, and `FindPath` delegates to `FindPathWithFilter` with `DefaultNavFilter()`.
- **Admissible 3D weighted portal A\***: traversal inside each polygon is weighted by `filter.AreaCost(poly.Area)` with an admissible 3D heuristic scaled by the minimum allowed area cost across the mesh, followed by exact 3D funnel smoothing across ramps and stacked levels.
- **Runtime mutation & filtered queries**: inspect or update polygons in O(1) via `PolygonCount`, `PolygonArea`, `SetPolygonArea`, `PolygonFlags`, `SetPolygonFlags`, `AddPolygonFlags`, and `ClearPolygonFlags`, and run filtered lookups with `FindPolygonWithFilter`, `ContainsWithFilter`, `NearestPolygonWithFilter`, `NearestPointWithFilter`, `RaycastWithFilter`, `MoveAlongSurfaceWithFilter`, `RandomPointWithFilter`, and `RandomPointAroundWithFilter`.

## Spatial queries, surface movement, and zero-allocation path reuse

`pathfind3d.NavMesh` builds a flat 3D AABB BVH spatial index at construction time so `FindPolygon`, `NearestPolygon`, `NearestPoint`, and pathfinding endpoint lookups scale logarithmically on large multi-level worlds without scanning every polygon:

```go
rng := random.New(42)

// Snap a 3D click or spawn position within 3D half-extents (cleanly separating stacked floors):
snapped, poly, ok := nav.NearestPoint(spawnPos, math3d.Vec3{X: 3, Y: 1.5, Z: 3})

// Cast a surface ray across ramps and bridges (stops at bridge edges instead of dropping levels):
hit, obstructed := nav.Raycast(agentPos, targetPos)

// Slide an agent along the 3D mesh surface while tracking ramp elevation and staying off ledges:
moved, onMesh := nav.MoveAlongSurface(agentPos, desiredPos)

// Pick deterministic 3D surface-area-weighted random points (anywhere or reachable within 3D radius):
patrolPt, _, _ := nav.RandomPoint(rng)
wanderPt, _, _ := nav.RandomPointAround(agentPos, 12, rng)

// Reuse buffers via NavMeshQuery for zero-allocation path and corridor queries:
query := pathfind3d.NewNavMeshQuery(nav)
pathBuf, ok = query.FindPathInto(pathBuf, snapped, wanderPt)
corridorBuf, _ = query.FindCorridorInto(corridorBuf, snapped, wanderPt)
_, _, _, _, _, _, _ = poly, hit, obstructed, moved, onMesh, patrolPt, ok
```

- **3D extent-bounded snapping**: `NearestPoint(p, extent)` and `NearestPointWithFilter(p, extent, filter)` restrict matches to `[p - extent, p + extent]` in full 3D (clipping sloped polygons to the extent box when needed), keeping stacked floors and overpasses cleanly separated by `extent.Y`.
- **3D surface raycast and sliding**: `Raycast` / `RaycastWithFilter` traverse only shared 3D polygon edges, returning `NavRaycastHit{Point, Normal, Poly, T, Hit}` with exact `HeightAt` elevation and a 3D surface-tangent inward normal; `MoveAlongSurface` / `MoveAlongSurfaceWithFilter` slide along boundary edges across ramps and bridges without dropping between stacked levels (`NavMoveResult{Point, Poly, Visited, HitBoundary}`).
- **Deterministic 3D surface-area random points**: `RandomPoint` / `RandomPointWithFilter` weight polygons by true 3D surface area (not just XZ projection); `RandomPointAround` / `RandomPointAroundWithFilter` sample only reachable polygons within 3D Euclidean `radius` of the snapped center (never jumping to unconnected floors overhead). Results are bit-identical across runs and architectures for the same `*random.Stream` seed.
- **Zero-allocation path reuse & concurrent queries**: `nav.FindPathInto`, `nav.FindPathWithFilterInto`, `nav.FindPathWithFilterAndOptionsInto`, and `nav.FindCorridorInto` reuse generation-stamped scratch buffers with `0` heap allocations after warm-up; allocate one `NewNavMeshQuery(nav)` per worker goroutine for concurrent read-only queries over a shared `NavMesh`.

For unreachable targets or frame-budgeted multi-agent queries, pass `pathfind.PathOptions{MaxExpansions, AllowPartial}` to `nav.FindPathWithOptions(startPos, goalPos, opts)` (returning a `NavPathResult` with `Partial`, `Exhausted`, and `Expansions`), or create a deterministic multi-frame `*pathfind3d.NavMeshSearch` via `nav.StartSearch(startPos, goalPos, opts)`:

```go
search := nav.StartSearch(startPos, goalPos, pathfind.PathOptions{
	MaxExpansions: 128,
	AllowPartial:  true,
})
// Step directly, or schedule alongside 2D searches in a pathfind.PathQueue:
queue.Submit(search, 0, func(_ pathfind.PathRequestID, _ pathfind.SearchStatus) {
	res := search.Result()
	if res.Found {
		follow3DWaypoints(res.Path) // res.Partial is true if goal was unreachable or budgeted out
	}
})
```

Stepping `NavMeshSearch` across any positive per-step budget returns the exact same 3D surface path as `FindPathWithOptions`, and partial paths end at the closest reachable 3D point on the explored polygons.

To bake the polygons from the level's geometry instead of writing them by hand, use [navbake3d](navbake3d.md): it rasterizes glTF models, terrain, `GridMap`s and physics colliders for each agent type and `Mesh.NavMesh` returns the `NavMesh`.

## 3D navigation agents (`NavAgent`, `NavAgentGroup`)

`pathfind3d.NavAgent` follows and locally repairs 3D polygon corridors over a `pathfind3d.NavMesh`, steering smoothly on XZ toward the next funnel corner while tracking exact 3D surface elevation (`NavPoly.HeightAt`) across sloped ramps and multi-level bridges, slowing inside `ArrivalRadius` to stop within `StoppingDistance`, and exposing explicit state (`NavAgentIdle`, `NavAgentMoving`, `NavAgentArrived`, `NavAgentUnreachable`):

```go
group := pathfind3d.NewNavAgentGroup(pathfind3d.NavAgentGroupConfig{
	MaxReplansPerStep: 4,
	ReplanInterval:    100 * time.Millisecond,
})
agent := group.Add(nav, startPos, pathfind3d.NavAgentConfig{
	MaxSpeed:         6,
	MaxAcceleration:  30,
	Radius:           0.4,
	ArrivalRadius:    1.5,
	StoppingDistance: 0.35,
	OnLink: func(a *pathfind3d.NavAgent, ev pathfind3d.NavLinkEvent, dt time.Duration) bool {
		// Drive custom jump/ladder traversal and return true when done.
		return true
	},
})
agent.SetTarget(goalPos)

// Inside game.Loop.Update:
agent.SetTarget(movingTargetPos)
group.Step(dt)
```

- **Local corridor repair & vertical separation**: `NavAgent` checks connected shared-edge neighbors first so agents on upper bridges never snap down to floors underneath, and repairs pushed start positions (`SetPosition`) or shifted targets locally within `MaxRepairDepth` (default 2) without a full A* replan.
- **Throttled & staggered replanning**: `NavAgentGroup.Step(dt)` strictly bounds full A* replans to `MaxReplansPerStep` per fixed step in deterministic round-robin order (optionally staggered by `StaggerStride`), and automatically detects mesh changes (including runtime area/flag updates and off-mesh links) via `NavMesh.Revision()`.
- **Deterministic fixed-step replay**: replaying the same inputs produces bit-identical 3D positions every step.

## Tiled 3D NavMesh, dynamic obstacles, and chunk streaming (`TiledNavMesh`, `NavTileStreamer`)

For open worlds, doors, placed buildings, and destructible cover, `pathfind3d.TiledNavMesh` partitions 3D walkable polygons into fixed-size XZ tiles aligned 1:1 with `world3d.ChunkCoord`, linking polygons in full 3D across tile borders (preserving ramps and stacked levels), rebuilding only touched tiles when dynamic obstacles change, and invalidating paths that crossed modified or unloaded tiles:

```go
tm := pathfind3d.NewTiledNavMesh(pathfind3d.TiledNavMeshConfig{TileSize: 64})

// Follow world3d chunk streaming: bake tiles on background workers and load/unload with chunks:
streamer, err := pathfind3d.NewNavTileStreamer(pathfind3d.NavTileStreamerConfig{
	Mesh:         tm,
	LoadRadius:   2,
	UnloadRadius: 3,
	Workers:      2,
	Bake: func(ctx context.Context, coord world3d.ChunkCoord, bounds math3d.AABB) (*pathfind3d.NavTile, error) {
		return bakeChunkNavTile(ctx, coord, bounds) // e.g. pathfind3d.BakeNavTile or navbake3d.BakeTile
	},
})
if err != nil {
	return err
}
defer streamer.Close()

streamer.Update(world3d.WorldPos64{X: float64(player.X), Z: float64(player.Z)})

// Drop a 3D box, cylinder, or extruded polygon obstacle (rebuilds ONLY touched tiles):
obsID, _ := tm.AddObstacle(pathfind3d.NavBoxObstacle(minPos, maxPos))
detour, _ := tm.FindPath(startPos, goalPos) // detours around box; vertical Y bounds keep bridges above unaffected
tm.RemoveObstacle(obsID)                    // restores original geometry and path
_, _ = detour, obsID
```

- **Independent tile lifecycle**: bake standalone tiles off-thread with `pathfind3d.BakeNavTile` (or voxel-bake directly from `navbake3d.Geometry` via `navbake3d.BakeTile`), then call `AddTile`, `ReplaceTile`, `SetTile`, `RemoveTile`, or `RebuildTile`; `BuildTiledNavMesh` / `BuildTiledNavMeshWithAreas` and `navbake3d.Mesh.TiledNavMesh(tileSize)` build a tiled mesh from existing polygons or baked meshes.
- **3D dynamic obstacles**: `NavBoxObstacle`, `NavOrientedBoxObstacle`, `NavCylinderObstacle`, and `NavPolygonObstacle` check vertical Y overlap so obstacles on a ground floor never carve bridges overhead. Both `NavObstacleCarve` (exact planar subtraction) and `NavObstacleMark` (splitting and setting `NavFlagDisabled` or custom `Area`/`Flags`) are supported; group multi-obstacle edits inside `tm.Batch(func() { ... })`.
- **Two-level 3D spatial index & path invalidation**: `FindPolygon`, `NearestPoint`, `NearestPolygon`, and `Raycast` use O(1) XZ tile lookup + per-tile 3D flat BVHs (plus a coarse 3D tile BVH for unbounded nearest lookups). Track path validity with `tm.IsPathValid(res)` (`TiledNavPathResult.Tiles`), register per-tile invalidation callbacks with `tm.WatchPath(res.TileCoords(), fn)`, or observe all tile changes via `tm.AddTileListener`.
