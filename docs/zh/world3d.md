# world3d

`game/world3d` 基于 [math3d](math3d.md) 提供 3D 世界构建原语：高度图地形、多层立体 3D 导航网格、模块化 3D 网格 `GridMap`、带视锥与距离裁剪的程序化植被散布，以及浮点原点重置与大世界区块流式加载。

```go
import "github.com/nsgo-dev/game/packages/game/world3d"
```

## 高度图地形（`Heightmap`）

通过 `LoadHeightmapGray` 或 `DecodeHeightmapPNG` 加载 16 位（或 8 位）灰度 PNG 高度图（或用 `NewHeightmap` 创建平地），即可双线性采样高度与法线、对地形三角形做射线检测、分块生成 `TerrainMesh` 网格，并通过 `ToGray16` / `EncodePNG` / `EncodeHeightmapPNG` 无损写回 16 位灰度 PNG（避免 8 位斜坡阶梯带状伪影）：

```go
hm := world3d.LoadHeightmapGray(grayImg, 2.0, 40.0)

y, ok := hm.HeightAt(player.X, player.Z)
normal := hm.NormalAt(player.X, player.Z)
hit, hitOK := hm.Raycast(ray, 500)

// 生成 32x32 网格区块供 GPU 上传：
chunkMesh := hm.BuildMesh(0, 0, 32, 32)
// 或直接桥接为 physics3d.HeightfieldCollider：
hfCollider := hm.Collider()
_, _, _, _, _, _ = y, ok, normal, hit, chunkMesh, hfCollider
_ = hitOK
```

## 地形多级细节（`TerrainLOD`）

大型地形若全部按满分辨率构建，远景会消耗数千万个三角形。`TerrainLOD` 将 `Heightmap` 划分为固定大小的区块（默认 64×64 格），依据相机到区块 AABB 的距离及迟滞（hysteresis）为每个区块选择 2 的幂顶点步长（`1, 2, 4, ...`），支持通过全局 [quality](quality.md) 画质预设（`TerrainLODBiasFor`）施加偏移，用区块 AABB 执行视锥裁剪，并为可见区块缓存无缝缝合的 `TerrainMesh` 网格：

```go
terrain := world3d.NewTerrainLOD(hm, world3d.TerrainLODConfig{
	ChunkCells: 64,
	Hysteresis: 0.10, // 各 LOD 距离阈值周围 10% 迟滞死区
	LODBias:    world3d.TerrainLODBiasFor(sel.Level()),
})

// 每帧更新（若使用 FloatingOrigin，可改用 UpdateWithOrigin）：
visibleChunks := terrain.Update(camPos, &frustum)
for _, ch := range visibleChunks {
	if ch.Changed {
		uploadChunkMesh(ch.Coord, ch.Mesh, ch.Revision)
	}
	drawTerrainChunk(ch.Coord, ch.Transform)
}
```

- **无缝接缝**：当区块的北、南、西、东任一相邻区块处于更粗糙的 LOD 时，`BuildMeshLOD` 会重新三角化该边界条带并直接连接到邻块的粗顶点，在任意 LOD 组合下（包括高度图边缘的非满尺寸区块）同时消除高度裂缝与 T 型接缝。
- **画质预设偏移**：玩家切换画质档位时可调用 `terrain.SetQualityLevel(level)`（或 `terrain.SetLODBias(bias)`）。`TerrainLODBiasFor` 为 `quality.Low` 返回 `+1.0`、`quality.Medium` 返回 `+0.5`、`quality.High` 返回 `0.0`，并按 `2^(-bias)` 缩放各层距离阈值。
- **浮点原点与流式加载**：设置 `WorldOrigin` 并调用 `UpdateWithOrigin(camLocal, &frustumLocal, origin)` 后，LOD 距离始终在 `float64` 世界空间计算，浮点原点重置不会触发 LOD 抖动或网格重建，而返回的 `Bounds` 与 `Transform` 会自动跟踪局部渲染空间。开启 `StreamManaged: true` 并将 `SetChunkActive` 接入 `ChunkStreamerConfig.OnLoad` / `OnUnload`（配合 `ChunkSize: terrain.ChunkWorldSize()`）即可按需流式启用区块。
- **直接构建网格**：`hm.BuildMeshLOD(col, row, cols, rows, lod, neighbors)` 与 `hm.ChunkBounds(col, row, cols, rows)` 均为只读操作，可在后台流式工作协程中并发调用。

