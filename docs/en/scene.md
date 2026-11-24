# scene

`game/scene` composes ordered world- and screen-space layers, hit-tests sprites and custom drawables in view coordinates, and manages scene lifecycles, overlay stacks, and animated transitions.

```go
import "github.com/nsgo-dev/game/packages/game/scene"
```

## Scene, layers, and drawables

A `Scene` holds a shared `*tilemap.Camera` and a slice of `*Layer` values drawn in stable ascending `Order`. Each layer operates in either `SpaceWorld` (drawn under `Scene.Camera`) or `SpaceScreen` (drawn directly in view space for HUD elements):

```go
sc := scene.NewScene(cam)

terrain := sc.AddLayer(scene.NewLayer("terrain", scene.SpaceWorld, 0))
terrain.AddTilemap(levelMap)

actors := sc.AddLayer(scene.NewLayer("actors", scene.SpaceWorld, 10))
actors.AddBatch(actorBatch)

hud := sc.AddLayer(scene.NewLayer("hud", scene.SpaceScreen, 100))
hud.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
	drawMinimap(d)
}))
```

- **Items**: a layer accepts `AddTilemap`, `AddBatch`, `AddDrawable`, or an explicit `AddItem(scene.Item{...})`. Items inside a layer draw in stable ascending `Z`.
- **`Drawable`**: wraps custom `Draw`, `Update`, `Hit`, `OnPress`, and `OnTouch` hooks with its own `Z` and `Visible` flag. Its zero value has `Visible: false`; start with `scene.NewDrawable(fn)`.
- **`Scene.Update(dt)`** advances `Camera`, tilemaps, and custom drawables in layer order; **`Scene.Draw(d)`** draws every visible layer while preserving `d`'s transform and paint state.

## Hit testing and input dispatch

`Scene.HitTest(p)` tests view-space point `p` in reverse draw order (highest `Layer.Order`, then highest item/sprite `Z`), converting `p` via `Camera.ViewToWorld` on `SpaceWorld` layers and testing `p` directly on `SpaceScreen` layers:

```go
if hit, ok := sc.HitTest(ev.Position); ok {
	if hit.Sprite != nil {
		selectUnit(hit.Sprite, hit.Point)
	}
}
```

Use `HitTestAll(p)` to retrieve every overlapping target from topmost to bottommost.

### Custom hit shapes and callbacks

By default, sprites hit-test against their transformed untrimmed frame bounds (`SpriteContains` / `SpriteToLocal`). Override hit geometry or attach direct press/touch handlers with `SetSpriteHit` (and remove with `ClearSpriteHit`):

```go
sc.SetSpriteHit(&coinSprite, scene.HitOptions{
	Hit: scene.SpriteLocalHit(&coinSprite, scene.CircleHit(core.Point{X: 8, Y: 8}, 7)),
	OnPress: func(hit scene.HitResult, ev core.PressEvent) {
		collectCoin()
	},
})
```

Built-in `HitFunc` helpers: `RectHit(bounds)`, `CircleHit(center, radius)`, `PolygonHit(vertices...)`, and `SpriteLocalHit(sp, local)`. Set `HitOptions{Ignore: true}` to make a decorative sprite transparent to clicks and touches.

Route view events directly to the topmost hit target with `sc.DispatchPress(ev)` and `sc.DispatchTouch(ev)` (when a target sets `OnPress` without `OnTouch`, `DispatchTouch` falls back to `OnPress` on `TouchStart`).

## Manager, stacks, and transitions

`Manager` owns a stack of `*Controller` scenes under a parent `*core.Scope`. Every entered scene receives its own child `*core.Scope` in `Enter(cx)`, so loops, signals, timers, and audio created under `cx` are automatically disposed when the scene exits:

