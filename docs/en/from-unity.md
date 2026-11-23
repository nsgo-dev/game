# Coming from Unity

A map from Unity's scenes, GameObjects and components, assets, events, Input System, physics and builds to this layer, with the page that covers each one.

A game here is Go code inside an NSGo component rather than C# scripts attached in an editor. Scenes, prefabs and settings are YAML text with a version field and readable object IDs such as `crate_3` that stay the same from save to save, where a `.unity` file refers to objects by opaque `fileID` numbers, so a change to a scene reads as a small diff. HUD and menus are native NSGo views over the game's canvas, where Unity would use uGUI or UI Toolkit.

## At a glance

| Unity | Here | Guide |
| --- | --- | --- |
| `FixedUpdate`, `Time.fixedDeltaTime` | `game.Loop.Update(dt)` with a fixed `dt`, `Loop.Rate` | [Loop](loop.md) |
| `Update`, `LateUpdate`, rendering | `game.Loop.Draw(alpha)` and the `core.Canvas` it redraws | [Loop](loop.md) |
| `.unity` scene | YAML scene document (`sceneformat`) | [Editor & Scene Format](editor.md) |
| `SceneManager.LoadScene`, additive loading | `scene.Manager.Switch`, `Push` and `Pop` | [scene](scene.md) |
| GameObject with components, `MonoBehaviour` | `entities.World` entities, components and systems | [Entities](entities.md) |
| `Transform` hierarchy | `scene3d.Node` tree | [scene3d](scene3d.md) |
| Prefab, `Instantiate`, `Destroy` | `entities.Prefab`, `World.Spawn`, `World.Destroy` | [Entities](entities.md) |
| `Resources.Load`, Addressables | `"asset:"` URIs for images and sounds, `assets.Manager` groups over an embedded file system | [assets](assets.md) |
| C# events, `UnityEvent` | `flow.Bus`; `core.Signal` for UI state | [flow](flow.md) |
| Coroutines, `Invoke`, `InvokeRepeating` | `flow.Seq`, `flow.Timers.After` and `Every` | [flow](flow.md) |
| Input System action maps | `input.Actions` | [input](input.md) |
| `Rigidbody2D`, `Collider2D`, joints | `physics` bodies, `RigidWorld`, joints | [physics](physics.md), [2D Rigid Bodies](physics2d-rigid.md) |
| `Rigidbody`, `CharacterController` | `physics3d.World`, `physics3d.CharacterController` | [physics3d](physics3d.md) |
| Build Settings, Player Settings | `nsgo.yaml` and `go tool nsgo build` | [Building and Shipping](shipping.md) |
| `PlayerPrefs` | `core.Storage`, `save` slots | [save](save.md) |

## Scenes

A scene document is YAML with `version: 1`: a virtual resolution, a background colour, a camera, tilemaps and ordered layers of objects. Each object has an `id`, a transform, an optional sprite, an optional `prefab` path and `components` and `properties` maps for the values you would show in the Inspector. `sceneformat.Parse` refuses unknown fields and bad transforms, `Encode` writes the same bytes for the same scene, and `Build` turns the document into layers ready to draw. See [Editor & Scene Format](editor.md).

At runtime `scene.Manager` decides which scene runs. `Switch` is `LoadScene` with a `Fade`, `Crossfade` or `Wipe` transition; `Push` with a `Transparent` controller lays a pause menu over the current scene, as additive loading of a UI scene would; `Pop` unloads it. Each scene's `Enter` gets a child `*core.Scope`, and loops, timers, sounds and signals made on it are disposed when the scene exits, so a scene leaves nothing behind. See [scene](scene.md).

## GameObjects and components

Unity attaches behaviour to objects; here data and behaviour are apart, as in Unity's DOTS:

