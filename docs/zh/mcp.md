# mcp

`game/mcp` 通过 NSGo 的 `github.com/nsgo-dev/nsgo/automation` 包，把运行中游戏的场景、图层、实体、组件、固定步长主循环控制、游戏坐标输入注入、可调参数、调试控制台、已加载资源组以及热重载能力暴露给 MCP 智能体。

NSGo 内置的 `nsgo mcp` 服务器只能看到原生视图，而游戏通常绘制在单个 `Canvas` 或 GPU 视图中。把 `mcp.Bridge` 挂载到 `core.Scope` 上后，它会通过 `automation.Register` 在引擎响应式图上注册游戏专用工具；当应用在 `go tool nsgo dev -automation` 或 `go tool nsgo test` 下运行时，`nsgo mcp` 会自动将这些工具与内置工具一并提供。在普通构建与发布构建中，`automation.Register` 为空操作，不会引入运行时协议开销。

```go
import "github.com/nsgo-dev/game/pkg/mcp"
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
	RenderStatsFunc: func() mcp.PerfRender3DStats { return mcp.PerfRender3DStats(g.lastRenderStats) },
	Assets:          g.assets,
	Resources:       g.resources,
	Shaders:         g.shaderReloader,
	Materials:       g.materialReloader,
	ReloadAsset:     g.reloadCustomAsset,
	PhysicsFunc:     func() *physics.World { return g.physics },
	ScreenshotDir:   g.screenshotDir,
})
if err != nil {
	return err
}
```

- 如果游戏在重开关卡时会替换 `*scene.Scene`、`*entities.World`、`*game.Loop`、`*debug.Console` 或 `*physics.World`，可以提供 `SceneFunc` / `WorldFunc` / `LoopFunc` / `ConsoleFunc` / `PhysicsFunc`（或调用 `bridge.SetScene` / `bridge.SetWorld` / `bridge.SetLoop` / `bridge.SetConsole` / `bridge.SetPhysics`）。
- 若要启用 `game_loop_seed`，可配置 `Random`（`*random.Stream`）、`RandomStreams`（`*random.Streams`）和/或自定义 `SetSeed` 回调；若都未配置，调用 `game_loop_seed` 会返回明确的可操作错误。
- 如果 `scene.Manager` 的控制器通过自定义 `Controller.Draw` 绘制而没有设置 `Controller.Scene`，`SceneFunc` / `Scene` 会自动作为当前场景的图层来源。
- 未提供 `EntityName` 或 `EntityLayer`（或其返回 `""`）时，若实体带有 [`sceneformat.ObjectRef`](editor.md)，`game/mcp` 会自动回退到其中的 `ID` 和 `Layer`。
- 对于 `game_screenshot`，可以提供 `CaptureFrame`（将诊断叠加层合成到已渲染的 `*image.RGBA` 上）、`DrawFrame`（录制自定义的 `core.Drawing` 绘制指令），或直接由 `game/mcp` 在 `Viewport` / `ViewportFunc`（及可选的 `ScreenshotBackground`）下渲染当前活跃的 `Scenes`/`Scene`。设置 `ScreenshotDir` 后，智能体可以传入相对的 `.png` 路径 `path` 并读取该文件；否则响应会内联返回 `png_base64`，而 MCP 客户端收到的是 JSON 文本，不是图片。

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
| `game_input_action` | `name`（必填）、`mode`（`"press"`、`"release"`、`"tap"`、`"axis"`；省略 `value` 时默认 `"tap"`，仅提供 `value` 时默认 `"axis"`）、`value`（可选，`[-1, 1]` 浮点数，`mode` 为 `"axis"` 时必填） | 向 `input.Actions` 注入动作按住、松开、单步点按或模拟量轴值，并返回该动作当前步快照状态。 |
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
| `game_perf_reset` | `{}` | 重置累积的 `debug.Overlay` 帧耗时与推送绘制统计、`debug.Profiler` 区间耗时以及 `debug.GCMonitor` 历史记录。通过 `StartRecording` 开始的分析器追踪录制会继续进行，已录制的事件也会保留。 |
| `game_assets_list` | `group`（可选）、`state`（`loaded`/`loading`/`unloaded`/`failed`）、`kind`（可选） | 按确定顺序排列的资源组列表（涵盖 `assets.Manager`、`resource.Library`、`hotreload.ShaderReloader`、`hotreload.MaterialReloader` 与自定义注册资源），包含加载状态、资源键/类型/URI、图集帧数、各组估算内存字节数及 `total_bytes`。 |
| `game_asset_reload` | `key`（资源名、URI 或相对路径）、`group`（可选）、`all`（布尔值，可选） | 就地重载匹配的单个资源（或在 `all: true` 时重载全部已加载资源），返回 `{reloaded, key, group, kind, bytes, duration_ms, revision, items, error}`；未报错但保持不变的条目会带有 `skipped`。解析或编译错误会以结构化结果返回，同时保留原先正常的资源不变。`all: true` 需要配置资源管理器、重载器或已注册资源；`Config.ReloadAsset` 只用于重载单个 key。 |
| `game_assets_reload_group` | `group`（必填） | 就地重载指定资源组内的所有已加载资源，返回逐项结果、重载数 `count`、失败数 `failed`、跳过数 `skipped`、内存字节数、耗时及错误详情，不会导致运行中的游戏崩溃。 |
| `game_screenshot` | `scene`、`width`、`height`、`format`（`"png"`）、`path`、`bounds`、`collision_shapes`、`physics`、`grid`、`grid_step`、`camera`、`labels`、`overlays`（`{bounds, collision_shapes, physics, grid, grid_step, camera, labels}`） | 将游戏画面截取为 PNG（返回 `width`、`height`、`format`、`bytes`、`png_base64` 或相对路径 `path`、`overlays_drawn` 汇总，以及软件光栅化器有遗漏时的 `skipped_ops` / `skipped_images`），可选在同一相机与视口变换下叠加像素级对齐的精灵包围盒、2D 物理碰撞形状、参考网格、相机边框和标签。 |

所有工具都会拒绝未知的参数字段。

## 确定性主循环控制

智能体可以通过暂停循环、设置确定性随机种子、步进精确次数的固定更新并立即检查实体状态来驱动可复现的测试流程：

- `game_loop_step` 在引擎 goroutine 上同步执行 `Update` 并在结束后调用一次 `Draw`，因此紧随其后的 `game_entity_inspect` 或 `game_component_get` 无需等待下一帧显示刷新即可观察到最新状态。
- 模拟游戏时间按固定步率段（`steps / rate`）累计，因此在 60 Hz 下步进 60 次时，`game_time_seconds` 与 `delta_seconds` 精确推进 `1.0` 秒，不产生整数纳秒截断误差。
- `game_loop_step` 将单次调用的步进上限限制在 `MaxLoopSteps`（`3600`，即 60 Hz 下的 1 分钟），防止意外长时间阻塞。

