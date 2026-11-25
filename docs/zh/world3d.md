# world3d

`game/world3d` 基于 [math3d](math3d.md) 提供 3D 世界构建原语：高度图地形、多层立体 3D 导航网格、模块化 3D 网格 `GridMap`、带视锥与距离裁剪的程序化植被散布，以及浮点原点重置与大世界区块流式加载。

```go
import "github.com/nsgo-dev/game/packages/game/world3d"
```

## 高度图地形（`Heightmap`）

通过 `LoadHeightmapGray` 加载灰度图（或直接填充 `Heights`），即可双线性采样高度与法线、对地形三角形做射线检测，并分块生成 `TerrainMesh` 网格：

```go
hm := world3d.LoadHeightmapGray(grayImg, 2.0, 40.0)

y, ok := hm.HeightAt(player.X, player.Z)
normal := hm.NormalAt(player.X, player.Z)
hit, hitOK := hm.Raycast(ray, 500)

// 生成 32x32 网格区块供 GPU 上传：
chunkMesh := hm.BuildMesh(0, 0, 32, 32)
_, _, _, _, _ = y, ok, normal, hit, chunkMesh
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
- **纯文本定义**：`ParseTerrain` / `EncodeTerrain` 读写第 1 版 YAML（`https://game.nsgo.dev/schemas/terrain.v1.json`），声明 `heightmap`、`cellSize`、`heightScale`、`origin`、`splatMaps`、`heightBlendContrast`、`triplanar` 及 1–8 个 `layers`（含逐层 `uvScale`、PBR 贴图与参数和 `heightBias`）。未知字段、重名层及权重图数量不足均会返回 `*loaderr.Error`。

## 多层 3D 导航网格（`NavMesh`）

用凸可行走多边形（斜坡、桥梁、多层楼板）构建 3D `NavMesh`。`BuildNavMesh` 会自动过滤超过 `maxSlopeRad` 的陡坡、在 3D 空间连接共享边，并在 `FindPath` 中执行门户 A* 与 3D 路径拉直：

```go
nav := world3d.BuildNavMesh(walkablePolys, math3d.DegToRad(45))
if path, ok := nav.FindPath(startPos, goalPos); ok {
	follow3DWaypoints(path)
}
```

`nav.FindPolygon(p)` 会在 `VerticalTolerance` 垂直容差内定位最近的可行走多边形，天然支持上下叠层与立交桥。

## 模块化 3D `GridMap`

在整数格点 `GridCoord{X, Y, Z}` 上摆放可复用的 3D 网格瓦片，支持 24 种立方体正交朝向（`GridOrientationMat4`）、碰撞盒提取、按区块裁剪的射线检测（沿射线对区块排序，再逐个测试区块中被占用的格子）以及带视锥裁剪的实例化合批：

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
