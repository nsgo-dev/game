# pathfind

`game/pathfind` 提供可直接运行在 `game.Loop` 固定步长上的确定性 2D 寻路：带视线平滑、支持六边形与交错等距拓扑的网格 `AStar` 寻路、分层寻路 `HPA`、跳点搜索 `JPS`、通用图搜索 `GraphSearch`（支持六边形瓦片地图适配器、航点图与自定义图上的 A*、Dijkstra 和带路径的移动范围查询）、带漏斗平滑、离网连接、多边形区域与过滤器的 2D 导航网格 `NavMesh`、带动态障碍物的分块导航网格、`NavAgent` 路径走廊，以及由 `PathQueue` 调度的有界、部分路径与增量寻路。[navbake](navbake.md) 从关卡几何体烘焙它的导航网格，[ai](ai.md) 让智能体沿它的路径转向，并按路径距离为战术位置打分。

```go
import "github.com/nsgo-dev/game/pkg/pathfind"
```

`pathfind` 不启动后台 goroutine，也不含隐藏的全局随机源。

## 网格 A* 寻路

`AStar` 可在任意 `Grid` 或带权重 `CostGrid`（`BoolGrid`、`CostMap` 或适配器 `GridFunc`）上搜索最低代价路径，并在多次调用间复用内部缓冲区：

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
astar.Diagonal = true // NoCornerCutting 默认为 true，防止斜向穿角

path, cost, ok := astar.Find(grid, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 18, Row: 10})
_, _ = path, cost
_ = ok
```

使用 `astar.FindInto(dst, grid, start, goal)` 可把结果写入调用方提供的 `[]pathfind.Cell` 切片，实现每次寻路零堆分配。若项目尚未接入瓦片地图，可直接使用扁平切片网格 `NewBoolGrid` 或 `NewCostMap`。

### 视线路径平滑与任意角度搜索（Theta*）

网格 A* 产出的逐格路径在使用 `FollowPath` 跟踪时往往呈阶梯状。可使用 `SmoothGridPath` 或复用缓冲区的 `SmoothGridPathInto` 将格子路径转换为平滑的世界坐标航点：

```go
opts := pathfind.GridPathOptions{
	Origin:   core.Point{X: 0, Y: 0},
	CellSize: 16,
	Radius:   6, // 保证圆形碰撞半径与所有阻挡格保持安全距离
}

rawCells, _, ok := astar.FindInto(cellBuf, grid, startCell, goalCell)
if ok {
	waypoints = pathfind.SmoothGridPathInto(waypoints, grid, rawCells, opts)
}
```

- **带半径间隙的视线检查**：只要两个保留航点之间的直线段与任意阻挡格（`!grid.Walkable(col, row)`）的距离都不小于 `Radius`，`SmoothGridPath` / `SmoothGridPathInto` 就会剔除中间冗余格子。矩形瓦片可设置 `CellWidth` / `CellHeight` 覆盖 `CellSize`，也可直接调用 `pathfind.GridLineOfSight(grid, a, b, opts)` 和 `opts.CellCenter(cell)`。
- **零堆分配缓冲区复用**：`SmoothGridPathInto(dst, grid, cells, opts)` 将结果写入 `dst[:0]`，当 `dst` 容量足够时全程零堆分配。
- **任意角度（Theta*）搜索**：在方形网格拓扑（`TopologySquare`、`TopologySquare4`、`TopologySquare8`）上将 `astar.AnyAngle` 设为 `true`，搜索会在格心视线穿过均匀代价可行走格时直接连接祖先节点（遵守 `NoCornerCutting`，且不会抄近路穿过高代价 `CostGrid` 地形）。若需要正值实体 `Radius` 的安全间隙，建议配合常规逐格 `AStar`（`AnyAngle: false`）再调用 `SmoothGridPathInto`，以保留绕角所需的拐点格。

### 六边形与交错等距拓扑

通过 `pathfind.TopologyFromTilemap(m)` 自动匹配任意 `tilemap.Tilemap` 的网格拓扑，或手动指定 `astar.Topology`（`TopologySquare`、`TopologySquare4`、`TopologySquare8`、`TopologyHexPointyOdd`、`TopologyHexPointyEven`、`TopologyHexFlatOdd`、`TopologyHexFlatEven`、`TopologyStaggeredYOdd`、`TopologyStaggeredYEven`、`TopologyStaggeredXOdd`、`TopologyStaggeredXEven`）：

```go
astar := pathfind.NewAStar()
astar.Topology = pathfind.TopologyFromTilemap(hexMap)

