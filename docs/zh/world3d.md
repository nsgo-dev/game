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

在整数格点 `GridCoord{X, Y, Z}` 上摆放可复用的 3D 网格瓦片，支持 24 种立方体正交朝向（`GridOrientationMat4`）、碰撞盒提取、DDA 射线检测以及带视锥裁剪的实例化合批：

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

返回的每个 `FoliageInstance` 包含世界变换 `Transform` 以及 `[0, 1]` 范围的平滑远距淡出系数 `Fade`。

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
}
streamer.Update(origin.LocalToWorld(playerLocalPos))
```
