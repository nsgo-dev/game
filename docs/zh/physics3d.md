# physics3d

```go
import "github.com/nsgo-dev/game/packages/game/physics3d"
```

纯 Go 实现的紧凑、确定性 3D 碰撞检测、射线检测、刚体动力学与运动学角色控制器。

## 架构决策（#180）

`physics3d` 采用**方案 (a)**：仅依赖 Go 标准库的零 cgo 纯 Go 实现。

选择纯 Go 而不封装 C/C++ 物理引擎（如 Jolt、Bullet 或 PhysX）的原因：

- **全平台开箱即用**：macOS、Windows、Linux、iOS、Android 和 WebAssembly 都可以用普通的 `go build` / `go tool nsgo check` 直接编译，不需要 C/C++ 工具链、各平台静态库或 Wasm/FFI 胶水代码。
- **零 FFI 开销**：重叠查询、射线检测、刚体步进和角色移动直接在 Go 值上运行，没有 cgo 调用边界成本。
- **按位一致的确定性**：配合 `game.Loop` 的固定步长推进 `World`，宽相、窄相、冲量求解和位置修正都按固定的刚体与接触顺序执行，不依赖 map 遍历顺序或墙钟时间，相同的初始状态在多次运行中产生完全一致的轨迹。

包的能力范围保持克制和清晰：盒体（轴对齐或带朝向的 Box）、球体（Sphere）和胶囊体（Capsule），可以与物体和角色控制器碰撞的静态三角网格（`TriMeshCollider`，用 `World.AddTriMesh` 加入）与高度场（`HeightfieldCollider`，用 `World.AddHeightfield` 加入），精确的重叠与射线查询，带线性与角动力学的刚体、关节、布娃娃，以及胶囊角色控制器。刚体使用标量（各向同性）惯量而非完整的惯性张量，每对刚体的接触只用一个代表点。

## 坐标系约定

`physics3d` 采用右手、Y 轴向上的坐标系（`+X` 向右，`+Y` 向上，`+Z` 朝向观察者）。`NewWorld()` 将默认重力设为 `(0, -9.81, 0)`。

## 形状、重叠查询与射线检测

三种基本形状通过带标签的 `Shape` 封装（`BoxShape`、`SphereShape`、`CapsuleShape`）：

- `Box`：由 `Center` 和 `HalfExtents` 定义的盒体（也可用 `BoxFromMinMax(min, max)` 构造）。默认轴对齐；外层 `Shape` 设置 `Rot` 后，盒体绕自身 `Center` 旋转。`Shape.Transformed(pos, rot)` 与 `Body.WorldShape()` 会保留旋转刚体的盒体朝向，重叠检测、射线检测和调试绘制都按真实的角点计算；`Shape.Bounds()` 返回用于宽相的外接轴对齐包围盒。
- `Sphere`：由 `Center` 和 `Radius` 定义的球体。
- `Capsule`：由线段 `A` 到 `B` 沿半径 `Radius` 扫略而成的胶囊体（也可用 `VerticalCapsule(center, radius, height)` 构造竖直胶囊）。

重叠检测返回 `Contact`，其中单位法线 `Normal` 从 `B` 指向 `A`（将 `A` 沿 `Normal` 移动 `Depth` 即可分离两者）：

- `TestBoxBox`、`TestOrientedBoxBox`（在 15 条面轴与边轴上做分离轴测试）、`TestSphereSphere`、`TestCapsuleCapsule`、`TestBoxSphere`、`TestBoxCapsule`、`TestSphereCapsule`，以及能处理带朝向盒体的 `TestShapes(a, b)`。
- 胶囊的线段穿过盒体时，`TestBoxCapsule` 在盒体各面轴与“边轴叉乘线段方向”中取穿透最浅的轴，因此把盒体沿 `Normal*Depth` 移动即可让整个胶囊分离。两个交叉的胶囊，以及球心落在胶囊轴线上的球，得到垂直于轴线的法线，而不是任意方向。
- `world.QueryOverlap(shape)` 按确定的插入顺序返回所有重叠的刚体。