## 输入注入与视口坐标映射

在 `mcp.Config` 中配置 `Actions`、`Viewport` / `ViewportFunc`、`ViewSizeFunc` 或 `OnPointer`（或通过 `bridge.SetActions` / `bridge.SetViewport` 动态更新）后，智能体可以直接使用虚拟游戏坐标驱动游戏，而无需手动计算窗口黑边与缩放偏移：

- **动作与轴注入**：`game_input_action` 驱动 `Actions.Press`、`Actions.Release`、`Actions.Trigger` 和 `Actions.SetAxis`（省略 `value` 时 `mode` 默认 `"tap"`，提供 `value` 且省略 `mode` 时默认 `"axis"`，`"axis"` 模式必填 `value`）；`game_input_axis` 一次设置或清除一个或多个 `[-1, 1]` 模拟量轴。所有注入输入与物理输入一样遵循 `Actions.Step()` 步快照语义，并会在 `Actions.Reset()` 时一并清除。
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
- `Config.RenderStatsFunc` 会在 `render3d` 字段下附加来自 [`render3d.RenderStats`](render3d.md) 的 3D 裁剪、合批、状态切换与 GPU 内存明细；在未配置 2D 绘制统计来源时，还会自动填充顶层的 `draw_calls`、`batches`、`triangles` 与 `vertices`。`mcp.PerfRender3DStats` 与 `render3d.RenderStats` 字段相同，可直接用 `mcp.PerfRender3DStats(stats)` 转换；`game/mcp` 不导入 `game/render3d`，因此挂接桥接器的 2D 游戏不会链接 3D 渲染器。
- `game_perf_readout` 始终返回 `sources` 对象，标明哪些遥测来源已启用（`overlay`、`profiler`、`gc`、`memory`、`draw_stats`、`render3d`）。当游戏通过 `Overlay.NoteFrameCause` 标注某帧尖峰的原因时，`history` 条目会附带可选的 `cause` 字符串。

## 资源检查与热重载

配置 `Config.Assets`（或 `Config.AssetsFunc` / `bridge.SetAssets`）、`Config.Resources`、`Config.Shaders`、`Config.Materials` 和/或 `Config.ReloadAsset`，即可让智能体检查资源内存占用并在不重启游戏的情况下触发热重载：

- **图集就地更新**：在 `assets.Manager` 中按资源名（如 `"hero"`）、完整 URI（`"asset:sprites/hero.json"`）、相对路径（`"sprites/hero.json"`）或图片路径（`"sprites/hero.png"`）重载图集时，会直接就地更新原有的 `*sprite.Atlas` 指针并递增 `assets.Manager.Revision()`，使持有该指针的精灵与读取 `Revision()` 的画布立即刷新。
- **独立资源与图集**：若游戏在 `assets.Manager` 之外维护了独立的 `*sprite.Atlas` 或自定义资源，可通过 `bridge.RegisterAtlas(...)` 或 `bridge.RegisterAsset(mcp.CustomAsset{...})` 注册。
- **安全的错误处理**：遇到语法、Schema 或着色器编译错误时，原有可用的图集、着色器、材质或资源会保持激活状态，工具返回 `reloaded: false` 并在 `error` 字段中附带详细诊断信息。
- **跳过的资源**：音效不会就地替换；没有对应文件的图片（如程序生成的纹理）无从重载；屏幕不显示的密度对应的文件（如 2x 屏幕上的 1x 文件）即使替换也不会改变画面。这些条目会在 `skipped` 中给出原因（`assets.SkippedSound`、`assets.SkippedNoFile`、`assets.SkippedDensity`），既不计入重载成功，也不计入失败。按组重载时，组的读取器找不到文件的图片同样会被跳过；按 key 单独重载该图片则会返回读取错误。
- **不在引擎 goroutine 上读取**：`Manager.ReloadAsset` 与 `Manager.ReloadGroup` 与 `Group.Load` 一样，在其他 goroutine 上读取文件、解析图集并运行自定义加载器，完成后再在引擎 goroutine 上应用结果。工具调用会等待这些工作完成，因此自定义读取器或加载器不能等待引擎 goroutine；若调用的 context 先结束，则不会替换任何资源，工具返回该 context 的错误。

## 截图与诊断叠加层

`game_screenshot` 会渲染或捕获当前游戏画面，并在与游戏完全一致的 `screen.Viewport` 裁剪/缩放/黑边以及 `tilemap.Camera`（含视差图层与 `SpaceScreen` HUD 图层）变换下合成所请求的叠加层：

- **`bounds`**：为渲染场景中每个可见精灵绘制 1 像素青色（`#00e5ff`）包围盒边框。
- **`collision_shapes` / `physics`**：绘制来自 `Config.Physics` 的 2D 碰撞形状（矩形、多边形、线段与链物体绿色 `#22c55e`，带 `+X` 朝向刻度的圆形与胶囊体天蓝色 `#38bdf8`，触发器粉色 `#f472b6`、实心瓦片琥珀色 `#f59e0b`、单向平台瓦片紫色 `#c084fc`）以及 `Config.RigidPhysics` 中的旋转刚体，并在 `overlays_drawn` 中按 `physics_boxes`、`physics_circles`、`physics_polygons`、`physics_capsules`、`physics_lines`、`physics_triggers` 与 `physics_tiles` 计数。矩形、圆形、触发器与瓦片的配色与 `physics.DefaultDebugStyle()` 及 `mcp.DefaultOverlayStyle()` 一致。
- **`grid`**：按 `grid_step` 或 `overlays.grid_step`（默认 `32` 点）绘制世界空间参考网格。
- **`camera`**：绘制当前活跃相机的可见范围边框与中心十字线（`#facc15`）。
- **`labels`**：为精灵帧名和带标签的物理体绘制紧凑点阵文字标签。
- **`path`**：指定后通过 `os.Root` 将 PNG 写入 `Config.ScreenshotDir` 内的相对 `.png` 路径，绝对路径、`..` 和符号链接都无法离开该目录，并在响应中省略 `png_base64`。未指定时在 JSON 结果中返回 `png_base64`。MCP 客户端收到的这份结果是文本，因此需要智能体查看图片时，优先使用 `path`。
- **尺寸与像素比**：未传 `width` 和 `height` 时，截图覆盖 `game_input_pointer` 所映射的视图：依次取 `ViewSizeFunc`、已挂载的窗口、视口的虚拟尺寸、场景相机的尺寸，单位为点，每点一个像素。`CaptureFrame` 的图像被视为以显示器像素比覆盖该视图（400x225 点视图的 800x450 帧即 2x），因此视口像游戏一样按点布局，叠加层再缩放到图像上；宽高比不同的图像按每点一个像素处理。推断出的尺寸与显式尺寸受同样的上限约束（每边 `8192` 像素，总计 `4096x4096` 像素）。
- **`scene`**：用软件光栅化器单独渲染该场景，不使用 `CaptureFrame` 和 `DrawFrame`。若该场景的控制器通过自定义 `Controller.Draw` 绘制而没有 `scene.Scene`，则会拒绝。
- **部分渲染**：软件光栅化器可以绘制矩形、直线、圆弧、二次与三次曲线、填充、描边、矩形裁剪，以及像素在内存中的图像和图集（`ImageSource.Pixels` 或 `Data`，设置了 `Size` 时按图像的点读取）。它会统计未绘制的内容：`skipped_ops` 按操作类型计数（文字、圆角矩形、椭圆、回退为首个颜色的渐变、混合模式、虚线、阴影、网格），`skipped_images` 计数来自 URI、`asset:` 文件或 `Offscreen` 画布的图像。这些内容重要时请提供 `CaptureFrame`。`overlays_drawn.sprite_bounds` 统计图层 `Item.Batch` 中的精灵；`Drawable` 条目或瓦片地图绘制的精灵不会画出包围框。

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

