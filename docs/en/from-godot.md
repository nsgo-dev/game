# Coming from Godot

A map from Godot 4's scenes, nodes, resources, signals, input map, physics and export to this layer, with the page that covers each one.

The biggest change is where a game lives. In Godot you assemble scenes in the editor and attach GDScript to nodes; here a game is Go code inside an NSGo component, and its scenes, prefabs and settings are YAML text files that you write by hand or save from the [editor](editor.md). Godot's habit of keeping scenes and resources as text (`.tscn`, `.tres`) carries over: every file you write is text you can read and diff in review.

## At a glance

| Godot | Here | Guide |
| --- | --- | --- |
| `_physics_process(delta)`, physics ticks per second | `game.Loop.Update(dt)` with a fixed `dt`, `Loop.Rate` | [Loop](loop.md) |
| `_process(delta)` | `game.Loop.Draw(alpha)` and the `core.Canvas` it redraws | [Loop](loop.md) |
| `.tscn` scene, `PackedScene` | YAML scene document (`sceneformat`) built into layers | [Editor & Scene Format](editor.md) |
| `get_tree().change_scene_to_file()` | `scene.Manager.Switch` with a transition | [scene](scene.md) |
| `Node2D` tree, `CanvasLayer` | `scene.Scene` layers in world or screen space | [scene](scene.md) |
| `Node3D` tree, groups | `scene3d.Node` tree, `FindByTag` | [scene3d](scene3d.md) |
| Instantiating a scene as a template | `entities.Prefab`, or a scene object's `prefab` field | [Entities](entities.md) |
| `Resource`, `.tres`, `ResourceLoader` | YAML resource files in a `resource.Library`; images and sounds under `resources/`; data through `assets.Manager` with `WithFS` | [resource](resources.md), [assets](assets.md) |
| Signals, `connect`, `emit` | `flow.Bus` with `Subscribe` and `Publish`; `core.Signal` for UI state | [flow](flow.md) |
| `[connection]` in a `.tscn` | A scene file's `connections`, run by `sceneformat.Connect` and `Signals.Emit`, or deferred, as `CONNECT_DEFERRED` does, with `Signals.Enqueue` and `Flush`; `SpawnConnected` binds them to entities | [Editor & Scene Format](editor.md#connections-490) |
| `Timer` node, `await` on a signal | `flow.Timers`, `flow.Seq` and `flow.Wait` | [flow](flow.md) |
| `Tween` | `tween.Tween`, `tween.Timeline` | [tween](tween.md) |
| Input Map, `Input.is_action_just_pressed` | `input.Actions`, `JustPressed` | [input](input.md) |
| `CharacterBody2D.move_and_slide()` | `World.MoveAndSlide` / `MoveAndSlideWith` against bodies, shapes, tiles and static rigid bodies (`physics.MoveAndSlide` for tiles alone; `platformer.Controller.StepWorld` for platformers) | [physics](physics.md), [platformer](platformer.md) |
| `RigidBody2D`, joints | `physics.RigidWorld`, `RigidBody`, joints | [2D Rigid Bodies](physics2d-rigid.md) |
| `CharacterBody3D`, `RigidBody3D` | `physics3d.CharacterController`, `physics3d.World` | [physics3d](physics3d.md) |
| Export presets, export templates | `nsgo.yaml` and `go tool nsgo build` | [Building and Shipping](shipping.md) |

## Scenes

A Godot scene is a saved node tree you instance or switch to. Here the two jobs are split:

- **What a scene contains** is a YAML document with `version: 1`: a camera, tilemaps and ordered layers of placed objects, each with a transform, a sprite and free-form `components` and `properties` (the role of exported variables). `sceneformat.Parse` reads it and refuses unknown fields; `sceneformat.Build` turns it into layers ready to draw. See [Editor & Scene Format](editor.md).
- **Which scene runs** is `scene.Manager`, which holds a stack of `scene.Controller` values. `Switch` replaces the current scene with a `Fade`, `Crossfade` or `Wipe` transition, `Push` lays a pause menu over it and `Pop` returns. See [scene](scene.md).

`Controller.Enter` receives a child `*core.Scope` of its own. Loops, timers, sounds and signals made on it are disposed when the scene exits, which covers what `_ready`, `_exit_tree` and `queue_free` do for a scene's own objects.

