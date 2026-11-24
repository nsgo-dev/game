# 概览

game 是用 Go 编写的 [NSGo](https://nsgo.dev/zh/) 应用游戏层。游戏绘制在一个 NSGo 视图中，可以是 `Canvas`、像素缓冲区或着色器，HUD 和菜单仍然是叠在上面的普通原生 NSGo 视图。它只使用 NSGo 的公开 API（`core`、`audio`，之后还有 `gamepad`），所以没有导入它的应用不会链接其中任何代码。

> 项目还处在早期：下面的包已经可用并有测试，[API 稳定性](stability.md)说明了哪些包的 API 是稳定的、哪些还会变化。从[快速开始](getting-started.md)入手创建并运行一个游戏，再跟着[教程](tutorial.md)做出一款完整的游戏。

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
| `platformer` | `github.com/nsgo-dev/game/packages/game/platformer` | [platformer](platformer.md) | 基于 `CharacterBody` 的平台跳跃 `Controller`：土狼时间、跳跃缓冲、可变跳跃高度、贴墙下滑与蹬墙跳，参数来自 YAML 配置 |
| `screen` | `github.com/nsgo-dev/game/packages/game/screen` | [screen](screen.md) | 虚拟分辨率 `Viewport` 缩放、像素对齐、移动端安全区与视频设置 |
| `project` | `github.com/nsgo-dev/game/packages/game/project` | [项目文件](project.md) | 唯一的项目文件 `game.yaml`：窗口、虚拟分辨率、输入映射、音频总线、自动加载和导出预设，并与 `nsgo.yaml` 保持一致 |
| `refs` | `github.com/nsgo-dev/game/packages/game/refs` | [稳定引用](project.md#稳定引用) | 稳定文件 ID、项目索引，以及改写所有引用的文件移动，供 `game mv`、`game index` 和 `game check` 使用 |
| `assets` | `github.com/nsgo-dev/game/packages/game/assets` | [assets](assets.md) | 按场景异步加载的资源组、响应式进度与 `LoadingScreen` |
| `resource` | `github.com/nsgo-dev/game/packages/game/resource` | [resource](resources.md) | 共享 YAML 资源文件（材质、曲线、渐变、动画片段、UI 主题、总线布局、物理材质），以及共享并重载它们的 `Library` |
| `text` | `github.com/nsgo-dev/game/packages/game/text` | [text](text.md) | AngelCode BMFont 位图字体、2D 与 3D 世界空间中的有向距离场字体，以及 `DrawPlatform` 平台矢量文本 |
| `placeholder` | `github.com/nsgo-dev/game/packages/game/placeholder` | [占位素材](placeholder.md) | 用于原型制作的 CC0 素材包：精灵表、图块集与关卡、像素字体、声音和 glTF 模型，附带导入设置旁车文件 |
| `particles` | `github.com/nsgo-dev/game/packages/game/particles` | [particles](particles.md) | 确定性 2D 粒子发射器 `Emitter`、发射形状、爆发与生命周期曲线 |
| `bullets` | `github.com/nsgo-dev/game/packages/game/bullets` | [bullets](bullets.md) | 射击游戏弹幕（环形、螺旋、瞄准连发），由 YAML 定义，对象池复用并通过精灵批次绘制 |
| `save` | `github.com/nsgo-dev/game/packages/game/save` | [save](save.md) | 带版本号的 JSON 存档槽、结构迁移与原子备份恢复 |
| `achievements` | `github.com/nsgo-dev/game/packages/game/achievements` | [achievements](achievements.md) | 在带版本号的 YAML 中声明成就、只增不减并保存在 `core.Storage` 的进度，以及上报到 Game Center 和 Play Games |
| `datatable` | `github.com/nsgo-dev/game/packages/game/tooling/datatable` | [datatable](datatable.md) | 带版本号的 YAML（及 CSV/JSON）类型化数据表，单元格级错误、确定性保存与热重载 |
| `debug` | `github.com/nsgo-dev/game/packages/game/debug` | [debug](debug.md) | 帧耗时图、碰撞线框、场景检查器及 `-tags release` 零开销裁剪 |
| `mcp` | `github.com/nsgo-dev/game/packages/game/mcp` | [mcp](mcp.md) | 通过 `nsgo/automation` 注册的 MCP 工具，用于检查运行中的场景、图层、实体与组件 |
| `flow` | `github.com/nsgo-dev/game/packages/game/flow` | [flow](flow.md) | 游戏时钟 `Timers`、`Machine[S]`、`Seq`/`Parallel`/`Race` 与类型化事件 `Bus` |
| `stats` | `github.com/nsgo-dev/game/packages/game/stats` | [stats](stats.md) | 基础属性与来自各来源的 add/mul/override 修饰符，改动即重算，按固定步长计时的修饰符与 YAML 属性文件 |
| `tween` | `github.com/nsgo-dev/game/packages/game/tween` | [tween](tween.md) | 固定步长 `Tween`、`Timeline` 与相机运镜补间 |
| `animclip` | `github.com/nsgo-dev/game/packages/game/animclip` | [animclip](animclip.md) | 为任何已注册属性打关键帧的动画片段，通过 `tween.Timeline` 在场景对象上播放 |
| `cards` | `github.com/nsgo-dev/game/packages/game/cards` | [cards](cards.md) | 在牌桌槽位间拖放并吸附或弹回，扇形手牌、牌堆、叠放牌列与网格布局，以及 YAML 牌桌文件 |
| `rooms` | `github.com/nsgo-dev/game/packages/game/rooms` | [rooms](rooms.md) | 房间地图：每个房间独立的相机边界，以及房间之间的切换、滑动或淡入淡出过渡 |
| `ai` | `github.com/nsgo-dev/game/packages/game/ai` | [ai](ai.md) | 网格 `AStar`、2D `NavMesh`、转向行为与行为树 |
| `rts` | `github.com/nsgo-dev/game/packages/game/rts` | [rts](rts.md) | 流场、带分离的大规模单位群 `Crowd`、YAML 阵型、框选/点选与编队 |
| `procgen` | `github.com/nsgo-dev/game/packages/game/procgen` | [procgen](procgen.md) | 带种子的 BSP 房间、元胞自动机洞穴与基于 YAML 规则的波函数坍缩，附连通性检查与 tilemap 绘制 |
| `replay` | `github.com/nsgo-dev/game/packages/game/replay` | [replay](replay.md) | 输入录制/回放（`Recorder`/`Player`）、可编辑的 YAML 录像与确定性测试断言 |
| `statedump` | `github.com/nsgo-dev/game/packages/game/statedump` | [statedump](statedump.md) | 无窗口运行并转储为确定性 YAML（实体、组件、场景栈、全局值），以及两次运行之间的行 `Diff` |
| `cutscene` | `github.com/nsgo-dev/game/packages/game/cutscene` | [cutscene](cutscene.md) | 覆盖在游戏视图上、可跳过的视频过场动画，可放在场景中或场景之间，以 YAML 文件描述 |
| `rhythm` | `github.com/nsgo-dev/game/packages/game/rhythm` | [rhythm](rhythm.md) | 支持速度与拍号变化的 YAML 节拍图、基于音频时钟的音乐时钟，以及按设备的音频/画面延迟校准 |
| `undo` | `github.com/nsgo-dev/game/packages/game/undo` | [undo](undo.md) | 可撤销操作，支持无限撤销、重做与 `Seek`，可保存为 YAML `Log` |
| `fighting` | `github.com/nsgo-dev/game/packages/game/fighting` | [fighting](fighting.md) | 逐步输入缓冲、小键盘指令输入、蓄力、双击、同时按键与连招，由 YAML 招式表定义 |
| `math2d` | `github.com/nsgo-dev/game/packages/game/math2d` | [math2d](math2d.md) | 共享 2D `Vec2`、`Rect`、`Circle` 与 `Affine` 值类型及无损 `core` 互转 |
| `gameui` | `github.com/nsgo-dev/game/packages/game/gameui` | [gameui](gameui.md) | 画布内盒式布局菜单、虚拟分辨率缩放、空间焦点导航与世界坐标锚定原生视图 |
| `lights2d` | `github.com/nsgo-dev/game/packages/game/lights2d` | [lights2d](lights2d.md) | 2D 环境暗场、点光源与聚光灯、可见性多边形阴影及法线贴图精灵 |
| `effects` | `github.com/nsgo-dev/game/packages/game/effects` | [effects](effects.md) | 在一次绘制中串联作用于游戏画面的 Kage 着色器通道、随帧时钟变化的 uniform 及 CPU 参考图 |
| `worldmap` | `github.com/nsgo-dev/game/packages/game/worldmap` | [worldmap](worldmap.md) | YAML 房间地图、随存档保存的房间发现与标记、当前区域小地图及世界地图 |
| `fog` | `github.com/nsgo-dev/game/packages/game/fog` | [fog](fog.md) | 按队伍划分的战争迷雾：阴影投射视野源、已探索与可见格子、地图叠加层与 JSON 存档 |
| `quality` | `github.com/nsgo-dev/game/packages/game/quality` | [quality](quality.md) | 按设备类型与实测帧时间选择低、中、高画质预设，可在设置中覆盖 |
| `a11y` | `github.com/nsgo-dev/game/packages/game/a11y` | [a11y](a11y.md) | 实时字幕、色盲调色板与 CVD 模拟、持久化辅助选项及屏幕阅读器菜单 |
| `l10n` | `github.com/nsgo-dev/game/packages/game/l10n` | [l10n](l10n.md) | 支持 CLDR 复数的 JSON 字符串表、脚本 `FontSet`、RTL 视觉排序与多语言资源目录 |
| `perf` | `github.com/nsgo-dev/game/packages/game/perf` | [perf](perf.md) | 零分配 `Pool`/`Arena`/`SlicePool`、`SpatialHash`、`Quadtree` 与 `CullBatch` |
| `idle` | `github.com/nsgo-dev/game/packages/game/idle` | [idle](idle.md) | 超出 float64 范围的 `Number`、带上限的离线收益，以及大数字的缩写、科学计数法与本地化格式 |
| `net` | `github.com/nsgo-dev/game/packages/game/net` | [net](net.md) | 跨平台 WebSocket/UDP/回环传输、服务端权威增量复制与锁步/回滚同步 |
| `server` | `github.com/nsgo-dev/game/packages/game/server` | [server](server.md) | 专用服务器无头固定步长 `Runner`：多场比赛独立速率、超时报告与有界信号停止 |
| `accounts` | `github.com/nsgo-dev/game/packages/game/accounts` | [accounts](accounts.md) | 可自行部署的玩家账号：游客升级为邮箱或绑定平台/OAuth 身份、argon2id 密码、SQLite/Postgres 存储，以及游戏服务器离线校验的 Ed25519 令牌 |
| `cloudsave` | `github.com/nsgo-dev/game/packages/game/cloudsave` | [cloudsave](cloudsave.md) | 置于 `liveops.CloudSync` 之后的自部署云存档：经 HTTPS 按账号划分的槽位、把并发保存变成冲突的条件写入、大小上限、可恢复的历史版本，以及 SQLite/Postgres 存储 |
| `leaderboard` | `github.com/nsgo-dev/game/packages/game/leaderboard` | [leaderboard](leaderboard.md) | 在 YAML 里一次性声明排行榜，通过 Game Center 或 Play 游戏服务提交分数并显示原生排行榜 |
| `leaderboard/selfhost` | `github.com/nsgo-dev/game/packages/game/leaderboard/selfhost` | [leaderboard/selfhost](leaderboard-selfhost.md) | 可自行部署的跨平台排行榜：总榜、日榜和周榜并保留往期，成绩由账号令牌认证、可选由游戏服务器签名，支持前 N 名、我附近和好友查询 |
| `presence` | `github.com/nsgo-dev/game/packages/game/presence` | [presence](presence.md) | 通过与平台无关的 `Activity` 与 `Manager` 在 Steam（经 purego 调用 `ISteamFriends`）和 Discord（纯 Go 本地 IPC v1）好友列表显示丰富状态 |
| `inventory` | `github.com/nsgo-dev/game/packages/game/inventory` | [inventory](inventory.md) | YAML 道具定义、可堆叠的格子容器、带类型的装备槽与存档快照 |
| `ability` | `github.com/nsgo-dev/game/packages/game/ability` | [ability](ability.md) | YAML 技能集与逐步推进的 `Caster`：由输入动作触发的消耗、冷却、充能、施法与引导时间以及条件 |
| `quest` | `github.com/nsgo-dev/game/packages/game/quest` | [quest](quest.md) | YAML 任务书：收集/到达/击败/对话目标、前置任务与奖励，由总线事件推进并以快照存档 |
| `loot` | `github.com/nsgo-dev/game/packages/game/loot` | [loot](loot.md) | YAML 编写的可嵌套加权掉落表，支持保底掉落与基于种子随机流的硬/软保底计数 |
| `vn` | `github.com/nsgo-dev/game/packages/game/vn` | [vn](vn.md) | 视觉小说演出：YAML 角色表的立绘与表情、名牌、打字机文本框、选项、回看记录、自动/快进模式与语音 |
| `modding` | `github.com/nsgo-dev/game/packages/game/modding` | [modding](modding.md) | 能力沙箱隔离的 TSX `"game"` 模组 API 与 `mod.yaml` 依赖/生命周期管理器 |
| `math3d` | `github.com/nsgo-dev/game/packages/game/math3d` | [math3d](math3d.md) | 3D 向量、矩阵、四元数、变换、包围体、射线、视锥体与统一数学约定 |
| `scene3d` | `github.com/nsgo-dev/game/packages/game/scene3d` | [scene3d](scene3d.md) | 3D 节点树、插槽/附件、动态胖 AABB/SAH BVH、轨道/FPS/第三人称相机与 3D 拾取 |
| `anim3d` | `github.com/nsgo-dev/game/packages/game/anim3d` | [anim3d](anim3d.md) | 3D 姿态状态机、1D/2D 混合空间、骨骼遮罩与叠加层、帧事件、IK 与根骨骼运动 |
| `world3d` | `github.com/nsgo-dev/game/packages/game/world3d` | [world3d](world3d.md) | 高度图地形、多层 3D 导航网格、3D `GridMap`、程序化植被与浮点原点区块流式加载 |
| `voxel` | `github.com/nsgo-dev/game/packages/game/voxel` | [voxel](voxel.md) | 分块 3D 体素世界：贪婪网格合并、跨区块剔除与 AO、脏区块局部重建、DDA 射线拾取与 YAML 存档 |
| `render3d` | `github.com/nsgo-dev/game/packages/game/render3d` | [render3d](render3d.md) | GPU 网格缓冲、纹理与采样器、相机 uniform、CPU 视锥裁剪、绘制排序、零分配实例化、LOD 滞回、调试线框/手柄、公告板、3D 条带拖尾与 GPU 预算 |
| `tooling/jsonschema` | `github.com/nsgo-dev/game/packages/game/tooling/jsonschema` | [YAML 文件的 Schema](schemas.md) | 由 Go 类型生成供编辑器和 AI 工具使用的 JSON Schema、已发布的场景 Schema，以及带 JSON Pointer 路径的校验 |
| `tooling/library` | `github.com/nsgo-dev/game/packages/game/tooling/library` | [资源与插件库](library.md) | 共享资源、编辑器插件和模板包的 YAML 索引，按摘要校验后安装到项目中，并记录其文件和 `game.yaml` 引用 |
| `framedata` | `github.com/nsgo-dev/game/packages/game/framedata` | [framedata](framedata.md) | 以 YAML 编写的格斗游戏逐帧攻击框、受击框、硬直与取消窗口，每步确定性解析 |
| `vehicle` | `github.com/nsgo-dev/game/packages/game/vehicle` | [vehicle](vehicle.md) | 基于 `physics3d` 的射线车辆：车轮悬挂、轮胎摩擦与车辆控制器，比赛检查点与圈数，以及 YAML 车辆文件和单圈幽灵 |

核心包 API 的单页速查见[包](packages.md)。每个加载器在文件无法使用时都会给出文件、行号、字段和修复方法，见[错误](errors.md)。游戏读写的每种文件及其版本和示例见[文件格式](formats.md)。

## 从其他引擎迁移

每篇指南都把对应引擎的场景、节点、资源、信号、输入映射、物理和导出对应到上面的包：[Godot](from-godot.md)、[Ebitengine](from-ebitengine.md) 和 [Unity](from-unity.md)。

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

关于六个目标平台的图标、签名、公证、商店打包与 `nsgo check` 验证，见[构建与发布](shipping.md)；打包图集、转换音频、生成资源清单、编码纹理、打开场景编辑器，以及从 `game.yaml` 构建游戏全部资源且只重建有改动部分的 `go tool game` 见[命令行](cli.md)。

### VR 与 AR

游戏层目前不支持 VR 和 AR（[#495](https://github.com/nsgo-dev/game/issues/495)）。所有包都绘制到一个平面的 NSGo 视图中，即 `Canvas` 或 [nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059) 的 GPU 视图，并读取触摸、鼠标、键盘、手柄和设备运动传感器的输入。NSGo v0.2 在任何目标平台上都没有 XR 运行时，而本仓库只使用 NSGo 的公开 API、不包含平台代码，因此无法自行添加。

等 NSGo 在至少一个运行时（桌面与 Android 头显上的 OpenXR、visionOS、ARKit、ARCore 或 WebXR）上提供以下能力后，这里才能开始 XR 的工作：

- **XR 会话**：启动会话，在头显被摘下或应用失去焦点时暂停，以及结束会话。
- **位姿**：每帧给出头部和每个控制器的位姿，并按该帧显示的时刻做预测，同时提供运行时的控制器与手部输入。
- **立体渲染**：每只眼睛一个渲染目标，各有自己的视图与投影矩阵，由 GPU 视图以头显的刷新率绘制并提交给运行时的合成器。
- **AR 所需**：绘制内容背后的相机透视画面、检测到的平面、对真实世界的命中测试，以及保持在原位的锚点。

首先应面向的运行时是 OpenXR，因为同一套 API 覆盖 Windows、Linux 和 Android 上的头显，即 NSGo 的三个目标平台；visionOS、ARKit、ARCore 和 WebXR 可以随后跟进。会话、位姿和立体渲染排在最前，有了它们就足以支持 VR；AR 所需的各项排在其后。

具备这些之后，本仓库会在 `scene3d` 中加入 XR 相机组件，在 `input` 中加入 XR 动作，并复用 `math3d` 的变换来表示位姿。在此之前，3D 游戏只在平面屏幕上运行。

## 面向语言模型

与 [nsgo.dev](https://nsgo.dev/llms.txt) 一样，本站为语言模型和编程智能体发布英文文档：[llms.txt](https://game.nsgo.dev/llms.txt) 列出每个页面并附一句简介，[llms-full.txt](https://game.nsgo.dev/llms-full.txt) 把每个页面的 Markdown 合在一个文件里。两者都随网站从 `docs/en` 构建，因此始终与这些页面一致。面向贡献者的[智能体评测套件](agent-eval.md)用 YAML、构建与 `game/mcp` 检查为固定的游戏任务评分，也可以让编程智能体来完成这些任务。