## 把绘制渲染为 PNG

`mcp.RenderPNG(ops, width, height, background)` 用 `game_screenshot` 自行绘制画面时所用的软件光栅化器，把录制好的 `core.Drawing` 的绘制操作渲染成 PNG，并在 `SkippedOps` 和 `SkippedImages` 中报告它略去的内容（文字，以及不是以 `Pixels` 或 `Data` 形式驻留内存的图片）。编辑器的 `viewport_image` 用的就是它，因此在每个平台和无头模式下得到的画面都一样。

## 编辑器工具

上面的工具连接的是运行中的游戏。NSGo 游戏编辑器有自己的 MCP 服务器，即 `game/editor/editormcp` 包，智能体通过它读取和编辑编辑器中打开的项目：每次编辑都经过检查器、场景树和视口所用的同一批 `Editor` 方法，因此会立刻出现在场景树和视口中，用户按一次撤销即可撤回。

```go
import "github.com/nsgo-dev/game/pkg/editor/editormcp"
```

### 连接智能体

`go tool game editor .` 会在项目自己的本地端点上提供所打开的项目：一个只有当前用户能打开的 Unix 域套接字，以项目路径的哈希命名，位于用户缓存目录下的 `nsgo-game/editor` 中（设置了 `$NSGO_GAME_EDITOR_ENDPOINTS` 时位于该目录）。它从不是网络端口。编辑器打开另一个项目时，端点随之切换；在同一项目上再开一个编辑器，第一个编辑器继续提供服务，第二个会在 stderr 上说明原因。

在项目目录中运行一次 `go tool game mcp install`，即可写出 Claude Code（`.mcp.json`）与 Codex（`.codex/config.toml`）自动读取的项目级 MCP 配置：

```sh
go tool game mcp install
```

`go tool game mcp install` 是幂等的：重复运行会保留两个文件中其他不相关的 MCP 服务器或 Codex 配置，生成逐字节相同的文件，并打印每个文件是新写入还是未变更。在项目目录之外也可以传入 `-project <dir>`，或手动注册 `go tool game mcp`：

```sh
claude mcp add game-editor -- go tool game mcp
```

`go tool game mcp` 是一个基于标准输入输出的 MCP 服务器，它找到打开了该项目的编辑器，把每次工具调用、资源读取和订阅都转发给它，并将客户端名称与传输方式（例如 `Claude Code over stdio`）上报给编辑器的连接对话框与状态胶囊。它列出编辑器的工具，包括编辑器其他部分添加的工具；编辑器关闭后再次打开时，它会重新连接，因此智能体的会话比编辑器活得更久。没有编辑器打开该项目时，每次调用都返回一个错误，说明这一点以及如何打开项目：`no editor has the project open at /path/to/game: open it with go tool game editor /path/to/game and try again`。

### 工具