```go
mgr := scene.NewManager(cx)

play := &scene.Controller{
	Scene: gameScene,
	Enter: func(scx *core.Scope) {
		// Resources owned by this scene's child scope
		startLevelMusic(scx)
	},
	Pause:  func() { pauseSimulation() },
	Resume: func() { resumeSimulation() },
	Exit:   func() { cleanupLevel() },
	Update: func(dt time.Duration) {
		gameScene.Update(dt)
	},
}

pauseMenu := &scene.Controller{
	Scene:       pauseScene,
	Transparent: true, // draws the paused scene underneath
}

mgr.Switch(play, scene.Fade(400*time.Millisecond, core.RGB(0x000000)))
// Later, push an overlay without exiting play:
mgr.Push(pauseMenu)
// Pop to resume play:
mgr.Pop()
```

### Transitions

Pass a `*Transition` to `mgr.Switch(next, tr)`:

| Helper | Visual behavior |
| --- | --- |
| `Fade(d, color)` | Fades outgoing scene to `color` over the first half, then reveals incoming scene over the second half (crossfades when `color` is transparent/0). |
| `Crossfade(d)` | Crossfades directly from outgoing to incoming scene over `d`. |
| `Wipe(d, dir)` | Clips scenes along an edge moving in `DirectionLeft`, `DirectionRight`, `DirectionUp`, or `DirectionDown`. |
| `CustomTransition(d, draw)` | Calls your custom draw function with eased `progress` in `[0, 1]` plus `drawOld` and `drawNew`. |

Set `tr.Curve` to any `core.Curve` easing function. While a transition runs (`mgr.Transitioning()`), the incoming scene enters immediately while the outgoing scene stays alive to render until completion, then exits and disposes its child scope.

Drive the manager from your `game.Loop` and `core.Canvas`:

```go
// In Loop.Update:
mgr.Update(dt)

// In Canvas.Draw (automatically subscribes to scene changes & transitions):
mgr.Draw(d, size, alpha.Get())
```

## 2D layers over a 3D view (`Overlay3D`)

`Overlay3D` renders 2D screen-space layers (`SpaceScreen`), standalone `*sprite.Batch`es, and custom draw callbacks in a transparent `core.Canvas` stacked directly over a 3D `gpu.View` (and beneath any native NSGo HUD views in `gpu.ViewProps.Children`):

```go
ov := scene.NewOverlay3D(cx)
ov.Scene = hudScene      // draws visible SpaceScreen layers (SpaceWorld layers are skipped)
ov.AddLayer(crosshair)   // extra 2D layers merged by stable ascending Layer.Order
ov.AddBatch(markerBatch) // standalone sprite batches drawn over the layers
ov.Viewport = &screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit}

// Mount directly over a 3D gpu.View:
return ov.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady:     onReady,
	OnFrame:     onFrame,
	Children:    nativeHUDViews, // rendered above the 2D overlay canvas
})
```

- **Draw order**: visible `SpaceScreen` layers from `Scene` and visible `Layers` merge in stable ascending `Layer.Order`, followed by `Batches` in slice order and `OnDraw` (wrapped in `Save`/`Restore`). Call `Scene.DrawScreenLayers(d)` directly if you manage your own canvas.
- **Transparency & virtual viewports**: the overlay canvas has no background fill so transparent regions reveal the 3D scene underneath and semi-transparent sprites alpha-blend over it. When `Viewport` is set with a zero `BarColor`, letterbox and pillarbox margins stay transparent instead of painting opaque black bars over the 3D view; set `BarColor` explicitly (such as `core.RGB(0x000000)`) to draw bars.
- **Pointer pass-through & hit testing**: by default the overlay canvas sets `core.PointerEventsNone` so clicks and touches pass straight through to the 3D `gpu.View`. Set `Interactive: true` (or `OnPress`) to handle pointer events directly on the overlay canvas, or call `ov.HitTest`, `ov.HitTestAll`, `ov.DispatchPress`, and `ov.DispatchTouch` manually with the view size.
- **3D-to-2D projection**: map a 3D world position into 2D overlay coordinates with `scene.ProjectToOverlay(cam, worldPos, viewW, viewH)` (or `ov.Project(cam, worldPos, windowSize)` to convert into `ov.Viewport`'s virtual coordinates) so 2D health bars, reticles, and markers track 3D objects accurately. Call `ov.Invalidate()` (or `ov.SetHidden(bool)`) whenever dynamic 2D overlay state changes.
