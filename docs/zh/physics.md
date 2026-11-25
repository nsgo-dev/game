# physics

`game/physics` 提供 2D 碰撞形状（`Box`、`Circle`、凸多边形 `Polygon`、`Capsule`、`Segment` 与 `Chain`）、精确重叠接触计算、防穿墙瓦片滑动解析 `MoveAndSlide`，以及 `World` 上的确定性空间查询与射线检测。

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

## 多边形、胶囊体、线段与链

斜坡、楔块、旋转的道具、角色以及用线条画出的地形，仅靠矩形和圆形不够用。另外四种参照 Box2D 设计的形状同样封装在 `Shape` 中，凡是矩形能用的地方都能用：`TestShapes`、`Body`、`Trigger`、`World` 的查询与射线、`RigidBody` 以及调试绘制。

| 形状 | 构造方式 | 用途 |
| --- | --- | --- |
| `Polygon` | `NewPolygon(points, radius)`、`NewBoxPolygon(cx, cy, w, h, angle)`，再用 `PolygonShape(p)` | 3 到 `MaxPolygonVertices`（8）个顶点的凸多边形，可用 `Radius` 做圆角，使其能顺滑越过拐角 |
| `Capsule` | `CapsuleShape(Capsule{X1, Y1, X2, Y2, R})` | 线段按半径扫掠而成，适合不应卡在瓦片接缝或台阶边缘的角色 |
| `Segment` | `SegmentShape(Segment{X1, Y1, X2, Y2})` | 没有厚度的线段，适合边沿和墙 |
| `Chain` | `ChainShape(points, loop)` | 折线或闭合环，适合静态地形 |

`NewPolygon` 会对任意点求凸包，合并几乎重合的点，去掉共线顶点，并使结果在屏幕上按顺时针环绕；凸包顶点少于 3 个或多于 8 个时返回 `false`。只有在多边形已是凸的且按此方向环绕时，才手写 `Polygon`。

```go
ramp, _ := physics.NewPolygon([]core.Point{{X: 0, Y: 200}, {X: 300, Y: 200}, {X: 300, Y: 50}}, 0)
hero := physics.CapsuleShape(physics.Capsule{X1: 120, Y1: 100, X2: 120, Y2: 130, R: 8})
if c := physics.TestShapes(hero, physics.PolygonShape(ramp)); c.Hit {
	// 沿坡面法线把角色推出去。
	_ = c
}
```

任意两种形状组合都会报告命中、法线和深度。矩形与圆形之间仍使用各自的检测函数；其余组合对形状的核心（多边形、线段或点）做分离轴检测，再加上各自的圆角半径，接触点取两个表面之间的中点。线段和链没有面积：它们从两侧与所有有面积的形状碰撞，但彼此之间从不碰撞；它们也不包含任何点，因此 `QueryPoint` 不会返回它们。

链只报告其最深的那段线段接触。在内部顶点处，它只采用相邻线段所允许的法线，因此沿一段平直的多段链滑动的箱子或滚动的球不会卡在线段之间的接缝上，也就是 Box2D 的链同样消除的幽灵碰撞。链的开放端点与单独线段的端点行为一致。

`Shape.Area`、`Shape.Centroid` 与 `Shape.MomentOfInertia(mass)` 给出刚体所用的质量属性；圆角多边形按 Box2D 的做法，把顶点向外推出来近似。

`MoveAndSlide` 与 `CharacterBody` 用物体的包围盒与瓦片求解，因此多边形或胶囊体 `Body` 按其包围盒移动并保持自身形状。`DrawShape` 以 `PolygonColor`、`CapsuleColor` 与 `SegmentColor` 绘制多边形（含圆角）、带中轴的胶囊体、线段与链的轮廓。

与这里的所有形状一样，由于没有连续碰撞检测，线段或链挡不住在一步之内中心越过它的物体：让快速物体每步的移动不超过一个半径，或者提高世界的步进频率。

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
- **瓦片地图**：[`tilemap.Tilemap`](tilemap.md) 的 [`tileshape.View`](tileshape.md) 就是一个 `TileGrid`，可以连同它从逐瓦片碰撞形状推导出的 `Slope` 线段一起直接传给 `MoveAndSlide`、`CharacterBody.Move` 与 `World`。
- **自定义 `TileGrid`**：只需实现 `TileSize() (float32, float32)`、`GridSize() (int, int)` 和 `TileFlagsAt(col, row int) TileFlags`，即可将其他任意地图接入 `MoveAndSlide` 与 `World`。
- **斜坡与瓦片配合**：`CharacterBody.Move` 会先把角色抬到可行走的斜坡上，再检测瓦片墙，因此斜坡顶端与实心瓦片顶面相接时角色会走上瓦片；角色走出斜坡下端时，会贴合到 `SnapDist` 以内的瓦片顶面。

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

