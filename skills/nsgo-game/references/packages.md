# Package Catalog

Every non-internal package under `github.com/nsgo-dev` (shown relative to that prefix). **Stable** packages only break in minor releases after a deprecation period and never import experimental packages; **Experimental** packages may change in minor releases.

## Core, math and timing

| Package | Status | Purpose |
| --- | --- | --- |
| `game` | Stable | Fixed-step `Loop` (`Rate`, `Update`, `Draw`, `Paused`, `TimeScale`, `Step`, `StepNow`, `Steps`, `ElapsedSeconds`) |
| `game/pkg/math2d` | Stable | Shared 2D `Vec2`, `Rect`, `Circle`, `Affine` types with lossless `core` conversions |
| `game/pkg/random` | Stable | Seeded deterministic `Stream` and named `Streams` with `SetSeed` |
| `game/pkg/screen` | Stable | Virtual resolution `Viewport` scaling, pixel snapping, safe areas, video settings |
| `game/pkg/flow` | Stable | Game-clock `Timers`, `Machine[S]`, `Seq`/`Parallel`/`Race`, typed event `Bus`, Yarn dialogue |
| `game/pkg/tween` | Stable | Fixed-step `Tween`, `Timeline`, easing functions and camera choreography |
| `game/pkg/animclip` | Experimental | Property-keyed animation clips played on scene objects through `tween.Timeline` |
| `game/pkg/loaderr` | Stable | Structured load errors with file, line, field and actionable fix hints |

## 2D rendering, scenes, ECS and assets