| 工具 | 动作 | 参数 | 作用 |
| --- | --- | --- | --- |
| `scene_read` | read | `file`、`offset`、`limit`（均可选） | 以从 1 开始编号的行读取项目中的场景、预制体、数据表或其他文本文件，或 `game.yaml`。编辑器对打开的场景或 `game.yaml` 有未保存的编辑、或待确认智能体回合在内存中暂存了改动时，该文件按编辑器当前持有的内容读取，回复会注明这一点并说明其行号与磁盘上的文件不同；否则，以及其他任何文件，都从磁盘读取，保留注释，行号与 `game_check` 报告的一致。不给 `file` 时读取打开的场景。路径必须相对于项目根目录，且不能离开项目。 |
| `scene_edit` | edit | `edits`（必填）、`file`、`label`、`why`（可选；每项编辑也可单独带 `why`） | 按顺序把编辑应用到打开的场景（或在智能体回合进行中，在内存中应用到项目中的任意场景或预制体），不写入磁盘。在活跃回合内，这些编辑归入同一个待确认回合，并把 `why` 说明记录到对应的派生变更上；在回合之外则记录为一条标签为 `label` 的普通撤销步骤。改动在保留并保存前一直不落盘。 |
| `turn_start` | edit | `prompt`、`step`（可选） | 开启一个待确认智能体回合，使随后的 `scene_edit`、`object_set_field` 与 `file_edit` 跨文件编辑合并为同一个可审查回合，并在用户保留时成为单个撤销步骤。 |
| `turn_end` | edit | `summary`、`step`（可选） | 将当前进行中的回合标记为待用户审查状态，附带可选的完成摘要（以及可选的进度步骤），并返回当前派生回合快照。 |
| `turn_status` | read | 无 | 只读返回当前智能体回合的快照：状态（`working`、`pending`、`stopped`、`kept`、`reverted`）、请求提示词、进度步骤、摘要、每个文件的增删行数（`+added`/`−removed`）以及结构化派生的变更列表（含 `kind`、`title`、`detail`、`file`、从 1 开始的 `line`、`why` 与 `reverted`）。刻意不提供用于保留或还原回合的 MCP 工具，决定权始终在用户手中。 |
| `file_edit` | edit | `file`（必填）、`yaml`（必填）、`why`、`label`（可选） | 在活跃智能体回合内校验并在内存中编辑 `game.yaml`、数据表（`data/*.yaml` / `*.table.yaml`）、动画片段（`*.anim.yaml` / `*.clip.yaml`）、预制体（`*.prefab.yaml`）或场景（`*.scene.yaml`），在用户保留并保存前不写入磁盘（强制校验符号链接包含性、“仅限本人”边界、拒绝 `.git/**` 与 2 MiB 单文件上限）。 |
| `selection_get` | read | 无 | 打开的场景、当前图层、选中的对象及其 YAML 行（行号与 `scene_read` 一致）、多选时的全部选中对象列表 `selectedIds`、文件浏览器中选中的文件、当前工具和视图模式。 |
| `viewport_image` | read | `scale`（可选，0.25 到 4）、`viewport_overlays`（可选字符串数组） | 视口当前样子的 PNG：通过编辑器视图看到的场景，以及相机框、瓦片网格、选择框与视口叠加层，尺寸为视口的点尺寸乘以 `scale`。省略 `viewport_overlays` 时使用编辑器当前启用的叠加层集合；传入叠加层 ID（`collision_shapes`、`triggers`、`connections`、`patrol_paths`、`navigation`、`camera_bounds`、`labels` 或已启用的插件叠加层 ID）时仅覆盖本次截图，传 `[]` 则不绘制任何叠加层。叠加层文字（如连接箭头上的 `"pulled → open"`）会渲染到 PNG 中；不在内存中的图片会被略去并给出数量。 |
| `game_check` | run | 无 | 运行问题面板的检查（使用编辑器组件注册表的 `assetcheck`，以及 `project.Check`，并包含编辑器中打开且未保存的文档），返回其中的可见问题，每条带文件、从 1 开始的行号、列号（可定位时）和消息。 |
| `problem_propose_fix` | read | `problem`（必填）、`explanation`（必填）、`edit`（必填） | 在克隆场景上校验针对实时问题的修复建议，计算精确的 `-`/`+` YAML 差异，并在编辑器的 Agent 面板展示审查卡片（含应用修复、定位查看、忽略与提问）。绝不自动应用修复或修改任何文件——没有任何 MCP 工具会直接应用修复。 |
| `history_list` | read | `author`（`all`、`you`、`agent`）、`limit`（1–500，默认 50）（均可选） | 读取编辑器统一的 History 时间线（按最新优先排列），包含手动编辑、智能体回合、外部 Git 重载与保存点的作者、摘要、详情、涉及文件、时间戳、变更/保留计数、未保存状态以及当前预览条目 ID。 |
| `review_status` | read | 无 | 返回当前或最近一次逐项智能体回合审核的状态：是否打开或挂起、用户请求文本、轮次状态、按文件统计摘要、每项改动的已读与撤回标记、与开发者手动编辑重叠的冲突及合并选择（`mine`、`agent`、`both`）、审核中就地手改并标注为 `"you"` 的 YAML 字段与代码行（保留后仍供第二轮读取）、当前阻塞项（包含 YAML 解析错误）以及最近一次保留/撤回结果。绝不暴露未发送草稿批注，强制遵守 `SceneAndData` 与 `GoSource` 隐私策略，且保留或撤回始终由开发者决定。 |
| `review_notes_get` | read | 无 | 读取当前智能体回合审核的轮次状态、改动列表以及作者通过 **Send N notes** 或 **Revert and redo with this note** 显式发送的批注（含 `change_id`、`file`、`object`、`field`、`kind`、`note`、`revert_first`）。未发送草稿与发送前移除的批注绝不返回。 |
| `review_redo` | edit | `changes`（必填非空数组，每项含 `change_id` 及可选 `detail`、`x`、`y`、`component`、`fields`、`file_content`、`old_value`、`new_value`、`delta`、`yaml_lines`） | 仅重做当前审核中作者留有批注的改动，在同一回合内推进至 Round 2+ 并以只读方式保留先前轮次。未加批注的改动（以及 `file_content` 中夹带的未批注项）会被原子拒绝；重做项重新标为未读且 Why 更新为 `"Redone from your note: …"`，**Keep** 仍将最新一轮作为单个撤销步骤应用。 |
| `game_playtest` | run | `file`、`run`、`count`、`seeds`、`out`、`recordings`（均可选） | 校验 `playtest.yaml`，无头运行配置好的机器人试玩并按种子录制 `.replay.yaml` 回放，写出 YAML 报告并返回每项指标的值、目标状态（`met` 或 `missed`）以及热力图坐标数据。 |
| `playtest_define` | edit | `metric`（必填）、`file`、`rationale`、`replace`（可选） | 校验提议的试玩指标，生成规范的 `playtest.yaml` 差异并作为待审查提案排队（`Server.PlaytestReviews` / `ApplyPlaytestReview` / `DismissPlaytestReview`），在审查确认前不改写磁盘上的 `playtest.yaml`。 |
| `playtest_report` | edit | `file`、`open`、`read`、`suggestions`、`watchRun`（均可选） | 打开或查看编辑器的试玩报告界面，加载 `playtest.report.yaml` 报告文件，更新 Agent 解读与可执行建议，或在播放模式中打开指定轮次（从 1 开始）已保存的 `.replay.yaml` 回放录制。 |
| `play_frozen_context` | read | `includeImage`（可选，默认 `true`） | 读取冻结的运行模式上下文：截取的视口帧 PNG（`includeImage` 为 `true` 时附上）、作者的问题、已保存或最近 60 秒滚动输入回放（版本 2 YAML）及 `windowStartFrame`、实时实体状态与改动、运行标记（含 `LastMarksThread`）、输出面板检测到的 panic 报告以及当前智能体步进日志。 |
| `play_replay_step` | run | `from_frame`、`to_frame`、`steps`（1–3600，与 `to_frame` 互斥）、`replay_file`（可选项目内 `.replay.yaml` 相对路径）、`note`、`includeImage`（均可选） | 使用已保存或滚动输入回放在 `from_frame..to_frame` 区间内步进运行模式（每次最多 3,600 帧）并始终保持暂停，更新视口智能体徽章（`"agent stepping frames A–B"`）和 Agent 标签页步进日志（`"Stepped frames A–B with game_loop_step…"`），并可选返回结果暂停帧的 PNG。 |
| `code_review_propose` | code | `files`（必填数组，元素为 `{path, content, delete?, note?}`）、`prompt`、`round`、`summary`、`failure_explanation`、`suspect_file`、`suspect_line`、`play_impact`、`open`（默认 `true`）、`run_checks`（默认 `true`） | 在不触碰磁盘的情况下，将待审 Go 文件（及可选的伴随 YAML/预制体文件）暂存到编辑器的逐文件 Go 代码审查中；若审查已打开则在同一回合内原位推进为 Round 2，并在 UI goroutine 之外针对 Go 构建覆盖层运行 `nsgo check` 与 `nsgo test`。 |
| `code_review_status` | read | 无 | 返回当前 Go 代码审查状态：轮次、待审 YAML 与 Go 文件的行数变化与确认状态、`nsgo check` / `nsgo test` 结果与失败详情、`fixRequested` 标志以及保留后仍等待确认的 Go 文件列表。 |
| `code_review_run_checks` | run | `failure_explanation`、`suspect_file`、`suspect_line`（均可选） | 在不修改磁盘文件的前提下，针对当前待审 Go 构建覆盖层重新运行 `nsgo check` 与 `nsgo test`，并返回更新后的结构化检查与测试状态。 |
| `states_write` | edit | `file`（必填）、`yaml`（必填）、`target`（可选）、`open`（可选，默认 `true`） | 用 `animclip.ParseStateMachine` 校验 `*.states.yaml` 精灵状态机（遇到未知字段、缺失目标状态或无效条件表达式时拒绝且不改动文件），将规范 YAML 写入 `file`，并可选在编辑器的状态机图面板中为 `target` 打开它。 |
| `states_test` | run | `file`（可选）、`durationSeconds`（可选，默认 `8`）、`rate`（可选，默认 `60`）、`script`（可选） | 通过 `animclip.SimulateStateMachine` 在固定步长下用脚本化输入驱动 `*.states.yaml` 状态机，返回状态切换轨迹、访问过的状态、卡死状态检查和摘要（例如 `Played 8 s of scripted input · no stuck states`）。 |
| `cutscene_write` | edit | `file`（必填）、`yaml`（必填）、`prompt`（可选）、`review`（可选）、`open`（可选，默认 `true`） | 使用 `cutscene.Parse` 严格校验 `*.cutscene.yaml` 文件（校验失败时不改动磁盘和编辑器），并在过场时间轴中打开交互式 Before/After 审查而不立即覆盖磁盘文件（仅当显式传入 `review: false` 时直接写出规范 YAML）。 |
| `cutscene_scrub` | read | `file`（可选）、`timeSeconds`（可选）、`view`（可选：`"before"` 或 `"after"`） | 按需打开 `file` 并将过场时间轴播放头拖动到 `timeSeconds`，返回该时刻的相机姿态与标签、角色位置与状态、当前激活的本地化字幕以及审查状态。 |
| `tileset_read` | read | `file`（可选） | 读取当前打开的 YAML 图块集（`*.tileset.yaml`，或相对于项目根的 `file`，未指定时回退到当前图层的 `tilesetFile`），返回规范 YAML 及结构化的瓦片规则（碰撞标志、形状、动画、地形、名称、权重与自定义属性）。 |
| `tileset_edit` | edit | `ops`（必填）、`file`、`save`（可选） | 按顺序把图块集操作（`set_tile` 设置 `name`/`solid`/`oneWay`/`flags`/`weight`/`shapes`/`animation`/`terrain`/`neighbors`，以及 `set_property`、`remove_property`、`add_terrain`、`remove_terrain`）作为一个原子撤销步骤应用到 YAML 图块集；除非传入 `save: true`，否则改动在保存前保持未保存状态。 |
| `refs_find` | read | `file`、`open`（可选） | 按场景（含实例覆盖计数）、预制体变体（`inherits`）、数据表行与 Go 代码（经 `go/parser` 查找并标记 `notRewritten` 的字符串常量）分组返回预制体、场景或项目文件的全部引用，并可选择在编辑器中打开查找用法界面。 |
| `refactor_rename` | edit | `from`、`to`（必填）、`apply`（可选） | 在编辑器中准备仅改写 YAML 的安全重命名审查，返回精确的 `-`/`+` 行差异与未改写 Go 代码警告，在用户点击 Keep 前不写盘（传入 `apply: true` 时通过 `MoveFile` 作为单个撤销步骤应用）。 |
| `tile_query` | read | `layer`、`tileLayer`、`empty`、`tile`、`tiles`、`tileName`、`tileNames`、`rect`、`minCells`、`diagonal`、`includeCells`（均可选） | 按瓦片 ID、YAML 图块集名称或空单元格查询内联瓦片地图图层中的格子及连通区域/空腔（例如 `"an 11 × 5 pocket at columns 2–12, rows 12–16"`），返回包围盒、格数、摘要与坐标。 |
| `tile_paint` | edit | `layer`、`tileLayer`、`tile`、`tileName`、`flipH`/`flipV`/`flipD`、`weighted`、`seed`、`noAdjacentDuplicates`、`rect`、`rects`、`cells`、`label`、`review`（默认 `true`） | 在内联瓦片地图图层上绘制显式格子、闭合矩形或带固定种子的确定性加权随机瓦片抽取，记录为一次可撤销编辑，并默认纳入当前按区域瓦片审查。 |
| `tile_autotile` | edit | `layer`、`tileLayer`、`terrain`、`rect`、`rects`、`cells`、`place`、`label`、`review`（默认 `true`） | 使用分层瓦片地图的 YAML 图块集地形规则（4 位正交或 8 位角门控）重绘或放置指定区域，记录为一次可撤销编辑，并默认纳入当前瓦片审查。 |
| `tile_erase` | edit | `layer`、`tileLayer`、`rect`、`rects`、`cells`、`label`、`review`（默认 `true`） | 在内联瓦片地图图层上擦除显式格子或闭合矩形，记录为一次可撤销编辑，并默认纳入当前瓦片审查。 |
| `tile_review_status` | read | `view`（可选：`"before"` 或 `"after"`）、`focus`（可选区域 ID） | 返回当前瓦片编辑审查的分组区域（`painted`、`replaced`、`erased`）、格数、包围盒、还原状态、智能体步骤及 Before/After 视图状态；可选切换视图或聚焦某个区域。 |
| `tile_review_revert_region` | edit | `id` 或 `index` | 将当前瓦片审查中的单个区域标记为已还原，并立即更新 2D 视口。 |
| `tile_review_restore_region` | edit | `id` 或 `index` | 恢复当前瓦片审查中已还原的单个区域，并立即更新 2D 视口。 |
| `tile_review_keep` | edit | 无 | 将当前瓦片审查中所有保留区域合并提交为 `History` 中的单个可撤销步骤，并关闭审查。 |
| `tile_review_revert_all` | edit | 无 | 丢弃当前瓦片审查中的所有待确认改动，还原到审查前的瓦片地图并关闭审查。 |
| `object_copy_yaml` | read | `id`（可选） | 将当前场景中的对象（本地或解析后的继承/实例对象）序列化为规范 `sceneformat` YAML，复制到编辑器剪贴板并显示复制提示。 |
| `object_paste_yaml` | edit | `yaml`、`layer`（可选） | 解析规范 `sceneformat` 对象 YAML（省略 `yaml` 时读取编辑器当前剪贴板，最大 4 MiB），分配全新的唯一 ID 并作为一个撤销步骤放置到 `layer`。 |
| `object_duplicate` | edit | `id`（可选） | 在同一图层中克隆对象（及其子项）并分配全新的唯一 ID，作为单个撤销步骤。 |
| `object_group` | edit | `ids`、`name`（可选） | 新建父分组对象并将给定对象 ID（或当前选中对象）移入其下，保持世界变换不变，作为单个撤销步骤。 |
| `object_move_under` | edit | `id`、`parent`、`listCandidates`（可选） | 将对象重新挂到 `parent` 下（`parent` 为空时移至图层顶层）并保持世界变换不变，作为单个撤销步骤；当 `listCandidates: true` 时仅列出合法候选父级而不修改场景。 |
| `object_select_same_prefab` | read | `id`（可选） | 选中当前场景中与目标对象共享同一 `prefab` 或实例化 `scene` 引用的全部对象，并返回 `selectedIds`。 |
| `object_visibility` | edit | `id`、`hidden`、`locked`（可选） | 设置本地或解析对象的会话级隐藏与/或锁定状态，不修改场景文件。 |
| `object_find_usages` | read | `id`（可选） | 在打开的项目中查找对象的实例化场景、预制体或 ID 的引用与搜索匹配（对场景/预制体路径打开查找用法界面，对普通 ID 打开搜索面板）。 |
| `release_notes_draft` | edit | `markdown`、`kept_reviews`（均可选） | 根据自上一发布标签（无标签时自初始提交）以来的 git 提交、已保留的审查摘要及 `changelog.d` 片段起草面向玩家的 Markdown 发布说明（或直接保存经校验的 `markdown`，上限 256 KiB），并仅原子写入 `game release -release-notes` 读取的同一文件。 |
| `release_review_uploads` | run | 无 | 打开构建与发布界面的 **Review uploads** 确认对话框，列出每个产物的名称、体积、目标商店（`itch.io`、`Google Play · internal testing`、`TestFlight`）与版本号。绝不启动上传：无论权限配置如何，商店上传始终需要开发者本人显式确认。 |
| `agent_turn_get` | read | `turnId`、`includeScene`、`includeImage`（可选） | 读取从 `⌘L` 就地提问框或智能体面板提交的最新（或指定）请求，返回提示词、`skipPlan` 标记、选中对象 ID 与名称、打开场景的路径与 YAML，以及捕获的视口 PNG 图像。 |
| `agent_turn_reply` | edit | `turnId`、`reply`、`status`（可选） | 更新智能体面板对话线程中指定回合的状态（`pending`、`running`、`done`、`stopped`）和/或写入智能体回复文本。 |
| `tasks_list` | read | `include_dismissed`（可选，默认 `false`） | 列出需要确认、运行中、待审查、排队中与失败五个分组中的后台智能体任务（`include_dismissed` 为 `true` 时同时包含已忽略/已完成任务），并返回当前对象持有锁与 `TaskSummary` 徽章计数（`dockBadge = needsYou + readyToReview + failed`）。 |
| `tasks_submit` | edit | `prompt`（必填）、`title`、`mode`（`"queue"` 或 `"parallel"`，默认 `"queue"`）、`scene`（可选） | 向编辑器后台任务列表提交请求：`"queue"` 排在当前运行或待审查回合之后，仅在该回合被保留或还原后启动；`"parallel"` 在并发上限内立即作为独立会话启动，并带有自己的编号叠加层。 |
| `tasks_control` | edit | `action`（必填：`"keep"`、`"revert"`、`"dismiss"`、`"stop"`、`"retry"`、`"open"`、`"focus"`、`"allow"` 或 `"deny"`）、`task_id`、`task_ids`（用于批量忽略） | 控制后台智能体任务：保留或安全还原单个回合（保留其他并行/已保留修改）、批量忽略待审查或失败任务而不还原已保留修改、停止或重试任务、聚焦或打开任务对应场景（严格限制在项目根目录内），或响应权限请求。 |
| `project_context` | read | 无 | 返回智能体每次请求前读取的共享项目上下文：`brief.yaml`（规范 YAML、解析字段、缺失栏目缺口与结构化试玩目标）与 `AGENTS.md`（纯 Markdown 项目规则与条数），以及待审阅文档变更。 |
| `brief_edit` | edit | `yaml` 或结构化字段（`pitch`、`genre`、`tags`、`platforms`、`artDirection`、`toneAndAudio`、`feel`）、`summary`、`rationale` | 严格校验提议的 `brief.yaml` 修改（未知字段带行号拒绝），并排队为可审查的 `DocReview` 差异卡片，在用户保留前不改动磁盘。 |
| `rules_edit` | edit | `markdown` 或 `appendRule`、`summary`、`rationale` | 为 `AGENTS.md` 提议整段修改或追加单条规则，作为可审查的 `DocReview` 差异卡片排队，在用户保留前不改动磁盘。 |
| `history_list` | read | `author`（`"all"`、`"you"`、`"agent"`）、`limit`（可选） | 按时间倒序列出编辑器统一的历史时间线，包含手动编辑、智能体回合与已保存检查点。 |
| `permissions_draft` | read | `request`（可选）、`rules`（必填） | 提交仅限用户审阅的权限草案卡片，默认不勾选任何规则；绝不直接修改 `permissions.yaml`。 |
| `game_delete_file` | delete | `file`（必填）、`reason`（可选） | 在**设置 › 工具**中默认关闭（启用前不会向已连接客户端公布）。启用后在“审查更改”面板中提议删除项目文件并展示行数与场景引用，在用户确认前绝不改动磁盘（拒绝仅限用户文件、符号链接、`.git/**`、`game.yaml`、已打开场景标签页及超过 1 MiB 的文件）。 |
| `datatable_review_propose` | edit | `files`（必填）、`prompt`、`checks`（可选） | 使用 `datatable.ParseDocument`（及已注册的 Go 行类型）校验一个或多个版本 1 YAML 数据表文件，并打开带旧值 → 新值、`Δ%`、单行 Revert/Restore 与可选智能体核验指标的逐行数据表审查界面，在用户点击 Keep 前不修改磁盘。 |
| `datatable_review_checks` | edit | `checks`（必填） | 在当前数据表审查中附上或刷新智能体的具名核验指标（`label`、`value`、`before`、`basis`），并在作者还原、编辑或重排记录后清除 `checksOutOfDate` 标记。 |
| `datatable_review_status` | read | `file`（可选） | 返回当前数据表审查的文件列表、保留/还原/用户编辑计数、当前文件改动记录状态、已附带的核验指标以及 `checksOutOfDate` 是否为真。 |
| `sketch_read` | read | `includeImage`、`scale` | 读取当前或已发送的视口草图标记（画笔、箭头、方框、文本），返回连续编号、精确场景坐标、标签、提示词、确认闸门状态，以及可选的叠加编号标记的视口渲染 PNG。 |
| `sketch_reading` | edit | `summary`、`items`、`assumptions` | 向编辑器提交智能体对已发送草图的复述卡片（`"I read your sketch as: …"`）与假设说明；在草图等待确认期间，`scene_edit` 及其他变更场景的工具会被拦住，直到用户点击 **Go ahead**、**Correct a mark** 或以文字回复。 |
| `plan_propose` | read | `title`、`steps`（必填）；`id`、`turnId`、`prompt`、`summary`、`estimate`、`reason`、`files`、`questions`、`wait`（可选） | 在修改任何场景或文件前提出包含澄清问题（支持预览与虚影/标题联动）、可编辑编号步骤、规划场景树节点与视口紫色虚影的结构化计划；默认等待用户批准（批准执行或直接执行）或停止后返回结构化的 `AgentPlanDecision`（含回答、调整后的数量与备注、移除步骤）。 |
| `plan_step_update` | read | `step`、`status`、`complete`（可选） | 更新已批准计划步骤的执行状态（`pending`、`running`、`done`、`failed`、`skipped`）或结束计划并将步骤完成清单附加到回合审查，同时回报用户是否已按下停止。 |

