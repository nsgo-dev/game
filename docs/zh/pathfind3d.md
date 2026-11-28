# pathfind3d

`game/pathfind3d` 基于 [math3d](math3d.md) 提供确定性的 3D 导航：覆盖斜坡、桥梁与多层楼板的多层导航网格，支持离网连接、多边形区域与过滤器、可复用的零分配查询与增量搜索，带智能体群组的 `NavAgent` 路径走廊，以及随 [world3d](world3d.md) 区块流式加载、支持动态障碍物的分块网格。它与 2D 对应包 [pathfind](pathfind.md) 共用 `NavArea`、`NavFlags` 和 `NavFilter`，[navbake3d](navbake3d.md) 则从关卡几何体烘焙它的网格。

```go
import "github.com/nsgo-dev/game/pkg/pathfind3d"
```

## 多层 3D 导航网格（`NavMesh`）

用凸可行走多边形（斜坡、桥梁、多层楼板）构建 3D `NavMesh`。`BuildNavMesh` 会自动过滤超过 `maxSlopeRad` 的陡坡、在 3D 空间连接共享边，并在 `FindPath` 中执行门户 A* 与 3D 路径拉直：

```go
nav := pathfind3d.BuildNavMesh(walkablePolys, math3d.DegToRad(45))
if path, ok := nav.FindPath(startPos, goalPos); ok {
	follow3DWaypoints(path)
}
```

`nav.FindPolygon(p)` 会在 `VerticalTolerance` 垂直容差内定位最近的可行走多边形（天然支持上下叠层与立交桥）；也可用 `Contains(p)` 与 `NearestPolygon(p)` 检查点是否在网格上或寻找最近表面点，`FindCorridor(start, goal)` 查询多边形走廊，`AddOffMeshLink(NavOffMeshLink{...})` / `OffMeshLinks()` / `ClearOffMeshLinks()` 管理跳跃、梯子或传送等离网连接，`SetEdgeFlags(fromPoly, toPoly, flags)` 为特殊共享边设置标记。

## 离网连接（跳跃、梯子与传送门）

使用 `AddOffMeshLink` 连接垂直分离的平台边缘、不连通的孤岛、梯子或传送门：

```go
nav.AddOffMeshLink(pathfind3d.NavOffMeshLink{
	Start:         math3d.Vec3{X: 9, Y: 4, Z: 5},
	End:           math3d.Vec3{X: 13, Y: 0, Z: 5},
	Radius:        0.5, // 在保持上下楼层隔离的前提下将边缘外的端点吸附到最近多边形
	Bidirectional: false,
	Cost:          0, // <= 0 时使用 3D 欧氏距离
	Area:          pathfind3d.NavAreaJump,
	Flags:         uint32(pathfind3d.NavFlagJump),
	ID:            7,
	UserData:      "drop",
})

res := nav.FindPathWithOptions(startPos, goalPos, pathfind.PathOptions{})
if res.Found {
	for i := 0; i+1 < len(res.Path); i++ {
		if link, ok := res.LinkAt(i); ok {
			// 路径段 Path[link.StartIndex] -> Path[link.EndIndex] 穿越一条 3D 离网连接。
			_ = link.UserData
		}
	}
}
```

- **路径段标记**：`NavPathResult.Links` 按路径顺序记录每一段穿越的 3D 离网连接（含 `StartIndex`、`EndIndex == StartIndex + 1`、`FromPoly`、`ToPoly`、`Start`、`End`、`Radius`、`Area`、`Flags`、`LinkID`、`UserData`），并提供 `HasLinks()` 与 `LinkAt(segmentIndex)`；`NavAgent` 会在 `NavLinkEvent` 中透传相同的 `Radius`、`Area` 与 `UserData`，并在 `Radius` 范围内触发连接穿越。
- **捷径感知 3D 搜索与过滤**：A\* 按 `NavFilter` 对离网连接的 `Area` 与 `Flags` 过滤并按 `filter.AreaCost(link.Area)` 缩放代价，且针对低代价传送连接自动维护可采纳的 3D 启发式，确保仅在比步行更便宜时才走传送连接。
- **YAML 持久化与场景摆放**：在场景对象上放置 `"nav_link"` 组件（`sceneformat.RegisterNavLink`、`sceneformat.ExtractNavLinks3D`、`sceneformat.ExtractBuiltNavLinks3D`）；`navbake3d.Geometry.AddSceneFile` 会自动将其写入 `*.navmesh.yaml` 的 `meshes[].links`，`navbake3d.Mesh.NavMesh` 加载时自动还原。