path, cost, ok := astar.Find(grid, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 6, Row: 4})
_, _, _ = path, cost, ok
```

- 六边形拓扑使用 6 邻接轴坐标与精确立方距离启发式，与 `tilemap.Tilemap.HexNeighbors` 和 `HexDistance` 完全一致。
- 交错等距拓扑将偏移坐标映射回底层菱形晶格（默认 4 向沿边移动；将 `Diagonal` 设为 `true` 时开放受 `NoCornerCutting` 约束的 8 向顶点对角移动）。
- 当移动代价同时取决于起点与终点（例如高度差、渡河或控制区惩罚）时，可实现 `EdgeCostGrid`（`EdgeCost(fromCol, fromRow, toCol, toRow) float64`）；返回负值、`NaN` 或无穷大表示禁止该边通行。
- 可通过 `topo.Neighbors(c)`、`topo.NeighborsInto(dst, c)` 和 `topo.Distance(a, b)` 直接查询邻格与开阔地步距。

### 移动范围查询

战棋与战术游戏可用 Dijkstra 搜索计算从 `start` 在行动点预算内可到达的所有格子及其最小代价：

```go
reachable := astar.MovementRange(costMap, pathfind.Cell{Col: 2, Row: 2}, 4)
for _, rc := range reachable {
	// rc.Cell 可达，且最小移动代价 rc.Cost <= 4
	_ = rc
}
```

- 返回结果顺序完全确定：按 `Cost` 升序排列，同代价按行主序索引打破平局。
- 使用 `astar.MovementRangeInto(dst, grid, start, budget)` 可复用切片实现零堆分配；使用 `astar.MovementRangeWithCost(dst, grid, start, budget, edgeCost)` 或便捷函数 `pathfind.ReachableCells(grid, topo, start, budget, edgeCost)` 可传入自定义 `func(from, to pathfind.Cell) float64` 边代价回调（支持小于 1 的道路减耗及负值禁行边）。

## 分层寻路（HPA*）与跳点搜索（JPS）

在 1024×1024 等大型策略或开放世界瓦片地图上，逐格 `AStar` 开销较高。`game/pathfind` 提供两种互补的高速网格寻路器，二者在预热后通过 `FindInto` 均可实现零堆分配：

### 分层寻路 HPA*（`HPA`）

`HPA` 将任意 `Grid`、`CostGrid` 或 `EdgeCostGrid` 划分为固定大小的方形簇（`ClusterSize`，默认 `16`），在相邻簇的共享边界上建立入口节点，并预计算簇内入口间的最短路径：

```go
hpa := pathfind.NewHPA(grid, pathfind.HPAOptions{
	ClusterSize: 32,
	Diagonal:    true, // AllowCornerCutting 默认为 false，防止斜向穿角
})

path, cost, ok := hpa.Find(pathfind.Cell{Col: 2, Row: 2}, pathfind.Cell{Col: 1021, Row: 1021})
_, _, _ = path, cost, ok

