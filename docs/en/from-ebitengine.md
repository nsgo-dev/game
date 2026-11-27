# Coming from Ebitengine

A map from Ebitengine's game loop, images, input, audio and builds to this layer, and to the scenes, nodes, resources, signals, input maps, physics and export it adds on top, with the page that covers each one.

Both are Go, so most of your game logic moves over unchanged. What changes is the host: an Ebitengine game owns the whole window and draws everything on the GPU, while a game here is an NSGo component that draws into one `core.Canvas`, with its HUD and menus as native NSGo views on top. Ebitengine leaves scenes, entities, events and physics to you; this layer ships packages for each, which you can adopt one at a time.

## At a glance

| Ebitengine | Here | Guide |
| --- | --- | --- |
| `ebiten.Game.Update` at `ebiten.TPS()` | `game.Loop.Update(dt)` at `Loop.Rate` | [Loop](loop.md) |
| `ebiten.Game.Draw(screen)` | `core.Canvas.Draw(d, size)`, redrawn from `Loop.Draw(alpha)` | [Loop](loop.md) |
| `Layout(outsideWidth, outsideHeight)` | `screen.Viewport` with `ModeFit`, `ModeFill`, `ModeStretch` or `ModeExpand` | [screen](screen.md) |
| `*ebiten.Image`, `SubImage` | `sprite.Atlas` and its `Frame` values | [sprite](sprite.md) |
| `DrawImageOptions.GeoM`, `ColorScale` alpha | `sprite.Sprite` position, origin, rotation, scale, flip and `Alpha` | [sprite](sprite.md) |
| A hand-written scene switcher | `scene.Manager` with transitions | [scene](scene.md) |
| A slice of structs you update yourself | `entities.World`, components and systems | [Entities](entities.md) |
| `//go:embed` plus `ebitenutil.NewImageFromFile` | `"asset:"` URIs in `resources/` loaded via `assets.Manager` (optional `assets.WithFS` for `//go:embed`) | [assets](assets.md) |
| Channels or callbacks between systems | `flow.Bus`; `core.Signal` for UI state | [flow](flow.md) |
| `inpututil.IsKeyJustPressed`, touch IDs | `input.Actions` with `JustPressed` and touch regions | [input](input.md) |
| Your own AABB checks | `physics` shapes, `MoveAndSlide`, `RigidWorld` | [physics](physics.md) |
| `audio.Context`, `audio.Player` | `sound.Mixer`, `Effect` and `Music` | [sound](sound.md) |
| `go build`, `GOOS=js GOARCH=wasm`, `ebitenmobile bind` | `go tool nsgo build` for every target | [Building and Shipping](shipping.md) |

## The loop

Ebitengine calls `Update` 60 times a second by default and `Draw` once per display frame. `game.Loop` keeps the same split: `Update` gets a fixed `dt` of `1/Rate` seconds (60 updates a second by default), and `Draw` runs once per display frame with `alpha`, how far the frame falls into the next update. Pass `alpha` to the canvas through a `core.Signal` and interpolate between the last two states, so motion stays smooth on 120 Hz displays without changing the update rate. `Loop` also pauses while the app is in the background and caps the updates one frame may run after a hitch. See [Loop](loop.md).

`ebiten.ActualTPS` and `ActualFPS` have no direct counterpart; the [debug](debug.md) overlay draws a frame-time graph, and `nsgo dev -fps` shows NSGo's monitor.

## Drawing

An Ebitengine `Draw` receives the screen image; a canvas `Draw` receives a `*core.Drawing` and the view's size in points. A sprite sheet becomes a `sprite.Atlas` loaded from a TexturePacker or Aseprite export, or written by hand, and `SubImage` rectangles become its named frames and animations. Where you set `GeoM` translate, rotate and scale, set the fields on a `sprite.Sprite`; a `sprite.Batch` draws many sprites from one atlas in `Z` order. See [sprite](sprite.md).

`Layout` becomes a `screen.Viewport`: give it the virtual resolution, choose how it fits the window, and draw inside `vp.Draw`. Integer scaling for pixel art and safe-area rectangles live in the same package. See [screen](screen.md). For maps, `tilemap` loads Tiled `.tmj` files and draws only the visible tiles through a 2D camera. See [tilemap](tilemap.md).

## Scenes

Ebitengine has no scene type, so games usually keep a `current Scene` field and swap it. `scene.Manager` does that with a stack: `Switch` replaces the scene with a fade, crossfade or wipe, `Push` lays an overlay such as a pause menu over it, and `Pop` returns. Each scene gets its own `*core.Scope`, and everything made on it is disposed when the scene exits. A `scene.Scene` inside it orders layers in world space (under a camera) and screen space (for the HUD), and hit-tests taps against sprites. See [scene](scene.md).