## 多边形区域代价、运行时标志与查询过滤器

使用 `BuildNavMeshWithAreas` 和 `NavPolygonInput{Vertices, Area, Flags}` 在构建时为 3D 多边形设置区域与通行标志。`pathfind3d.NavArea`、`pathfind3d.NavFlags` 和 `pathfind3d.NavFilter` 直接作为 [pathfind](pathfind.md) 中对应类型的别名，因此同一套区域常量、标志位和过滤器可在 2D 与 3D 导航网格间无缝复用：

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

// 运行时 O(1) 开关门或吊桥多边形，无需重建导航网格：
nav.AddPolygonFlags(gatePolyIndex, pathfind3d.NavFlagDisabled)
detour, ok := nav.FindPathWithFilter(startPos, goalPos, filter)
nav.ClearPolygonFlags(gatePolyIndex, pathfind3d.NavFlagDisabled)
_, _, _ = path, detour, ok
```

- **默认兼容**：`BuildNavMesh` 为每个可行走 `NavPoly` 分配 `NavAreaDefault` 与 `NavFlagWalk`，`FindPath` 使用 `DefaultNavFilter()` 调用 `FindPathWithFilter`。
- **可采纳 3D 加权门户 A\***：每个多边形内的移动距离按 `filter.AreaCost(poly.Area)` 加权，并以网格内允许区域的最小代价缩放 3D 距离启发值，随后沿斜坡与多层桥梁执行精确的 3D 漏斗平滑。
- **运行时修改与过滤查询**：通过 `PolygonCount`、`PolygonArea`、`SetPolygonArea`、`PolygonFlags`、`SetPolygonFlags`、`AddPolygonFlags` 和 `ClearPolygonFlags` 以 O(1) 读写多边形区域与标志；通过 `FindPolygonWithFilter`、`ContainsWithFilter`、`NearestPolygonWithFilter`、`NearestPointWithFilter`、`RaycastWithFilter`、`MoveAlongSurfaceWithFilter`、`RandomPointWithFilter` 与 `RandomPointAroundWithFilter` 执行过滤空间查询。

## 空间查询、表面滑动与零分配路径复用

`pathfind3d.NavMesh` 在构建时自动建立扁平的 3D AABB BVH 空间索引，使 `FindPolygon`、`NearestPolygon`、`NearestPoint` 与寻路起终点定位在大型多层 3D 场景中保持对数级查询开销，不再遍历全部多边形：

```go
rng := random.New(42)

// 在 3D 半尺寸范围内吸附点击或生成点（按 Y 范围干净隔离上下叠层）：
snapped, poly, ok := nav.NearestPoint(spawnPos, math3d.Vec3{X: 3, Y: 1.5, Z: 3})

// 沿斜坡与桥面执行表面射线检测（遇桥边立即阻挡，不会误跳到下方地面）：
hit, obstructed := nav.Raycast(agentPos, targetPos)

// 沿 3D 导航网格表面平滑滑动，持续贴合斜坡高度并防止跌落边缘：
moved, onMesh := nav.MoveAlongSurface(agentPos, desiredPos)

// 按确定性随机流与真实 3D 表面积加权采样随机点（全网或 3D 半径内可达范围）：
patrolPt, _, _ := nav.RandomPoint(rng)
wanderPt, _, _ := nav.RandomPointAround(agentPos, 12, rng)