// 障碍或地形代价变更时，仅标记受影响的格子或矩形区域：
grid.SetWalkable(400, 320, false)
hpa.UpdateCell(400, 320) // 或 hpa.UpdateRect(minC, minR, maxC, maxR)
hpa.RebuildDirty()       // 可选：下次 Find 也会自动增量重建脏簇
```

- **性能与最优性界**：在 1024×1024 多房间基准图上（`BenchmarkHPA1024` 对比 `BenchmarkAStar1024`），`HPA.FindInto` 比 `AStar.FindInto` 快两个数量级以上（约 150×，抽象搜索 `FindAbstractInto` 约 780×），且路径代价稳定控制在最优 A* 代价的 1.15× 以内。
- **增量更新**：`UpdateCell` 与 `UpdateRect` 仅将受影响的簇（以及位于共享边界时紧邻的簇）标脏；`RebuildDirty` 仅重建脏边界与脏簇，绝不整图重建；可通过 `Stats()` 检查簇重建计数。
- **抽象与走廊查询**：使用 `FindAbstract` / `FindAbstractInto` 可跳过逐格细化、直接返回入口航点；使用 `FindClusters` / `FindClustersInto` 可获取经过的 `[]ClusterID` 簇走廊（可直接交给 `rts.SectorFlowField.BuildAlongHPA` 构建分区域流场）。

### 跳点搜索 JPS（`JPS`）

`JPS` 是面向等代价方形网格（`Grid`）的高速 8 方向寻路器，通过跳过开阔空间中的对称节点大幅减少优先队列操作，并保证返回与 `AStar`（`Diagonal: true`）完全一致的精确最优八方向路径代价：

```go
jps := pathfind.NewJPS() // NoCornerCutting 默认为 true
path, cost, ok := jps.Find(grid, start, goal)
waypoints, _, _ := jps.FindJumpPoints(grid, start, goal) // 仅返回拐点跳点
_, _, _ = path, cost, waypoints
```

- 使用 `jps.FindInto(dst, grid, start, goal)` 或 `jps.FindJumpPointsInto(dst, grid, start, goal)` 可复用调用方缓冲区实现零堆分配。
- `JPS` 假设所有可通行格子步长代价均为 `1`；带权重的 `CostGrid` / `EdgeCostGrid` 请使用 `AStar` 或 `HPA`。

## 通用图搜索、六边形适配器与航点图

当需要在六边形瓦片地图、手工摆放的航点网络、3D 格子网格或自定义领域图上寻路时，可使用 `GraphSearch[N]` 搜索任意实现 `Graph[N]`（`AppendNeighbors(dst []GraphEdge[N], node N) []GraphEdge[N]`）或 `HeuristicGraph[N]`（增加 `Heuristic(from, goal N) float64`）的图：

- **复用缓冲区的 A* 与 Dijkstra**：`Find` / `FindInto`、`FindWithHeuristic` / `FindWithHeuristicInto` 与 `FindDijkstra` / `FindDijkstraInto` 在多次调用间复用内部节点、堆、索引表和邻居缓冲区（预热后 `FindInto` 与 `FindDijkstraInto` 零堆分配，`Find` 仅分配返回的路径切片）。平局打破规则与 `AStar` 一致：依次按最低 `f`、最低 `h`、`GraphSearch.Less`（或可选 `NodeOrder[N]`）、确定性发现顺序排序。
- **带路径重建的范围查询**：`Range(g, start, budget)` 与复用缓冲区的 `RangeInto(dst, g, start, budget)` 在预算 `budget` 内运行 Dijkstra，返回 `*GraphRange[N]`，既包含所有可达节点与最小代价（`Nodes()`、`Contains(n)`、`Cost(n)`、`Parent(n)`），又支持直接重建到任意可达节点的最短路径（`Path(n)` 与零堆分配的 `PathInto(dst, n)`）。
- **函数回调图**：可通过 `GraphFunc[N]{Neighbors, Estimate, Valid, Less}` 快速包装临时图，或实现可选接口 `ValidatedGraph[N]` 在搜索前拦截越界、阻挡或已删除的起终点。

### 基于 `tilemap.Tilemap` 的六边形网格适配器

`NewHexGrid(m)` 直接适配任意六边形 `*tilemap.Tilemap`，完整支持全部交错轴（`StaggerX`、`StaggerY`）与奇偶偏移（`StaggerOdd`、`StaggerEven`），内部直接调用 `m.HexNeighbors` 与 `m.HexDistance`：

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

// 回合制战棋移动范围查询，并可直接获取悬停格子的移动路径预览：
moveRange := search.Range(hex, start, 4)
if preview, reachable := moveRange.Path(hoveredCell); reachable {
	_, _, _, _ = path, cost, ok, preview
}
```

可通过 `SolidMask`（默认 `tilemap.TileSolid`）、`Walk`、`Cost` 或有向 `StepCost` 自定义可通行性与逐格/逐边地形代价。`HexGrid` 同时实现了 `Grid`、`CostGrid` 和 `EdgeCostGrid`。

### 航点图（WaypointGraph）

`NewWaypointGraph()` 管理手工摆放的 2D（`Add`、`SetPos`、`Pos`、`Nearest`、`Points` / `PointsInto`）或 3D（`Add3D`、`SetPos3D`、`Pos3D`、`Nearest3D`、`Points3D` / `Points3DInto`）航点，提供稳定整数 ID、自动欧氏距离连边（`Connect`、`ConnectUndirected`）、显式权重的有向/无向边（`AddEdge`、`AddUndirectedEdge`）、临时开关节点（`SetEnabled`）以及删除节点（`Remove`）：

```go
wg := pathfind.NewWaypointGraph()
a := wg.Add(core.Point{X: 0, Y: 0})
b := wg.Add(core.Point{X: 80, Y: 0})
c := wg.Add(core.Point{X: 80, Y: 60})
wg.ConnectUndirected(a, b) // 自动采用欧氏距离代价（80）
wg.ConnectUndirected(b, c) // 自动采用欧氏距离代价（60）
wg.AddEdge(a, c, 120)      // 显式指定有向边代价

search := pathfind.NewGraphSearch[int]()
nodePath, totalCost, ok := search.Find(wg, a, c)
waypoints := wg.Points(nodePath) // 可直接传给 ai.FollowPath
_, _, _ = totalCost, ok, waypoints
```