编辑器 MCP 服务器注册的每个工具都可在**设置 › 工具**中按电脑独立开关（`Editor.SetMCPToolEnabled`）：关闭后会从服务器公布的工具列表中移除（发送 `tools/list_changed`），被直接调用时返回可读的拒绝原因。**设置 › 用量与隐私**中的四项**允许离开此计算机的内容**开关（场景与数据文件、视口画面、Go 源代码、美术与音频文件）对所有已连接智能体的全部工具调用（包含符号链接解析后的真实路径）与全部资源读取（`editor://scene`、`editor://selection`、`editor://agent/turn`、`editor://brief`、`editor://rules`、`editor://context`）强制生效，只要对应内容类型已关闭即返回拒绝原因。在**设置 › 工具**中配置的其他 MCP 服务器（默认包含经由 `go tool nsgo mcp` 访问运行中游戏的 `nsgo-game`）会通过 `editormcp.InstallWithServers` 自动同步到项目的 `.mcp.json` 与 `.codex/config.toml`。

`scene_edit` 接受的编辑，每项是一个带 `op` 的对象：

| `op` | 字段 | 编辑器方法 |
| --- | --- | --- |
| `add` | `object`（写法同场景文件）、`layer`、`parent` | `PlaceObject`，有 `parent` 时再 `ReparentObject` |
| `remove` | `id` | `DeleteObject` |
| `move` | `id`、`x`、`y` | `MoveObject` |
| `set` | `id`、`fields` | 对象的各个设置方法：`id`、`name`、`x`、`y`、`z`、`rotation`、`scaleX`、`scaleY`、`alpha`、`originX`、`originY`、`flipX`、`flipY`、`sprite`、`material`、`posZ`、`rotX`、`rotY`、`rotZ`、`scaleZ`、`depth`、`mesh` |
| `set` | `id`、`component`、`fields` | 对每个字段调用 `SetComponentField` |
| `add_component` | `id`、`component`、`fields` | `AddComponent`，然后 `SetComponentField` |
| `remove_component` | `id`、`component` | `RemoveComponent` |
| `set_property`、`remove_property` | `id`、`key`、`value` | `SetProperty`、`DeleteProperty` |
| `reparent` | `id`、`parent`、`index` | `ReparentObject` |
| `move_to_layer` | `id`、`layer`、`index` | `MoveObjectToLayer` |

