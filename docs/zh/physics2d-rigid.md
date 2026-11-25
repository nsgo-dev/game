# 2D 刚体与关节架构说明

本文档记录 `github.com/nsgo-dev/game/packages/game/physics` 中 2D 刚体动力学的架构决策（[#320](https://github.com/nsgo-dev/game/issues/320)、[#321](https://github.com/nsgo-dev/game/issues/321)、[#322](https://github.com/nsgo-dev/game/issues/322)）。

## 决策

直接在 `packages/game/physics` 内实现轻量级、纯 Go、确定性的 2D 刚体与约束求解器（`RigidWorld`、`RigidBody` 与 `Joint` 类型），而不是通过 cgo 绑定 C/C++ 物理引擎（如 Box2D、Chipmunk2D）或引入第三方 Go 依赖。

## 原因

1. **零 cgo、零第三方依赖**：NSGo 游戏需要同时面向 macOS、iOS、Windows、Linux、Android 和 Web（WASM）。通过 cgo 封装 Box2D 或 Chipmunk 会带来复杂的 C/C++ 交叉编译工具链、各平台静态库及 WASM 桥接成本。纯 Go 求解器可以用标准的 `go tool nsgo` 在所有目标平台上一致构建。
2. **固定步长下的确定性**：`game.Loop` 以固定步长 `dt` 推进游戏逻辑。求解器不依赖 map 遍历、后台协程或平台相关的 C 数学库，保证相同输入在每次运行和不同目标上都能复现一致的模拟轨迹。
3. **统一的坐标系与类型**：`RigidBody` 直接复用 `Shape`（`Box`、`Circle`、`Polygon`、`Capsule`、`Segment` 和 `Chain`）、`LayerMask` 以及 NSGo 的屏幕坐标系（X 向右、Y 向下、角度为顺时针弧度），避免运动学碰撞、空间查询、精灵绘制和刚体之间反复做坐标或类型转换。
4. **贴合 2D 游戏规模**：绝大多数 2D NSGo 游戏需要的是玩法级动力学——箱子堆叠、掉落物、摆锤、弹簧悬挂、布娃娃和机关谜题——而不是工业级 CAD 或重型连续碰撞求解器。

## 求解器设计

- **积分方式**：辛欧拉（半隐式欧拉）积分。先将重力、外力与阻尼积分到速度，再由约束求解器修正速度，最后用修正后的速度更新位置与角度。
- **接触生成**：按确定性的 `i < j` 刚体配对顺序执行，结合 `LayerMask` 过滤与旋转包围盒（AABB）粗筛。窄相检测支持圆–圆、圆–旋转矩形（OBB）和 OBB–OBB（基于 2D SAT 与 Sutherland–Hodgman 裁切生成最多 2 个接触点，确保堆叠的矩形稳定不晃动）。凡涉及多边形、胶囊体、线段或链的组合，都把两个形状视为圆角凸包（凸核心加半径），对核心执行同样的 SAT 与裁切，与 Box2D 的 `b2CollidePolygons` 一致：面贴面时最多 2 个接触点，其余情况 1 个。链对每段接触的线段各生成一个流形，并丢弃内部顶点会误加的法线，使物体能顺滑地滑过、滚过接缝。
- **速度求解**：顺序冲量（Sequential Impulse）求解器（`RigidWorld.SolverIterations`，默认 10 次迭代），支持累计法向与库仑摩擦冲量钳位，并对弹性恢复设置速度阈值，防止静止堆叠出现微幅弹跳。
- **位置修正**：在位置积分后执行带容差（slop = `0.01`）的直接线性位置投影，消除残余穿透而不额外注入虚假反弹速度。
- **关节约束**：
  - `DistanceJoint`：定长杆约束，支持可选的 `Stiffness` 与 XPBD `Compliance` 柔度参数。
  - `SpringJoint`：胡克定律阻尼弹簧，采用无条件稳定的隐式冲量积分。
  - `RevoluteJoint`：二自由度铰链/销钉约束，通过 2×2 有效质量块矩阵使两个局部锚点在世界空间重合，同时允许自由旋转。

## 取舍与适用范围

- **支持的形状**：动态刚体可用旋转 `Box` 与 `Circle`、凸 `Polygon`（最多 8 个顶点，可带圆角）和 `Capsule`；静态刚体还可用 `Segment` 与 `Chain`。凹地形用 `Chain`（或多个凸多边形）表示；凹形动态刚体与扫掠连续碰撞检测（CCD）不在内置求解器范围内，因此在一步之内中心越过线段或链的物体会穿过它。
- **规模**：面向单场景数十至数百个活跃刚体；若游戏需要数千刚体的大规模物理模拟，可在 `packages/game` 之外按需集成专用引擎。

## 用任意形状创建刚体

`NewRigidBody(shape, mass)` 把刚体的质心放在 `shape.Centroid()`，转动惯量取自 `shape.MomentOfInertia(mass)`；`NewStaticRigidBody(shape)` 用于关卡几何。`Box` 或 `Circle` 得到的刚体与 `NewRigidBox`、`NewRigidCircle` 相同。线段和链没有面积，因此它们的刚体总是静态的。

多边形、胶囊体、线段或链刚体在局部坐标中保存形状，每步结束后把按 `Angle` 旋转后的结果写回 `Shape`，因此 `World` 查询、触发器与 `DrawShape` 看到的都是它的真实位姿。在结构体字面量中或两步之间赋给 `Shape` 的多边形或胶囊体保持在原处：下一步把 `X`、`Y` 移到它的质心，使刚体绕质心转动，并重新计算刚体自己推导出的 `Inertia`（你设成其他值的 `Inertia` 会保留）。线段或链以当前的 `X`、`Y` 为锚点。

```go
ramp, _ := physics.NewPolygon([]core.Point{{X: 0, Y: 50}, {X: 0, Y: 200}, {X: 300, Y: 200}}, 0)
rw.AddBody(physics.NewStaticRigidBody(physics.PolygonShape(ramp)))
crate := physics.NewRigidBody(physics.PolygonShape(physics.NewBoxPolygon(64.5, 70.6, 20, 20, 0.4636)), 1)
rw.AddBody(crate) // 沿斜坡滑下
```

## 示例

```go
var rw physics.RigidWorld
rw.GravityY = 600

floor := physics.NewStaticRigidBox(160, 220, 320, 20)
crate := physics.NewRigidBox(160, 100, 32, 32, 1)
pendulum := physics.NewRigidCircle(240, 140, 12, 1)
pivot := physics.NewStaticRigidBox(240, 60, 8, 8)

rw.AddBody(floor, crate, pendulum, pivot)
rw.AddJoint(physics.NewDistanceJoint(pivot, pendulum, core.Point{}, core.Point{}, 80))

loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		rw.Step(dt)
	},
}
```
