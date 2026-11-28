# 从 Construct 迁移

把 Construct 的布局、对象类型、事件表和行为逐一对应到这个游戏层，并给出讲解每一项的页面。

最大的区别在于这里没有事件表。在 Construct 里，你在事件表中添加条件和动作，再给对象类型挂上行为；在这里，游戏是 NSGo 组件里的 Go 代码，布局是 YAML 场景文件，可以手写，也可以由[编辑器](editor.md)保存，事件表里的规则变成每个固定步长运行的 Go 系统，简单的“发生这件事就做那件事”的连接则写在场景文件的 `connections` 里。

## 速览

| Construct | 这里 | 指南 |
| --- | --- | --- |
| 布局、图层 | 带有序图层的 YAML 场景文档（`sceneformat`） | [编辑器与场景格式](editor.md) |
| Go to layout | 带过渡效果的 `scene.Manager.Switch` | [scene](scene.md) |
| 对象类型、实例 | 为场景文件注册的组件类型与预制体；数据放在 `entities.World` 中 | [编辑器与场景格式](editor.md#组件类型)、[实体](entities.md) |
| 实例变量 | 场景对象的 `properties` 及其组件字段 | [编辑器与场景格式](editor.md) |
| 家族（Families） | 查询所有带有共同组件的实体的系统 | [实体](entities.md) |
| 事件表、条件与动作 | `game.Loop` 上的 Go 系统；场景的 `connections` 把对象的事件连到处理函数 | [Loop](loop.md)、[编辑器与场景格式](editor.md#连接490) |
| Every tick | 固定 `dt` 的 `game.Loop.Update(dt)` | [Loop](loop.md) |
| 全局变量 | 在根组件中创建一次、再传给需要它的场景的普通 Go 值 | [从 Godot 迁移](from-godot.md#场景) |
| 函数、自定义动作 | Go 函数与方法 | — |
| Keyboard、Mouse、Touch 与 Gamepad 对象 | 带命名 action 和轴的 `input.Actions` | [input](input.md) |
| 精灵、动画 | `sprite.LoadAtlas` 的帧与动画，`*.states.yaml` 精灵状态机 | [sprite](sprite.md)、[animclip](animclip.md#精灵状态机) |
| Tilemap 对象 | 通过 `tilemap.LoadTiled` 读取 Tiled 地图 | [tilemap](tilemap.md) |
| Particles 对象 | `particles.Emitter` | [particles](particles.md) |
| 时间轴（Timeline） | 通过 `tween.Timeline` 播放的 `animclip` 片段 | [animclip](animclip.md)、[tween](tween.md) |
| Local Storage | 带迁移的版本化 JSON `save` 存档槽 | [save](save.md) |
| 导出项目 | `go tool game release` | [构建与发布](shipping.md) |

## 行为

| Construct 行为 | 这里 | 指南 |
| --- | --- | --- |
| Platform | `platformer.Controller`，从 YAML 配置读取土狼时间、跳跃缓冲和蹬墙跳 | [platformer](platformer.md) |
| Solid | 内置的 `collider2d` 组件，或 `physics.TileSolid` 图块 | [physics](physics.md) |
| Jump-thru | `physics.TileOneWay` 图块，或设置了 `OneWay` 的 `Body` | [physics](physics.md) |
| Pathfinding | 2D `NavMesh` 上的 `pathfind.NavAgent`，或网格 `AStar` | [pathfind](pathfind.md) |
| Line of Sight | 具备视觉、听觉和记忆的 `ai.Perception` | [ai](ai.md#感知) |
| Timer | 内置的 `timer` 组件，或 Go 中的 `flow.Timers` | [编辑器与场景格式](editor.md#内置计时器组件1362)、[flow](flow.md) |
| Tween | `tween.Tween` 与 `tween.Timeline` | [tween](tween.md) |
| Physics | `physics.RigidWorld` 与 `RigidBody` | [2D 刚体](physics2d-rigid.md) |
| Bullet、Sine、Rotate、Orbit、Wrap、Bound to layout、Destroy outside layout、Fade、Flash、Move To | 尚未提供；可直接挂载的运动组件由 [#1751](https://github.com/nsgo-dev/game/issues/1751) 跟踪。在此之前，用 Go 系统里的几行代码实现 | — |
| 8 Direction、Tile Movement、Car、Drag & Drop、Turret | 尚未提供；由输入驱动的控制器组件由 [#1752](https://github.com/nsgo-dev/game/issues/1752) 跟踪 | — |

## 第一个项目

从与你的游戏最接近的模板开始：`go run github.com/nsgo-dev/game/cmd/game@latest new -template platformer2d mygame` 会写出一个带房间、角色控制器和检查点的平台跳跃游戏（[指南](template-platformer2d.md)），`new -list` 会列出其他模板（[项目模板](templates.md)）。用 `go tool game editor` 打开项目：它的 2D 视口就是布局视图，检查器代替属性栏编辑对象的组件和属性，它保存的文件就是游戏加载的那份 YAML。
