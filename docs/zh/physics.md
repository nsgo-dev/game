# physics

`game/physics` 提供 2D 碰撞形状（`Box` 与 `Circle`）、精确重叠接触计算、防穿墙瓦片滑动解析 `MoveAndSlide`，以及 `World` 上的确定性空间查询与射线检测。

```go
import "github.com/nsgo-dev/game/packages/game/physics"
```

坐标系与 NSGo 屏幕空间一致：X 向右递增，Y 向下递增。

## 形状与重叠检测

`Box{X, Y, W, H}`（左上角原点）与 `Circle{X, Y, R}`（圆心坐标）统一封装为 `Shape`（`BoxShape(b)`、`CircleShape(c)`）。可直接调用 `TestBoxBox`、`TestCircleCircle`、`TestBoxCircle`、`TestCircleBox` 或 `TestShapes` 进行检测：

```go
playerBox := physics.Box{X: 32, Y: 48, W: 14, H: 20}
coin := physics.Circle{X: 44, Y: 54, R: 6}

if c := physics.TestBoxCircle(playerBox, coin); c.Hit {
	// 法线方向从 B（coin）指向 A（playerBox）；
	// 将 A 平移 (NormalX*Depth, NormalY*Depth) 即可沿最小穿透轴分离。
	_ = c
}
```

仅在边缘或角点相切而穿透深度为 0 的形状会返回 `Hit == false`。空形状（`W <= 0`、`H <= 0` 或 `R <= 0`）不会与任何物体重叠。

## MoveAndSlide 与 TileGrid

`MoveAndSlide` 在每个 `game.Loop` 固定步长中按 `(VX*dt, VY*dt)` 推进 `*Body`，先解算 X 轴再解算 Y 轴，清零被阻挡方向的速度，并自动细分大位移，避免高速物体穿透单格墙壁或平台：

```go
grid := physics.NewGrid(40, 20, 16, 16)
grid.FillRect(0, 18, 40, 2, physics.TileSolid)
grid.FillRect(10, 14, 6, 1, physics.TileOneWay)

player := physics.NewBoxBody(32, 200, 12, 16)
player.Tag = "player"

// 在 game.Loop.Update 中：
player.VY += 900 * float32(dt.Seconds()) // 重力
player.OneWayDrop = actions.Pressed("down") && actions.JustPressed("jump")

res := physics.MoveAndSlide(player, grid, dt)
if res.Grounded && actions.JustPressed("jump") {
	player.VY = -320
}
```

- **接触标志**：`MoveAndSlide` 同时更新返回值 `MoveResult` 和物体自身的 `Grounded`、`OnCeiling`、`OnLeftWall`、`OnRightWall` 标志，并列出每次瓦片接触 `Contacts`（含 `Col`、`Row`、`Flags`、`NormalX`、`NormalY`）。
- **单向平台**：标记为 `TileOneWay` 的瓦片只在物体起点位于瓦片顶边之上且向下移动时阻挡；将 `Body.OneWayDrop` 设为 `true` 可向下穿过。
- **自定义 `TileGrid`**：只需实现 `TileSize() (float32, float32)`、`GridSize() (int, int)` 和 `TileFlagsAt(col, row int) TileFlags`，即可将 [`tilemap.Tilemap`](tilemap.md) 或任意地图直接接入 `MoveAndSlide` 与 `World`。

## World 空间查询与射线检测

`World` 汇总动态 `Bodies` 和可选的 `Tiles`（`TileGrid`），提供确定性的点查询、形状重叠查询和射线检测：

```go
world := &physics.World{Tiles: grid}
world.Add(player, enemy, trigger)

// 点查询（按 World.Bodies 顺序返回）
clicked := world.QueryPoint(mouseWorld.X, mouseWorld.Y)

// 形状重叠查询（物体与实心瓦片）
hitbox := physics.BoxShape(physics.Box{X: attackX, Y: attackY, W: 24, H: 16})
hits := world.QueryOverlapMask(hitbox, physics.QueryBodies|physics.QueryTiles)
for _, h := range hits {
	if h.Body != nil && h.Body.Tag == "enemy" {
		damageEnemy(h.Body)
	}
}

// 射线检测（视线或即时命中武器）
if hit, ok := world.RaycastFirst(gunPos, targetPos, physics.QueryAll); ok {
	spawnImpactSpark(hit.Point, hit.NormalX, hit.NormalY)
}
```

`QueryMask` 掩码（零值等价于 `QueryAll`）：

| 掩码 | 包含的目标 |
| --- | --- |
| `QueryBodies` | `World.Bodies` 中的全部物体（保持切片顺序）。 |
| `QueryTiles` | `World.Tiles` 中的实心瓦片（`TileSolid`）。 |
| `QueryOneWay` | 单向平台瓦片（`TileOneWay`）；射线仅在向下穿过其顶边时命中。 |
| `QueryAll` | `QueryBodies \| QueryTiles \| QueryOneWay`。 |

`World.Raycast` 按归一化距离 `T`（`[0, 1]`）升序返回线段上的所有命中（同距离时物体排在瓦片之前）；`RaycastFirst` 只返回最近的一个命中。使用 `world.Remove(b)` 可移除物体。

## 绳索与软体（XPBD）

`SoftWorld` 提供纯 Go、确定性的 2D 扩展基于位置动力学（XPBD）粒子与约束求解器，适用于绳索、悬挂链条和可变形软体：

- **粒子与约束**：`Particle`（位置、速度、`InvMass`、`Radius`、`Friction`、`Restitution`、`Pinned`、`Layer`/`Mask`）、双向与单向距离约束 `DistanceConstraint`（`AddDistance`、`AddTether`）、三粒子弯曲约束 `BendingConstraint`（`AddBending`）、带气压的多边形面积约束 `AreaConstraint`（`AddArea`），以及 2D 刚性形状匹配约束 `ShapeMatchConstraint`（`AddShapeMatch`）。
- **刚体双向耦合**：`AttachRigidBody` 可在局部锚点处将粒子附着到 `*RigidBody`（`Compliance == 0` 为刚性挂点，`> 0` 为弹性连接），在子步内平滑插值附着刚体的位姿，并将线性与角向反作用冲量反馈回刚体。
- **世界与瓦片碰撞**：设置 `SoftWorld.World`、`SoftWorld.RigidWorld` 和/或 `SoftWorld.Tiles` 后，粒子可与 `Box`、`Circle`、带朝向的 `RigidBody`、`TileSolid` 及 `TileOneWay` 瓦片碰撞，并支持 `LayerMask` 层级过滤（`TileLayer`/`TileMask`）。
- **构建器与调试绘制**：`AddRope(RopeConfig)` 构建不可拉伸绳索并自动添加长距离拉绳约束，保证挂载重型刚体摆动时不超限拉伸；`AddSoftBody(SoftBodyConfig)` 构建气压或形状匹配软体；`DrawSoftWorld(d, sw, style)` 绘制调试线框与固定点（`release` 构建下编译为空操作）。

```go
rw := &physics.RigidWorld{GravityY: 600}
crate := physics.NewRigidBox(120, 12, 24, 24, 10)
rw.AddBody(crate)

sw := physics.NewSoftWorld()
sw.RigidWorld = rw
sw.GravityY = 600

rope := sw.AddRope(physics.RopeConfig{
	StartX: 0, StartY: 0, EndX: 120, EndY: 0,
	Segments: 12, TotalMass: 0.5, PinStart: true,
})
sw.AttachRigidBody(rope[len(rope)-1], crate, core.Point{Y: -12}, 0)

// 在 game.Loop.Update 中：
rw.Step(dt)
sw.Step(dt)
```
