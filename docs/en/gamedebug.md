# gamedebug

`game/pkg/tooling/gamedebug` provides development-only diagnostics for NSGo games: a key-toggled frame-time graph, a live scene/layer inspector, and world-space collision and sprite-bounds wireframes — all stripped to zero-cost no-ops with `-tags release`.

It is unrelated to NSGo DevTools (`nsgo dev -devtools`), the browser page the NSGo dev server serves for any NSGo app; this package draws inside the game's own window.

```go
import "github.com/nsgo-dev/game/pkg/tooling/gamedebug"
```

## Wiring the overlay and frame-time graph

Create an `*Overlay` with `gamedebug.New`, wrap your `game.Loop` with `WrapLoop`, and mount `overlay.View(cx)` alongside your game canvas:

```go
overlay := gamedebug.New(gamedebug.Options{
	ToggleKey: "F3", // default when empty
	TargetFPS: 60,   // reference line on the frame graph
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
overlay.DrawShapes(d, func(s *gamedebug.ShapeOverlay) {
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

1. **Pull via `Options.DrawStats`**: a `func() gamedebug.DrawStats` called whenever the overlay refreshes.
2. **Push via `overlay.SetDrawStats(stats)`**: used when `Options.DrawStats` is nil; call it once per frame after drawing.

```go
overlay := gamedebug.New(gamedebug.Options{
	DrawStats: func() gamedebug.DrawStats {
		return gamedebug.DrawStats{DrawCalls: r.drawCalls, Sprites: len(actorsBatch.Sprites), Batches: r.batches}
	},
})
```

`overlay.DrawStats()` returns the current values and `overlay.HasDrawStats()` reports whether either source is set; the line stays hidden until one is. [`game/mcp`](mcp.md)'s `game_perf_readout` reports the same values.

## Sound voices and concurrency

Pass `Sound: mixer` in `gamedebug.Options` to show live active/fading voice counts against the mixer's global voice budget, active counts per concurrency group, and any currently ducked buses in the overlay HUD. `overlay.Sound()` returns the attached `*sound.Mixer`, and [`game/mcp`](mcp.md)'s `game_sound_readout` reads the same snapshot.

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
overlay := gamedebug.New(gamedebug.Options{
	AI: func() gamedebug.AISnapshot {
		return gamedebug.BehaviorTreeSnapshot("guard-1", guardInspector)
	},
})

// Or push a snapshot on demand (behavior tree, state machine, or both):
overlay.SetAI(gamedebug.StateMachineSnapshot("boss", bossFSM))
```

- **Pull (`Options.AI`) or push (`SetAI` / `ClearAI` / `RefreshAI`)**: works just like `Scenes`, refreshing on recorded frames or reactively when `Options.AI` reads NSGo signals.
- **Steering guides**: inside `core.Canvas.Draw` while your camera transform is active, draw a 2D steering agent's body radius, velocity, desired velocity, target reticle, and waypoint path with `overlay.DrawSteering` (or `ShapeOverlay.DrawSteering`, `DrawVector`, `DrawPath`, `DrawTarget`):

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

All AI panel views and steering draw helpers compile to zero-cost no-ops under `-tags release`, so none of the inspection UI or drawing code is linked into release binaries.

## Visual log

A `*VLog` keeps a history of what gameplay systems decided, so the reason an enemy took a path several seconds ago is still there after the frame that drew it. Every record carries the fixed step it was made at and the entity it is about:

```go
vlog := gamedebug.NewVLog(4096) // entries and shapes kept; the oldest is dropped once full

vlog.Log(step, "enemy-3", "ai", gamedebug.VLogWarn, "path blocked, repathing")
vlog.Shape(step, "enemy-3", "path", [][3]float32{{0, 0, 0}, {4, 0, 2}, {6, 0, 8}})
vlog.Snapshot(step, "enemy-3", statsCategory) // any gamedebug.Category
```

- **`Category`** is the interface a system implements to describe one entity: `Name()` and `Snapshot(entity) []Field`. `Snapshot` records the fields as an `info` entry under the category's name.
- **`Entries(entity, category, from, to)`** and **`Shapes(entity, from, to)`** return records in recorded order; an empty entity or category matches any, and `to == 0` means no upper bound.
- **`WriteJSONL(w)`** saves the log as JSON Lines: a header `{"format":"gamedebug.vlog","version":1}`, then one `{"type":"entry",...}` line per entry and one `{"type":"shape",...}` line per shape. The output is deterministic, so saving a log read back from a file gives the same bytes. **`ReadVLog(r)`** reads it back and refuses a missing or unknown header, an unknown record type or level, and unknown fields.

A `VLog` is safe for concurrent use. Under `-tags release` every `VLog` method is a no-op, `Snapshot` does not call the category, and `ReadVLog` returns an error. The overlay does not draw the log or show categories yet.

## Stripping debug code in release builds

Release builds strip `pkg/tooling/gamedebug`, `pkg/tooling/hotreload`, `pkg/tooling/devstate`, and physics/AI debug drawing when built with the `release` Go build tag:

- **`game release` and the scene editor's Export / Build and Release screens** pass `GOFLAGS=-tags=release` to `nsgo build` automatically, and `game release` checks built desktop binaries afterwards to fail if the `release` tag is missing or a debug-only symbol was linked.
- **Plain `nsgo build` or `go build`** does not set `release` automatically yet (tracked upstream in [nsgo-dev/nsgo#2695](https://github.com/nsgo-dev/nsgo/issues/2695)), so pass it through `GOFLAGS` for the invocation:

```sh
GOFLAGS=-tags=release go tool nsgo build -target macos,windows,linux,web
go build -tags release ./...
```

Do not put `release` in `nsgo.yaml`'s `build.tags`: NSGo applies `build.tags` to every `go` command it runs, including `nsgo dev` and `nsgo test`, which would strip the overlay from development runs and tests as well.

Under `-tags release`, `gamedebug.Enabled()` returns `false`, `Overlay.View(cx)` returns `nil`, and every `Overlay`, `FrameStats`, and `ShapeOverlay` method compiles to an empty no-op — so none of the overlay UI, frame graph, or wireframe drawing code is linked into your shipping binary.

## Network inspector

Pass `Options.Net func() NetSnapshot` (or push snapshots with `Overlay.SetNet`) to show a live network panel inside the overlay. The panel displays per-channel and per-peer bytes/s and packets/s, the transport's `netplay.InboxStats` counters (highlighting dropped unreliable packets, refused reliable packets, or an active reliable backlog), and the most recent decoded wire message types.

To feed the panel without per-frame heap allocations, wrap your `netplay.Transport` with `gamedebug.NewNetMonitor` and call `SampleFrame(dt)` once per frame:

```go
netMon := gamedebug.NewNetMonitor(12)
transport = netMon.Wrap(transport)

overlay := gamedebug.New(gamedebug.Options{
	Net: netMon.Snapshot,
})
```

`NetMonitor` decodes sent and received packets using the generated `pkg/online/netplay/wire` codecs into reused struct buffers, so steady-state packet observation, rate sampling, and `Overlay.RecordFrame` perform zero heap allocations. Under `-tags release`, `NewNetMonitor` returns a no-op stub and `Wrap` returns the inner transport unchanged.
