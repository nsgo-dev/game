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
	Scenes:          g.scenes,
	SceneName:       func(c *scene.Controller) string { return g.sceneNames[c] },
	SceneFunc:       func() *scene.Scene { return g.activeScene },
	NamedScenes:     map[string]*scene.Scene{"title": g.titleScene},
	WorldFunc:       func() *entities.World { return g.world },
	EntityName:      func(e entities.Entity) string { return g.names[e] },
	EntityTags:      func(e entities.Entity) []string { return g.tags[e] },
	EntityLayer:     func(e entities.Entity) string { return g.layers[e] },
	Loop:            g.loop,
	RandomStreams:   g.rng,
	Actions:         g.actions,
	Viewport:        &g.viewport,
	ViewSizeFunc:    g.currentViewSize,
	Console:         g.console,
	Overlay:         g.debugOverlay,
	RenderStatsFunc: func() render3d.RenderStats { return g.lastRenderStats },
})
if err != nil {
	return err
}
```

- 如果游戏在重开关卡时会替换 `*scene.Scene`、`*entities.World`、`*game.Loop` 或 `*debug.Console`，可以提供 `SceneFunc` / `WorldFunc` / `LoopFunc` / `ConsoleFunc`（或调用 `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop` / `bridge.SetConsole`）。
- 若要启用 `game_loop_seed`，可配置 `Random`（`*random.Stream`）、`RandomStreams`（`*random.Streams`）和/或自定义 `SetSeed` 回调；若都未配置，调用 `game_loop_seed` 会返回明确的可操作错误。
- 如果 `scene.Manager` 的控制器通过自定义 `Controller.Draw` 绘制而没有设置 `Controller.Scene`，`SceneFunc` / `Scene` 会自动作为当前场景的图层来源。
- 未提供 `EntityName` 或 `EntityLayer`（或其返回 `""`）时，若实体带有 [`sceneformat.ObjectRef`](editor.md)，`game/mcp` 会自动回退到其中的 `ID` 和 `Layer`。

## 检查与编辑组件

已在 [`entities.World`](entities.md) 上通过 `entities.RegisterCodec` 注册的组件会自动支持读取和编辑。此外：

- 内置的 `entities.Transform2D`、`entities.WorldTransform2D` 和 `sceneformat.ObjectRef` 组件在未被同类型或同名注册覆盖时，会自动以 `"transform2d"`、`"world_transform2d"` 和 `"object_ref"` 的名称暴露。其中 `"transform2d"` 和 `"object_ref"` 可直接编辑（编辑 `"transform2d"` 会自动传播 `Hierarchy2D` 脏标记）；`"world_transform2d"` 由层级派生，为只读组件。
- 未写入存档的瞬态运行时组件可以通过 `mcp.RegisterComponent[T](bridge, name)` 或 `mcp.RegisterComponentFunc(bridge, name, read)` 只读暴露，也可以通过 `mcp.RegisterMutableComponent[T](bridge, name)` 或 `mcp.RegisterComponentEditor[T](bridge, name, apply)` 同时支持读取与编辑。`RegisterComponentEditor` 可以为此前同类型的 `RegisterComponent[T]` 注册补上编辑能力，但不能用于 `RegisterComponentFunc` 读取器，因为后者返回的值不一定以 `T` 存储。
- `game_component_set` 使用 `DisallowUnknownFields` 严格解码，拒绝非有限浮点数（`NaN`、`±Inf`）及非法 `null`，默认以补丁模式更新现有字段（保留未传入字段，传入 `"replace": true` 时从零值整块替换），且解码失败时不会留下半修改状态。补丁不会改动它没有修改的字段，包括未导出字段、带 `json:"-"` 标签的字段（如回调和句柄），以及指针、map 和切片的同一性；被修改的字段会得到新分配的值，因此解码出错或编辑器拒绝该值时，存活组件保持原样。对于 codec 组件，解码结果若无法重新编码（例如自定义 `UnmarshalJSON` 产生的 `NaN`），会在写入前被拒绝。
- 组件值默认用 `encoding/json` 序列化；若运行时值包含非有限浮点数（`NaN`、`+Inf`、`-Inf`）、回调函数字段、channel 或循环指针，`game/mcp` 会回退到安全的清洗编码（`"NaN"`、`"+Inf"`、`"-Inf"`、`"<func>"`、`"<chan>"`、`"<cycle>"`），使智能体在状态异常时仍能诊断问题。

```go
if err := mcp.RegisterMutableComponent[AIState](bridge, "ai"); err != nil {
	return err
}
if err := mcp.RegisterComponentEditor[Health](bridge, "health", func(w *entities.World, e entities.Entity, h Health) error {
	if h.HP < 0 {
		return errors.New("hp cannot be negative")
	}
	entities.Set(w, e, h)
	return nil
}); err != nil {
	return err
}
```

## 注册预制体

可以在 `mcp.Config.Prefabs` 或 `*mcp.Bridge` 上注册预制体，供智能体通过 `game_prefabs_list` 发现并用 `game_entity_spawn` 生成：

- `bridge.RegisterPrefab(name, spawn)`（或 `Config.Prefabs`）注册自定义 `mcp.PrefabSpawner`（`func(w *entities.World, overrides map[string]any) (entities.Entity, error)`）。
- `bridge.RegisterEntityPrefab(p)` 按 `p.Name()` 注册 [`*entities.Prefab`](entities.md)。
- `bridge.RegisterScenePrefab(name, doc)` 注册经过校验且自包含的 [`*sceneformat.SceneDoc`](editor.md) 模板，并通过 `sceneformat.SpawnEntities` 实例化。文档必须恰好有一个顶层对象作为生成的根实体，其余对象都应嵌套在它下面。每次生成都会原样附加文档中的 `sceneformat.ObjectRef` ID，因此多次生成的实体拥有相同的对象 ID；除非 `EntityName` 另有返回，`game_entities_list` 也会把它显示为实体名称。

```go
if err := bridge.RegisterEntityPrefab(coinPrefab); err != nil {
	return err
}
if err := bridge.RegisterScenePrefab("turret", turretDoc); err != nil {
	return err
}
```

如果在 `game_entity_spawn` 生成实体后，应用 `parent` 或 `components`、或回读新实体的组件时发生错误，新生成的实体（以及场景预制体的子实体）会自动回滚销毁。`x` 和 `y` 在生成任何实体之前就已校验。

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
| `game_prefabs_list` | `{}` | 已注册预制体的排序列表（`name` 与 `kind`：`"custom"`、`"entity_prefab"` 或 `"scene"`）。 |
| `game_entity_spawn` | `prefab`、`overrides`、`parent`、`x`、`y`、`components`（均为可选） | 新生成存活实体的稳定引用 `ref`、`index`、`generation`、`prefab`、元数据、`parent`、`children` 及初始 `components` 快照。 |
| `game_entity_destroy` | `ref`（必填）、`policy`（可选：`"cascade"`、`"orphan"`、`"keep_world"` 或 `"reparent"`） | 销毁结果，包含 `ref`、应用于子实体的 `policy`（省略时为层级的默认策略，通常是 `"orphan"`；省略且实体没有子实体时为 `"none"`）与实际销毁实体数 `destroyed`。`"reparent"` 会保持子实体的世界变换。 |
| `game_component_set` | `ref`（必填）、`component`（必填）、`value`（必填）、`replace`（可选，默认 `false`） | 回读自存活实体的更新后组件 `value`；修改会在下一次模拟步进中生效。若写入成功但回读失败，`value` 为 `null`，`read_error` 说明原因，修改依然有效。 |
| `game_component_remove` | `ref`（必填）、`component`（必填） | 报告指定组件是否存在并已从存活实体移除（`removed`）。 |
| `game_replay_status` | `{}` | 当前录制与回放状态（`recording`、`replaying`、`replay_done`、`frames_recorded`、`replay_frame`、`replay_total_frames`）、当前录制/回放名称与元数据，以及排序后的 `saved_recordings`。 |
| `game_replay_record_start` | `name`、`seed`、`actions`、`axes`、`metadata`、`reset`（均可选；配置了 `ResetFunc` 或种子目标时 `reset` 默认 `true`） | 按 `seed` 重置（见下文），然后通过 `replay.Recorder` 以游戏的固定步频开始逐步录制输入，并返回解析后的录制名称、种子、步频、监听列表与元数据。 |
| `game_replay_record_stop` | `name`（可选）、`save_path`（可选，相对于 `ReplayConfig.SaveDir`）、`overwrite`（默认 `false`） | 停止当前录制，将其保存到内存（并可选写入 `SaveDir`），返回摘要与确定性的 v2 YAML 文本（`yaml`）。同名的已存录制或已存在的文件会被拒绝（除非 `overwrite: true`），录制继续进行。 |
| `game_replay_play` | `name` 或 `yaml`（二选一）、`run_to_end`（默认 `false`）、`steps`（`1..3600`）、`reset`（配置了 `ResetFunc` 或种子目标时默认 `true`） | 按录制的种子重置，然后通过 `replay.Player` 开始确定性回放，并可选通过 `StepFunc` 或游戏循环的 `StepNow` 同步推进全部帧（`run_to_end`，录制不超过 `3600` 步）或 `steps` 帧。`rate` 与游戏固定步频不同的录制会被拒绝。 |
| `game_replay_stop` | `{}` | 安全停止当前正在进行的回放，并报告已回放的帧数。 |
| `game_replay_list` | `{}` | 按名称排序列出已存储的录制及其 `frames`、`seed`、`step_rate`、`events`、`actions`、`axes` 与 `metadata`。 |
| `game_perf_readout` | `include_history`（可选，默认 `false`）、`history_limit`（可选）、`include_zones`（可选，默认 `false`）、`zones_limit`（可选） | 与调试浮层数值一致的实时性能读数：`fps`、滑动平均与最近一帧的 `frame_ms`/`update_ms`/`draw_ms`、最小/最大及 p50/p95/p99 帧时间、绘制统计（`draw_calls`、`sprites`、`batches`、`triangles`、`vertices`）、每帧堆分配（`allocs_per_frame`、`alloc_bytes_per_frame`、`last_frame_allocs`、`last_frame_alloc_bytes`）、`heap_bytes`、`gc_count`、GC 停顿与卡顿统计、可选的 `render3d` 与 `asset_memory` 明细，以及可选的时间序 `history` 与分析器 `zones`。 |
| `game_perf_zones` | `limit`（可选） | 按最近帧耗时降序排列的 `debug.Profiler` 命名计时区间列表（`name`、`last_ms`、`avg_ms`、`max_ms`、`calls`），以及 `recording`、`recorded_frames` 和 `total`。 |
| `game_perf_reset` | `{}` | 重置累积的 `debug.Overlay` 帧耗时与推送绘制统计、`debug.Profiler` 区间以及 `debug.GCMonitor` 历史记录。 |

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
## 录制与回放输入

使用 `mcp.NewReplayController` 创建 `*mcp.ReplayController`，将其作为游戏的 [`replay.ActionSource`](replay.md) 传入更新循环，并挂到 `mcp.Config.Replay`：

```go
var ctrl *mcp.ReplayController
ctrl = mcp.NewReplayController(mcp.ReplayConfig{
	Source:   g.actions,
	Actions:  []string{"left", "right", "jump", "dash"},
	Axes:     []string{"move_x"},
	StepRate: 60,
	SeedFunc: func() uint64 { return g.seed },
	ResetFunc: func(seed uint64) error {
		g.actions.Reset()
		g.Reset(seed)
		return nil
	},
	StepFunc: func(dt time.Duration) {
		g.Step(ctrl, dt)
	},
})