A level's layout can live in a YAML scene file rather than in Go: `sceneformat.Parse` reads it and `sceneformat.Build` produces layers ready to draw. See [Editor & Scene Format](editor.md).

## Nodes

Where an Ebitengine game keeps `[]*Enemy` and loops over it, `entities` keeps components in typed sparse sets on a `World` and runs systems in a declared order on each fixed step, with no allocations in steady state. `Prefab` stamps a template's components onto new entities, and a world saves to JSON through [save](save.md). See [Entities](entities.md).

For 3D, `scene3d.Node` is a transform hierarchy with sockets, tags and picking. See [scene3d](scene3d.md).

## Resources

Put images, sounds, atlas JSON, tilemaps, models and other runtime files under `resources/`; NSGo packages them for every target, and `"asset:..."` URIs open them everywhere — including the web, fetched on demand through `core.Scope.ReadAsset` without embedding them in the binary. `assets.Manager` reads `"asset:"` bytes from the app's resources by default (pass `assets.WithFS(fsys)` to override with `//go:embed` or `os.DirFS`, or `WithReadAsset` for custom storage), loads atlases, sounds, images and your own parsed files in named groups off the engine goroutine, exposes progress for a loading screen and releases a group when its scene ends. See [assets](assets.md).

## Signals

Systems that call each other or share channels can publish typed events on a `flow.Bus` instead: `flow.Subscribe` takes a function of the event's type, `flow.Publish` delivers at once and `flow.Enqueue` with `Bus.Flush` delivers once per step. `flow.Timers`, `flow.Machine` and `flow.Seq` replace frame counters, state switch statements and cutscene goroutines, and all of them advance only on the loop's `dt`, so they pause and replay with the game. See [flow](flow.md).

State the HUD shows, such as a score, is a [`core.Signal`](https://pkg.go.dev/github.com/nsgo-dev/nsgo/core#Signal) that an NSGo view reads and re-renders on.

## Input maps

Ebitengine asks about devices directly (`ebiten.IsKeyPressed`, `inpututil.IsMouseButtonJustPressed`, `ebiten.AppendTouchIDs`). `input.Actions` names actions instead and binds inputs to them, so the game code asks about `"jump"`, not a key:

| Ebitengine | Here |
| --- | --- |
| `ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)` | `actions.Bind("fire", input.MouseButton(core.MouseButtonLeft))`, then `actions.Pressed("fire")` |
| `inpututil.IsMouseButtonJustPressed` | `actions.JustPressed` |
| `inpututil.IsMouseButtonJustReleased` | `actions.JustReleased` |
| Checking touch positions against a button's rectangle | `input.TouchRegion(rect)`, or a `VirtualPad` |
| Two opposing inputs combined into a direction | `actions.BindAxis("move_x", "left", "right")` and `actions.Axis` |

Call `actions.Step()` at the start of each `Update`; a tap that starts and ends between two updates still registers for one. `actions.Save` and `Load` keep remapped bindings in `core.Storage`. Key and gamepad bindings are tracked in [#3](https://github.com/nsgo-dev/game/issues/3); until they land, a key bound with NSGo's `cx.OnKey` can call `actions.Trigger(name)`, which registers the action for one step. See [input](input.md).

## Physics

Ebitengine has no physics, so this is new rather than a port. [physics](physics.md) holds box and circle overlap tests, `MoveAndSlide` for platformer bodies against a tile grid with one-way platforms, trigger areas, and raycasts. [2D Rigid Bodies](physics2d-rigid.md) adds a deterministic rigid-body solver with distance, spring and revolute joints, and [physics3d](physics3d.md) covers 3D bodies and a capsule character controller. All of them step on the loop's fixed `dt`. The 2D packages work in the same Y-down screen space the drawing uses, while physics3d works in the 3D space: right-handed, Y up, units in metres.

## Export

| Ebitengine | Here |
| --- | --- |
| `go run .` | `go tool nsgo dev`, which also reloads on save |
| `GOOS=js GOARCH=wasm go build` and a `wasm_exec.js` page | `go tool nsgo build -target web` |
| `ebitenmobile bind` and an Xcode or Android Studio project | `go tool nsgo build -target ios` or `android` |
| Bundling a `.app` or installer yourself | `go tool nsgo build`, which writes `.dmg`, NSIS installers, `.deb`, `.AppImage`, `.apk` and `.ipa` |

App name, identifier, version, icons and targets live in `nsgo.yaml`, and `-channel store` builds store packages. See [Building and Shipping](shipping.md), and [Getting Started](getting-started.md) for a first project.