`WaypointGraph.Heuristic` 返回航点间欧氏距离（在所有边代价不小于欧氏距离时保证可采纳）；若图中包含代价低于直线距离的传送门或捷径边，请调用 `search.FindDijkstra`。
## 带漏斗平滑的 2D NavMesh

对于开放式竞技场或非网格地形，可用凸多边形构建 `NavMesh`。`BuildNavMesh` 会自动统一绕序、沿共享边（含部分共线重叠）连接相邻多边形；`FindPath` 通过门户图 A* 加上 Simple Stupid Funnel Algorithm 拉直路径：

```go
mesh := pathfind.BuildNavMesh([][]core.Point{
	{{X: 0, Y: 0}, {X: 120, Y: 0}, {X: 120, Y: 80}, {X: 0, Y: 80}},
	{{X: 120, Y: 20}, {X: 240, Y: 20}, {X: 240, Y: 100}, {X: 120, Y: 100}},
})

waypoints, ok := mesh.FindPath(core.Point{X: 16, Y: 16}, core.Point{X: 220, Y: 85})
_ = waypoints
_ = ok
```

可通过 `Contains(p)`、`FindPolygon(p)`、`NearestPolygon(p)`、`Neighbors(index)`、`Polygon(index)`、`PolygonCount()` 和 `FindCorridor(start, goal)` 查询网格几何信息；通过 `AddOffMeshLink(NavOffMeshLink{...})` 添加离网连接，通过 `OffMeshLinks()` 与 `ClearOffMeshLinks()` 查询或清除离网连接，通过 `SetEdgeFlags(fromPoly, toPoly, flags)` 标记共享边。

若要把关卡切分成这些多边形，并让路径与墙保持代理半径的距离，可用 [navbake](navbake.md) 从关卡的外轮廓与障碍物或从 tilemap 烘焙网格。

### 离网连接（跳跃、梯子与传送门）

使用 `AddOffMeshLink` 连接不共享边的多边形，例如单向跌落、跨隙跳跃、双向梯子、门或传送门：

```go
mesh.AddOffMeshLink(pathfind.NavOffMeshLink{
	Start:         core.Point{X: 90, Y: 40},
	End:           core.Point{X: 140, Y: 40},
	Radius:        4, // 将距边缘 4 单位以内的端点吸附到多边形，并作为智能体进入半径
	Bidirectional: false,
	Cost:          12, // <= 0 时使用欧氏距离
	Area:          pathfind.NavAreaJump,
	Flags:         uint32(pathfind.NavFlagJump),
	ID:            1,
	UserData:      "drop",
})

res := mesh.FindPathWithOptions(start, goal, pathfind.PathOptions{})
if res.Found {
	for i := 0; i+1 < len(res.Path); i++ {
		if link, ok := res.LinkAt(i); ok {
			// 路径段 Path[link.StartIndex] -> Path[link.EndIndex] 穿越一条离网连接。
			_ = link.UserData
		}
	}
}
```

- **路径段标记**：`NavPathResult.Links` 按路径顺序记录每一段穿越的离网连接（含 `StartIndex`、`EndIndex == StartIndex + 1`、`FromPoly`、`ToPoly`、`Start`、`End`、`Radius`、`Area`、`Flags`、`LinkID`、`UserData`），并提供 `HasLinks()` 与 `LinkAt(segmentIndex)` 辅助方法；共线路径简化不会折叠连接的起止点。
- **捷径感知最优搜索与过滤**：A\* 将离网连接视为有向边（`Bidirectional` 为 `true` 时双向建边），按 `NavFilter` 对其 `Area` 与 `Flags`（含 `NavAreaJump`、`NavAreaClimb`、`NavAreaTeleport`、`NavFlagJump`、`NavFlagClimb`、`NavFlagTeleport`）进行过滤并按 `filter.AreaCost(link.Area)` 缩放代价；当存在代价低于直线距离的传送连接时自动调整启发式以保持可采纳性，确保仅在比步行更便宜时才选用传送连接。
- **YAML 持久化与场景摆放**：可在 2D 烘焙网格 YAML 中通过 `navbake.Source.Links` / `navbake.Mesh.Links` 保存离网连接（`navbake.Mesh.NavMesh` 加载时自动注册），或在场景对象上挂载内置 `"nav_link"` 组件（`sceneformat.RegisterNavLink`、`sceneformat.ExtractNavLinks2D`、`sceneformat.ExtractBuiltNavLinks2D`）。

### 导航智能体与路径走廊（`NavAgent`、`NavAgentGroup`）

