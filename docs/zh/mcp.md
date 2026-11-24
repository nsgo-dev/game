# mcp

`game/mcp` 通过 NSGo 的 `github.com/nsgo-dev/nsgo/automation` 包，把运行中游戏的场景、图层、实体、组件、固定步长主循环控制、游戏坐标输入注入、可调参数与调试控制台暴露给 MCP 智能体。

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
	Actions:       g.actions,
	Viewport:      &g.viewport,
	ViewSizeFunc:  g.currentViewSize,
	Console:       g.console,
})
if err != nil {
	return err
}
```

- 如果游戏在重开关卡时会替换 `*scene.Scene`、`*entities.World`、`*game.Loop` 或 `*debug.Console`，可以提供 `SceneFunc` / `WorldFunc` / `LoopFunc` / `ConsoleFunc`（或调用 `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop` / `bridge.SetConsole`）。
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
| `game_input_action` | `name`（必填）、`mode`（`"press"`、`"release"`、`"tap"`，默认 `"tap"`）、`value`（可选，`[-1, 1]` 浮点数） | 向 `input.Actions` 注入动作按住、松开、单步点按或模拟量轴值，并返回该动作当前步快照状态。 |
| `game_input_axis` | `name` + `value`、`axes` 映射（`{name: value}`）和/或 `clear`（`bool`） | 在 `input.Actions` 上设置或清除单个或多个 `[-1, 1]` 模拟量轴值。 |
| `game_input_pointer` | `x`、`y`（必填）、`action`（`"press"`、`"release"`、`"move"`、`"click"`，默认 `"click"`）、`space`（`"game"` 或 `"world"`，默认 `"game"`）、`button`（`"left"`、`"right"`、`"middle"`） | 将坐标经当前场景 `Camera`（当 `space="world"`）和 `screen.Viewport` 映射到视图坐标，转发到 `input.Actions`，执行 `Scene.HitTest` / `Scene.DispatchPress`，调用 `Config.OnPointer`，并返回游戏/视图坐标、`in_viewport` 与命中信息（`hit`、`hit_layer`、`hit_kind`、`hit_frame`、`hit_batch_index`、`hit_sprite_index`、`hit_drawable_index`、`dispatched`）。 |
| `game_input_actions_list` | `{}` | 列出 `input.Actions` 上已配置的动作和轴名称及其当前步状态（`pressed`、`just_pressed`、`just_released`、`binding_count`、`value`、`negative`、`positive`）。 |
| `game_tunables_list` | `prefix`（可选） | 按名称排序的已注册 `debug.Console` 可调参数列表，包含 `type`、当前 `value`、注册时的 `default` 以及数值型的 `min`/`max` 范围。 |
| `game_tunable_get` | `name`（必填） | 单个已注册可调参数的 `name`、`type`、当前 `value`、注册时的 `default` 以及数值型的 `min`/`max` 范围。 |
| `game_tunable_set` | `name`（必填）、`value`（必填）、`clamp`（可选，默认 `false`） | 按严格类型检查与范围校验（或 `clamp: true` 时钳位）更新已注册的可调参数（`float`、`int`、`bool` 或 `string`），返回 `old_value`、生效后的 `value`、元数据与 `clamped` 标记。 |
| `game_console_commands_list` | `include_builtin`（可选，默认 `true`） | 按名称排序的 `debug.Console` 内置命令与自定义命令列表，包含 `help` 说明与 `builtin` 标记。 |
| `game_console_exec` | `command`（必填） | 通过 `debug.Console` 执行一条命令行，返回 `command`、`ok`、`output`、`lines` 以及命令失败时的 `error`。 |

所有工具都会拒绝未知的参数字段。

## 确定性主循环控制

智能体可以通过暂停循环、设置确定性随机种子、步进精确次数的固定更新并立即检查实体状态来驱动可复现的测试流程：

- `game_loop_step` 在引擎 goroutine 上同步执行 `Update` 并在结束后调用一次 `Draw`，因此紧随其后的 `game_entity_inspect` 或 `game_component_get` 无需等待下一帧显示刷新即可观察到最新状态。
- 模拟游戏时间按固定步率段（`steps / rate`）累计，因此在 60 Hz 下步进 60 次时，`game_time_seconds` 与 `delta_seconds` 精确推进 `1.0` 秒，不产生整数纳秒截断误差。
- `game_loop_step` 将单次调用的步进上限限制在 `MaxLoopSteps`（`3600`，即 60 Hz 下的 1 分钟），防止意外长时间阻塞。

## 输入注入与视口坐标映射

在 `mcp.Config` 中配置 `Actions`、`Viewport` / `ViewportFunc`、`ViewSizeFunc` 或 `OnPointer`（或通过 `bridge.SetActions` / `bridge.SetViewport` 动态更新）后，智能体可以直接使用虚拟游戏坐标驱动游戏，而无需手动计算窗口黑边与缩放偏移：

- **动作与轴注入**：`game_input_action` 驱动 `Actions.Press`、`Actions.Release`、`Actions.Trigger` 和 `Actions.SetAxis`；`game_input_axis` 一次设置或清除一个或多个 `[-1, 1]` 模拟量轴。所有注入输入与物理输入一样遵循 `Actions.Step()` 步快照语义，并会在 `Actions.Reset()` 时一并清除。
- **指针坐标管线**：`game_input_pointer` 接收 `"game"` 空间（虚拟画布坐标）或 `"world"` 空间（经当前场景 `Camera.WorldToView` 转换到游戏空间）的 `(x, y)`，随后结合 `ViewSizeFunc`（回退到挂载作用域的 `Env().Window()`，再回退到视口虚拟尺寸）通过 `screen.Viewport.GameToView` 映射到视图坐标。
- **事件派发**：映射后的视图坐标会转发到 `input.Actions`（因此 `Actions.MapPoint = viewport.ViewToGame` 会准确还原游戏坐标并触发 `MouseRegion` 绑定）。当 `in_viewport` 为真时，会在游戏坐标下执行 `Scene.HitTest` 与主按钮 `Scene.DispatchPress`，确保在精灵游戏位置按下即可命中该精灵并触发其 `OnPress` 回调；`Config.OnPointer` 会收到每次注入的 `PointerEvent`（`"click"` 依次派发 `PointerPress` 和 `PointerRelease`）。

## 可调参数与调试控制台

通过 `Config.Console`、`Config.ConsoleFunc` 或 `bridge.SetConsole` 传入 [`*debug.Console`](debug.md)，即可让智能体检查和调整运行中的游戏参数并执行自定义调试命令：

```go
console := debug.NewConsole()
console.RegisterFloat("physics.gravity", 0, 2000, &g.gravity)
console.RegisterInt("player.max_jumps", 1, 5, &g.maxJumps)
console.RegisterBool("player.god_mode", &g.godMode)
console.RegisterString("level.name", &g.levelName)
console.RegisterCommand("spawn", "spawn <kind> [count]", g.handleSpawn)
```

- `game_tunables_list` 与 `game_tunable_get` 直接读取实时值，不会向控制台交互历史或输出日志中写入多余记录。
- `game_tunable_set` 默认严格校验参数注册类型（`float`、`int`、`bool` 或 `string`）及数值 `[min, max]` 范围，校验失败时不修改变量。传入 `"clamp": true` 时会将越界数值钳位到 `[min, max]` 并在响应中标记 `"clamped": true`。
- `game_console_exec` 通过 `debug.Console.Exec` 运行内置命令（`help`、`list`、`get`、`set`、`clear`）与游戏自定义命令，返回 `{command, ok, output, lines, error}`。无效的 MCP 参数或未配置 `Console` 会返回工具级错误；命令本身的执行失败（例如未知命令名或参数无效）则返回 `ok: false` 与对应的 `error` 信息。
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
