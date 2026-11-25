# debug

`game/debug` 为 NSGo 游戏提供开发期调试浮层：快捷键开关的帧耗时柱状图、实时场景/图层检查器，以及世界空间碰撞体与精灵包围盒线框；使用 `-tags release` 构建时全部裁剪为零开销空操作。

```go
import "github.com/nsgo-dev/game/packages/game/debug"
```

## 接入调试浮层与帧耗时图

通过 `debug.New` 创建 `*Overlay`，用 `WrapLoop` 包装 `game.Loop`，并把 `overlay.View(cx)` 叠放在游戏画布上方：

```go
overlay := debug.New(debug.Options{
	ToggleKey: "F3", // 为空时默认 "F3"
	TargetFPS: 60,   // 帧耗时图的参考线
	LineWidth: 1,
	Scenes: func() []debug.SceneSnapshot {
		return []debug.SceneSnapshot{
			{
				Name: "world",
				Layers: []debug.LayerSnapshot{
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
overlay.DrawShapes(d, func(s *debug.ShapeOverlay) {
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

1. **通过 `Options.DrawStats` 拉取**：一个 `func() debug.DrawStats`，浮层每次刷新时调用。
2. **通过 `overlay.SetDrawStats(stats)` 推送**：在 `Options.DrawStats` 为 nil 时使用，每帧绘制完成后调用一次。

```go
overlay := debug.New(debug.Options{
	DrawStats: func() debug.DrawStats {
		return debug.DrawStats{DrawCalls: r.drawCalls, Sprites: len(actorsBatch.Sprites), Batches: r.batches}
	},
})
```

`overlay.DrawStats()` 返回当前数值，`overlay.HasDrawStats()` 表示两种来源是否设置了其一；都未设置时不显示这一行。[`game/mcp`](mcp.md) 的 `game_perf_readout` 报告的是同一组数值。

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
overlay := debug.New(debug.Options{
	AI: func() debug.AISnapshot {
		return debug.BehaviorTreeSnapshot("guard-1", guardInspector)
	},
})

// 或按需推送快照（支持行为树、状态机或同时显示）：
overlay.SetAI(debug.StateMachineSnapshot("boss", bossFSM))
```

- **拉取（`Options.AI`）或推送（`SetAI` / `ClearAI` / `RefreshAI`）**：与 `Scenes` 机制一致，随帧记录刷新，或当 `Options.AI` 读取 NSGo signal 时自动响应式刷新。
- **转向调试绘制**：在 `core.Canvas.Draw` 保持相机变换生效时，调用 `overlay.DrawSteering`（或 `ShapeOverlay` 上的 `DrawSteering`、`DrawVector`、`DrawPath`、`DrawTarget`）绘制 2D 转向智能体的半径、当前速度、期望速度、目标准星与路点路径：

```go
overlay.DrawSteering(d, debug.SteeringDebug{
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

## 在发布构建中彻底裁剪

发布构建时传入 `-tags release`：

```sh
go build -tags release ./...
```

启用 `-tags release` 后，`debug.Enabled()` 返回 `false`，`Overlay.View(cx)` 返回 `nil`，`Overlay`、`FrameStats` 和 `ShapeOverlay` 的所有方法均替换为空实现，浮层 UI、帧耗时图与线框绘制代码不会被链接进发布包。