## 地形材质层与混合权重图（`TerrainSplatMap`、`TerrainDoc`）

使用 1 到 2 张 RGBA 权重图（splat map）在高度图上混合最多 8 层 PBR 材质（第 0 张图的 R、G、B、A 通道对应第 0..3 层，第 1 张图对应第 4..7 层），在带版本的 YAML 地形定义文件（`*.terrain.yaml`）中声明各层参数，通过 `Heightmap.LayerAt(x, z)` 在玩法逻辑中查询脚下地表，并配合 [render3d](render3d.md) 的 `TerrainMaterial` / `GPUTerrainMaterial` 进行光照与阴影渲染：

```go
doc, err := world3d.ParseTerrain(yamlBytes) // 严格校验的第 1 版 YAML（EncodeTerrain 输出规范格式）
if err != nil {
	return err
}

splat, err := world3d.LoadTerrainSplatMap(doc.LayerInfos(), splatImg0) // 5..8 层时传入 splatImg0, splatImg1
if err != nil {
	return err
}
splat.HeightBlendContrast = doc.HeightBlendContrast
hm.Splat = splat

// 面向脚步声、粒子与摩擦力的零分配玩法查询：
sample, inBounds := hm.LayerAt(player.X, player.Z)
playFootstep(sample.Name, sample.Dominant, sample.Weight(sample.Dominant))
_ = inBounds
```

- **双线性采样与边界钳制**：`TerrainSplatMap.SampleUV` 与 `Heightmap.LayerAt` 采用与 GPU 线性边缘钳制采样一致的纹素中心双线性插值；坐标超出高度图范围时，`Heightmap.LayerAt` 返回 `ok = false` 以及钳制到边缘的采样结果。
- **基于高度图的锐利混合**：为各层附加灰度高度图（`LoadTerrainHeightLayer`）并将 `HeightBlendContrast` 设为 `(0, 1]` 后，可通过与 `render3d` 地形着色器完全一致的代数公式 `BlendTerrainWeights` 锐化层间过渡（如岩石从泥土中突出）。
- **纯文本定义**：`ParseTerrain` / `EncodeTerrain` 读写第 1 版 YAML（`https://game.nsgo.dev/schemas/terrain.v1.json`），声明 `size`（`[cols, rows]`）、`heightmap`（16 位灰度 PNG）、`cellSize`、`heightScale`、`origin`、`chunkSize`（每个 LOD 区块边长的格数，默认 `64`）、打包 RGBA `splatMaps` 或逐层灰度 `weightMap` PNG（`PackLayerWeightMaps`、`LoadTerrainSplatMapFromWeights`）、`heightBlendContrast`、`triplanar`、1–8 个 `layers`（支持可选 `material` 材质资源路径、`weightMap`、`uvScale`、PBR 贴图与参数和 `heightBias`），以及可选内嵌的 `foliage` 植被散布层与灰度 `densityMap` PNG（`DensityMap.EncodePNG`、`Heightmap.FoliageDensityAt`、`TerrainDoc.ScatterFoliage`）。未知字段、重名层、混用 `splatMaps` 与逐层 `weightMap`、尺寸不匹配及权重图数量不足均会返回 `*loaderr.Error`。调用 `doc.BuildHeightmap(imgs)` / `doc.BuildLOD(hm)`（或 `world3d.LoadTerrainFS(fsys, path)`）可直接从 YAML 构建运行时 `*Heightmap`、关联的 `*TerrainSplatMap`、植被密度图与 `*TerrainLOD`。`game check` 会检查所有 `*.terrain.yaml` 文件及其引用的高度图、权重图、材质资源、各层贴图以及植被网格和密度图。

## 多层 3D 导航网格（`NavMesh`）

用凸可行走多边形（斜坡、桥梁、多层楼板）构建 3D `NavMesh`。`BuildNavMesh` 会自动过滤超过 `maxSlopeRad` 的陡坡、在 3D 空间连接共享边，并在 `FindPath` 中执行门户 A* 与 3D 路径拉直：

