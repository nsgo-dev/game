# gamedebug

`game/pkg/tooling/gamedebug` 为 NSGo 游戏提供开发期调试浮层：快捷键开关的帧耗时柱状图、实时场景/图层检查器，以及世界空间碰撞体与精灵包围盒线框；使用 `-tags release` 构建时全部裁剪为零开销空操作。

它与 NSGo DevTools（`nsgo dev -devtools`）无关：后者是 NSGo 开发服务器为任意 NSGo 应用提供的浏览器页面，本包则绘制在游戏自己的窗口里。

```go
import "github.com/nsgo-dev/game/pkg/tooling/gamedebug"
```

## 接入调试浮层与帧耗时图

通过 `gamedebug.New` 创建 `*Overlay`，用 `WrapLoop` 包装 `game.Loop`，并把 `overlay.View(cx)` 叠放在游戏画布上方：

```go
overlay := gamedebug.New(gamedebug.Options{
	ToggleKey: "F3", // 为空时默认 "F3"
	TargetFPS: 60,   // 帧耗时图的参考线
	LineWidth: 1,
	Scenes: func() []gamedebug.SceneSnapshot {
		return []gamedebug.SceneSnapshot{
			{
				Name: "world",
				Layers: []gamedebug.LayerSnapshot{
					{Name: "terrain", Space: "world", Objects: 1},
					{Name: "actors", Space: "world", Objects: len(actorsBatch.Sprites)},
					{Name: "hud", Space: "screen", Objects: 3},
				},
			},
		}
	},
})

overlay.WrapLoop(loop)
loop.Run(cx)

return core.View{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1},
	Children: []core.Node{
		gameCanvas,
		overlay.View(cx), // 在 -tags release 下返回 nil
	},
}
```

- **`WrapLoop(l)`** 会包装 `l.Update` 和 `l.Draw`，自动计时并调用 `RecordFrame(total, update, draw)` 写入固定容量环形缓冲区 `FrameStats`（`FrameHistory = 120` 帧，每帧零堆分配）。
- **`overlay.Stats()`** 暴露 `FPS()`、`Average()`、`Latest()`、`MaxTotal()`、`Len()`、`At(i)`、`Samples(dst)`、`Reset()` 及 `DrawGraph(d, size, targetFPS)`，方便在其他位置复用统计数据。
- **显示切换**：按下 `ToggleKey`（默认 `"F3"`）或调用 `overlay.Toggle()` / `overlay.SetVisible(true)`。在响应式视图或 `Canvas.Draw` 中读取 `overlay.Visible()` 会自动订阅开关状态。

## 绘制碰撞体与精灵包围盒线框

在 `core.Canvas.Draw` 中保持相机/视口变换生效时绘制调试线框。`ShapeOverlay` 会自动将 `LineWidth` 除以当前绘制变换的缩放比例，确保任意相机缩放下线宽始终均匀：

```go
d.Save()
cam.Apply(d)
worldScene.Draw(d)

// 仅在浮层可见时执行，并自动让画布订阅开关切换
overlay.DrawShapes(d, func(s *gamedebug.ShapeOverlay) {
	s.DrawBox(d, playerBounds, core.RGB(0x22c55e))
	s.DrawCircle(d, aggroX, aggroY, aggroRadius, core.RGB(0xf59e0b))
	s.DrawBatchBounds(d, actorsBatch, core.RGB(0x38bdf8))
})
d.Restore()
```

也可以直接调用 `*Overlay` 上的快捷方法：`DrawBox`、`DrawCircle`、`DrawSpriteBounds` 与 `DrawBatchBounds`。

`DrawFighterBoxes(d, fighter, hurt, hit)` 描画 [framedata](framedata.md) `Fighter` 在当前帧生效的受击框和攻击框，攻击框在上，并按角色朝向镜像：

```go
overlay.DrawFighterBoxes(d, p1, core.RGB(0x38bdf8), core.RGB(0xef4444))
```

## 绘制统计

可以用以下两种方式提供每帧的绘制提交数，浮层会显示一行 `Draw: N calls · N sprites · N batches`，三角形数或顶点数不为零时再追加 `· N tris · N verts`：

