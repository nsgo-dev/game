# Overview

game is a pure-Go 2D and 3D game layer for [NSGo](https://nsgo.dev) apps. A game draws into one NSGo view—a `Canvas`, a pixel buffer, a shader or a GPU view—while its HUD and menus stay ordinary native NSGo views on top. It uses only NSGo's public API (`core`, `audio`, `gamepad`, and GPU views), so an app that does not import it links none of it.

> NSGo Game `v0.1.0` requires Go 1.27+ and NSGo `v0.3.4+`. [API Stability](stability.md) says which packages are stable and which are experimental. Start with [Getting Started](getting-started.md) to build and run a game, pick one of the [starter templates](templates.md), or follow the [tutorial](tutorial.md) to build a complete game.

## 2D and 3D

1. **2D**: a fixed-step loop, input actions, sprites, tilemaps, scenes, physics, lighting, particles, UI and sound, drawn at 60 fps or the display's rate.
2. **3D**: meshes, cameras, PBR lighting, skeletal animation, 3D physics, terrain, voxels and glTF models drawn with depth on NSGo's GPU view. See [3D Models from Blender](3d-models-blender.md) for coordinate conventions, Blender 4.x export settings, FBX/OBJ conversion and sample glTF assets, and [Lantern 3D](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d) for an interactive glTF 3D scene example with orbit camera, dynamic lighting, a native HUD, and 2D ↔ 3D scene switching with shared input and sound.

The loop, input actions, sound, assets and scene switching are shared by both 2D and 3D, so you can combine 2D and 3D scenes in one game without rewriting its structure. Every author-facing file—scenes, prefabs, materials, animations, tables and project settings—is plain-text versioned YAML ([File Formats](formats.md)), editable by hand, with coding agents, or in the visual [Scene Editor](editor.md).

## Packages

| Package | Import path | Guide | What it holds |
| --- | --- | --- | --- |
| `game` | `github.com/nsgo-dev/game` | [Loop](loop.md) | `Loop`, the fixed-step loop every package shares |
| `sprite` | `github.com/nsgo-dev/game/pkg/sprite` | [sprite](sprite.md) | Atlases, frames, animations, sprites and batches drawn into a `Canvas` |
| `sound` | `github.com/nsgo-dev/game/pkg/sound` | [sound](sound.md) | The `Mixer`, its effects and music |
| `input` | `github.com/nsgo-dev/game/pkg/input` | [input](input.md) | Named actions and axes from touch regions, mouse buttons, keyboard keys, pointer-lock mouselook and game controllers, persisted in `core.Storage` |
| `tilemap` | `github.com/nsgo-dev/game/pkg/tilemap` | [tilemap](tilemap.md) | 2D `Camera`, culled tilemaps, Tiled `.tmj` loader, animated tiles and raycasts |
| `scene` | `github.com/nsgo-dev/game/pkg/scene` | [scene](scene.md) | World/screen layers, hit testing, scene stacks and animated transitions |
| `sceneformat` | `github.com/nsgo-dev/game/pkg/sceneformat` | [Scene format](formats.md#scene) | Versioned YAML scene documents: strict parsing and validation, deterministic encoding and building into sprite batches ready to draw |
| `editor` | `github.com/nsgo-dev/game/pkg/editor` | [Scene Editor](editor.md) | The NSGo Scene Editor over `sceneformat`: undoable scene, tilemap, layer and property editing, play-in-editor, asset browser, plugins and project panels |
| `editor/theme` | `github.com/nsgo-dev/game/pkg/editor/theme` | [Design tokens](editor.md#design-tokens-1002) | The editor's design tokens: the embedded YAML theme, the dark and light palettes, type, spacing, shape and motion constants, and the WCAG 2.2 contrast table every palette meets |
| `editor/keychain` | `github.com/nsgo-dev/game/pkg/editor/keychain` | [Build and release screen](editor.md#build-and-release-screen-1048-1050) | The editor's OS secret storage: macOS Keychain, Linux Secret Service and Windows Credential Manager behind one `Store`, with an in-memory `Memory` for tests |
| `physics` | `github.com/nsgo-dev/game/pkg/physics` | [physics](physics.md) | `Box`/`Circle` overlaps, `MoveAndSlide` tile resolution and `World` queries |
| `physics3d` | `github.com/nsgo-dev/game/pkg/physics3d` | [physics3d](physics3d.md) | Deterministic 3D collision detection, raycasts, rigid-body dynamics and a kinematic character controller |
| `platformer` | `github.com/nsgo-dev/game/pkg/platformer` | [platformer](platformer.md) | Platformer `Controller` on `CharacterBody`: coyote time, jump buffer, variable jump height, wall slide and wall jump from a YAML config |
| `screen` | `github.com/nsgo-dev/game/pkg/screen` | [screen](screen.md) | Virtual resolution `Viewport` scaling, pixel snapping, safe-area rectangles and video settings |
| `project` | `github.com/nsgo-dev/game/pkg/project` | [Project File](project.md) | `game.yaml`, the one project file: window, virtual resolution, input map, audio buses, autoloads and export presets, kept in step with `nsgo.yaml` |
| `tooling/refs` | `github.com/nsgo-dev/game/pkg/tooling/refs` | [Stable references](project.md#stable-references) | Stable file IDs, the project index and moves that rewrite every reference to a file, behind `game mv`, `game index` and `game check` |
| `assets` | `github.com/nsgo-dev/game/pkg/assets` | [assets](assets.md) | Async per-scene asset groups, reactive progress and `LoadingScreen` |
| `resource` | `github.com/nsgo-dev/game/pkg/resource` | [resource](resources.md) | Shared YAML resource files (materials, curves, gradients, animation clips, UI themes, bus layouts, physics materials) and a `Library` that shares and reloads them |
| `importsettings` | `github.com/nsgo-dev/game/pkg/importsettings` | [Import settings sidecar](formats.md#import-settings-sidecar) | `.import.yaml` sidecars beside an image, sound, font or model that say how `game assets` imports it, overriding the target's settings |
| `ktx2` | `github.com/nsgo-dev/game/pkg/ktx2` | [tex](cli.md#tex) | KTX2 texture container encoding and decoding, mipmap chains, Basis Universal ETC1S and UASTC codecs and transcoding to BC, ETC2 and ASTC blocks |
| `loaderr` | `github.com/nsgo-dev/game/pkg/loaderr` | [Errors](errors.md) | The error every loader returns for a file it cannot use: file, line, column, field, what was expected and how to fix it |
| `text` | `github.com/nsgo-dev/game/pkg/text` | [text](text.md) | AngelCode BMFont bitmap fonts, signed-distance-field fonts in 2D and 3D world space, and platform text via `DrawPlatform` |
| `placeholder` | `github.com/nsgo-dev/game/pkg/placeholder` | [Placeholder Assets](placeholder.md) | A CC0 pack of sprite sheets, a tileset and level, a pixel font, sounds and glTF models, with their import settings sidecars, to prototype with |
| `particles` | `github.com/nsgo-dev/game/pkg/particles` | [particles](particles.md) | Seeded deterministic 2D `Emitter`, spawn shapes, bursts and lifetime curves |
| `pixelfx` | `github.com/nsgo-dev/game/pkg/pixelfx` | [pixelfx](pixelfx.md) | A Go port of pixelfx's pixel art effects: the painter, simulation and acting system, effects by category (`pixelfx/fire`, `pixelfx/explosions`, ...), recoloring, mirroring and sprite sheet export |
| `bullets` | `github.com/nsgo-dev/game/pkg/bullets` | [bullets](bullets.md) | Shooter bullet patterns (rings, spirals, aimed bursts) from YAML, pooled and drawn through a sprite batch |
| `save` | `github.com/nsgo-dev/game/pkg/save` | [save](save.md) | Versioned JSON save slots, schema migrations and atomic backup recovery |
| `achievements` | `github.com/nsgo-dev/game/pkg/online/achievements` | [achievements](achievements.md) | Achievements declared in versioned YAML, monotonic progress persisted in `core.Storage`, and reporting to Game Center and Play Games |
| `datatable` | `github.com/nsgo-dev/game/pkg/datatable` | [datatable](datatable.md) | Typed data tables in versioned YAML (and CSV/JSON) with cell-level errors, deterministic saves and hot reload |
| `tooling/gamedebug` | `github.com/nsgo-dev/game/pkg/tooling/gamedebug` | [gamedebug](gamedebug.md) | Frame-time graph, wireframe shapes, scene inspector and `-tags release` stripping |
| `mcp` | `github.com/nsgo-dev/game/pkg/mcp` | [mcp](mcp.md) | MCP tools registered through `nsgo/automation` to inspect running scenes, layers, entities and components |
| `flow` | `github.com/nsgo-dev/game/pkg/flow` | [flow](flow.md) | Game-clock `Timers`, `Machine[S]`, `Seq`/`Parallel`/`Race` and typed event `Bus` |
| `dialogue` | `github.com/nsgo-dev/game/pkg/dialogue` | [dialogue](dialogue.md) | Yarn Spinner scripts run line by line and choice by choice, with variables, conditions, game commands and saved `State` |
| `entities` | `github.com/nsgo-dev/game/pkg/entities` | [entities](entities.md) | Zero-reflection ECS: generational entities, typed sparse-set components, allocation-free queries, ordered systems, prefabs and JSON snapshots |
| `stats` | `github.com/nsgo-dev/game/pkg/stats` | [stats](stats.md) | Base stats with add/mul/override modifiers from sources, recomputed on change, timed modifiers on the fixed step and a YAML stats file |
| `tween` | `github.com/nsgo-dev/game/pkg/tween` | [tween](tween.md) | Fixed-step `Tween`, `Timeline` and camera choreography helpers |
| `animclip` | `github.com/nsgo-dev/game/pkg/animclip` | [animclip](animclip.md) | Animation clips that key any registered property, played on scene objects through `tween.Timeline` |
| `skeletal` | `github.com/nsgo-dev/game/pkg/skeletal` | [skeletal](skeletal.md) | 2D skeletal rigs in versioned YAML, loaded, saved, animated and drawn through `sprite.Batch` |
| `cards` | `github.com/nsgo-dev/game/pkg/cards` | [cards](cards.md) | Drag and drop between board slots with snap and return, fanned hands, decks, piles and grids, and YAML board files |
| `rooms` | `github.com/nsgo-dev/game/pkg/rooms` | [rooms](rooms.md) | Room maps with per-room camera bounds and cut, slide or fade transitions between them |
| `ai` | `github.com/nsgo-dev/game/pkg/ai` | [ai](ai.md) | Steering behaviors, ORCA avoidance, behavior trees, utility AI, GOAP, tactical queries and perception |
| `ai/perception3d` | `github.com/nsgo-dev/game/pkg/ai/perception3d` | [ai](ai.md#3d) | `ai` perception for 3D agents on `math3d.Vec3`, with `physics3d` line of sight |
| `ai/cover3d` | `github.com/nsgo-dev/game/pkg/ai/cover3d` | [ai](ai.md#cover-points-and-generators) | `ai` cover points generated from the colliders of a `physics3d` world on the XZ plane |
| `pathfind` | `github.com/nsgo-dev/game/pkg/pathfind` | [pathfind](pathfind.md) | Grid `AStar`, HPA*, JPS, graph and waypoint search, 2D `NavMesh` with `NavAgent`, tiled meshes and budgeted path queues |
| `pathfind/navbake` | `github.com/nsgo-dev/game/pkg/pathfind/navbake` | [navbake](navbake.md) | Bakes `pathfind.NavMesh` polygons from concave obstacles or a tilemap's solid cells for each agent radius, saved as versioned YAML, with a debug overlay |
| `rts` | `github.com/nsgo-dev/game/pkg/rts` | [rts](rts.md) | Flow fields, a `Crowd` of many units with separation, YAML formations, box/click selection and control groups |
| `procgen` | `github.com/nsgo-dev/game/pkg/procgen` | [procgen](procgen.md) | Seeded BSP rooms, cellular-automata caves and wave function collapse from YAML rules, with connectivity checks and tilemap painting |
| `replay` | `github.com/nsgo-dev/game/pkg/replay` | [replay](replay.md) | Input `Recorder`/`Player` with editable YAML recordings and deterministic test assertions |
| `statedump` | `github.com/nsgo-dev/game/pkg/statedump` | [statedump](statedump.md) | Headless runs dumped as deterministic YAML (entities, components, scene stack, values) and a line `Diff` between runs |
| `gametest` | `github.com/nsgo-dev/game/pkg/gametest` | [gametest](gametest.md) | Deterministic software rasterizer for `core.DrawOp`, golden PNG assertions with persistent failure diffs, and `ImageDraws` structural checks |
| `tooling/playtest` | `github.com/nsgo-dev/game/pkg/tooling/playtest` | [playtest](playtest.md) | `playtest.yaml` bots, seeded headless runs with replay recordings, metrics against goals and deterministic YAML reports |
| `cutscene` | `github.com/nsgo-dev/game/pkg/cutscene` | [cutscene](cutscene.md) | Skippable video cutscenes over the game's view, in a scene or between scenes, from YAML files |
| `rhythm` | `github.com/nsgo-dev/game/pkg/rhythm` | [rhythm](rhythm.md) | YAML beat maps with tempo and meter changes, a music clock on the audio clock, and per-device audio/video latency calibration |
| `undo` | `github.com/nsgo-dev/game/pkg/undo` | [undo](undo.md) | Reversible moves with unlimited undo, redo and `Seek`, saved as a YAML `Log` |
| `fighting` | `github.com/nsgo-dev/game/pkg/fighting` | [fighting](fighting.md) | Per-step input buffer, numpad motion inputs, charges, double taps, chords and combos from YAML move sets |
| `math2d` | `github.com/nsgo-dev/game/pkg/math2d` | [math2d](math2d.md) | Shared 2D `Vec2`, `Rect`, `Circle` and `Affine` types with lossless `core` conversions |
| `random` | `github.com/nsgo-dev/game/pkg/random` | [Determinism](determinism.md#2-use-seeded-randomstream-per-purpose) | Seeded cross-platform `Stream` (Xoshiro256**), procedural noise and helpers that verify a simulation is deterministic |
| `gameui` | `github.com/nsgo-dev/game/pkg/gameui` | [gameui](gameui.md) | In-canvas box-layout menus, virtual-resolution scaling, spatial focus nav and world-anchored views |
| `lights2d` | `github.com/nsgo-dev/game/pkg/lights2d` | [lights2d](lights2d.md) | 2D ambient darkness, point/cone lights, visibility-polygon shadows and normal-mapped sprites |
| `tilemap/tileshape` | `github.com/nsgo-dev/game/pkg/tilemap/tileshape` | [tileshape](tileshape.md) | Tilemap collision and lighting geometry: a physics `TileGrid` view, slopes from per-tile shapes, merged outlines and `lights2d` occluders |
| `postfx` | `github.com/nsgo-dev/game/pkg/postfx` | [postfx](postfx.md) | Kage shader passes chained over the game's frame in one draw, uniforms on the frame clock and CPU reference pictures |
| `worldmap` | `github.com/nsgo-dev/game/pkg/worldmap` | [worldmap](worldmap.md) | YAML room maps, saved room discovery and markers, a minimap of the current area and a world map |
| `fog` | `github.com/nsgo-dev/game/pkg/fog` | [fog](fog.md) | Per-team fog of war: shadowcast vision sources, explored versus visible cells, map overlay and JSON saves |
| `quality` | `github.com/nsgo-dev/game/pkg/quality` | [quality](quality.md) | Low, medium and high presets picked from the device class and measured frame times, overridable in settings |
| `a11y` | `github.com/nsgo-dev/game/pkg/a11y` | [a11y](a11y.md) | Live captions, color-blind palettes and CVD simulation, persisted assist options and accessible menus |
| `l10n` | `github.com/nsgo-dev/game/pkg/l10n` | [l10n](l10n.md) | JSON string tables with CLDR plurals, script `FontSet`, RTL visual ordering and per-language asset catalogs |
| `mempool` | `github.com/nsgo-dev/game/pkg/mempool` | [mempool](mempool.md) | Zero-alloc object pools: `Pool`, `Arena` with generational `Handle`s and `SlicePool` |
| `spatial` | `github.com/nsgo-dev/game/pkg/spatial` | [spatial](spatial.md) | 2D broad phase with `Hash` and `Quadtree`, and viewport culling with `Cull` and `CullBatch` |
| `idle` | `github.com/nsgo-dev/game/pkg/idle` | [idle](idle.md) | `Number` past float64's range, capped offline progress and short/scientific/locale formatting of large numbers |
| `netplay` | `github.com/nsgo-dev/game/pkg/online/netplay` | [netplay](netplay.md) | Cross-target WebSocket/UDP/loopback transports, delta replication and lockstep/rollback netcode |
| `wsclient` | `github.com/nsgo-dev/game/pkg/online/wsclient` | [wsclient](wsclient.md) | Plain WebSocket client for third-party servers, with the same API on native and web targets |
| `gameserver` | `github.com/nsgo-dev/game/pkg/online/gameserver` | [gameserver](gameserver.md) | Headless fixed-step `Runner` for dedicated servers: several matches at their own rates, overrun reports and bounded signal shutdown |
| `audit` | `github.com/nsgo-dev/game/pkg/online/audit` | [gameserver](gameserver.md) | Append-only JSON Lines audit log of a server's administrative events: one file per UTC day, a non-blocking queue with a drop counter, retention |
| `accounts` | `github.com/nsgo-dev/game/pkg/online/accounts` | [accounts](accounts.md) | Self-hosted player accounts: guests upgraded to email or a linked platform/OAuth identity, argon2id passwords, SQLite/Postgres stores and Ed25519 tokens game servers verify offline |
| `cloudsave` | `github.com/nsgo-dev/game/pkg/online/cloudsave` | [cloudsave](cloudsave.md) | Self-hosted cloud saves behind `liveops.CloudSync`: per-account slots over HTTPS, conditional writes that turn concurrent saves into conflicts, size limits, kept versions with restore, SQLite (`cloudsave/sqlitestore`) and Postgres (`cloudsave/pgstore`) stores |
| `leaderboard` | `github.com/nsgo-dev/game/pkg/online/leaderboard` | [leaderboard](leaderboard.md) | Boards declared once in YAML, scores submitted and native boards shown through Game Center or Play Games |
| `leaderboard/selfhost` | `github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost` | [leaderboard/selfhost](leaderboard-selfhost.md) | Self-hosted cross-platform leaderboards: all-time, daily and weekly boards with kept past periods, scores authenticated by account tokens and optionally signed by the game server, top-N, around-me and friends queries |
| `eos` | `github.com/nsgo-dev/game/pkg/online/eos` | [eos](epic.md) | Epic Online Services boundary: exchange-code login, achievements, leaderboards and rich presence over an injected asynchronous driver, results on the engine goroutine |
| `presence` | `github.com/nsgo-dev/game/pkg/online/presence` | [presence](presence.md) | Rich presence on Steam (`ISteamFriends` via purego) and Discord (pure Go local IPC v1) behind a provider-neutral `Activity` and `Manager` |
| `steam` | `github.com/nsgo-dev/game/pkg/online/steam` | [steam](steamworks.md) | Injectable Steamworks boundary for achievements, stats, leaderboard scores and rich presence; games supply the native `Driver` |
| `liveops` | `github.com/nsgo-dev/game/pkg/online/liveops` | [liveops](liveops.md) | Provider-agnostic crash reports, consent-gated analytics, the `CloudSync` interface and in-app purchases credited into the save |
| `inventory` | `github.com/nsgo-dev/game/pkg/inventory` | [inventory](inventory.md) | Item definitions in YAML, stacking slot containers, typed equipment slots and save snapshots |
| `ability` | `github.com/nsgo-dev/game/pkg/ability` | [ability](ability.md) | YAML ability sets and a per-step `Caster`: costs, cooldowns, charges, cast and channel times and conditions driven by input actions |
| `quest` | `github.com/nsgo-dev/game/pkg/quest` | [quest](quest.md) | YAML quest books with collect/reach/defeat/talk objectives, prerequisites and rewards, advanced by bus events and saved as snapshots |
| `loot` | `github.com/nsgo-dev/game/pkg/loot` | [loot](loot.md) | Nested weighted drop tables in YAML with guaranteed drops and hard/soft pity counters on seeded streams |
| `rpgdata` | `github.com/nsgo-dev/game/pkg/rpgdata` | [rpgdata](rpgdata.md) | RPG database in YAML: classes with level curves and learned skills, actors, skills, enemies and troops, with references checked on load and a linear experience progression |
| `battle` | `github.com/nsgo-dev/game/pkg/battle` | [battle](battle.md) | Deterministic turn-based battles between a party and a troop: agility order, scoped actions with MP costs, hit and critical rolls on seeded streams, presentation-independent events and snapshots |
| `vn` | `github.com/nsgo-dev/game/pkg/vn` | [vn](vn.md) | Visual novel presenter: YAML cast of portraits and expressions, name plates, typewriter text box, choices, backlog, auto/skip modes and voice lines |
| `modding` | `github.com/nsgo-dev/game/pkg/modding` | [modding](modding.md) | Capability-sandboxed TSX `"game"` modding API and `mod.yaml` dependency/lifecycle manager |
| `gameplugin` | `github.com/nsgo-dev/game/pkg/gameplugin` | [Plugins](plugins.md) | Runtime plugin contract and deterministic installer: API version check, conflict detection, and ordered registration of components, systems, effects, data tables and console commands |
| `gameplugin/manifest` | `github.com/nsgo-dev/game/pkg/gameplugin/manifest` | [Plugins](plugins.md) | Strict parser, validator and deterministic YAML encoder for Go plugin `plugin.yaml` manifests |
| `math3d` | `github.com/nsgo-dev/game/pkg/math3d` | [math3d](math3d.md) | 3D vectors, matrices, quaternions, transforms, bounds, rays, frustums and conventions |
| `scene3d` | `github.com/nsgo-dev/game/pkg/scene3d` | [scene3d](scene3d.md) | 3D node tree, sockets/attachments, dynamic fat-AABB/SAH BVH, Orbit/FPS/ThirdPerson cameras and picking |
| `anim3d` | `github.com/nsgo-dev/game/pkg/anim3d` | [anim3d](anim3d.md) | 3D pose state machines, 1D/2D blend spaces, bone-masked/additive layers, clip events, IK and root motion |
| `world3d` | `github.com/nsgo-dev/game/pkg/world3d` | [world3d](world3d.md) | Heightmap terrain, 3D `GridMap`, scattered foliage and floating-origin chunk streaming |
| `pathfind3d` | `github.com/nsgo-dev/game/pkg/pathfind3d` | [pathfind3d](pathfind3d.md) | Multi-level 3D `NavMesh` with areas, off-mesh links and reusable queries, `NavAgent` groups, and tiled meshes with dynamic obstacles that stream with `world3d` chunks |
| `pathfind3d/navbake3d` | `github.com/nsgo-dev/game/pkg/pathfind3d/navbake3d` | [navbake3d](navbake3d.md) | Navigation meshes baked from glTF, terrain, `GridMap` and physics level geometry per agent type, saved as YAML a scene names, with debug drawing and `game navmesh` |
| `voxel` | `github.com/nsgo-dev/game/pkg/voxel` | [voxel](voxel.md) | Chunked 3D voxel worlds with greedy meshing, cross-chunk culling and AO, dirty-chunk editing, DDA raycasting and YAML saves |
| `render3d` | `github.com/nsgo-dev/game/pkg/render3d` | [render3d](render3d.md) | GPU mesh buffers, textures and samplers, camera uniforms, CPU frustum culling, draw sorting, 0-alloc instancing, LOD hysteresis, debug draw/gizmos, billboards, 3D ribbon trails and GPU budgets |
| `scenemodel` | `github.com/nsgo-dev/game/pkg/scenemodel` | [scenemodel](scenemodel.md) | Scene file objects drawn in 3D: built-in primitives and glTF models with material overrides, built into a `scene3d` tree with bounds and drawn by a lit, textured renderer |
| `gltf` | `github.com/nsgo-dev/game/pkg/gltf` | [3D Models from Blender](3d-models-blender.md) | Pure-Go glTF 2.0 and GLB loader: typed accessors, node hierarchy, PBR materials, skins, morph targets, animations and Draco/meshopt compression |
| `jsonschema` | `github.com/nsgo-dev/game/pkg/jsonschema` | [Schemas for YAML files](schemas.md) | JSON Schemas built from Go types for editors and AI tools, the published scene schema and validation with JSON Pointer paths |
| `tooling/library` | `github.com/nsgo-dev/game/pkg/tooling/library` | [Asset and Plugin Library](library.md) | A YAML index of shared asset, editor plugin and template packs, checked by digest and installed into a project with their files and `game.yaml` references recorded |
| `framedata` | `github.com/nsgo-dev/game/pkg/framedata` | [framedata](framedata.md) | Fighting game hitboxes, hurtboxes, hitstun and cancel windows per frame in YAML, resolved deterministically each step |
| `vehicle3d` | `github.com/nsgo-dev/game/pkg/vehicle3d` | [vehicle3d](vehicle3d.md) | Raycast cars on `physics3d`: wheel suspension, tire friction and a car controller, race checkpoints and laps, and YAML car files and lap ghosts |

For a single-page summary of the core APIs, see [Packages](packages.md). Every loader reports a file it cannot use with the file, line, field and a fix; see [Errors](errors.md). [File Formats](formats.md) lists every file a game reads or writes, with its version and an example.

## Coming from another engine

Each guide maps that engine's scenes, nodes, resources, signals, input maps, physics and export to the packages above: [Godot](from-godot.md), [Ebitengine](from-ebitengine.md), [Unity](from-unity.md) and [Unreal Engine](from-unreal.md). For 2D tools, [GameMaker](from-gamemaker.md), [Construct](from-construct.md) and [RPG Maker](from-rpgmaker.md) map rooms, layouts, event sheets, behaviors and the RPG Maker database the same way.

## Install

The module needs Go 1.27 and NSGo `v0.3.4` or later:

```sh
go get github.com/nsgo-dev/game
```

## How a frame runs

A game is an NSGo component. It starts a `game.Loop` on its scope, which runs on NSGo's `RequestAnimationFrame`:

1. Each display frame adds the time since the last one and runs `Update` once for each whole fixed step it holds, zero or more times.
2. `Update` moves the game's state by the fixed `dt`, advancing animations and timers.
3. `Draw` runs once, with how far the frame falls into the next step, and sets a signal the game's `Canvas` reads, so the canvas draws again without a commit of the view tree.

While the window is in the background, the loop runs no updates and the mixer plays nothing; both resume from where they were.

## Targets

NSGo Game has six primary build targets—macOS, iOS, Windows, Linux, Android and the web—while HarmonyOS, mini programs, watchOS and tvOS remain experimental targets outside the first stage. Feature availability and CI test coverage differ across targets; see [Platform support](platforms.md) for the per-feature matrix and the upstream issues tracking each gap.

Consoles (Nintendo Switch, PlayStation and Xbox) are out of scope: their SDKs are under NDA and need C toolchains, and NSGo and this repository are Go only, with no hand-written C.

See [Building and Shipping](shipping.md) for icons, signing, notarization, store packaging and `nsgo check` across all six build targets, [Accessibility Guide](accessibility-guide.md) for wiring player accessibility settings, GAG/XAG guidelines, pre-release checks and App Store accessibility labels, and [Command Line](cli.md) for `go tool game`, which packs atlases, converts audio, generates asset manifests, encodes textures, opens the scene editor, and builds all of a game's assets from `game.yaml`, rebuilding only what changed.

### VR and AR

The game layer does not support VR or AR for now ([#495](https://github.com/nsgo-dev/game/issues/495)). Every package draws into one flat NSGo view, a `Canvas` or a GPU view, and reads touch, mouse, keyboard, gamepads and device motion. NSGo currently has no XR runtime on any of its targets, and this repository uses only NSGo's public API and holds no platform code, so it cannot add one itself.

XR work here can start once NSGo provides, on at least one runtime (OpenXR on desktop and Android headsets, visionOS, ARKit, ARCore or WebXR):

- **An XR session**: starting it, pausing when the headset is taken off or the app loses focus, and ending it.
- **Poses**: the head's and each controller's pose every frame, predicted for when the frame is shown, and the runtime's controller and hand input.
- **Stereo rendering**: a render target per eye with its own view and projection, drawn by the GPU view at the headset's rate and handed to the runtime's compositor.
- **For AR**: the camera's passthrough behind the drawing, detected planes, hit tests against the real world and anchors that stay in place.

OpenXR is the runtime to target first, since one API covers headsets on Windows, Linux and Android, three of NSGo's targets; visionOS, ARKit, ARCore and WebXR can follow. The session, poses and stereo rendering come first and are enough for VR; the AR items come after them.

With those, this repository would add an XR camera rig to `scene3d` and XR actions to `input`, reusing `math3d`'s transforms for poses. Until then a 3D game runs on a flat screen.

## For language models

The site publishes the English docs for language models and coding agents, as [nsgo.dev](https://nsgo.dev/llms.txt) does: [llms.txt](https://game.nsgo.dev/llms.txt) lists every page with a line about it, and [llms-full.txt](https://game.nsgo.dev/llms-full.txt) holds every page's Markdown in one file. Both are built from `docs/en` with the site, so they always match these pages.

For coding agents that support Agent Skills, install the game-layer skill with `bunx skills add nsgo-dev/game`; see [Agent Skill](agent-skill.md).