`NavAgent` 在 `pathfind.NavMesh` 上维护当前有效的多边形走廊，直接从当前位置沿剩余走廊计算下一个漏斗拐角（`NextCorner`），平滑转向并在 `ArrivalRadius` 内减速直至停稳于 `StoppingDistance` 内，同时暴露明确的导航状态（`NavAgentIdle`、`NavAgentMoving`、`NavAgentArrived`、`NavAgentUnreachable`）：

```go
agent := pathfind.NewNavAgent(mesh, core.Point{X: 16, Y: 16}, pathfind.NavAgentConfig{
	MaxSpeed:         120,
	MaxAcceleration:  480,
	Radius:           10,
	ArrivalRadius:    40,
	StoppingDistance: 8,
	ReplanInterval:   100 * time.Millisecond,
	OnLink: func(a *pathfind.NavAgent, ev pathfind.NavLinkEvent, dt time.Duration) bool {
		// 自定义离网连接或标记边穿越完成时返回 true。
		return true
	},
})
agent.SetTarget(playerPos)

// 在 game.Loop.Update 中：
agent.SetTarget(playerPos)
agent.Step(dt)
```

- **廉价局部走廊修复**：当智能体正常前进、被外力推离原走廊落到相邻多边形（`SetPosition`），或追逐的目标移入相邻多边形时，`NavAgent` 通过有界邻接桥接（最多 `MaxRepairDepth` 跳，默认 2）与环路裁剪在局部修复走廊，无需触发完整 A* 重规划。仅当走廊断裂超过 `MaxRepairDepth`、目标持续漂移或网格 `Revision()` 变化（`AddOffMeshLink`、`SetEdgeFlags`、`BumpRevision` 或 `SetMesh`）时才执行完整重规划。
- **节流与错峰群体规划**：使用 `NewNavAgentGroup(NavAgentGroupConfig{MaxReplansPerStep: 4, ReplanInterval: 100 * time.Millisecond, StaggerStride: 1})` 并在固定步长中调用 `group.Step(dt)`，按确定性轮询顺序（可选配合 `StaggerStride` 错峰）严格将每步完整 A* 重规划次数限制在 `MaxReplansPerStep` 以内，避免大量追击智能体在同一帧造成卡顿尖峰。
- **外部避障速度**：做局部避障时，先读取 `DesiredVelocity()`（`Step` 本步要转向的速度；空闲、不可达、已到达或正在穿越连接时为零），交给 `ai.ORCASolver` 等求解器，再用 `StepWithVelocity(v, dt)` 代替 `Step(dt)`。智能体在 `MaxAcceleration` 限制下趋近 `v`，速度上限为 `MaxSpeed`，并与 `Step` 一样进行走廊修复、沿墙滑动、连接穿越与到达判定；原样传回 `DesiredVelocity()` 时的移动与 `Step` 相同。该接口用于独立智能体：`NavAgentGroup` 中的智能体由 `group.Step` 移动。
- **离网连接与标记边回调**：经过离网连接（`NavLinkOffMesh`）或带标记共享边（`SetEdgeFlags`）时每步调用 `OnLink`；返回 `false` 表示由回调接管跳跃、开门等自定义过渡，返回 `true` 后恢复走廊转向。未设置 `OnLink` 时，离网连接按 `MaxSpeed` 线性穿越。
- **确定性步进**：`Step(dt)` 不使用后台 goroutine、不遍历 Go map、无隐藏随机源；重放相同输入序列每一步位置按位完全一致。

### 多边形区域代价、运行时标志与查询过滤器

构建导航网格时可通过 `BuildNavMeshWithAreas` 与 `NavPolygonInput{Vertices, Area, Flags}` 为每个凸多边形指定区域 ID（`NavAreaDefault`、`NavAreaRoad`、`NavAreaGrass`、`NavAreaMud`、`NavAreaWater`、`NavAreaDoor`、`NavAreaHazard` 或任意自定义 `uint8` `NavArea`）与运行时通行标志（`NavFlagWalk`、`NavFlagSwim`、`NavFlagDoor`、`NavFlagJump`、`NavFlagDisabled`、`NavFlagAll`；`Flags` 为 0 时默认赋值 `NavFlagWalk`）：

