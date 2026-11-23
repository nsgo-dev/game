# Overview

game is a game layer for [NSGo](https://nsgo.dev) apps, written in Go. A game draws into one NSGo view, a `Canvas`, a pixel buffer or a shader, and its HUD and menus stay ordinary native NSGo views on top. It uses only NSGo's public API (`core`, `audio`, and later `gamepad`), so an app that does not import it links none of it.

> The project is early: the packages below work and are tested, but their APIs will change. Start with [Getting Started](getting-started.md) to build and run a game, then follow the [tutorial](tutorial.md) to build a whole one.

## Stages

1. **2D**: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound, drawn at 60 fps or the display's rate.
2. **3D**: meshes, cameras, lights and glTF models drawn with depth on a GPU view NSGo adds for it ([nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)). See [3D Models from Blender](3d-models-blender.md) for coordinate conventions, Blender 4.x export settings, FBX/OBJ conversion and sample glTF assets.

The loop, input actions, sound and scene switching are shared by both stages, so 3D adds drawing without replacing them. The plan is [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047); its tasks are this repository's issues and the engine primitives NSGo adds for it.

## Packages

| Package | Import path | Guide | What it holds |
| --- | --- | --- | --- |
| `game` | `github.com/nsgo-dev/game/packages/game` | [Loop](loop.md) | `Loop`, the fixed-step loop every package shares |
| `sprite` | `github.com/nsgo-dev/game/packages/game/sprite` | [sprite](sprite.md) | Atlases, frames, animations, sprites and batches drawn into a `Canvas` |
| `sound` | `github.com/nsgo-dev/game/packages/game/sound` | [sound](sound.md) | The `Mixer`, its effects and music |
| `input` | `github.com/nsgo-dev/game/packages/game/input` | [input](input.md) | Named actions and axes from touch regions, mouse buttons, keyboard keys, pointer-lock mouselook and game controllers, persisted in `core.Storage` |
| `tilemap` | `github.com/nsgo-dev/game/packages/game/tilemap` | [tilemap](tilemap.md) | 2D `Camera`, culled tilemaps, Tiled `.tmj` loader, animated tiles and raycasts |
| `scene` | `github.com/nsgo-dev/game/packages/game/scene` | [scene](scene.md) | World/screen layers, hit testing, scene stacks and animated transitions |
| `physics` | `github.com/nsgo-dev/game/packages/game/physics` | [physics](physics.md) | `Box`/`Circle` overlaps, `MoveAndSlide` tile resolution and `World` queries |
| `platformer` | `github.com/nsgo-dev/game/packages/game/platformer` | [platformer](platformer.md) | Platformer `Controller` on `CharacterBody`: coyote time, jump buffer, variable jump height, wall slide and wall jump from a YAML config |
| `screen` | `github.com/nsgo-dev/game/packages/game/screen` | [screen](screen.md) | Virtual resolution `Viewport` scaling, pixel snapping, safe-area rectangles and video settings |
| `assets` | `github.com/nsgo-dev/game/packages/game/assets` | [assets](assets.md) | Async per-scene asset groups, reactive progress and `LoadingScreen` |
| `resource` | `github.com/nsgo-dev/game/packages/game/resource` | [resource](resources.md) | Shared YAML resource files (materials, curves, gradients, animation clips, UI themes, bus layouts, physics materials) and a `Library` that shares and reloads them |
| `text` | `github.com/nsgo-dev/game/packages/game/text` | [text](text.md) | AngelCode BMFont bitmap fonts and platform text via `DrawPlatform` |
| `particles` | `github.com/nsgo-dev/game/packages/game/particles` | [particles](particles.md) | Seeded deterministic 2D `Emitter`, spawn shapes, bursts and lifetime curves |
| `save` | `github.com/nsgo-dev/game/packages/game/save` | [save](save.md) | Versioned JSON save slots, schema migrations and atomic backup recovery |
| `achievements` | `github.com/nsgo-dev/game/packages/game/achievements` | [achievements](achievements.md) | Achievements declared in versioned YAML, monotonic progress persisted in `core.Storage`, and reporting to Game Center and Play Games |
| `datatable` | `github.com/nsgo-dev/game/packages/game/tooling/datatable` | [datatable](datatable.md) | Typed data tables in versioned YAML (and CSV/JSON) with cell-level errors, deterministic saves and hot reload |
| `debug` | `github.com/nsgo-dev/game/packages/game/debug` | [debug](debug.md) | Frame-time graph, wireframe shapes, scene inspector and `-tags release` stripping |
| `flow` | `github.com/nsgo-dev/game/packages/game/flow` | [flow](flow.md) | Game-clock `Timers`, `Machine[S]`, `Seq`/`Parallel`/`Race` and typed event `Bus` |
| `stats` | `github.com/nsgo-dev/game/packages/game/stats` | [stats](stats.md) | Base stats with add/mul/override modifiers from sources, recomputed on change, timed modifiers on the fixed step and a YAML stats file |
| `tween` | `github.com/nsgo-dev/game/packages/game/tween` | [tween](tween.md) | Fixed-step `Tween`, `Timeline` and camera choreography helpers |
| `ai` | `github.com/nsgo-dev/game/packages/game/ai` | [ai](ai.md) | Grid `AStar`, 2D `NavMesh`, steering behaviors and behavior trees |
| `rts` | `github.com/nsgo-dev/game/packages/game/rts` | [rts](rts.md) | Flow fields, a `Crowd` of many units with separation, YAML formations, box/click selection and control groups |
| `replay` | `github.com/nsgo-dev/game/packages/game/replay` | [replay](replay.md) | Input `Recorder`/`Player` with editable YAML recordings and deterministic test assertions |
| `statedump` | `github.com/nsgo-dev/game/packages/game/statedump` | [statedump](statedump.md) | Headless runs dumped as deterministic YAML (entities, components, scene stack, values) and a line `Diff` between runs |
| `cutscene` | `github.com/nsgo-dev/game/packages/game/cutscene` | [cutscene](cutscene.md) | Skippable video cutscenes over the game's view, in a scene or between scenes, from YAML files |
| `rhythm` | `github.com/nsgo-dev/game/packages/game/rhythm` | [rhythm](rhythm.md) | YAML beat maps with tempo and meter changes, a music clock on the audio clock, and per-device audio/video latency calibration |
| `undo` | `github.com/nsgo-dev/game/packages/game/undo` | [undo](undo.md) | Reversible moves with unlimited undo, redo and `Seek`, saved as a YAML `Log` |
| `math2d` | `github.com/nsgo-dev/game/packages/game/math2d` | [math2d](math2d.md) | Shared 2D `Vec2`, `Rect`, `Circle` and `Affine` types with lossless `core` conversions |
| `gameui` | `github.com/nsgo-dev/game/packages/game/gameui` | [gameui](gameui.md) | In-canvas box-layout menus, virtual-resolution scaling, spatial focus nav and world-anchored views |
| `lights2d` | `github.com/nsgo-dev/game/packages/game/lights2d` | [lights2d](lights2d.md) | 2D ambient darkness, point/cone lights, visibility-polygon shadows and normal-mapped sprites |
| `effects` | `github.com/nsgo-dev/game/packages/game/effects` | [effects](effects.md) | Kage shader passes chained over the game's frame in one draw, uniforms on the frame clock and CPU reference pictures |
| `worldmap` | `github.com/nsgo-dev/game/packages/game/worldmap` | [worldmap](worldmap.md) | YAML room maps, saved room discovery and markers, a minimap of the current area and a world map |
| `a11y` | `github.com/nsgo-dev/game/packages/game/a11y` | [a11y](a11y.md) | Live captions, color-blind palettes and CVD simulation, persisted assist options and accessible menus |
| `l10n` | `github.com/nsgo-dev/game/packages/game/l10n` | [l10n](l10n.md) | JSON string tables with CLDR plurals, script `FontSet`, RTL visual ordering and per-language asset catalogs |
| `perf` | `github.com/nsgo-dev/game/packages/game/perf` | [perf](perf.md) | Zero-alloc `Pool`/`Arena`/`SlicePool`, `SpatialHash`, `Quadtree` and `CullBatch` |
| `idle` | `github.com/nsgo-dev/game/packages/game/idle` | [idle](idle.md) | `Number` past float64's range, capped offline progress and short/scientific/locale formatting of large numbers |
| `net` | `github.com/nsgo-dev/game/packages/game/net` | [net](net.md) | Cross-target WebSocket/UDP/loopback transports, delta replication and lockstep/rollback netcode |
| `server` | `github.com/nsgo-dev/game/packages/game/server` | [server](server.md) | Headless fixed-step `Runner` for dedicated servers: several matches at their own rates, overrun reports and bounded signal shutdown |
| `accounts` | `github.com/nsgo-dev/game/packages/game/accounts` | [accounts](accounts.md) | Self-hosted player accounts: guests upgraded to email or a linked platform/OAuth identity, argon2id passwords, SQLite/Postgres stores and Ed25519 tokens game servers verify offline |
| `leaderboard` | `github.com/nsgo-dev/game/packages/game/leaderboard` | [leaderboard](leaderboard.md) | Boards declared once in YAML, scores submitted and native boards shown through Game Center or Play Games |
| `inventory` | `github.com/nsgo-dev/game/packages/game/inventory` | [inventory](inventory.md) | Item definitions in YAML, stacking slot containers, typed equipment slots and save snapshots |
| `modding` | `github.com/nsgo-dev/game/packages/game/modding` | [modding](modding.md) | Capability-sandboxed TSX `"game"` modding API and `mod.yaml` dependency/lifecycle manager |
| `math3d` | `github.com/nsgo-dev/game/packages/game/math3d` | [math3d](math3d.md) | 3D vectors, matrices, quaternions, transforms, bounds, rays, frustums and conventions |
| `scene3d` | `github.com/nsgo-dev/game/packages/game/scene3d` | [scene3d](scene3d.md) | 3D node tree, sockets/attachments, dynamic fat-AABB/SAH BVH, Orbit/FPS/ThirdPerson cameras and picking |
| `anim3d` | `github.com/nsgo-dev/game/packages/game/anim3d` | [anim3d](anim3d.md) | 3D pose state machines, 1D/2D blend spaces, bone-masked/additive layers, clip events, IK and root motion |
| `world3d` | `github.com/nsgo-dev/game/packages/game/world3d` | [world3d](world3d.md) | Heightmap terrain, multi-level 3D navmeshes, 3D `GridMap`, scattered foliage and floating-origin streaming |
| `render3d` | `github.com/nsgo-dev/game/packages/game/render3d` | [render3d](render3d.md) | GPU mesh buffers, CPU frustum culling, draw sorting, 0-alloc instancing, LOD hysteresis, debug draw/gizmos, billboards, 3D ribbon trails and GPU budgets |

For a single-page summary of the core APIs, see [Packages](packages.md).

## Install

The module needs Go 1.27 and NSGo `v0.3.0` or later:

```sh
go get github.com/nsgo-dev/game/packages/game
```

## How a frame runs

A game is an NSGo component. It starts a `game.Loop` on its scope, which runs on NSGo's `RequestAnimationFrame`:

1. Each display frame adds the time since the last one and runs `Update` once for each whole fixed step it holds, zero or more times.
2. `Update` moves the game's state by the fixed `dt`, advancing animations and timers.
3. `Draw` runs once, with how far the frame falls into the next step, and sets a signal the game's `Canvas` reads, so the canvas draws again without a commit of the view tree.

While the window is in the background, the loop runs no updates and the mixer plays nothing; both resume from where they were.

## Targets

macOS, iOS, Windows, Linux, Android and the web. HarmonyOS, mini programs, watchOS and tvOS are outside the first stage.

Consoles (Nintendo Switch, PlayStation and Xbox) are out of scope: their SDKs are under NDA and need C toolchains, and NSGo and this repository are Go only, with no hand-written C.

See [Building and Shipping](shipping.md) for icons, signing, notarization, store packaging and `nsgo check` across all six targets, and [Command Line](cli.md) for `go tool game`, which packs atlases, converts audio, generates asset manifests, encodes textures and opens the scene editor.

### VR and AR

The game layer does not support VR or AR for now ([#495](https://github.com/nsgo-dev/game/issues/495)). Every package draws into one flat NSGo view, a `Canvas` or the GPU view of [nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059), and reads touch, mouse, keyboard, gamepads and device motion. NSGo v0.2 has no XR runtime on any of its targets, and this repository uses only NSGo's public API and holds no platform code, so it cannot add one itself.

XR work here can start once NSGo provides, on at least one runtime (OpenXR on desktop and Android headsets, visionOS, ARKit, ARCore or WebXR):

- **An XR session**: starting it, pausing when the headset is taken off or the app loses focus, and ending it.
- **Poses**: the head's and each controller's pose every frame, predicted for when the frame is shown, and the runtime's controller and hand input.
- **Stereo rendering**: a render target per eye with its own view and projection, drawn by the GPU view at the headset's rate and handed to the runtime's compositor.
- **For AR**: the camera's passthrough behind the drawing, detected planes, hit tests against the real world and anchors that stay in place.

OpenXR is the runtime to target first, since one API covers headsets on Windows, Linux and Android, three of NSGo's targets; visionOS, ARKit, ARCore and WebXR can follow. The session, poses and stereo rendering come first and are enough for VR; the AR items come after them.

With those, this repository would add an XR camera rig to `scene3d` and XR actions to `input`, reusing `math3d`'s transforms for poses. Until then a 3D game runs on a flat screen.