| Package | Status | Purpose |
| --- | --- | --- |
| `game/pkg/sprite` | Stable | Atlases (TexturePacker/Aseprite), frames, animations, sprites, effects and `Batch` drawing |
| `game/pkg/tilemap` | Stable | 2D `Camera`, culled tilemaps, Tiled `.tmj` and LDtk loaders, per-tile collision shapes, animated tiles, raycasts |
| `game/pkg/scene` | Stable | World/screen layers, parallax, hit testing, `DispatchPress`, scene stacks and transitions |
| `game/pkg/sceneformat` | Experimental | Plain-text YAML `.scene.yaml` documents, validation, prefab resolution and `SpawnEntities` |
| `game/pkg/entities` | Experimental | Generational ECS `World`, typed component stores, `Transform2D`/`Hierarchy2D`, `Prefab`, codecs |
| `game/pkg/assets` | Experimental | Async per-scene asset groups, `Manager`, reactive loading progress, `LoadingScreen`, DLC manifests |
| `game/pkg/resource` | Experimental | Shared YAML resources (materials, curves, gradients, clips, UI themes, physics materials) and `Library` |
| `game/pkg/text` | Experimental | AngelCode BMFont bitmap fonts, 2D/3D MSDF/SDF fonts and `DrawPlatform` |
| `game/pkg/particles` | Experimental | Seeded deterministic 2D `Emitter`, spawn shapes, bursts and YAML definitions |
| `game/pkg/pixelfx` | Experimental | Go port of pixelfx: painter, frames and light, simulation, rig and acts, effect registry, sprite sheet export, `View` |
| `game/pkg/pixelfx/arcane` | Experimental | pixelfx arcane effects (arcane beam, arcane missiles, arcane orb, blink in/out, magic circle cast, mana burst, portal, rune circle, spell charge, star fall, time stop) |
| `game/pkg/pixelfx/critters` | Experimental | pixelfx critters loops (bat, bird, butterfly, eyeball, fish school, flying skull, frog, ghost, jellyfish, slime, snake, wisp) |
| `game/pkg/pixelfx/explosions` | Experimental | pixelfx explosions (barrel, big, cartoon POW, dirt, firework burst, firework rocket, grenade, implosion, nuke, plasma, small, smoke bomb) |
| `game/pkg/pixelfx/fire` | Experimental | pixelfx fire effects (brazier, campfire, candle, fire breath, fire ring, fire tornado, fireball, flame pillar, flamethrower, meteor, spirit flame, torch) |
| `game/pkg/pixelfx/holy` | Experimental | pixelfx holy effects (blessing, divine shield, feather burst, halo, heal, healing ring, holy pillar, resurrection, smite, sunburst) |
| `game/pkg/pixelfx/ice` | Experimental | pixelfx ice effects (blizzard vortex, freeze, frost breath, frost nova, ice crystal, ice lance, ice shatter, ice spikes, icicle fall, snowflake burst) |
| `game/pkg/pixelfx/impacts` | Experimental | pixelfx impacts (blunt hit, bullet impact, critical hit, ground slam, heavy landing, hit flash, hit spark, impact frame, parry, ricochet, shield block) |
| `game/pkg/pixelfx/lightning` | Experimental | pixelfx lightning effects (ball lightning, chain lightning, charge up, electrified, lightning strike, plasma ball, spark arc, storm cloud, tesla coil, thunderclap, zap hit) |
| `game/pkg/pixelfx/melee` | Experimental | pixelfx melee attacks acted out by the rig (claw swipe, dagger flurry, double slash, hammer smash, heavy cleave, scythe reap, spear sweep, spin slash, sword slash, thrust, uppercut, whip crack) |
| `game/pkg/pixelfx/moves` | Experimental | pixelfx character moves acted out by the rig (arrow shot, earth stomp, ground pound, pistol shots, rocket launcher) |
| `game/pkg/pixelfx/nature` | Experimental | pixelfx nature effects (acid splash, boulder, earth spike, healing bloom, leaf storm, poison cloud, quake, root snare, spore burst, thorns, tornado, toxic puddle, vine whip) |
| `game/pkg/pixelfx/pickups` | Experimental | pixelfx pickups and loot effects (coin, chest, health, mana, keys, orbs, buffs, drops) |
| `game/pkg/pixelfx/projectiles` | Experimental | pixelfx projectile and muzzle-flash effects (arrow, laser, rocket, missile, tracer, cannonball, disc, flare, plasma, muzzles) |
| `game/pkg/pixelfx/scifi` | Experimental | pixelfx sci-fi effects (EMP blast, energy core, force field, glitch, hologram, jetpack, laser grid, scanner, teleporter pad, tractor beam, warp in/out) |
| `game/pkg/pixelfx/shadow` | Experimental | pixelfx shadow effects (black hole, curse, dark explosion, eclipse, grasping hands, hex sigil, life steal, necro skulls, shadow flames, shadow tendrils, soul drain, void orb) |
| `game/pkg/pixelfx/spawn` | Experimental | pixelfx spawn and death effects (coin drop, disintegrate, dissolve burn, egg hatch, poof, respawn beam, shatter death, soul rise, spawn portal, summon rise) |
| `game/pkg/pixelfx/status` | Experimental | pixelfx status effects (bleeding, burning, charmed, confused, cursed, frozen, haste, petrified, poisoned, rage, sleep, slowed, stunned) |
| `game/pkg/pixelfx/transitions` | Experimental | pixelfx 320 x 180 screen transitions (blinds, burn away, CRT off, diamond wipe, glitch wipe, iris, pixel dissolve, shatter wipe, slash wipe, star iris) |
| `game/pkg/pixelfx/ui` | Experimental | pixelfx UI effects (achievement, coin collect, combo counter, confetti, countdown, crit/damage numbers, exclamation, health bar hit, heart lost, level up, quest complete) |
| `game/pkg/pixelfx/water` | Experimental | pixelfx water effects (bubbles, drip, fountain, geyser, rain splash, ripples, splash, water jet, water orb, waterfall, wave crash, whirlpool) |
| `game/pkg/pixelfx/weather` | Experimental | pixelfx weather loops (aurora, blizzard, cherry blossoms, clouds, falling leaves, fireflies, fog, god rays, rain, shooting stars, snow, storm) |
| `game/pkg/lights2d` | Experimental | 2D ambient darkness, point/cone lights, visibility-polygon shadows, normal-mapped sprites |
| `game/pkg/tilemap/tileshape` | Experimental | Tilemap collision geometry: a physics `TileGrid` view, slopes from Tiled per-tile shapes, merged outlines, static boxes and `lights2d` occluders |
| `game/pkg/postfx` | Experimental | Chained Kage shader post-processing passes over the game frame |
| `game/pkg/skeletal` | Experimental | 2D bone rigs, IK, skins and YAML rig definitions |
| `game/pkg/cutscene` | Experimental | Skippable video and YAML timeline cutscenes (camera, actor, dialogue, sound and music tracks) |
| `game/pkg/placeholder` | Experimental | CC0 prototyping pack (sprites, tiles, pixel font, sounds, glTF models) with import sidecars |

