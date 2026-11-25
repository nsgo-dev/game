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

- **`Save() / Restore(snap) / Checksum()`**：`World.Save()` 返回带版本号的 `WorldSnapshot`（`SnapshotVersion`），完整记录重力、瓦片网格、刚体、触发器以及逐触发器的 enter/stay/exit 追踪状态。`RigidWorld.Save()` 返回 `RigidWorldSnapshot`，记录重力、求解器配置、刚体（含待结算的力/力矩累加器与重力缩放标记）、触发器/区域，以及所有内置关节（`DistanceJoint`、`SpringJoint`、`RevoluteJoint` 及其锚点偏移、柔度与刚度参数）。
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
