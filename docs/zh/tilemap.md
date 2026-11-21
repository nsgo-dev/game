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