## Input, 2D physics, audio and UI

| Package | Status | Purpose |
| --- | --- | --- |
| `game/pkg/input` | Stable | Named `Actions` and axes across keyboard, mouse, touch, pointer-lock and gamepads; `core.Storage` bindings |
| `game/pkg/physics` | Stable | `Box`/`Circle` overlaps, `MoveAndSlide` tile resolution, `World`, `CharacterBody`, `RigidWorld` |
| `game/pkg/sound` | Stable | `Mixer`, sound effects, music playback, bus routing, 3D positional audio and pause-with-game |
| `game/pkg/gameui` | Experimental | In-canvas box-layout menus, virtual-resolution scaling, spatial focus nav, world-anchored views |
| `game/pkg/a11y` | Experimental | Live captions, color-blind palettes, CVD simulation, assist options, accessible menus |
| `game/pkg/l10n` | Experimental | JSON string tables with CLDR plurals, script `FontSet`, RTL ordering, localized asset catalogs |
| `game/pkg/quality` | Experimental | Low/medium/high quality presets from device class and measured frame times |

## 3D stage

| Package | Status | Purpose |
| --- | --- | --- |
| `game/pkg/math3d` | Experimental | 3D vectors, matrices, quaternions, transforms, AABBs, rays and frustums |
| `game/pkg/scene3d` | Experimental | 3D node tree, sockets/attachments, dynamic BVH, Orbit/FPS/ThirdPerson cameras, picking, decals |
| `game/pkg/render3d` | Experimental | GPU mesh buffers, textures/samplers, culling, sorting, 0-alloc instancing, LODs, gizmos, trails, `RenderStats` |
| `game/pkg/scenemodel` | Experimental | Scene file objects drawn in 3D: built-in primitives (`mesh`) and glTF models (`model`, `modelNode`, `modelMesh`) with material overrides, built into a `scene3d` tree with bounds, and a lit, textured `Renderer` |
| `game/pkg/anim3d` | Experimental | 3D pose state machines, 1D/2D blend spaces, bone masks, clip events, IK, root motion |
| `game/pkg/physics3d` | Experimental | 3D rigid bodies, shapes, raycasts, sweeps and character movement |
| `game/pkg/world3d` | Experimental | Heightmap terrain, multi-level 3D navmeshes, 3D `GridMap`, foliage, floating-origin streaming |
| `game/pkg/ai/navbake3d` | Experimental | Recast-style navigation mesh baking from glTF, scenes, terrain, GridMaps and static colliders, one mesh per agent type, versioned YAML output, debug draw |
| `game/pkg/voxel` | Experimental | Chunked 3D voxel worlds, greedy meshing, cross-chunk AO, DDA raycasting, YAML saves |
| `game/pkg/gltf` | Experimental | glTF 2.0 (`.gltf`/`.glb`) loader for meshes, materials, skins and animations |
| `game/pkg/vehicle3d` | Experimental | Raycast cars on `physics3d`, suspension, tire friction, checkpoints, laps and YAML lap ghosts |