```go
nav := world3d.BuildNavMesh(walkablePolys, math3d.DegToRad(45))
if path, ok := nav.FindPath(startPos, goalPos); ok {
	follow3DWaypoints(path)
}
```

`nav.FindPolygon(p)` 会在 `VerticalTolerance` 垂直容差内定位最近的可行走多边形（天然支持上下叠层与立交桥）；也可用 `Contains(p)` 与 `NearestPolygon(p)` 检查点是否在网格上或寻找最近表面点，`FindCorridor(start, goal)` 查询多边形走廊，`AddOffMeshLink(NavOffMeshLink{...})` / `OffMeshLinks()` / `ClearOffMeshLinks()` 管理跳跃、梯子或传送等离网连接，`SetEdgeFlags(fromPoly, toPoly, flags)` 为特殊共享边设置标记。

### 离网连接（跳跃、梯子与传送门）

使用 `AddOffMeshLink` 连接垂直分离的平台边缘、不连通的孤岛、梯子或传送门：

```go
nav.AddOffMeshLink(world3d.NavOffMeshLink{
	Start:         math3d.Vec3{X: 9, Y: 4, Z: 5},
	End:           math3d.Vec3{X: 13, Y: 0, Z: 5},
	Radius:        0.5, // 在保持上下楼层隔离的前提下将边缘外的端点吸附到最近多边形
	Bidirectional: false,
	Cost:          0, // <= 0 时使用 3D 欧氏距离
	Area:          world3d.NavAreaJump,
	Flags:         uint32(world3d.NavFlagJump),
	ID:            7,
	UserData:      "drop",
})

res := nav.FindPathWithOptions(startPos, goalPos, ai.PathOptions{})
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

### 多边形区域代价、运行时标志与查询过滤器

使用 `BuildNavMeshWithAreas` 和 `NavPolygonInput{Vertices, Area, Flags}` 在构建时为 3D 多边形设置区域与通行标志。`world3d.NavArea`、`world3d.NavFlags` 和 `world3d.NavFilter` 直接作为 [ai](ai.md) 中对应类型的别名，因此同一套区域常量、标志位和过滤器可在 2D 与 3D 导航网格间无缝复用：

```go
nav := world3d.BuildNavMeshWithAreas([]world3d.NavPolygonInput{
	{Vertices: grassQuad, Area: world3d.NavAreaGrass},
	{Vertices: roadQuad, Area: world3d.NavAreaRoad},
	{Vertices: waterQuad, Area: world3d.NavAreaWater, Flags: world3d.NavFlagSwim},
	{Vertices: gateQuad, Area: world3d.NavAreaDoor, Flags: world3d.NavFlagWalk | world3d.NavFlagDoor},
}, math3d.DegToRad(45))

filter := world3d.DefaultNavFilter()
filter.SetAreaCost(world3d.NavAreaRoad, 0.5)
filter.ExcludeFlags |= world3d.NavFlagSwim

path, ok := nav.FindPathWithFilter(startPos, goalPos, filter)

// 运行时 O(1) 开关门或吊桥多边形，无需重建导航网格：
nav.AddPolygonFlags(gatePolyIndex, world3d.NavFlagDisabled)
detour, ok := nav.FindPathWithFilter(startPos, goalPos, filter)
nav.ClearPolygonFlags(gatePolyIndex, world3d.NavFlagDisabled)
_, _, _ = path, detour, ok
```

- **默认兼容**：`BuildNavMesh` 为每个可行走 `NavPoly` 分配 `NavAreaDefault` 与 `NavFlagWalk`，`FindPath` 使用 `DefaultNavFilter()` 调用 `FindPathWithFilter`。
- **可采纳 3D 加权门户 A\***：每个多边形内的移动距离按 `filter.AreaCost(poly.Area)` 加权，并以网格内允许区域的最小代价缩放 3D 距离启发值，随后沿斜坡与多层桥梁执行精确的 3D 漏斗平滑。
- **运行时修改与过滤查询**：通过 `PolygonCount`、`PolygonArea`、`SetPolygonArea`、`PolygonFlags`、`SetPolygonFlags`、`AddPolygonFlags` 和 `ClearPolygonFlags` 以 O(1) 读写多边形区域与标志；通过 `FindPolygonWithFilter`、`ContainsWithFilter`、`NearestPolygonWithFilter`、`NearestPointWithFilter`、`RaycastWithFilter`、`MoveAlongSurfaceWithFilter`、`RandomPointWithFilter` 与 `RandomPointAroundWithFilter` 执行过滤空间查询。

### 空间查询、表面滑动与零分配路径复用

`world3d.NavMesh` 在构建时自动建立扁平的 3D AABB BVH 空间索引，使 `FindPolygon`、`NearestPolygon`、`NearestPoint` 与寻路起终点定位在大型多层 3D 场景中保持对数级查询开销，不再遍历全部多边形：

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
query := world3d.NewNavMeshQuery(nav)
pathBuf, ok = query.FindPathInto(pathBuf, snapped, wanderPt)
corridorBuf, _ = query.FindCorridorInto(corridorBuf, snapped, wanderPt)
_, _, _, _, _, _, _ = poly, hit, obstructed, moved, onMesh, patrolPt, ok
```