// 通过 NavMeshQuery 复用缓冲区，实现零堆分配寻路与走廊查询：
query := pathfind3d.NewNavMeshQuery(nav)
pathBuf, ok = query.FindPathInto(pathBuf, snapped, wanderPt)
corridorBuf, _ = query.FindCorridorInto(corridorBuf, snapped, wanderPt)
_, _, _, _, _, _, _ = poly, hit, obstructed, moved, onMesh, patrolPt, ok
```

- **3D 范围限定吸附**：`NearestPoint(p, extent)` 与 `NearestPointWithFilter(p, extent, filter)` 在三维盒 `[p - extent, p + extent]` 内求最近可行走表面点（必要时将斜坡多边形裁剪到范围盒内），天然通过 `extent.Y` 区分上下叠层与立交桥。
- **3D 表面射线与沿面滑动**：`Raycast` / `RaycastWithFilter` 仅穿越 3D 共享边，返回带精确 `HeightAt` 高度与 3D 表面切向内法线的 `NavRaycastHit{Point, Normal, Poly, T, Hit}`；`MoveAlongSurface` / `MoveAlongSurfaceWithFilter` 沿边界平滑滑动并贴合斜坡与桥面，绝不在叠层间误跳（返回 `NavMoveResult{Point, Poly, Visited, HitBoundary}`）。
- **跨平台确定性 3D 表面积采样**：`RandomPoint` / `RandomPointWithFilter` 按真实三维表面积（而非仅 XZ 投影面积）加权采样；`RandomPointAround` / `RandomPointAroundWithFilter` 仅在从起点连通可达且位于三维欧氏 `radius` 内的多边形上采样，绝不会采到头顶未连通的楼层。相同 `*random.Stream` 种子下跨运行与跨平台按位一致。
- **零堆分配路径复用与并发查询**：`nav.FindPathInto`、`nav.FindPathWithFilterInto`、`nav.FindPathWithFilterAndOptionsInto` 与 `nav.FindCorridorInto` 使用带代数戳的复用缓冲区，预热后重复调用均为 `0` 堆分配；多线程并发只读查询同一 `NavMesh` 时，请为每个工作协程创建独立的 `NewNavMeshQuery(nav)`。

面对不可达目标或需要按帧预算分摊多智能体寻路时，可向 `nav.FindPathWithOptions(startPos, goalPos, opts)` 传入 `pathfind.PathOptions{MaxExpansions, AllowPartial}`（返回包含 `Partial`、`Exhausted` 与 `Expansions` 的 `NavPathResult`），或通过 `nav.StartSearch(startPos, goalPos, opts)` 创建可跨帧步进的 `*pathfind3d.NavMeshSearch`：

```go
search := nav.StartSearch(startPos, goalPos, pathfind.PathOptions{
	MaxExpansions: 128,
	AllowPartial:  true,
})
// 可直接手动 Step，也可与 2D 寻路一起提交到 pathfind.PathQueue 统一限额调度：
queue.Submit(search, 0, func(_ pathfind.PathRequestID, _ pathfind.SearchStatus) {
	res := search.Result()
	if res.Found {
		follow3DWaypoints(res.Path) // 目标不可达或预算耗尽时 res.Partial 为 true
	}
})
```

按任意正数步长分帧推进 `NavMeshSearch` 都会得到与 `FindPathWithOptions` 完全一致的 3D 贴地路径，部分路径会终止于已探索多边形上离目标最近的 3D 点。

若要从关卡几何体烘焙这些多边形而不是手写，请使用 [navbake3d](navbake3d.md)：它为每种代理类型光栅化 glTF 模型、地形、`GridMap` 和物理碰撞体，`Mesh.NavMesh` 返回对应的 `NavMesh`。

## 3D 导航智能体（`NavAgent`、`NavAgentGroup`）

`pathfind3d.NavAgent` 在 `pathfind3d.NavMesh` 上维护并局部修复 3D 多边形走廊，在水平 XZ 平面朝下一个漏斗拐角平滑转向，同时沿斜坡与多层桥梁精确跟踪地表高度（`NavPoly.HeightAt`），进入 `ArrivalRadius` 后平滑减速直至停稳于 `StoppingDistance` 内，并暴露明确的导航状态（`NavAgentIdle`、`NavAgentMoving`、`NavAgentArrived`、`NavAgentUnreachable`）：

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
		// 驱动自定义跳跃或攀爬动作，完成后返回 true。
		return true
	},
})
agent.SetTarget(goalPos)

// 在 game.Loop.Update 中：
agent.SetTarget(movingTargetPos)
group.Step(dt)
```

- **局部走廊修复与多层高度隔离**：`NavAgent` 优先检查通过共享边相连的邻居，确保桥面上的智能体不会误吸到下方地面；被推离走廊（`SetPosition`）或目标移至邻近多边形时，在 `MaxRepairDepth`（默认 2）跳范围内局部修复走廊，无需执行完整 A* 重规划。
- **节流与错峰规划**：`NavAgentGroup.Step(dt)` 按确定性轮询顺序（可选配合 `StaggerStride` 错峰）严格将每步完整 A* 重规划次数限制在 `MaxReplansPerStep` 以内，并通过 `NavMesh.Revision()` 自动感知导航网格变更（包含运行时区域/标志修改与离网连接）。
- **外部避障速度**：`DesiredVelocity()` 返回 `Step` 本步要转向的 XZ 速度（空闲、不可达、已到达或正在穿越连接时为零），供 `ai.ORCASolver` 的 XZ 求解使用；`StepWithVelocity(v, dt)` 则让独立智能体改用选定的速度移动：`v.Y` 被忽略，因为高度跟随所走的表面；速度在 `MaxAcceleration` 限制下趋近、上限为 `MaxSpeed`，走廊修复、表面高度跟踪、连接穿越与到达判定与 `Step` 一致。`NavAgentGroup` 中的智能体由 `group.Step` 移动。
- **确定性固定步长重放**：重放相同输入序列时，每一步 3D 坐标均按位完全一致。