## Gameplay and genre kits

| Package | Status | Purpose |
| --- | --- | --- |
| `game/pkg/ai` | Stable | Grid `AStar`, 2D `NavMesh`, steering behaviors, behavior trees and perception (sight, hearing, memory) |
| `game/pkg/ai/perception3d` | Experimental | `ai` perception for 3D agents on `math3d.Vec3`, with `physics3d` line of sight |
| `game/pkg/ai/navbake` | Experimental | Bakes 2D navmeshes from obstacles or tilemaps with an agent radius, versioned YAML meshes, debug drawing |
| `game/pkg/platformer` | Experimental | Platformer `Controller` (coyote time, jump buffer, variable jump, wall slide/jump) from YAML |
| `game/pkg/bullets` | Experimental | Pooled shooter bullet patterns (rings, spirals, aimed bursts) from YAML |
| `game/pkg/cards` | Experimental | Board slot drag-and-drop, fanned hands, decks, piles, grids and YAML boards |
| `game/pkg/rooms` | Experimental | Room maps with per-room camera bounds and cut/slide/fade transitions |
| `game/pkg/worldmap` | Experimental | YAML room maps, saved discovery/markers, minimap and world map |
| `game/pkg/fog` | Experimental | Per-team shadowcast fog of war, explored vs visible cells, map overlay, JSON saves |
| `game/pkg/rts` | Experimental | Flow fields, `Crowd` separation, YAML formations, box selection and control groups |
| `game/pkg/procgen` | Experimental | Seeded BSP rooms, cellular-automata caves and wave function collapse from YAML rules |
| `game/pkg/rhythm` | Experimental | YAML beat maps, audio-clock music timing and per-device A/V latency calibration |
| `game/pkg/undo` | Experimental | Reversible moves with unlimited undo/redo/`Seek` and YAML `Log` |
| `game/pkg/fighting` | Experimental | Per-step input buffer, numpad motion inputs, charges, chords and combos from YAML |
| `game/pkg/framedata` | Experimental | Fighting game per-frame hitboxes, hurtboxes, hitstun and cancel windows in YAML |
| `game/pkg/stats` | Experimental | Base stats with sourced add/mul/override modifiers, timed modifiers and YAML configs |
| `game/pkg/inventory` | Experimental | YAML item catalogs, stacking containers, typed equipment slots and snapshots |
| `game/pkg/ability` | Experimental | YAML ability sets and per-step `Caster` (costs, cooldowns, charges, cast/channel times) |
| `game/pkg/quest` | Experimental | YAML quest books with objectives, prerequisites, rewards and event-bus progression |
| `game/pkg/loot` | Experimental | Nested weighted drop tables in YAML with guaranteed drops and hard/soft pity |
| `game/pkg/vn` | Experimental | Visual novel presenter (YAML cast, portraits, typewriter text, choices, backlog, auto/skip) |
| `game/pkg/idle` | Experimental | Big `Number` beyond float64, capped offline progress and localized formatting |

## Persistence, networking, liveops and modding

