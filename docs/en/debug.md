# debug

`game/debug` provides development-only diagnostics for NSGo games: a key-toggled frame-time graph, a live scene/layer inspector, and world-space collision and sprite-bounds wireframes — all stripped to zero-cost no-ops with `-tags release`.

```go
import "github.com/nsgo-dev/game/pkg/debug"
```

## Wiring the overlay and frame-time graph

Create an `*Overlay` with `debug.New`, wrap your `game.Loop` with `WrapLoop`, and mount `overlay.View(cx)` alongside your game canvas:

```go
overlay := debug.New(debug.Options{
	ToggleKey: "F3", // default when empty
	TargetFPS: 60,   // reference line on the frame graph
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
		overlay.View(cx), // returns nil under -tags release
	},
}
```

- **`WrapLoop(l)`** wraps `l.Update` and `l.Draw` to time each frame and call `RecordFrame(total, update, draw)` into the fixed-size `FrameStats` ring buffer (`FrameHistory = 120` samples, zero per-frame heap allocations).
- **`overlay.Stats()`** exposes `FPS()`, `Average()`, `Latest()`, `MaxTotal()`, `Len()`, `At(i)`, `Samples(dst)`, `Reset()`, and `DrawGraph(d, size, targetFPS)` if you want to embed metrics elsewhere.
- **Toggling**: press `ToggleKey` (default `"F3"`) or call `overlay.Toggle()` / `overlay.SetVisible(true)`. Reading `overlay.Visible()` inside a reactive view or `Canvas.Draw` subscribes automatically.

## Drawing collision and sprite-bounds wireframes

Draw world-space debug wireframes inside `core.Canvas.Draw` while your camera/viewport transform is active. `ShapeOverlay` automatically divides `LineWidth` by the drawing's active scale so wireframes stay crisp at any camera zoom:

```go
d.Save()
cam.Apply(d)
worldScene.Draw(d)

// Only executes when the overlay is visible; subscribes the canvas to toggles
overlay.DrawShapes(d, func(s *debug.ShapeOverlay) {
	s.DrawBox(d, playerBounds, core.RGB(0x22c55e))
	s.DrawCircle(d, aggroX, aggroY, aggroRadius, core.RGB(0xf59e0b))
	s.DrawBatchBounds(d, actorsBatch, core.RGB(0x38bdf8))
})
d.Restore()
```

Convenience shortcuts `overlay.DrawBox`, `overlay.DrawCircle`, `overlay.DrawSpriteBounds`, and `overlay.DrawBatchBounds` are also available directly on `*Overlay`.

`DrawFighterBoxes(d, fighter, hurt, hit)` strokes the hurtboxes and hitboxes a [framedata](framedata.md) `Fighter` has active on its current frame, hitboxes on top, mirrored as the fighter faces:

```go
overlay.DrawFighterBoxes(d, p1, core.RGB(0x38bdf8), core.RGB(0xef4444))
```

## Draw statistics

Report each frame's draw submissions in either of two ways, and the overlay shows them as a `Draw: N calls · N sprites · N batches` line, followed by `· N tris · N verts` once either count is non-zero:

1. **Pull via `Options.DrawStats`**: a `func() debug.DrawStats` called whenever the overlay refreshes.
2. **Push via `overlay.SetDrawStats(stats)`**: used when `Options.DrawStats` is nil; call it once per frame after drawing.

```go
overlay := debug.New(debug.Options{
	DrawStats: func() debug.DrawStats {
		return debug.DrawStats{DrawCalls: r.drawCalls, Sprites: len(actorsBatch.Sprites), Batches: r.batches}
	},
})
```

`overlay.DrawStats()` returns the current values and `overlay.HasDrawStats()` reports whether either source is set; the line stays hidden until one is. [`game/mcp`](mcp.md)'s `game_perf_readout` reports the same values.

## Resetting the readouts

- `overlay.ResetStats()` clears the frame-time ring buffer and any pushed draw statistics and refreshes the HUD. An `Options.DrawStats` callback keeps reporting.
- `GCMonitor.Reset()` clears the GC frame history, hitch count, pause peaks and per-frame allocation averages, and re-reads the runtime counters so the next frame is measured from that point (see [GC and frame allocations](gc-and-allocations.md)).
- `Profiler.Reset()` clears every timing scope's accumulated timings and history. Scope names keep their IDs, so a scope still open across the reset is charged to its own name, and a trace recording started with `StartRecording` keeps running and keeps its events.

[`game/mcp`](mcp.md)'s `game_perf_reset` calls all three.

## Scene inspector

Supply live scene structure in either of two ways:

1. **Pull via `Options.Scenes`**: if the callback reads NSGo signals, the inspector updates reactively; otherwise it refreshes on recorded frames and whenever you call `overlay.RefreshScenes()`.
2. **Push via `overlay.SetScenes(snapshots)`**: updates the inspector immediately when `Options.Scenes` is nil.

## AI inspector and steering guides

Inspect a chosen agent's [`game/ai`](ai.md) behavior tree (with the running path highlighted) or [`game/flow`](flow.md) `Machine` state and bounded transition history inside the overlay panel:

```go
overlay := debug.New(debug.Options{
	AI: func() debug.AISnapshot {
		return debug.BehaviorTreeSnapshot("guard-1", guardInspector)
	},
})

// Or push a snapshot on demand (behavior tree, state machine, or both):
overlay.SetAI(debug.StateMachineSnapshot("boss", bossFSM))
```

- **Pull (`Options.AI`) or push (`SetAI` / `ClearAI` / `RefreshAI`)**: works just like `Scenes`, refreshing on recorded frames or reactively when `Options.AI` reads NSGo signals.
- **Steering guides**: inside `core.Canvas.Draw` while your camera transform is active, draw a 2D steering agent's body radius, velocity, desired velocity, target reticle, and waypoint path with `overlay.DrawSteering` (or `ShapeOverlay.DrawSteering`, `DrawVector`, `DrawPath`, `DrawTarget`):

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

All AI panel views and steering draw helpers compile to zero-cost no-ops under `-tags release`, so none of the inspection UI or drawing code is linked into release binaries.

## Stripping debug code in release builds

Build release binaries with `-tags release`:

```sh
go build -tags release ./...
```

Under `-tags release`, `debug.Enabled()` returns `false`, `Overlay.View(cx)` returns `nil`, and every `Overlay`, `FrameStats`, and `ShapeOverlay` method compiles to an empty no-op — so none of the overlay UI, frame graph, or wireframe drawing code is linked into your shipping binary.

## Network inspector

Pass `Options.Net func() NetSnapshot` (or push snapshots with `Overlay.SetNet`) to show a live network panel inside the overlay. The panel displays per-channel and per-peer bytes/s and packets/s, the transport's `netplay.InboxStats` counters (highlighting dropped unreliable packets, refused reliable packets, or an active reliable backlog), and the most recent decoded wire message types.

To feed the panel without per-frame heap allocations, wrap your `netplay.Transport` with `debug.NewNetMonitor` and call `SampleFrame(dt)` once per frame:

```go
netMon := debug.NewNetMonitor(12)
transport = netMon.Wrap(transport)

overlay := debug.New(debug.Options{
	Net: netMon.Snapshot,
})
```

`NetMonitor` decodes sent and received packets using the generated `pkg/online/netplay/wire` codecs into reused struct buffers, so steady-state packet observation, rate sampling, and `Overlay.RecordFrame` perform zero heap allocations. Under `-tags release`, `NewNetMonitor` returns a no-op stub and `Wrap` returns the inner transport unchanged.
