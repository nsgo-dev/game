# vehicle

```go
import "github.com/nsgo-dev/game/packages/game/vehicle"
```

基于 [`physics3d`](physics3d.md) 世界的射线车辆：车轮碰撞、弹簧阻尼悬挂、纵向与横向轮胎摩擦和车辆控制器，以及竞速用的检查点、圈速与幽灵回放。

## 模型

一辆 `Vehicle` 由一个动态 `physics3d.Body`（车身，其 `Pos` 即质心）和一组车轮组成。车轮没有自己的刚体：每个固定步长里，`Vehicle.Step` 从每个车轮的挂点沿车身向下的轴投射一条射线，因此一个车轮只花一次射线检测，也不会被几何体卡住。

对每个着地的车轮，一步会：

1. **悬挂**：把射线的压缩量换算成弹簧阻尼力 `Stiffness * 压缩量 - Damping * 伸长速度`，只推不拉，并受 `MaxForce` 限制，在挂点处把车身向上推。
2. **轮胎**：计算纵向冲量（驱动轮上的引擎、制动轮上的刹车、滚动阻力）和抵消该车轮所分担侧滑的横向冲量，再把两者之和限制在 `Friction * 载荷` 的摩擦圆内。达到上限的车轮报告 `Sliding`，可用于胎痕或轮胎尖啸。
3. **反作用**：把相反的冲量施加到车轮下方的动态刚体上，所以车能推动箱子，或压动驶过的平台。

每个车轮的轮胎冲量都从同一个车身状态算出，算完后再统一施加，因此结果与车轮顺序无关，左右对称的车会直线行驶。随后空气阻力 `Drag * 速度²` 作用在车身上，再由 `World.Step` 积分。

车辆在局部空间朝向 `-Z`，`+X` 向右，`+Y` 向上，与[从 Blender 导入 3D 模型](3d-models-blender.md)的约定一致。

## 驾驶车辆

```go
world := physics3d.NewWorld()
world.AddBody(physics3d.NewStaticBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 200, Y: 1, Z: 200}}),
	physics3d.Vec3{Y: -1},
))
car, err := vehicle.New(world, vehicle.ReferenceConfig(), physics3d.Vec3{Y: 0.65}, physics3d.QuatIdentity())
if err != nil { ... }

controls := vehicle.DefaultControls()
loop := &game.Loop{Update: func(dt time.Duration) {
	actions.Step()
	car.SetInput(controls.Read(actions))
	car.Step(world, dt)
	world.Step(dt)
}}
```

每个固定步长调用一次 `Vehicle.Step`，放在 `World.Step` 之前。`Input` 包含 `Throttle`（`[-1, 1]`，负值倒车）、`Brake`（`[0, 1]`）、`Steer`（`[-1, 1]`，正值向右转）和 `Handbrake`；`SetInput` 会把每个轴限制在范围内并把 NaN 当作 0，输入在再次设置前一直有效。转向按 `SteerRate` 逐步逼近请求的角度，引擎力在 `MaxSpeed` 时衰减到零。

`Controls` 指定车辆读取的轴和动作（默认为 `"throttle"`、`"brake"`、`"steer"` 和 `"handbrake"`），所以同一份代码既能用实时的 `*input.Actions` 驾驶，也能在录制时读 `replay.Recorder`、回放时读 `replay.Player`。

读取车辆状态：

- `Speed()` 是沿 `Forward()` 的带符号速度，倒车时为负；`Up()` 是车身的向上轴，可用于检测翻车。
- `Grounded()` 统计着地的车轮数，每个 `Wheels[i]` 报告 `Contact`、`ContactPoint`、`ContactNormal`、`Ground`、`SuspensionLength`、`SuspensionForce`、`SteerAngle`、`SpinSpeed`、`SpinAngle` 和 `Sliding`。
- `WheelTransform(i)` 返回第 `i` 个车轮在世界空间中的中心和朝向（含转向与滚动），用于绘制车轮网格，网格的轮轴是其局部 X 轴。
- `Reset(pos, rot)` 把车瞬移到新位置、停下并清空车轮状态，例如在检查点重生。

