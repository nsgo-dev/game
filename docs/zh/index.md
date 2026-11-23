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
| `input` | `github.com/nsgo-dev/game/packages/game/input` | [input](input.md) | 基于触摸区域、鼠标按键、键盘按键、指针锁定鼠标视角和游戏手柄的命名动作与轴，支持 `core.Storage` 持久化 |
| `tilemap` | `github.com/nsgo-dev/game/packages/game/tilemap` | [tilemap](tilemap.md) | 2D 相机、视锥裁剪瓦片地图、Tiled `.tmj` 加载、动画瓦片与射线查询 |
| `scene` | `github.com/nsgo-dev/game/packages/game/scene` | [scene](scene.md) | 世界/屏幕图层、命中测试、场景栈与过渡动画 |
| `physics` | `github.com/nsgo-dev/game/packages/game/physics` | [physics](physics.md) | `Box`/`Circle` 重叠检测、`MoveAndSlide` 瓦片滑动解析与 `World` 空间查询 |
| `screen` | `github.com/nsgo-dev/game/packages/game/screen` | [screen](screen.md) | 虚拟分辨率 `Viewport` 缩放、像素对齐、移动端安全区与视频设置 |
| `assets` | `github.com/nsgo-dev/game/packages/game/assets` | [assets](assets.md) | 按场景异步加载的资源组、响应式进度与 `LoadingScreen` |
| `resource` | `github.com/nsgo-dev/game/packages/game/resource` | [resource](resources.md) | 共享 YAML 资源文件（材质、曲线、渐变、动画片段、UI 主题、总线布局、物理材质），以及共享并重载它们的 `Library` |
| `text` | `github.com/nsgo-dev/game/packages/game/text` | [text](text.md) | AngelCode BMFont 位图字体与 `DrawPlatform` 平台矢量文本 |
| `particles` | `github.com/nsgo-dev/game/packages/game/particles` | [particles](particles.md) | 确定性 2D 粒子发射器 `Emitter`、发射形状、爆发与生命周期曲线 |
| `save` | `github.com/nsgo-dev/game/packages/game/save` | [save](save.md) | 带版本号的 JSON 存档槽、结构迁移与原子备份恢复 |
| `achievements` | `github.com/nsgo-dev/game/packages/game/achievements` | [achievements](achievements.md) | 在带版本号的 YAML 中声明成就、只增不减并保存在 `core.Storage` 的进度，以及上报到 Game Center 和 Play Games |
| `datatable` | `github.com/nsgo-dev/game/packages/game/tooling/datatable` | [datatable](datatable.md) | 带版本号的 YAML（及 CSV/JSON）类型化数据表，单元格级错误、确定性保存与热重载 |
| `debug` | `github.com/nsgo-dev/game/packages/game/debug` | [debug](debug.md) | 帧耗时图、碰撞线框、场景检查器及 `-tags release` 零开销裁剪 |
| `flow` | `github.com/nsgo-dev/game/packages/game/flow` | [flow](flow.md) | 游戏时钟 `Timers`、`Machine[S]`、`Seq`/`Parallel`/`Race` 与类型化事件 `Bus` |
| `stats` | `github.com/nsgo-dev/game/packages/game/stats` | [stats](stats.md) | 基础属性与来自各来源的 add/mul/override 修饰符，改动即重算，按固定步长计时的修饰符与 YAML 属性文件 |
| `tween` | `github.com/nsgo-dev/game/packages/game/tween` | [tween](tween.md) | 固定步长 `Tween`、`Timeline` 与相机运镜补间 |
| `ai` | `github.com/nsgo-dev/game/packages/game/ai` | [ai](ai.md) | 网格 `AStar`、2D `NavMesh`、转向行为与行为树 |
| `replay` | `github.com/nsgo-dev/game/packages/game/replay` | [replay](replay.md) | 输入录制/回放（`Recorder`/`Player`）、可编辑的 YAML 录像与确定性测试断言 |
| `statedump` | `github.com/nsgo-dev/game/packages/game/statedump` | [statedump](statedump.md) | 无窗口运行并转储为确定性 YAML（实体、组件、场景栈、全局值），以及两次运行之间的行 `Diff` |
| `cutscene` | `github.com/nsgo-dev/game/packages/game/cutscene` | [cutscene](cutscene.md) | 覆盖在游戏视图上、可跳过的视频过场动画，可放在场景中或场景之间，以 YAML 文件描述 |
| `rhythm` | `github.com/nsgo-dev/game/packages/game/rhythm` | [rhythm](rhythm.md) | 支持速度与拍号变化的 YAML 节拍图、基于音频时钟的音乐时钟，以及按设备的音频/画面延迟校准 |
| `undo` | `github.com/nsgo-dev/game/packages/game/undo` | [undo](undo.md) | 可撤销操作，支持无限撤销、重做与 `Seek`，可保存为 YAML `Log` |
| `math2d` | `github.com/nsgo-dev/game/packages/game/math2d` | [math2d](math2d.md) | 共享 2D `Vec2`、`Rect`、`Circle` 与 `Affine` 值类型及无损 `core` 互转 |
| `gameui` | `github.com/nsgo-dev/game/packages/game/gameui` | [gameui](gameui.md) | 画布内盒式布局菜单、虚拟分辨率缩放、空间焦点导航与世界坐标锚定原生视图 |
| `lights2d` | `github.com/nsgo-dev/game/packages/game/lights2d` | [lights2d](lights2d.md) | 2D 环境暗场、点光源与聚光灯、可见性多边形阴影及法线贴图精灵 |
| `effects` | `github.com/nsgo-dev/game/packages/game/effects` | [effects](effects.md) | 在一次绘制中串联作用于游戏画面的 Kage 着色器通道、随帧时钟变化的 uniform 及 CPU 参考图 |
| `a11y` | `github.com/nsgo-dev/game/packages/game/a11y` | [a11y](a11y.md) | 实时字幕、色盲调色板与 CVD 模拟、持久化辅助选项及屏幕阅读器菜单 |
| `l10n` | `github.com/nsgo-dev/game/packages/game/l10n` | [l10n](l10n.md) | 支持 CLDR 复数的 JSON 字符串表、脚本 `FontSet`、RTL 视觉排序与多语言资源目录 |
| `perf` | `github.com/nsgo-dev/game/packages/game/perf` | [perf](perf.md) | 零分配 `Pool`/`Arena`/`SlicePool`、`SpatialHash`、`Quadtree` 与 `CullBatch` |
| `net` | `github.com/nsgo-dev/game/packages/game/net` | [net](net.md) | 跨平台 WebSocket/UDP/回环传输、服务端权威增量复制与锁步/回滚同步 |
| `server` | `github.com/nsgo-dev/game/packages/game/server` | [server](server.md) | 专用服务器无头固定步长 `Runner`：多场比赛独立速率、超时报告与有界信号停止 |
| `accounts` | `github.com/nsgo-dev/game/packages/game/accounts` | [accounts](accounts.md) | 可自行部署的玩家账号：游客升级为邮箱或绑定平台/OAuth 身份、argon2id 密码、SQLite/Postgres 存储，以及游戏服务器离线校验的 Ed25519 令牌 |
| `leaderboard` | `github.com/nsgo-dev/game/packages/game/leaderboard` | [leaderboard](leaderboard.md) | 在 YAML 里一次性声明排行榜，通过 Game Center 或 Play 游戏服务提交分数并显示原生排行榜 |
| `modding` | `github.com/nsgo-dev/game/packages/game/modding` | [modding](modding.md) | 能力沙箱隔离的 TSX `"game"` 模组 API 与 `mod.yaml` 依赖/生命周期管理器 |
| `math3d` | `github.com/nsgo-dev/game/packages/game/math3d` | [math3d](math3d.md) | 3D 向量、矩阵、四元数、变换、包围体、射线、视锥体与统一数学约定 |
| `scene3d` | `github.com/nsgo-dev/game/packages/game/scene3d` | [scene3d](scene3d.md) | 3D 节点树、插槽/附件、动态胖 AABB/SAH BVH、轨道/FPS/第三人称相机与 3D 拾取 |
| `anim3d` | `github.com/nsgo-dev/game/packages/game/anim3d` | [anim3d](anim3d.md) | 3D 姿态状态机、1D/2D 混合空间、骨骼遮罩与叠加层、帧事件、IK 与根骨骼运动 |
| `world3d` | `github.com/nsgo-dev/game/packages/game/world3d` | [world3d](world3d.md) | 高度图地形、多层 3D 导航网格、3D `GridMap`、程序化植被与浮点原点区块流式加载 |
| `render3d` | `github.com/nsgo-dev/game/packages/game/render3d` | [render3d](render3d.md) | GPU 网格缓冲、CPU 视锥裁剪、绘制排序、零分配实例化、LOD 滞回、调试线框/手柄、公告板、3D 条带拖尾与 GPU 预算 |

