# Coming from Unreal Engine

A map from Unreal Engine's levels, actors, Blueprints, data assets, Enhanced Input, Gameplay Ability System, AI, Niagara, audio, Sequencer, UMG, Chaos Physics, materials, replication, saves and packaging to this layer, with the page that covers each one.

A game here is pure Go code inside an NSGo component rather than C++ modules and Blueprint graphs attached to a UObject hierarchy. Every author-facing file—scenes, prefabs, materials, data tables, ability sets, behavior trees, UI screens, cutscenes and project settings—is plain-text versioned YAML instead of binary `.uasset` or `.umap` packages, so every edit reads as a clean diff in Git with no exclusive file locks. HUD and menus are native NSGo views over the game's canvas or [gameui](gameui.md) screens drawn inside it.

## At a glance

| Unreal Engine | Here | Guide |
| --- | --- | --- |
| `Tick`, fixed physics substepping | `game.Loop.Update(dt)` at a fixed `Loop.Rate`; `game.Loop.Draw(alpha)` with interpolation | [Loop](loop.md) |
| Level (`.umap`), Sublevels, World Partition & Data Layers | YAML scene files (`sceneformat`), `scene.Manager` stacks, `world3d.ChunkStreamer` & `FloatingOrigin` (scene cell partitioning & data layers: [#1728](https://github.com/nsgo-dev/game/issues/1728)) | [Editor & Scene Format](editor.md), [scene](scene.md), [world3d](world3d.md) |
| Actor, `SceneComponent`, `ActorComponent`, Blueprint class defaults | Scene objects, `scene3d.Node` with named sockets, `entities.World` entities, components, systems & `entities.Prefab` | [Entities](entities.md), [scene3d](scene3d.md) |
| Blueprints, Level Blueprint, Blueprint mods | Plain Go and YAML (`sceneformat` connections; `pkg/modding` TSX sandbox for player mods; no visual scripting; declarative scene rules: [#1753](https://github.com/nsgo-dev/game/issues/1753)) | [Editor & Scene Format](editor.md), [flow](flow.md), [modding](modding.md) |
| Data Asset, Data Table, Curve Table | YAML [resource](resources.md) files (`resource.Library`, `resource.Curve`) and typed `datatable` YAML/CSV/JSON tables | [resource](resources.md), [datatable](datatable.md) |
| Enhanced Input (Input Actions & Mapping Contexts) | `input.Actions` actions and axes declared in `game.yaml` (`input.Players` for local multiplayer) | [input](input.md), [Project File](project.md) |
| Gameplay Ability System (Abilities, Attributes, Tags, Effects, Cues) | `ability` (`Set`, `Caster`) and `stats` (`Sheet`, `Modifier`, `Effect`); string conditions today (tags: [#1720](https://github.com/nsgo-dev/game/issues/1720), effects: [#1721](https://github.com/nsgo-dev/game/issues/1721), cues: [#1722](https://github.com/nsgo-dev/game/issues/1722)) | [ability](ability.md), [stats](stats.md) |
| Behavior Trees, Blackboard, EQS, AI Perception, NavMesh | `ai` behavior trees (`*.bt.yaml`, `Blackboard` observer aborts), `TacticalQuery`, `Perception` / `perception3d`, `pathfind.NavMesh` & `pathfind3d.NavMesh` | [ai](ai.md), [pathfind](pathfind.md), [pathfind3d](pathfind3d.md) |
| Niagara particle systems | `particles` 2D `Emitter` and 3D `Emitter3D` + instanced `GPUParticles3D` (multi-emitter files: [#1730](https://github.com/nsgo-dev/game/issues/1730), 3D features: [#1377](https://github.com/nsgo-dev/game/issues/1377), GPU compute: [#1639](https://github.com/nsgo-dev/game/issues/1639)) | [particles](particles.md) |
| Sound Cues, Attenuation, Sound Classes & Submixes | `sound.Cue`, `Listener2D` / `Listener3D` / `Playback3D`, `sound.Mixer` buses & `busLayout` DSP chains (YAML sound files: [#1401](https://github.com/nsgo-dev/game/issues/1401), concurrency groups: [#1731](https://github.com/nsgo-dev/game/issues/1731)) | [sound](sound.md), [resource](resources.md) |
| Sequencer, Media Player | `cutscene` version 2 YAML timelines (`TimelinePlayer`) & skippable video cutscenes (`Player`); `animclip` property clips (3D timelines: [#1633](https://github.com/nsgo-dev/game/issues/1633)) | [cutscene](cutscene.md), [animclip](animclip.md) |
| UMG & Common UI | Native NSGo views over the canvas, or `gameui` (`*.screen.yaml`, `Root`, `SpatialNavigator`, `SettingsMenu`, `AnchorOverlay`) | [gameui](gameui.md) |
| Chaos Physics (rigid bodies, constraints, `CharacterMovementComponent`, vehicles) | `physics` / `RigidWorld` (2D), `physics3d.World`, `CharacterController`, joints & ragdolls, `vehicle3d` raycast cars | [physics](physics.md), [physics3d](physics3d.md), [vehicle3d](vehicle3d.md) |
| Materials, Lumen, Nanite, Post-Process Volumes | `resource.Material` YAML, `render3d` PBR/toon/WGSL materials, `LightRig`, `CascadedShadowMap`, `LightmapRenderer`, `PostStack3D`; no Lumen or Nanite (lightmap baker: [#1396](https://github.com/nsgo-dev/game/issues/1396)) | [render3d](render3d.md), [resource](resources.md) |
| Replication, Dedicated Server | `pkg/online/netplay` (`ServerWorld`/`ClientWorld` deltas, WebTransport/WebSocket/UDP, lockstep/rollback) & `pkg/online/gameserver` (`Runner`); ECS replication: [#1444](https://github.com/nsgo-dev/game/issues/1444) | [netplay](netplay.md), [gameserver](gameserver.md) |
| SaveGame objects & slots | `save.Store` versioned JSON slots with migrations & atomic backup recovery; `pkg/online/cloudsave` | [save](save.md), [cloudsave](cloudsave.md) |
| Packaging, Cooking, Export targets | `game.yaml` export presets, `go tool game assets`, `go tool game release`, `go tool nsgo build` & `go tool nsgo check` | [Building and Shipping](shipping.md), [Command Line](cli.md) |

## Levels and World Partition

Unreal stores each level in a binary `.umap` file and streams large worlds through World Partition and Data Layers. Here the layout, runtime switching and streaming live in three places:

- **Scene files (`sceneformat`)**: a level is a versioned YAML file with a camera, tilemaps, ordered layers of objects, instanced or inherited child scenes, and `components` and `properties` maps. `sceneformat.Parse` refuses unknown fields, `Encode` writes deterministic bytes, and `go tool game merge` provides an object-level Git merge driver so teams edit the same scene without locking files. See [Editor & Scene Format](editor.md).
- **Scene transitions (`scene.Manager`)**: `Switch` replaces the active scene with a `Fade`, `Crossfade` or `Wipe` transition; `Push` with a `Transparent` controller overlays a pause menu or HUD; `Pop` removes it. Each scene's `Enter` gets a child `*core.Scope`, disposing its loops, timers, sounds and signals automatically on exit. See [scene](scene.md).
- **Large 3D worlds (`world3d`)**: `Heightmap` with `TerrainLOD` (stitched LOD chunks and up to 8 splat-mapped PBR layers), modular `GridMap`, procedural `FoliageField`, `FloatingOrigin` (`WorldPos64` in `float64` to prevent `float32` jitter far from the origin, filling the role of Large World Coordinates / World Origin Rebasing), and asynchronous `ChunkStreamer` (with `pathfind3d.NavTileStreamer` for navigation tiles) loads and unloads world chunks around the player. See [world3d](world3d.md) and [pathfind3d](pathfind3d.md).

Partitioning a single large scene YAML file into streamed grid cells and runtime-toggled Data Layers is tracked in [#1728](https://github.com/nsgo-dev/game/issues/1728).

## Actors, components and coordinates

Unreal builds gameplay around `AActor` subclasses that own `USceneComponent` transforms and `UActorComponent` logic. Here data, transforms and update logic stay separate:

- **Gameplay objects (`entities`)**: an `entities.Entity` is a 64-bit generational handle, so stale references never alias newly spawned entities. Components are plain Go structs attached with `entities.Set`, `Get`, `Has` and `Remove`; systems iterate them allocation-free with `Each1`/`Each2`/`Each3` and declare execution order with `Scheduler.AddSystem` (`Before`, `After`), replacing Unreal's tick groups and tick prerequisites. `entities.Prefab` (`With`, `WithFunc`, `Clone`, `Spawn`) fills the role of Blueprint actor templates. See [Entities](entities.md).
- **3D hierarchy (`scene3d`)**: `scene3d.Node` holds parent-child local transforms, named sockets (`DefineSocket`, `Attach`) for attaching weapons or props to bones, tags (`FindByTag`), typed attachments (`SetAttachment`), and a dynamic `BVH` for frustum culling and picking. See [scene3d](scene3d.md).
- **3D skeletal animation (`anim3d`)**: replaces Animation Blueprints with Go-driven `StateMachine`, `BlendSpace1D`/`BlendSpace2D` with foot-phase sync, `LayerStack` with `BoneMask` override and additive poses, timed `ClipEvent`s (like Anim Notifies), two-bone and FABRIK IK, and root motion extraction. See [anim3d](anim3d.md); authoring animation graphs in YAML is tracked in [#1405](https://github.com/nsgo-dev/game/issues/1405).

Coordinates and units differ from Unreal's:

- **3D**: Unreal is left-handed, Z-up (`+X` forward, `+Y` right, `+Z` up) in **centimetres**. Here `math3d`, `scene3d`, `render3d`, `physics3d` and `world3d` are **right-handed, Y-up** (`+X` right, `+Y` up, `-Z` forward, matching glTF 2.0, WebGPU and Metal) in **metres**. A point `(ux, uy, uz)` in Unreal centimetres maps to `(uy / 100, uz / 100, -ux / 100)` in metres here. Angles are in radians (`DegToRad` / `RadToDeg`). See [math3d](math3d.md) and [3D Models from Blender](3d-models-blender.md).
- **2D**: screen space with `+X` right, `+Y` down, clockwise rotation in radians, and units in points.

## Blueprints and scripting

There is no visual node-graph scripting language (no Blueprints). The engine splits what Blueprints do into three parts:

- **Gameplay logic in Go**: written directly as Go systems, state machines (`flow.Machine`) and functions compiled into the binary—no reflection glue, fast incremental builds and deterministic fixed-step simulation ([Determinism](determinism.md)).
- **Events and latent actions (`flow`)**: Event Dispatchers map to a typed `flow.Bus` (`Subscribe`, `Publish`, `Enqueue`/`Flush`); Blueprint `Delay`, timers and timelines map to `flow.Seq`/`Parallel`/`Race`, `flow.Timers` and `tween.Timeline`, all advancing strictly on fixed steps with no goroutines. See [flow](flow.md) and [tween](tween.md).
- **Scene wiring and player mods**: simple object-to-object wiring in a scene file uses `connections` (`sceneformat.Connect`, `Signals.Emit` and `Signals.Listen`); declarative conditions and built-in actions without Go are tracked in [#1753](https://github.com/nsgo-dev/game/issues/1753). For user-generated mods at run time, `pkg/modding` runs capability-sandboxed TSX/JS scripts with `mod.yaml` manifests, dependency resolution, `ContentFS` overlays and optional Steam Workshop sync. See [modding](modding.md). Editor extensions are Go `editor.Plugin`s compiled into the project's editor command (TSX editor plugins are tracked in [#1288](https://github.com/nsgo-dev/game/issues/1288)).

## Data assets, data tables and curves

- **Data Assets (`resource`)**: Unreal's `UDataAsset` and shared asset types map to versioned YAML [resource](resources.md) files loaded and shared by path through `resource.Library` (`material`, `curve`, `gradient`, `animation`, `theme`, `busLayout`, `physicsMaterial`), with live reload in dev via `hotreload.Watcher.WatchResources`. For custom game data assets, define a Go struct and decode a strict versioned YAML file.
- **Data Tables (`datatable`)**: versioned YAML tables (`version: 1`, `rows:`)—as well as CSV and JSON spreadsheet exports—decode directly into typed Go structs via `datatable.LoadFile[T]` or `LoadYAML[T]`, reporting cell-level `*datatable.CellError` diagnostics, saving deterministically with `SaveYAML`, editing in the scene editor's Data Table view, and hot-reloading in dev with `datatablewatch.Watch`. See [datatable](datatable.md).
- **Curve Tables**: `resource.Curve` (`type: curve`, `linear`/`step`/`smooth` keys, `Curve.Eval(t)`) and `particles.FloatCurve` / `ColorCurve` replace `UCurveFloat` and Curve Tables.
- **Asset loading & references**: runtime files under `resources/` open by `"asset:<path>"` URIs on every target, and `assets.Manager` loads named per-scene groups asynchronously with reactive progress for loading screens ([assets](assets.md)). Instead of Unreal's asset redirectors, `pkg/tooling/refs` keeps stable `uid` headers and `game.index.yaml`, and `go tool game mv` / `go tool game index` rewrite every reference in place when files move ([Stable references](project.md#stable-references)).

## Enhanced Input

`input.Actions` maps named actions and axes to keyboard keys, mouse buttons and pointer-lock mouselook, gamepad buttons and sticks, touch regions, on-screen `TouchControls` (`*.touch.yaml`), and device motion sensors. Declare the default input map in `game.yaml`'s `input` section (`project.File.BindInput(actions)`) or bind in Go, call `actions.Step()` once at the start of `game.Loop.Update`, and read the step's snapshot:

| Unreal Enhanced Input | Here |
| --- | --- |
| Input Action (`IA_*`) | Named action or axis in `game.yaml` `input:` or `input.Actions` |
| Input Mapping Context (`IMC_*`) | `project.File.BindInput(actions)`, or mode-specific bindings swapped in Go |
| `Started` / `Triggered` / `Completed` | `actions.JustPressed(name)` / `actions.Pressed(name)` / `actions.JustReleased(name)` |
| 1D / 2D Axis with Dead Zone & Negate modifiers | `BindAxis`, `BindGamepadAxis` (`GamepadAxisOptions{DeadZone, Axial, Sensitivity, Invert}`), `BindPointerAxis` (`Delta`), `BindMotionAxis` |
| Touch Interface Setup | `input.TouchControls` (`*.touch.yaml`) with safe-area anchoring, auto hide/show and `VirtualPad` |
| Local multiplayer input routing | `input.Players` with exclusive gamepad assignment, split keyboard and `DisconnectPolicy` |
| Player Mappable Key Settings | `actions.Save` and `actions.Load` in `core.Storage` (`Load` merges per name so new defaults stay bound) |

Call `actions.WatchKeys(cx)` and `actions.WatchGamepads(cx)` once on the loop scope to hear window-wide keyboard and controller input. Motion-input buffers, charges and chords for action or fighting games live in [fighting](fighting.md). Player-facing sensitivity, invert-axis and dead-zone settings wired into input and 3D cameras are tracked in [#1780](https://github.com/nsgo-dev/game/issues/1780). See [input](input.md) and [Project File](project.md).

## Gameplay Ability System

Two packages cover the core of Unreal's Gameplay Ability System (GAS) on deterministic fixed steps:

- **Gameplay Abilities (`ability`)**: author an ability set in versioned YAML (`costs`, `cooldown`, `charges` and `recharge`, `castTime`, `channelTime` with `tickInterval`, `trigger: press|hold`, and `conditions` such as `[grounded, '!silenced']`), then step a `ability.Caster` per character after `actions.Step()`. `Caster` checks `CanActivate`/`TryActivate`, spends `Resources` (`ability.Pool` or a custom adapter), fires `OnEvent` (`EventStarted`, `EventActivated`, `EventChannelTick`, `EventInterrupted`, `EventCooldownEnded`), and supports deterministic `Snapshot`/`Restore` for saves and rollback. See [ability](ability.md).
- **Attribute Sets & Modifiers (`stats`)**: `stats.Sheet` holds named base stats with `Min`/`Max` bounds, `OpAdd`, `OpMul` and prioritized `OpOverride` modifiers tagged by source (`"equip:weapon"`, `"buff:haste"`), fixed-step `AddTimed` modifiers, grouped `Effect`s (`Apply` to stack, `Refresh` to reset duration cleanly), `OnChange` callbacks, and versioned YAML stats files. See [stats](stats.md).

What is still planned compared to full GAS:

- **Gameplay Tags**: conditions and modifier sources are plain strings today; a project-wide hierarchical tag registry in YAML with tag containers and tag queries is tracked in [#1720](https://github.com/nsgo-dev/game/issues/1720).
- **Gameplay Effects**: instant, timed and periodic attribute/resource changes with stacking policies, granted tags and immunity applied directly by abilities are tracked in [#1721](https://github.com/nsgo-dev/game/issues/1721).
- **Gameplay Cues**: declarative YAML cues mapping named cosmetic events from abilities and effects to particles, sounds and camera shake are tracked in [#1722](https://github.com/nsgo-dev/game/issues/1722).

## AI: behavior trees, tactical queries and perception

Package `ai` covers Unreal's AI toolset on the fixed step with zero background goroutines:

- **Behavior Trees & Blackboard**: build `ai.Node` trees in Go or in versioned `*.bt.yaml` files with subtrees, parameter substitution and key remapping. Composites (`Sequence`, `Selector`, `Parallel`, `WeightedSelector`, `UtilitySelector`), decorators (`Guard`, `Cooldown`, `TimeLimit`, `Retry`, `Service`) and `BlackboardCondition` with Unreal-style observer aborts (`AbortNone`, `AbortSelf`, `AbortLower`, `AbortBoth`) work over typed `ai.Blackboard`s with optional shared squad parents (`NewChildBlackboard`). `ai` also includes GOAP (`GOAPPlanner`), attack token pools (`TokenPool`), engagement rings (`SlotAssigner`), `Squad` roles/orders and `InfluenceLayer` maps.
- **EQS (Environment Query System)**: `ai.TacticalQuery` runs candidate generators (`CoverPointGenerator`, `RingGenerator`, `GridGenerator`, `NavMeshGenerator`) through ordered filter-and-score tests (`DistanceTest`, `AngleTest`, `InfluenceTest`, `CoverHeightTest`, `LOSTest`, `PathDistanceTest`, `CustomTest`), paired with `PointReservations` (preventing two agents from claiming the same spot) and a per-step `QueryBudget`.
- **AI Perception**: `ai.Perception` / `PerceptionSystem` (2D) and `ai/perception3d` (3D) combine sight cones (with peripheral range and budgeted line-of-sight adapters), hearing (`PostNoise`), team attitudes (`Hostile`, `Neutral`, `Friendly`) and decaying memory (`EventGained`, `EventUpdated`, `EventLost`, `EventForgotten`). See [ai](ai.md) and [3D perception](ai.md#3d).
- **Navigation**: 2D `pathfind.NavMesh` / `TiledNavMesh` (baked with [navbake](navbake.md)) and multi-level 3D `pathfind3d.NavMesh` / `TiledNavMesh` / `NavTileStreamer` (baked with [navbake3d](navbake3d.md) and `go tool game navmesh`) support polygon areas, runtime flags, off-mesh links, `NavAgent` / `NavAgentGroup`, and `ORCASolver` local avoidance.

Tracked AI gaps: `NavAgent` following dynamic `TiledNavMesh` updates ([#1717](https://github.com/nsgo-dev/game/issues/1717)), a combined NavMesh + ORCA crowd controller ([#1718](https://github.com/nsgo-dev/game/issues/1718)), and Smart Objects with claimable interaction slots ([#1726](https://github.com/nsgo-dev/game/issues/1726)).

## Particles and audio

- **Niagara → `particles`**: seeded, deterministic 2D `Emitter` (spawn shapes, bursts, sub-emitters, size/alpha/rotation/color lifetime curves, YAML configs and `HotConfig` hot reload) and 3D `Emitter3D` + `GPUParticles3D` (3D shapes, spherical/cylindrical/stretched billboards, depth sorting, additive/alpha blending, drawn in a single instanced GPU call with no compute shaders required). See [particles](particles.md). Planned additions: multi-emitter effect files with named parameters and particle events ([#1730](https://github.com/nsgo-dev/game/issues/1730)), 3D sub-emitters, collision, attractors, turbulence, trails and mesh particles ([#1377](https://github.com/nsgo-dev/game/issues/1377)), and optional GPU compute simulation ([#1639](https://github.com/nsgo-dev/game/issues/1639)).
- **Sound Cues, attenuation & submixes → `sound`**: `sound.Mixer` manages named volume buses persisted in `core.Storage` (`music`, `sfx`, `ui`), crossfading `Music` (`PlayLooped` for intro+loop), voice-limited `Effect`s, randomized `sound.Cue` variants (weighted selection, no-repeat window, pitch/volume ranges), `Listener2D` (`PlayAt`) and `Listener3D` (`PlayAt3D` and live-updating `Play3D` / `Playback3D` with linear/inverse/exponential rolloff, rear head-shadow and directional emission cones), and bus DSP chains (`LowPass`, `HighPass`, `Delay`, `Reverb`, `Gain`, `Bitcrush`) configured in `game.yaml` or `resource.BusLayout` (currently applied at load time to in-memory 16-bit PCM WAV). See [sound](sound.md). Tracked gaps: live software mixer for real-time bus effects ([#1398](https://github.com/nsgo-dev/game/issues/1398)), YAML sound cue files ([#1401](https://github.com/nsgo-dev/game/issues/1401)), scene emitters & Doppler ([#1402](https://github.com/nsgo-dev/game/issues/1402)), and shared sound concurrency/ducking groups ([#1731](https://github.com/nsgo-dev/game/issues/1731)).

## Sequencer and UI

- **Sequencer & Media Player → `cutscene` and `animclip`**: `cutscene` plays both skippable video cutscenes (`version: 1` or `2` over `core.Video`, pausing `Loop` and `Mixer`) and deterministic in-engine YAML timeline cutscenes (`version: 2`, `kind: timeline`) with `camera`, `actor`, localized `dialogue`, `sound` and `music` tracks driven by `TimelinePlayer` / `Player.PlayTimeline` (with `Seek` scrubbing and `go tool game check` validation). Property-keyed animation clips for objects and sprites live in [animclip](animclip.md). Tracked gaps: 3D timeline cutscenes ([#1633](https://github.com/nsgo-dev/game/issues/1633)) and signal, activation, nested and custom timeline tracks ([#1632](https://github.com/nsgo-dev/game/issues/1632)).
- **UMG & Common UI → NSGo views and `gameui`**: you can layer ordinary native NSGo views over the canvas (bound to `core.Signal` values), or build in-canvas HUDs and menus with `gameui`—either in Go or in strict `*.screen.yaml` files (`vbox`, `hbox`, `label`, `button`, `image_progress`, `touch_stick`, `touch_button`, nine-point anchors, safe areas, `deviceClasses`, `SpatialNavigator` for gamepad/keyboard focus, drop-in `SettingsMenu`, and `AnchorOverlay` for world-anchored native views). See [gameui](gameui.md). Tracked gaps: reusable screen fragments ([#1343](https://github.com/nsgo-dev/game/issues/1343)), modal popup stack with focus restore ([#1347](https://github.com/nsgo-dev/game/issues/1347)), and declarative data bindings in screen files ([#1636](https://github.com/nsgo-dev/game/issues/1636)).

## Physics and materials

- **Chaos Physics → `physics`, `physics3d` and `vehicle3d`**: pure-Go deterministic solvers stepping on `game.Loop`'s fixed `dt` with zero cgo across all targets. In 3D, `physics3d.World` supports static and dynamic rigid bodies (`Box`, `Sphere`, `Capsule`, `ConvexHull`, `Cylinder`, `CompoundShape`), static `TriMeshCollider`, `HeightfieldCollider` and `VoxelCollider`, raycasts and `ShapeCast` sweeps (for example third-person camera spring arms), joints, ragdolls and a capsule `CharacterController`; [vehicle3d](vehicle3d.md) builds raycast cars with suspension and tire friction on top. In 2D, [physics](physics.md) and [2D Rigid Bodies](physics2d-rigid.md) provide overlaps, `MoveAndSlide`, triggers, raycasts and rigid bodies with joints. There is no Chaos Destruction or cloth simulation; continuous collision detection improvements are tracked in [#1712](https://github.com/nsgo-dev/game/issues/1712) and 3D joints/shapes in scene files in [#1379](https://github.com/nsgo-dev/game/issues/1379).
- **Materials & Lumen → `resource.Material` and `render3d`**: `render3d` ships metallic-roughness PBR materials (`Material`, `GPUMaterial`, `UploadGLTFMaterials`, extended clearcoat/transmission), toon and unlit materials, hot-reloaded WGSL `CustomMaterial`, `LightRig` and `ClusteredLightSystem`, directional/cascaded/spot/point shadow maps, `OcclusionBuffer`, `HDRTarget` + `ToneMapPass`, `PostStack3D` (bloom, SSAO, depth of field), and `LightmapRenderer` / PBR lightmaps. There is no real-time global illumination (Lumen), virtualized geometry (Nanite) or node-based Material Graph editor—materials are versioned YAML (`resource.Material`) or WGSL shaders. See [render3d](render3d.md). Tracked gaps: lightmap baking ([#1396](https://github.com/nsgo-dev/game/issues/1396)) and richer scene material and shader files ([#1384](https://github.com/nsgo-dev/game/issues/1384), [#1386](https://github.com/nsgo-dev/game/issues/1386)).

## Networking and saves

- **Replication & Dedicated Server → `pkg/online/netplay` and `pkg/online/gameserver`**: `netplay` runs over a unified `Transport` across WebTransport (QUIC datagrams + streams on native and browser), WebSocket, encrypted UDP (Noise NK + ChaCha20-Poly1305) and deterministic loopback. `ServerWorld` and `ClientWorld` provide server-authoritative field-level delta snapshot replication with client prediction and interpolation; `LockstepSession` and `RollbackSession` cover fighting, RTS and arcade co-op netcode; and `*.wire.yaml` schemas generate zero-allocation Go and TypeScript codecs ([netplay/wire](netplay-wire.md)). Dedicated servers run as plain headless Go binaries on `pkg/online/gameserver` (`Runner`) with no UI or cgo, alongside self-hosted `accounts`, `cloudsave` and `leaderboard/selfhost`. See [netplay](netplay.md) and [gameserver](gameserver.md). Tracked gaps: automatic `entities.World` replication ([#1444](https://github.com/nsgo-dev/game/issues/1444)), custom replicated layouts beyond 2D transforms ([#1442](https://github.com/nsgo-dev/game/issues/1442)), and interest management ([#1325](https://github.com/nsgo-dev/game/issues/1325)).
- **`USaveGame` → `save`**: `save.Store` (`NewStorageStore` across all targets including web, `NewFileStore` on desktop and mobile) writes versioned JSON envelopes with sequential schema migrations (`WithMigration`) and atomic backup recovery, plus `save.SettingsStore` for player settings and `pkg/online/cloudsave` / `liveops.CloudSyncer` for cloud sync. See [save](save.md) and [cloudsave](cloudsave.md). Tracked gaps: listing slot headers without loading and async save/load ([#1733](https://github.com/nsgo-dev/game/issues/1733)), and whole-game registered saveables ([#1759](https://github.com/nsgo-dev/game/issues/1759)).

## Packaging and shipping

Project settings and per-platform export presets live in `game.yaml`, kept in step with `nsgo.yaml`. Go cross-compiles to Windows, Linux, Android (ARM64) and WebAssembly from any host with no platform SDK installs; only iOS and signed macOS builds need a Mac with Xcode. Consoles (Nintendo Switch, PlayStation and Xbox) are out of scope because their SDKs are under NDA and require C toolchains ([Platform support](platforms.md)).

| Unreal Engine | Here |
| --- | --- |
| Play in Editor (PIE) | `go tool game dev` (or `go tool nsgo dev`, with `-target web`, `ios_simulator` or `android`) |
| Cook Content | `go tool game assets` (incremental atlases, KTX2 textures, audio, BMFonts, GLBs and manifest) |
| Map Check / Validate Data | `go tool game check` and `go tool nsgo check -target all` |
| Package Project (Export Presets) | `go tool game release` (reads `game.yaml` `export.presets`, builds with `-tags release`, writes `CREDITS.md`) or `GOFLAGS=-tags=release go tool nsgo build` |
| Store distribution (`.aab`, `.ipa`, `.pkg`, `.msix`) | `go tool game release -channel store` |

Named export presets with feature tags, build tags and asset filters are tracked in [#1428](https://github.com/nsgo-dev/game/issues/1428). See [Building and Shipping](shipping.md), [Command Line](cli.md) and [Getting Started](getting-started.md) to build your first project.
