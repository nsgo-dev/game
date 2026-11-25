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

- **Items**: a layer accepts `AddTilemap`, `AddTileLayer`, `AddBatch`, `AddDrawable`, or an explicit `AddItem(scene.Item{...})`. Items inside a layer draw in stable ascending `Z`.
- **Y-sorting (`DepthSort = scene.SortY`)**: sorts sprites, custom `Drawable`s, and tiles from any `tilemap.TileLayer` with `YSort: true` in one stable painter's-order list by ascending Y (with optional `SortKey` / `SpriteSortKey` overrides and per-tile `sort_origin_y` offsets). Non-Y-sorted tilemap layers before Y-sorted layers draw behind sprites, and trailing non-Y-sorted layers (such as roofs) draw above them; use `AddTileLayer(m, layer)` to bind a single tile layer to a specific scene layer.
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
- **3D-to-2D projection**: map a 3D world position through a column-major view-projection matrix (such as `cam.ViewProj()` from `game/scene3d`) into 2D overlay coordinates with `scene.ProjectToOverlay(cam.ViewProj(), p.X, p.Y, p.Z, viewW, viewH)` (or `ov.Project(cam.ViewProj(), p.X, p.Y, p.Z, windowSize)` to convert into `ov.Viewport`'s virtual coordinates) so 2D health bars, reticles, and markers track 3D objects accurately. Call `ov.Invalidate()` (or `ov.SetHidden(bool)`) whenever dynamic 2D overlay state changes.
## Native HUD over a 3D view (`HUD3D`)

`HUD3D` composes native NSGo views (`ui.Button`, `ui.Card`, `ui.Badge`, `ui.Progress`, `ui.Switch`, `core.Text`, and custom `core.View` trees) over a 3D `gpu.View` with automatic safe-area insets, reactive visibility toggling, and mouse/touch pass-through:

```go
hud := scene.NewHUD3D(cx)
hud.Padding = core.All(12)

// Decorative readout: mouse and touch pass straight through to the 3D scene.
hud.AddPassThrough(scene.HUDTopLeft, ui.Badge{Text: "HP 100"})

// Interactive controls anchored to safe-area corners:
hud.AddAt(scene.HUDTopRight, ui.Button{
	Text:    "Pause",
	OnPress: togglePause,
})
hud.AddAt(scene.HUDBottomRight, ui.Button{
	Text:    "Jump",
	OnPress: triggerJump,
})

// Mount directly over a 3D gpu.View:
return hud.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady:     onReady,
	OnFrame:     onFrame,
	OnPress:     pick3DObject,
})
```

- **Mouse and touch pass-through**: the HUD overlay container uses `core.PointerEventsBoxNone` so interactive HUD controls handle clicks and touches directly while open regions pass mouse and touch events unobstructed to the 3D view underneath. Mark decorative elements (such as crosshairs or score readouts) with `AddPassThrough` (or `HUDSlot{PassThrough: true}`) to apply `core.PointerEventsNone`.
- **Anchors & custom layouts**: place widgets with `AddAt` / `AddPassThrough` / `AddSlot` at any of the 9 safe-area anchors (`HUDTopLeft`, `HUDTopCenter`, `HUDTopRight`, `HUDCenterLeft`, `HUDCenter`, `HUDCenterRight`, `HUDBottomLeft`, `HUDBottomCenter`, `HUDBottomRight`) or `HUDFull`, or add custom pass-through containers directly with `Add` / `Children`.
- **Safe-area insets**: `core.EnvSafeArea` padding is applied automatically (combined with `HUD3D.Padding`) so HUD controls stay clear of notches, status bars, and home indicators and adapt reactively on rotation. Set `IgnoreSafeArea: true` or call `hud.SetSafeArea(false)` to disable it.
- **Visibility toggle**: call `hud.SetHidden(true)`, `hud.SetVisible(false)`, or `hud.Toggle()` to hide the HUD (`core.DisplayNone` + `core.PointerEventsNone`) without remounting HUD state; while hidden, all clicks and touches reach the 3D view.
- **Composing with 3D input wrappers or 2D overlays**: use `hud.Wrap(cx, view3D)` to Z-stack the HUD as a sibling over any already-built 3D view (for example a `gpu.View` wrapped in a `core.View` handling `OnMouseDown`, `OnMouseMove`, and `OnTouch` for 3D camera orbit), or mount `hud.Layer(cx)` directly inside `gpu.ViewProps.Children` above a 2D overlay canvas.

## Switching between 2D and 3D scenes

`Manager.View(opts)` mounts a reactive view host that switches seamlessly between 2D canvas scenes (`Controller.Scene` / `Controller.Draw`) and 3D or native view scenes (`Controller.View`, such as `gpu.View` or `Overlay3D.View`) while every scene shares the same `*input.Actions` and the same `*sound.Mixer`:

```go
mgr := scene.NewManager(cx)
actions.WatchKeys(cx) // keys reach Actions whether or not the stage holds focus
mgr.Actions = actions // set before mgr.View: attached to the persistent stage view, survives 2D <-> 3D switches
mixer := sound.NewMixer(cx, "game.sound") // on the Manager's scope, not a scene's, so music plays on across switches
mixer.Music("music").Play(theme, 0) // started once here, so no scene owns it

var overworld2D, dungeon3D *scene.Controller
overworld2D = &scene.Controller{
	Scene: mapScene,
	Update: func(dt time.Duration) {
		if mgr.Actions.JustPressed("enter_3d") {
			mgr.Switch(dungeon3D, scene.Fade(400*time.Millisecond, core.RGB(0x000000)))
		}
	},
}
dungeon3D = &scene.Controller{
	View: func(scx *core.Scope) core.Node {
		return ov.View(scx, gpu.ViewProps{
			Style:       core.Style{Width: core.Pct(100), Height: core.Pct(100)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady:     onReady,
			OnFrame:     onFrame,
		})
	},
	Update: func(dt time.Duration) {
		if mgr.Actions.JustPressed("exit_3d") {
			mgr.Switch(overworld2D, scene.Fade(400*time.Millisecond, core.RGB(0x000000)))
		}
	},
}

mgr.Switch(overworld2D, nil)
return mgr.View(scene.HostOptions{
	Alpha:    func() float64 { _ = frameTick.Get(); return alpha },
	Viewport: &viewport,
	Children: persistentHUD,
})
```

- **Stable GPU view mounting**: each entered scene receives a unique instance ID rendered via `core.Each`. Mounted 3D `gpu.View` surfaces and GPU pipelines stay alive across transition frames and across `Push`/`Pop` of 2D overlays (transparent or opaque), and unmount cleanly when the scene exits.
- **View scope**: the `cx` passed to `Controller.View` is the mounted row's scope, not the scope `Enter` receives. When a scene leaves, `Exit` runs and `Enter`'s scope is disposed first, then the row and everything created under `View`'s `cx` are unmounted in the same `Switch`, `Pop`, or transition step, so no frame or `OnFrame` callback runs in between. Create the state a 3D view reads every frame in `View` or on the Manager's scope.
- **Transitions across 2D ↔ 3D**: `Fade(d, color)` fades the outgoing 2D or 3D scene to `color` on a top overlay canvas over the first half, reveals the incoming scene at the midpoint (`0.5`), and fades out over the second half; `Crossfade(d)` animates outgoing/incoming wrapper `Opacity`; `Wipe(d, dir)` uses exact path clipping between 2D scenes and smooth crossfade opacity when a `Controller.View` scene is involved; `CustomTransition`, or a built-in transition whose `Draw` you replaced, draws its own frames between 2D scenes and custom overlay effects above the active view otherwise. A `Transition` with a nil `Draw` shows the incoming scene for its whole `Duration`, as `Manager.Draw` does.
- **Shared input**: `Manager.View` attaches `Manager.Actions` to its persistent stage view when `View` is called, so set `Actions` first. Key events reach the stage only while it holds focus: pass `HostOptions.Ref` and call its `Focus()` once mounted (focus and pointer lock then survive every switch), or call `Actions.WatchKeys` as above. `Switch`, `Push`, and `Pop` call `Actions.Reset()` by default so transient press edges and held keys do not leak into the next scene while all bindings, axes, and pointer lock stay intact (set `Manager.KeepInputOnSwitch = true` to preserve held inputs across transitions).
- **Shared sound**: the Manager needs no mixer of its own. A `sound.Mixer` created on the Manager's scope (or an ancestor of it) is not disposed when scenes exit, so background music plays on and SFX groups stay live across every 2D ↔ 3D switch while each scene reaches the mixer through its own closures. A mixer created under a scene's `Enter` scope stops with that scene.
- **2D overlays & invalidation**: pushing a `Transparent: true` 2D controller over a 3D scene keeps the 3D view visible underneath while pausing its `Update` and setting `PointerEventsNone` on the paused layer. When `HostOptions.Viewport` is set, transparent pushed 2D overlays treat a zero `BarColor` as `core.Transparent` so margins do not obscure the 3D scene. Call `mgr.Invalidate()` to redraw mounted 2D canvases on demand without remounting 3D views.

For a complete runnable game that switches between a 2D courtyard overworld (`scene.Scene` + `sprite.Batch`) and a 3D glTF shrine (`gpu.View`), pushes a transparent 2D pause overlay over the 3D view, and shares one `*input.Actions` and `*sound.Mixer` across transitions, see [`examples/lantern3d`](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d).

## Physics in scene files

Scenes authored in YAML with [`game/sceneformat`](editor.md#built-in-physics-components-817) can declare 2D (`body2d`, `collider2d`, `trigger2d`) and 3D (`body3d`, `collider3d`, `trigger3d`) physics components directly on objects, using named collision layers from [`game.yaml`](project.md). Pass a built scene to `sceneformat.BuildPhysics` to instantiate its `physics.World`, `physics.RigidWorld`, and `physics3d.World` and step them each fixed tick alongside your scene.