1. **通过 `Options.DrawStats` 拉取**：一个 `func() gamedebug.DrawStats`，浮层每次刷新时调用。
2. **通过 `overlay.SetDrawStats(stats)` 推送**：在 `Options.DrawStats` 为 nil 时使用，每帧绘制完成后调用一次。

```go
overlay := gamedebug.New(gamedebug.Options{
	DrawStats: func() gamedebug.DrawStats {
		return gamedebug.DrawStats{DrawCalls: r.drawCalls, Sprites: len(actorsBatch.Sprites), Batches: r.batches}
	},
})
```

`overlay.DrawStats()` 返回当前数值，`overlay.HasDrawStats()` 表示两种来源是否设置了其一；都未设置时不显示这一行。[`game/mcp`](mcp.md) 的 `game_perf_readout` 报告的是同一组数值。

## 音频发声与并发状态

在 `gamedebug.Options` 中传入 `Sound: mixer` 后，浮层 HUD 会显示当前活跃/淡出中的发声数与全局发声预算、各并发分组的活跃数，以及正在闪避的总线增益。`overlay.Sound()` 返回挂载的 `*sound.Mixer`，[`game/mcp`](mcp.md) 的 `game_sound_readout` 读取的是同一份快照。

## 重置读数

- `overlay.ResetStats()` 清空帧耗时环形缓冲区和已推送的绘制统计，并刷新 HUD。`Options.DrawStats` 回调会继续提供数值。
- `GCMonitor.Reset()` 清空 GC 帧历史、卡顿计数、停顿峰值和每帧分配均值，并重新读取运行时计数器，下一帧从此刻开始计量（见 [GC 与帧分配预算](gc-and-allocations.md)）。
- `Profiler.Reset()` 清空每个计时区间累积的耗时和历史。区间名保留原有 ID，因此跨越重置仍未结束的区间会计入它自己的名字；通过 `StartRecording` 开始的追踪录制会继续进行，已录制的事件也会保留。

[`game/mcp`](mcp.md) 的 `game_perf_reset` 会依次调用这三者。

## 场景检查器

提供实时场景结构有两种方式：

1. **拉取模式（`Options.Scenes`）**：若回调中读取了 NSGo signal，检查器会自动响应式刷新；否则随帧记录以及手动调用 `overlay.RefreshScenes()` 时刷新。
2. **推送模式（`overlay.SetScenes(snapshots)`）**：当 `Options.Scenes` 为 nil 时，立即更新检查器列表。

## AI 检查器与转向可视化

在调试浮层中实时查看选定智能体的 [`game/ai`](ai.md) 行为树（高亮当前运行路径）或 [`game/flow`](flow.md) `Machine` 当前状态与有界转换历史：

```go
overlay := gamedebug.New(gamedebug.Options{
	AI: func() gamedebug.AISnapshot {
		return gamedebug.BehaviorTreeSnapshot("guard-1", guardInspector)
	},
})

// 或按需推送快照（支持行为树、状态机或同时显示）：
overlay.SetAI(gamedebug.StateMachineSnapshot("boss", bossFSM))
```

- **拉取（`Options.AI`）或推送（`SetAI` / `ClearAI` / `RefreshAI`）**：与 `Scenes` 机制一致，随帧记录刷新，或当 `Options.AI` 读取 NSGo signal 时自动响应式刷新。
- **转向调试绘制**：在 `core.Canvas.Draw` 保持相机变换生效时，调用 `overlay.DrawSteering`（或 `ShapeOverlay` 上的 `DrawSteering`、`DrawVector`、`DrawPath`、`DrawTarget`）绘制 2D 转向智能体的半径、当前速度、期望速度、目标准星与路点路径：

```go
overlay.DrawSteering(d, gamedebug.SteeringDebug{
	Agent:           agent,
	DesiredVelocity: desiredVel,
	Target:          targetPos,
	HasTarget:       true,
	Path:            waypoints,
	WaypointIndex:   followState.Index,
	VelocityScale:   0.5,
})
```

在 `-tags release` 构建下，所有 AI 检查面板与转向绘制辅助函数都会编译为零开销空操作，不会链接进发布包。

## 可视化日志

`*VLog` 保存玩法系统做出的决策历史：敌人几秒前为什么选了那条路径，在绘制它的那一帧过去之后依然可查。每条记录都带有产生时的固定步与所属实体：

