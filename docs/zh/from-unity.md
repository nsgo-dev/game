# 从 Unity 迁移

把 Unity 的场景、GameObject 与组件、资源、事件、Input System、物理和构建对应到这个游戏层，并给出讲解每一项的页面。

这里的游戏是 NSGo 组件里的 Go 代码，而不是在编辑器里挂上的 C# 脚本。场景、预制体和设置都是带版本字段的 YAML 文本，对象 ID 是 `crate_3` 这样可读、每次保存都不变的名字，而 `.unity` 文件用不透明的 `fileID` 数字引用对象，所以对场景的修改呈现为一处小小的差异。HUD 和菜单是叠在游戏画布上的原生 NSGo 视图，对应 Unity 中的 uGUI 或 UI Toolkit。

## 速览

| Unity | 这里 | 指南 |
| --- | --- | --- |
| `FixedUpdate`、`Time.fixedDeltaTime` | 固定 `dt` 的 `game.Loop.Update(dt)`、`Loop.Rate` | [Loop](loop.md) |
| `Update`、`LateUpdate`、渲染 | `game.Loop.Draw(alpha)` 及它触发重绘的 `core.Canvas` | [Loop](loop.md) |
| `.unity` 场景 | YAML 场景文档（`sceneformat`） | [编辑器与场景格式](editor.md) |
| `SceneManager.LoadScene`、叠加加载 | `scene.Manager.Switch`、`Push` 与 `Pop` | [scene](scene.md) |
| 带组件的 GameObject、`MonoBehaviour` | `entities.World` 的实体、组件与系统 | [实体](entities.md) |
| `Transform` 层级 | `scene3d.Node` 树 | [scene3d](scene3d.md) |
| 预制体、`Instantiate`、`Destroy` | `entities.Prefab`、`World.Spawn`、`World.Destroy` | [实体](entities.md) |
| `Resources.Load`、Addressables | 图像和声音用 `"asset:"` URI，基于嵌入文件系统的 `assets.Manager` 资源组 | [assets](assets.md) |
| C# 事件、`UnityEvent` | `flow.Bus`；界面状态用 `core.Signal` | [flow](flow.md) |
| 协程、`Invoke`、`InvokeRepeating` | `flow.Seq`、`flow.Timers.After` 与 `Every` | [flow](flow.md) |
| Input System 动作映射 | `input.Actions` | [input](input.md) |
| `Rigidbody2D`、`Collider2D`、关节 | `physics` 物体、`RigidWorld`、关节 | [physics](physics.md)、[2D 刚体](physics2d-rigid.md) |
| `Rigidbody`、`CharacterController` | `physics3d.World`、`physics3d.CharacterController` | [physics3d](physics3d.md) |
| Build Settings、Player Settings | `nsgo.yaml` 与 `go tool nsgo build` | [构建与发布](shipping.md) |
| `PlayerPrefs` | `core.Storage`、`save` 存档槽 | [save](save.md) |
| `Editor` 文件夹中的编辑器脚本、编辑器扩展包 | 编译进游戏自己的编辑器命令的 `editor.Plugin` | [编辑器插件](editor.md#编辑器插件492) |

## 场景

场景文档是带 `version: 1` 的 YAML：虚拟分辨率、背景色、相机、瓦片地图，以及按顺序排列、放着对象的图层。每个对象有 `id`、变换、可选的精灵、可选的 `prefab` 路径，以及用来存放原本在 Inspector 中显示的值的 `components` 和 `properties` 映射。`sceneformat.Parse` 拒绝未知字段和非法变换，`Encode` 对同一场景总是写出相同的字节，`Build` 把文档变成可以直接绘制的图层。见[编辑器与场景格式](editor.md)。

运行时由 `scene.Manager` 决定运行哪个场景。`Switch` 相当于带 `Fade`、`Crossfade` 或 `Wipe` 过渡的 `LoadScene`；`Push` 一个 `Transparent` 控制器，会在当前场景上叠一个暂停菜单，就像叠加加载一个 UI 场景；`Pop` 卸载它。每个场景的 `Enter` 会得到一个子 `*core.Scope`，在它上面创建的循环、计时器、声音和信号会在场景退出时释放，所以场景不会留下任何东西。见 [scene](scene.md)。

## GameObject 与组件

Unity 把行为挂在对象上；这里数据和行为是分开的，与 Unity 的 DOTS 类似：

- `entities.Entity` 是带代数（generation）的句柄，已销毁的句柄永远不会被误认为新实体（对应 Unity 中对“已销毁对象”的检查）。
- 组件就是普通的 Go 结构体：`entities.Set(world, e, Health{HP: 10})`、`Get`、`Has` 和 `Remove`。
- `MonoBehaviour` 的 `FixedUpdate` 变成一个系统，遍历所有具备所需组件的实体（`entities.Each2[Position, Velocity]`）。`Scheduler.AddSystem` 配合 `Before` 和 `After` 确定顺序，对应 Unity 的 Script Execution Order。
- 预制体是 `entities.Prefab`：`With` 添加组件值，`Clone` 生成变体，`Spawn` 和 `SpawnN` 实例化。

见[实体](entities.md)。3D 变换方面，`scene3d.Node` 持有父节点、子节点和局部位置、旋转、缩放；插槽用于把武器挂到骨骼上这类需求，`FindByName` 和 `FindByTag` 对应 `GameObject.Find` 和标签。见 [scene3d](scene3d.md)。

坐标系与 Unity 不同。2D 绘制和 2D 物理使用屏幕空间：X 向右，Y 向下，旋转顺时针为正，单位是点。3D 是右手系，Y 向上，`-Z` 朝前，而 Unity 是左手系、`+Z` 朝前，所以迁移位置和朝向时要把 Z 取反。两边的 3D 单位都是一米。见 [math3d](math3d.md) 和[从 Blender 导入 3D 模型](3d-models-blender.md)。

## 资源

运行时加载的图像和声音放在 `resources/` 下，在所有目标平台上都用 `"asset:<路径>"` 打开，例如 `core.Src("asset:hero.png")`，就像 `Resources.Load` 的路径。`assets.Manager` 自己读取的数据，例如图集 JSON、原始字节和你自己解析的文件，来自你交给它的文件系统：用 `//go:embed` 嵌入后传入 `assets.NewManager(cx, assets.WithFS(fsys))`，或者用 `WithReadAsset` 接入自定义存储。`assets.Manager` 按场景把资源分组，在引擎 goroutine 之外加载资源组，提供可以绑定到加载界面的进度，并在场景结束时释放，这正是大多数游戏用到的那部分 Addressables 功能。见 [assets](assets.md)。

这里没有资源数据库，也没有 `.meta` 文件。文件按其工具写出的格式读取：TexturePacker 和 Aseprite 图集（[sprite](sprite.md)）、Tiled 地图（[tilemap](tilemap.md)）以及 glTF 模型（[从 Blender 导入 3D 模型](3d-models-blender.md)）。`ScriptableObject` 变成一个解码到 Go 结构体的 YAML 文件。`PlayerPrefs` 变成 `core.Storage`，存档则是 [save](save.md) 中带版本、迁移和备份的 JSON 存档槽。

## 事件

- **C# 事件和 `UnityEvent`** 变成类型化的 `flow.Bus`。事件是一个结构体类型：`flow.Subscribe(&bus, func(e EnemyDefeated) {...})` 添加监听器并返回移除它的函数，`flow.Publish` 立即触发事件，`flow.Enqueue` 配合 `Bus.Flush` 把物理过程中触发的事件留到这一步结束再投递。`SendMessage` 和以字符串为键的消息机制没有对应物。见 [flow](flow.md)。
- **协程**变成由 `flow.Runner` 在每个固定步推进的步进序列，不用 goroutine：`flow.Seq(flow.Wait(time.Second), flow.Do(spawn))` 就是 `yield return new WaitForSeconds(1)`，`flow.WaitUntil` 就是 `WaitUntil`。它们随游戏一起暂停，并以相同方式重放。`Invoke` 和 `InvokeRepeating` 变成 `flow.Timers`。
- **用于游戏逻辑的 Animator 状态机**变成 `flow.Machine`；3D 动画方面，[anim3d](anim3d.md) 提供状态机和混合空间，[tween](tween.md) 覆盖通常用 DOTween 做的事。
- **界面绑定**：HUD 显示的值是一个 [`core.Signal`](https://pkg.go.dev/github.com/nsgo-dev/nsgo/core#Signal)，读取它的视图会在它变化时重新渲染。

## Input System

`input.Actions` 值就是一个动作映射：把名字绑定到输入，每个固定步读取一次。

| Unity Input System | 这里 |
| --- | --- |
| 带绑定的动作 | `actions.Bind("jump", input.TouchRegion(rect), input.MouseButton(core.MouseButtonLeft))` |
| `action.IsPressed()` | `actions.Pressed("jump")` |
| `action.WasPressedThisFrame()` | `actions.JustPressed("jump")` |
| `action.WasReleasedThisFrame()` | `actions.JustReleased("jump")` |
| 1D 轴组合 | `actions.BindAxis("move_x", "left", "right")`，然后 `actions.Axis("move_x")` |
| On-Screen Stick 和 Button | `input.VirtualPad` |
| 保存重新绑定的覆盖设置 | 用 `actions.Save` 和 `actions.Load` 存到 `core.Storage` |

在每次更新开始时调用 `actions.Step()`。目前可以绑定的是触控区域、鼠标按钮和设备体感。键盘和手柄绑定在 [#3](https://github.com/nsgo-dev/game/issues/3) 中跟踪；在那之前，可以用 NSGo 的 `cx.OnKey` 绑定按键，在回调里调用 `actions.Trigger(name)`，让该动作在一个步内处于按下状态。见 [input](input.md)。

## 物理

一切都按循环的固定 `dt`、以固定顺序推进，所以相同的输入每次运行都得到相同的结果（见[确定性指南](determinism.md)）。

| Unity | 这里 |
| --- | --- |
| 带 `BoxCollider2D` 或 `CircleCollider2D` 的动态 `Rigidbody2D` | `physics.RigidWorld` 中的 `physics.RigidBody` |
| `PolygonCollider2D`、`CapsuleCollider2D`、`EdgeCollider2D` | 用 `NewRigidBody` 创建的 `Polygon`（凸，最多 8 个顶点）、`Capsule`、`Segment` 或 `Chain` 形状 |
| `HingeJoint2D`、`SpringJoint2D`、`DistanceJoint2D` | `RevoluteJoint`、`SpringJoint`、`DistanceJoint` |
| 运动学平台跳跃控制器 | 针对 `TileGrid` 的 `physics.MoveAndSlide`，带 `Grounded` 和贴墙标志 |
| `isTrigger` 和 `OnTriggerEnter2D` | 带 `OnEnter`、`OnStay` 和 `OnExit` 的 `physics.Trigger`，由 `World.StepTriggers` 驱动 |
| `Physics2D.Raycast`、`OverlapBox` | `World.RaycastFirst`、`World.QueryOverlap` |
| 层与碰撞矩阵 | 每个物体和触发器上的 `Layer` 与 `Mask`（`LayerMask`） |
| 带盒体、球体或胶囊体碰撞体的 `Rigidbody` | `physics3d.World` 中的 `Body` |
| `CharacterController.Move` | `physics3d.CharacterController.Move`，带坡度限制和台阶 |

见 [physics](physics.md)、[2D 刚体](physics2d-rigid.md)和 [physics3d](physics3d.md)。运动的碰撞体有盒形、圆形、球体和胶囊体。对于静态的 3D 关卡几何，`MeshCollider` 对应用 `World.AddTriMesh` 加入的 `physics3d.TriMeshCollider`，`TerrainCollider` 对应用 `World.AddHeightfield` 加入的 `physics3d.HeightfieldCollider`。

## 构建

Build Settings 和 Player Settings 变成项目根目录下的 `nsgo.yaml`：产品名称、包标识符、版本、构建号、图标以及目标平台列表。不需要安装平台模块，因为 Go 本身可以交叉编译；iOS 和签名的 macOS 构建需要装有 Xcode 的 Mac。

| Unity | 这里 |
| --- | --- |
| Play 模式 | `go tool nsgo dev`，可加 `-target web`、`ios_simulator` 或 `android` |
| Build and Run | `go tool nsgo build -run` |
| 为某个平台构建 | `go tool nsgo build -target macos,windows,linux,web,ios,android` |
| 应用商店构建（`.aab`、Mac App Store） | `go tool nsgo build -channel store` |

发布前用 `go tool nsgo check -target all` 针对每个目标平台检查游戏。见[构建与发布](shipping.md)；第一个项目从[快速开始](getting-started.md)入手。
