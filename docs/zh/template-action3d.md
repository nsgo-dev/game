# 动作冒险模板

`action3d` 是一个第三人称 3D 动作冒险游戏：一名骑士穿过有围墙的庭院，与守卫交战，跳上高台取得钥匙，钥匙打开通往神龛的大门。到达神龛即赢得本局；生命值耗尽则失败。用它创建项目：

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template action3d mygame
cd mygame
go tool nsgo dev
```

不使用 `game` 工具时，也可以运行 `nsgo init -template github.com/nsgo-dev/game/template/action3d -module example.com/mygame mygame`（见[模板](templates.md)）。

## 它展示了什么

- **环绕相机。** [`scene3d.OrbitController`](scene3d.md) 以固定的高度和距离跟随主角，随玩家的操作转向；主角与相机之间有墙时，用 [physics3d](physics3d.md) 的射线检测把相机移到墙前。
- **角色移动。** 每个角色都是静态方块世界中的一个 `physics3d.CharacterController` 胶囊体：相对相机方向奔跑，跳上台阶跨不上去的高台，门打开时它的碰撞体离开世界。
- **由状态机混合的动画。** 奔跑、跳跃、攻击、受击和倒地的片段由 [`anim3d.StateMachine`](anim3d.md) 在一副方块骨架上播放。移动状态用 `anim3d.BlendSpace1D` 按角色速度把待机混合到奔跑；条件转换在角色离地时开始跳跃、在挥砍结束后回到移动；每一击都在攻击片段的 `hit` 事件上命中，由动画决定命中的时刻。
- **近战敌人。** 敌人在视野和拴绳范围内追击主角，挥出一记较慢的攻击（主角的命中会打断它），主角跑远后走回原处。
- **关卡就是场景文件。** 方块、出生点、敌人、钥匙、门、目标和灯都是[场景文件](editor.md)中带组件的对象；捡起钥匙就打开门，是场景文件里的一条连接决定的，而不是 Go 代码。
- **渲染。** [render3d](render3d.md) 的光照 PBR 管线用同一个立方体网格绘制所有方块，由月光、环境补光和场景中的灯照明，可运行在 NSGo 的所有 GPU 后端上，包括 WebGL 2。
- **所有输入方式。** 键盘与鼠标、游戏手柄和触控都经过同一个 [`input.Actions`](input.md)，按 `game.yaml` 中的输入映射绑定；屏幕被触摸后，会出现屏幕摇杆和按钮（`input.VirtualPad`）。
- **会玩游戏的测试。** 无头 nsgotest 测试用游戏手柄把整局打到胜利，用键盘打到失败，操作触控和鼠标，检查每个数据文件有效且是规范格式，并在软件 GPU 上绘制关卡。

## 操作

| | 键盘与鼠标 | 游戏手柄 | 触控 |
| --- | --- | --- | --- |
| 移动 | W A S D 或方向键 | 左摇杆或十字键 | 在左半屏拖动 |
| 转动相机 | Q 和 E，或用鼠标拖动 | 右摇杆或肩键 | 在右半屏拖动 |
| 跳跃 | 空格 | A（下方按键） | 跳跃按钮 |
| 攻击 | J 或回车 | X（左方按键）或右扳机 | 攻击按钮 |
| 再玩一次 | R | Start | 再玩一次按钮 |

## 项目结构

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | [项目文件](project.md)：名称、标识符、窗口、输入映射、自动加载的场景和数据文件，以及导出预设。 |
| `scenes/keep.scene.yaml` | 关卡。每个对象的 `x`、`y`（向上）和 `posZ` 以米为单位确定位置，缩放决定方块大小，组件说明它是什么：`block`、`spawn`、`enemy`、`key`、`door`、`goal` 或 `lamp`。`connections` 把钥匙的 `collected` 事件连接到门的 `unlock` 处理器。 |
| `data/actors.yaml` | 主角和敌人的参数、两者共用的方块骨架，以及动画片段，单位是米、度和秒。 |
| `actors.go` | `data/actors.yaml` 的格式：版本 1，拒绝未知字段，编码器的输出读回后得到相同的字节。 |
| `level.go` | 关卡的组件类型，以及场景如何变成方块、出生点和灯。 |
| `game.go` | 模拟：物理世界、主角和敌人的规则、钥匙、门、胜负。它不了解输入设备和 GPU，测试可以直接推进它。 |
| `rig.go` | 把骨架和片段转换为 anim3d 片段，为每个角色构建状态机并摆放它的方块。 |
| `render.go` | 相机、灯光和 GPU 渲染器。 |
| `view.go` | 根组件：固定步长的 [`game.Loop`](loop.md)、输入、HUD 和触控按钮。 |
| `*_test.go` | 通关测试，以及输入、渲染和文件格式测试。 |

## 在哪里修改

- **关卡。** 在 `scenes/keep.scene.yaml` 中移动墙、增加敌人或灯，或画一个新关卡，可以手写，也可以用场景编辑器（`go run github.com/nsgo-dev/game/cmd/game editor .`）。每个对象都位于所在图层的顶层，只有出生点可以旋转（用 `rotY`）：关卡把位置读作世界坐标、把方块读作与坐标轴对齐的盒子，所以会拒绝嵌套在其他对象里的对象和旋转过的方块。测试会检查它仍能加载；`game_test.go` 中机器人的路线说明了如何赢得一局，所以新的布局需要新的路线。
- **战斗。** 所有数值都在 `data/actors.yaml` 中：敌人的 `attackSpeed` 小于 1 会让挥砍变慢，`health`、`reach`、`damage`、`cooldown` 和 `knockback` 不改代码就能调整难度。
- **动画。** 编辑 `data/actors.yaml` 中片段的关键帧，或增加一个片段，并在 `rig.go` 的 `newAnimator` 中为它增加一个状态。
- **新对象。** 在 `level.go` 的 `levelComponents` 中注册组件类型，例如移动平台或拉杆，在 `buildLevel` 中读取它，再在场景中把它的事件连接到处理器。
- **模型。** 用 glTF 模型替换方块：用 `gltf` 包加载（见[从 Blender 导入 3D 模型](3d-models-blender.md)），用 `render3d.UploadMesh` 上传网格，并在 `renderer.draw` 中绘制。

编辑 `game.yaml` 之后，`go run github.com/nsgo-dev/game/cmd/gameproject sync` 会写入 `nsgo.yaml` 与它共有的设置；移动或增加文件之后，`go run github.com/nsgo-dev/game/cmd/game index` 会更新项目的稳定文件 ID 索引 `game.index.yaml`。
