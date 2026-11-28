# 从 GameMaker 迁移

把 GameMaker 的房间、对象、事件、精灵、序列和导出逐一对应到这个游戏层，并给出讲解每一项的页面。

最大的区别在于游戏逻辑写在哪里。在 GameMaker 里，你在对象的事件中写 GML，再把实例摆进房间；在这里，游戏是 NSGo 组件里的 Go 代码，房间是 YAML 场景文件，可以手写，也可以由[编辑器](editor.md)保存，每一步都要运行的代码是固定步长循环上的系统，而不是每个对象各自的 Step 事件。

## 速览

| GameMaker | 这里 | 指南 |
| --- | --- | --- |
| Step 事件、游戏速度 | 固定 `dt` 的 `game.Loop.Update(dt)`、`Loop.Rate` | [Loop](loop.md) |
| Draw 事件 | `game.Loop.Draw(alpha)` 及它触发重绘的 `core.Canvas` | [Loop](loop.md) |
| Draw GUI 事件 | `scene.Scene` 中位于 `SpaceScreen` 的图层，或 [gameui](gameui.md) 控件 | [scene](scene.md) |
| 房间、房间图层 | 带有序图层的 YAML 场景文档（`sceneformat`），由 `sceneformat.Build` 构建 | [编辑器与场景格式](editor.md) |
| `room_goto`、房间过渡 | 带过渡效果的 `scene.Manager.Switch`；相连且各有相机边界的房间用 [rooms](rooms.md) | [scene](scene.md) |
| 对象、父对象 | 为场景文件注册的组件类型与预制体；数据放在 `entities.World` 中 | [编辑器与场景格式](editor.md#组件类型)、[实体](entities.md) |
| 对象变量、Variable Definitions | 场景对象的 `properties` 及其组件字段 | [编辑器与场景格式](editor.md) |
| `instance_create_layer`、`instance_destroy` | `entities.Prefab` 的 `Spawn`、`World.Spawn` 与 `World.Destroy` | [实体](entities.md) |
| 全局变量 | 在根组件中创建一次、再传给需要它的场景的普通 Go 值 | [从 Godot 迁移](from-godot.md#场景) |
| 键盘、手柄与触摸检测 | 在命名 action 上调用 `input.Actions` 的 `Pressed` 与 `JustPressed` | [input](input.md) |
| 精灵、子图像 | `sprite.LoadAtlas` 的帧与动画 | [sprite](sprite.md) |
| 碰撞遮罩 | 内置的 `collider2d` 组件，或 Go 中的 `physics` 形状 | [physics](physics.md#在场景文件中编写物理) |
| 碰撞事件 | 内置 `trigger2d` 组件的 `enter`、`stay` 与 `exit` 事件，由场景的 `connections` 连接 | [编辑器与场景格式](editor.md#连接490) |
| Alarm 事件 | Go 中的 `flow.Timers`，或场景中内置的 `timer` 组件 | [flow](flow.md)、[编辑器与场景格式](editor.md#内置计时器组件1362) |
| 图块集、自动图块 | 通过 `tilemap.LoadTiled` 读取 Tiled 地图，用 `SetTerrain` 绘制地形 | [tilemap](tilemap.md) |
| 按状态切换精灵动画 | `*.states.yaml` 精灵状态机 | [animclip](animclip.md#精灵状态机) |
| 序列（Sequences） | 通过 `tween.Timeline` 播放的 `animclip` 片段；视频过场见 [cutscene](cutscene.md) | [animclip](animclip.md) |
| 粒子系统 | `particles.Emitter` | [particles](particles.md) |
| 音频组、`audio_play_sound` | [sound](sound.md)，总线在 `game.yaml` 中声明 | [sound](sound.md)、[项目文件](project.md) |
| `ini_open`、用缓冲区写存档 | 带迁移的版本化 JSON `save` 存档槽 | [save](save.md) |
| 游戏选项、目标平台 | `game.yaml` 与 `nsgo.yaml` | [项目文件](project.md) |
| 创建可执行文件 | `go tool game release` | [构建与发布](shipping.md) |

## 第一个项目

从与你的游戏最接近的模板开始：`go run github.com/nsgo-dev/game/cmd/game@latest new -template platformer2d mygame` 会写出一个带房间、角色控制器和检查点的平台跳跃游戏（[指南](template-platformer2d.md)），`new -list` 会列出其他模板（[项目模板](templates.md)）。用 `go tool game editor` 打开项目：它的 2D 视口就是房间编辑器，检查器编辑对象的组件和属性，它保存的文件就是游戏加载的那份 YAML。