## 分块 3D NavMesh、动态障碍物与区块流式加载（`TiledNavMesh`、`NavTileStreamer`）

针对开放世界、开关门、建造物与可破坏掩体，`pathfind3d.TiledNavMesh` 将 3D 可行走多边形切分为与 `world3d.ChunkCoord` 一一对齐的固定尺寸 XZ 瓦片，跨瓦片边界在三维空间精确连接共享边（保持斜坡与上下叠层隔离），动态障碍物变更时仅重建受影响的瓦片，并自动使经过变更或卸载瓦片的路径失效：

```go
tm := pathfind3d.NewTiledNavMesh(pathfind3d.TiledNavMeshConfig{TileSize: 64})

// 跟随 world3d 区块流式加载：在后台工作协程烘焙瓦片，随区块自动载入与卸载：
streamer, err := pathfind3d.NewNavTileStreamer(pathfind3d.NavTileStreamerConfig{
	Mesh:         tm,
	LoadRadius:   2,
	UnloadRadius: 3,
	Workers:      2,
	Bake: func(ctx context.Context, coord world3d.ChunkCoord, bounds math3d.AABB) (*pathfind3d.NavTile, error) {
		return bakeChunkNavTile(ctx, coord, bounds) // 例如 pathfind3d.BakeNavTile 或 navbake3d.BakeTile
	},
})
if err != nil {
	return err
}
defer streamer.Close()

streamer.Update(world3d.WorldPos64{X: float64(player.X), Z: float64(player.Z)})

// 放入 3D 盒形、圆柱或拉伸多边形障碍物（仅重建受影响的瓦片）：
obsID, _ := tm.AddObstacle(pathfind3d.NavBoxObstacle(minPos, maxPos))
detour, _ := tm.FindPath(startPos, goalPos) // 自动绕开障碍物；按 Y 高度范围隔离，不影响上方桥面
tm.RemoveObstacle(obsID)                    // 移除后立即恢复原始几何与路径
_, _ = detour, obsID
```

- **独立瓦片生命周期**：可通过 `pathfind3d.BakeNavTile`（或通过 `navbake3d.BakeTile` 直接从 `navbake3d.Geometry` 按区块体素烘焙）在后台线程独立烘焙瓦片，再用 `AddTile`、`ReplaceTile`、`SetTile`、`RemoveTile` 或 `RebuildTile` 安装与更新；`BuildTiledNavMesh` / `BuildTiledNavMeshWithAreas` 与 `navbake3d.Mesh.TiledNavMesh(tileSize)` 可直接从已有几何或烘焙网格构建分块网格。
- **3D 动态障碍物**：`NavBoxObstacle`、`NavOrientedBoxObstacle`、`NavCylinderObstacle` 与 `NavPolygonObstacle` 均按 Y 轴范围判定高度重叠，地面障碍绝不会误挖上方桥面。同时支持 `NavObstacleCarve`（精确平面凸挖空）与 `NavObstacleMark`（沿轮廓切分并设置 `NavFlagDisabled` 或自定义 `Area`/`Flags`）；多障碍物同帧更新可用 `tm.Batch(func() { ... })` 合批。
- **两级 3D 空间索引与路径失效**：`FindPolygon`、`NearestPoint`、`NearestPolygon` 与 `Raycast` 使用 O(1) XZ 瓦片查找 + 单瓦片扁平 3D BVH（无界最近点查询走粗粒度 3D 瓦片 BVH），无需扫描整张网格。通过 `tm.IsPathValid(res)`（检查 `TiledNavPathResult.Tiles`）判断路径有效性，用 `tm.WatchPath(res.TileCoords(), fn)` 仅订阅经过对应瓦片的失效回调，或用 `tm.AddTileListener` 监听所有瓦片事件。
