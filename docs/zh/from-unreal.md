# 从 Unreal Engine 迁移

把 Unreal Engine 的关卡、Actor、蓝图、数据资产、Enhanced Input、Gameplay Ability System、AI、Niagara、音频、Sequencer、UMG、Chaos 物理、材质、网络复制、存档与打包对应到这个游戏层，并给出讲解每一项的页面。

这里的游戏是 NSGo 组件里的纯 Go 代码，而不是挂在 UObject 继承树上的 C++ 模块和蓝图节点图。每个面向作者的文件——场景、预制体、材质、数据表、技能集、行为树、界面屏幕、过场动画与项目设置——都是带版本号的纯文本 YAML，而不是二进制的 `.uasset` 或 `.umap` 资源包，因此每次改动在 Git 中都呈现为清晰的文本差异，不需要独占锁。HUD 和菜单可以是叠在游戏画布上的原生 NSGo 视图，也可以是绘制在画布内的 [gameui](gameui.md) 屏幕。

## 速览

| Unreal Engine | 这里 | 指南 |
| --- | --- | --- |
| `Tick`、固定步长物理子步 | 固定 `Loop.Rate` 的 `game.Loop.Update(dt)` 与带插值的 `game.Loop.Draw(alpha)` | [Loop](loop.md) |
| 关卡（`.umap`）、子关卡、World Partition 与 Data Layers | YAML 场景文件（`sceneformat`）、`scene.Manager` 场景栈、`world3d.ChunkStreamer` 与 `FloatingOrigin`（场景分格流式加载与数据层：[#1728](https://github.com/nsgo-dev/game/issues/1728)） | [编辑器与场景格式](editor.md)、[scene](scene.md)、[world3d](world3d.md) |
| Actor、`SceneComponent`、`ActorComponent`、蓝图类默认值 | 场景对象、带命名插槽的 `scene3d.Node`、`entities.World` 实体/组件/系统与 `entities.Prefab` | [实体](entities.md)、[scene3d](scene3d.md) |
| 蓝图（Blueprints）、关卡蓝图、蓝图模组 | 纯 Go 与 YAML（`sceneformat` 连接；玩家模组用 `pkg/modding` TSX 沙箱；没有可视化脚本；声明式场景规则：[#1753](https://github.com/nsgo-dev/game/issues/1753)） | [编辑器与场景格式](editor.md)、[flow](flow.md)、[modding](modding.md) |
| Data Asset、Data Table、Curve Table | YAML [resource](resources.md) 文件（`resource.Library`、`resource.Curve`）与类型化 `datatable` YAML/CSV/JSON 数据表 | [resource](resources.md)、[datatable](datatable.md) |
| Enhanced Input（Input Actions 与 Mapping Contexts） | 在 `game.yaml` 中声明的 `input.Actions` 动作与轴（本地多人用 `input.Players`） | [input](input.md)、[项目文件](project.md) |
| Gameplay Ability System（技能、属性集、标签、效果、表现提示） | `ability`（`Set`、`Caster`）与 `stats`（`Sheet`、`Modifier`、`Effect`）；条件目前为字符串（标签注册表：[#1720](https://github.com/nsgo-dev/game/issues/1720)、效果：[#1721](https://github.com/nsgo-dev/game/issues/1721)、表现提示：[#1722](https://github.com/nsgo-dev/game/issues/1722)） | [ability](ability.md)、[stats](stats.md) |
| 行为树、黑板、EQS、AI 感知、导航网格 | `ai` 行为树（`*.bt.yaml`、带观察者中止的 `Blackboard`）、`TacticalQuery`、`Perception` / `perception3d`、`pathfind.NavMesh` 与 `pathfind3d.NavMesh` | [ai](ai.md)、[pathfind](pathfind.md)、[pathfind3d](pathfind3d.md) |
| Niagara 粒子系统 | `particles` 2D `Emitter` 与 3D `Emitter3D` + 实例化 `GPUParticles3D`（多发射器特效文件：[#1730](https://github.com/nsgo-dev/game/issues/1730)、3D 特性：[#1377](https://github.com/nsgo-dev/game/issues/1377)、GPU 计算模拟：[#1639](https://github.com/nsgo-dev/game/issues/1639)） | [particles](particles.md) |
| Sound Cues、衰减、Sound Classes 与 Submixes | `sound.Cue`、`Listener2D` / `Listener3D` / `Playback3D`、`sound.Mixer` 总线与 `busLayout` DSP 链（YAML 声音文件：[#1401](https://github.com/nsgo-dev/game/issues/1401)、并发组：[#1731](https://github.com/nsgo-dev/game/issues/1731)） | [sound](sound.md)、[resource](resources.md) |
| Sequencer、Media Player | `cutscene` v2 YAML 时间轴（`TimelinePlayer`）与可跳过视频过场（`Player`）；`animclip` 属性片段（3D 时间轴过场：[#1633](https://github.com/nsgo-dev/game/issues/1633)） | [cutscene](cutscene.md)、[animclip](animclip.md) |
| UMG 与 Common UI | 叠在画布上的原生 NSGo 视图，或 `gameui`（`*.screen.yaml`、`Root`、`SpatialNavigator`、`SettingsMenu`、`AnchorOverlay`） | [gameui](gameui.md) |
| Chaos 物理（刚体、约束、`CharacterMovementComponent`、载具） | `physics` / `RigidWorld`（2D）、`physics3d.World`、`CharacterController`、关节与布娃娃、`vehicle3d` 射线车辆 | [physics](physics.md)、[physics3d](physics3d.md)、[vehicle3d](vehicle3d.md) |
| 材质、Lumen、Nanite、后期处理体积 | `resource.Material` YAML、`render3d` PBR/卡通/WGSL 材质、`LightRig`、`CascadedShadowMap`、`LightmapRenderer`、`PostStack3D`；没有 Lumen 或 Nanite（光照贴图烘焙：[#1396](https://github.com/nsgo-dev/game/issues/1396)） | [render3d](render3d.md)、[resource](resources.md) |
| 网络复制、专用服务器 | `pkg/online/netplay`（`ServerWorld`/`ClientWorld` 增量复制、WebTransport/WebSocket/UDP、锁步/回滚）与 `pkg/online/gameserver`（`Runner`）；ECS 复制：[#1444](https://github.com/nsgo-dev/game/issues/1444) | [netplay](netplay.md)、[gameserver](gameserver.md) |
| SaveGame 对象与存档槽 | `save.Store` 带版本、迁移与原子备份恢复的 JSON 存档槽；`pkg/online/cloudsave` | [save](save.md)、[cloudsave](cloudsave.md) |
| 打包、资源烘焙（Cooking）、目标平台 | `game.yaml` 导出预设、`go tool game assets`、`go tool game release`、`go tool nsgo build` 与 `go tool nsgo check` | [构建与发布](shipping.md)、[命令行](cli.md) |

## 关卡与 World Partition

Unreal 把每个关卡存成二进制 `.umap` 文件，通过 World Partition 和 Data Layers 流式加载大世界。这里把场景内容、运行时切换和流式分块拆成三部分：

- **场景文件（`sceneformat`）**：关卡是带版本号的 YAML 文件，包含相机、瓦片地图、按顺序排列的对象图层、实例化或继承的子场景，以及 `components` 和 `properties` 映射。`sceneformat.Parse` 拒绝未知字段，`Encode` 写出确定性字节，`go tool game merge` 提供按对象合并的 Git 合并驱动，团队可以并发编辑同一个场景而无需锁文件。见[编辑器与场景格式](editor.md)。
- **场景切换（`scene.Manager`）**：`Switch` 用 `Fade`、`Crossfade` 或 `Wipe` 过渡替换当前场景；`Push` 配合 `Transparent` 控制器在当前场景上叠放暂停菜单或 HUD；`Pop` 卸载它。每个场景的 `Enter` 都会拿到一个子 `*core.Scope`，退出时自动释放它创建的循环、计时器、声音和信号。见 [scene](scene.md)。
- **大型 3D 世界（`world3d`）**：提供带无缝接缝 LOD 与最多 8 层权重混合 PBR 材质的 `Heightmap` / `TerrainLOD`、模块化 `GridMap`、程序化 `FoliageField`、使用 `float64` 坐标防止远离原点时 `float32` 抖动的 `FloatingOrigin`（`WorldPos64`，对应 Unreal 的 Large World Coordinates / 世界原点偏移），以及围绕玩家异步加载/卸载区块的 `ChunkStreamer`（导航瓦片由 `pathfind3d.NavTileStreamer` 流式加载）。见 [world3d](world3d.md) 与 [pathfind3d](pathfind3d.md)。

将单个大型场景 YAML 文件划分为可流式加载的网格单元与运行时开关的 Data Layers 在 [#1728](https://github.com/nsgo-dev/game/issues/1728) 中跟踪。

## Actor、组件与坐标系

Unreal 围绕拥有 `USceneComponent` 变换和 `UActorComponent` 逻辑的 `AActor` 子类组织游戏对象。这里把数据、变换树和更新逻辑分开：

- **游戏对象（`entities`）**：`entities.Entity` 是 64 位带代数句柄，已销毁的句柄不会误指新实体。组件是普通 Go 结构体，通过 `entities.Set`、`Get`、`Has` 和 `Remove` 操作；系统用 `Each1`/`Each2`/`Each3` 零分配遍历，通过 `Scheduler.AddSystem`（`Before`、`After`）声明执行顺序，对应 Unreal 的 Tick Group 和 Tick 依赖。`entities.Prefab`（`With`、`WithFunc`、`Clone`、`Spawn`）对应蓝图 Actor 模板。见[实体](entities.md)。
- **3D 层级（`scene3d`）**：`scene3d.Node` 管理父子局部变换、用于把武器或道具挂到骨骼上的命名插槽（`DefineSocket`、`Attach`）、标签查询（`FindByTag`）、类型化附件（`SetAttachment`），以及用于视锥裁剪与拾取的动态 `BVH`。见 [scene3d](scene3d.md)。
- **3D 骨骼动画（`anim3d`）**：用 Go 驱动的 `StateMachine`、带步态相位同步的 `BlendSpace1D`/`BlendSpace2D`、带 `BoneMask` 覆盖与叠加姿态的 `LayerStack`、定时 `ClipEvent`（对应 Anim Notifies）、双骨骼与 FABRIK IK 以及根骨骼运动提取替代 Animation Blueprints。见 [anim3d](anim3d.md)；YAML 动画图文件在 [#1405](https://github.com/nsgo-dev/game/issues/1405) 中跟踪。

坐标系与单位和 Unreal 不同：

- **3D**：Unreal 使用左手系、Z 向上（`+X` 朝前、`+Y` 向右、`+Z` 向上），单位是**厘米**。这里的 `math3d`、`scene3d`、`render3d`、`physics3d` 和 `world3d` 统一使用**右手系、Y 向上**（`+X` 向右、`+Y` 向上、`-Z` 朝前，与 glTF 2.0、WebGPU 和 Metal 一致），单位是**米**。Unreal 中以厘米表示的坐标 `(ux, uy, uz)` 换算到这里（米）为 `(uy / 100, uz / 100, -ux / 100)`。角度使用弧度（`DegToRad` / `RadToDeg`）。见 [math3d](math3d.md) 与[从 Blender 导入 3D 模型](3d-models-blender.md)。
- **2D**：使用屏幕空间，`+X` 向右、`+Y` 向下，顺时针旋转为正（弧度），单位是点。

## 蓝图与脚本

这里没有可视化节点图脚本语言（没有蓝图）。原本由蓝图承担的工作在这里分成三块：

- **Go 编写游戏逻辑**：直接写 Go 系统、状态机（`flow.Machine`）与函数并编译进应用——没有反射胶水层，增量编译快，固定步长模拟完全确定（见[确定性指南](determinism.md)）。
- **事件与延时序列（`flow`）**：Event Dispatchers 对应类型化的 `flow.Bus`（`Subscribe`、`Publish`、`Enqueue`/`Flush`）；蓝图 `Delay`、计时器与时间轴对应 `flow.Seq`/`Parallel`/`Race`、`flow.Timers` 和 `tween.Timeline`，全部只在固定步长上推进，不用 goroutine。见 [flow](flow.md) 与 [tween](tween.md)。
- **场景连线与玩家模组**：场景内简单的对象事件连线使用 `connections`（`sceneformat.Connect`、`Signals.Emit` 与 `Signals.Listen`）；不写 Go 的声明式条件与内置动作规则在 [#1753](https://github.com/nsgo-dev/game/issues/1753) 中跟踪。面向玩家的运行时脚本与模组由 `pkg/modding` 负责，在能力沙箱中运行 TSX/JS 脚本，支持 `mod.yaml` 清单、依赖解析、`ContentFS` 内容覆盖与 Steam Workshop 同步，见 [modding](modding.md)。编辑器扩展是编译进项目编辑器命令的 Go `editor.Plugin`（TSX 编辑器插件在 [#1288](https://github.com/nsgo-dev/game/issues/1288) 中跟踪）。

## 数据资产、数据表与曲线

- **Data Assets（`resource`）**：Unreal 的 `UDataAsset` 和共享资产类型对应带版本的 YAML [resource](resources.md) 文件，通过 `resource.Library` 按路径加载并共享（`material`、`curve`、`gradient`、`animation`、`theme`、`busLayout`、`physicsMaterial`），开发期可由 `hotreload.Watcher.WatchResources` 热重载。自定义数据资产直接定义 Go 结构体并严格解码带版本的 YAML 文件即可。
- **Data Tables（`datatable`）**：带版本的 YAML 数据表（`version: 1`、`rows:`）——也支持电子表格导出的 CSV 和 JSON——通过 `datatable.LoadFile[T]` 或 `LoadYAML[T]` 直接解码为类型化 Go 结构体，提供单元格级的 `*datatable.CellError` 报错、确定性的 `SaveYAML`、编辑器内的数据表面板以及 `datatablewatch.Watch` 热重载。见 [datatable](datatable.md)。
- **Curve Tables**：`resource.Curve`（`type: curve`，支持 `linear`/`step`/`smooth` 关键帧与 `Curve.Eval(t)`）以及 `particles.FloatCurve` / `ColorCurve` 对应 `UCurveFloat` 与曲线表。
- **资源加载与引用**：运行时素材放在 `resources/` 下，在所有目标平台上通过 `"asset:<路径>"` URI 打开；`assets.Manager` 按场景异步加载命名资源组并提供响应式进度条，覆盖 Unreal Asset Manager / Primary Assets 最常用的部分（[assets](assets.md)）。这里没有资源重定向器（Redirectors），而是由 `pkg/tooling/refs` 维护稳定的 `uid` 头和 `game.index.yaml`，移动文件时用 `go tool game mv` 或 `go tool game index` 原地改写所有引用（见[稳定引用](project.md#稳定引用)）。

## Enhanced Input

`input.Actions` 把命名动作和轴绑定到键盘按键、鼠标按键与指针锁定鼠标视角、手柄按键与摇杆、触控区域、屏幕触控层 `TouchControls`（`*.touch.yaml`）以及设备体感传感器。默认输入映射可以声明在 `game.yaml` 的 `input` 节（`project.File.BindInput(actions)`）或在 Go 代码中绑定，在每次 `game.Loop.Update` 开头调用一次 `actions.Step()` 并读取这一步的快照：

| Unreal Enhanced Input | 这里 |
| --- | --- |
| Input Action（`IA_*`） | `game.yaml` `input:` 或 `input.Actions` 中的命名动作与轴 |
| Input Mapping Context（`IMC_*`） | `project.File.BindInput(actions)`，或按模式在 Go 中切换绑定 |
| `Started` / `Triggered` / `Completed` | `actions.JustPressed(name)` / `actions.Pressed(name)` / `actions.JustReleased(name)` |
| 带死区与取反修饰器的 1D / 2D 轴 | `BindAxis`、`BindGamepadAxis`（`GamepadAxisOptions{DeadZone, Axial, Sensitivity, Invert}`）、`BindPointerAxis`（`Delta`）、`BindMotionAxis` |
| Touch Interface Setup | 支持安全区锚点、自动显隐的 `input.TouchControls`（`*.touch.yaml`）与 `VirtualPad` |
| 本地多人输入路由 | `input.Players`（独占手柄分配、同键盘分键位与断连策略 `DisconnectPolicy`） |
| Player Mappable Key Settings | 用 `actions.Save` 和 `actions.Load` 存到 `core.Storage`（`Load` 按名称合并，保留后续版本新增的默认绑定） |

在循环作用域上调用一次 `actions.WatchKeys(cx)` 和 `actions.WatchGamepads(cx)` 即可监听整个窗口的键盘与手柄输入。动作与格斗游戏所需的搓招缓冲、蓄力和组合键位于 [fighting](fighting.md)。面向玩家的灵敏度、轴反转与死区设置接入输入和 3D 相机在 [#1780](https://github.com/nsgo-dev/game/issues/1780) 中跟踪。见 [input](input.md) 与[项目文件](project.md)。

## Gameplay Ability System

两个包在固定步长上提供了 Unreal Gameplay Ability System（GAS）的核心能力：

- **技能（`ability`）**：在带版本的 YAML 中定义技能集（`costs`、`cooldown`、`charges` 与 `recharge`、`castTime`、`channelTime` 与 `tickInterval`、`trigger: press|hold`，以及 `[grounded, '!silenced']` 等 `conditions`），在 `actions.Step()` 之后每步推进每个角色的 `ability.Caster`。`Caster` 负责 `CanActivate`/`TryActivate` 校验、消耗 `Resources`（`ability.Pool` 或自定义适配器）、派发 `OnEvent`（`EventStarted`、`EventActivated`、`EventChannelTick`、`EventInterrupted`、`EventCooldownEnded`），并支持用于存档和回滚的确定性 `Snapshot`/`Restore`。见 [ability](ability.md)。
- **属性集与修饰符（`stats`）**：`stats.Sheet` 保存带 `Min`/`Max` 边界的命名基础属性，支持按来源标记（如 `"equip:weapon"`、`"buff:haste"`）的 `OpAdd`、`OpMul` 和带优先级的 `OpOverride` 修饰符、按固定步长计时的 `AddTimed`、成组施加或刷新的 `Effect`（`Apply` 叠加、`Refresh` 原地刷新时长）、`OnChange` 变更回调以及带版本的 YAML 属性文件。见 [stats](stats.md)。

与完整 GAS 相比仍在计划中的部分：

- **Gameplay Tags**：目前条件名和修饰符来源都是普通字符串；项目级 YAML 层级标签注册表、标签容器与标签查询在 [#1720](https://github.com/nsgo-dev/game/issues/1720) 中跟踪。
- **Gameplay Effects**：由技能直接施加、带瞬时/限时/周期性变更、叠加策略、授予标签与免疫规则的游戏效果在 [#1721](https://github.com/nsgo-dev/game/issues/1721) 中跟踪。
- **Gameplay Cues**：在 YAML 中把技能和效果触发的命名表现事件映射到粒子、声音与相机震动在 [#1722](https://github.com/nsgo-dev/game/issues/1722) 中跟踪。

## AI：行为树、战术查询与感知

`ai` 包在固定步长上提供与 Unreal 对应的 AI 能力，不启动后台 goroutine：

- **行为树与黑板**：可在 Go 或带版本的 `*.bt.yaml` 文件中编写 `ai.Node` 行为树，支持子树引用、参数替换与黑板键重映射。组合节点（`Sequence`、`Selector`、`Parallel`、`WeightedSelector`、`UtilitySelector`）、装饰节点（`Guard`、`Cooldown`、`TimeLimit`、`Retry`、`Service`）以及带 Unreal 风格观察者中止（`AbortNone`、`AbortSelf`、`AbortLower`、`AbortBoth`）的 `BlackboardCondition` 运行在类型化 `ai.Blackboard` 上，并支持共享父级小队黑板（`NewChildBlackboard`）。`ai` 还包含 GOAP（`GOAPPlanner`）、攻击令牌池（`TokenPool`）、围攻站位环（`SlotAssigner`）、小队角色与指令（`Squad`）以及影响力图（`InfluenceLayer`）。
- **EQS（环境查询系统）**：`ai.TacticalQuery` 串联候选点生成器（`CoverPointGenerator`、`RingGenerator`、`GridGenerator`、`NavMeshGenerator`）与按顺序执行的筛选/评分测试（`DistanceTest`、`AngleTest`、`InfluenceTest`、`CoverHeightTest`、`LOSTest`、`PathDistanceTest`、`CustomTest`），配合避免多人抢同一掩体的 `PointReservations` 与每步 `QueryBudget` 预算。
- **AI 感知**：`ai.Perception` / `PerceptionSystem`（2D）与 `ai/perception3d`（3D）整合了带外围视野与射线预算的视锥（`PhysicsLineOfSight`、`lights2d.LineOfSight`、`perception3d.PhysicsLineOfSight`）、听觉（`PostNoise`）、阵营态度（`Hostile`、`Neutral`、`Friendly`）和随时间衰减的记忆（`EventGained`、`EventUpdated`、`EventLost`、`EventForgotten`）。见 [ai](ai.md) 与 [3D 感知](ai.md#3d)。
- **导航**：2D 的 `pathfind.NavMesh` / `TiledNavMesh`（用 [navbake](navbake.md) 烘焙）与多层 3D 的 `pathfind3d.NavMesh` / `TiledNavMesh` / `NavTileStreamer`（用 [navbake3d](navbake3d.md) 和 `go tool game navmesh` 烘焙）支持多边形区域、运行时标志、网格外链接、`NavAgent` / `NavAgentGroup` 与 `ORCASolver` 局部避障。

已跟踪的 AI 缺口：`NavAgent` 感知 `TiledNavMesh` 动态障碍与区块更新（[#1717](https://github.com/nsgo-dev/game/issues/1717)）、整合 NavMesh 与 ORCA 的群体移动控制器（[#1718](https://github.com/nsgo-dev/game/issues/1718)），以及可申领交互槽位的 Smart Objects（[#1726](https://github.com/nsgo-dev/game/issues/1726)）。

## 粒子与音频

- **Niagara → `particles`**：确定性带种子的 2D `Emitter`（发射形状、爆发、子发射器、尺寸/透明度/旋转/颜色生命周期曲线、YAML 配置与 `HotConfig` 热重载）以及 3D `Emitter3D` + `GPUParticles3D`（3D 发射形状、球面/柱面/速度拉伸公告板、深度排序、叠加/透明混合，单次实例化 GPU 绘制即可渲染，不依赖计算着色器）。见 [particles](particles.md)。计划中的增强：含多个发射器、命名参数与粒子事件的特效文件（[#1730](https://github.com/nsgo-dev/game/issues/1730)）、3D 子发射器/碰撞/吸引子/湍流/拖尾与网格粒子（[#1377](https://github.com/nsgo-dev/game/issues/1377)），以及可选的 GPU 计算着色器模拟（[#1639](https://github.com/nsgo-dev/game/issues/1639)）。
- **Sound Cues、衰减与 Submixes → `sound`**：`sound.Mixer` 管理持久化在 `core.Storage` 中的音量总线（`music`、`sfx`、`ui`）、交叉淡入淡出的 `Music`（带前奏与循环点的 `PlayLooped`）、限声部 `Effect`、随机变体 `sound.Cue`（加权抽取、防连播窗口、音高与音量范围）、`Listener2D`（`PlayAt`）与 `Listener3D`（`PlayAt3D` 以及实时更新的 `Play3D` / `Playback3D`，支持线性/反比/指数衰减、后方头影衰减与定向发射锥），以及在 `game.yaml` 或 `resource.BusLayout` 中配置的总线 DSP 链（`LowPass`、`HighPass`、`Delay`、`Reverb`、`Gain`、`Bitcrush`，目前在加载内存 16 位 PCM WAV 时生效）。见 [sound](sound.md)。已跟踪的缺口：支持实时总线效果的软件混音器（[#1398](https://github.com/nsgo-dev/game/issues/1398)）、YAML 声音变体与衰减文件（[#1401](https://github.com/nsgo-dev/game/issues/1401)）、场景声源与多普勒效应（[#1402](https://github.com/nsgo-dev/game/issues/1402)），以及跨声音共享的并发与闪避组（[#1731](https://github.com/nsgo-dev/game/issues/1731)）。

## Sequencer 与 UI

- **Sequencer 与 Media Player → `cutscene` 和 `animclip`**：`cutscene` 同时支持可跳过的视频过场（`version: 1` 或 `2`，通过 `core.Video` 播放并暂停 `Loop` 与 `Mixer`）和确定性的引擎内 YAML 时间轴过场（`version: 2`、`kind: timeline`），包含 `camera`、`actor`、本地化 `dialogue`、`sound` 与 `music` 轨道，由 `TimelinePlayer` / `Player.PlayTimeline` 驱动（支持 `Seek` 拖动预览和 `go tool game check` 校验）。面向场景对象与精灵的通用属性关键帧片段在 [animclip](animclip.md)。已跟踪的缺口：3D 时间轴过场（[#1633](https://github.com/nsgo-dev/game/issues/1633)）以及信号标记、激活、嵌套与自定义时间轴轨道（[#1632](https://github.com/nsgo-dev/game/issues/1632)）。
- **UMG 与 Common UI → NSGo 视图与 `gameui`**：既可以直接把原生 NSGo 视图叠在画布上（绑定 `core.Signal`），也可以用 `gameui` 在画布内构建 HUD 与菜单——用 Go 代码或严格的 `*.screen.yaml` 文件编写（`vbox`、`hbox`、`label`、`button`、`image_progress`、`touch_stick`、`touch_button`、九点锚点、安全区、`deviceClasses`、面向手柄与键盘焦点导航的 `SpatialNavigator`、开箱即用的 `SettingsMenu`，以及把原生视图锚定到世界坐标的 `AnchorOverlay`）。见 [gameui](gameui.md)。已跟踪的缺口：可复用的屏幕片段（[#1343](https://github.com/nsgo-dev/game/issues/1343)）、带输入拦截与焦点恢复的弹窗栈（[#1347](https://github.com/nsgo-dev/game/issues/1347)），以及屏幕文件中的声明式数据绑定（[#1636](https://github.com/nsgo-dev/game/issues/1636)）。

## 物理与材质

- **Chaos 物理 → `physics`、`physics3d` 与 `vehicle3d`**：纯 Go 确定性解算器，在 `game.Loop` 固定步长上运行，所有目标平台零 cgo。3D 方面，`physics3d.World` 支持静态与动态刚体（`Box`、`Sphere`、`Capsule`、`ConvexHull`、`Cylinder`、`CompoundShape`）、静态 `TriMeshCollider`、`HeightfieldCollider` 与 `VoxelCollider`、射线检测与 `ShapeCast` 扫描（例如第三人称相机弹簧臂）、关节、布娃娃和胶囊 `CharacterController`；[vehicle3d](vehicle3d.md) 在此之上提供带悬挂和轮胎摩擦的射线车辆。2D 方面，[physics](physics.md) 和 [2D 刚体](physics2d-rigid.md) 提供重叠测试、`MoveAndSlide`、触发器、射线和带关节的刚体。这里没有 Chaos 破碎或布料模拟；连续碰撞检测改进在 [#1712](https://github.com/nsgo-dev/game/issues/1712) 中跟踪，场景文件中的 3D 关节与碰撞形状在 [#1379](https://github.com/nsgo-dev/game/issues/1379) 中跟踪。
- **材质与 Lumen → `resource.Material` 与 `render3d`**：`render3d` 提供金属度-粗糙度 PBR 材质（`Material`、`GPUMaterial`、`UploadGLTFMaterials` 及清漆/透射扩展）、卡通与无光照材质、支持热重载的自定义 WGSL `CustomMaterial`、`LightRig` 与 `ClusteredLightSystem`、方向光/级联/聚光/点光源阴影贴图、`OcclusionBuffer`、`HDRTarget` + `ToneMapPass`、`PostStack3D`（泛光、SSAO、景深）以及 `LightmapRenderer` / PBR 光照贴图。这里没有实时全局光照（Lumen）、虚拟微多边形几何（Nanite）或可视化材质节点图编辑器——材质是带版本的 YAML（`resource.Material`）或 WGSL 着色器。见 [render3d](render3d.md)。已跟踪的缺口：光照贴图烘焙器（[#1396](https://github.com/nsgo-dev/game/issues/1396)）以及更丰富的场景材质与着色器文件（[#1384](https://github.com/nsgo-dev/game/issues/1384)、[#1386](https://github.com/nsgo-dev/game/issues/1386)）。

## 网络与存档

- **网络复制与专用服务器 → `pkg/online/netplay` 和 `pkg/online/gameserver`**：`netplay` 通过统一的 `Transport` 覆盖 WebTransport（原生与浏览器上的 QUIC 数据报 + 流）、WebSocket、加密 UDP（Noise NK + ChaCha20-Poly1305）和确定性回环。`ServerWorld` 与 `ClientWorld` 提供服务端权威的字段级增量快照复制、客户端预测与插值；`LockstepSession` 与 `RollbackSession` 面向格斗、RTS 与街机联机；`*.wire.yaml` 协议定义可生成零分配 Go 与 TypeScript 编解码器（见 [netplay/wire](netplay-wire.md)）。专用服务器以无界面、无 cgo 的普通 Go 二进制运行在 `pkg/online/gameserver`（`Runner`）上，配套可自部署的 `accounts`、`cloudsave` 与 `leaderboard/selfhost`。见 [netplay](netplay.md) 与 [gameserver](gameserver.md)。已跟踪的缺口：`entities.World` 自动组件复制（[#1444](https://github.com/nsgo-dev/game/issues/1444)）、超出 2D 变换的自定义复制布局（[#1442](https://github.com/nsgo-dev/game/issues/1442)）以及兴趣管理（[#1325](https://github.com/nsgo-dev/game/issues/1325)）。
- **`USaveGame` → `save`**：`save.Store`（全平台含 Web 可用的 `NewStorageStore`，以及桌面与移动端的 `NewFileStore`）写出带版本号的 JSON 存档信封，支持顺序结构迁移（`WithMigration`）与原子备份恢复；玩家设置使用 `save.SettingsStore`，跨设备同步可选 `pkg/online/cloudsave` / `liveops.CloudSyncer`。见 [save](save.md) 与 [cloudsave](cloudsave.md)。已跟踪的缺口：不加载正文即可列出存档槽头信息与异步读写（[#1733](https://github.com/nsgo-dev/game/issues/1733)），以及整局可存档模块统一注册（[#1759](https://github.com/nsgo-dev/game/issues/1759)）。

## 打包与发布

项目设置和各平台导出预设保存在 `game.yaml` 中，并与 `nsgo.yaml` 保持一致。Go 可以从任意宿主机交叉编译到 Windows、Linux、Android（ARM64）与 WebAssembly，无需额外安装平台 SDK；只有 iOS 和签名的 macOS 构建需要装有 Xcode 的 Mac。游戏主机（Nintendo Switch、PlayStation 和 Xbox）不在支持范围内，因为它们的 SDK 受 NDA 约束且需要 C 工具链（见[平台支持](platforms.md)）。

| Unreal Engine | 这里 |
| --- | --- |
| 编辑器内运行（PIE） | `go tool game dev`（或 `go tool nsgo dev`，可加 `-target web`、`ios_simulator` 或 `android`） |
| 烘焙内容（Cook Content） | `go tool game assets`（增量打包图集、编码 KTX2 纹理、转换音频、烘焙 BMFont、打包 GLB 并生成清单） |
| 地图检查 / 资产校验 | `go tool game check` 与 `go tool nsgo check -target all` |
| 打包项目（导出预设） | `go tool game release`（读取 `game.yaml` 的 `export.presets`，带 `-tags release` 构建并生成 `CREDITS.md`）或 `GOFLAGS=-tags=release go tool nsgo build` |
| 商店分发包（`.aab`、`.ipa`、`.pkg`、`.msix`） | `go tool game release -channel store` |

带特性标签、Go 构建标签与资源过滤的命名导出预设在 [#1428](https://github.com/nsgo-dev/game/issues/1428) 中跟踪。见[构建与发布](shipping.md)、[命令行](cli.md)和[快速开始](getting-started.md)。