- **3D 范围限定吸附**：`NearestPoint(p, extent)` 与 `NearestPointWithFilter(p, extent, filter)` 在三维盒 `[p - extent, p + extent]` 内求最近可行走表面点（必要时将斜坡多边形裁剪到范围盒内），天然通过 `extent.Y` 区分上下叠层与立交桥。
- **3D 表面射线与沿面滑动**：`Raycast` / `RaycastWithFilter` 仅穿越 3D 共享边，返回带精确 `HeightAt` 高度与 3D 表面切向内法线的 `NavRaycastHit{Point, Normal, Poly, T, Hit}`；`MoveAlongSurface` / `MoveAlongSurfaceWithFilter` 沿边界平滑滑动并贴合斜坡与桥面，绝不在叠层间误跳（返回 `NavMoveResult{Point, Poly, Visited, HitBoundary}`）。
- **跨平台确定性 3D 表面积采样**：`RandomPoint` / `RandomPointWithFilter` 按真实三维表面积（而非仅 XZ 投影面积）加权采样；`RandomPointAround` / `RandomPointAroundWithFilter` 仅在从起点连通可达且位于三维欧氏 `radius` 内的多边形上采样，绝不会采到头顶未连通的楼层。相同 `*random.Stream` 种子下跨运行与跨平台按位一致。
- **零堆分配路径复用与并发查询**：`nav.FindPathInto`、`nav.FindPathWithFilterInto`、`nav.FindPathWithFilterAndOptionsInto` 与 `nav.FindCorridorInto` 使用带代数戳的复用缓冲区，预热后重复调用均为 `0` 堆分配；多线程并发只读查询同一 `NavMesh` 时，请为每个工作协程创建独立的 `NewNavMeshQuery(nav)`。

面对不可达目标或需要按帧预算分摊多智能体寻路时，可向 `nav.FindPathWithOptions(startPos, goalPos, opts)` 传入 `ai.PathOptions{MaxExpansions, AllowPartial}`（返回包含 `Partial`、`Exhausted` 与 `Expansions` 的 `NavPathResult`），或通过 `nav.StartSearch(startPos, goalPos, opts)` 创建可跨帧步进的 `*world3d.NavMeshSearch`：

```go
search := nav.StartSearch(startPos, goalPos, ai.PathOptions{
	MaxExpansions: 128,
	AllowPartial:  true,
})
// 可直接手动 Step，也可与 2D 寻路一起提交到 ai.PathQueue 统一限额调度：
queue.Submit(search, 0, func(_ ai.PathRequestID, _ ai.SearchStatus) {
	res := search.Result()
	if res.Found {
		follow3DWaypoints(res.Path) // 目标不可达或预算耗尽时 res.Partial 为 true
	}
})
```

按任意正数步长分帧推进 `NavMeshSearch` 都会得到与 `FindPathWithOptions` 完全一致的 3D 贴地路径，部分路径会终止于已探索多边形上离目标最近的 3D 点。

若要从关卡几何体烘焙这些多边形而不是手写，请使用 [navbake3d](navbake3d.md)：它为每种代理类型光栅化 glTF 模型、地形、`GridMap` 和物理碰撞体，`Mesh.NavMesh` 返回对应的 `NavMesh`。