核心包 API 的单页速查见[包](packages.md)。

## 安装

这个模块需要 Go 1.27 和 NSGo `v0.3.0` 或更高版本：

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

游戏主机（Nintendo Switch、PlayStation 和 Xbox）不在支持范围内：它们的 SDK 受保密协议（NDA）约束，且需要 C 工具链，而 NSGo 和本仓库只用 Go，不包含手写的 C 代码。

关于六个目标平台的图标、签名、公证、商店打包与 `nsgo check` 验证，见[构建与发布](shipping.md)；打包图集、转换音频、生成资源清单、编码纹理和打开场景编辑器的 `go tool game` 见[命令行](cli.md)。

### VR 与 AR

游戏层目前不支持 VR 和 AR（[#495](https://github.com/nsgo-dev/game/issues/495)）。所有包都绘制到一个平面的 NSGo 视图中，即 `Canvas` 或 [nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059) 的 GPU 视图，并读取触摸、鼠标、键盘、手柄和设备运动传感器的输入。NSGo v0.2 在任何目标平台上都没有 XR 运行时，而本仓库只使用 NSGo 的公开 API、不包含平台代码，因此无法自行添加。

等 NSGo 在至少一个运行时（桌面与 Android 头显上的 OpenXR、visionOS、ARKit、ARCore 或 WebXR）上提供以下能力后，这里才能开始 XR 的工作：

- **XR 会话**：启动会话，在头显被摘下或应用失去焦点时暂停，以及结束会话。
- **位姿**：每帧给出头部和每个控制器的位姿，并按该帧显示的时刻做预测，同时提供运行时的控制器与手部输入。
- **立体渲染**：每只眼睛一个渲染目标，各有自己的视图与投影矩阵，由 GPU 视图以头显的刷新率绘制并提交给运行时的合成器。
- **AR 所需**：绘制内容背后的相机透视画面、检测到的平面、对真实世界的命中测试，以及保持在原位的锚点。

首先应面向的运行时是 OpenXR，因为同一套 API 覆盖 Windows、Linux 和 Android 上的头显，即 NSGo 的三个目标平台；visionOS、ARKit、ARCore 和 WebXR 可以随后跟进。会话、位姿和立体渲染排在最前，有了它们就足以支持 VR；AR 所需的各项排在其后。

具备这些之后，本仓库会在 `scene3d` 中加入 XR 相机组件，在 `input` 中加入 XR 动作，并复用 `math3d` 的变换来表示位姿。在此之前，3D 游戏只在平面屏幕上运行。