- An `entities.Entity` is a handle with a generation, so a destroyed one is never mistaken for a new one (Unity's "destroyed object" checks).
- Components are plain Go structs: `entities.Set(world, e, Health{HP: 10})`, `Get`, `Has` and `Remove`.
- A `MonoBehaviour`'s `FixedUpdate` becomes a system that runs over every entity with the components it needs (`entities.Each2[Position, Velocity]`). `Scheduler.AddSystem` with `Before` and `After` fixes the order, where Unity uses Script Execution Order.
- A prefab is an `entities.Prefab`: `With` adds a component value, `Clone` makes a variant, `Spawn` and `SpawnN` instantiate.

See [Entities](entities.md). For 3D transforms, `scene3d.Node` holds a parent, children and a local position, rotation and scale; sockets stand in for attaching a weapon to a bone, and `FindByName` and `FindByTag` for `GameObject.Find` and tags. See [scene3d](scene3d.md).

Coordinates differ from Unity's. 2D drawing and 2D physics use screen space: X right, Y down, rotation clockwise, units in points. 3D is right-handed with Y up and `-Z` forward, while Unity is left-handed with `+Z` forward, so negate Z when you move positions and look directions across. One 3D unit is a metre in both. See [math3d](math3d.md) and [3D Models from Blender](3d-models-blender.md).

## Assets

Images and sounds loaded at runtime live in `resources/` and open by `"asset:<path>"`, such as `core.Src("asset:hero.png")`, on every target, as `Resources.Load` paths do. Data that `assets.Manager` reads itself, such as atlas JSON, raw bytes and your own parsed files, comes from the file system you give it: embed it with `//go:embed` and pass `assets.NewManager(cx, assets.WithFS(fsys))`, or pass `WithReadAsset` for custom storage. `assets.Manager` groups assets per scene, loads a group off the engine goroutine with progress you can bind to a loading screen, and releases it when the scene ends, the part of Addressables most games use. See [assets](assets.md).

There is no asset database or `.meta` file. Files are read as their tools write them: TexturePacker and Aseprite atlases ([sprite](sprite.md)), Tiled maps ([tilemap](tilemap.md)) and glTF models ([3D Models from Blender](3d-models-blender.md)). A `ScriptableObject` becomes a YAML file decoded into a Go struct. `PlayerPrefs` becomes `core.Storage`, and saved games are versioned JSON slots with migrations and a backup in [save](save.md).

## Events

- **C# events and `UnityEvent`** become a typed `flow.Bus`. The event is a struct type: `flow.Subscribe(&bus, func(e EnemyDefeated) {...})` adds a listener and returns the function that removes it, `flow.Publish` raises the event at once, and `flow.Enqueue` with `Bus.Flush` holds events raised during physics until the end of the step. `SendMessage` and string-keyed messaging have no counterpart. See [flow](flow.md).
- **Coroutines** become step sequences that a `flow.Runner` advances on each fixed step, with no goroutines: `flow.Seq(flow.Wait(time.Second), flow.Do(spawn))` is `yield return new WaitForSeconds(1)`, and `flow.WaitUntil` is `WaitUntil`. They pause with the game and replay the same way. `Invoke` and `InvokeRepeating` become `flow.Timers`.
- **Animator state machines** for gameplay become `flow.Machine`; for 3D animation, [anim3d](anim3d.md) has state machines and blend spaces, and [tween](tween.md) covers what DOTween is used for.
- **UI bindings**: a value the HUD shows is a [`core.Signal`](https://pkg.go.dev/github.com/nsgo-dev/nsgo/core#Signal), and a view that reads it re-renders when it changes.

## Input System

An `input.Actions` value is an action map: names bound to inputs, read once per fixed step.

| Unity Input System | Here |
| --- | --- |
| An action with bindings | `actions.Bind("jump", input.TouchRegion(rect), input.MouseButton(core.MouseButtonLeft))` |
| `action.IsPressed()` | `actions.Pressed("jump")` |
| `action.WasPressedThisFrame()` | `actions.JustPressed("jump")` |
| `action.WasReleasedThisFrame()` | `actions.JustReleased("jump")` |
| A 1D axis composite | `actions.BindAxis("move_x", "left", "right")`, then `actions.Axis("move_x")` |
| On-Screen Stick and Button | `input.VirtualPad` |
| Saving rebinding overrides | `actions.Save` and `actions.Load` in `core.Storage` |

Call `actions.Step()` at the start of each update. Bindings today are touch regions, mouse buttons and device motion. Key and gamepad bindings are tracked in [#3](https://github.com/nsgo-dev/game/issues/3); until they land, a key bound with NSGo's `cx.OnKey` can call `actions.Trigger(name)`, which registers the action for one step. See [input](input.md).

## Physics

Everything steps on the loop's fixed `dt`, in a fixed order, so the same inputs give the same result run after run (see [Determinism](determinism.md)).

| Unity | Here |
| --- | --- |
| `Rigidbody2D` (dynamic) with `BoxCollider2D` or `CircleCollider2D` | `physics.RigidBody` in a `physics.RigidWorld` |
| `HingeJoint2D`, `SpringJoint2D`, `DistanceJoint2D` | `RevoluteJoint`, `SpringJoint`, `DistanceJoint` |
| A kinematic platformer controller | `physics.MoveAndSlide` against a `TileGrid`, with `Grounded` and wall flags |
| `isTrigger` and `OnTriggerEnter2D` | `physics.Trigger` with `OnEnter`, `OnStay` and `OnExit`, run by `World.StepTriggers` |
| `Physics2D.Raycast`, `OverlapBox` | `World.RaycastFirst`, `World.QueryOverlap` |
| Layers and the collision matrix | `Layer` and `Mask` (`LayerMask`) on each body and trigger |
| `Rigidbody` with box, sphere or capsule colliders | `physics3d.World` with `Body` values |
| `CharacterController.Move` | `physics3d.CharacterController.Move`, with slope limits and steps |

See [physics](physics.md), [2D Rigid Bodies](physics2d-rigid.md) and [physics3d](physics3d.md). Moving colliders are boxes, circles, spheres and capsules. For static 3D level geometry, `MeshCollider` maps to `physics3d.TriMeshCollider` added with `World.AddTriMesh`, and `TerrainCollider` to `physics3d.HeightfieldCollider` added with `World.AddHeightfield`.

## Builds

Build Settings and Player Settings become `nsgo.yaml` at the project root: product name, bundle identifier, version, build number, icons and the list of targets. There are no platform modules to install, because Go cross-compiles; iOS and signed macOS builds need a Mac with Xcode.

| Unity | Here |
| --- | --- |
| Play mode | `go tool nsgo dev`, with `-target web`, `ios_simulator` or `android` |
| Build and Run | `go tool nsgo build -run` |
| Build for a platform | `go tool nsgo build -target macos,windows,linux,web,ios,android` |
| Store builds (`.aab`, Mac App Store) | `go tool nsgo build -channel store` |

`go tool nsgo check -target all` vets the game for every target before a release. See [Building and Shipping](shipping.md), and [Getting Started](getting-started.md) for a first project.