### 3D 导航智能体（`NavAgent`、`NavAgentGroup`）

`world3d.NavAgent` 在 `world3d.NavMesh` 上维护并局部修复 3D 多边形走廊，在水平 XZ 平面朝下一个漏斗拐角平滑转向，同时沿斜坡与多层桥梁精确跟踪地表高度（`NavPoly.HeightAt`），进入 `ArrivalRadius` 后平滑减速直至停稳于 `StoppingDistance` 内，并暴露明确的导航状态（`NavAgentIdle`、`NavAgentMoving`、`NavAgentArrived`、`NavAgentUnreachable`）：

```go
group := world3d.NewNavAgentGroup(world3d.NavAgentGroupConfig{
	MaxReplansPerStep: 4,
	ReplanInterval:    100 * time.Millisecond,
})
agent := group.Add(nav, startPos, world3d.NavAgentConfig{
	MaxSpeed:         6,
	MaxAcceleration:  30,
	Radius:           0.4,
	ArrivalRadius:    1.5,
	StoppingDistance: 0.35,
	OnLink: func(a *world3d.NavAgent, ev world3d.NavLinkEvent, dt time.Duration) bool {
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
- **确定性固定步长重放**：重放相同输入序列时，每一步 3D 坐标均按位完全一致。

### 分块 3D NavMesh、动态障碍物与区块流式加载（`TiledNavMesh`、`NavTileStreamer`）

针对开放世界、开关门、建造物与可破坏掩体，`world3d.TiledNavMesh` 将 3D 可行走多边形切分为与 `ChunkCoord` 一一对齐的固定尺寸 XZ 瓦片，跨瓦片边界在三维空间精确连接共享边（保持斜坡与上下叠层隔离），动态障碍物变更时仅重建受影响的瓦片，并自动使经过变更或卸载瓦片的路径失效：

```go
tm := world3d.NewTiledNavMesh(world3d.TiledNavMeshConfig{TileSize: 64})

// 跟随 world3d 区块流式加载：在后台工作协程烘焙瓦片，随区块自动载入与卸载：
streamer, err := world3d.NewNavTileStreamer(world3d.NavTileStreamerConfig{
	Mesh:         tm,
	LoadRadius:   2,
	UnloadRadius: 3,
	Workers:      2,
	Bake: func(ctx context.Context, coord world3d.ChunkCoord, bounds math3d.AABB) (*world3d.NavTile, error) {
		return bakeChunkNavTile(ctx, coord, bounds) // 例如 world3d.BakeNavTile 或 navbake3d.BakeTile
	},
})
if err != nil {
	return err
}
defer streamer.Close()

streamer.Update(world3d.WorldPos64{X: float64(player.X), Z: float64(player.Z)})

