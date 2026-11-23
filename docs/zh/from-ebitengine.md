# 从 Ebitengine 迁移

把 Ebitengine 的游戏循环、图像、输入、音频和构建对应到这个游戏层，以及它在此之上提供的场景、节点、资源、信号、输入映射、物理和导出，并给出讲解每一项的页面。

两者都是 Go，所以大部分游戏逻辑可以原样搬过来。变化的是宿主：Ebitengine 游戏独占整个窗口，所有内容都在 GPU 上绘制；这里的游戏是一个 NSGo 组件，绘制到一个 `core.Canvas` 中，HUD 和菜单是叠在上面的原生 NSGo 视图。Ebitengine 把场景、实体、事件和物理留给你自己写；这个游戏层为每一项都提供了包，可以一个一个地采用。

## 速览

| Ebitengine | 这里 | 指南 |
| --- | --- | --- |
| 以 `ebiten.TPS()` 调用的 `ebiten.Game.Update` | 以 `Loop.Rate` 调用的 `game.Loop.Update(dt)` | [Loop](loop.md) |
| `ebiten.Game.Draw(screen)` | `core.Canvas.Draw(d, size)`，由 `Loop.Draw(alpha)` 触发重绘 | [Loop](loop.md) |
| `Layout(outsideWidth, outsideHeight)` | 使用 `ModeFit`、`ModeFill`、`ModeStretch` 或 `ModeExpand` 的 `screen.Viewport` | [screen](screen.md) |
| `*ebiten.Image`、`SubImage` | `sprite.Atlas` 及其 `Frame` | [sprite](sprite.md) |
| `DrawImageOptions.GeoM`、`ColorScale` 透明度 | `sprite.Sprite` 的位置、原点、旋转、缩放、翻转和 `Alpha` | [sprite](sprite.md) |
| 自己写的场景切换器 | 带过渡效果的 `scene.Manager` | [scene](scene.md) |
| 自己逐个更新的结构体切片 | `entities.World`、组件与系统 | [实体](entities.md) |
| `//go:embed` 加 `ebitenutil.NewImageFromFile` | `resources/`、`"asset:"` URI 和 `assets.Manager` | [assets](assets.md) |
| 系统之间的 channel 或回调 | `flow.Bus`；界面状态用 `core.Signal` | [flow](flow.md) |
| `inpututil.IsKeyJustPressed`、触控 ID | 带 `JustPressed` 和触控区域的 `input.Actions` | [input](input.md) |
| 自己写的 AABB 检测 | `physics` 形状、`MoveAndSlide`、`RigidWorld` | [physics](physics.md) |
| `audio.Context`、`audio.Player` | `sound.Mixer`、`Effect` 和 `Music` | [sound](sound.md) |
| `go build`、`GOOS=js GOARCH=wasm`、`ebitenmobile bind` | 所有目标平台都用 `go tool nsgo build` | [构建与发布](shipping.md) |

## 循环

Ebitengine 默认每秒调用 60 次 `Update`，每个显示帧调用一次 `Draw`。`game.Loop` 保持同样的分工：`Update` 得到固定的 `dt`，即 `1/Rate` 秒（默认每秒 60 次更新），`Draw` 每个显示帧运行一次，并带上 `alpha`，表示这一帧落在下一次更新之前的多远处。通过 `core.Signal` 把 `alpha` 传给画布，在最近两个状态之间插值，这样在 120 Hz 显示器上运动依然平滑，而更新频率不变。`Loop` 还会在应用进入后台时暂停，并在卡顿后限制单帧补跑的更新次数。见 [Loop](loop.md)。

`ebiten.ActualTPS` 和 `ActualFPS` 没有直接对应物；[debug](debug.md) 叠加层会绘制帧时间曲线，`nsgo dev -fps` 会显示 NSGo 的性能监视器。

## 绘制

Ebitengine 的 `Draw` 收到的是屏幕图像；画布的 `Draw` 收到的是 `*core.Drawing` 和以点为单位的视图尺寸。精灵表变成从 TexturePacker 或 Aseprite 导出文件加载、或手写的 `sprite.Atlas`，`SubImage` 矩形变成其中命名的帧和动画。原来设置 `GeoM` 平移、旋转、缩放的地方，改为设置 `sprite.Sprite` 的字段；`sprite.Batch` 按 `Z` 顺序绘制同一图集中的大量精灵。见 [sprite](sprite.md)。

`Layout` 变成 `screen.Viewport`：给它虚拟分辨率，选择它如何适配窗口，然后在 `vp.Draw` 内绘制。像素风的整数缩放和安全区矩形也在同一个包里。见 [screen](screen.md)。地图方面，`tilemap` 加载 Tiled `.tmj` 文件，并通过 2D 相机只绘制可见的瓦片。见 [tilemap](tilemap.md)。

## 场景

Ebitengine 没有场景类型，游戏通常保存一个 `current Scene` 字段并替换它。`scene.Manager` 用一个栈来做这件事：`Switch` 用淡入淡出、交叉淡化或擦除效果替换场景，`Push` 在上面叠一个暂停菜单之类的覆盖层，`Pop` 返回。每个场景都有自己的 `*core.Scope`，在它上面创建的东西会在场景退出时全部释放。其中的 `scene.Scene` 把图层按世界空间（在相机下）和屏幕空间（用于 HUD）排序，并对精灵做点击命中测试。见 [scene](scene.md)。