`add` 的对象由场景格式自己的读取器配合编辑器的组件注册表读取，因此会像打开文件时那样被拒绝。一次调用中只要有一项编辑被编辑器拒绝，所有编辑都不生效：场景、其撤销历史和磁盘上的文件都保持原样，工具错误会写出文件、编辑序号、对象、组件和字段以及修正方法，例如 `scene_edit: scenes/room.scene.yaml: edit 2 (set) on object "hero" component "body" field "sped": unknown field (registered: friction, layer, solid, speed); use a component and fields the project registers, as the message lists them. Nothing was changed.` 结构化结果以 `editormcp.EditError` 的形式包含同样的信息。

在智能体回合进行期间（从 `turn_start` 到 `turn_end`，或从编辑器右侧 Agent 面板发起请求），智能体通过 `scene_edit`、`object_set_field` 与 `file_edit` 对场景、预制体、数据表、`game.yaml` 与动画片段所做的全部编辑都会暂存在内存中并归入同一个待确认回合，绝不提前写盘。编辑器通过把每个被改动文件与回合前状态做结构化对比来派生变更（并保留工具调用附带的 `why` 说明），允许用户在待确认期间按单条变更或按整个文件随时还原与恢复；用户点击 **Keep** 时把剩余改动跨全部文件提交为一条标记为 `AuthorAgent` 的撤销步骤，点击 **Revert** 时把每个文件按字节恢复到回合前状态。`turn_status` 会如实报告派生变更以及用户已还原的条目；按设计不存在可由智能体调用的保留或还原回合 MCP 工具。在回合之外，每次 `scene_edit` 或 `object_set_field` 仍是打开场景上的单个普通撤销步骤。

