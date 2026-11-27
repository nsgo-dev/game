# 概览

game 是用纯 Go 编写的 [NSGo](https://nsgo.dev/zh/) 应用 2D 与 3D 游戏层。游戏绘制在一个 NSGo 视图中——可以是 `Canvas`、像素缓冲区、着色器或 GPU 视图——HUD 和菜单仍然是叠在上面的普通原生 NSGo 视图。它只使用 NSGo 的公开 API（`core`、`audio`、`gamepad` 与 GPU 视图），所以没有导入它的应用不会链接其中任何代码。

> NSGo Game `v0.1.0` 需要 Go 1.27+ 与 NSGo `v0.3.4+`。可以在[API 稳定性](stability.md)中查看哪些包已稳定、哪些仍属实验性。从[快速开始](getting-started.md)创建并运行你的第一个游戏，选择一套[入门模板](templates.md)，或跟着[教程](tutorial.md)完成一款完整游戏。

## 2D 与 3D

1. **2D**：固定步长循环、输入动作、精灵、瓦片地图、场景、物理、光照、粒子、UI 和声音，以 60 fps 或显示器的刷新率绘制。
2. **3D**：在 NSGo GPU 视图上带深度地绘制网格、相机、PBR 光照、骨骼动画、3D 物理、地形、体素和 glTF 模型。坐标系约定、Blender 4.x 导出清单、FBX/OBJ 转换与示例 glTF 资产见[从 Blender 导入 3D 模型](3d-models-blender.md)；包含轨道相机、动态光照、原生 HUD 以及共享输入与音频的 2D ↔ 3D 场景切换示例见 [Lantern 3D](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d)。

循环、输入动作、声音、资源和场景切换由 2D 与 3D 共用，可以在同一款游戏里自由混用 2D 与 3D 场景而无需重写架构。每个面向作者的文件——场景、预制体、材质、动画、数据表与项目配置——都是带版本的纯文本 YAML（[文件格式](formats.md)），既适合手工与编程智能体直接读写，也可以在可视化[场景编辑器](editor.md)中编辑。

## 包

| 包 | 导入路径 | 指南 | 内容 |
| --- | --- | --- | --- |
| `game` | `github.com/nsgo-dev/game` | [Loop](loop.md) | `Loop`，所有包共用的固定步长循环 |
| `sprite` | `github.com/nsgo-dev/game/pkg/sprite` | [sprite](sprite.md) | 图集、帧、动画、精灵，以及绘制到 `Canvas` 中的批次 |
| `sound` | `github.com/nsgo-dev/game/pkg/sound` | [sound](sound.md) | `Mixer` 及其音效和音乐 |
| `input` | `github.com/nsgo-dev/game/pkg/input` | [input](input.md) | 基于触摸区域、鼠标按键、键盘按键、指针锁定鼠标视角和游戏手柄的命名动作与轴，支持 `core.Storage` 持久化 |
| `tilemap` | `github.com/nsgo-dev/game/pkg/tilemap` | [tilemap](tilemap.md) | 2D 相机、视锥裁剪瓦片地图、Tiled `.tmj` 加载、动画瓦片与射线查询 |
| `scene` | `github.com/nsgo-dev/game/pkg/scene` | [scene](scene.md) | 世界/屏幕图层、命中测试、场景栈与过渡动画 |
| `sceneformat` | `github.com/nsgo-dev/game/pkg/sceneformat` | [场景格式](formats.md#场景) | 带版本的 YAML 场景文档：严格解析与校验、确定性编码，以及构建成可直接绘制的精灵批次 |
| `editor` | `github.com/nsgo-dev/game/pkg/editor` | [场景编辑器](editor.md) | 基于 `sceneformat` 的 NSGo 场景编辑器：可撤销的场景、瓦片地图、图层与属性编辑，编辑器内试玩、资源浏览器、插件和项目面板 |
| `physics` | `github.com/nsgo-dev/game/pkg/physics` | [physics](physics.md) | `Box`/`Circle` 重叠检测、`MoveAndSlide` 瓦片滑动解析与 `World` 空间查询 |
| `physics3d` | `github.com/nsgo-dev/game/pkg/physics3d` | [physics3d](physics3d.md) | 确定性的 3D 碰撞检测、射线检测、刚体动力学与运动学角色控制器 |
| `platformer` | `github.com/nsgo-dev/game/pkg/platformer` | [platformer](platformer.md) | 基于 `CharacterBody` 的平台跳跃 `Controller`：土狼时间、跳跃缓冲、可变跳跃高度、贴墙下滑与蹬墙跳，参数来自 YAML 配置 |
| `screen` | `github.com/nsgo-dev/game/pkg/screen` | [screen](screen.md) | 虚拟分辨率 `Viewport` 缩放、像素对齐、移动端安全区与视频设置 |
| `project` | `github.com/nsgo-dev/game/pkg/project` | [项目文件](project.md) | 唯一的项目文件 `game.yaml`：窗口、虚拟分辨率、输入映射、音频总线、自动加载和导出预设，并与 `nsgo.yaml` 保持一致 |
| `tooling/refs` | `github.com/nsgo-dev/game/pkg/tooling/refs` | [稳定引用](project.md#稳定引用) | 稳定文件 ID、项目索引，以及改写所有引用的文件移动，供 `game mv`、`game index` 和 `game check` 使用 |
| `assets` | `github.com/nsgo-dev/game/pkg/assets` | [assets](assets.md) | 按场景异步加载的资源组、响应式进度与 `LoadingScreen` |
| `resource` | `github.com/nsgo-dev/game/pkg/resource` | [resource](resources.md) | 共享 YAML 资源文件（材质、曲线、渐变、动画片段、UI 主题、总线布局、物理材质），以及共享并重载它们的 `Library` |
| `importsettings` | `github.com/nsgo-dev/game/pkg/importsettings` | [导入设置旁注文件](formats.md#导入设置旁注文件) | 图片、声音、字体或模型旁边的 `.import.yaml` 旁注文件，说明 `game assets` 如何导入它，并覆盖目标的设置 |
| `ktx2` | `github.com/nsgo-dev/game/pkg/ktx2` | [tex](cli.md#tex) | KTX2 纹理容器的编码与解码、mipmap 链、Basis Universal ETC1S 与 UASTC 编解码，以及转码为 BC、ETC2 和 ASTC 块 |
| `loaderr` | `github.com/nsgo-dev/game/pkg/loaderr` | [可据以行动的错误](errors.md) | 每个加载器在文件无法使用时返回的错误：文件、行、列、字段、期望的内容以及修复方法 |
| `text` | `github.com/nsgo-dev/game/pkg/text` | [text](text.md) | AngelCode BMFont 位图字体、2D 与 3D 世界空间中的有向距离场字体，以及 `DrawPlatform` 平台矢量文本 |
| `placeholder` | `github.com/nsgo-dev/game/pkg/placeholder` | [占位素材](placeholder.md) | 用于原型制作的 CC0 素材包：精灵表、图块集与关卡、像素字体、声音和 glTF 模型，附带导入设置旁车文件 |
| `particles` | `github.com/nsgo-dev/game/pkg/particles` | [particles](particles.md) | 确定性 2D 粒子发射器 `Emitter`、发射形状、爆发与生命周期曲线 |
| `pixelfx` | `github.com/nsgo-dev/game/pkg/pixelfx` | [pixelfx](pixelfx.md) | pixelfx 像素风特效的 Go 移植：画笔、模拟和表演系统，按类别分包的特效（`pixelfx/fire`、`pixelfx/explosions` 等），换色、镜像与精灵表导出 |
| `bullets` | `github.com/nsgo-dev/game/pkg/bullets` | [bullets](bullets.md) | 射击游戏弹幕（环形、螺旋、瞄准连发），由 YAML 定义，对象池复用并通过精灵批次绘制 |
| `save` | `github.com/nsgo-dev/game/pkg/save` | [save](save.md) | 带版本号的 JSON 存档槽、结构迁移与原子备份恢复 |
| `achievements` | `github.com/nsgo-dev/game/pkg/online/achievements` | [achievements](achievements.md) | 在带版本号的 YAML 中声明成就、只增不减并保存在 `core.Storage` 的进度，以及上报到 Game Center 和 Play Games |
| `datatable` | `github.com/nsgo-dev/game/pkg/datatable` | [datatable](datatable.md) | 带版本号的 YAML（及 CSV/JSON）类型化数据表，单元格级错误、确定性保存与热重载 |
| `tooling/gamedebug` | `github.com/nsgo-dev/game/pkg/tooling/gamedebug` | [gamedebug](gamedebug.md) | 帧耗时图、碰撞线框、场景检查器及 `-tags release` 零开销裁剪 |
| `mcp` | `github.com/nsgo-dev/game/pkg/mcp` | [mcp](mcp.md) | 通过 `nsgo/automation` 注册的 MCP 工具，用于检查运行中的场景、图层、实体与组件 |
| `flow` | `github.com/nsgo-dev/game/pkg/flow` | [flow](flow.md) | 游戏时钟 `Timers`、`Machine[S]`、`Seq`/`Parallel`/`Race` 与类型化事件 `Bus` |
| `entities` | `github.com/nsgo-dev/game/pkg/entities` | [实体（entities）](entities.md) | 零反射 ECS：带代数的实体、类型化稀疏集组件、零分配查询、有序系统、预制体与 JSON 快照 |
| `stats` | `github.com/nsgo-dev/game/pkg/stats` | [stats](stats.md) | 基础属性与来自各来源的 add/mul/override 修饰符，改动即重算，按固定步长计时的修饰符与 YAML 属性文件 |
| `tween` | `github.com/nsgo-dev/game/pkg/tween` | [tween](tween.md) | 固定步长 `Tween`、`Timeline` 与相机运镜补间 |
| `animclip` | `github.com/nsgo-dev/game/pkg/animclip` | [animclip](animclip.md) | 为任何已注册属性打关键帧的动画片段，通过 `tween.Timeline` 在场景对象上播放 |
| `skeletal` | `github.com/nsgo-dev/game/pkg/skeletal` | [skeletal](skeletal.md) | 带版本 YAML 格式的 2D 骨骼绑定，可加载、保存、播放动画并通过 `sprite.Batch` 绘制 |
| `cards` | `github.com/nsgo-dev/game/pkg/cards` | [cards](cards.md) | 在牌桌槽位间拖放并吸附或弹回，扇形手牌、牌堆、叠放牌列与网格布局，以及 YAML 牌桌文件 |
| `rooms` | `github.com/nsgo-dev/game/pkg/rooms` | [rooms](rooms.md) | 房间地图：每个房间独立的相机边界，以及房间之间的切换、滑动或淡入淡出过渡 |
| `ai` | `github.com/nsgo-dev/game/pkg/ai` | [ai](ai.md) | 网格 `AStar`、2D `NavMesh`、转向行为、行为树与感知 |
| `ai/perception3d` | `github.com/nsgo-dev/game/pkg/ai/perception3d` | [ai](ai.md#3d) | 面向 3D 智能体的 `ai` 感知，基于 `math3d.Vec3`，用 `physics3d` 判定视线 |
| `ai/navbake` | `github.com/nsgo-dev/game/pkg/ai/navbake` | [navbake](navbake.md) | 按代理半径从凹障碍物或 tilemap 的实心格烘焙 `ai.NavMesh` 多边形，保存为带版本的 YAML，并提供调试叠加绘制 |
| `rts` | `github.com/nsgo-dev/game/pkg/rts` | [rts](rts.md) | 流场、带分离的大规模单位群 `Crowd`、YAML 阵型、框选/点选与编队 |
| `procgen` | `github.com/nsgo-dev/game/pkg/procgen` | [procgen](procgen.md) | 带种子的 BSP 房间、元胞自动机洞穴与基于 YAML 规则的波函数坍缩，附连通性检查与 tilemap 绘制 |
| `replay` | `github.com/nsgo-dev/game/pkg/replay` | [replay](replay.md) | 输入录制/回放（`Recorder`/`Player`）、可编辑的 YAML 录像与确定性测试断言 |
| `statedump` | `github.com/nsgo-dev/game/pkg/statedump` | [statedump](statedump.md) | 无窗口运行并转储为确定性 YAML（实体、组件、场景栈、全局值），以及两次运行之间的行 `Diff` |
| `gametest` | `github.com/nsgo-dev/game/pkg/gametest` | [gametest](gametest.md) | `core.DrawOp` 确定性软件光栅化、带持久化差异图的黄金 PNG 断言以及 `ImageDraws` 结构化图像校验 |
| `tooling/playtest` | `github.com/nsgo-dev/game/pkg/tooling/playtest` | [自动化试玩](playtest.md) | `playtest.yaml` 机器人、带回放录制的种子化无头运行、按目标评估的指标，以及确定性的 YAML 报告 |
| `cutscene` | `github.com/nsgo-dev/game/pkg/cutscene` | [cutscene](cutscene.md) | 覆盖在游戏视图上、可跳过的视频过场动画，可放在场景中或场景之间，以 YAML 文件描述 |
| `rhythm` | `github.com/nsgo-dev/game/pkg/rhythm` | [rhythm](rhythm.md) | 支持速度与拍号变化的 YAML 节拍图、基于音频时钟的音乐时钟，以及按设备的音频/画面延迟校准 |
| `undo` | `github.com/nsgo-dev/game/pkg/undo` | [undo](undo.md) | 可撤销操作，支持无限撤销、重做与 `Seek`，可保存为 YAML `Log` |
| `fighting` | `github.com/nsgo-dev/game/pkg/fighting` | [fighting](fighting.md) | 逐步输入缓冲、小键盘指令输入、蓄力、双击、同时按键与连招，由 YAML 招式表定义 |
| `math2d` | `github.com/nsgo-dev/game/pkg/math2d` | [math2d](math2d.md) | 共享 2D `Vec2`、`Rect`、`Circle` 与 `Affine` 值类型及无损 `core` 互转 |
| `random` | `github.com/nsgo-dev/game/pkg/random` | [确定性指南](determinism.md#2-按用途使用带种子的-randomstream) | 带种子的跨平台 `Stream`（Xoshiro256**）、程序化噪声，以及验证模拟确定性的辅助函数 |
| `gameui` | `github.com/nsgo-dev/game/pkg/gameui` | [gameui](gameui.md) | 画布内盒式布局菜单、虚拟分辨率缩放、空间焦点导航与世界坐标锚定原生视图 |
| `lights2d` | `github.com/nsgo-dev/game/pkg/lights2d` | [lights2d](lights2d.md) | 2D 环境暗场、点光源与聚光灯、可见性多边形阴影及法线贴图精灵 |
| `tilemap/tileshape` | `github.com/nsgo-dev/game/pkg/tilemap/tileshape` | [tileshape](tileshape.md) | 瓦片地图的碰撞与光照几何：物理 `TileGrid` 视图、由逐瓦片形状生成的斜坡、合并轮廓与 `lights2d` 遮挡体 |
| `effects` | `github.com/nsgo-dev/game/pkg/effects` | [effects](effects.md) | 在一次绘制中串联作用于游戏画面的 Kage 着色器通道、随帧时钟变化的 uniform 及 CPU 参考图 |
| `worldmap` | `github.com/nsgo-dev/game/pkg/worldmap` | [worldmap](worldmap.md) | YAML 房间地图、随存档保存的房间发现与标记、当前区域小地图及世界地图 |
| `fog` | `github.com/nsgo-dev/game/pkg/fog` | [fog](fog.md) | 按队伍划分的战争迷雾：阴影投射视野源、已探索与可见格子、地图叠加层与 JSON 存档 |
| `quality` | `github.com/nsgo-dev/game/pkg/quality` | [quality](quality.md) | 按设备类型与实测帧时间选择低、中、高画质预设，可在设置中覆盖 |
| `a11y` | `github.com/nsgo-dev/game/pkg/a11y` | [a11y](a11y.md) | 实时字幕、色盲调色板与 CVD 模拟、持久化辅助选项及屏幕阅读器菜单 |
| `l10n` | `github.com/nsgo-dev/game/pkg/l10n` | [l10n](l10n.md) | 支持 CLDR 复数的 JSON 字符串表、脚本 `FontSet`、RTL 视觉排序与多语言资源目录 |
| `perf` | `github.com/nsgo-dev/game/pkg/perf` | [perf](perf.md) | 零分配 `Pool`/`Arena`/`SlicePool`、`SpatialHash`、`Quadtree` 与 `CullBatch` |
| `idle` | `github.com/nsgo-dev/game/pkg/idle` | [idle](idle.md) | 超出 float64 范围的 `Number`、带上限的离线收益，以及大数字的缩写、科学计数法与本地化格式 |
| `netplay` | `github.com/nsgo-dev/game/pkg/online/netplay` | [netplay](netplay.md) | 跨平台 WebSocket/UDP/回环传输、服务端权威增量复制与锁步/回滚同步 |
| `wsclient` | `github.com/nsgo-dev/game/pkg/online/wsclient` | [wsclient](wsclient.md) | 连接第三方服务器的朴素 WebSocket 客户端，原生与 Web 平台同一套 API |
| `server` | `github.com/nsgo-dev/game/pkg/online/server` | [server](server.md) | 专用服务器无头固定步长 `Runner`：多场比赛独立速率、超时报告与有界信号停止 |
| `audit` | `github.com/nsgo-dev/game/pkg/online/audit` | [server](server.md) | 服务器管理事件的只追加 JSON Lines 审计日志：每个 UTC 日一个文件、不阻塞的队列与丢弃计数、按天保留 |
| `accounts` | `github.com/nsgo-dev/game/pkg/online/accounts` | [accounts](accounts.md) | 可自行部署的玩家账号：游客升级为邮箱或绑定平台/OAuth 身份、argon2id 密码、SQLite/Postgres 存储，以及游戏服务器离线校验的 Ed25519 令牌 |
| `cloudsave` | `github.com/nsgo-dev/game/pkg/online/cloudsave` | [cloudsave](cloudsave.md) | 置于 `liveops.CloudSync` 之后的自部署云存档：经 HTTPS 按账号划分的槽位、把并发保存变成冲突的条件写入、大小上限、可恢复的历史版本，以及 SQLite/Postgres 存储 |
| `leaderboard` | `github.com/nsgo-dev/game/pkg/online/leaderboard` | [leaderboard](leaderboard.md) | 在 YAML 里一次性声明排行榜，通过 Game Center 或 Play 游戏服务提交分数并显示原生排行榜 |
| `leaderboard/selfhost` | `github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost` | [leaderboard/selfhost](leaderboard-selfhost.md) | 可自行部署的跨平台排行榜：总榜、日榜和周榜并保留往期，成绩由账号令牌认证、可选由游戏服务器签名，支持前 N 名、我附近和好友查询 |
| `presence` | `github.com/nsgo-dev/game/pkg/online/presence` | [presence](presence.md) | 通过与平台无关的 `Activity` 与 `Manager` 在 Steam（经 purego 调用 `ISteamFriends`）和 Discord（纯 Go 本地 IPC v1）好友列表显示丰富状态 |
| `liveops` | `github.com/nsgo-dev/game/pkg/online/liveops` | [线上运营](liveops.md) | 与服务商无关的崩溃报告、经同意才收集的分析、`CloudSync` 接口，以及计入存档的应用内购买 |
| `inventory` | `github.com/nsgo-dev/game/pkg/inventory` | [inventory](inventory.md) | YAML 道具定义、可堆叠的格子容器、带类型的装备槽与存档快照 |
| `ability` | `github.com/nsgo-dev/game/pkg/ability` | [ability](ability.md) | YAML 技能集与逐步推进的 `Caster`：由输入动作触发的消耗、冷却、充能、施法与引导时间以及条件 |
| `quest` | `github.com/nsgo-dev/game/pkg/quest` | [quest](quest.md) | YAML 任务书：收集/到达/击败/对话目标、前置任务与奖励，由总线事件推进并以快照存档 |
| `loot` | `github.com/nsgo-dev/game/pkg/loot` | [loot](loot.md) | YAML 编写的可嵌套加权掉落表，支持保底掉落与基于种子随机流的硬/软保底计数 |
| `vn` | `github.com/nsgo-dev/game/pkg/vn` | [vn](vn.md) | 视觉小说演出：YAML 角色表的立绘与表情、名牌、打字机文本框、选项、回看记录、自动/快进模式与语音 |
| `modding` | `github.com/nsgo-dev/game/pkg/modding` | [modding](modding.md) | 能力沙箱隔离的 TSX `"game"` 模组 API 与 `mod.yaml` 依赖/生命周期管理器 |
| `plugin` | `github.com/nsgo-dev/game/pkg/plugin` | [插件](plugins.md) | 运行时插件契约与确定性安装器：API 版本校验、冲突检测，以及组件、系统、效果、数据表与控制台命令的有序注册 |
| `plugin/manifest` | `github.com/nsgo-dev/game/pkg/plugin/manifest` | [插件](plugins.md) | Go 插件 `plugin.yaml` 清单的严格解析器、校验器与确定性 YAML 编码器 |
| `math3d` | `github.com/nsgo-dev/game/pkg/math3d` | [math3d](math3d.md) | 3D 向量、矩阵、四元数、变换、包围体、射线、视锥体与统一数学约定 |
| `scene3d` | `github.com/nsgo-dev/game/pkg/scene3d` | [scene3d](scene3d.md) | 3D 节点树、插槽/附件、动态胖 AABB/SAH BVH、轨道/FPS/第三人称相机与 3D 拾取 |
| `anim3d` | `github.com/nsgo-dev/game/pkg/anim3d` | [anim3d](anim3d.md) | 3D 姿态状态机、1D/2D 混合空间、骨骼遮罩与叠加层、帧事件、IK 与根骨骼运动 |
| `world3d` | `github.com/nsgo-dev/game/pkg/world3d` | [world3d](world3d.md) | 高度图地形、多层 3D 导航网格、3D `GridMap`、程序化植被与浮点原点区块流式加载 |
| `ai/navbake3d` | `github.com/nsgo-dev/game/pkg/ai/navbake3d` | [navbake3d](navbake3d.md) | 按代理类型从 glTF、地形、`GridMap` 和物理关卡几何体烘焙导航网格，保存为场景引用的 YAML，附调试绘制和 `game navmesh` |
| `voxel` | `github.com/nsgo-dev/game/pkg/voxel` | [voxel](voxel.md) | 分块 3D 体素世界：贪婪网格合并、跨区块剔除与 AO、脏区块局部重建、DDA 射线拾取与 YAML 存档 |
| `render3d` | `github.com/nsgo-dev/game/pkg/render3d` | [render3d](render3d.md) | GPU 网格缓冲、纹理与采样器、相机 uniform、CPU 视锥裁剪、绘制排序、零分配实例化、LOD 滞回、调试线框/手柄、公告板、3D 条带拖尾与 GPU 预算 |
| `scenemodel` | `github.com/nsgo-dev/game/pkg/scenemodel` | [scenemodel](scenemodel.md) | 在 3D 中绘制场景文件的对象：内置基本形体和可替换材质的 glTF 模型，构建成带包围盒的 `scene3d` 节点树，并由带光照和纹理的渲染器绘制 |
| `gltf` | `github.com/nsgo-dev/game/pkg/gltf` | [从 Blender 导入 3D 模型](3d-models-blender.md) | 纯 Go 的 glTF 2.0 与 GLB 加载器：类型化访问器、节点层级、PBR 材质、蒙皮、变形目标、动画以及 Draco/meshopt 压缩 |
| `jsonschema` | `github.com/nsgo-dev/game/pkg/jsonschema` | [YAML 文件的 Schema](schemas.md) | 由 Go 类型生成供编辑器和 AI 工具使用的 JSON Schema、已发布的场景 Schema，以及带 JSON Pointer 路径的校验 |
| `tooling/library` | `github.com/nsgo-dev/game/pkg/tooling/library` | [资源与插件库](library.md) | 共享资源、编辑器插件和模板包的 YAML 索引，按摘要校验后安装到项目中，并记录其文件和 `game.yaml` 引用 |
| `framedata` | `github.com/nsgo-dev/game/pkg/framedata` | [framedata](framedata.md) | 以 YAML 编写的格斗游戏逐帧攻击框、受击框、硬直与取消窗口，每步确定性解析 |
| `vehicle3d` | `github.com/nsgo-dev/game/pkg/vehicle3d` | [vehicle3d](vehicle3d.md) | 基于 `physics3d` 的射线车辆：车轮悬挂、轮胎摩擦与车辆控制器，比赛检查点与圈数，以及 YAML 车辆文件和单圈幽灵 |

核心包 API 的单页速查见[包](packages.md)。每个加载器在文件无法使用时都会给出文件、行号、字段和修复方法，见[错误](errors.md)。游戏读写的每种文件及其版本和示例见[文件格式](formats.md)。

## 从其他引擎迁移

每篇指南都把对应引擎的场景、节点、资源、信号、输入映射、物理和导出对应到上面的包：[Godot](from-godot.md)、[Ebitengine](from-ebitengine.md) 和 [Unity](from-unity.md)。

## 安装

这个模块需要 Go 1.27 和 NSGo `v0.3.4` 或更高版本：

```sh
go get github.com/nsgo-dev/game
```

## 一帧如何运行

游戏就是一个 NSGo 组件。它在自己的作用域上启动一个 `game.Loop`，循环运行在 NSGo 的 `RequestAnimationFrame` 上：

1. 每个显示帧累加距上一帧的时间，其中包含几个完整的固定步长，就运行几次 `Update`，可能是零次，也可能是多次。
2. `Update` 按固定的 `dt` 推进游戏状态，让动画和计时器向前走。
3. `Draw` 运行一次，参数是这一帧在下一步中走到了多远。它设置游戏的 `Canvas` 读取的 signal，于是画布重新绘制，不需要提交视图树。

窗口在后台时，循环不运行任何更新，混音器也不播放任何声音；回到前台后两者都从原处继续。

## 目标平台

NSGo Game 面向六个主要构建目标——macOS、iOS、Windows、Linux、Android 和 Web——而鸿蒙、小程序、watchOS 和 tvOS 属于第一阶段之外的实验性目标。各目标的功能支持情况与 CI 测试覆盖并不一致，请查阅[平台支持](platforms.md)了解逐功能支持矩阵及每项缺口对应的上游 issue。

游戏主机（Nintendo Switch、PlayStation 和 Xbox）不在支持范围内：它们的 SDK 受保密协议（NDA）约束，且需要 C 工具链，而 NSGo 和本仓库只用 Go，不包含手写的 C 代码。

关于六个构建目标的图标、签名、公证、商店打包与 `nsgo check` 验证，见[构建与发布](shipping.md)；关于无障碍设置端到端串联、GAG/XAG 准则映射、发布前核查清单与 App Store 无障碍营养标签，见[无障碍指南](accessibility-guide.md)；打包图集、转换音频、生成资源清单、编码纹理、打开场景编辑器，以及从 `game.yaml` 构建游戏全部资源且只重建有改动部分的 `go tool game` 见[命令行](cli.md)。

### VR 与 AR

游戏层目前不支持 VR 和 AR（[#495](https://github.com/nsgo-dev/game/issues/495)）。所有包都绘制到一个平面的 NSGo 视图中（`Canvas` 或 GPU 视图），并读取触摸、鼠标、键盘、手柄和设备运动传感器的输入。NSGo 目前在任何目标平台上都没有 XR 运行时，而本仓库只使用 NSGo 的公开 API、不包含平台代码，因此无法自行添加。

等 NSGo 在至少一个运行时（桌面与 Android 头显上的 OpenXR、visionOS、ARKit、ARCore 或 WebXR）上提供以下能力后，这里才能开始 XR 的工作：

- **XR 会话**：启动会话，在头显被摘下或应用失去焦点时暂停，以及结束会话。
- **位姿**：每帧给出头部和每个控制器的位姿，并按该帧显示的时刻做预测，同时提供运行时的控制器与手部输入。
- **立体渲染**：每只眼睛一个渲染目标，各有自己的视图与投影矩阵，由 GPU 视图以头显的刷新率绘制并提交给运行时的合成器。
- **AR 所需**：绘制内容背后的相机透视画面、检测到的平面、对真实世界的命中测试，以及保持在原位的锚点。

首先应面向的运行时是 OpenXR，因为同一套 API 覆盖 Windows、Linux 和 Android 上的头显，即 NSGo 的三个目标平台；visionOS、ARKit、ARCore 和 WebXR 可以随后跟进。会话、位姿和立体渲染排在最前，有了它们就足以支持 VR；AR 所需的各项排在其后。

具备这些之后，本仓库会在 `scene3d` 中加入 XR 相机组件，在 `input` 中加入 XR 动作，并复用 `math3d` 的变换来表示位姿。在此之前，3D 游戏只在平面屏幕上运行。

## 面向语言模型

与 [nsgo.dev](https://nsgo.dev/llms.txt) 一样，本站为语言模型和编程智能体发布英文文档：[llms.txt](https://game.nsgo.dev/llms.txt) 列出每个页面并附一句简介，[llms-full.txt](https://game.nsgo.dev/llms-full.txt) 把每个页面的 Markdown 合在一个文件里。两者都随网站从 `docs/en` 构建，因此始终与这些页面一致。面向贡献者的[智能体评测套件](agent-eval.md)用 YAML、构建与 `game/mcp` 检查为固定的游戏任务评分，也可以让编程智能体来完成这些任务。

对于支持 Agent Skills 的编程智能体，可通过 `bunx skills add nsgo-dev/game` 安装游戏层技能，详见[智能体技能](agent-skill.md)。