射线检测支持对单个形状测试 `Ray{Origin, Dir, MaxDist}`（`RaycastBox`、`RaycastSphere`、`RaycastCapsule`、`RaycastShape`），以及对整个世界发射线段（`world.Raycast(from, to)` 和 `world.RaycastFirst(from, to)`），返回按 `[0, 1]` 范围内的 `T` 升序排列的 `RaycastHit`，包含世界坐标命中点 `Point`、距离 `Distance` 和向外单位法线 `Normal`。起点位于盒体、球体或胶囊内部的射线在 `T` 为 0 处命中，`Point` 为起点，`Normal` 沿射线反方向（`-Dir`）。

## 刚体动力学

`World` 管理静态（`Static: true` 或零质量）和动态 `Body`，在 `Step(dt)` 中通过半隐式欧拉积分、确定性碰撞检测、带热启动和三维各向同性库仑摩擦的迭代冲量求解器、非线性高斯-赛德尔位置修正以及静止休眠机制推进模拟。

```go
world := physics3d.NewWorld()
world.AddBody(physics3d.NewStaticBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 1, Z: 20}}),
	physics3d.Vec3{Y: -1},
))
crate := world.AddBody(physics3d.NewDynamicBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
	physics3d.Vec3{Y: 3},
	1.0,
))

loop := &game.Loop{
	Update: func(dt time.Duration) {
		world.Step(dt)
		_ = crate.Pos
	},
}
```

接触冲量作用在接触点上，因此摩擦会让滑动的球滚起来，立在棱上的盒子会倒向一个面。`LockRotation` 可禁止动态刚体旋转。`AddBody` 会把非有限的输入（如 NaN 速度或质量）回退为默认值，没有质量的运动学刚体仍保持运动学。

静止的刚体会进入休眠。以下情况会唤醒休眠刚体：运动中的动态刚体或运动学刚体（通过速度运动，或直接设置 `Pos`/`Rot`）碰到它；与它相连的关节另一端在运动；它的某个接触结束（支撑物移开）；以及调用 `Wake`、`ApplyImpulse` 或移除刚体、网格、关节时。

单步位移超过自身最小尺寸一半的动态刚体，会以子步对静态、运动学和休眠刚体以及三角网格、高度场做扫掠，在第一次重叠处停下，因此高速刚体不会穿过薄的几何体。高速动态刚体之间不做扫掠。

`HingeJoint.Angle` 会跨步累计，持续转动的铰链会报告超过 ±π 的角度，`MinAngle`/`MaxAngle` 也可以跨越一圈以上。`DistanceJoint` 在距离位于 `[MinDistance, MaxDistance]` 之内时是松弛的；`Stiffness` 和 `Damping` 只让上下限变软，若要静止长度为 `L` 的弹簧，把两个上下限都设为 `L`。

## 角色控制器

`CharacterController` 通过 `Move(world, wishMove, dt)` 在 `World` 中移动一个运动学胶囊（`Pos` 为脚底位置，尺寸为 `Radius` 和 `Height`）：

- 在平地上行走，并在不超过 `MaxSlopeAngle`（默认 45°）的斜坡上平滑上下滑行，下坡时保持贴地。
- 阻止攀爬超过 `MaxSlopeAngle` 的陡坡，并在重力作用下沿陡坡滑落。
- 在验证头顶净空、水平前进增益和可站立踏面后，自动迈上不超过 `StepHeight` 的台阶。
- 遇到竖直墙面时干净停止，斜向撞墙时沿墙面切向滑行而不穿透。
- 以 `PushForce`（牛顿）推动走进的动态刚体；默认值 0 时动态刚体像墙一样挡住角色。

```go
cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
loop := &game.Loop{
	Update: func(dt time.Duration) {
		res := cc.Move(world, physics3d.Vec3{X: 3.5}, dt)
		_ = res.Pos
	},
}
```

角色控制器不是 `World` 中的刚体，因此动态刚体自身不会与它碰撞。需要碰撞时，添加一个跟随 `Pos` 的运动学刚体，并把 `IgnoreBodyID` 设为它的 ID。
