# tilemap

`game/tilemap` 负责带视锥裁剪的正交瓦片地图绘制、Tiled `.tmj` 地图与动画瓦片加载、瓦片碰撞标志与射线查询，以及支持平滑跟随、边界限制和屏幕震动的 2D 相机。

```go
import "github.com/nsgo-dev/game/packages/game/tilemap"
```

## Camera

`Camera` 将世界坐标映射到尺寸为 `Width × Height` 点、以 `(X, Y)` 为中心的视图中，按 `Zoom`（`<= 0` 视为 `1`）缩放，并按顺时针弧度 `Rotation` 旋转：

```go
bounds := m.WorldBounds()
cam := &tilemap.Camera{
	Width:  320,
	Height: 180,
	Zoom:   1,
	Bounds: &bounds,
}
```

### 跟随、边界限制与震动

在 `game.Loop.Update` 中推进相机跟随与震动：

```go
// 按指数平滑跟随玩家（smoothing <= 0 表示直接对齐）
cam.Follow(playerPos, 8, dt)

// 推进当前屏幕震动
cam.Update(dt)

// 受击时触发确定性的屏幕震动
if tookDamage {
	cam.Shake(6, 250*time.Millisecond)
}
```

- `Clamp()` 在设置了 `Bounds` 时把 `VisibleBounds()` 限制在世界边界内；如果地图在某条轴上比视图还小，相机会在该轴居中。`Follow` 会自动调用 `Clamp`。
- `Shake(intensity, duration)` 按 `ShakeSeed` 生成确定性偏移并在 `duration` 内衰减归零（传入非正参数会取消当前震动）。可通过 `ShakeOffset()` 读取当前偏移。
- 使用 `WorldToView(p)` 和 `ViewToWorld(p)` 在世界与视图坐标间换算，使用 `VisibleBounds()` 获取当前可见世界矩形。

### 绘制时应用相机

用 `d.Save()` / `d.Restore()` 包裹世界空间绘制，在绘制地图和世界精灵前调用 `cam.Apply(d)`：

```go
d.Save()
cam.Apply(d)
m.Draw(d, cam)
actorsBatch.Draw(d)
d.Restore()
```

把 `cam` 传给 `m.Draw(d, cam)` 或 `m.DrawLayer(d, layer, cam)` 后，只会遍历与 `cam.VisibleBounds()` 相交的行列。

## 加载 Tiled 地图

`LoadTiled` 解析未压缩的正交 Tiled JSON 地图（`.tmj`），支持内嵌与外部 `.tsj` 图块集、GID 翻转位（`FlipHorizontal`、`FlipVertical`、`FlipDiagonal`）、对象层、带类型的自定义属性以及动画瓦片：

```go
//go:embed resources/maps/*.tmj resources/maps/*.tsj
var mapFS embed.FS

raw, err := mapFS.ReadFile("resources/maps/level1.tmj")
if err != nil {
	return err
}

m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
	LoadJSON: func(source string) ([]byte, error) {
		return mapFS.ReadFile("resources/maps/" + source)
	},
	LoadImage: func(imagePath string) (core.ImageSource, error) {
		return core.Src("asset:maps/" + path.Base(imagePath)), nil
	},
})
if err != nil {
	return err
}
```

### 用代码构建瓦片地图

对于程序化生成或测试，可以用 `NewGridTileset` 或 `NewAtlasTileset` 创建图块集，再直接填充 `Tilemap`：

```go
ts := tilemap.NewGridTileset(1, core.Src("asset:tiles.png"), 16, 16, 8, 64)
m := tilemap.New(40, 20, 16, 16, ts)
ground := tilemap.NewTileLayer("ground", 40, 20, nil)
m.Layers = append(m.Layers, ground)
m.SetTile(0, 5, 19, 1) // 图层 0，第 5 列第 19 行 -> GID 1
```

通过 `m.Layer("ground")` 和 `m.ObjectLayer("spawns")` 按名称查找图层与对象层。地图、图层、图块集、瓦片（`m.TileProperties(gid)`）和对象上的自定义属性都可通过 `Properties` 的类型化方法（`Bool`、`Int`、`Float`、`String`、`Color`、`File`、`ObjectID`、`Has`）读取。多边形与折线对象可通过 `obj.WorldPoints()` 获取叠加了 `(obj.X, obj.Y)` 的世界坐标顶点。

## 动画瓦片

Tiled 瓦片动画会加载到 `Tileset.Animations`（以 0 起始的本地瓦片 ID 为键）。在每个 `game.Loop.Update` 步长调用 `m.Update(dt)` 即可推进全部动画瓦片：