Autoloads have a place in `game.yaml`: its `autoload` section lists the scenes and resources to load at startup and names the main scene, and `LoadAutoloads` reads and validates them all before the first frame (see [Reading it in a game](project.md#reading-it-in-a-game)). Any other singleton is a plain Go value: make it once in the root component and pass it to the scenes that need it.

## Nodes

There is no single node class that everything inherits from. Pick the piece that fits:

- **2D drawing**: a `scene.Scene` holds layers; a layer in `SpaceWorld` draws under the scene's camera (like `Node2D` under a `Camera2D`) and a layer in `SpaceScreen` draws in view space (like a `CanvasLayer`). Layers hold tilemaps, sprite batches and `scene.Drawable` values with their own `Draw`, `Update` and hit-test hooks. See [scene](scene.md), [sprite](sprite.md) and [tilemap](tilemap.md).
- **3D hierarchy**: `scene3d.Node` has a parent, children, a local transform, tags (Godot's groups, found with `FindByTag`), named sockets for attachments and typed values set with `SetAttachment`. See [scene3d](scene3d.md).
- **Game objects with behaviour**: instead of a script per node, keep data in components on an `entities.World` and write systems that run in a declared order each fixed step. `Prefab` stamps the same components onto new entities, as instancing a small scene does. See [Entities](entities.md).
- **HUD and menus**: Godot's `Control` nodes become ordinary NSGo views layered over the canvas, or [gameui](gameui.md) widgets drawn inside it.

2D coordinates match Godot's: X right, Y down, rotation clockwise in radians. 3D matches too: right-handed, Y up, `-Z` forward, one unit a metre (see [math3d](math3d.md) and [3D Models from Blender](3d-models-blender.md)).

## Resources

Godot's `.tres` resources map to [resource](resources.md) files: one YAML document each, with a `type` and `version` header, for materials, curves, gradients, animation clips, UI themes, sound bus layouts and physics materials. A `resource.Library` loads them from an `fs.FS` by path and keeps one value per path, as `ResourceLoader` caches a loaded resource.

Images and sounds the game loads at runtime go in `resources/` and are opened by `"asset:<path>"` URIs, such as `core.Src("asset:hero.png")`, on every target. Data that `assets.Manager` reads itself, such as atlas JSON, raw bytes and your own parsed files, comes from the file system you give it: embed it with `//go:embed` and pass `assets.NewManager(cx, assets.WithFS(fsys))`, or pass `WithReadAsset` for custom storage. Without either, the manager reads an `assets/` directory relative to the working directory, which works only for desktop runs from the project root. `assets.Manager` loads atlases, sounds, images, bytes and your own parsed files in named groups off the engine goroutine, reports progress as signals for a loading screen and releases a group when its scene ends. See [assets](assets.md).

Files are read in the form the tool that made them writes, with no import step between them and the game, except where `game assets` builds them: it packs atlases, encodes KTX2 textures, converts sounds, bakes fonts into bitmap fonts and packs glTF models into GLB files, and the `.import` files map to its import settings sidecars, YAML files such as `hero.png.import.yaml` beside each image, sound, font or model that hold its compression, filter, sprite sheet slicing, loop points, font size and characters, or model scale ([Command Line](cli.md#assets)). Files the game reads as they are include TexturePacker and Aseprite atlases ([sprite](sprite.md)), Tiled maps ([tilemap](tilemap.md)) and glTF models ([3D Models from Blender](3d-models-blender.md)). For your own `Resource` subclasses, write a YAML or JSON file and decode it into a Go struct, refusing unknown fields as the scene format does.

Saved games take the place of `ConfigFile` and `FileAccess` writes: [save](save.md) keeps versioned JSON slots with migrations and a backup.

## Signals

Godot's signals do two jobs here, each with its own tool:

- **Gameplay events** between systems go through a typed `flow.Bus`. A struct type is the signal; `flow.Subscribe` connects, `flow.Publish` emits at once, `flow.Enqueue` with `Bus.Flush` emits later (Godot's deferred connection) and `flow.SubscribeOnce` is a one-shot connection. Nothing is keyed by a string, so a misspelt event does not compile. See [flow](flow.md).
- **State a view shows**, such as a score in the HUD, is a [`core.Signal`](https://pkg.go.dev/github.com/nsgo-dev/nsgo/core#Signal). A view that reads it re-renders when it changes, without a handler wired by hand.

Waiting with `await` becomes a step sequence: `flow.Seq(flow.Wait(d), flow.Do(fn))` run by a `flow.Runner` each fixed step, with no goroutines. The `Timer` node becomes `flow.Timers.After` and `Every`. Area callbacks such as `body_entered` are covered under physics below.

## Input map

Godot's Input Map becomes an `input.Actions` value: bind names to inputs, call `Step` once at the start of each fixed update and ask about that step.

| Godot | Here |
| --- | --- |
| Adding an action in Project Settings | `actions.Bind("jump", input.TouchRegion(rect), input.MouseButton(core.MouseButtonLeft))` |
| `Input.is_action_pressed("jump")` | `actions.Pressed("jump")` |
| `Input.is_action_just_pressed("jump")` | `actions.JustPressed("jump")` |
| `Input.is_action_just_released("jump")` | `actions.JustReleased("jump")` |
| `Input.get_axis("left", "right")` | `actions.BindAxis("move_x", "left", "right")`, then `actions.Axis("move_x")` |
| `TouchScreenButton` | `input.TouchRegion`, or a `VirtualPad` of on-screen sticks and buttons |
| Saving remapped controls | `actions.Save` and `actions.Load` in `core.Storage` |

Bindings today are touch regions, mouse buttons and device motion. Key and gamepad bindings are tracked in [#3](https://github.com/nsgo-dev/game/issues/3); until they land, a key bound with NSGo's `cx.OnKey` can call `actions.Trigger("pause")`, which registers the action for one step. See [input](input.md).

## Physics

The 2D physics package works in the same screen space as the drawing and steps on the loop's fixed `dt`.

- **`CharacterBody2D`**: `World.MoveAndSlide(body, dt)` and `World.MoveAndSlideWith(body, dt, opts)` sweep a `Body`'s shape against static and kinematic bodies (including `Body.OneWay` platforms), solid and one-way tiles, and static `RigidBody` colliders, sliding along non-axis-aligned normals and returning `Collisions` plus `OnFloor`, `OnWall` and `OnCeiling` with configurable `UpX`/`UpY`, `FloorMaxAngle` and `MotionFloating` (top-down) mode — matching Godot's `move_and_slide()`, `is_on_floor()`, `get_slide_collision()`, `up_direction` and `motion_mode`. `physics.MoveAndSlide(body, grid, dt)` remains available for tile-only movement, and `CharacterBody.MoveWorld` / `platformer.Controller.StepWorld` add slopes and moving platforms. See [physics](physics.md) and [platformer](platformer.md).
- **`Area2D`**: a `physics.Trigger` added with `World.AddTrigger` calls `OnEnter`, `OnStay` and `OnExit` from `World.StepTriggers`, as `body_entered` and `body_exited` do.
- **`RayCast2D` and `intersect_ray`**: `World.RaycastFirst` and `World.Raycast`; `World.QueryOverlap` and `QueryPoint` cover shape and point queries.
- **Collision layers and masks**: the `Layer` and `Mask` fields (`LayerMask`) on bodies and triggers.
- **`RigidBody2D` and joints**: `physics.RigidWorld` with `RigidBody` boxes and circles; `RevoluteJoint` is a `PinJoint2D`, `SpringJoint` a `DampedSpringJoint2D` and `DistanceJoint` a fixed rod. See [2D Rigid Bodies](physics2d-rigid.md).
- **3D**: `physics3d.CharacterController` walks a capsule over slopes and stairs as `CharacterBody3D` does, and `physics3d.World` steps rigid boxes, spheres and capsules. See [physics3d](physics3d.md).

2D shapes are boxes, circles, convex polygons (up to 8 vertices, optionally rounded), capsules, segments and chains (see [physics](physics.md#polygons-capsules-segments-and-chains)); a concave outline is built from several convex polygons or, for static level edges, a chain. Scene files can place only box and circle colliders so far (#1379). In 3D, bodies are boxes, spheres and capsules, and static level geometry can collide as a triangle mesh or a heightfield: `ConcavePolygonShape3D` maps to `physics3d.TriMeshCollider` added with `World.AddTriMesh`, and `HeightMapShape3D` to `physics3d.HeightfieldCollider` added with `World.AddHeightfield`.

## Export

Export presets become `nsgo.yaml` at the project root: the app's name, identifier, version, icons and the targets to build. There are no export templates to download, because Go cross-compiles; only iOS and signed macOS builds need a Mac with Xcode.

| Godot | Here |
| --- | --- |
| Running the project from the editor | `go tool nsgo dev`, with `-target web`, `ios_simulator` or `android` |
| `export_presets.cfg` | `nsgo.yaml` (`build.targets`, `icon`, `id`, `version`) |
| `godot --export-release` | `go tool nsgo build` |
| Export for a store | `go tool nsgo build -channel store` |

`go tool nsgo check -target all` vets the game for every target before a release. See [Building and Shipping](shipping.md), and [Getting Started](getting-started.md) for a first project.