// 放入 3D 盒形、圆柱或拉伸多边形障碍物（仅重建受影响的瓦片）：
obsID, _ := tm.AddObstacle(world3d.NavBoxObstacle(minPos, maxPos))
detour, _ := tm.FindPath(startPos, goalPos) // 自动绕开障碍物；按 Y 高度范围隔离，不影响上方桥面
tm.RemoveObstacle(obsID)                    // 移除后立即恢复原始几何与路径
_, _ = detour, obsID
```

- **独立瓦片生命周期**：可通过 `world3d.BakeNavTile`（或通过 `navbake3d.BakeTile` 直接从 `navbake3d.Geometry` 按区块体素烘焙）在后台线程独立烘焙瓦片，再用 `AddTile`、`ReplaceTile`、`SetTile`、`RemoveTile` 或 `RebuildTile` 安装与更新；`BuildTiledNavMesh` / `BuildTiledNavMeshWithAreas` 与 `navbake3d.Mesh.TiledNavMesh(tileSize)` 可直接从已有几何或烘焙网格构建分块网格。
- **3D 动态障碍物**：`NavBoxObstacle`、`NavOrientedBoxObstacle`、`NavCylinderObstacle` 与 `NavPolygonObstacle` 均按 Y 轴范围判定高度重叠，地面障碍绝不会误挖上方桥面。同时支持 `NavObstacleCarve`（精确平面凸挖空）与 `NavObstacleMark`（沿轮廓切分并设置 `NavFlagDisabled` 或自定义 `Area`/`Flags`）；多障碍物同帧更新可用 `tm.Batch(func() { ... })` 合批。
- **两级 3D 空间索引与路径失效**：`FindPolygon`、`NearestPoint`、`NearestPolygon` 与 `Raycast` 使用 O(1) XZ 瓦片查找 + 单瓦片扁平 3D BVH（无界最近点查询走粗粒度 3D 瓦片 BVH），无需扫描整张网格。通过 `tm.IsPathValid(res)`（检查 `TiledNavPathResult.Tiles`）判断路径有效性，用 `tm.WatchPath(res.TileCoords(), fn)` 仅订阅经过对应瓦片的失效回调，或用 `tm.AddTileListener` 监听所有瓦片事件。

## 模块化 3D `GridMap`

在整数格点 `GridCoord{X, Y, Z}` 上摆放可复用的 3D 网格瓦片，支持 24 种立方体正交朝向（`GridOrientationMat4`、`RotateGridOrientation`、`FindGridOrientation`）、碰撞盒提取、按区块裁剪的射线检测（沿射线对区块排序，再逐个测试区块中被占用的格子）、格子遍历（`EachCell`）、按 ID 排序的网格库枚举（`MeshLibrary.Items`）以及带视锥裁剪的实例化合批。`LoadMeshLibraryFromGLTF` 同时支持读取 `.gltf` JSON 和二进制 `.glb` 网格库：

```go
lib, err := world3d.LoadMeshLibraryFromGLTF(gltfBytes) // 或使用 world3d.NewMeshLibrary()
if err != nil {
	return err
}
grid := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, lib)
floorDef, _ := lib.ByName("stone_floor")
grid.FillBox(world3d.GridCoord{X: 0, Y: 0, Z: 0}, world3d.GridCoord{X: 15, Y: 0, Z: 15}, floorDef.ID, 0)

batches = grid.BuildInstances(&frustum, batches[:0])
colliders := grid.CollisionBoxesIn(playerAABB)
hit, ok := grid.Raycast(ray, 100)
_, _, _, _ = batches, colliders, hit, ok
```

如需将 `GridMap` 中各瓦片的 `ColliderBoxes` 加入 [`physics3d.World`](physics3d.md)，调用 `grid.AddCollidersToWorld(world, opts)`（或 `grid.BuildStaticBodies(opts)` / `grid.MergedColliderBoxes()`）。该方法会通过 `physics3d.MergeBoxes` 按 X → Z → Y 顺序贪心合并相邻对齐的格子碰撞盒，在不改变碰撞结果的前提下大幅减少静态刚体数量并消除格子间内部接缝（如需逐格生成独立刚体，可设置 `GridColliderOptions{DisableMerge: true}`）。

网格地图也可以用第 1 版 YAML 编写和保存（`*.gridmap.yaml`，Schema 为 `https://game.nsgo.dev/schemas/gridmap.v1.json`），声明 `origin`、`cellSize`、`meshLibrary`、互不重叠的填充盒游程（`boxes`）和单个格子（`cells`），均按规范的 `(y, z, x)` 顺序写出：

```go
grid, doc, err := world3d.LoadGridMapFS(projectFS, "levels/dungeon.gridmap.yaml")
if err != nil {
	return err
}
exported, err := grid.ToDoc(doc.Name, doc.MeshLibrary)
if err != nil {
	return err
}
yamlBytes, err := world3d.EncodeGridMap(exported) // 输出确定，ParseGridMap 拒绝未知字段
_, _ = exported, yamlBytes
```

