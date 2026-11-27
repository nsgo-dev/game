# 3D 赛车模板

`racing3d` 模板是一个小型 3D 赛车游戏：从发车格最后一位出发，在封闭赛道上与两名电脑对手比赛两圈，包含倒计时、检查点、最佳圈幽灵车和结算画面。它可以用键盘、手柄或触屏操作，运行在桌面、手机和网页上。

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template racing3d myracer
cd myracer
go tool nsgo dev
```

[项目模板](templates.md)介绍了 `game new` 和 `nsgo init -template`，后者从模块 `github.com/nsgo-dev/game/template/racing3d` 写出同样的项目。

## 模板展示了什么

- [`vehicle`](vehicle.md) 的射线车辆，运行在 [`physics3d`](physics3d.md) 世界上，所有车辆的调校都来自同一个 `vehicle.Config` 文件。
- 用 `vehicle.Race` 统计圈数和检查点：车辆按顺序通过全部检查点才算完成一圈，抄近路没有好处。
- 三秒倒计时，显示圈数、名次和单圈计时的 HUD，以及结算画面。第一名冲线即获胜；名次靠后，或在两名对手都完赛前没有完赛，即失败。
- 沿赛车线行驶的对手：它们朝赛车线前方的一点转向，并保持为每个弯道预先规划好制动的速度，再乘以各自的速度系数。
- 最佳飞驰圈的幽灵车：由 `vehicle.GhostRecorder` 录制，从第二圈起的每一圈陪你比赛，并以 `vehicle.EncodeGhost` 写出的 YAML 保存在应用存储中，下次启动仍在。第一圈从起跑线后方的发车格起步，既不会存为幽灵车，也不会与幽灵车同跑；只有一圈的比赛没有幽灵车。保存的幽灵车记有赛道文件的哈希，修改 `data/track.yaml` 中的任何内容都会让它作废。
- 在 GPU 视图上用 [`render3d`](render3d.md) 的光照 PBR 管线绘制的场景：路面、路肩、立柱和车辆是代码生成的网格，路边的木箱是[占位素材包](placeholder.md)的 `crate.glb`，用 `gltf` 读取。
- `game.yaml` 中的一份输入映射同时覆盖键盘、手柄和触屏，由 `input.Actions` 和 `vehicle.Controls` 读取，屏幕上的摇杆和踏板由 `input.VirtualPad` 提供。
- 通过应用用每种输入完整跑完比赛的测试，分别得到胜利和失败。

## 操作

| | 键盘 | 手柄 | 触屏 |
| --- | --- | --- | --- |
| 加速 | 上方向键或 W | 右扳机或 A | GAS |
| 刹车，停下后倒车 | 下方向键或 S | 左扳机或 X | BRAKE |
| 转向 | 左右方向键，或 A 和 D | 左摇杆或方向键 | 左下区域任意位置的摇杆 |
| 手刹 | 空格 | B | |
| 回到赛道 | R | Y | R |
| 再来一局 | 回车 | Start | Race again |

## 项目结构

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | 项目文件：窗口和输入映射，包含动作 `throttle`、`brake`、`steerLeft`、`steerRight`、`handbrake`、`respawn`、`confirm`，以及由按键和左摇杆组成的 `steer` 轴。 |
| `data/track.yaml` | 赛道：中心线控制点、路宽、圈数、检查点、发车格，以及各对手的名字、颜色、速度系数和行驶车道。 |
| `data/car.yaml` | 车辆调校，即一个 `vehicle.Config`。 |
| `track.go` | 赛道文件的格式：`ParseTrack` 拒绝未知字段和越界的值，`EncodeTrack` 原样写回。 |
| `course.go` | 由赛道生成的几何：沿中心线每隔约两米的采样点、检查点、发车格，以及每个采样点上赛车线的速度。 |
| `race.go` | 不含绘制的比赛本身：物理世界、车辆、倒计时、圈数、名次、结算和幽灵车。 |
| `driver.go` | 对手使用的赛车线驾驶员。 |
| `scene.go`、`meshes.go`、`renderer.go` | 网格、灯光和 GPU 绘制。 |
| `game.go` | 根组件：GPU 视图、HUD、结算画面、触屏控件、追尾相机和 `game.Loop`。 |
| `game_test.go`、`race_test.go`、`track_test.go` | 通过应用分别用手柄、键盘和触屏进行的比赛；只在模拟上运行的比赛；赛道格式。 |

比赛是基于 `physics3d` 和 `vehicle` 的纯 Go 代码，不涉及绘制，因此 `race_test.go` 几毫秒就能跑完整场比赛。应用层的测试用 `nsgotest.NoGPU` 启动，因为 `nsgotest` 用软件光栅器绘制 GPU 视图，跑几分钟的比赛太慢；`TestRaceViewDraws` 单独以小尺寸检查绘制结果。

## 从哪里改起

- **赛道**：修改 `data/track.yaml` 中的 `points`、`width` 和 `start`，每个控制点都要在距原点 300 米以内，也就是地面所在的范围。路面、路肩、检查点、发车格和赛车线都随控制点生成，`TestCourseGeometry` 会检查结果。
- **对手**：每个对手的 `pace` 决定其快慢，文件最多容纳七名对手。`laps` 决定比赛长度。
- **车辆**：修改 `data/car.yaml`；[vehicle](vehicle.md) 介绍了每个字段。
- **车辆之间的碰撞**：默认车辆互相穿过，拥挤的弯道不会毁掉一场比赛。在 `race.go` 的 `newRace` 中把 `layerCar` 加进车身的碰撞掩码，车辆就会相撞。
- **操作**：修改 `game.yaml` 中的绑定；代码只读取动作和轴的名字。
- **美术**：在 `sceneMeshes` 中用 `gltf.LoadFS` 加载自己的 glTF 模型，像木箱那样把图元作为部件加入。
