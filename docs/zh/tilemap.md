# tilemap

`game/tilemap` 负责带视锥裁剪的正交、等距、交错与六边形瓦片地图绘制、Tiled `.tmx`/`.tmj` 地图（含内嵌或外部 `.tsx`/`.tsj` 图块集）、YAML 图块集（`*.tileset.yaml`）与动画瓦片加载、瓦片碰撞标志与射线查询，以及支持平滑跟随、边界限制和屏幕震动的 2D 相机。

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

`LoadTiled` 会根据输入字节自动识别并解析 Tiled XML（`.tmx`）与 JSON（`.tmj`）地图到同一个 `*Tilemap`。它支持内嵌及外部 `.tsx`/`.tsj` 网格图块集与图片集合图块集（包含逐瓦片图片、可选子矩形、`tileoffset`、`objectalignment`，以及 `.tmx` 引用 `.tsj` 或 `.tmj` 引用 `.tsx` 的跨格式组合），支持有限图层与无限地图分块的 CSV、XML `<tile>` 以及 base64 瓦片数据（未压缩、`zlib` 与 `gzip`），同时支持图像层、嵌套图层组、图层着色 `tintcolor`（嵌套组间通道相乘）、Tiled JSON Wang 地形集（`.tmj`/`.tsj` 中的 `wangsets`）与逐瓦片概率、GID 翻转位（`FlipHorizontal`、`FlipVertical`、`FlipDiagonal`）、对象层（包含可绘制的图块对象、作为数据加载的文本对象以及外部 `.tx`/`.tj` 对象模板）、带类型的自定义属性、动画瓦片，以及在 Tiled 碰撞编辑器中为瓦片绘制的碰撞形状。对于 `zstd` 压缩，加载器会返回带有文件、行号与修复提示的 `*loaderr.Error` 明确拒绝。

```go
//go:embed resources/maps/*.tmx resources/maps/*.tsx resources/maps/*.tx
var mapFS embed.FS

raw, err := mapFS.ReadFile("resources/maps/level1.tmx")
if err != nil {
	return err
}

m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
	LoadTileset: func(source string) ([]byte, error) {
		return mapFS.ReadFile("resources/maps/" + source)
	},
	LoadTemplate: func(source string) ([]byte, error) {
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

当 `LoadTileset` 为 nil 时，沿用 `TilesetLoader.LoadJSON` 的旧调用方仍可同时加载 `.tsj` 与 `.tsx` 外部图块集。

### 图片集合图块集、图块对象、对象模板与文本对象

- **图片集合图块集**：带有逐瓦片 `<image>` 的图块集会加载到 `Tileset.TileImages`（`map[uint32]TileImage`，以 0 起始的本地瓦片 ID 为键），保留每个瓦片自身的宽高和可选子矩形（`X`、`Y`、`Width`、`Height`），路径相对外部图块集目录解析。`Tileset.TileOffsetX`/`TileOffsetY` 会作用于瓦片层与图块对象绘制；`Tileset.ObjectAlignment`（`ObjectAlignmentUnspecified`、`TopLeft`、`Top`、`TopRight`、`Left`、`Center`、`Right`、`BottomLeft`、`Bottom`、`BottomRight`）控制图块对象的锚点（正交/交错/六边形地图默认 `BottomLeft`，等距地图默认 `Bottom`）。
- **绘制图块对象与图层着色**：`m.Draw(d, cam)` 会按 Tiled 的原始图层顺序穿插绘制瓦片层、图像层与对象层，通过 `m.DrawObjectLayer(d, layer, cam)` 绘制每个可见的 `ObjectTile`，完整应用图层可见性、不透明度、`TintColor`、偏移、视差、对象尺寸、顺时针旋转 `Rotation`、GID 翻转位、`tileoffset`、`objectalignment` 以及相机视锥裁剪。设置 `TileLayer.TintColor`、`ImageLayer.TintColor` 或 `ObjectLayer.TintColor` 后，绘制时会对该层像素乘以对应着色。
- **对象模板（`.tx` / `.tj`）**：当对象引用外部模板时，`LoadTiled` 通过 `TilesetLoader.LoadTemplate` 读取并按路径缓存模板，将模板的字段、几何形状与自定义属性和对象实例上的覆盖值合并，并把模板引用的图块集 GID 重映射到地图图块集中（若与地图已有外部图块集规范路径一致则直接复用）。
- **文本对象**：Tiled 的 `<text>` / `"text"` 对象会以 `Shape == ObjectText` 和 `Object.Text`（`*TextObject`，包含 `Text`、`FontFamily`、`PixelSize`、`Wrap`、`Color`、`Bold`、`Italic`、`Underline`、`Strikeout`、`Kerning`、`HAlign`、`VAlign`）作为结构化数据加载，供游戏侧自行排版绘制。

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

作为碰撞几何绘制的对象可用 `obj.PhysicsShape()` 转为 [physics](physics.md) 形状；`layer.PhysicsBodies()` 为每个对象返回一个静态 `physics.Body`，已按图层偏移移动，并以对象 ID 为 `ID`、对象类型为 `Tag`、对象本身为 `UserData`。隐藏的对象同样计入，因为碰撞图层通常是隐藏的：

| Tiled 对象 | 形状 |
| --- | --- |
| 矩形 | `Box`，旋转后为 `Polygon` |
| 椭圆 | 正圆为 `Circle`，否则为内接的 8 顶点 `Polygon` |
| 多边形 | 凸且不超过 8 个顶点时为 `Polygon`，否则为沿轮廓闭合的 `Chain` |
| 折线 | 两个点为 `Segment`，否则为开放的 `Chain` |
| 点、图块 | 无 |

```go
for _, ground := range m.ObjectLayer("collision").PhysicsBodies() {
	rigid.AddBody(physics.NewStaticRigidBody(ground.Shape))
}
```

不存在的图层不返回任何物体。与对象本身一样，这些物体位于地图的局部坐标中；对于用 `Origin` 放置的地图，用 `ground.Shape.Translated(float32(m.Origin.X), float32(m.Origin.Y))` 移动每个形状。

## Tiled Wang 集与多地形自动拼接

`LoadTiled` 会从内嵌 `.tmj` 和外部 `.tsj` 图块集中解析 Tiled `wangsets`（`corner`、`edge` 与 `mixed`）到 `Tileset.WangSets`，包含每个地形颜色（`WangColor`：`Name`、`Color`、`Tile`、`Probability`、`Properties`）、每个瓦片按顺时针排列的 8 槽 `WangID`（`[Top, TopRight, Right, BottomRight, Bottom, BottomLeft, Left, TopLeft]`）以及逐瓦片概率权重（`Tileset.TileProbabilities` / `Tileset.TileProbability`）。

使用 `m.SetTerrain` 与 `m.SetTerrainRect` 可按 Tiled 地形画笔语义绘制草地、泥土、水域与悬崖等多地形过渡，统一支持正交与等距地图以及稠密与稀疏分块图层：

```go
ws := m.WangSet("Terrains") // 或 ts.WangSet("Terrains") / ts.WangSetAt(0)
grass, _ := ws.ColorIndex("Grass")
dirt, _ := ws.ColorIndex("Dirt")
water, _ := ws.ColorIndex("Water")

