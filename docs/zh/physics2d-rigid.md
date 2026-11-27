# 2D 刚体与关节架构说明

本文档记录 `github.com/nsgo-dev/game/pkg/physics` 中 2D 刚体动力学的架构决策（[#320](https://github.com/nsgo-dev/game/issues/320)、[#321](https://github.com/nsgo-dev/game/issues/321)、[#322](https://github.com/nsgo-dev/game/issues/322)、[#804](https://github.com/nsgo-dev/game/issues/804)、[#805](https://github.com/nsgo-dev/game/issues/805)）。

## 决策

直接在 `pkg/physics` 内实现轻量级、纯 Go、确定性的 2D 刚体与约束求解器（`RigidWorld`、`RigidBody` 与 `Joint` 类型），而不是通过 cgo 绑定 C/C++ 物理引擎（如 Box2D、Chipmunk2D）或引入第三方 Go 依赖。

## 原因

1. **零 cgo、零第三方依赖**：NSGo 游戏需要同时面向 macOS、iOS、Windows、Linux、Android 和 Web（WASM）。通过 cgo 封装 Box2D 或 Chipmunk 会带来复杂的 C/C++ 交叉编译工具链、各平台静态库及 WASM 桥接成本。纯 Go 求解器可以用标准的 `go tool nsgo` 在所有目标平台上一致构建。
2. **固定步长下的确定性**：`game.Loop` 以固定步长 `dt` 推进游戏逻辑。求解器不依赖 map 遍历、后台协程或平台相关的 C 数学库，保证相同输入在每次运行和不同目标上都能复现一致的模拟轨迹。
3. **统一的坐标系与类型**：`RigidBody` 直接复用 `Shape`（`Box`、`Circle`、`Polygon`、`Capsule`、`Segment` 和 `Chain`）、`LayerMask` 以及 NSGo 的屏幕坐标系（X 向右、Y 向下、角度为顺时针弧度），避免运动学碰撞、空间查询、精灵绘制和刚体之间反复做坐标或类型转换。
4. **贴合 2D 游戏规模**：绝大多数 2D NSGo 游戏需要的是玩法级动力学——箱子堆叠、掉落物、摆锤、弹簧悬挂、布娃娃和机关谜题——而不是工业级 CAD 求解器。

## 求解器设计

- **积分方式**：辛欧拉（半隐式欧拉）积分。先将重力、外力与阻尼积分到速度，再由约束求解器修正速度，最后用修正后的速度更新位置与角度。
- **宽相（broadphase）**：扫描排序（sort and sweep）。每一轮把刚体的包围盒按 X 排序（相同时按刚体下标），沿排好序的列表找出两轴都重叠的包围盒，再把这些刚体对按 `(i, j)` 排序。得到的刚体对及其顺序与原来 `i < j` 双重循环检测的完全一致，`LayerMask` 过滤也相同，所以接触及其顺序不变，变的只是工作量：它随重叠的刚体对数增长，而不再随刚体数的平方增长。接触检测和位置修正的每一轮迭代都使用它。
- **接触生成**：窄相检测支持圆–圆、圆–旋转矩形（OBB）和 OBB–OBB（基于 2D SAT 与 Sutherland–Hodgman 裁切生成最多 2 个接触点，确保堆叠的矩形稳定不晃动）。凡涉及多边形、胶囊体、线段或链的组合，都把两个形状视为圆角凸包（凸核心加半径），对核心执行同样的 SAT 与裁切，与 Box2D 的 `b2CollidePolygons` 一致：面贴面时最多 2 个接触点，其余情况 1 个。链对每段接触的线段各生成一个流形，并丢弃内部顶点会误加的法线，使物体能顺滑地滑过、滚过接缝。两个重叠形状的流形还会保留仍相距不超过 1 px 的裁切点，求解器允许这段间隙先合拢再施加推力，因此稍微不水平地放着的箱子两个角都保持接触，而不会在两个角之间来回摇晃。每个接触点带有特征 id，记录它来自哪个参考面以及哪个入射顶点或裁切平面；只有当另一个面的分离量明显更大时参考面才会切换，因此同一个物理接触在相邻两步之间保持相同的 id。
- **速度求解**：顺序冲量（Sequential Impulse）求解器（`RigidWorld.SolverIterations`，默认 10 次迭代），支持累计法向与库仑摩擦冲量钳位，并对弹性恢复设置速度阈值，防止静止堆叠出现微幅弹跳。求解带热启动（warm starting）：每个接触点在一步结束时的法向与摩擦冲量按刚体对、流形和特征 id 缓存下来，按前后两步的步长之比缩放，在下一步迭代之前先施加，因此静止的堆叠在每一步开始时就已经撑住了自身重量。
- **位置修正**：在位置积分后执行带容差（slop = `0.01`）的直接线性位置投影，消除残余穿透而不额外注入虚假反弹速度。
- **连续碰撞**：位置积分之后，若动态刚体上任意一点在这一步移动超过其厚度的一半，或者它是 `Bullet`，就把它从起始位姿到结束位姿对路径经过的静态刚体做扫掠。保守推进（conservative advancement）求出碰撞时刻，把刚体退回到最早的碰撞位姿，并在它的面与表面相接的每个点上去掉它朝向表面的速度（平坦瓦片地面的各块瓦片也一并计入），恢复系数与接触求解器使用的相同，因此平着落地的方块保持水平并能静止下来。这次碰撞会开始一个接触，并报告撞击时的速度。刚体在一步开始时所依托的静态表面不会挡住它，完全位于该表面背后的静态部件也不会，例如瓦片地面的下一块瓦片或平直链条的下一段，所以沿地面高速滑行的刚体越过接缝时保持速度；朝已经贴着、但尚无接触的静态刚体射出的刚体会停在原地；对于不是 `Bullet` 的刚体，若碰撞后只会浅浅地进入形状，则交给常规接触处理。高速动态刚体之间不做扫掠。
- **休眠**：移动速度低于 `SleepLinearThreshold`（2 px/s）且转动速度低于 `SleepAngularThreshold`（2°/s）的动态刚体会累计静止时间。本步的接触与关节把刚体连成岛（island），当岛内每个刚体都静止了 `TimeToSleep`（0.5 s）后，整个岛进入休眠：速度清零，世界不再积分或求解它们，也不检测休眠刚体与静态刚体之间的接触。休眠的刚体在以下情况下会连同整个岛一起被唤醒：调用 `Wake`，施加力、力矩或冲量，位姿或速度被修改，与醒着的刚体接触，通过关节连接到醒着的刚体，它所依托的刚体被移除或移动，或者它所处的重力、阻尼或流体发生变化。被 `MouseJoint` 拉住的刚体，或被开启马达的关节驱动的刚体，在驱动开启期间保持清醒，因此写入新的 `Target` 或 `MotorSpeed` 会在下一步生效。`RigidBody.Sleeping` 报告休眠状态，`RigidWorld.DisableSleep` 可关闭休眠。
- **关节约束**：
  - `DistanceJoint`（`NewDistanceJoint`、`NewRopeJoint`）：定长杆约束，支持可选的 `Stiffness` 与 XPBD `Compliance` 柔度参数，并支持单向绳索/最大距离模式（`MaxOnly: true`）以及松弛区间模式（`[MinLength, MaxLength]`）。
  - `SpringJoint`（`NewSpringJoint`）：胡克定律阻尼弹簧，采用无条件稳定的隐式冲量积分。
  - `RevoluteJoint`（`NewRevoluteJoint`）：二自由度铰链/销钉约束，通过 2×2 有效质量块矩阵使局部锚点重合，支持角度限位（`EnableLimit`、`LowerAngle`、`UpperAngle`）、角速度马达（`EnableMotor`、`MotorSpeed`、`MaxMotorTorque`）以及 `JointAngle()` / `JointSpeed()` 读数。
  - `PrismaticJoint`（`NewPrismaticJoint`）：沿 `LocalAxisA` 滑动的一自由度滑块约束，锁定相对转角（`ReferenceAngle`），支持位移限位（`EnableLimit`、`LowerTranslation`、`UpperTranslation`）、线性马达（`EnableMotor`、`MotorSpeed`、`MaxMotorForce`）以及 `JointTranslation()` / `JointSpeed()` 读数。
  - `WeldJoint`（`NewWeldJoint`）：通过耦合 3×3 块矩阵求解的三自由度刚性焊接关节，也可通过 `FrequencyHz`/`DampingRatio`（或直接设置 `Stiffness`/`Damping`）作为柔顺阻尼弹簧姿态锁使用。
  - `WheelJoint`（`NewWheelJoint`）：2D 车辆悬挂关节，约束车轮轮毂沿 `LocalAxisA` 伸缩，带悬挂弹簧（`FrequencyHz`/`DampingRatio` 或 `Stiffness`/`Damping`）、可选行程限位（`EnableLimit`、`LowerTranslation`、`UpperTranslation`）与旋转驱动马达（`EnableMotor`、`MotorSpeed`、`MaxMotorTorque`）。
  - `MouseJoint`（`NewMouseJoint`）：单刚体目标牵引关节，以受 `MaxForce` 限制的阻尼弹簧将局部锚点 `Anchor` 拉向可动态更新的世界坐标 `Target`（`SetTarget`）。
  - **连接体碰撞、反作用力与断裂**：双刚体关节默认关闭相连刚体间的接触碰撞（`CollideConnected: false`）。所有内置关节均追踪每步的 `ReactionForce() (fx, fy float32)`、`ReactionTorque() float32` 与 `Broken() bool`，并支持可选的 `BreakForce` / `BreakTorque` 断裂阈值；超过阈值的关节会在位置修正前按插入顺序确定性移除，并通过 `RigidWorld.JointBreakEvents()` 与 `RigidWorld.OnJointBreak` 上报。

## 取舍与适用范围

- **支持的形状**：动态刚体可用旋转 `Box` 与 `Circle`、凸 `Polygon`（最多 8 个顶点，可带圆角）和 `Capsule`；静态刚体还可用 `Segment` 与 `Chain`。凹地形用 `Chain`（或多个凸多边形）表示；凹形动态刚体不在内置求解器范围内。
- **连续碰撞**：高速刚体只对静态刚体和瓦片做扫掠，因此仍可能穿过很薄的动态或运动学刚体；碰撞会结束刚体在这一步的运动，而不会把剩余时间用于沿表面滑动。对绝不能在一步结束时陷入静态刚体的刚体设置 `Bullet`。
- **休眠**：岛作为整体休眠，因此只要有一个刚体一直在动（例如角色推着一堆箱子，或持续 `ApplyForce` 的刚体），它接触到的所有刚体都会保持清醒。通过直接修改字段移动的刚体会在下一步被唤醒；修改休眠刚体的形状不会唤醒它，修改后请调用 `Wake`。
- **规模**：宽相与休眠让每一步的开销与醒着的刚体及其接触成正比。在性能场景（`BenchmarkRigidWorld500Bodies` 与 `BenchmarkRigidWorld500BodiesPacked`）中，500 个箱子、圆、多边形和胶囊体落入容器，在运行 Linux 的 Ryzen 9 3900X 单核上下落时每步约 2 ms，堆积密实但仍醒着时每步 5 到 6 ms，均在 60 fps 一帧的 16.7 ms 之内。若游戏需要数千刚体的大规模物理模拟，可在 `github.com/nsgo-dev/game` 之外按需集成专用引擎。

## 运动学刚体、传感器、瓦片与接触事件

- **运动学刚体**：`RigidBody.Kinematic` 让刚体只按指令移动：按 `VX`、`VY` 与 `AngularVel`（不受重力、外力、阻尼或区域效果影响），或者按 `SetTargetPose(x, y, angle)`，它给刚体在下一步恰好到达该位姿所需的速度，并让它精确落在那里，就像动画驱动移动平台一样；没有收到新目标的刚体会停下。求解器把它当作质量无穷大，因此它会推开挡路的动态刚体，并靠摩擦带动停在它上面的刚体，而任何东西都推不动它。它从不与静态刚体、其他运动学刚体或瓦片碰撞，从不休眠，并让它推动的刚体保持清醒。直接给 `X`、`Y` 赋值相当于瞬移：它落入的刚体无论是否在休眠都会被推出，但不会获得它的运动。运动学刚体从不使用质量，但 `NewRigidBox`、`NewRigidCircle` 与 `NewRigidBody` 会把质量不为正的刚体设为静态，而 `Static` 优先于 `Kinematic`，所以要以正质量构造它（如下例），或者以零质量构造后清除 `Static`。
- **传感器**：`RigidBody.Sensor` 让任意类型的刚体只检测重叠而不碰撞。每步之后，`RigidWorld.SensorEvents()`（以及 `OnSensor`）按刚体在这一步结束时的位姿，列出开始或结束与传感器重叠的刚体。传感器从不检测其他传感器，`World.MoveAndSlide` 也会忽略静态传感器。
- **瓦片**：`RigidWorld.Tiles` 接受任意 `TileGrid`，与 `World` 和 `MoveAndSlide` 使用同一个接口。每个 `TileSolid` 格子作为一个方块与动态刚体碰撞，每个 `TileOneWay` 格子只以顶面碰撞：接住从上方落下的刚体，让从下方或侧面来的刚体穿过。被相邻实心瓦片覆盖的面从不推动刚体：偏向这种面的接触会改为对瓦片的开放面裁剪，与链条在平直顶点处的做法相同，因此箱子能在瓦片地面上滑动、圆和胶囊体能滚动，而不会卡在接缝上。同一平面的多块瓦片只按其中最深的一块修正一次刚体位置。`TileLayer` 与 `TileMask` 像刚体的层一样过滤瓦片，`TileFriction`（零表示 `DefaultTileFriction`，即 0.4；负值表示无摩擦）与 `TileRestitution` 设置瓦片材质；高速刚体会像对任何静态刚体一样对瓦片做扫掠；当脚下的瓦片被移除，或网格被移动、改变大小时，休眠的刚体会被唤醒。
- **查询**：`RigidWorld.Raycast`、`RaycastFirst`、`QueryPoint` 与 `QueryOverlap` 按每个刚体的真实位姿（`RigidBody.WorldShape`，旋转的方块视为多边形）进行查询，除只返回刚体的 `QueryPoint` 外（瓦片用 `TileFlagsAt` 读取）也查询瓦片。它们返回与 `World` 查询相同的 `RaycastHit` 与 `OverlapHit` 类型，并设置其中的 `RigidBody`。`ShapeCast`、`ShapeCastAll` 及其 `With` 形式对同样的刚体与瓦片做形状投射。`QueryMask` 选择刚体、传感器（`QuerySensors`，`QueryAll` 和零值掩码都不包含它；只设 `QuerySensors` 时只选传感器，`ShapeCastOptions.QueryMask` 也是如此）、实心瓦片与单向瓦片，`LayerMask` 按层过滤刚体，但从不过滤瓦片，瓦片的 `TileLayer` 与 `TileMask` 只决定它们与什么碰撞。端点不是有限值的射线什么也不命中。命中结果顺序固定：射线与形状投射按距离排序；重叠查询以及距离相同的命中先按 `Bodies` 顺序列出刚体，再按行优先顺序列出瓦片。`World` 的 `Raycast`、`RaycastFirst`、`QueryPoint` 与 `QueryOverlap` 把层掩码留给 `Layer` 变体，而 `RigidWorld` 的查询直接以参数接收 `QueryMask` 与 `LayerMask`（零值表示全部），因为刚体世界还需要用掩码选中传感器；在两者之间移植代码时需要增减这两个参数。
- **接触事件**：每步之后，`RigidWorld.ContactEvents()`（以及 `OnContact`）列出开始的接触，附带接触点、法线和 `ApproachSpeed`（两者在这一步解算接触之前相互接近的速度），以及因为两者相距超过 0.25 px、其中一个离开世界或变成传感器、或两者都变成静态或运动学刚体而结束的接触。法线从 `BodyA` 指向 `BodyB`，与 `physics3d.ContactEvent` 相反，后者的法线从 `BodyB` 指向 `BodyA`。一个刚体无论接触多少块瓦片，都只算作与瓦片的一次接触，因此在瓦片地面上滑行的箱子只会开始接触一次。接触在这一步首次发现两者重叠时开始，或在连续碰撞把高速刚体停在某个表面时开始，在第一次发现两者相距超过 0.25 px 的那一步结束；两个都在休眠的刚体之间的接触会保留，因此静止的一堆刚体不会报告任何事件。开始事件按这一步发现接触的顺序排列（先是按 `Bodies` 顺序的刚体对，再是每个刚体与瓦片），其后是结束事件，最后是按 `Bodies` 顺序列出的、被连续碰撞停住的高速刚体的开始事件，所以相同的输入在每次运行中、以及 `Save` 与 `Restore` 之后，都以相同顺序产生相同的事件。
- **快照**：`RigidBodySnapshot` 保存 `Kinematic`、`Sensor` 以及待到达或上一次的目标，`RigidWorldSnapshot` 保存 `*Grid` 类型的 `Tiles`（`GridSnapshot` 现在也会保存网格原点）、瓦片设置、接触缓存中的瓦片条目，以及下一步用来比较的接触与传感器重叠。`Restore` 会清空 `ContactEvents` 与 `SensorEvents`。其他 `TileGrid` 类型的瓦片无法保存：快照只记录它们存在（`CustomTiles`），`Restore` 会保留调用方的 `Tiles`；而对于原本没有瓦片的世界的快照，`Restore` 会清空 `Tiles`。这些字段使 `SnapshotVersion` 升为 2；`Restore` 仍可读取版本 1 的快照，并把它当作没有瓦片的世界，由于其中没有可供比较的接触，恢复后的第一步会为每个早已存在的接触报告一次 `ContactBegin`。`Checksum` 覆盖了全部这些状态，因此对于任何有刚体相互接触的世界，它给出的摘要与此前的版本不同。

```go
level := physics.NewGrid(40, 12, 16, 16)
level.FillRect(0, 10, 40, 1, physics.TileSolid)
w := &physics.RigidWorld{GravityY: 600, Tiles: level}

lift := physics.NewRigidBox(100, 120, 96, 12, 1)
lift.Kinematic = true
crate := physics.NewRigidBox(100, 90, 20, 20, 1)
w.AddBody(lift, crate)
w.OnContact = func(ev physics.ContactEvent) {
	if ev.Kind == physics.ContactBegin && ev.ApproachSpeed > 200 {
		playThud(ev.Point, ev.ApproachSpeed)
	}
}

// 每个固定步长，升降台跟随动画移动并带动箱子。
lift.SetTargetPose(liftX(t), 120, 0)
w.Step(dt)

if hit, ok := w.RaycastFirst(eye, target, physics.QueryAll, 0); ok && hit.RigidBody == crate {
	// 能看见箱子，命中点 hit.Point，朝向 (hit.NormalX, hit.NormalY)。
}
```

## 休眠与高速刚体

```go
w := &physics.RigidWorld{GravityY: 600}
floor := physics.NewStaticRigidBox(100, 110, 400, 20)
crate := physics.NewRigidBox(100, 90, 20, 20, 1)
shot := physics.NewRigidCircle(0, 0, 4, 0.1)
shot.Bullet = true // 绝不会在一步结束时陷入静态刚体
w.AddBody(floor, crate, shot)

// ……静止半秒之后：
if crate.Sleeping() {
	crate.ApplyImpulse(0, -100) // 唤醒它以及它接触到的所有刚体
}
```

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
rope := physics.NewRopeJoint(pivot, pendulum, core.Point{}, core.Point{}, 80)
rope.BreakForce = 4000
rw.AddJoint(rope)
rw.OnJointBreak = func(ev physics.JointBreakEvent) {
	// 确定性地处理绳索或可破坏连接断裂。
	_ = ev
}

loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		rw.Step(dt)
	},
}
```