`RayMask` 选择车轮射线检测的层；触发器刚体和车身自身总会被跳过，所以检查点不会把车托起来。静止且没有输入的车会像其他刚体一样休眠，`SetInput` 给出油门、刹车、转向或手刹时再唤醒。

## 车辆文件

车辆调校是一个 `Config`，以带版本号的 YAML 编写。`ParseConfig` 解码并校验，拒绝未知字段和多余的文档；`EncodeConfig` 写出确定性的 2 空格缩进 YAML，`ParseConfig` 读回后不变。长度单位为米，质量为千克，力为牛顿，角度为度；位置在车身空间中，相对于质心。

```yaml
version: 1
name: reference
mass: 1200
chassisHalfExtents: [0.9, 0.3, 2]
chassisOffset: [0, 0.2, 0]
engineForce: 6000
reverseForce: 3000
maxSpeed: 40
maxReverseSpeed: 8
brakeForce: 12000
handbrakeGrip: 0.4
maxSteerAngle: 30
steerRate: 120
drag: 0.4
rollingResistance: 0.015
rollInfluence: 0.1
angularDamping: 0.5
wheels:
  - name: front-left
    anchor: [-0.8, -0.1, -1.3]
    radius: 0.35
    restLength: 0.3
    stiffness: 30000
    damping: 4000
    maxForce: 20000
    friction: 1.2
    steer: true
    brake: true
  # 后面依次是 front-right、rear-left 和 rear-right；后轮不设 steer，而设 drive 和 handbrake。
```

| 字段 | 含义 |
| --- | --- |
| `mass`、`chassisHalfExtents` | 车身质量和碰撞盒半尺寸，均 `> 0` |
| `chassisOffset` | 碰撞盒相对质心的偏移；Y 为正时质心在盒内更低，车更稳 |
| `engineForce`、`reverseForce` | 全油门、静止时的总驱动力，平均分给各驱动轮 |
| `maxSpeed`、`maxReverseSpeed` | 驱动力衰减到零时的速度（m/s） |
| `brakeForce` | 全刹车时的总制动力，平均分给各制动轮 |
| `handbrakeGrip` | 拉手刹时手刹轮的横向抓地力，`[0, 1]`，越低越容易侧滑 |
| `maxSteerAngle`、`steerRate` | 最大转向角（`[0, 80]` 度）和转向轮的转动速度（度每秒） |
| `drag`、`rollingResistance` | 空气阻力系数，以及占每个车轮载荷比例的滚动阻力 |
| `rollInfluence` | 横向轮胎力使车身侧倾的程度，`[0, 1]`：0 作用在质心高度，1 作用在接地点 |
| `angularDamping` | 车身的 `physics3d.Body.AngularDamping` |
| `wheels` | 1 到 32 个车轮，名称唯一，顺序固定 |
| `anchor`、`radius`、`restLength` | 悬挂顶部位置、车轮半径和无载荷时的悬挂长度；射线长 `restLength + radius` |
| `stiffness`、`damping`、`maxForce` | 弹簧刚度（N/m）、阻尼（N·s/m）和可选的力上限（0 表示不设上限） |
| `friction` | 轮胎摩擦系数：轮胎合力不超过 `friction` 乘以车轮载荷 |
| `steer`、`drive`、`brake`、`handbrake` | 车轮接收哪些输入 |

`ReferenceConfig()` 返回参考车：一辆 1200 kg 的后驱两厢车，加速约 0.5 g，刹车约 1 g，极速 40 m/s。对应文件是 `vehicle/testdata/reference_car.yaml`。

## 比赛与圈数

一场比赛是一组有序的检查点触发器。`AddCheckpoint` 向世界添加一个静态触发器刚体，`NewRace` 按指定圈数跟踪各车手依次通过它们：

