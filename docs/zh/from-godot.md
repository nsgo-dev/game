# 从 Godot 迁移

把 Godot 4 的场景、节点、资源、信号、输入映射、物理和导出逐一对应到这个游戏层，并给出讲解每一项的页面。

最大的区别在于游戏写在哪里。在 Godot 里，你在编辑器中搭场景，再给节点挂 GDScript；在这里，游戏是 NSGo 组件里的 Go 代码，场景、预制体和设置都是 YAML 文本文件，可以手写，也可以由[编辑器](editor.md)保存。Godot 把场景和资源存成文本（`.tscn`、`.tres`）的习惯在这里延续：你写的每个文件都是能在评审中阅读和比对差异的文本。

## 速览

| Godot | 这里 | 指南 |
| --- | --- | --- |
| `_physics_process(delta)`、每秒物理帧数 | 固定 `dt` 的 `game.Loop.Update(dt)`、`Loop.Rate` | [Loop](loop.md) |
| `_process(delta)` | `game.Loop.Draw(alpha)` 及它触发重绘的 `core.Canvas` | [Loop](loop.md) |
| `.tscn` 场景、`PackedScene` | YAML 场景文档（`sceneformat`），构建为图层 | [编辑器与场景格式](editor.md) |
| `get_tree().change_scene_to_file()` | 带过渡效果的 `scene.Manager.Switch` | [scene](scene.md) |
| `Node2D` 树、`CanvasLayer` | 世界空间或屏幕空间的 `scene.Scene` 图层 | [scene](scene.md) |
| `Node3D` 树、分组 | `scene3d.Node` 树、`FindByTag` | [scene3d](scene3d.md) |
| 把场景当模板实例化 | `entities.Prefab`，或场景对象的 `prefab` 字段 | [实体](entities.md) |
| `Resource`、`.tres`、`ResourceLoader` | 放进 `resource.Library` 的 YAML 资源文件；`resources/` 下的图像和声音；数据通过带 `WithFS` 的 `assets.Manager` 读取 | [resource](resources.md)、[assets](assets.md) |
| 信号、`connect`、`emit` | `flow.Bus` 的 `Subscribe` 与 `Publish`；界面状态用 `core.Signal` | [flow](flow.md) |
| `.tscn` 里的 `[connection]` | 场景文件的 `connections`，由 `sceneformat.Connect` 与 `Signals.Emit` 运行，或者像 `CONNECT_DEFERRED` 那样用 `Signals.Enqueue` 与 `Flush` 延后运行；`SpawnConnected` 把它们绑定到实体 | [编辑器与场景格式](editor.md#连接490) |
| `Timer` 节点、`await` 信号 | `flow.Timers`、`flow.Seq` 与 `flow.Wait` | [flow](flow.md) |
| `Tween` | `tween.Tween`、`tween.Timeline` | [tween](tween.md) |
| `RandomNumberGenerator`（`randf`、`randi_range`、`randfn`、`rand_weighted`） | 按用途播种和派生的 `random.Stream`（`Float64`、`IntRange`、`Normal`、`WeightedIndex`） | [确定性](determinism.md) |
| 输入映射、`Input.is_action_just_pressed` | `input.Actions`、`JustPressed` | [input](input.md) |
| `CharacterBody2D.move_and_slide()` | 针对物体、形状、瓦片与静态刚体的 `World.MoveAndSlide` / `MoveAndSlideWith`（纯瓦片可用 `physics.MoveAndSlide`，平台跳跃可用 `platformer.Controller.StepWorld`） | [physics](physics.md)、[platformer](platformer.md) |
| `RigidBody2D`、关节 | `physics.RigidWorld`、`RigidBody`、关节 | [2D 刚体](physics2d-rigid.md) |
| `CharacterBody3D`、`RigidBody3D` | `physics3d.CharacterController`、`physics3d.World` | [physics3d](physics3d.md) |
| 导出预设、导出模板 | `nsgo.yaml` 与 `go tool nsgo build` | [构建与发布](shipping.md) |

## 场景

Godot 的场景是一棵保存下来的节点树，可以实例化，也可以切换过去。这里把这两件事分开：

- **场景里有什么**由一份带 `version: 1` 的 YAML 文档描述：相机、瓦片地图和按顺序排列的图层，图层里放着对象，每个对象有变换、精灵，以及自由格式的 `components` 和 `properties`（承担导出变量的角色）。`sceneformat.Parse` 读取它并拒绝未知字段；`sceneformat.Build` 把它变成可以直接绘制的图层。见[编辑器与场景格式](editor.md)。
- **运行哪个场景**由 `scene.Manager` 决定，它持有一个 `scene.Controller` 栈。`Switch` 用 `Fade`、`Crossfade` 或 `Wipe` 过渡替换当前场景，`Push` 在上面叠一个暂停菜单，`Pop` 返回。见 [scene](scene.md)。

`Controller.Enter` 会收到一个自己的子 `*core.Scope`。在它上面创建的循环、计时器、声音和信号会在场景退出时一并释放，这覆盖了 `_ready`、`_exit_tree` 和 `queue_free` 对场景自身对象所做的事。

自动加载（autoload）写在 `game.yaml` 里：它的 `autoload` 部分列出启动时要加载的场景和资源，并指定主场景，`LoadAutoloads` 会在第一帧之前读取并校验全部文件（见[在游戏中读取](project.md#在游戏中读取)）。其他单例就是普通的 Go 值：在根组件里创建一次，再传给需要它的场景。

## 节点

这里没有一个让所有东西都继承的节点类，按需要选用：

- **2D 绘制**：`scene.Scene` 持有图层；`SpaceWorld` 图层在场景相机下绘制（相当于 `Camera2D` 下的 `Node2D`），`SpaceScreen` 图层在视图空间绘制（相当于 `CanvasLayer`）。图层中可以放瓦片地图、精灵批次，以及带自己 `Draw`、`Update` 和命中测试钩子的 `scene.Drawable`。见 [scene](scene.md)、[sprite](sprite.md) 和 [tilemap](tilemap.md)。
- **3D 层级**：`scene3d.Node` 有父节点、子节点、局部变换、标签（即 Godot 的分组，用 `FindByTag` 查找）、用于挂接物体的命名插槽，以及用 `SetAttachment` 设置的类型化数据。见 [scene3d](scene3d.md)。
- **带行为的游戏对象**：不是给每个节点挂脚本，而是把数据放在 `entities.World` 的组件里，再写系统，每个固定步按声明的顺序运行。`Prefab` 把同一组组件盖到新实体上，就像实例化一个小场景。见[实体](entities.md)。
- **HUD 和菜单**：Godot 的 `Control` 节点在这里是叠在画布上的普通 NSGo 视图，或在画布内绘制的 [gameui](gameui.md) 控件。

2D 坐标与 Godot 一致：X 向右，Y 向下，旋转以弧度计、顺时针为正。3D 也一致：右手系，Y 向上，`-Z` 朝前，一个单位为一米（见 [math3d](math3d.md) 和[从 Blender 导入 3D 模型](3d-models-blender.md)）。

## 资源

Godot 的 `.tres` 资源对应 [resource](resources.md) 文件：每个文件是一个带 `type` 和 `version` 头的 YAML 文档，用于材质、曲线、渐变、动画片段、UI 主题、声音总线布局和物理材质。`resource.Library` 按路径从 `fs.FS` 加载它们，每个路径只保留一个值，就像 `ResourceLoader` 缓存已加载的资源。

游戏运行时加载的图像和声音放在 `resources/` 下，在所有目标平台上都用 `"asset:<路径>"` URI 打开，例如 `core.Src("asset:hero.png")`。`assets.Manager` 自己读取的数据，例如图集 JSON、原始字节和你自己解析的文件，来自你交给它的文件系统：用 `//go:embed` 嵌入后传入 `assets.NewManager(cx, assets.WithFS(fsys))`，或者用 `WithReadAsset` 接入自定义存储。两者都不传时，它读取相对于工作目录的 `assets/` 目录，只有在项目根目录下运行桌面版时才能用。`assets.Manager` 在引擎 goroutine 之外按命名资源组加载图集、声音、图像、字节和你自己解析的文件，以信号形式报告进度供加载界面使用，并在场景结束时释放资源组。见 [assets](assets.md)。

文件按生成它的工具写出的格式直接读取，游戏与它们之间没有导入步骤，只有 `game assets` 构建的文件例外：它打包图集、编码 KTX2 纹理、转换声音、把字体烘焙为位图字体并把 glTF 模型打包为 GLB 文件，Godot 的 `.import` 文件对应它的导入设置附属文件，即放在每张图片、每个声音、每个字体或每个模型旁边的 YAML 文件（如 `hero.png.import.yaml`），保存压缩格式、过滤方式、精灵表切分、循环点、字号与字符，或模型缩放（见[命令行](cli.md#assets)）。游戏直接读取的文件包括 TexturePacker 和 Aseprite 图集（[sprite](sprite.md)）、Tiled 地图（[tilemap](tilemap.md)）以及 glTF 模型（[从 Blender 导入 3D 模型](3d-models-blender.md)）。你自己的 `Resource` 子类，写成 YAML 或 JSON 文件并解码到 Go 结构体，像场景格式一样拒绝未知字段。

存档取代 `ConfigFile` 和 `FileAccess` 写文件：[save](save.md) 提供带版本的 JSON 存档槽、迁移和备份。

## 信号

Godot 的信号在这里承担的两件事，各有各的工具：

- **系统之间的游戏事件**走类型化的 `flow.Bus`。一个结构体类型就是一个信号；`flow.Subscribe` 相当于连接，`flow.Publish` 立即发出，`flow.Enqueue` 配合 `Bus.Flush` 延后发出（Godot 的延迟连接），`flow.SubscribeOnce` 是一次性连接。没有任何东西以字符串为键，拼错的事件编译不过。见 [flow](flow.md)。
- **视图显示的状态**，例如 HUD 里的分数，是一个 [`core.Signal`](https://pkg.go.dev/github.com/nsgo-dev/nsgo/core#Signal)。读取它的视图会在它变化时重新渲染，不需要手动接处理函数。

用 `await` 等待变成步进序列：`flow.Seq(flow.Wait(d), flow.Do(fn))` 由 `flow.Runner` 在每个固定步推进，不用 goroutine。`Timer` 节点变成 `flow.Timers.After` 和 `Every`。`body_entered` 之类区域回调见下面的物理一节。

## 输入映射

Godot 的输入映射在这里是一个 `input.Actions` 值：把名字绑定到输入，在每次固定更新开始时调用一次 `Step`，再查询这一步的状态。

| Godot | 这里 |
| --- | --- |
| 在项目设置中添加动作 | `actions.Bind("jump", input.TouchRegion(rect), input.MouseButton(core.MouseButtonLeft))` |
| `Input.is_action_pressed("jump")` | `actions.Pressed("jump")` |
| `Input.is_action_just_pressed("jump")` | `actions.JustPressed("jump")` |
| `Input.is_action_just_released("jump")` | `actions.JustReleased("jump")` |
| `Input.get_axis("left", "right")` | `actions.BindAxis("move_x", "left", "right")`，然后 `actions.Axis("move_x")` |
| `TouchScreenButton` | `input.TouchRegion`，或由屏幕摇杆和按钮组成的 `VirtualPad` |
| 保存重新映射的按键 | 用 `actions.Save` 和 `actions.Load` 存到 `core.Storage` |

目前可以绑定的是触控区域、鼠标按钮和设备体感。键盘和手柄绑定在 [#3](https://github.com/nsgo-dev/game/issues/3) 中跟踪；在那之前，可以用 NSGo 的 `cx.OnKey` 绑定按键，在回调里调用 `actions.Trigger("pause")`，让该动作在一个步内处于按下状态。见 [input](input.md)。

## 物理

2D 物理包与绘制使用同一个屏幕空间，并按循环的固定 `dt` 推进。

- **`CharacterBody2D`**：`World.MoveAndSlide(body, dt)` 与 `World.MoveAndSlideWith(body, dt, opts)` 按 `Body` 的真实形状对静态与运动学物体（含 `Body.OneWay` 单向平台）、实心与单向瓦片及静态 `RigidBody` 碰撞体做扫掠滑动，支持非轴对齐法线，并返回 `Collisions` 与 `OnFloor`、`OnWall`、`OnCeiling`（可配置 `UpX`/`UpY`、`FloorMaxAngle` 及俯视角 `MotionFloating` 模式），对应 Godot 的 `move_and_slide()`、`is_on_floor()`、`get_slide_collision()`、`up_direction` 与 `motion_mode`。仅与瓦片碰撞时仍可使用 `physics.MoveAndSlide(body, grid, dt)`；需要斜坡与移动平台时使用 `CharacterBody.MoveWorld` 或 `platformer.Controller.StepWorld`。见 [physics](physics.md) 与 [platformer](platformer.md)。
- **`Area2D`**：用 `World.AddTrigger` 加入的 `physics.Trigger` 会在 `World.StepTriggers` 中调用 `OnEnter`、`OnStay` 和 `OnExit`，相当于 `body_entered` 和 `body_exited`。
- **`RayCast2D` 和 `intersect_ray`**：`World.RaycastFirst` 和 `World.Raycast`；形状和点查询用 `World.QueryOverlap` 和 `QueryPoint`。
- **碰撞层与掩码**：物体和触发器上的 `Layer` 与 `Mask` 字段（`LayerMask`）。
- **`RigidBody2D` 与关节**：`physics.RigidWorld` 加上盒形和圆形的 `RigidBody`；`RevoluteJoint` 对应 `PinJoint2D`，`SpringJoint` 对应 `DampedSpringJoint2D`，`DistanceJoint` 是定长杆。见 [2D 刚体](physics2d-rigid.md)。
- **3D**：`physics3d.CharacterController` 像 `CharacterBody3D` 一样让胶囊体走过斜坡和台阶，`physics3d.World` 推进盒体、球体和胶囊体刚体。见 [physics3d](physics3d.md)。

2D 形状有盒形、圆形、凸多边形（最多 8 个顶点，可加圆角）、胶囊、线段和链（见 [physics](physics.md#多边形胶囊体线段与链)）；凹轮廓由多个凸多边形拼成，静态的关卡边缘也可以用链。场景文件目前只能放盒形和圆形碰撞体（#1379）。3D 中物体是盒体、球体和胶囊体，静态的关卡几何可以用三角网格或高度场参与碰撞：`ConcavePolygonShape3D` 对应用 `World.AddTriMesh` 加入的 `physics3d.TriMeshCollider`，`HeightMapShape3D` 对应用 `World.AddHeightfield` 加入的 `physics3d.HeightfieldCollider`。

## 导出

导出预设变成项目根目录下的 `nsgo.yaml`：应用名称、标识符、版本、图标以及要构建的目标平台。不需要下载导出模板，因为 Go 本身可以交叉编译；只有 iOS 和签名的 macOS 构建需要装有 Xcode 的 Mac。

| Godot | 这里 |
| --- | --- |
| 在编辑器中运行项目 | `go tool nsgo dev`，可加 `-target web`、`ios_simulator` 或 `android` |
| `export_presets.cfg` | `nsgo.yaml`（`build.targets`、`icon`、`id`、`version`） |
| `godot --export-release` | `go tool nsgo build` |
| 为应用商店导出 | `go tool nsgo build -channel store` |

发布前用 `go tool nsgo check -target all` 针对每个目标平台检查游戏。见[构建与发布](shipping.md)；第一个项目从[快速开始](getting-started.md)入手。
