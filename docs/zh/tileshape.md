# tileshape

`github.com/nsgo-dev/game/pkg/tilemap/tileshape` 从 [`tilemap.Tilemap`](tilemap.md) 推导碰撞与光照几何：一个 `View`，可直接作为 [physics](physics.md) 与 [platformer](platformer.md) 控制器的 `TileGrid`；斜坡与非整格瓦片生成的斜坡线段；实心格子与瓦片形状合并后的轮廓；静态盒体；以及 [lights2d](lights2d.md) 遮挡体。它相当于 Godot TileSet 中每个瓦片的物理多边形与遮挡多边形。

```go
import "github.com/nsgo-dev/game/pkg/tilemap/tileshape"
```

## 瓦片形状

在 Tiled 的碰撞编辑器中为瓦片绘制碰撞形状：矩形、多边形、椭圆和折线。`tilemap.LoadTiled` 把它们读入 `Tileset.Shapes`（以本地瓦片 ID 为键），每个都是 `TileShape`，点位于瓦片自身的坐标系；椭圆近似为 16 边形（`EllipseShapeSegments`），对象的旋转会被应用。碰撞编辑器里的点对象和瓦片对象会被跳过。用代码构建的地图可以直接设置 `Tileset.Shapes`。

放置的瓦片的形状跟随其 GID 的翻转位，与绘制瓦片图像使用同一变换，因此翻转或旋转的斜坡瓦片就是镜像或旋转后的斜坡。`m.TileShapes(gid)` 返回已翻转的形状，`TileShape.Flipped` 对单个形状应用翻转位。

## View

```go
m, err := tilemap.LoadTiled(data, loader)
if err != nil {
	return err
}
v := tileshape.New(m, tileshape.Options{LayerNames: []string{"ground"}})

player := physics.NewCharacterBody(physics.NewBoxBody(32, 200, 12, 16))
ctl, _ := platformer.NewController(player, platformer.DefaultConfig())

// 在 game.Loop.Update 中：
near := player.Body.Bounds()
slopes = v.AppendSlopesIn(slopes[:0], core.Bounds{
	Origin: core.Point{X: core.Points(near.X - 64), Y: core.Points(near.Y - 64)},
	Size:   core.Size{Width: core.Points(near.W + 128), Height: core.Points(near.H + 128)},
})
ctl.Step(in, v, slopes, platforms, dt)
```

`View` 实现了 `physics.TileGrid` 与 `physics.TileGridOrigin`，返回的所有线段、斜坡和盒子都位于包含地图 `Origin` 的世界坐标中，因此同一个值可以传给 `physics.MoveAndSlide`、`CharacterBody.Move`、`platformer.Controller.Step` 以及 `physics.World{Tiles: v}`。`Options.Layers` 与 `Options.LayerNames` 选择参与碰撞的瓦片图层；两者都为空时所有瓦片图层都参与，包括之后追加到 `m.Layers` 的图层。

当格子中的瓦片带有 `TileSolid` 或 `TileOneWay`（来自 `SetFlags`、瓦片的 `solid` 或 `oneWay` 属性，或 `SetCellFlags` 覆盖，覆盖优先于该格的所有瓦片），或某个瓦片的形状填满整个格子时，该格整体参与碰撞。形状没有填满格子的瓦片通过形状参与碰撞：`TileFlagsAt` 对该格既不报告 `TileSolid` 也不报告 `TileOneWay`，形状的边成为斜坡。设置 `Options.ShapedSolid` 可把这类格子当作整块实心格报告，供只认识整格的代码使用，例如单独使用 `MoveAndSlide` 或 `World` 的射线检测。折线是角色可以站立的开放边：它是斜坡，但不属于轮廓和遮挡体，也不会让所在格子变成实心。

| 方法 | 返回 |
| --- | --- |
| `TileFlagsAt(col, row)` | 供物理使用的格子碰撞标志 |
| `Slopes()` | 实心形状朝上和竖直的边、单向形状朝上的边以及所有折线，作为 `physics.Slope` 线段 |
| `AppendSlopesIn(dst, r)` | `r` 附近区块的斜坡，在大地图上只把附近的斜坡传给 `CharacterBody.Move` |
| `Outline()` | 所有实心格子与实心形状的边界：去掉共享边、合并共线边，实心一侧在右 |
| `Boxes()` | 实心格子以及半格瓦片等轴对齐矩形实心形状跨邻居合并后的最大矩形，可交给 `physics.NewStaticRigidBox` |
| `StaticShapes()` | 全部实心几何跨邻居合并后的静态 `physics.Shape`：实心格子与矩形非整格瓦片合并为 `Box`，相邻凸多边形非整格瓦片（如多格斜坡）合并为凸 `Polygon`，凹多边形或超过 8 顶点的形状（如椭圆）转为闭合 `Chain`，可直接交给 `physics.NewStaticRigidBody` 或 `physics.World` |
| `AppendStaticShapesIn(dst, r)` | `r` 附近区块跨块合并后的静态形状 |
| `Occluders()` | 以 `lights2d` 遮挡体形式给出的 `Outline`，每个区块一个 |

有了这些斜坡，`CharacterBody` 可以走过 45 度、2:1（约 26.6 度，即瓦片集常说的 22.5 度瓦片）、真正 22.5 度以及半高瓦片，从斜坡顶端走上实心瓦片，并在走出斜坡下端时贴合到地面瓦片上，无需手工放置线段。`CharacterBody` 没有天花板线段，所以非整格形状的下表面挡不住跳跃；需要天花板的地方请使用整格瓦片或 `ShapedSolid`。

## 光照

```go
lighting := &lights2d.Scene{Ambient: core.RGBA(0x0c101e, 0.85)}
lighting.Occluders = v.Occluders()

// 地图改变之后：
m.SetTile(0, col, row, 0)
lighting.Occluders = v.Occluders()
```

推导出的遮挡体投下的阴影与每个实心格子一个 `RectOccluder`、每个实心形状一个 `PolygonOccluder` 相同，但线段少得多。单向格子和单向形状不投射阴影。

## 运行时修改

几何按 16×16 格的区块缓存，首次使用时构建。`View` 跟随地图的变更日志 `Tilemap.TileRevision` 与 `Tilemap.TileChangesSince`，因此在 `SetTile`、`FillRect`、`SetCellFlags` 或 `ClearCellFlags` 之后，下一次调用只会重建变化格子周围的区块。`SetFlags`、大范围的 `FillRect` 或超出日志容量的修改则会重建所有格子。直接写入图层的 `Data` 后请调用 `v.Invalidate(col, row, w, h)`。修改瓦片集的 `Shapes` 后请调用 `v.InvalidateAll()`：`Invalidate` 会保留已经从各瓦片形状推导出的几何。

比地图格子大的瓦片在格子内按底边对齐放置，因此它的形状可以伸入上方和右侧的格子。`View` 会按最大瓦片伸出的格数扩大比较边的邻域，以及每次修改后重建的区块范围。

`View` 返回的切片与其缓存共享：在地图改变之前一直有效，且不得修改。只支持有边界的正交地图：无边界地图（`Tilemap.Infinite`，或稀疏分块图层，例如无限 Tiled 地图加载后的图层）的 `View` 没有任何格子。图层偏移会被忽略，与 tilemap 自身的碰撞查询一致。