`game check` 会检查所有 `*.gridmap.yaml` 文件（以及场景对象上的 `gridMap` 引用），确认其 `meshLibrary` 存在且可加载（`.gltf` 或 `.glb`），并校验 `boxes[*].tile` 与 `cells[*].tile` 中的每个瓦片名称都在该网格库中存在。场景对象还可以通过 `gridMap` 引用 `.gridmap.yaml` 文件，在 [scenemodel](scenemodel.md) 中自动实例化并在 [3D 编辑器](editor.md#3d-gridmap-绘制899) 中交互式绘制。

## 程序化植被与视锥裁剪（`FoliageField`）

按坡度、海拔和可选的 `DensityMap` 在 `Heightmap` 上确定性散布草丛、岩石或树木，并按 `QualityProfile` 收集视锥与距离裁剪后的可见实例：

```go
field := world3d.Scatter(hm, world3d.FoliageLayer{
	Name:            "grass",
	MeshID:          grassMeshID,
	MaterialID:      grassMatID,
	Density:         1.5,
	MinScale:        0.8,
	MaxScale:        1.3,
	AlignToNormal:   0.6,
	MaxSlopeDeg:     35,
	BaseMaxDistance: 80,
	Seed:            42,
}, hm.Bounds())

quality := world3d.DefaultQualityProfile(world3d.QualityHigh)
visible = field.CollectVisible(camPos, frustum, quality, visible[:0])
```

植被层也可以用第 1 版 YAML 定义（`*.foliage.yaml`，Schema 为 `https://game.nsgo.dev/schemas/foliage.v1.json`），声明每层的 `mesh`、可选的 `material` 与 `densityMap`、`density`（每平方米候选实例数）或 `spacing`（平均世界间距，等价于 `density = 1 / spacing²`，二者择一）、缩放范围（`minScale`、`maxScale`）、`alignToNormal`、坡度范围（`minSlopeDeg`、`maxSlopeDeg`）、海拔范围（`minHeight`、`maxHeight`）、`baseMaxDistance`、`boundingRadius` 和 `seed`：

```go
foliageDoc, err := world3d.LoadFoliageFS(projectFS, "terrain/meadow.foliage.yaml")
if err != nil {
	return err
}
fields, err := foliageDoc.ScatterAll(hm, hm.Bounds(), world3d.FoliageBuildOptions{
	ResolveMesh:     resolveMeshID,
	ResolveMaterial: resolveMaterialID,
	FS:              projectFS,
})
```

`ParseFoliage` 会拒绝未知字段和矛盾配置（如同时设置 `density` 和 `spacing`），`EncodeFoliage` 输出确定性的规范 YAML，`game check` 会检查所有 `*.foliage.yaml` 文件及其引用的地形、网格、材质和密度图。

`world3d.QualityLevelFor(level)` 把全局 [quality](quality.md) 档位映射到同名的植被档位，因此 `DefaultQualityProfile(QualityLevelFor(sel.Level()))` 会跟随玩家的画质预设。`QualityUltra` 仍需显式选择，高于所有预设。

返回的每个 `FoliageInstance` 包含世界变换 `Transform` 以及 `[0, 1]` 范围的平滑远距淡出系数 `Fade`。将 `FoliageLayer.BoundingRadius` 设为缩放为 1 时、以网格轴心为球心并包住整个网格的球半径后，裁剪与可视距离都使用每个实例缩放后的包围球，并从球面最近点开始计算，因此大树只要有一部分在范围内就会保留。未设置时，裁剪假定网格半径为 1，距离从轴心开始计算。

`Heightmap.Raycast` 只遍历射线经过的格子，不会扫描整个高度网格。

## 浮点原点与区块流式加载

使用 `FloatingOrigin`（基于 `float64` 的 `WorldPos64`）消除远离原点时的 `float32` 抖动，并用 `ChunkStreamer[T]` 在后台异步加载和卸载周边区块：

```go
origin := world3d.NewFloatingOrigin(512) // 玩家偏离局部原点超过 512 米时触发重置
streamer := world3d.NewChunkStreamer(world3d.ChunkStreamerConfig[*ChunkData]{
	ChunkSize:               64,
	LoadRadius:              3,
	UnloadRadius:            5,
	MaxIntegrationsPerFrame: 2,
	Workers:                 2,
	Loader:                  loadChunkAsync,
	OnLoad:                  attachChunk,
	OnUnload:                releaseChunk,
})
defer streamer.Close()

// 每帧更新：
if rebased, shift := origin.Update(playerLocalPos); rebased {
	rebaseSceneObjects(shift) // 用 world3d.RebaseMat4(m, shift) 平移矩阵
	playerLocalPos = playerLocalPos.Add(shift) // 玩家与其他物体一起平移
}
streamer.Update(origin.LocalToWorld(playerLocalPos))
```