```go
vlog := gamedebug.NewVLog(4096) // 条目与形状各保留的数量；写满后丢弃最旧的

vlog.Log(step, "enemy-3", "ai", gamedebug.VLogWarn, "path blocked, repathing")
vlog.Shape(step, "enemy-3", "path", [][3]float32{{0, 0, 0}, {4, 0, 2}, {6, 0, 8}})
vlog.Snapshot(step, "enemy-3", statsCategory) // 任意 gamedebug.Category
```

- **`Category`** 是系统用来描述单个实体的接口：`Name()` 与 `Snapshot(entity) []Field`。`Snapshot` 把这些字段记成该分类名下的一条 `info` 条目。
- **`Entries(entity, category, from, to)`** 与 **`Shapes(entity, from, to)`** 按记录顺序返回结果；实体或分类为空时匹配全部，`to == 0` 表示没有上界。
- **`WriteJSONL(w)`** 把日志存为 JSON Lines：先是表头 `{"format":"gamedebug.vlog","version":1}`，随后每个条目一行 `{"type":"entry",...}`、每个形状一行 `{"type":"shape",...}`。输出是确定的，从文件读回的日志再保存一次，字节完全相同。**`ReadVLog(r)`** 负责读回，缺少或无法识别的表头、未知的记录类型或级别、未知字段都会被拒绝。

`VLog` 可以并发使用。在 `-tags release` 下，`VLog` 的所有方法都是空实现，`Snapshot` 不会调用分类，`ReadVLog` 返回错误。调试浮层目前还不会绘制日志或展示分类。

## 在发布构建中彻底裁剪

使用 `release` 构建标签时，发布包会裁剪掉 `pkg/tooling/gamedebug`、`pkg/tooling/hotreload`、`pkg/tooling/devstate` 以及物理与 AI 的调试绘制代码：

- **`game release` 以及场景编辑器的导出面板 / 构建与发布界面**会自动为 `nsgo build` 传入 `GOFLAGS=-tags=release`，并且 `game release` 会在构建完成后校验桌面端二进制，若缺少 `release` 标签或链接了仅限开发期的调试符号则直接失败。
- **直接运行 `nsgo build` 或 `go build`** 目前不会自动附加 `release` 标签（上游跟踪见 [nsgo-dev/nsgo#2695](https://github.com/nsgo-dev/nsgo/issues/2695)），请在单次调用的环境变量中通过 `GOFLAGS` 传入：

```sh
GOFLAGS=-tags=release go tool nsgo build -target macos,windows,linux,web
go build -tags release ./...
```

不要把 `release` 写进 `nsgo.yaml` 的 `build.tags`：NSGo 会把 `build.tags` 应用到包括 `nsgo dev` 和 `nsgo test` 在内的所有 `go` 命令上，导致开发期运行与测试也失去调试浮层。

启用 `-tags release` 后，`gamedebug.Enabled()` 返回 `false`，`Overlay.View(cx)` 返回 `nil`，`Overlay`、`FrameStats` 和 `ShapeOverlay` 的所有方法均替换为空实现，浮层 UI、帧耗时图与线框绘制代码不会被链接进发布包。

## 网络检查面板

传入 `Options.Net func() NetSnapshot`（或通过 `Overlay.SetNet` 推送快照）可在调试浮层中显示实时网络面板，展示各通道与各对等端的字节/秒与包/秒速率、传输层的 `netplay.InboxStats` 计数器（并在出现不可靠丢包、可靠拒收或可靠积压告警时高亮），以及最近解码的线协议消息类型。

如需在不增加每帧堆分配的前提下驱动该面板，可用 `gamedebug.NewNetMonitor` 包装 `netplay.Transport` 并在每帧调用 `SampleFrame(dt)`：

```go
netMon := gamedebug.NewNetMonitor(12)
transport = netMon.Wrap(transport)

overlay := gamedebug.New(gamedebug.Options{
	Net: netMon.Snapshot,
})
```

`NetMonitor` 使用 `pkg/online/netplay/wire` 生成的编解码器解码到复用的结构体缓冲区中，因此稳态下的收发观测、速率采样以及 `Overlay.RecordFrame` 均为零堆分配。在 `-tags release` 构建下，`NewNetMonitor` 返回空实现，`Wrap` 直接返回原始传输。