关卡布局可以写在 YAML 场景文件里而不是 Go 代码里：`sceneformat.Parse` 读取它，`sceneformat.Build` 生成可以直接绘制的图层。见[编辑器与场景格式](editor.md)。

## 节点

Ebitengine 游戏常用 `[]*Enemy` 加循环来管理对象；`entities` 则把组件存放在 `World` 上的类型化稀疏集合中，并在每个固定步按声明的顺序运行系统，稳定运行时不产生内存分配。`Prefab` 把模板的组件盖到新实体上，整个世界可以通过 [save](save.md) 存成 JSON。见[实体](entities.md)。

3D 方面，`scene3d.Node` 是带插槽、标签和拾取功能的变换层级。见 [scene3d](scene3d.md)。

## 资源

运行时文件放在 `resources/` 下；NSGo 会把它们打包到每个目标平台，`"asset:hero.png"` 在任何平台都能打开，包括 Web，而在 Ebitengine 中 Web 上的文件需要嵌入或自行获取。你仍然可以用 `//go:embed` 嵌入 JSON。`assets.Manager` 在引擎 goroutine 之外按命名资源组加载图集、声音、图像和你自己解析的文件，提供加载界面所需的进度，并在场景结束时释放资源组。见 [assets](assets.md)。

## 信号

原来彼此直接调用或共享 channel 的系统，可以改为在 `flow.Bus` 上发布类型化事件：`flow.Subscribe` 接受一个以事件类型为参数的函数，`flow.Publish` 立即投递，`flow.Enqueue` 配合 `Bus.Flush` 每步投递一次。`flow.Timers`、`flow.Machine` 和 `flow.Seq` 取代帧计数器、状态 switch 语句和过场动画 goroutine，它们都只随循环的 `dt` 推进，所以会随游戏一起暂停和重放。见 [flow](flow.md)。

HUD 显示的状态，例如分数，是一个 [`core.Signal`](https://pkg.go.dev/github.com/nsgo-dev/nsgo/core#Signal)，读取它的 NSGo 视图会在它变化时重新渲染。

## 输入映射

Ebitengine 直接查询设备（`ebiten.IsKeyPressed`、`inpututil.IsMouseButtonJustPressed`、`ebiten.AppendTouchIDs`）。`input.Actions` 则给动作命名，再把输入绑定到动作上，游戏代码查询的是 `"jump"`，而不是某个键：

| Ebitengine | 这里 |
| --- | --- |
| `ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)` | `actions.Bind("fire", input.MouseButton(core.MouseButtonLeft))`，然后 `actions.Pressed("fire")` |
| `inpututil.IsMouseButtonJustPressed` | `actions.JustPressed` |
| `inpututil.IsMouseButtonJustReleased` | `actions.JustReleased` |
| 检查触控位置是否落在按钮矩形内 | `input.TouchRegion(rect)`，或 `VirtualPad` |
| 把两个相反的输入合成一个方向 | `actions.BindAxis("move_x", "left", "right")` 和 `actions.Axis` |

在每次 `Update` 开始时调用 `actions.Step()`；在两次更新之间开始并结束的轻触仍会被记录一步。`actions.Save` 和 `Load` 把重新映射的绑定保存在 `core.Storage` 中。键盘和手柄绑定在 [#3](https://github.com/nsgo-dev/game/issues/3) 中跟踪；在那之前，可以用 NSGo 的 `cx.OnKey` 绑定按键，在回调里调用 `actions.Trigger(name)`，让该动作在一个步内处于按下状态。见 [input](input.md)。

## 物理

Ebitengine 没有物理，所以这是新增能力而不是移植。[physics](physics.md) 提供盒形和圆形的重叠测试、针对带单向平台的瓦片网格的平台跳跃 `MoveAndSlide`、触发区域和射线检测。[2D 刚体](physics2d-rigid.md)增加了一个确定性的刚体求解器，带距离、弹簧和旋转关节；[physics3d](physics3d.md) 覆盖 3D 物体和胶囊体角色控制器。它们都按循环的固定 `dt` 推进，并使用与绘制相同的屏幕空间。

## 导出

| Ebitengine | 这里 |
| --- | --- |
| `go run .` | `go tool nsgo dev`，保存时还会自动重载 |
| `GOOS=js GOARCH=wasm go build` 加一个 `wasm_exec.js` 页面 | `go tool nsgo build -target web` |
| `ebitenmobile bind` 加一个 Xcode 或 Android Studio 工程 | `go tool nsgo build -target ios` 或 `android` |
| 自己打包 `.app` 或安装程序 | `go tool nsgo build`，会生成 `.dmg`、NSIS 安装程序、`.deb`、`.AppImage`、`.apk` 和 `.ipa` |

应用名称、标识符、版本、图标和目标平台写在 `nsgo.yaml` 中，`-channel store` 构建应用商店包。见[构建与发布](shipping.md)；第一个项目从[快速开始](getting-started.md)入手。