当编辑器的自动保存处于失败状态时（例如目标目录只读或磁盘已满），`editormcp` 返回的每次工具结果都会追加一条警告，说明当前更改尚未写入磁盘并附上失败原因（同时在 `_meta` 中附带 `nsgo.dev/editsNotOnDisk`、`nsgo.dev/saveError`、`nsgo.dev/saveWarning`，并在 `Result` / `AutomationResult` 中填充 `SaveWarning`），直到保存成功为止，避免智能体基于未落盘的编辑继续构建。

### 变更流

服务器暴露六个资源：`editor://scene`（编辑器当前持有的打开场景 YAML）、`editor://selection`（`selection_get` 报告的选择内容 JSON）、`editor://agent/turn`（通过 `⌘L` 就地提问框或智能体面板发起的最新请求，包含 `selectionIds`、`scenePath`、`sceneYaml` 与 `skipPlan` 的 JSON 载荷，以及捕获的 `image/png` 视口二进制图像）、`editor://brief`（当前项目的 `brief.yaml` 规范 YAML）、`editor://rules`（当前项目的 `AGENTS.md` 纯 Markdown）以及 `editor://context`（与 `project_context` 一致的综合 JSON 项目上下文）。订阅它们的客户端会在每次变化时收到通知，通知的 `_meta` 说明是谁做的改动：工具调用为 `"nsgo.dev/by": "agent"`，编辑器自身的控件为 `"person"`，编辑器的修订号在 `"nsgo.dev/revision"` 下（`editor://agent/turn` 通知还会附带 `"nsgo.dev/turnId"` 与 `"nsgo.dev/skipPlan"`）。一连串变化可能合并为一条通知。