```go
mesh := pathfind.BuildNavMeshWithAreas([]pathfind.NavPolygonInput{
	{Vertices: grassPoly, Area: pathfind.NavAreaGrass},
	{Vertices: roadPoly, Area: pathfind.NavAreaRoad},
	{Vertices: waterPoly, Area: pathfind.NavAreaWater, Flags: pathfind.NavFlagSwim},
	{Vertices: doorPoly, Area: pathfind.NavAreaDoor, Flags: pathfind.NavFlagWalk | pathfind.NavFlagDoor},
})

filter := pathfind.DefaultNavFilter()
filter.SetAreaCost(pathfind.NavAreaRoad, 0.5)   // 道路每单位距离代价减半
filter.SetAreaCost(pathfind.NavAreaMud, 2.5)
filter.ExcludeFlags |= pathfind.NavFlagSwim     // 陆地单位绕开水域

path, ok := mesh.FindPathWithFilter(start, goal, filter)

// 在运行时以 O(1) 开关门多边形，无需重建导航网格：
mesh.AddPolygonFlags(doorIndex, pathfind.NavFlagDisabled)
detour, ok := mesh.FindPathWithFilter(start, goal, filter)
mesh.ClearPolygonFlags(doorIndex, pathfind.NavFlagDisabled)
_, _, _ = path, detour, ok
```

- **默认兼容**：`BuildNavMesh` 为所有多边形设置 `NavAreaDefault` 与 `NavFlagWalk`，`FindPath` 使用 `DefaultNavFilter()`（`IncludeFlags: NavFlagAll`、`ExcludeFlags: NavFlagDisabled`、各区域默认代价 `1.0`），与既有行为完全一致。
- **可采纳加权 A\***：`FindPathWithFilter` 按 `filter.AreaCost(area)` 对每个多边形内的移动距离加权，并以网格内可通行区域的最小代价缩放 A\* 距离启发值，确保道路代价低于 `1.0` 时仍能求得最优走廊；非正值、`NaN` 或无穷大代价视为禁止通行。
- **运行时零重建修改**：通过 `PolygonArea`、`SetPolygonArea`、`PolygonFlags`、`SetPolygonFlags`、`AddPolygonFlags` 和 `ClearPolygonFlags` 以 O(1) 修改多边形区域或标志；空间查询 `FindPolygonWithFilter`、`ContainsWithFilter`、`NearestPolygonWithFilter`、`NearestPointWithFilter`、`RaycastWithFilter`、`MoveAlongSurfaceWithFilter`、`RandomPointWithFilter`、`RandomPointAroundWithFilter` 以及带过滤的有界/增量搜索 `FindPathWithFilterAndOptions`、`StartSearchWithFilter` 同样遵循 `NavFilter`。

### 空间查询、表面滑动与零分配路径复用

`pathfind.NavMesh` 在构建时自动建立扁平的 2D AABB BVH 空间索引，使 `FindPolygon`、`NearestPolygon`、`NearestPoint` 及寻路起终点定位在大型导航网格上保持对数级开销，不再全量扫描所有多边形：

```go
rng := random.New(42)

// 在给定半尺寸范围内将点击或生成位置吸附到最近的可通行多边形：
snapped, poly, ok := mesh.NearestPoint(clickPos, core.Point{X: 32, Y: 32})

// 沿导航网格表面检测直线是否畅通：
hit, obstructed := mesh.Raycast(agentPos, targetPos)

// 将移动中的智能体朝 desiredPos 沿可行走表面滑动并钳制在网格内：
moved, onMesh := mesh.MoveAlongSurface(agentPos, desiredPos)

// 按确定性随机流与面积加权采样随机点（全网任意位置或半径内可达位置）：
patrolPt, _, _ := mesh.RandomPoint(rng)
wanderPt, _, _ := mesh.RandomPointAround(agentPos, 96, rng)

// 复用调用方切片与查询对象缓冲区，实现零堆分配寻路与走廊查询：
query := pathfind.NewNavMeshQuery(mesh)
pathBuf, ok = query.FindPathInto(pathBuf, snapped, wanderPt)
corridorBuf, _ = query.FindCorridorInto(corridorBuf, snapped, wanderPt)
_, _, _, _, _, _, _ = poly, hit, obstructed, moved, onMesh, patrolPt, ok
```