bridge, err := mcp.Attach(cx, mcp.Config{
	WorldFunc: func() *entities.World { return g.world },
	Replay:    ctrl,
})
```

- `ReplayController` 实现了 `replay.ActionSource`，游戏逻辑在实时游玩（转发至 `Source`）、录制（`replay.Recorder`）与回放（`replay.Player`）三种模式下完全共用同一套更新循环。`Source` 为必填项。回放期间它仍会被步进，因此恢复实时输入时它的上一步快照是最新的。
- 录制与回放共用同一个重置：先用种子重新设定桥接器的种子目标（`Config.Random`、`Config.RandomStreams`、`Config.SetSeed`，与 `game_loop_seed` 使用的相同），再调用 `ResetFunc(seed)`。只要有可重置的内容，两个工具的 `reset` 都默认为 `true`，因此在游戏中途以默认参数开始的录制也能回放到相同状态。向 `game_replay_record_start` 传 `reset: false` 会从当前状态开始录制，这样的录制只有从同一状态出发才能复现。
- 录制的 `rate` 就是游戏的固定步频：桥接器有游戏循环（`Config.Loop` / `Config.LoopFunc`）时取其 `StepRate()`，否则取 `ReplayConfig.StepRate`（默认 `60`）。`game_replay_play` 会拒绝以其他步频录制的录制，因为每一步的 `dt` 都会不同。
- 通过 `game_replay_record_stop` 结束的录制会按名称存入内存，并返回标准、确定性的 [`game/replay`](replay.md) v2 YAML 文本。若希望智能体同时通过 `save_path` 落盘，可配置 `ReplayConfig.SaveDir`；写入经由 `os.Root` 进行，绝对路径、`..` 与符号链接都无法离开 `SaveDir`。`metadata` 只保存在内存存储中，不属于 YAML，因此保存的文件或内联 `yaml` 回放都不带它。
- `game_replay_play` 支持按已存 `name` 或内联 `yaml` 文本回放，可同步回放（`run_to_end: true` 或 `steps: N`），也可随游戏循环逐帧回放。未设置 `StepFunc` 时，同步回放通过 `Loop.StepNow` 推进桥接器的游戏循环，因此 `game_loop_status` 会计入回放的步数与游戏时间。与 `game_loop_step` 相同，单次调用最多推进 `MaxLoopSteps`（`3600`）步。
- 最后一个录制步之后，下一次 `ctrl.Step()` 是一个收尾回放步，会释放录制结束时仍按住的动作（触发一次 `JustReleased`）；再下一步才恢复实时输入并调用 `OnReplayStop`。

## 性能读数

通过 `Config.Overlay` 传入 [`debug.Overlay`](debug.md)，即可把调试 HUD 显示的帧耗时、绘制统计（`debug.Options.DrawStats` 或 `Overlay.SetDrawStats`）、GC 与每帧分配计数（`debug.GCMonitor`）、资源内存（`debug.MemoryTracker`）以及命名计时区间（`debug.Profiler`）以完全一致的数值暴露给智能体。

- `Config.Profiler`、`Config.GC`、`Config.Memory` 与 `Config.DrawStatsFunc` 既可覆盖 `Config.Overlay` 上的对应组件，也可在不启用浮层时单独使用。
- `Config.RenderStatsFunc` 会在 `render3d` 字段下附加来自 [`render3d.RenderStats`](render3d.md) 的 3D 裁剪、合批、状态切换与 GPU 内存明细；在未配置 2D 绘制统计来源时，还会自动填充顶层的 `draw_calls`、`batches`、`triangles` 与 `vertices`。
- `game_perf_readout` 始终返回 `sources` 对象，标明哪些遥测来源已启用（`overlay`、`profiler`、`gc`、`memory`、`draw_stats`、`render3d`）。

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