`World.Raycast` 按归一化距离 `T`（`[0, 1]`）升序返回线段上的所有命中（同距离时物体排在瓦片之前）；`RaycastFirst` 只返回最近的一个命中。射线可从任一侧命中线段或链，法线朝向射线起点；沿线段方向前进的射线不会命中它。使用 `world.Remove(b)` 可移除物体。

## 来自 Tiled 的关卡几何

`tilemap.Object.PhysicsShape` 把 Tiled 中的矩形、椭圆、多边形或折线对象转成形状，`ObjectLayer.PhysicsBodies` 为图层中的每个对象返回一个静态 `Body`，因此在 Tiled 中画的地形无需手写转换代码就能参与碰撞：

```go
for _, ground := range m.ObjectLayer("collision").PhysicsBodies() {
	world.Add(ground)                                      // 运动学查询与触发器
	rigid.AddBody(physics.NewStaticRigidBody(ground.Shape)) // 刚体
}
```

各类对象如何映射为形状，见 [tilemap](tilemap.md)。

### 形状投射（Shape Cast）

沿位移 `(dx, dy)` 扫掠 `Box` 或 `Circle`，计算精确解析碰撞时间（针对物体、旋转 `RigidBody` 以及支持 `TileGridOrigin` 的瓦片网格执行闵可夫斯基和扫掠）：

- **`World`**：`ShapeCast(shape, dx, dy, mask)` 返回最早命中的 `ShapeCastHit`（包含 `Fraction`、`Point`、`NormalX`/`NormalY`、`Body` 或 `Tile`/`Col`/`Row`/`TileFlags`），`ShapeCastAll` 按 `Fraction` 升序返回全部命中（相同分数时按 `World.Bodies` 顺序物体优先于按 `(Row, Col)` 行主序排列的瓦片）。使用 `ShapeCastLayer`/`ShapeCastAllLayer` 按 `LayerMask` 过滤，或使用 `ShapeCastWith`/`ShapeCastAllWith`（`ShapeCastOptions`）组合 `QueryMask`、`LayerMask` 与 `IgnoreBody`。
- **`RigidWorld`**：`ShapeCast(shape, dx, dy, layerMask)`、`ShapeCastAll`、`ShapeCastWith` 与 `ShapeCastAllWith` 针对静态与动态 `*RigidBody`（包含通过 2D 运动分离轴定理精确解算的旋转 OBB）扫掠，并填充 `ShapeCastHit.RigidBody`。
- **独立接口**：`ShapeCastShape(moving, dx, dy, target)` 直接测试单对形状。初始重叠在 `Fraction == 0` 处返回 `TestShapes` 接触；紧贴墙面或地面平行滑过时不会误卡住。

```go
// 让 16x16 盒体沿瓦片走廊向前扫掠，不会在地板或天花板处卡住。
probe := physics.BoxShape(physics.Box{X: 16, Y: 16, W: 16, H: 16})
if hit, ok := world.ShapeCast(probe, 160, 0, physics.QueryTiles); ok {
	// hit.Fraction 在 [0, 1] 内，包含 hit.Point、hit.NormalX/Y、hit.Col/Row
	_ = hit
}
```

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

## 重力缩放与区域覆盖

`RigidBody` 和 `CharacterBody` 均支持逐物体 `GravityScale`（默认为 `1`；调用 `SetGravityScale(0)` 可设为失重，负值可反转重力）。

任意 `Trigger`（刚体通过 `RigidWorld.AddTrigger` 注册，查询与角色控制器通过 `World.AddTrigger` 注册）都可以配置 `Trigger.Area`（`AreaSettings`），在 `RigidWorld.Step` 和 `CharacterBody.Move` 期间对重叠物体覆盖重力、阻尼以及流体浮力与阻力：

