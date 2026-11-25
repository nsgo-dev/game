# Overview

game is a game layer for [NSGo](https://nsgo.dev) apps, written in Go. A game draws into one NSGo view, a `Canvas`, a pixel buffer or a shader, and its HUD and menus stay ordinary native NSGo views on top. It uses only NSGo's public API (`core`, `audio`, and later `gamepad`), so an app that does not import it links none of it.

> The project is early: the packages below work and are tested, and [API Stability](stability.md) says which of their APIs are stable and which may still change. Start with [Getting Started](getting-started.md) to build and run a game, then follow the [tutorial](tutorial.md) to build a whole one.

## Stages

1. **2D**: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound, drawn at 60 fps or the display's rate.
2. **3D**: meshes, cameras, lights and glTF models drawn with depth on a GPU view NSGo adds for it ([nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)). See [3D Models from Blender](3d-models-blender.md) for coordinate conventions, Blender 4.x export settings, FBX/OBJ conversion and sample glTF assets, and [Lantern 3D](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d) for an interactive glTF 3D scene example with orbit camera, dynamic lighting, a native HUD, and 2D ↔ 3D scene switching with shared input and sound.

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
| `project` | `github.com/nsgo-dev/game/packages/game/project` | [Project File](project.md) | `game.yaml`, the one project file: window, virtual resolution, input map, audio buses, autoloads and export presets, kept in step with `nsgo.yaml` |
| `refs` | `github.com/nsgo-dev/game/packages/game/refs` | [Stable references](project.md#stable-references) | Stable file IDs, the project index and moves that rewrite every reference to a file, behind `game mv`, `game index` and `game check` |
| `assets` | `github.com/nsgo-dev/game/packages/game/assets` | [assets](assets.md) | Async per-scene asset groups, reactive progress and `LoadingScreen` |
| `resource` | `github.com/nsgo-dev/game/packages/game/resource` | [resource](resources.md) | Shared YAML resource files (materials, curves, gradients, animation clips, UI themes, bus layouts, physics materials) and a `Library` that shares and reloads them |
| `text` | `github.com/nsgo-dev/game/packages/game/text` | [text](text.md) | AngelCode BMFont bitmap fonts, signed-distance-field fonts in 2D and 3D world space, and platform text via `DrawPlatform` |
| `placeholder` | `github.com/nsgo-dev/game/packages/game/placeholder` | [Placeholder Assets](placeholder.md) | A CC0 pack of sprite sheets, a tileset and level, a pixel font, sounds and glTF models, with their import settings sidecars, to prototype with |
| `particles` | `github.com/nsgo-dev/game/packages/game/particles` | [particles](particles.md) | Seeded deterministic 2D `Emitter`, spawn shapes, bursts and lifetime curves |
| `pixelfx` | `github.com/nsgo-dev/game/packages/game/pixelfx` | [pixelfx](pixelfx.md) | A Go port of pixelfx's pixel art effects: the painter, simulation and acting system, effects by category (`pixelfx/fire`, `pixelfx/explosions`, ...), recoloring, mirroring and sprite sheet export |
| `bullets` | `github.com/nsgo-dev/game/packages/game/bullets` | [bullets](bullets.md) | Shooter bullet patterns (rings, spirals, aimed bursts) from YAML, pooled and drawn through a sprite batch |
| `save` | `github.com/nsgo-dev/game/packages/game/save` | [save](save.md) | Versioned JSON save slots, schema migrations and atomic backup recovery |
| `achievements` | `github.com/nsgo-dev/game/packages/game/achievements` | [achievements](achievements.md) | Achievements declared in versioned YAML, monotonic progress persisted in `core.Storage`, and reporting to Game Center and Play Games |
| `datatable` | `github.com/nsgo-dev/game/packages/game/tooling/datatable` | [datatable](datatable.md) | Typed data tables in versioned YAML (and CSV/JSON) with cell-level errors, deterministic saves and hot reload |
| `debug` | `github.com/nsgo-dev/game/packages/game/debug` | [debug](debug.md) | Frame-time graph, wireframe shapes, scene inspector and `-tags release` stripping |
| `mcp` | `github.com/nsgo-dev/game/packages/game/mcp` | [mcp](mcp.md) | MCP tools registered through `nsgo/automation` to inspect running scenes, layers, entities and components |
| `flow` | `github.com/nsgo-dev/game/packages/game/flow` | [flow](flow.md) | Game-clock `Timers`, `Machine[S]`, `Seq`/`Parallel`/`Race` and typed event `Bus` |
| `stats` | `github.com/nsgo-dev/game/packages/game/stats` | [stats](stats.md) | Base stats with add/mul/override modifiers from sources, recomputed on change, timed modifiers on the fixed step and a YAML stats file |
| `tween` | `github.com/nsgo-dev/game/packages/game/tween` | [tween](tween.md) | Fixed-step `Tween`, `Timeline` and camera choreography helpers |
| `animclip` | `github.com/nsgo-dev/game/packages/game/animclip` | [animclip](animclip.md) | Animation clips that key any registered property, played on scene objects through `tween.Timeline` |
| `cards` | `github.com/nsgo-dev/game/packages/game/cards` | [cards](cards.md) | Drag and drop between board slots with snap and return, fanned hands, decks, piles and grids, and YAML board files |
| `rooms` | `github.com/nsgo-dev/game/packages/game/rooms` | [rooms](rooms.md) | Room maps with per-room camera bounds and cut, slide or fade transitions between them |
| `ai` | `github.com/nsgo-dev/game/packages/game/ai` | [ai](ai.md) | Grid `AStar`, 2D `NavMesh`, steering behaviors and behavior trees |
| `rts` | `github.com/nsgo-dev/game/packages/game/rts` | [rts](rts.md) | Flow fields, a `Crowd` of many units with separation, YAML formations, box/click selection and control groups |
| `procgen` | `github.com/nsgo-dev/game/packages/game/procgen` | [procgen](procgen.md) | Seeded BSP rooms, cellular-automata caves and wave function collapse from YAML rules, with connectivity checks and tilemap painting |
| `replay` | `github.com/nsgo-dev/game/packages/game/replay` | [replay](replay.md) | Input `Recorder`/`Player` with editable YAML recordings and deterministic test assertions |
| `statedump` | `github.com/nsgo-dev/game/packages/game/statedump` | [statedump](statedump.md) | Headless runs dumped as deterministic YAML (entities, components, scene stack, values) and a line `Diff` between runs |
| `cutscene` | `github.com/nsgo-dev/game/packages/game/cutscene` | [cutscene](cutscene.md) | Skippable video cutscenes over the game's view, in a scene or between scenes, from YAML files |
| `rhythm` | `github.com/nsgo-dev/game/packages/game/rhythm` | [rhythm](rhythm.md) | YAML beat maps with tempo and meter changes, a music clock on the audio clock, and per-device audio/video latency calibration |
| `undo` | `github.com/nsgo-dev/game/packages/game/undo` | [undo](undo.md) | Reversible moves with unlimited undo, redo and `Seek`, saved as a YAML `Log` |
| `fighting` | `github.com/nsgo-dev/game/packages/game/fighting` | [fighting](fighting.md) | Per-step input buffer, numpad motion inputs, charges, double taps, chords and combos from YAML move sets |
| `math2d` | `github.com/nsgo-dev/game/packages/game/math2d` | [math2d](math2d.md) | Shared 2D `Vec2`, `Rect`, `Circle` and `Affine` types with lossless `core` conversions |
| `gameui` | `github.com/nsgo-dev/game/packages/game/gameui` | [gameui](gameui.md) | In-canvas box-layout menus, virtual-resolution scaling, spatial focus nav and world-anchored views |
| `lights2d` | `github.com/nsgo-dev/game/packages/game/lights2d` | [lights2d](lights2d.md) | 2D ambient darkness, point/cone lights, visibility-polygon shadows and normal-mapped sprites |
| `effects` | `github.com/nsgo-dev/game/packages/game/effects` | [effects](effects.md) | Kage shader passes chained over the game's frame in one draw, uniforms on the frame clock and CPU reference pictures |
| `worldmap` | `github.com/nsgo-dev/game/packages/game/worldmap` | [worldmap](worldmap.md) | YAML room maps, saved room discovery and markers, a minimap of the current area and a world map |
| `fog` | `github.com/nsgo-dev/game/packages/game/fog` | [fog](fog.md) | Per-team fog of war: shadowcast vision sources, explored versus visible cells, map overlay and JSON saves |
| `quality` | `github.com/nsgo-dev/game/packages/game/quality` | [quality](quality.md) | Low, medium and high presets picked from the device class and measured frame times, overridable in settings |
| `a11y` | `github.com/nsgo-dev/game/packages/game/a11y` | [a11y](a11y.md) | Live captions, color-blind palettes and CVD simulation, persisted assist options and accessible menus |
| `l10n` | `github.com/nsgo-dev/game/packages/game/l10n` | [l10n](l10n.md) | JSON string tables with CLDR plurals, script `FontSet`, RTL visual ordering and per-language asset catalogs |
| `perf` | `github.com/nsgo-dev/game/packages/game/perf` | [perf](perf.md) | Zero-alloc `Pool`/`Arena`/`SlicePool`, `SpatialHash`, `Quadtree` and `CullBatch` |
| `idle` | `github.com/nsgo-dev/game/packages/game/idle` | [idle](idle.md) | `Number` past float64's range, capped offline progress and short/scientific/locale formatting of large numbers |
| `net` | `github.com/nsgo-dev/game/packages/game/net` | [net](net.md) | Cross-target WebSocket/UDP/loopback transports, delta replication and lockstep/rollback netcode |
| `server` | `github.com/nsgo-dev/game/packages/game/server` | [server](server.md) | Headless fixed-step `Runner` for dedicated servers: several matches at their own rates, overrun reports and bounded signal shutdown |
| `accounts` | `github.com/nsgo-dev/game/packages/game/accounts` | [accounts](accounts.md) | Self-hosted player accounts: guests upgraded to email or a linked platform/OAuth identity, argon2id passwords, SQLite/Postgres stores and Ed25519 tokens game servers verify offline |
| `cloudsave` | `github.com/nsgo-dev/game/packages/game/cloudsave` | [cloudsave](cloudsave.md) | Self-hosted cloud saves behind `liveops.CloudSync`: per-account slots over HTTPS, conditional writes that turn concurrent saves into conflicts, size limits, kept versions with restore, SQLite/Postgres stores |
| `leaderboard` | `github.com/nsgo-dev/game/packages/game/leaderboard` | [leaderboard](leaderboard.md) | Boards declared once in YAML, scores submitted and native boards shown through Game Center or Play Games |
| `leaderboard/selfhost` | `github.com/nsgo-dev/game/packages/game/leaderboard/selfhost` | [leaderboard/selfhost](leaderboard-selfhost.md) | Self-hosted cross-platform leaderboards: all-time, daily and weekly boards with kept past periods, scores authenticated by account tokens and optionally signed by the game server, top-N, around-me and friends queries |
| `presence` | `github.com/nsgo-dev/game/packages/game/presence` | [presence](presence.md) | Rich presence on Steam (`ISteamFriends` via purego) and Discord (pure Go local IPC v1) behind a provider-neutral `Activity` and `Manager` |
| `inventory` | `github.com/nsgo-dev/game/packages/game/inventory` | [inventory](inventory.md) | Item definitions in YAML, stacking slot containers, typed equipment slots and save snapshots |
| `ability` | `github.com/nsgo-dev/game/packages/game/ability` | [ability](ability.md) | YAML ability sets and a per-step `Caster`: costs, cooldowns, charges, cast and channel times and conditions driven by input actions |
| `quest` | `github.com/nsgo-dev/game/packages/game/quest` | [quest](quest.md) | YAML quest books with collect/reach/defeat/talk objectives, prerequisites and rewards, advanced by bus events and saved as snapshots |
| `loot` | `github.com/nsgo-dev/game/packages/game/loot` | [loot](loot.md) | Nested weighted drop tables in YAML with guaranteed drops and hard/soft pity counters on seeded streams |
| `vn` | `github.com/nsgo-dev/game/packages/game/vn` | [vn](vn.md) | Visual novel presenter: YAML cast of portraits and expressions, name plates, typewriter text box, choices, backlog, auto/skip modes and voice lines |
| `modding` | `github.com/nsgo-dev/game/packages/game/modding` | [modding](modding.md) | Capability-sandboxed TSX `"game"` modding API and `mod.yaml` dependency/lifecycle manager |
| `math3d` | `github.com/nsgo-dev/game/packages/game/math3d` | [math3d](math3d.md) | 3D vectors, matrices, quaternions, transforms, bounds, rays, frustums and conventions |
| `scene3d` | `github.com/nsgo-dev/game/packages/game/scene3d` | [scene3d](scene3d.md) | 3D node tree, sockets/attachments, dynamic fat-AABB/SAH BVH, Orbit/FPS/ThirdPerson cameras and picking |
| `anim3d` | `github.com/nsgo-dev/game/packages/game/anim3d` | [anim3d](anim3d.md) | 3D pose state machines, 1D/2D blend spaces, bone-masked/additive layers, clip events, IK and root motion |
| `world3d` | `github.com/nsgo-dev/game/packages/game/world3d` | [world3d](world3d.md) | Heightmap terrain, multi-level 3D navmeshes, 3D `GridMap`, scattered foliage and floating-origin streaming |
| `voxel` | `github.com/nsgo-dev/game/packages/game/voxel` | [voxel](voxel.md) | Chunked 3D voxel worlds with greedy meshing, cross-chunk culling and AO, dirty-chunk editing, DDA raycasting and YAML saves |
| `render3d` | `github.com/nsgo-dev/game/packages/game/render3d` | [render3d](render3d.md) | GPU mesh buffers, textures and samplers, camera uniforms, CPU frustum culling, draw sorting, 0-alloc instancing, LOD hysteresis, debug draw/gizmos, billboards, 3D ribbon trails and GPU budgets |
| `tooling/jsonschema` | `github.com/nsgo-dev/game/packages/game/tooling/jsonschema` | [Schemas for YAML files](schemas.md) | JSON Schemas built from Go types for editors and AI tools, the published scene schema and validation with JSON Pointer paths |
| `tooling/library` | `github.com/nsgo-dev/game/packages/game/tooling/library` | [Asset and Plugin Library](library.md) | A YAML index of shared asset, editor plugin and template packs, checked by digest and installed into a project with their files and `game.yaml` references recorded |
| `framedata` | `github.com/nsgo-dev/game/packages/game/framedata` | [framedata](framedata.md) | Fighting game hitboxes, hurtboxes, hitstun and cancel windows per frame in YAML, resolved deterministically each step |
| `vehicle` | `github.com/nsgo-dev/game/packages/game/vehicle` | [vehicle](vehicle.md) | Raycast cars on `physics3d`: wheel suspension, tire friction and a car controller, race checkpoints and laps, and YAML car files and lap ghosts |

For a single-page summary of the core APIs, see [Packages](packages.md). Every loader reports a file it cannot use with the file, line, field and a fix; see [Errors](errors.md). [File Formats](formats.md) lists every file a game reads or writes, with its version and an example.

## Coming from another engine

Each guide maps that engine's scenes, nodes, resources, signals, input maps, physics and export to the packages above: [Godot](from-godot.md), [Ebitengine](from-ebitengine.md) and [Unity](from-unity.md).

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

See [Building and Shipping](shipping.md) for icons, signing, notarization, store packaging and `nsgo check` across all six targets, and [Command Line](cli.md) for `go tool game`, which packs atlases, converts audio, generates asset manifests, encodes textures, opens the scene editor, and builds all of a game's assets from `game.yaml`, rebuilding only what changed.

### VR and AR

The game layer does not support VR or AR for now ([#495](https://github.com/nsgo-dev/game/issues/495)). Every package draws into one flat NSGo view, a `Canvas` or the GPU view of [nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059), and reads touch, mouse, keyboard, gamepads and device motion. NSGo v0.2 has no XR runtime on any of its targets, and this repository uses only NSGo's public API and holds no platform code, so it cannot add one itself.

XR work here can start once NSGo provides, on at least one runtime (OpenXR on desktop and Android headsets, visionOS, ARKit, ARCore or WebXR):

- **An XR session**: starting it, pausing when the headset is taken off or the app loses focus, and ending it.
- **Poses**: the head's and each controller's pose every frame, predicted for when the frame is shown, and the runtime's controller and hand input.
- **Stereo rendering**: a render target per eye with its own view and projection, drawn by the GPU view at the headset's rate and handed to the runtime's compositor.
- **For AR**: the camera's passthrough behind the drawing, detected planes, hit tests against the real world and anchors that stay in place.

OpenXR is the runtime to target first, since one API covers headsets on Windows, Linux and Android, three of NSGo's targets; visionOS, ARKit, ARCore and WebXR can follow. The session, poses and stereo rendering come first and are enough for VR; the AR items come after them.

With those, this repository would add an XR camera rig to `scene3d` and XR actions to `input`, reusing `math3d`'s transforms for poses. Until then a 3D game runs on a flat screen.

## For language models

The site publishes the English docs for language models and coding agents, as [nsgo.dev](https://nsgo.dev/llms.txt) does: [llms.txt](https://game.nsgo.dev/llms.txt) lists every page with a line about it, and [llms-full.txt](https://game.nsgo.dev/llms-full.txt) holds every page's Markdown in one file. Both are built from `docs/en` with the site, so they always match these pages. For contributors, the [Agent Evaluation Suite](agent-eval.md) grades fixed game tasks with YAML, build and `game/mcp` checks, and can run a coding agent on them.

For coding agents that support Agent Skills, install the game-layer skill with `bunx skills add nsgo-dev/game`; see [Agent Skill](agent-skill.md).