- **范围限定吸附**：`NearestPoint(p, extent)` 与 `NearestPointWithFilter(p, extent, filter)` 在 `[p - extent, p + extent]` 盒内寻找允许通行的最近多边形表面点（当重叠多边形的全局最近点落在盒外时自动裁剪到范围盒内）。
- **表面射线与沿面滑动**：`Raycast` / `RaycastWithFilter` 沿共享边门户步进并返回 `NavRaycastHit{Point, Normal, Poly, T, Hit}`（遇阻时给出指向网格内部的单位边法线）；`MoveAlongSurface` / `MoveAlongSurfaceWithFilter` 沿阻挡边界平滑滑动并跨多边形跟踪路径，返回 `NavMoveResult{Point, Poly, Visited, HitBoundary}`。
- **跨平台确定性面积加权随机点**：`RandomPoint` / `RandomPointWithFilter` 在所有允许通行的多边形上按面积均匀采样；`RandomPointAround` / `RandomPointAroundWithFilter` 仅在从 `center` 可达且位于 `radius` 内的连通多边形上采样，绝不会落入断开的孤岛。使用相同 `*random.Stream` 种子时，跨运行与跨平台结果按位一致。
- **零堆分配复用与并发只读查询**：`mesh.FindPathInto`、`mesh.FindPathWithFilterInto`、`mesh.FindPathWithFilterAndOptionsInto` 与 `mesh.FindCorridorInto` 使用带代数戳的复用缓冲区，预热后重复调用均为 `0` 堆分配。在多个 goroutine 上并发查询同一只读 `NavMesh` 时，请为每个工作协程创建独立的 `NewNavMeshQuery(mesh)`（同时提供零分配的 `MoveAlongSurfaceInto` 与缓冲区复用的 `RandomPointAround`）。
- **路径长度与采样点**：`query.PathLength(start, goal)` 返回两点之间经漏斗平滑的路径长度，预热后不再分配内存；`mesh.AppendSamples(dst, center, maxRadius, spacing)` 追加落在网格内的规则网格点（`spacing <= 0` 时改为每个多边形的形心），可选只保留距 `center` 不超过 `maxRadius` 的点。[ai](ai.md) 的 `PathDistanceTest` 与 `NavMeshGenerator` 即基于这两个方法。

### 分块 2D NavMesh 与动态障碍物（`TiledNavMesh`）

当门、可破坏墙壁、建造物或流式区域在运行时改变可行走区域时，可使用 `TiledNavMesh`：它将 2D 多边形切分为固定尺寸的正方形瓦片（`NavTileCoord{X, Y}`），支持独立烘焙（`BakeNavTile` / `BakeTile`）、添加（`AddTile`）、替换（`ReplaceTile` / `SetTile`）、移除（`RemoveTile`）与重建（`RebuildTile`），并自动跨瓦片边界连接多边形，使跨边界路径与未分块 `NavMesh` 保持一致：

```go
tm := pathfind.BuildTiledNavMesh(corridorPolys, 32) // 或 pathfind.NewTiledNavMesh(pathfind.TiledNavMeshConfig{TileSize: 32})

res := tm.FindPathWithOptions(start, goal, pathfind.PathOptions{})
watchID := tm.WatchPath(res.TileCoords(), func(ev pathfind.NavTileChange) {
	// 仅经过变更瓦片 ev.Coord 的路径会收到失效通知。
	replanPath()
})

// 放入盒形、圆形或（凸/凹）多边形障碍物——仅重建受影响的瓦片：
obsID, _ := tm.AddObstacle(pathfind.NavBoxObstacle(minPt, maxPt))
detour, _ := tm.FindPath(start, goal) // 自动绕开障碍物；此时 tm.IsPathValid(res) 为 false

// 移除障碍物后立即恢复原始可行走几何与路径：
tm.RemoveObstacle(obsID)
tm.UnwatchPath(watchID)
_, _ = detour, watchID
```

- **动态障碍物形状与模式**：支持 `NavBoxObstacle`、`NavOrientedBoxObstacle`、`NavCircleObstacle` 与 `NavPolygonObstacle`（兼容凸/凹多边形及可选 `Margin` 膨胀）；默认采用 `NavObstacleCarve`（精确凸多边形挖空，让路径平滑绕行），也可选 `NavObstacleMark` 沿障碍轮廓切分多边形并赋予 `NavFlagDisabled` 或自定义 `Area`/`Flags`（如 `NavAreaMud`）。同一帧内有多处改动时，可用 `tm.Batch(func() { ... })` 合并批量更新，确保每个受影响瓦片最多重建一次。
- **两级空间索引**：`FindPolygon`、`NearestPoint`、`NearestPolygon` 与 `Raycast` 结合 O(1) 瓦片网格定位（大范围/无界查询走粗粒度瓦片 BVH 分支定界）与单瓦片扁平 AABB BVH，无需扫描整张导航网格。
- **按瓦片精准路径失效**：`TiledNavPathResult.Tiles` 记录路径经过的每个瓦片及其版本号，`tm.IsPathValid(res)` 按经过瓦片数 O(K) 判断有效性；`WatchPath` 按瓦片建立反向索引，仅通知经过变更或卸载瓦片的路径（`AddTileListener` 可监听所有瓦片事件）。
- **跨瓦片离网连接**：通过 `AddOffMeshLink(NavOffMeshLink)`（以及 `OffMeshLinks`、`ClearOffMeshLinks`）在 `TiledNavMesh` 上注册单向或双向跳跃、跌落、梯子与传送门，支持端点 `Radius` 吸附、`NavFilter` 区域/标志过滤与代价缩放、捷径感知 A\*，并通过 `TiledNavPathResult.Links`（`HasLinks`、`LinkAt`）返回带 `NavPolyRef` 端点的连接段。连接按候选端点瓦片索引，在端点被障碍物覆盖或瓦片卸载时自动解绑，并在瓦片替换、重建或移除障碍物后自动重新挂接。
- **烘焙桥接**：调用 `navbake.Mesh.TiledNavMesh(tileSize)` 可直接将 2D 烘焙网格转为支持动态障碍物的分块导航网格，并自动注册所有已烘焙的 `Links`。