```go
m.Update(dt)
```

也可以手动给图块集配置 `ts.Animations[localID] = &tilemap.TileAnimation{Frames: ...}`。

## 瓦片与精灵的 Y 轴排序

在任意 `TileLayer` 上开启逐层 Y 轴排序（设置 `layer.YSort = true`、调用 `m.SetLayerYSort(index, true)`，或在 Tiled 图层自定义属性中设置布尔属性 `y_sort`、`ysort`、`ySort`、`y-sort`），即可让树木、墙体、石柱等高大瓦片按纵深与场景精灵和自定义绘制项穿插绘制：

```go
m.Layer("trees").YSort = true

// 可选：将树瓦片的排序基线从底边上移到树干底部。
// 等价于在 Tiled 中为该瓦片设置浮点/整数自定义属性 "sort_origin_y" = -8。
m.SetTileSortOriginY(treeGID, -8)

world := sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
world.DepthSort = scene.SortY
world.AddTilemap(m)
world.AddBatch(actorsBatch)
```

- **默认排序基线**：每个可见瓦片默认按其世界坐标底边（`offY + tileOriginY + TileHeight`）参与排序，统一支持正交、等距（isometric）、交错（staggered）和六边形地图，并与脚底锚点的角色精灵（`OriginY = height`）自然对齐。
- **逐瓦片排序原点**：在 Tiled 中为瓦片添加自定义属性 `sort_origin_y`（常量 `tilemap.PropSortOriginY`，同时兼容别名 `y_sort_origin` 与 `ysort_origin`），或在代码中调用 `m.SetTileSortOriginY(gid, offsetY)`，即可垂直偏移该瓦片的排序线（负值向上移至树干或等距菱形中心，正值向下移）。可通过 `m.TileSortOriginY(gid)` 读取。
- **图层顺序与裁剪**：在 `scene.SortY` 图层中，首个 Y 排序图层之前的普通图层绘制在精灵后方（仍享受 `CacheStaticLayer` 静态缓存），Y 排序图层中的可见瓦片逐个与精灵、自定义 `Drawable` 穿插排序，位于其后的普通图层（如屋顶、高空树冠）则绘制在前方。完整支持相机视锥裁剪（含高大瓦片与图集帧）、动画瓦片、翻转位及图层不透明度。
- **自定义绘制循环**：如需脱离 `scene` 自行穿插绘制，可使用 `m.AppendYSortTiles(dst[:0], layer, cam)`、`tilemap.SortYSortTiles(tiles)` 与 `tilemap.YSortDrawer`（或 `YSortTile.Draw(d)`），复用缓冲切片在预热后实现 `tilemap` 侧每帧零堆分配。

## 瓦片标志、区域查询与射线检测

为全局瓦片 ID 或单个地图格子设置碰撞与玩法 `TileFlags`（`TileSolid`、`TileOneWay` 或自定义位标志）：

```go
m.SetFlags(1, tilemap.TileSolid)
m.SetFlags(2, tilemap.TileOneWay)

// 覆盖单个格子（例如运行时被破坏的墙体）
m.SetCellFlags(10, 6, 0)
// 之后恢复该格子的瓦片 ID 默认标志
m.ClearCellFlags(10, 6)
```

网格查询方法：

- `FlagsAt(col, row)` 返回该格在所有图层上合并后的 `TileFlags`（若存在单格覆盖则返回覆盖值）。
- `TilesInRect(r, mask)` 按行主序返回所有与 `r` 重叠且满足 `mask`（`mask == 0` 表示任意非空瓦片）的 `[]TileHit`。每个格子采用半开区间 `[col*TileWidth, (col+1)*TileWidth) × [row*TileHeight, (row+1)*TileHeight)`。
- `Raycast(from, to, mask)` 沿线段 `from -> to` 返回命中的第一个 `RaycastHit`（包含 `Col`、`Row`、`TileID`、`Flags`、`Point`、`Normal` 以及 `[0, 1]` 范围内的比例 `T`）。

```go
if hit, ok := m.Raycast(enemyEye, playerPos, tilemap.TileSolid); ok {
	// 视线被 hit.Point 处、法线为 hit.Normal 的瓦片阻挡
	_ = hit
}
```

## 等距、交错等距与六边形地图

设置 `Orientation`（`OrientationOrthogonal`、`OrientationIsometric`、`OrientationStaggered` 或 `OrientationHexagonal`）以及 `StaggerAxis`（`StaggerY` / `StaggerX`）、`StaggerIndex`（`StaggerOdd` / `StaggerEven`）与 `HexSideLength`，即可投影并绘制菱形等距、交错等距以及尖顶/平顶六边形地图：