```go
gate := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 7, Y: 2, Z: 0.5}})
race, err := vehicle.NewRace(3,
	vehicle.AddCheckpoint(world, gate, start),
	vehicle.AddCheckpoint(world, gate, sector1),
	vehicle.AddCheckpoint(world, gate, sector2),
)
world.OnTrigger = race.Observe
racer := race.AddRacer(car.Body, "player")
race.OnLap = func(ev vehicle.LapEvent) { ... }

// 每个固定步长：
race.Tick()
car.Step(world, dt)
world.Step(dt)
```

检查点 0 是起点/终点线。车手的第一圈从加入时的那一步开始；必须依次进入检查点 1、2……再进入检查点 0 才算完成一圈，进入其他检查点会被忽略，所以抄近路不算数。时间以固定步数计：每步在 `World.Step` 之前调用一次 `Tick`，乘以步长即得实际时间。

每个 `Racer` 报告 `Laps`、`Next`、`LastLap`、`BestLap`、`Finished` 和 `FinishStep`；每完成一圈，`OnLap` 都会收到一个 `LapEvent`，其中注明这圈是否为该车手的最快圈、是否为最后一圈。`Standings()` 给出名次：已完赛的按完赛步数排，其余按完成圈数和通过的检查点数排，并列时按加入顺序。`Observe` 会忽略不是进入检查点的事件，所以可以接在其他触发器处理函数之后。

## 幽灵

`Ghost` 是一圈的录像：每隔 `Interval` 步采样一次车身姿态，回放时作为半透明的对手。它只是姿态数据，所以回放不需要物理模拟。

```go
rec := &vehicle.GhostRecorder{Interval: 2}
race.OnLap = func(ev vehicle.LapEvent) {
	rec.Record(car.Body.Pos, car.Body.Rot)
	if ev.Best {
		best = rec.Ghost(60, ev.Steps)
	}
	rec.Reset()
}
// 每次 World.Step 之后：
rec.Record(car.Body.Pos, car.Body.Rot)

// 绘制幽灵，配合 game.Loop 的绘制插值系数：
pos, rot := best.Pose(float64(step) + alpha)
```

`Pose` 在帧之间对位置做线性插值、对朝向做球面插值，超出录像范围的步数会钳制到第一帧或最后一帧。

和本仓库的其他文件一样，幽灵是文本：`EncodeGhost` 写出带版本号的 YAML，每帧的位置和旋转各占一行流式写法，`ParseGhost` 读回后不变，并拒绝未知字段和超过 60 Hz 下一小时的帧数。

```yaml
version: 1
rate: 60
interval: 2
lapSteps: 812
frames:
  - p: [0, 0.62, 14]
    r: [0, 0, 0, 1]
  - p: [0, 0.62, 13.98]
    r: [0, 0, 0, 1]
```

若要精确重现一次驾驶（例如用于缺陷报告），应改为录制车辆的输入：`replay.NewRecorder(actions, seed, rate, controls.Actions(), controls.Axes())`，再通过 `replay.Player` 回放到 `Controls.Read`。

## 确定性

`Vehicle.Step` 不使用 map、系统时钟或随机数，并按 `Config` 顺序遍历车轮，`physics3d` 也按固定顺序步进，因此在同一平台上，相同的 `Config`、起始姿态和每步 `Input` 在同一个世界里会得到逐位相同的姿态、圈速和幽灵。本包的验收测试让参考车在测试赛道（两条 40 m 直道由 15 m 半径的发夹弯相连，并有一条减速带）上跑两圈，并检查两次运行的终态逐位相同；另一个测试用 `replay` 录制这次驾驶，让录像经过文件格式往返，再回放到新的世界中，得到相同的终态。

## 限制

- 车身只作为一个盒子参与碰撞，且 `physics3d` 把转动惯量视为各向同性，所以俯仰、侧倾和偏航对同样的力矩反应相同；可通过 `rollInfluence`、`chassisOffset` 和 `angularDamping` 调整操控。
- 轮胎使用摩擦圆，而不是 Pacejka 之类基于滑移曲线的轮胎模型；引擎是随速度衰减的力，而不是带挡位的扭矩曲线。
- 与 `physics3d` 的射线检测一样，车轮射线会逐个检测世界中的每个刚体；大型静态场景应放进三角网格或高度场。