rng := random.New(42)
m.SetTerrainRect(0, 0, 0, m.Width, m.Height, ws, grass, rng)
m.SetTerrain(0, 4, 3, ws, dirt, rng)
m.SetTerrainRect(0, 6, 2, 3, 3, ws, water, rng)
```

- **角点、边线与混合集合**：`WangCorner` 匹配四个角点，`WangEdge` 匹配四条边，`WangMixed` 同时匹配全部八个槽位。共享边或角发生变化的邻格会自动更新，并支持多跳过渡（例如在没有草地直连水域的图块集中向草地绘制水域时，自动生成一圈泥土岸线过渡）。
- **确定性按概率选型**：当多个瓦片并列最佳匹配时，`SetTerrain` 与 `SetTerrainRect` 会通过 `*random.Stream`（或任意 `TerrainRand`）按瓦片概率加权抽取（相同种子始终选出完全一致的变体）；传入 `rng == nil` 时确定性地选择本地 `TileID` 最小的瓦片。
- **代码定义的位掩码拼接**：`AutotileSet`、`ApplyAutotile` 与 `SetAutotile` 继续保留，用于代码中定义的 4 位（16 瓦片）与 8 位角点门控（47 瓦片）单地形自动拼接。

## YAML 图块集

YAML 图块集（`*.tileset.yaml`，`TilesetDoc`）是 Godot TileSet 资源的文本形式：它只说明一次每个瓦片是什么，使用它的地图只负责放置瓦片。场景文件在分层网格中放置它的瓦片（[编辑器与场景格式](editor.md#瓦片地图tilemaps)）。它的瓦片要么从网格图片中切出（`image`、`columns`、`tileCount`，以及可选的 `spacing` 和 `margin`），要么取自精灵图集的帧（`atlas`，以及按局部 ID 顺序排列的 `frames`）。瓦片的局部 ID 是它在网格中从左上角起逐行计数的序号，或在 `frames` 中的序号，从 0 开始。

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/tileset.v1.json
version: 1
name: cave
tileWidth: 16
tileHeight: 16
image: tiles/cave.png
columns: 4
tileCount: 8
terrains:
  - name: rock
    mode: cardinal
tiles:
  - id: 0
    solid: true
    terrain: rock
  - id: 1
    solid: true
    properties:
      friction: 0.5
    terrain: rock
    neighbors: [east]
  - id: 2
    shapes:
      - kind: polygon
        points: [[0, 16], [16, 0], [16, 16]]
  - id: 4
    animation:
      - tile: 4
        duration: 200ms
      - tile: 5
        duration: 200ms
```

`tiles` 按 ID 升序列出除图片外还带有其他信息的瓦片：

