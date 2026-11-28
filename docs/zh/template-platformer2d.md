# platformer2d 模板

`platformer2d` 是一个小型横版平台跳跃游戏：提灯人在灯笼山的三个房间里奔跑跳跃，收集火花，点亮一盏灯作为重生点，抵达信标即获胜。尖刺和深坑会让玩家失去一条命，命用完这一局就输了。它基于 [`platformer`](platformer.md)、[`physics`](physics.md)、[`tilemap`](tilemap.md)、[`rooms`](rooms.md)、[`input`](input.md)、[`sprite`](sprite.md) 和 [`screen`](screen.md) 构建，美术由代码绘制。

模板包含可由编辑器打开的 `scenes/level1.scene.yaml` 入口场景。在碰撞与玩法规则逐步迁移到场景组件期间，`data/level.yaml` 的运行时网格仍是权威数据。

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template platformer2d mygame
cd mygame
go tool nsgo dev
go tool game playtest
```

`nsgo init -template github.com/nsgo-dev/game/template/platformer2d` 会写出同样的项目，见[项目模板](templates.md)。

## 它展示了什么

- 基于 `physics.CharacterBody` 的 [`platformer.Controller`](platformer.md)：可变高度跳跃、土狼时间、跳跃缓冲、单向平台、贴墙下滑和蹬墙跳，全部由 `data/player.yaml` 调校。
- 在 `data/level.yaml` 里用文本画出的关卡，构建为用于碰撞的 `physics.Grid`、用于绘制的 `tilemap.Tilemap`，以及尖刺、火花、灯和信标的 `physics.Trigger` 感应区。
- 来自 `data/rooms.yaml`、各有相机边界的房间：[`rooms.Director`](rooms.md) 把相机从一个房间滑到下一个房间，玩家等相机停稳后再继续。
- `game.yaml` 里的一份输入映射覆盖键盘和手柄，用 `Project.BindInput` 绑定，并配合来自 `controls.touch.yaml` 的 `input.TouchControls`，在视图坐标的安全区内绘制屏幕触控控件。
- 按整数倍缩放的 320×180 虚拟屏幕，画布上方是原生 HUD 和一局结束时的面板，在手机上请求横屏方向（`cx.RequestOrientation(core.OrientationLandscape)`）。
- 测试分别用键盘、手柄和双指触屏把关卡玩到获胜，一直撞向尖刺直到输掉，并检查规则、控制器的时序以及每个数据文件都是规范格式。

## 操作

| | 键盘 | 手柄 | 触屏 |
| --- | --- | --- | --- |
| 奔跑 | 左右方向键、A 和 D | 左摇杆、方向键 | 左下角的方向盘（手指可在左右之间滑动） |
| 跳跃（按住跳得更高） | 空格、上方向键、W | 下方按键 | 右下角的 JUMP 按钮 |
| 一局结束后再来一局 | R、回车 | Start | “Play again”按钮 |

屏幕触控控件在手机和平板上一开始就会显示，在桌面上默认隐藏直到屏幕被触摸，再次按下键盘或使用手柄时会重新隐藏。

## 组织方式

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | 项目文件：窗口、虚拟屏幕、输入映射和导出预设。 |
| `controls.touch.yaml` | 屏幕触控布局：左下角方向盘和右下角跳跃按钮，由 `input.TouchControls` 读取。 |
| `data/level.yaml` | 关卡：每个字符一个图块，以及图块大小和一局开始时的命数。 |
| `data/rooms.yaml` | 以世界坐标表示的房间，以及进入房间时的相机过渡，格式见 [`rooms`](rooms.md#房间地图)。 |
| `data/player.yaml` | 控制器的调校参数，格式见 [`platformer`](platformer.md)。 |
| `level.go` | 关卡格式：解析、校验和规范编码。 |
| `world.go` | 关卡的一局：碰撞网格、玩家、感应区、命数、检查点、终点和房间，每个固定步长推进一次，不涉及设备和绘制。 |
| `game.go` | 根组件、固定步长循环、输入采样、HUD 和一局结束时的面板。 |
| `draw.go`、`art.go` | 绘制一局，以及由代码绘制的图块集和玩家帧。 |
| `playtest.yaml`、`playtest_test.go` | [`platformer-runner`](playtest.md) 自动化试玩配置与由 `go tool game playtest` 驱动的 `TestPlaytest` 适配器。 |
| `*_test.go` | 通关测试、规则、控制器时序和数据文件。 |

关卡是纯文本，每行一排图块：

```yaml
version: 1
name: Lantern Hills
tileSize: 16
lives: 3
tiles: |
  #....................#
  #.P....^^....o.......#
  ######################
```

| 字符 | 图块 |
| --- | --- |
| `.` | 空 |
| `#` | 地面，各个方向都是实心 |
| `=` | 可以从下方跳上去的平台 |
| `^` | 尖刺，会失去一条命 |
| `o` | 要收集的火花 |
| `C` | 检查点的灯 |
| `G` | 终点信标，恰好一个 |
| `P` | 玩家出发的位置，恰好一个 |

`name` 会显示在一局结束时的面板上。每一行宽度相同，每个 `P` 和 `C` 下方都要有地面或平台。未知字段、未知字符和不是 1 的版本号都会被拒绝；游戏写回文件时逐字节不变，所以代码评审里只会看到真正的改动。

## 在哪里修改

- **新关卡**：编辑 `data/level.yaml` 里的图块，并让 `data/rooms.yaml` 覆盖每一列；`parseLevel` 会拒绝脚下没有地面或平台的起点（`P`）和灯（`C`），`TestDataFilesAreCanonical` 和 `TestRoomsCoverTheLevel` 会指出问题所在。通关测试会在玩家前方遇到尖刺、深坑和墙时起跳，路线如果需要更多操作，就要修改 `game_test.go` 里的 `obstacleAhead`。
- **跳跃手感**：`data/player.yaml`。更长的 `coyote_time` 或 `jump_buffer` 能容忍按晚了或按早了，`wall_jump_speed_y: 0` 关闭蹬墙跳。
- **新的图块种类**：在 `level.go` 里加一个字符，在 `newWorld` 里加一个分支决定它的作用，在 `newTilemap` 里加一个分支决定它的样子。
- **敌人和移动平台**：给每个对象一个 `physics.Body`，在 `world.step` 里移动它，把移动平台传给 `Controller.Step`，再加一个设置 `hurt` 的感应区。
- **操作**：键盘和手柄在 `game.yaml` 的 `input` 部分，屏幕触控控件在 `controls.touch.yaml`。
- **美术**：在 `newPlayerAtlas` 里用 `sprite.LoadAtlas` 加载图集，在 `newTilemap` 里加载图块集图片，或者用 `tilemap.LoadTiled` 从 Tiled 地图加载整个关卡。
- **声音**：用 `game.yaml` 里 `audio` 部分配置的 `sound.Mixer`。