### 添加工具

工具是一个注册表，编辑器的其他部分通过 `Server.AddTool` 加入，使每个带按钮或快捷键的操作都有对应的工具。`Tool` 有名称、描述、参数的 JSON Schema、`Action`（`ActionRead`、`ActionEdit`、`ActionCreate`、`ActionDelete`、`ActionRun` 或 `ActionCode`，编辑器的权限规则据此判断）、可选的 `Target` 目标解析函数以及处理函数。处理函数运行在服务器的某个 goroutine 上，通过 `Call.Editor` 访问编辑器：它把一个函数交给编辑器的 goroutine 执行并等待完成，因此工具可以等待别处的工作，也可以返回图片。工具列表变化时会通知客户端。

### 权限规则强制执行

在执行任何修改类或运行类工具调用（除 `ActionRead` 之外的所有操作）之前，`editormcp.Server` 都会依据当前项目的 `permissions.yaml`（`editor.PermissionsFile`，文件不存在时默认使用 `Balanced` 方案）自上而下按**首条匹配生效**原则进行判定：

- **`allow`（允许）**：立即执行工具。
- **`ask`（询问）**：若配置了 `Options.OnPermissionAsk` 则调用回调，否则在 `Editor` 上记录待处理请求（`PendingPermissionRequests`）并直接返回带 `decision: "ask"` 的结构化 `*editormcp.PermissionError`，不会执行工具。选择**始终允许**（`PermissionAnswerAllowAlways` / `Editor.ApproveAlwaysPermission`）会在匹配规则之前插入一条 `allow` 规则、保存到 `permissions.yaml`，并显示带 **查看（Show）** 和 **撤销（Undo）** 按钮的 `"Always permission rule saved"` 提示条。
- **`deny`（拒绝）**：在 `Editor` 上记录已拦截步骤（`BlockedPermissions`），返回写明具体规则序号、规则摘要、方案名称及设置跳转路径（如 `"Settings › Permissions · rule 7 of 7"`）的结构化 `*editormcp.PermissionError`，绝不调用底层工具处理函数。

权限强制执行采用默认安全拒绝（fail-closed）原则：`Server.AddTool` 拒绝空白或未识别的 `Action`；当 `permissions.yaml` 解析失败时拒绝所有修改类与运行类调用直至修复；文件路径目标在规则匹配前会相对项目根规范化并解析符号链接（拒绝绝对路径、`..` 越界与符号链接逃逸，并在 macOS/Windows 上按大小写不敏感匹配）；多目标工具（`refactor_rename` 同时校验 `from`、目标路径 `to` 及 `apply: true` 时被改写的引用文件；`merge_finish` 同时校验场景编辑与 `git merge` 运行）按最严格结论（`deny > ask > allow`）生效。

权限规则仅约束编辑器的 MCP 服务器工具；外部智能体直接修改磁盘文件仍会通过编辑器的磁盘监视器提示。

```go
s := editormcp.New(ed, editormcp.Options{Run: cx.Post})
_ = s.AddTool(editormcp.Tool{
	Name:   "scene_name",
	Action: editormcp.ActionRead,
	Handler: func(ctx context.Context, c *editormcp.Call) (*editormcp.Result, error) {
		var name string
		err := c.Editor(ctx, func(ed *editor.Editor) error { name = ed.Doc.Name; return nil })
		return &editormcp.Result{Text: name}, err
	},
})
_ = s.Open()
```

`Options.Run` 把函数交给编辑器的 goroutine（窗口中的编辑器用 `cx.Post`）；不设置时，服务器在调用者的 goroutine 上逐个执行，适用于没有窗口驱动的编辑器。`Server.Connect` 在任意 MCP 传输上提供一个会话，测试中可配合 `mcp.NewInMemoryTransports` 使用。

### 为什么有自己的服务器

NSGo 的 `automation` 只在 `nsgo dev -automation` 和测试构建中注册应用工具，而 `nsgo mcp` 只能连接 `nsgo dev -automation` 运行的应用；编辑器是普通构建。应用工具的处理函数还会在图 goroutine 上同步运行（nsgo-dev/nsgo#2342），无法在不卡住编辑器的情况下等待项目检查，并且只能返回 JSON，截图只能以 base64 文本而不是图片的形式到达智能体（nsgo-dev/nsgo#2343）。编辑器的服务器使用官方 MCP Go SDK（NSGo 自己的 `nsgo mcp` 也基于它），因此工具可以在编辑器 goroutine 之外等待，`viewport_image` 返回真正的图片。对于在 `go tool nsgo dev -automation` 下运行的编辑器，`Server.RegisterAutomation` 仍会把同样的工具提供给 `nsgo mcp`，每个工具在图 goroutine 上从头运行到尾，返回 PNG 为 base64 的 `AutomationResult`。

## 编辑器 MCP 的仅限用户规则与提议删除（#1015）

在 `pkg/editor/editormcp` 中，编辑器 MCP 服务器在任何权限配置档（`Cautious`、`Balanced`、`Autonomous`）下都会强制执行两条不可绕过的信任边界：

- **仅限用户硬拒绝（`*editormcp.YouOnlyError`）**：所有非只读工具调用（包括内置工具和通过 `Server.AddTool` 注册的自定义工具）在执行处理器之前都会校验显式参数路径、派生目标（含无扩展名 `refactor_rename` 与 `release_notes_draft`）以及当前打开的文档路径，若指向 `permissions.yaml`、`.nsgo/permissions.yaml`、`tools.yaml`、`mcp.yaml`、`models.yaml`、`usage.yaml` 或 `Editor.SettingsPath`，一律拒绝。
- **`permissions_draft`**：提交一份仅限用户权限草案卡片（`Editor.SubmitPermissionsDraft`），默认不勾选任何规则，绝不直接写入 `permissions.yaml`，必须由用户手动勾选并点击应用。
- **`game_delete_file`**：直接拒绝仅限用户文件、符号链接、`.git/**`、`game.yaml`、已打开的场景标签页以及大于 1 MiB 的文件；对普通项目文件，只在审查面板中提议删除并展示行数与场景引用情况（`Editor.ProposeFileDeletion`），绝不直接删除磁盘文件。**全部保留**不会删除待确认文件；经用户确认删除后可通过 `⌘Z` 从检查点逐字节恢复。