| 字段 | 含义 |
| --- | --- |
| `solid`、`oneWay`、`flags` | `TileSolid`、`TileOneWay` 以及游戏自定义的其他 `TileFlags` 位，用 `SetFlags` 设置到该瓦片上 |
| `properties` | 自定义属性，每个值为布尔、整数、数字或字符串，即 `TileProperties` 返回的内容；图块集本身也有 `properties` |
| `shapes` | 瓦片坐标中的碰撞形状：`rect` 和 `ellipse` 由 `x`、`y`、`width`、`height` 给出，`polygon`（至少 3 个点）和 `polyline`（至少 2 个点）由 `points` 给出，均可带 `name`、`type` 和 `properties`；它们与 Tiled 碰撞编辑器画出的形状一样载入 `Tileset.Shapes`，供 [tileshape](tileshape.md) 使用 |
| `animation` | 由 `tile` 和正的 `duration`（如 `150ms`）组成的帧，`Update` 循环播放 |
| `terrain`、`neighbors` | 瓦片所属的 `terrains` 中的地形集，以及同一地形的单元格与它相邻的方向（`north`、`east`、`south`、`west`，`blob` 地形还可以有 `northeast`、`southeast`、`southwest`、`northwest`）；角方向只有在它两侧的边方向都列出时才能列出（`northeast` 需要 `north` 和 `east`），因为只有两侧的边都匹配时角才会匹配 |

地形的 `mode` 为 `cardinal` 时匹配四条边上的邻居（16 个瓦片，`Autotile4`），为 `blob` 时在角两侧的边都匹配时也匹配该角（47 个瓦片，`Autotile8`）。同一地形的两个瓦片不能列出相同的邻居，不列任何邻居的那个是它的默认瓦片。

`ParseTileset` 拒绝未知字段以及违反上述任何规则的文件，返回指向出错行的 `*loaderr.Error`；`EncodeTileset` 写出规范形式，因此加载再保存图块集不会改变任何字节；`LoadTilesetFS` 从 `fs.FS` 读取图块集。`m.AddTilesetDoc(doc, firstGID, assets)` 把它构建成 `m` 中 GID 从 `firstGID` 开始的 `Tileset`，通过 `TilesetAssets.Image`（为 nil 时为 `core.ImageSource{URI: path}`）解析图片或通过 `TilesetAssets.Atlas` 解析图集，并把瓦片的标志设置到地图上：

```go
doc, err := tilemap.LoadTilesetFS(os.DirFS(projectDir), "tilesets/cave.tileset.yaml")
if err != nil {
	return err
}
m := tilemap.New(40, 23, 16, 16)
if _, err := m.AddTilesetDoc(doc, 1, tilemap.TilesetAssets{}); err != nil {
	return err
}
// 用 rock 地形自动铺设一个图层。
rock := m.Terrain("rock")
m.ApplyAutotile(0, rock.Autotile, rock.Contains)
```

每个地形成为 `Tileset.Terrains` 中的一个 `Terrain`，它的 `Autotile` 把邻居掩码映射到 GID，供 `ApplyAutotile` 和 `SetAutotile` 使用，`Contains` 判断一个瓦片是否属于它。`TilesetFrame(doc, atlas, id)` 给出瓦片在图片中的位置，编辑器的调色板就用它显示瓦片。

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
- 需要在这类投影地图上做寻路或战术移动范围查询时，可将 `ai.TopologyFromTilemap(m)` 传入 [`game/ai`](ai.md#六边形与交错等距拓扑)（`AStar.Topology`、`AStar.MovementRange`、`ReachableCells`），或用 `ai.NewHexGrid(m)` 直接包装六边形地图供 [`ai.GraphSearch`](ai.md#通用图搜索六边形适配器与航点图) 使用（含范围内每格的最短路径重建）。
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

## 碰撞形状与变更跟踪

在 Tiled 碰撞编辑器中为瓦片绘制的矩形、多边形、椭圆和折线会读入 `Tileset.Shapes`（以本地瓦片 ID 为键），每个都是位于瓦片自身坐标系中的 `TileShape`：椭圆近似为 `EllipseShapeSegments` 个点，对象的旋转会被应用。`m.TileShapes(gid)` 返回放置后的瓦片按其 GID 翻转位翻转、旋转后的形状，与 `DrawLayer` 绘制该瓦片的变换相同；`m.TileBox(gid)` 返回形状所在瓦片的尺寸。

`m.TileRevision()` 与 `m.TileChangesSince(rev, fn)` 除了瓦片修改，也跟踪碰撞标志的修改：`SetCellFlags` 与 `ClearCellFlags` 会记录它们修改的格子，而 `SetFlags` 可能改变显示该瓦片的所有格子，因此会要求调用方完整重建。没有改变任何内容的调用不会记录。`m.Flags(gid)` 返回设置在瓦片 ID 上的标志，`m.CellFlags(col, row)` 返回单格覆盖（如果有）。

[tileshape](tileshape.md) 基于这两者：它的 `View` 是地图的 `physics.TileGrid`，从瓦片形状推导斜坡，并把实心格子与形状合并为轮廓和光照遮挡体，在每次变化后只重建附近的部分。
