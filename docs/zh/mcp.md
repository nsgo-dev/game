# mcp

`game/mcp` 通过 NSGo 的 `github.com/nsgo-dev/nsgo/automation` 包，把运行中游戏的场景、图层、实体、组件以及固定步长主循环控制暴露给 MCP 智能体。

NSGo 内置的 `nsgo mcp` 服务器只能看到原生视图，而游戏通常绘制在单个 `Canvas` 或 GPU 视图中。把 `mcp.Bridge` 挂载到 `core.Scope` 上后，它会通过 `automation.Register` 在引擎响应式图上注册游戏专用工具；当应用在 `go tool nsgo dev -automation` 或 `go tool nsgo test` 下运行时，`nsgo mcp` 会自动将这些工具与内置工具一并提供。在普通构建与发布构建中，`automation.Register` 为空操作，不会引入运行时协议开销。

```go
import "github.com/nsgo-dev/game/packages/game/mcp"
```

## 挂载桥接器

在引擎 goroutine 上调用 `mcp.Attach`（或先调用 `mcp.New` 再调用 `bridge.Attach`），传入希望智能体检查的子系统。`mcp.Config` 的每个字段都是可选的；注册的工具会一直有效，直到 `cx` 被销毁或调用 `bridge.Detach()`。

```go
bridge, err := mcp.Attach(cx, mcp.Config{
	Scenes:        g.scenes,
	SceneName:     func(c *scene.Controller) string { return g.sceneNames[c] },
	SceneFunc:     func() *scene.Scene { return g.activeScene },
	NamedScenes:   map[string]*scene.Scene{"title": g.titleScene},
	WorldFunc:     func() *entities.World { return g.world },
	EntityName:    func(e entities.Entity) string { return g.names[e] },
	EntityTags:    func(e entities.Entity) []string { return g.tags[e] },
	EntityLayer:   func(e entities.Entity) string { return g.layers[e] },
	Loop:          g.loop,
	RandomStreams: g.rng,
})
if err != nil {
	return err
}
```

- 如果游戏在重开关卡时会替换 `*scene.Scene`、`*entities.World` 或 `*game.Loop`，可以提供 `SceneFunc` / `WorldFunc` / `LoopFunc`（或调用 `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop`）。
- 若要启用 `game_loop_seed`，可配置 `Random`（`*random.Stream`）、`RandomStreams`（`*random.Streams`）和/或自定义 `SetSeed` 回调；若都未配置，调用 `game_loop_seed` 会返回明确的可操作错误。
- 如果 `scene.Manager` 的控制器通过自定义 `Controller.Draw` 绘制而没有设置 `Controller.Scene`，`SceneFunc` / `Scene` 会自动作为当前场景的图层来源。
- 未提供 `EntityName` 或 `EntityLayer`（或其返回 `""`）时，若实体带有 [`sceneformat.ObjectRef`](editor.md)，`game/mcp` 会自动回退到其中的 `ID` 和 `Layer`。

## 检查组件

已在 [`entities.World`](entities.md) 上通过 `entities.RegisterCodec` 注册的组件会自动可读。此外：

- 内置的 `entities.Transform2D`、`entities.WorldTransform2D` 和 `sceneformat.ObjectRef` 组件在未被同类型或同名注册覆盖时，会自动以 `"transform2d"`、`"world_transform2d"` 和 `"object_ref"` 的名称暴露。
- 未写入存档的瞬态运行时组件可以通过 `mcp.RegisterComponent[T](bridge, name)` 注册，或使用 `mcp.RegisterComponentFunc(bridge, name, read)` 提供自定义读取器。
- 组件值默认用 `encoding/json` 序列化；若运行时值包含非有限浮点数（`NaN`、`+Inf`、`-Inf`）、回调函数字段、channel 或循环指针，`game/mcp` 会回退到安全的清洗编码（`"NaN"`、`"+Inf"`、`"-Inf"`、`"<func>"`、`"<chan>"`、`"<cycle>"`），使智能体在状态异常时仍能诊断问题。

```go
if err := mcp.RegisterComponent[AIState](bridge, "ai"); err != nil {
	return err
}
```

## 已注册的工具

