# 概览

game 是用 Go 编写的 [NSGo](https://nsgo.dev/zh/) 应用游戏层。游戏绘制在一个 NSGo 视图中，可以是 `Canvas`、像素缓冲区或着色器，HUD 和菜单仍然是叠在上面的普通原生 NSGo 视图。它只使用 NSGo 的公开 API（`core`、`audio`，之后还有 `gamepad`），所以没有导入它的应用不会链接其中任何代码。

> 项目还处在早期：下面的包已经可用并有测试，但 API 还会变化。入门模板和入门指南会随 [#38](https://github.com/nsgo-dev/game/issues/38) 一起提供。

## 阶段

1. **2D**：固定步长循环、输入动作、精灵、瓦片地图、场景和声音，以 60 fps 或显示器的刷新率绘制。
2. **3D**：在 NSGo 为此新增的 GPU 视图上，带深度地绘制网格、相机、光照和 glTF 模型（[nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)）。

循环、输入动作、声音和场景切换由两个阶段共用，所以 3D 只是增加绘制，不会替换它们。开发计划是 [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047)，其中的任务就是本仓库的 issue，以及 NSGo 为它新增的引擎基础能力。

## 包

| 包 | 导入路径 | 内容 |
| --- | --- | --- |
| `game` | `github.com/nsgo-dev/game/packages/game` | `Loop`，所有包共用的固定步长循环 |
| `sprite` | `github.com/nsgo-dev/game/packages/game/sprite` | 图集、帧、动画、精灵，以及绘制到 `Canvas` 中的批次 |
| `sound` | `github.com/nsgo-dev/game/packages/game/sound` | `Mixer` 及其音效和音乐 |

计划中的包：用于输入动作的 `input`（[#3](https://github.com/nsgo-dev/game/issues/3)），用于瓦片地图和相机的 `tilemap`（[#5](https://github.com/nsgo-dev/game/issues/5)），用于绘制图层、命中测试和场景切换的 `scene`（[#6](https://github.com/nsgo-dev/game/issues/6)），以及一个可以玩的示例游戏（[#8](https://github.com/nsgo-dev/game/issues/8)）。

每个包的说明见[包](packages.md)。

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