## 有界、部分路径与增量寻路

点击不可达目标时单位应尽量走近目标，同时大量并发长寻路也不应拖垮单帧预算。`AStar`、2D `NavMesh` 以及 [pathfind3d](pathfind3d.md) 的 3D `NavMesh` 共享 `PathOptions{MaxExpansions, AllowPartial}` 与 `IncrementalSearch` 接口：

```go
// 单次有界搜索，并启用部分路径回退：
res := astar.FindWithOptions(grid, start, goal, pathfind.PathOptions{
	MaxExpansions: 128,
	AllowPartial:  true,
})
if res.Found {
	// 当 res.Partial 为 true 时，res.Path 终止于已探索范围内离目标最近的可行走格子。
	// res.Exhausted 表示搜索是否因触及 MaxExpansions 而提前停止。
	follow(res.Path)
}

// 2D NavMesh 有界 / 部分路径搜索（复用同一 PathOptions）：
navRes := mesh.FindPathWithOptions(startPt, goalPt, pathfind.PathOptions{
	MaxExpansions: 64,
	AllowPartial:  true,
})
_ = navRes
```

- **部分路径**：开启 `AllowPartial: true` 后，当目标被墙围死、位于障碍格或网格外部、或超出 `MaxExpansions` 预算时，`FindWithOptions` / `FindWithOptionsInto`（预热后零堆分配）以及 `NavMesh.FindPathWithOptions` 会返回通往已探索范围内最近格子或最近多边形边界点的合法路径，并标记 `Partial: true`。设置 `AStar.MaxExpansions > 0` 也会限制 `Find` / `FindInto`，并在 `PathOptions.MaxExpansions == 0` 时作为默认预算（传入负值 `PathOptions.MaxExpansions` 可显式覆盖为不限步数）。
- **确定性增量步进**：通过 `astar.StartSearch(grid, start, goal, opts)`（`*AStarSearch`，支持 `Reset` 复用缓冲区）或 `mesh.StartSearch(start, goal, opts)`（`*NavMeshSearch`）启动跨帧搜索，并每帧调用 `Step(maxExpansions)` 推进。无论按任意正数步长切分（哪怕每次仅展开 1 个节点），最终得到的路径、代价、`Partial` 标记与展开次数均与单次搜索完全一致。
- **预算化寻路请求队列（`PathQueue`）**：使用 `NewPathQueue`、`Submit` / `SubmitGrid` / `SubmitNavMesh`、`Cancel` 和 `Step(budget)` / `StepDefault()`，在严格的单步节点展开预算内统一调度网格、2D `NavMesh` 与 `pathfind3d.NavMesh` 寻路请求：

```go
queue := pathfind.NewPathQueue(pathfind.PathQueueConfig{
	DefaultBudget: 64,                     // StepDefault 每步严格不超过该展开次数
	Policy:        pathfind.PathQueueRoundRobin, // 或 pathfind.PathQueueFIFO（默认）
	SliceSize:     16,                     // 轮转模式下单次时间片上限（0 表示均分剩余预算）
})

reqID, search := queue.SubmitGrid(astar, grid, start, goal, pathfind.PathOptions{
	AllowPartial: true,
}, 1, func(id pathfind.PathRequestID, res pathfind.GridPathResult) {
	assignUnitPath(res.Path, res.Partial)
})

// 在 game.Loop.Update 中：
_ = queue.StepDefault() // 绝不超过 DefaultBudget 次展开
_ = search.Status()     // 可直接轮询状态，或通过 queue.Cancel(reqID) 取消请求
_ = reqID
```

高优先级请求优先执行，同优先级内按确定性 FIFO 或轮转顺序推进，取消后的请求不会继续步进也不会触发回调。
