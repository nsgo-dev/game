# debug

`game/debug` provides development-only diagnostics for NSGo games: a key-toggled frame-time graph, a live scene/layer inspector, and world-space collision and sprite-bounds wireframes — all stripped to zero-cost no-ops with `-tags release`.

```go
import "github.com/nsgo-dev/game/packages/game/debug"
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

## Scene inspector

Supply live scene structure in either of two ways:

1. **Pull via `Options.Scenes`**: if the callback reads NSGo signals, the inspector updates reactively; otherwise it refreshes on recorded frames and whenever you call `overlay.RefreshScenes()`.
2. **Push via `overlay.SetScenes(snapshots)`**: updates the inspector immediately when `Options.Scenes` is nil.

## Stripping debug code in release builds

Build release binaries with `-tags release`:

```sh
go build -tags release ./...
```

Under `-tags release`, `debug.Enabled()` returns `false`, `Overlay.View(cx)` returns `nil`, and every `Overlay`, `FrameStats`, and `ShapeOverlay` method compiles to an empty no-op — so none of the overlay UI, frame graph, or wireframe drawing code is linked into your shipping binary.