- `TileOrigin(col, row)` 与 `TileToWorld(col, row)` 返回格子在世界空间中的包围盒原点与中心点，`WorldToTile(p)` 将格内任意世界坐标还原回 `(col, row)`。
- `HexNeighbors(col, row)` 与 `HexDistance(c1, r1, c2, r2)` 针对两种交错轴与奇偶偏移计算精确的 6 邻格与立方距离。
- 需要在这类投影地图上做寻路或战术移动范围查询时，可将 `ai.TopologyFromTilemap(m)` 传入 [`game/ai`](ai.md#六边形与交错等距拓扑)（`AStar.Topology`、`AStar.MovementRange`、`ReachableCells`）。
- 需要在等距、交错等距与六边形地图上做视线计算与对齐瓦片轮廓的战争迷雾绘制时，可用 `fog.NewFromTilemap(m, tilemap.TileSolid)` 构建 [`game/fog`](fog.md)。

## 稀疏分块图层与流式加载

对于无边界的 2D 沙盒或开放世界地图（支持负坐标与任意大坐标），可在 `NewInfinite(tileWidth, tileHeight, tilesets...)` 中使用 `NewChunkedTileLayer(name, chunkSize)` 创建稀疏分块 `TileLayer`。分块尺寸为 `[MinChunkSize, MaxChunkSize]` 内的 2 的幂（默认 `DefaultChunkSize = 16`），通过无分支位运算（`TileToChunk`）统一映射正负坐标：

```go
m := tilemap.NewInfinite(16, 16, ts)
m.Layers = []tilemap.TileLayer{
	tilemap.NewChunkedTileLayer("world", tilemap.DefaultChunkSize),
}
m.SetFlags(1, tilemap.TileSolid)

// 任意正负坐标均沿用完全一致的读写、查询与绘制 API：
m.SetTile(0, -17, -16, 1)
gid := m.TileAt(0, -17, -16)
hits := m.TilesInRect(rect, tilemap.TileSolid)
rayHit, ok := m.Raycast(from, to, tilemap.TileSolid)
```

- **无限 Tiled 地图**：`LoadTiled` 会将无限 `.tmj` 图层（`"infinite": true` 或包含 `"chunks"`）直接加载为稀疏分块 `TileLayer`，保留原始负分块坐标，不再平铺为巨型稠密数组。
- **逐分块静态缓存与脏标记**：通过 `SetTile` 或 `FillRect` 修改瓦片时，仅将受影响的分块标记为 `Dirty` 和 `Modified`，不会使其他分块缓存失效。使用 `m.CacheStaticChunk(layerIndex, coord, images)` 烘焙单个静态分块，或使用 `m.CacheDirtyChunks(layerIndex, images)` 仅重烘脏分块；通过 `ChunkCache`、`InvalidateChunkCache`、`ClearChunkCache`、`layer.DirtyChunks()` 与 `layer.ModifiedChunks()` 检查或失效单个分块。
- **确定性纯文本分块持久化**：`Chunk.ToDoc()` / `ChunkFromDoc` 以及 `EncodeChunkYAML` / `DecodeChunkYAML`（或 `EncodeChunkJSON` / `DecodeChunkJSON`）以带版本号（`ChunkFormatVersion = 1`）的游程编码 YAML 或 JSON 读写分块，完整保留含翻转位在内的 32 位 GID，且加载与保存按字节完全一致。内置存储提供 `NewMemoryChunkStore()` 与 `NewDirChunkStore(dir, format)`（写出 `chunk_<x>_<y>.yaml` 或 `.json`）。
- **有界流式加载**：
  - 使用 `tilemap.NewChunkStream(tilemap.ChunkStreamConfig{...})` 进行同步 2D 流式加载，支持 `Generator` 生成回调、`LoadRadius`/`UnloadRadius` 滞回卸载、`MaxLoadsPerUpdate` 每帧预算，以及通过 `ChunkStore` 自动保存和恢复已修改分块。
  - 如需基于 `world3d.ChunkStreamer` 的多线程后台分块加载，可使用 `game/world3d` 中的 `world3d.NewTilemapStreamer(world3d.TilemapStreamerConfig{...})`，获得后台工作协程、过时任务取消、每帧集成预算及修改分块的 YAML/JSON 持久化能力。

```go
store := tilemap.NewDirChunkStore(saveDir, tilemap.ChunkFormatYAML)
stream := tilemap.NewChunkStream(tilemap.ChunkStreamConfig{
	Map:          m,
	LayerIndex:   0,
	LoadRadius:   2,
	UnloadRadius: 3,
	Format:       tilemap.ChunkFormatYAML,
	Store:        store,
	Generator: func(coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
		// 按 coord 生成程序化地形...
		return nil
	},
})
defer stream.Close()

// 在更新循环中推进流式加载：
stream.Update(playerPos)
```

## 多关卡世界（LDtk 与 Tiled `.world`）

使用 `Tilemap.Origin`、LDtk 世界布局、Tiled `.world` 文件以及基于区域的 `WorldStream`，可以将多张瓦片地图直接安放在同一个世界坐标系中：

- **世界原点（`Tilemap.Origin`）**：每个 `Tilemap` 都带有 `Origin core.Point`（零值 `(0, 0)` 保持零开销向后兼容）。`Draw`、`DrawLayer`、`DrawImageLayer`、`CacheStaticLayer`、`AppendYSortTiles`、`WorldBounds`、`TileOrigin`、`TileToWorld`、`WorldToTile`、`TilesInRect` 与 `Raycast` 均统一遵循 `Origin`；`fog.NewFromTilemap`（`fog.Fog.Origin`）以及实现可选接口 `physics.TileGridOrigin`（`GridOrigin() (x, y float32)`）的 `physics.TileGrid` 适配器同样自动遵循该世界原点。
- **LDtk 多关卡世界**：在 `Free` 和 `GridVania` 布局下，`LoadLDtk` 与 `LoadLDtkAll` 会按关卡的 `(worldX, worldY)` 填充 `Origin`，并从 `__neighbours` 解析邻接列表 `Neighbours`（`[]LevelNeighbour`，包含 `Identifier`、`IID`、`UID` 以及方向 `n`/`s`/`e`/`w`/`ne`/`nw`/`se`/`sw`/`o`/`<`/`>`）。`LoadLDtkWorld(data, worldIdentifier, resolveImage)` 返回按需延迟加载的 `*LevelWorld`，同时支持单世界与多世界 `.ldtk` 项目（拒绝没有 2D 世界坐标的 `LinearHorizontal` 与 `LinearVertical` 布局）。
- **Tiled `.world` 文件**：`ParseTiledWorld(data)` 解析 Tiled `.world` JSON 文件，同时支持显式 `maps` 列表（`fileName`、`x`、`y` 与可选 `width`/`height`）和正则 `patterns`（`regexp`、`multiplierX`、`multiplierY`、`offsetX`、`offsetY`、`mapWidth`、`mapHeight`），并在 JSON 或正则非法时返回带行号、字段与修复建议的 `*loaderr.Error`。`TiledWorld.Resolve(fileNames)` 以确定性顺序展开正则匹配并跳过显式已列出的地图；`LoadTiledWorld(data, loader)` 构建按需加载的 `*LevelWorld`，并将每张加载出的 `.tmj` 地图的 `Origin` 设为 `.world` 中的世界坐标。
- **按区域流式加载关卡（`WorldStream`）**：只加载与相机或查询矩形（外扩 `LoadMargin`）重叠的关卡，并卸载超出 `UnloadMargin` 滞回范围的关卡，按确定性世界顺序触发 `OnEnter`/`OnLeave` 并返回 `[]WorldStreamEvent`。`WorldStream` 还提供无缝多关卡绘制 `Draw`、`UpdateAnimations`、`LevelAt`、`FlagsAtWorld`、`TilesInRect`、`Raycast` 以及统一网格访问器（`TileSize`、`GridSize`、`GridOrigin`、`GridFlagsAt`），使 `physics.Body` 或 `physics.CharacterBody` 可以无卡顿地跨关卡接缝行走。

```go
world, err := tilemap.LoadLDtkWorld(ldtkBytes, "", func(relPath string) core.ImageSource {
	return core.Src("asset:" + relPath)
})
if err != nil {
	return err
}

stream := tilemap.NewWorldStream(tilemap.WorldStreamConfig{
	World:        world,
	LoadMargin:   64,
	UnloadMargin: 128,
	OnEnter: func(lvl *tilemap.WorldLevel, m *tilemap.Tilemap) {
		// 为 lvl.ID 生成实体或初始化战争迷雾...
	},
	OnLeave: func(lvl *tilemap.WorldLevel, m *tilemap.Tilemap) {
		// 清理属于 lvl.ID 的实体...
	},
})
defer stream.Close()

// 在更新循环中：
stream.UpdateCamera(cam)
stream.UpdateAnimations(dt)

// 在绘制循环中（cam.Apply(d) 之后）：
stream.Draw(d, cam)
```
