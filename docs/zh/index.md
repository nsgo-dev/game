# 概览

game 是用 Go 编写的 [NSGo](https://nsgo.dev/zh/) 应用游戏层。游戏绘制在一个 NSGo 视图中，可以是 `Canvas`、像素缓冲区或着色器，HUD 和菜单仍然是叠在上面的普通原生 NSGo 视图。它只使用 NSGo 的公开 API（`core`、`audio`，之后还有 `gamepad`），所以没有导入它的应用不会链接其中任何代码。

> 项目还处在早期：下面的包已经可用并有测试，但 API 还会变化。从[快速开始](getting-started.md)入手创建并运行一个游戏。

## 阶段

1. **2D**：固定步长循环、输入动作、精灵、瓦片地图、场景和声音，以 60 fps 或显示器的刷新率绘制。
2. **3D**：在 NSGo 为此新增的 GPU 视图上，带深度地绘制网格、相机、光照和 glTF 模型（[nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)）。坐标系约定、Blender 4.x 导出清单、FBX/OBJ 转换与示例 glTF 资产见[从 Blender 导入 3D 模型](3d-models-blender.md)。

循环、输入动作、声音和场景切换由两个阶段共用，所以 3D 只是增加绘制，不会替换它们。开发计划是 [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047)，其中的任务就是本仓库的 issue，以及 NSGo 为它新增的引擎基础能力。

## 包

| 包 | 导入路径 | 指南 | 内容 |
| --- | --- | --- | --- |
| `game` | `github.com/nsgo-dev/game/packages/game` | [Loop](loop.md) | `Loop`，所有包共用的固定步长循环 |
| `sprite` | `github.com/nsgo-dev/game/packages/game/sprite` | [sprite](sprite.md) | 图集、帧、动画、精灵，以及绘制到 `Canvas` 中的批次 |
| `sound` | `github.com/nsgo-dev/game/packages/game/sound` | [sound](sound.md) | `Mixer` 及其音效和音乐 |
| `input` | `github.com/nsgo-dev/game/packages/game/input` | [input](input.md) | 基于触摸区域和鼠标按键的命名动作与轴，支持 `core.Storage` 持久化 |
| `tilemap` | `github.com/nsgo-dev/game/packages/game/tilemap` | [tilemap](tilemap.md) | 2D 相机、视锥裁剪瓦片地图、Tiled `.tmj` 加载、动画瓦片与射线查询 |
| `scene` | `github.com/nsgo-dev/game/packages/game/scene` | [scene](scene.md) | 世界/屏幕图层、命中测试、场景栈与过渡动画 |
| `physics` | `github.com/nsgo-dev/game/packages/game/physics` | [physics](physics.md) | `Box`/`Circle` 重叠检测、`MoveAndSlide` 瓦片滑动解析与 `World` 空间查询 |
| `screen` | `github.com/nsgo-dev/game/packages/game/screen` | [screen](screen.md) | 虚拟分辨率 `Viewport` 缩放、像素对齐与移动端安全区 |
| `assets` | `github.com/nsgo-dev/game/packages/game/assets` | [assets](assets.md) | 按场景异步加载的资源组、响应式进度与 `LoadingScreen` |
| `text` | `github.com/nsgo-dev/game/packages/game/text` | [text](text.md) | AngelCode BMFont 位图字体与 `DrawPlatform` 平台矢量文本 |
| `particles` | `github.com/nsgo-dev/game/packages/game/particles` | [particles](particles.md) | 确定性 2D 粒子发射器 `Emitter`、发射形状、爆发与生命周期曲线 |
| `save` | `github.com/nsgo-dev/game/packages/game/save` | [save](save.md) | 带版本号的 JSON 存档槽、结构迁移与原子备份恢复 |
| `debug` | `github.com/nsgo-dev/game/packages/game/debug` | [debug](debug.md) | 帧耗时图、碰撞线框、场景检查器及 `-tags release` 零开销裁剪 |
| `flow` | `github.com/nsgo-dev/game/packages/game/flow` | [flow](flow.md) | 游戏时钟 `Timers`、`Machine[S]`、`Seq`/`Parallel`/`Race` 与类型化事件 `Bus` |
| `tween` | `github.com/nsgo-dev/game/packages/game/tween` | [tween](tween.md) | 固定步长 `Tween`、`Timeline` 与相机运镜补间 |
| `ai` | `github.com/nsgo-dev/game/packages/game/ai` | [ai](ai.md) | 网格 `AStar`、2D `NavMesh`、转向行为与行为树 |
| `replay` | `github.com/nsgo-dev/game/packages/game/replay` | [replay](replay.md) | 增量输入录制/回放（`Recorder`/`Player`）与确定性测试断言 |

核心包 API 的单页速查见[包](packages.md)。

## 安装

这个模块需要 Go 1.27 和 NSGo v0.2 或更高版本：

```sh
go get github.com/nsgo-dev/game/packages/game
```

## 一帧如何运行

游戏就是一个 NSGo 组件。它在自己的作用域上启动一个 `game.Loop`，循环运行在 NSGo 的 `RequestAnimationFrame` 上：

1. 每个显示帧累加距上一帧的时间，其中包含几个完整的固定步长，就运行几次 `Update`，可能是零次，也可能是多次。
2. `Update` 按固定的 `dt` 推进游戏状态，让动画和计时器向前走。
3. `Draw` 运行一次，参数是这一帧在下一步中走到了多远。它设置游戏的 `Canvas` 读取的 signal，于是画布重新绘制，不需要提交视图树。

窗口在后台时，循环不运行任何更新，混音器也不播放任何声音；回到前台后两者都从原处继续。

## 目标平台

macOS、iOS、Windows、Linux、Android 和 Web。鸿蒙、小程序、watchOS 和 tvOS 不在第一阶段的范围内。

关于六个目标平台的图标、签名、公证、商店打包与 `nsgo check` 验证，见[构建与发布](shipping.md)。