| Package | Status | Purpose |
| --- | --- | --- |
| `game/pkg/save` | Stable | Versioned JSON save slots, schema migrations, player settings and atomic backup recovery |
| `game/pkg/online/netplay` | Experimental | Cross-target WebSocket/UDP/loopback transports, snapshot replication, lockstep/rollback |
| `game/pkg/online/netplay/wire` | Experimental | YAML wire message schemas (`*.wire.yaml`), integer-only `qfloat`/`fixed` quantization, bit packing and zero-allocation Go codec generation |
| `game/pkg/online/wsclient` | Experimental | Plain RFC 6455 WebSocket client for third-party servers, same API on native and web |
| `game/pkg/online/gameserver` | Experimental | Headless fixed-step `Runner` for dedicated game servers with multiple match loops |
| `game/pkg/online/liveops` | Experimental | Cloud sync interfaces (`CloudSync`), conflict handling and liveops helpers |
| `game/pkg/online/liveops/crashreport` | Experimental | NSGo-free crash report JSON schema (`CrashReport`, `StepRecord`) shared by `liveops` and `server` |
| `game/pkg/online/accounts` | Experimental | Self-hosted player accounts, guest upgrades, OAuth/email, argon2id passwords |
| `game/cmd/accountsd` | Experimental | Self-hosted accounts daemon |
| `game/pkg/online/accounts/authtoken` | Experimental | Ed25519 account tokens verified offline by game servers |
| `game/pkg/online/accounts/pgstore` | Experimental | PostgreSQL store backend for `game/online/accounts` |
| `game/pkg/online/accounts/sqlitestore` | Experimental | SQLite store backend for `game/online/accounts` |
| `game/pkg/online/accounts/sqlstore` | Experimental | Shared SQL helpers for `game/online/accounts` stores |
| `game/pkg/online/accounts/storetest` | Experimental | Conformance test suite for `game/online/accounts` stores |
| `game/pkg/online/cloudsave` | Experimental | Self-hosted HTTPS cloud save service behind `liveops.CloudSync` |
| `game/cmd/cloudsaved` | Experimental | Self-hosted cloud save daemon |
| `game/pkg/online/cloudsave/pgstore` | Experimental | PostgreSQL store backend for `game/online/cloudsave` |
| `game/pkg/online/cloudsave/sqlitestore` | Experimental | SQLite store backend for `game/online/cloudsave` |
| `game/pkg/online/cloudsave/sqlstore` | Experimental | Shared SQL helpers for `game/online/cloudsave` stores |
| `game/pkg/online/cloudsave/storetest` | Experimental | Conformance test suite for `game/online/cloudsave` stores |
| `game/pkg/online/achievements` | Experimental | YAML achievement definitions, monotonic `core.Storage` progress, Game Center / Play Games |
| `game/pkg/online/audit` | Experimental | Append-only JSON Lines audit log for servers, one file per UTC day, non-blocking queue with drop counter, retention |
| `game/pkg/online/leaderboard` | Experimental | YAML leaderboards for Game Center and Play Games |
| `game/pkg/online/leaderboard/selfhost` | Experimental | Self-hosted cross-platform leaderboards (all-time/daily/weekly, signed scores) |
| `game/cmd/leaderboardd` | Experimental | Self-hosted leaderboard daemon |
| `game/pkg/online/presence` | Experimental | Provider-neutral rich presence (`Activity`, `Manager`) on Steam and Discord |
| `game/pkg/modding` | Experimental | Capability-sandboxed TSX `"game"` modding API and `mod.yaml` lifecycle manager |

## Debugging, testing, MCP and editor

| Package | Status | Purpose |
| --- | --- | --- |
| `game/pkg/tooling/gamedebug` | Experimental | Frame-time `Overlay`, `Profiler`, `GCMonitor`, `MemoryTracker`, `Console` tunables, wireframe debug draw (`-tags release` stripped) |
| `game/pkg/mcp` | Experimental | `Bridge` registering game MCP tools with `nsgo/automation` for `nsgo mcp` and `nsgotest` |
| `game/pkg/replay` | Experimental | `Recorder`/`Player`, editable YAML recordings and deterministic replay assertions |
| `game/pkg/tooling/playtest` | Experimental | `playtest.yaml` parser/encoder, headless seeded bot runner with replays, metrics collector, `platformer-runner` profile and YAML report |
| `game/pkg/statedump` | Experimental | Headless deterministic YAML state dumps and line diffs between runs |
| `game/pkg/gametest` | Experimental | Testing helpers and fixtures for game-layer tests |
| `game/pkg/perf` | Experimental | Zero-alloc `Pool`/`Arena`/`SlicePool`, `SpatialHash`, `Quadtree` and `CullBatch` |
| `game/pkg/editor` | Experimental | Visual scene and animation clip editor embedded behind `go tool game editor` |
| `game/pkg/editor/editorcmd` | Experimental | The scene editor's command line, shared by `go tool game editor` and a game's own editor command from `game generate editor` |
| `game/pkg/editor/editormcp` | Experimental | The editor's MCP server: `scene_read`, `scene_edit`, `selection_get`, `viewport_image`, `game_check`, `game_playtest` and `playtest_define` on the open project, served on a per-project local endpoint that `go tool game mcp` forwards to |