- **重力覆盖**：通过 `GravityMode`（`AreaOverrideCombine`、`AreaOverrideReplace`、`AreaOverrideAdd`）和 `Priority` 控制重叠区域优先级（高优先级优先，同优先级按触发器切片顺序确定性决胜）。支持定向重力 `(GravityX, GravityY)`，也支持 `PointGravity: true` 配合 `(GravityPointX, GravityPointY)`、`GravityStrength`、`GravityFalloff`（`GravityFalloffNone`、`GravityFalloffLinear`、`GravityFalloffInverse`、`GravityFalloffInverseSquare`）与 `GravityUnitDistance` 实现中心点引力。
- **阻尼覆盖**：`LinearDampingMode` / `LinearDamping` 与 `AngularDampingMode` / `AngularDamping` 使用相同的优先级与合并/替换/叠加规则覆盖刚体的基础阻尼。
- **浮力与流体阻力**：设置 `FluidDensity`（单位面积质量）后，会根据物体包围盒浸没比例施加阿基米德浮力，使隐含密度为 `Mass / Area` 的物体稳定浮在浸没比例 `(Mass / Area) / FluidDensity` 处，并可配合 `FluidLinearDrag`、`FluidAngularDrag` 与水流速度 `(FluidVelX, FluidVelY)`。
- **角色控制器**：设置 `CharacterBody.World`（配合 `World.GravityX` / `World.GravityY` 与 `World.Triggers`）后，`CharacterBody.Move` 会自动按区域解算后的重力（乘以 `CharacterBody.GravityScale`）、流体阻力与线性阻尼积分速度。也可通过 `GravityAt(x, y)` 和 `AreaEffectAtShape(shape, layer, mask)` 直接查询区域场。

```go
water := &physics.Trigger{
	Shape: physics.BoxShape(physics.Box{X: 0, Y: 100, W: 400, H: 200}),
	Area: physics.AreaSettings{
		FluidDensity:     1.0,
		FluidLinearDrag:  4.0,
		FluidAngularDrag: 2.0,
		FluidVelX:        30,
	},
}
rigidWorld.AddTrigger(water)
```

## 快照、回滚与跨架构确定性

`World` 与 `RigidWorld` 支持完整的世界状态快照以及跨 CPU 架构（amd64 与 arm64）的比特级确定性：

- **`Save() / Restore(snap) / Checksum()`**：`World.Save()` 返回带版本号的 `WorldSnapshot`（`SnapshotVersion`），完整记录重力、瓦片网格、刚体、触发器以及逐触发器的 enter/stay/exit 追踪状态。`RigidWorld.Save()` 返回 `RigidWorldSnapshot`，记录重力、求解器配置、刚体（含待结算的力/力矩累加器、重力缩放标记，以及求解器为 `Polygon`、`Capsule`、`Segment` 与 `Chain` 形状旋转所用的局部空间几何）、触发器/区域，以及所有内置关节（`DistanceJoint`、`SpringJoint`、`RevoluteJoint` 及其锚点偏移、柔度与刚度参数）。
- **纯文本 JSON 序列化**：`WorldSnapshot` 和 `RigidWorldSnapshot` 可直接使用 `encoding/json` 序列化与反序列化，不使用 `encoding/gob` 或自定义二进制格式。
- **原地保留指针与回调**：当数量和类型匹配时，`Restore` 会原地更新已有的 `*Body`、`*RigidBody`、`*Trigger` 和 `Joint` 实例，在回滚期间保持游戏实体引用、`UserData` 以及触发器回调（`OnEnter`/`OnStay`/`OnExit`）有效。
- **跨架构确定性**：所有步进求解器、积分、关节、窄相碰撞与区域计算路径都避免了 arm64 融合乘加（`FMADD`/`FMSUB`）收缩及含 FMA 的标准库超越函数，因此在第 100 步保存、推进到第 200 步、恢复并再次推进到第 200 步时，在 amd64 与 arm64 上都会得到比特完全一致的位置、速度、接触、事件与 `Checksum()` 值。

```go
snap := rigidWorld.Save()
hashAtSave := rigidWorld.Checksum()

// 以纯文本 JSON 持久化或传输（或直接存入回滚环形缓冲）：
raw, _ := json.Marshal(snap)

// 稍后恢复并按比特完全一致地重新模拟：
var restored physics.RigidWorldSnapshot
_ = json.Unmarshal(raw, &restored)
if err := rigidWorld.Restore(restored); err != nil {
	return err
}
_ = hashAtSave
```