| 工具 | 参数 | 返回内容 |
| --- | --- | --- |
| `game_scenes_list` | `{}` | 活跃场景栈（从栈底到栈顶）、`current` 当前场景名、`stack_depth`、过渡状态与 `progress`、图层数量、相机状态以及未激活的 `NamedScenes`。 |
| `game_layers_list` | `scene`（可选）、`include_hidden`（可选，默认 `true`） | 指定场景（默认当前活跃场景）按绘制顺序排列的图层列表（`draw_index`、切片 `index`、`name`、`space`、`visible`、`order`、实际生效的 `parallax_x`/`parallax_y`、偏移、平铺周期、`depth_sort`，以及 item/batch/sprite/tilemap/drawable 计数）。 |
| `game_entities_list` | `component`、`layer`、`name`（子串）、`tag`、`parent`（引用或 `"root"`）、`include_components`（默认 `false`）、`offset`、`limit` | 按槽位升序排列的存活实体列表，包含 `total`、`returned`、稳定引用 `ref`、`index`、`generation`、元数据、`parent`、`child_count`、排序后的 `components` 名称列表，以及可选内联 `values`。 |
| `game_entity_inspect` | `ref`（必填） | 单个存活实体的完整信息：`ref`、`index`、`generation`、元数据、`parent`、`children` 引用，以及按组件名映射的全部可读组件值。 |
| `game_component_get` | `ref`（必填）、`component`（必填） | 单个存活实体上指定组件的序列化 `value`。 |
| `game_loop_status` | `{}` | 当前循环状态：`paused`、实际生效的 `time_scale`、`step_seconds`、`step_hz`、累计 `steps`、`game_time_seconds` 以及 `seed_configured`。 |
| `game_loop_pause` | `{}` | 暂停主循环（`Loop.SetPaused(true)`），不累积追赶时间，并返回更新后的循环状态。 |
| `game_loop_resume` | `{}` | 恢复主循环（`Loop.SetPaused(false)`），并返回更新后的循环状态。 |
| `game_loop_step` | `count`（可选，默认 `1`，范围 `1..3600`） | 无论主循环是否暂停，立即同步推进 `count` 次固定步长更新（`Loop.StepNow`），返回 `steps_advanced`、`total_steps`、`game_time_seconds`、`delta_seconds` 与 `paused`。在 60 Hz 下步进 60 次会精确推进 `1.0` 秒游戏时间。 |
| `game_loop_time_scale` | `time_scale`（必填，有限且 `>= 0`） | 设置 `Loop.TimeScale`（传 `0` 重置为默认 `1x` 倍速），并返回更新后的循环状态。 |
| `game_loop_seed` | `seed`（必填，无符号 64 位整数） | 为 `Config.Random`、`Config.RandomStreams` 重新设种，并/或调用 `Config.SetSeed`，返回 `{seed}`。 |

所有工具都会拒绝未知的参数字段。

## 确定性主循环控制

智能体可以通过暂停循环、设置确定性随机种子、步进精确次数的固定更新并立即检查实体状态来驱动可复现的测试流程：

- `game_loop_step` 在引擎 goroutine 上同步执行 `Update` 并在结束后调用一次 `Draw`，因此紧随其后的 `game_entity_inspect` 或 `game_component_get` 无需等待下一帧显示刷新即可观察到最新状态。
- 模拟游戏时间按固定步率段（`steps / rate`）累计，因此在 60 Hz 下步进 60 次时，`game_time_seconds` 与 `delta_seconds` 精确推进 `1.0` 秒，不产生整数纳秒截断误差。
- `game_loop_step` 将单次调用的步进上限限制在 `MaxLoopSteps`（`3600`，即 60 Hz 下的 1 分钟），防止意外长时间阻塞。

## 稳定的实体引用

每个存活实体都会获得一个格式为 `"entity:<index>:<generation>"`（例如 `"entity:0:1"`）的代数引用，由 `FormatEntityRef` 格式化并由 `ParseEntityRef` 解析：

- 只要实体保持存活，即使经过多次模拟步进以及周围其他实体的生成与销毁，它的 `ref` 始终保持不变。
- 实体被销毁且其槽位随后被复用时，新实体会获得递增的代数（例如 `"entity:0:2"`）。使用过期的 `"entity:0:1"` 调用 `game_entity_inspect` 或 `game_component_get` 会明确返回 `"not alive"` 错误，绝不会静默读到新实体。

## 使用 `nsgotest` 测试

在测试中，于 `nsgotest.Start` 内挂载桥接器，并在 `app.Post` 内通过 `automation.Call` 调用工具：

```go
var raw json.RawMessage
var callErr error
app.Post(func() {
	raw, callErr = automation.Call(cx, t.Context(), mcp.ToolEntitiesList, nil)
})
app.Settle()
```