## Project, CLI and asset tooling

| Package | Status | Purpose |
| --- | --- | --- |
| `game/pkg/project` | Experimental | `game.yaml` parser, canonical encoder and `nsgo.yaml` consistency checker |
| `game/pkg/tooling/refs` | Experimental | Stable file UIDs, project index and reference-rewriting file moves |
| `game/pkg/plugin` | Experimental | Runtime Go `Plugin`, `Host`, atomic deterministic `Install`, standard scheduler stages and `OnWorld` codecs |
| `game/pkg/plugin/devtools` | Experimental | Development-time plugin wiring (`PrefabSpec` for `mcp.Bridge` and `playtest` bot factories) split out to keep game binaries lean |
| `game/pkg/plugin/manifest` | Experimental | Strict `plugin.yaml` parser, validator and deterministic byte-ordered encoder for Go plugins |
| `game/cmd/editor` | Experimental | Standalone desktop application entrypoint for the NSGo Game Editor |
| `game/cmd/game` | Experimental | The `game` CLI (`go tool game` / `go run github.com/nsgo-dev/game/cmd/game@latest`) |
| `game/cmd/gamewire` | Experimental | Generates and checks deterministic zero-allocation Go codecs from `*.wire.yaml` wire message schemas |
| `game/cmd/nsgo-game` | Experimental | The `nsgo-game` PATH extension binary for NSGo's generic extension mechanism (requires an NSGo release newer than v0.3.4) |
| `game/pkg/datatable` | Experimental | Typed YAML/CSV/JSON data tables with cell-level errors and hot reload |
| `game/pkg/datatable/datatablewatch` | Experimental | Hot reload of `datatable.Table` through a `tooling/hotreload` watcher |
| `game/pkg/importsettings` | Experimental | `<asset>.import.yaml` sidecar parser and encoder |
| `game/pkg/jsonschema` | Experimental | JSON Schema builder and validator with JSON Pointer error paths |
| `game/pkg/ktx2` | Experimental | KTX2 texture encoder (BC1, BC3, ETC2, RGBA8, mipmaps, sRGB) |
| `game/pkg/tooling/assetbuild` | Experimental | Incremental asset pipeline driven by `game.yaml` |
| `game/pkg/tooling/assetcheck` | Experimental | Validator for scenes, Tiled/LDtk maps, atlases and file references |
| `game/pkg/tooling/atlaspack` | Experimental | PNG sprite atlas packer outputting PNG pages and TexturePacker JSON |
| `game/pkg/tooling/audioconv` | Experimental | WAV normalization, resampling and WAV/AAC/MP3 conversion |
| `game/pkg/tooling/bench` | Experimental | Headless benchmark runner and baseline regression checker |
| `game/pkg/tooling/credits` | Experimental | Asset license/author credits generator from `.import.yaml` sidecars |
| `game/pkg/tooling/devstate` | Experimental | Dev session state snapshots persisted across reloads |
| `game/pkg/tooling/hotreload` | Experimental | Asset, 3D shader (`ShaderReloader`) and material (`MaterialReloader`) hot reload |
| `game/pkg/tooling/library` | Experimental | Digest-verified asset, plugin and template pack registry |
| `game/pkg/tooling/manifest` | Experimental | Typed Go asset manifest generator (`asset:` URIs or `//go:embed`) |
| `game/pkg/tooling/sizecheck` | Experimental | WebAssembly and mobile package size measurement and budget checks behind `game size` |
