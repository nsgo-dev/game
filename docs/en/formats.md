# File Formats

Every file a game made with these packages reads or writes, with the names its files go by, the version it declares, the Go functions that read and write it, the guide that describes its fields and an example, so a person or an AI can edit one without reading the code.

The files a game's author writes are YAML, apart from those under JSON files and Other tools' formats below. The tests check every example on this page: a YAML example is in its format's canonical form, the bytes its encoder writes, and loads and saves back unchanged, so none shows a key the format refuses or a layout it would rewrite; a JSON or Yarn example loads with the format's reader.

## What every YAML format does

- **A version.** The document's top-level `version` key, an integer, says which version of the format it is in; a mod manifest calls it `manifestVersion`. A file without it, with `version: 0` or with a version newer than the code reads is refused.
- **Strict keys.** A key the format does not define is refused, as are a second YAML document in the file and a key given twice, with an error that names the file, line and field and says how to fix it ([Errors](errors.md)).
- **One canonical form.** Saving writes the keys in a fixed order, sorts what has no order of its own and writes no timestamp or random ID, so the same value always gives the same bytes and a file in that form loads and saves back unchanged. The first save of a hand-written file drops its comments and rewrites its layout into that form; every save after that changes only what changed.
- **Schemas.** Scene files and import settings sidecars have published JSON Schemas, and a data table's schema is generated from its row type, so an editor completes and checks them as they are written ([Schemas for YAML files](schemas.md)).

## Project and assets

<!-- format: project -->
### Project file

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `game.yaml` at the project's root | `version: 1` | `project.Parse`, `project.Encode` | [Project File](project.md); schema `https://game.nsgo.dev/schemas/game.v1.json` |

```yaml
version: 1
name: Cave Run
id: dev.nsgo.game.examples.caverun
appVersion: 0.1.0
window:
  title: Cave Run
  width: 960
  height: 540
display:
  width: 320
  height: 180
  mode: fit
autoload:
  mainScene: title
  scenes:
    - name: title
      path: scenes/title.scene.yaml
```

<!-- format: project#brief -->
### Game brief

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `brief.yaml` at the project's root | `version: 1` | `project.ParseBrief`, `project.EncodeBrief` | [Editor](editor.md#game-brief-and-project-rules-1026); schema `https://game.nsgo.dev/schemas/brief.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/brief.v1.json
version: 1
pitch: 'A short, tense dungeon platformer: a knight with one sword and three hearts dives through five floors, each ending in a door that only opens once its lever is found.'
genre: [2D platformer, action]
tags: [dungeon, pixel-art]
platforms: [desktop, 'phones, touch', web]
artDirection: 16 px pixel art, 4-colour shading, cold stone with warm torch light. No outlines on tiles; 1 px dark outline on characters.
feel:
  summary: Snappy, forgiving jumps (coyote time). A first-time player clears floor 1 in under a minute and dies 2–4 times on floor 3.
  targets:
    - metric: median_time
      scene: floor_01
      summary: first-time player clears floor 1 in under a minute
      unit: s
      max: 60
    - metric: deaths
      scene: floor_03
      summary: 2–4 deaths on floor 3
      min: 2
      max: 4
```

<!-- format: editor#settings -->
### Editor settings

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Per-user `editor.yaml` under the user config directory (`editor.DefaultSettingsPath`), outside any project | `version: 1` | `editor.ParseEditorSettings`, `editor.EncodeEditorSettings` | [Editor](editor.md#language-and-appearance-settings-921); schema `https://game.nsgo.dev/schemas/editor-settings.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/editor-settings.v1.json
version: 1
language: system
appearance: system
autoSave: true
viewportOverlays:
  - collision_shapes
  - connections
  - navigation
  - triggers
overlayLegend:
  - added
  - agent
  - changed
  - problem
  - removed
  - selection
shortcuts:
  view.frame: g
seenTips:
  - first_review.ask_selection
  - first_review.hold_b
  - first_review.keep_revert
agent:
  builtInProvider: Anthropic API
  tools:
    game_delete_file: false
  mcpServers:
    - name: nsgo-game
      title: Running game · nsgo dev
      description: inspect and drive the live game
      command: go
      args:
        - tool
        - nsgo
        - mcp
      enabled: true
  weeklyRequestLimit: 300
  weeklySpendLimitUSD: 20
  privacy:
    sceneAndData: true
    viewportImages: true
    goSource: false
    artAndAudio: false
  approvedAutomations:
    - project: /home/you/games/dungeon-run
      sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
```

<!-- format: editor#permissions -->
### Editor MCP permissions

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `permissions.yaml` beside `game.yaml` (tracked in Git and shared across the team) | `version: 1` | `editor.ParsePermissions`, `editor.EncodePermissions` | [Editor](editor.md#permissions-and-mcp-enforcement-1010); schema `https://game.nsgo.dev/schemas/permissions.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/permissions.v1.json
version: 1
profile: balanced
rules:
  - action: edit
    patterns:
      - scenes/**
      - prefabs/**
      - data/**
    decision: allow
  - action: run
    patterns:
      - game check
      - playtests
    decision: allow
  - action: edit
    patterns:
      - game.yaml
      - brief.yaml
    decision: ask
  - action: code
    patterns:
      - '**/*.go'
    decision: ask
  - action: create
    patterns:
      - '**'
    decision: ask
  - action: delete
    patterns:
      - '**'
    decision: deny
  - action: run
    patterns:
      - shell
    decision: deny
```

<!-- format: editor#thread -->
### Agent background task thread

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `.nsgo/threads/<id>.thread.yaml` under a project root (default `default.thread.yaml`) | `version: 1` | `editor.ParseAgentThread`, `editor.EncodeAgentThread` | [Editor](editor.md#background-task-list-with-queued-and-parallel-agent-requests-1022) |

```yaml
version: 1
id: default
scene: scenes/level_01.scene.yaml
tasks:
  - id: task-1
    number: 1
    title: slime_02 jumps the gap at x 360
    source: you
    startMode: queue
    state: running
    scene: scenes/level_01.scene.yaml
    stepSummary: placing the jump arc
    editingObjects:
      - slime_02
  - id: task-2
    number: 2
    title: Coin pickup sound a little higher
    source: you
    startMode: queue
    state: queued
    startsAfterTaskId: task-1
```

<!-- format: editor#automations -->
### Editor automations

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `automations.yaml` at the project's root beside `game.yaml` | `version: 1` | `editor.ParseAutomations`, `editor.EncodeAutomations` | [Editor](editor.md#agent-automations-1029); schema `https://game.nsgo.dev/schemas/automations.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/automations.v1.json
version: 1
automations:
  - id: level-check
    name: A level file changes
    enabled: true
    trigger:
      event: file_changed
      glob: scenes/level_*.scene.yaml
    instruction: runs game check and 5 playtests, reports only problems
  - id: review-brief
    name: You keep a review
    enabled: true
    trigger:
      event: review_kept
    instruction: updates the brief's open questions if the change answers one
  - id: new-prefab-example
    name: A new prefab is added
    enabled: true
    trigger:
      event: file_added
      kind: prefab
    instruction: writes its example scene and a test
  - id: before-export-mobile
    name: Before export
    enabled: false
    trigger:
      event: before_export
    instruction: checks frame time and memory on a low-end phone profile
  - id: nightly-balance
    name: Every night
    enabled: false
    trigger:
      event: daily
      time: "02:00"
    instruction: plays all floors 50 times and leaves a balance report
```
<!-- format: importsettings -->
### Import settings sidecar

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<asset>.import.yaml` beside an image, sound, font or model, such as `hero.png.import.yaml` | `version: 1` | `importsettings.Parse`, `importsettings.Encode` | [Command Line](cli.md#assets); schema `https://game.nsgo.dev/schemas/import.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/import.v1.json
version: 1
image:
  filter: nearest
  slice:
    width: 16
    height: 16
    count: 6
  pivot:
    default: [0.5, 1]
    cells:
      - cell: 2
        pivot: [0.25, 0.75]
credit:
  author: Kenney
  license: CC0-1.0
  source: https://kenney.nl/assets/kenney-fonts
```

<!-- format: importsettings#source -->
### Generated asset source sidecar

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<asset>.source.yaml` beside a generated image or sound, such as `slime_frost.png.source.yaml` | `version: 1` | `importsettings.ParseAssetSource`, `importsettings.EncodeAssetSource` | [Editor](editor.md); schema `https://game.nsgo.dev/schemas/source.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/source.v1.json
version: 1
generator: pixel-gen
model: pixflux-1
prompt: ice slime, 16 px pixel art, 4-colour shading, 1 px dark outline, facing right
seed: 81515
references:
  - art/characters/slime.png
  - brief.yaml#art
license: pixellab-commercial-1.2
commercialUse: true
approvedBy: kim
```

<!-- format: tooling/refs -->
### Project index

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `game.index.yaml` beside `game.yaml`, kept by `game index`, `game mv` and the editor | `version: 1` | `refs.ParseIndex`, `refs.EncodeIndex` | [Stable references](project.md#stable-references) |

```yaml
# The stable IDs of this project's files, kept by game index, game mv and the editor. Keep it in version control.
version: 1
files:
  - path: art/hero.png
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  - path: scenes/room.scene.yaml
    uid: scene00000001
```

<!-- format: tooling/library -->
### Library pack manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `pack.yaml` at the root of an asset or plugin pack | `version: 1` | `library.ParseManifest`, `library.EncodeManifest` | [Asset and Plugin Library](library.md) |

```yaml
version: 1
name: coins
kind: asset
packVersion: 1.0.0
description: A spinning coin.
license: CC0-1.0
files:
  - coin.png
autoload:
  resources:
    - name: coin
      path: coin.png
```

<!-- format: tooling/library#index -->
### Library index

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `index.yaml` at the root of a pack library | `version: 1` | `library.ParseIndex`, `library.EncodeIndex` | [Asset and Plugin Library](library.md) |

```yaml
version: 1
packs:
  - name: dungeon-tiles
    kind: asset
    packVersion: 1.0.0
    description: Stone tiles.
    tags: [2d, tiles]
    license: CC0-1.0
    source: b
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
```

<!-- format: l10n -->
### Localized asset manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `assets.yaml` | `version: 1` | `l10n.ParseAssetManifest`, `l10n.EncodeAssetManifest` | [l10n](l10n.md) |

```yaml
version: 1
base_locale: en
assets:
  ui/title_banner.png:
    en: ui/title_banner.png
    zh-Hans: ui/title_banner.zh-Hans.png
  voice/intro.wav:
    en: locales/en/voice/intro.wav
    ja: locales/ja/voice/intro.wav
groups:
  voice:
    - voice/intro.wav
```

<!-- format: l10n#strings -->
### Localization string table (YAML)

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| One `.yaml` file per locale, such as `locales/en.yaml` | `version: 1` | `l10n.ParseYAML`, `l10n.EncodeYAML`, `l10n.Catalog.LoadYAML` | [l10n](l10n.md) |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/l10n.v1.json
version: 1
locale: en
strings:
  hud.score: 'Score: {score}'
  menu.play: Play
  menu.quit: Quit
plurals:
  coins:
    zero: No coins
    one: '{count} coin'
    other: '{count} coins'
```

<!-- format: modding -->
### Mod manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `mod.yaml` at the root of a mod's directory | `manifestVersion: 1` or `manifestVersion: 2` | `modding.ParseManifest`, `modding.EncodeManifest` | [modding](modding.md); schemas `https://game.nsgo.dev/schemas/mod.v1.json`, `https://game.nsgo.dev/schemas/mod.v2.json` |

```yaml
manifestVersion: 1
id: com.example.boss-rush
name: Boss Rush
version: 1.2.0
author: Example Studio
description: Spawns a slime every five seconds and shows a HUD badge while the rush runs.
entry: mod.tsx
gameVersion: '>=0.2.0'
capabilities: [readState, spawnEntity, emitEvent, ui]
```

Version 2 adds `optionalDependencies`, `conflicts`, `platforms`, and `contributes` (and allows content-only mods without `entry`):

```yaml
manifestVersion: 2
id: com.example.boss-rush
name: Boss Rush
version: 1.3.0
entry: mod.tsx
dependencies:
  com.example.core: ^1.0.0
optionalDependencies:
  com.example.music: '>=0.2.0'
conflicts:
  com.example.old-boss: '*'
platforms: [linux, macos, windows]
contributes:
  assets: assets
  scenes: scenes
  l10n: locales
  datatables: tables
```

<!-- format: modding#workshop -->
### Steam Workshop metadata

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `workshop.yaml` beside `mod.yaml` in a mod published to Steam Workshop | `version: 1` | `modding.ParseWorkshopMetadata`, `modding.EncodeWorkshopMetadata` | [modding](modding.md) |

```yaml
version: 1
appId: 480
publishedFileId: 1001
title: Boss Rush
description: Spawns a slime every five seconds.
tags: [gameplay, survival]
visibility: public
preview: preview.png
```

<!-- format: gameplugin/manifest -->
### Plugin manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `plugin.yaml` at the root of a Go plugin module | `manifestVersion: 1` | `manifest.Parse`, `manifest.ParseFS`, `manifest.Encode` | [Plugins](plugins.md) |

```yaml
manifestVersion: 1
name: healthkit
module: example.com/healthkit
description: Health component, ward panel and admit tool
license: MIT
authors: [Jane Doe]
repository: https://github.com/example/healthkit
engine: '>=0.4.0 <0.5.0'
api: 1
platforms: [all]
entries:
  runtime: example.com/healthkit/health
  editor: example.com/healthkit/healtheditor
  cli:
    - name: healthkit
      package: example.com/healthkit/cmd/game-healthkit
requires:
  example.com/navkit: ^0.3.0
conflicts: [example.com/oldhealth]
contributes:
  components: [health]
  editor:
    panels: [ward]
    tools: [admit]
    commands: [heal-all]
  agentTools: [plugin.healthkit.heal_all]
```

<!-- format: tooling/hotreload -->
### 3D shader material

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.material.yaml`, hot-reloaded in `game dev` | `version: 1` | `hotreload.ParseMaterial`, `hotreload.EncodeMaterial` | [render3d](render3d.md) |

```yaml
version: 1
name: Crate
shader: shaders/lit.wgsl
blend: opaque
cull: back
frontFace: ccw
depthCompare: less
scalars:
  roughness: 0.4
colors:
  tint: '#d9c6a5'
textures:
  albedo: textures/crate.png
```

<!-- format: voxel -->
### Voxel world

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.voxel.yaml` | `version: 1` | `voxel.Parse`, `voxel.Encode` | [voxel](voxel.md); schema `https://game.nsgo.dev/schemas/voxel.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/voxel.v1.json
version: 1
name: hill
chunkSize: 4
voxelSize: 1
palette:
  - id: 1
    name: stone
chunks:
  - x: 0
    "y": 0
    z: 0
    runs:
      - 1*4
      - 0*60
```

<!-- format: world3d#gridmap -->
### 3D grid map

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.gridmap.yaml` | `version: 1` | `world3d.ParseGridMap`, `world3d.EncodeGridMap`, `world3d.LoadGridMapFS` | [world3d](world3d.md); schema `https://game.nsgo.dev/schemas/gridmap.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/gridmap.v1.json
version: 1
name: Stone Keep
cellSize:
  - 2
  - 2
  - 2
meshLibrary: models/dungeon_tiles.gltf
boxes:
  - min:
      - 0
      - 0
      - 0
    max:
      - 4
      - 0
      - 4
    tile: stone_floor
cells:
  - at:
      - 0
      - 1
      - 0
    tile: stone_pillar
  - at:
      - 4
      - 1
      - 0
    tile: stone_pillar
    orientation: 1
```

<!-- format: world3d#terrain -->
### Terrain definition

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.terrain.yaml` | `version: 1` | `world3d.ParseTerrain`, `world3d.EncodeTerrain`, `world3d.LoadTerrainFS` | [world3d](world3d.md); schema `https://game.nsgo.dev/schemas/terrain.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/terrain.v1.json
version: 1
name: Alpine Valley
heightmap: terrain/alpine_height.png
cellSize: 2
heightScale: 40
splatMaps:
  - terrain/alpine_splat0.png
heightBlendContrast: 0.65
triplanar:
  enabled: true
  slopeThreshold: 0.7
  slopeBlend: 0.15
  sharpness: 4
  scale: 0.25
layers:
  - name: grass
    albedo: textures/grass_albedo.png
    normal: textures/grass_normal.png
    height: textures/grass_height.png
    roughness: 0.85
    uvScale:
      - 16
      - 16
  - name: dirt
    albedo: textures/dirt_albedo.png
    normal: textures/dirt_normal.png
    height: textures/dirt_height.png
    roughness: 0.9
    uvScale:
      - 12
      - 12
    heightBias: -0.05
  - name: rock
    albedo: textures/rock_albedo.png
    normal: textures/rock_normal.png
    height: textures/rock_height.png
    roughness: 0.8
    uvScale:
      - 8
      - 8
    heightBias: 0.1
  - name: snow
    albedo: textures/snow_albedo.png
    normal: textures/snow_normal.png
    roughness: 0.5
    uvScale:
      - 10
      - 10
    heightBias: 0.05
```

<!-- format: world3d#foliage -->
### 3D foliage layers

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.foliage.yaml` | `version: 1` | `world3d.ParseFoliage`, `world3d.EncodeFoliage`, `world3d.LoadFoliageFS` | [world3d](world3d.md); schema `https://game.nsgo.dev/schemas/foliage.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/foliage.v1.json
version: 1
name: Alpine Meadow
terrain: terrain/alpine.terrain.yaml
layers:
  - name: grass
    mesh: models/grass_clump.glb
    material: materials/grass.material.yaml
    density: 1.5
    minScale: 0.8
    maxScale: 1.3
    alignToNormal: 0.6
    maxSlopeDeg: 35
    baseMaxDistance: 80
    seed: 42
  - name: pine_tree
    mesh: models/pine_tree.glb
    densityMap: terrain/forest_density.png
    spacing: 4
    minScale: 0.85
    maxScale: 1.25
    maxSlopeDeg: 28
    minHeight: 2
    maxHeight: 32
    baseMaxDistance: 160
    boundingRadius: 3.5
    seed: 108
```

<!-- format: tooling/storecapture -->
### Store screenshot plan

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `store/capture.yaml`, read by `go tool game capture` | `version: 1` | `storecapture.Decode`, `storecapture.LoadFile`, `Plan.Encode` | [Store screenshot plan](shipping.md#store-screenshot-plan) |

```yaml
version: 1
shots:
  - name: title
    scene: scenes/title.scene.yaml
    frame: 30
    output: title
    sizes:
      - preset: itch_cover
      - preset: minigame_share
  - name: boss
    scene: scenes/boss.scene.yaml
    recording: recordings/boss.replay.yaml
    time: 2.5
    output: play/boss
    sizes:
      - preset: google_play
      - width: 2796
        height: 1290
```

## Scenes, resources and animation

<!-- format: sceneformat -->
### Scene

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.scene.yaml`, such as `scenes/cave_room.scene.yaml` | `version: 1`, `version: 2` (instancing, inheritance, glTF models, particle emitters, layered tilemaps, LDtk levels), `version: 3` (parallax and repeating layers, tilemap offsets), or `version: 4` (3D lights, scene environment, 3D cameras, reflection probes, light probe grids); built-in physics components `version: 1`; built-in 2D lighting components `version: 1`; built-in 2D navigation region component `version: 1` | `sceneformat.Parse`, `sceneformat.Encode` | [Editor & Scene Format](editor.md); schemas `https://game.nsgo.dev/schemas/scene.v1.json`, `scene.v2.json`, `scene.v3.json` and `scene.v4.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v1.json
version: 1
name: Cave Room
virtualWidth: 320
virtualHeight: 180
layers:
  - name: actors
    space: world
    order: 10
    objects:
      - id: hero
        x: 48
        "y": 40
        sprite:
          atlas: actors
          frame: hero
```

<!-- format: gameui -->
### Game UI screen

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.screen.yaml`, such as `screens/hud.screen.yaml` and `screens/pause.screen.yaml` | `version: 1` | `gameui.ParseScreen`, `gameui.EncodeScreen`, `gameui.LoadScreenFS`, `gameui.BuildScreen` | [gameui](gameui.md); schema `https://game.nsgo.dev/schemas/screen.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/screen.v1.json
version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: status
    type: vbox
    anchor: top_left
    offset:
      x: 12
      "y": 12
    spacing: 4
    children:
      - id: hearts
        type: sprite_row
        width: 52
        height: 14
        sprite: art/ui/heart.png
        spriteCount: 3
      - id: coins
        type: label
        sizeMode: fit_text
        height: 16
        textKey: hud.coins
  - id: touch
    type: container
    deviceClasses:
      - phone
      - tablet
    children:
      - id: jump
        type: touch_button
        anchor: bottom_right
        offset:
          x: 24
          "y": 28
        width: 52
        height: 52
        textKey: hud.jump
        touchHitSize: 64
```

<!-- format: input#touch -->
### Touch control layout

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.touch.yaml`, such as Cave Run's `controls.touch.yaml`, and a player's edited layout in a key of `core.Storage` | `version: 1` | `input.ParseTouchLayout`, `input.EncodeTouchLayout`, `input.TouchControls.Save`, `input.TouchControls.Load` | [input](input.md#on-screen-touch-controls); schema `https://game.nsgo.dev/schemas/touchlayout.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/touchlayout.v1.json
version: 1
reference: {width: 844, height: 390}
maxScale: 1.5
idleAfter: 4s
idleAlpha: 0.45
controls:
  - id: move
    kind: dpad
    anchor: bottomLeft
    offset: {x: 28, "y": 24}
    size: {width: 150, height: 150}
    label: Move
    ways: 8
    left: left
    right: right
  - id: jump
    kind: button
    anchor: bottomRight
    offset: {x: 36, "y": 32}
    size: {width: 110, height: 110}
    label: Jump
    action: jump
    text: JUMP
```

<!-- format: sceneformat#conflict -->
### Scene merge conflict report

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Written by `game merge` (overwriting the unmerged `*.scene.yaml` or `-conflict-out`) when a three-way scene merge has conflicts | `version: 1` | `sceneformat.ParseConflict`, `sceneformat.EncodeConflict` | [Editor & Scene Format](editor.md#merging-scene-files-in-git-1058), [Command Line](cli.md#merge) |

```yaml
version: 1
path: scenes/level_01.scene.yaml
merged:
  - scope: object
    target: slime_03
    side: ours
    action: added
conflicts:
  - kind: field
    scope: object
    layer: actors
    object: door
    field: x
    base: 584
    ours: 560
    theirs: 600
  - kind: delete-modify
    scope: object
    layer: actors
    object: slime_02
    message: modified on ours, deleted on theirs
    base:
      id: slime_02
      x: 240
      "y": 280
    ours:
      components:
        patrol:
          speed: 30
      id: slime_02
      x: 240
      "y": 280
    theirs: null
```
<!-- format: pathfind3d/navbake3d -->
### 3D navigation meshes

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.navmesh.yaml`, such as `levels/level1.navmesh.yaml`, which a scene's `navMesh` names | `version: 1` | `navbake3d.Parse`, `navbake3d.Encode`; baked by `game navmesh` | [navbake3d](navbake3d.md) |

```yaml
version: 1
settings:
  cellSize: 0.3
  cellHeight: 0.2
  minRegionArea: 1
  maxEdgeLength: 12
  maxSimplificationError: 0.4
  maxVertsPerPoly: 6
  detailSampleDistance: 1.8
  detailSampleMaxError: 0.2
  layerMask: 4294967295
meshes:
  - agent:
      name: humanoid
      radius: 0.6
      height: 2
      maxSlope: 45
      maxClimb: 0.9
    vertices:
      - [0, 0, 0]
      - [0, 0, 4]
      - [4, 1, 4]
      - [4, 1, 0]
      - [8, 1, 4]
      - [8, 1, 0]
    polygons:
      - vertices: [0, 1, 2, 3]
        detail:
          - [2, 0.75, 2]
        triangles:
          - [0, 1, 4]
          - [1, 2, 4]
          - [2, 3, 4]
          - [3, 0, 4]
      - vertices: [3, 2, 4, 5]
        triangles:
          - [0, 1, 2]
          - [0, 2, 3]
```

<!-- format: render3d#lightprobes -->
### Baked light probe grids

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.probes.yaml`, such as `probes/room.probes.yaml`, which a scene object's `lightProbeGrid.data` names | `version: 1` | `render3d.ParseLightProbeGrid`, `render3d.EncodeLightProbeGrid`, `render3d.LoadLightProbeGridFS` | [render3d](render3d.md); schema `https://game.nsgo.dev/schemas/lightprobes.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/lightprobes.v1.json
version: 1
boundsMin: [-4, 0, -4]
boundsMax: [4, 4, 4]
counts: [2, 1, 1]
probes:
  - sh:
      - [0.5, 0.4, 0.3]
      - [0.1, 0.05, 0]
      - [0, 0, 0]
      - [0.15, 0.02, 0.02]
      - [0, 0, 0]
      - [0, 0, 0]
      - [0, 0, 0]
      - [0, 0, 0]
      - [0, 0, 0]
  - sh:
      - [0.3, 0.4, 0.5]
      - [0.05, 0.05, 0.1]
      - [0, 0, 0]
      - [-0.02, 0.02, 0.15]
      - [0, 0, 0]
      - [0, 0, 0]
      - [0, 0, 0]
      - [0, 0, 0]
      - [0, 0, 0]
```

<!-- format: tilemap#tileset -->
### Tileset

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.tileset.yaml`, such as `tilesets/cave.tileset.yaml`, named by a scene's inline or layered tilemap (`tilesetFile`) | `version: 1` | `tilemap.ParseTileset`, `tilemap.EncodeTileset`, `tilemap.LoadTilesetFS`; `Tilemap.AddTilesetDoc` builds it | [tilemap](tilemap.md#yaml-tilesets); schema `https://game.nsgo.dev/schemas/tileset.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/tileset.v1.json
version: 1
name: cave
tileWidth: 16
tileHeight: 16
image: tiles/cave.png
columns: 4
tileCount: 8
terrains:
  - name: rock
    mode: cardinal
tiles:
  - id: 0
    solid: true
    terrain: rock
  - id: 1
    solid: true
    properties:
      friction: 0.5
    terrain: rock
    neighbors: [east]
  - id: 2
    shapes:
      - kind: polygon
        points: [[0, 16], [16, 0], [16, 16]]
  - id: 4
    animation:
      - tile: 4
        duration: 200ms
      - tile: 5
        duration: 200ms
```

<!-- format: resource -->
### Shared resource

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<name>.<type>.yaml`, such as `stone.material.yaml`; `type` is `material`, `curve`, `gradient`, `animation`, `theme`, `busLayout` or `physicsMaterial` | `version: 1` | `resource.Parse`, `resource.Encode` | [resource](resources.md) |

```yaml
type: material
version: 1
uid: mat0000000004
name: Stone
baseColor: '#8a8f99'
texture: textures/stone.png
opacity: 0.75
blend: multiply
roughness: 0.9
```

```yaml
type: curve
version: 1
name: Jump height
keys:
  - time: 0
    value: 0
    interp: smooth
  - time: 0.5
    value: 1
  - time: 1
    value: 0
    interp: step
```

```yaml
type: animation
version: 1
name: Hero walk
length: 0.4
loop: true
tracks:
  - property: sprite.frame
    frames:
      - time: 0
        frame: hero_walk_0
      - time: 0.1
        frame: hero_walk_1
      - time: 0.2
        frame: hero_walk_2
      - time: 0.3
        frame: hero_walk_3
events:
  - time: 0.1
    name: footstep
  - time: 0.3
    name: footstep
```

<!-- format: animclip -->
### Sprite state machine

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.states.yaml`, such as `anim/knight.states.yaml` | `version: 1` | `animclip.ParseStateMachine`, `animclip.EncodeStateMachine` | [animclip](animclip.md#sprite-state-machines); schema `https://game.nsgo.dev/schemas/states.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/states.v1.json
version: 1
name: Knight
entry: idle
parameters:
  - name: move.x
    type: float
  - name: velocity.y
    type: float
  - name: on_floor
    type: bool
    default: true
  - name: jump
    type: trigger
  - name: attack
    type: trigger
  - name: hurt
    type: trigger
states:
  - name: idle
    clip: anim/knight_idle.animation.yaml
    x: 40
    "y": 230
  - name: run
    clip: anim/knight_run.animation.yaml
    x: 330
    "y": 230
  - name: jump
    clip: anim/knight_jump.animation.yaml
    x: 190
    "y": 70
  - name: fall
    clip: anim/knight_fall.animation.yaml
    x: 430
    "y": 70
  - name: attack
    clip: anim/knight_attack.animation.yaml
    x: 190
    "y": 400
  - name: hurt
    clip: anim/knight_hurt.animation.yaml
    x: 430
    "y": 400
transitions:
  - from: idle
    to: run
    label: moving
    when: '|move.x| > 0.1'
  - from: run
    to: idle
    label: stopped
    when: '|move.x| <= 0.1'
  - from: idle
    to: jump
    label: jump pressed, on floor
    when: jump && on_floor
  - from: run
    to: jump
    label: jump pressed, on floor
    when: jump && on_floor
  - from: jump
    to: fall
    label: falling
    when: velocity.y > 0
  - from: fall
    to: run
    label: lands
    when: on_floor
  - from: idle
    to: attack
    label: attack pressed
    when: attack
  - from: run
    to: attack
    label: attack pressed
    when: attack
  - from: attack
    to: idle
    label: clip ends
    onEnd: true
  - from: '*'
    to: hurt
    label: took damage
    when: hurt
  - from: hurt
    to: idle
    label: clip ends
    onEnd: true
```

<!-- format: particles -->
### Particle emitter

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.particles.yaml` / `*.emitter.yaml` (or any `.yaml` file the game loads, such as `sparks.yaml`) | `version: 1` | `particles.UnmarshalConfigYAML`, `particles.MarshalConfigYAML`; the same fields as JSON with `UnmarshalConfigJSON` and `MarshalConfigJSON` | [particles](particles.md); schema `https://game.nsgo.dev/schemas/particles.v1.json` |

```yaml
version: 1
space: local
shape:
  kind: circle
  radius: 6
rate: 45
maxParticles: 128
lifetimeMin: 300ms
lifetimeMax: 700ms
speedMin: 20
speedMax: 65
alphaCurve:
  - t: 0
    value: 1
  - t: 1
    value: 0
frameNames:
  - spark_0
```

<!-- format: skeletal -->
### Skeletal rig

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.skel.yaml` | `version: 1` | `skeletal.Load`, `skeletal.Encode` | [skeletal](skeletal.md) |

```yaml
version: 1
name: waver
bones:
  - {name: root}
  - {name: arm, parent: root, y: -14, length: 12}
slots:
  - {name: arm, bone: arm}
skins:
  default:
    arm: {frame: arm, originX: 3, originY: 2}
animations:
  wave:
    duration: 0.8
    loop: true
    bones:
      arm:
        rotate:
          - {time: 0, angle: 0.4}
          - {time: 0.4, angle: -0.4}
          - {time: 0.8, angle: 0.4}
```

<!-- format: cutscene -->
### Cutscene

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.cutscene.yaml` (or any `.yaml` file the game loads), such as `cutscenes/floor1_intro.cutscene.yaml` | `version: 1` (video), `version: 2` (video or timeline) | `cutscene.Parse`, `cutscene.Encode` | [cutscene](cutscene.md) |

```yaml
version: 1
source: asset:cutscenes/intro.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
```

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
skippable: true
skipAfter: 1s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: cam1
        start: 0s
        duration: 1.6s
        easing: hold
        action: hold
        target: door
      - id: cam2
        start: 1.6s
        duration: 2.4s
        easing: easeInOut
        action: pan
        target: knight
      - id: cam3
        start: 4s
        duration: 8s
        action: follow
        target: knight
  - id: knight
    name: Knight
    kind: actor
    target: knight
    clips:
      - id: kn1
        start: 1s
        duration: 2.4s
        easing: easeOut
        action: walk
        fromX: 60
        toX: 220
        state: run
      - id: kn2
        start: 3.4s
        duration: 4.6s
        action: state
        state: idle
  - id: dialogue
    name: Dialogue
    kind: dialogue
    clips:
      - id: dl1
        start: 3.6s
        duration: 2.4s
        line: intro.knight.01
  - id: sfx
    name: Sound
    kind: sound
    group: sfx
    clips:
      - id: sx1
        start: 200ms
        duration: 600ms
        source: audio/door_slam.wav
  - id: music
    name: Music
    kind: music
    group: music
    clips:
      - id: mu1
        start: 2s
        duration: 10s
        source: audio/cellar_theme.ogg
        fadeIn: 2s
```

<!-- format: rooms -->
### Room map

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `castle.rooms.yaml` | `version: 1` | `rooms.Parse`, `rooms.Encode` | [rooms](rooms.md) |

```yaml
version: 1
transition:
  kind: slide
  duration: 300ms
rooms:
  - id: entrance
    x: 0
    "y": 0
    width: 320
    height: 180
  - id: hall
    x: 320
    "y": 0
    width: 640
    height: 180
    transition:
      kind: fade
      color: '#102030'
```

<!-- format: worldmap -->
### World map

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `castle.yaml` | `version: 1` | `worldmap.Parse`, `worldmap.Encode` | [worldmap](worldmap.md) |

```yaml
version: 1
rooms:
  - id: entrance
    name: Castle Gate
    x: 0
    "y": 0
    width: 320
    height: 180
    links: [hall]
  - id: hall
    name: Great Hall
    x: 320
    "y": 0
    width: 320
    height: 180
markers:
  - id: save-gate
    kind: save
    room: entrance
    x: 40
    "y": 90
    label: Gate shrine
```

<!-- format: cards -->
### Card board

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `board.yaml` | `version: 1` | `cards.ParseBoardDoc`, `cards.EncodeBoardDoc` | [cards](cards.md) |

```yaml
version: 1
card_size:
  w: 63
  h: 88
slots:
  - id: hand
    rect:
      x: 40
      "y": 400
      w: 560
      h: 120
    layout:
      fan:
        arc: 24
        sag: 14
```

## Game data

<!-- format: datatable -->
### Data table

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `.yaml` or `.yml`, such as `weapons.yaml`; `.csv` and `.json` exports load too, without a version | `version: 1` | `datatable.LoadYAML`, `datatable.MarshalYAML`; `datatable.LoadFile` picks the reader by extension | [datatable](datatable.md); schema generated per row type with `datatable.Schema` |

```yaml
version: 1
rows:
  - id: sword
    damage: 12
    cooldown: 400ms
    tags: [melee]
  - id: bow
    damage: 8
    cooldown: 1s
    tags: [ranged, two-handed]
```

<!-- format: stats -->
### Stats

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `hero.yaml` | `version: 1` | `stats.Parse`, `stats.Encode` | [stats](stats.md) |

```yaml
version: 1
stats:
  - name: health
    base: 100
    min: 0
  - name: attack
    base: 12
effects:
  - name: berserk
    steps: 300
    modifiers:
      - stat: attack
        op: mul
        value: 1.5
```

<!-- format: ability -->
### Ability set

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `hero.abilities.yaml` | `version: 1` | `ability.Parse`, `ability.Encode` | [ability](ability.md) |

```yaml
version: 1
abilities:
  - id: fireball
    name: ability.fireball
    action: cast
    costs:
      - resource: mana
        amount: 20
    cooldown: 2s
    castTime: 500ms
    conditions: [grounded, '!silenced']
  - id: dash
    action: dash
    cooldown: 250ms
    charges: 3
    recharge: 4s
```

<!-- format: inventory -->
### Item catalog

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `items.yaml` | `version: 1` | `inventory.Parse`, `inventory.Catalog.Encode` | [inventory](inventory.md) |

```yaml
version: 1
items:
  - id: potion
    name: items.potion.name
    icon: asset:icons/potion.png
    maxStack: 20
    tags: [consumable, healing]
    stats:
      heal: 25
  - id: iron-helm
    name: items.iron-helm.name
    equip: head
    stats:
      armor: 3
```

<!-- format: quest -->
### Quest book

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `village.yaml` | `version: 1` | `quest.Parse`, `quest.Encode` | [quest](quest.md) |

```yaml
version: 1
quests:
  - id: welcome
    title: quest.welcome.title
    autoStart: true
    objectives:
      - id: meet-elder
        kind: talk
        target: elder
  - id: herbs
    title: quest.herbs.title
    requires: [welcome]
    objectives:
      - id: moonleaf
        kind: collect
        target: moonleaf
        count: 4
    rewards:
      - kind: xp
        id: xp
        amount: 20
```

<!-- format: loot -->
### Loot tables

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `chest.yaml` | `version: 1` | `loot.Parse`, `loot.Encode` | [loot](loot.md) |

```yaml
version: 1
tables:
  - name: chest
    rolls:
      min: 1
      max: 3
    entries:
      - item: sword
        weight: 2.5
      - nothing: true
        weight: 6.5
```

<!-- format: rpgdata -->
### RPG database

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `rpg.yaml` | `version: 1` | `rpgdata.Parse`, `rpgdata.Encode` | [rpgdata](rpgdata.md) |

```yaml
version: 1
classes:
  - id: hero
    name: Hero
    maxLevel: 10
    levelXP: 100
    stats:
      attack:
        - 5
        - 7
        - 9
      hp:
        - 20
        - 26
        - 32
    skills:
      - id: slash
        level: 1
      - id: cleave
        level: 3
actors:
  - id: ren
    name: Ren
    class: hero
    level: 1
skills:
  - id: slash
    name: Slash
    scope: one_enemy
    formula: a.attack * 2 - b.defense
  - id: cleave
    name: Cleave
    scope: all_enemies
    formula: a.attack + 4
    cost: 4
enemies:
  - id: slime
    name: Slime
    hp: 12
    experience: 3
    gold: 2
troops:
  - id: meadow
    members:
      - enemy: slime
        x: 0
        "y": 0
      - enemy: slime
        x: 48
        "y": 0
```

<!-- format: online/achievements -->
### Achievements

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game embeds, such as `achievements.yaml` | `version: 1` | `achievements.Parse`, `achievements.Encode` | [achievements](achievements.md) |

```yaml
version: 1
achievements:
  - id: gem-collector
    title: Gem Collector
    description: Clear 500 gems.
    steps: 500
    platforms:
      gameCenter: dev.nsgo.test.gem_collector
      playGames: CgkIabc123EAIQAg
```

<!-- format: online/leaderboard -->
### Leaderboards

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game embeds, such as `leaderboards.yaml` | `version: 1` | `leaderboard.Parse`, `leaderboard.Catalog.Encode` | [leaderboard](leaderboard.md) |

```yaml
version: 1
leaderboards:
  - name: high-score
    gameCenter: dev.nsgo.game.example.high_score
    playGames: CgkI6v7z8vQREAIQAQ
    miniGame:
      key: best
      order: desc
```

<!-- format: online/liveops#reminders -->
### Local reminders

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game embeds, such as `reminders.yaml` | `version: 1` | `liveops.ParseReminders`, `liveops.LoadRemindersFS`, `liveops.EncodeReminders` | [liveops](liveops.md#local-reminders); schema `https://game.nsgo.dev/schemas/reminders.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/reminders.v1.json
version: 1
quietHours:
  start: "22:00"
  end: "08:00"
reminders:
  - id: energy_full
    title: reminder.energy.title
    body: reminder.energy.body
    delay: 2h0m0s
    onOpen: keep
  - id: daily_reward
    title: reminder.daily.title
    body: reminder.daily.body
    at: "09:00"
    repeat: daily
    onOpen: cancel
  - id: weekend_event
    title: reminder.weekend.title
    body: reminder.weekend.body
    at: "18:00"
    weekday: friday
    repeat: weekly
    onOpen: reschedule
    silent: true
```

<!-- format: fighting -->
### Move set

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `moves.yaml` | `version: 1` | `fighting.Parse`, `fighting.Encode` | [fighting](fighting.md) |

```yaml
version: 1
input:
  up: up
  down: down
  left: left
  right: right
moves:
  - name: lp
    press:
      - lp
  - name: hadoken
    motion:
      - "2"
      - "3"
      - "6"
    window: 12
    press:
      - lp
    buffer: 8
    priority: 20
```

<!-- format: framedata -->
### Frame data

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `fighter.yaml` | `version: 1` | `framedata.Parse`, `framedata.Encode` | [framedata](framedata.md) |

```yaml
version: 1
moves:
  - name: idle
    animation: idle
    length: 8
    loop: true
    boxes:
      - kind: hurt
        start: 1
        end: 8
        x: -12
        "y": -80
        width: 24
        height: 80
```

<!-- format: platformer -->
### Platformer controller

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `hero.yaml` | `version: 1` | `platformer.ParseConfig`, `platformer.EncodeConfig` | [platformer](platformer.md) |

```yaml
version: 1
coyote_time: 100ms
jump_buffer: 100ms
jump_speed: 284
jump_cut: 0.55
jump_cut_min_speed: 110
gravity: 720
max_fall_speed: 320
run_speed: 98
ground_accel: 960
ground_decel: 1150
air_accel: 680
wall_slide_speed: 60
wall_coyote_time: 80ms
wall_jump_speed_x: 150
wall_jump_speed_y: 260
wall_jump_lock: 150ms
```

<!-- format: vehicle3d -->
### Vehicle

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `car.yaml` | `version: 1` | `vehicle3d.ParseConfig`, `vehicle3d.EncodeConfig` | [vehicle3d](vehicle3d.md) |

```yaml
version: 1
name: kart
mass: 300
chassisHalfExtents: [0.6, 0.2, 1]
chassisOffset: [0, 0, 0]
engineForce: 2000
reverseForce: 0
maxSpeed: 20
maxReverseSpeed: 5
brakeForce: 4000
handbrakeGrip: 0
maxSteerAngle: 25
steerRate: 90
drag: 0
rollingResistance: 0
rollInfluence: 0
angularDamping: 0
wheels:
  - name: front
    anchor: [0, -0.1, -0.8]
    radius: 0.25
    restLength: 0.2
    stiffness: 20000
    damping: 2000
    maxForce: 10000
    friction: 1.2
    steer: true
    brake: true
  - name: rear
    anchor: [0, -0.1, 0.8]
    radius: 0.25
    restLength: 0.2
    stiffness: 20000
    damping: 2000
    maxForce: 10000
    friction: 1.2
    drive: true
    brake: true
```

<!-- format: bullets -->
### Bullet patterns

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `patterns.yaml` | `version: 1` | `bullets.Parse`, `bullets.Encode` | [bullets](bullets.md) |

```yaml
version: 1
patterns:
  - name: bloom
    kind: ring
    frame: orb
    count: 24
    speed: 90
    spin: 7.5
    shots: 6
    interval: 400ms
    lifetime: 6s
    radius: 4
```

<!-- format: rts -->
### Formations

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `formations.yaml` | `version: 1` | `rts.ParseFormations`, `rts.Formations.Encode` | [rts](rts.md) |

```yaml
version: 1
formations:
  - name: line
    shape: line
    spacing: 1.5
  - name: wedge
    shape: wedge
    spacing: 1.5
```

<!-- format: ai -->
### Utility AI

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `guard.utility.yaml` | `version: 1` | `ai.ParseUtility`, `ai.EncodeUtility` | [ai](ai.md#utility-ai) |

```yaml
version: 1
momentum: 0.1
actions:
  - name: attack
    weight: 1.2
    considerations:
      - name: target_close
        input: target_distance
        max: 200
        curve:
          kind: linear
          slope: -1
          shiftY: 1
      - name: healthy
        input: health_ratio
        curve:
          kind: logistic
          slope: 1
          exponent: 10
          shiftX: 0.35
  - name: heal
    weight: 1.1
    considerations:
      - name: low_health
        input: health_ratio
        curve:
          kind: polynomial
          slope: -1
          exponent: 2
          shiftY: 1
      - name: has_potion
        input: potion_count
        curve:
          kind: step
          slope: 1
          shiftX: 0.5
```

<!-- format: ai#goap -->
### GOAP action set

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `camp.goap.yaml` | `version: 1` | `ai.ParseGOAP`, `ai.EncodeGOAP` | [ai](ai.md#goal-oriented-action-planning-goap) |

```yaml
version: 1
nodeBudget: 512
facts:
  bools:
    - has_axe
    - has_fire
  ints:
    - wood
actions:
  - name: fetch_axe
    cost: 1
    check: axe_available
    effects:
      bools:
        - fact: has_axe
          value: true
  - name: chop_wood
    cost: 1
    preconditions:
      bools:
        - fact: has_axe
          value: true
    effects:
      ints:
        - fact: wood
          op: add
          value: 1
  - name: gather_sticks
    cost: 3
    effects:
      ints:
        - fact: wood
          op: add
          value: 1
  - name: build_fire
    cost: 1
    preconditions:
      ints:
        - fact: wood
          op: gte
          value: 2
    effects:
      bools:
        - fact: has_fire
          value: true
      ints:
        - fact: wood
          op: add
          value: -2
goals:
  - name: light_campfire
    priority: 10
    target:
      bools:
        - fact: has_fire
          value: true
```

<!-- format: ai#bt -->
### Behavior tree

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.bt.yaml`, such as `guard.bt.yaml` | `version: 1` | `ai.ParseBehaviorTree`, `ai.EncodeBehaviorTree`, `ai.LoadBehaviorTreeFS`, `ai.BuildBehaviorTree` | [ai](ai.md#behavior-trees-in-yaml) · [schema](schemas.md) |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/bt.v1.json
version: 1
name: guard
blackboard:
  - key: enemy_seen
    type: bool
    default: false
  - key: enemy_distance
    type: float
    default: 999
root:
  type: selector
  children:
    - type: blackboard_condition
      key: enemy_seen
      op: eq
      value: true
      abort: both
      child:
        type: selector
        children:
          - type: blackboard_condition
            key: enemy_distance
            op: lte
            value: 32
            abort: both
            child:
              type: cooldown
              duration: 500ms
              child:
                type: action
                action: attack
                params:
                  damage: 15
          - type: action
            action: chase
            params:
              speed: 120
    - type: subtree
      path: patrol.bt.yaml
      params:
        wait: 200ms
      keys:
        home: home_pos
```

<!-- format: pathfind/navbake -->
### Navigation meshes

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `crypt.navmesh.yaml`, usually one per level and agent type | `version: 1` | `navbake.Parse`, `navbake.Mesh.Encode` | [navbake](navbake.md) |

```yaml
version: 1
agent: small
radius: 2
polygons:
  - [[2.0078125, 2.0078125], [37.992188, 2.0078125], [37.992188, 17.992188], [19.601562, 17.992188]]
  - [[2.0078125, 2.0078125], [19.601562, 17.992188], [18.863281, 18.296875]]
  - [[2.0078125, 2.0078125], [18.863281, 18.296875], [18.296875, 18.863281]]
  - [[2.0078125, 2.0078125], [18.296875, 18.863281], [17.992188, 19.601562]]
  - [[2.0078125, 2.0078125], [17.992188, 19.601562], [17.992188, 37.992188], [2.0078125, 37.992188]]
```

<!-- format: procgen -->
### Tile rules

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `ruins.yaml` | `version: 1` | `procgen.ParseRules`, `procgen.EncodeRules` | [procgen](procgen.md) |

```yaml
version: 1
tiles:
  - name: floor
    weight: 6
    walkable: true
  - name: water
    weight: 2
neighbors:
  - left: floor
    right: floor
  - left: floor
    right: water
  - left: water
    right: floor
  - left: water
    right: water
  - up: floor
    down: floor
  - up: floor
    down: water
  - up: water
    down: floor
  - up: water
    down: water
```

<!-- format: procgen#rooms -->
### Dungeon room templates

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `rooms.yaml` | `version: 1` | `procgen.ParseRoomTemplates`, `procgen.EncodeRoomTemplates` | [procgen](procgen.md) |

```yaml
version: 1
rooms:
  - id: start_hub
    weight: 1
    tags:
      - start
    doors:
      - side: north
        offset: 3
      - side: south
        offset: 3
      - side: east
        offset: 2
      - side: west
        offset: 2
    tiles:
      - '###.###'
      - '#.....#'
      - '.......'
      - '#.....#'
      - '###.###'
  - id: boss_vault
    tags:
      - goal
    width: 7
    height: 5
    source: rooms/boss_vault.tmj
    doors:
      - side: north
        offset: 3
      - side: south
        offset: 3
      - side: east
        offset: 2
      - side: west
        offset: 2
```

<!-- format: procgen#placement -->
### Procedural placement

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.placement.yaml` file the game loads, such as `forest.placement.yaml` | `version: 1` | `procgen.ParsePlacement`, `procgen.EncodePlacement` | [procgen](procgen.md) |

```yaml
version: 1
seed: 7
steps:
  - id: trees
    sampler: grid
    spacing: 2
    count: 6
    prefab: tree
  - id: lamps
    sampler: grid
    spacing: 4
    prefab: lamp
```

<!-- format: vn -->
### Visual novel cast

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `cast.yaml` | `version: 1` | `vn.ParseCast`, `vn.EncodeCast` | [vn](vn.md) |

```yaml
version: 1
slots:
  - name: left
    x: 0.25
characters:
  - id: aiko
    name: Aiko
    color: '#e07a5f'
    slot: left
    defaultExpression: neutral
    expressions:
      - name: neutral
        frame: aiko_neutral
      - name: smile
        frame: aiko_smile
```

<!-- format: rhythm -->
### Beat map

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A `.yaml` file next to the track, such as `opening.beatmap.yaml` | `version: 1` | `rhythm.ParseBeatMap`, `rhythm.EncodeBeatMap` | [rhythm](rhythm.md) |

```yaml
version: 1
title: Opening
offsetMs: 120
tempos:
  - beat: 0
    bpm: 120
  - beat: 62.5
    bpm: 140
meters:
  - beat: 0
    beatsPerBar: 4
```

<!-- format: idle -->
### Number locale

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `de.yaml` | `version: 1` | `idle.ParseLocale`, `idle.EncodeLocale` | [idle](idle.md) |

```yaml
version: 1
name: de
decimal: ','
group: .
exponent: E
units:
  - exp: 3
    suffix: ' Tsd.'
  - exp: 6
    suffix: ' Mio.'
```

<!-- format: quality -->
### Quality presets

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `quality.yaml` | `version: 1` | `quality.ParseConfig`, `quality.EncodeConfig` | [quality](quality.md) |

```yaml
version: 1
presets:
  low:
    resolution_scale: 0.5
    particle_scale: 0.25
    effects: false
    shadows: false
  medium:
    resolution_scale: 0.75
    particle_scale: 0.6
    effects: true
    shadows: false
  high:
    resolution_scale: 1
    particle_scale: 1
    effects: true
    shadows: true
devices:
  - model: Samsung SM-A
    os: android
    level: low
adapt:
  target_fps: 30
  window_frames: 120
  upgrade_windows: 3
```

## Servers

<!-- format: online/accounts -->
### Account service

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `accounts.yaml`, passed to `accountsd -config` | `version: 1` | `accounts.ParseConfig`, `accounts.Config.Marshal` | [accounts](accounts.md) |

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
tokenTTL: 1h30m30s
disableGuests: true
password:
  minLength: 10
  argon2id:
    time: 2
    memoryKiB: 19456
    threads: 1
allowedOrigins:
  - https://play.example.com
```

<!-- format: online/accounts/authtoken -->
### Token key set

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `keys.yaml`, written by `accountsd genkey` | `version: 1` | `authtoken.ParseKeySet`, `authtoken.KeySet.Marshal` | [accounts](accounts.md) |

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
keys:
  - id: 2317febd373fcbcd
    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
```

<!-- format: online/cloudsave -->
### Cloud save service

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `cloudsave.yaml`, passed to `cloudsaved -config` | `version: 1` | `cloudsave.ParseConfig`, `cloudsave.Config.Marshal` | [cloudsave](cloudsave.md) |

```yaml
version: 1
maxSlotBytes: 1048576
maxSlots: 32
keepVersions: 5
allowedOrigins:
  - https://play.example.com
```

<!-- format: online/leaderboard/selfhost -->
### Leaderboard service

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `leaderboards.yaml`, passed to `leaderboardd -config` | `version: 1` | `selfhost.ParseConfig`, `selfhost.Config.Marshal` | [leaderboard/selfhost](leaderboard-selfhost.md) |

```yaml
version: 1
proofTTL: 10m
boards:
  - name: high-score
    order: descending
    reset: allTime
  - name: weekly-sprint
    order: ascending
    reset: weekly
    weekStart: sunday
    keep: 4
allowedOrigins:
  - https://play.example.com
```

<!-- format: online/netplay/wire -->
### Netplay wire message schema

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.wire.yaml` (under `pkg/online/netplay/wire/` or a game package) | `version: 1` | `wire.Parse`, `wire.Encode` | [netplay/wire](netplay-wire.md); schema `https://game.nsgo.dev/schemas/wire.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/wire.v1.json
version: 1
package: props
order: le
enums:
  - name: Stance
    type: u8
    values:
      - name: idle
        value: 0
      - name: running
        value: 1
messages:
  - name: FighterProps
    fields:
      - name: team
        type: bits
        bits: 4
      - name: combo
        type: bits
        bits: 4
      - name: aim_x
        type: qfloat
        qfloat:
          min: -1
          max: 1
          bits: 12
      - name: aim_y
        type: qfloat
        qfloat:
          min: -1
          max: 1
          bits: 12
      - name: stance
        type: Stance
      - name: hp_delta
        type: svarint
        bits: 32
      - name: stamina
        type: fixed
        fixed:
          scale: 100
          type: u16
      - name: state
        type: mask
        mask:
          type: u8
          delta: true
          fields:
            - name: shield
              type: qfloat
              qfloat:
                min: 0
                max: 100
                bits: 10
            - name: tag
              type: string
```

## Files programs write in YAML

A program writes these for itself, but they are YAML for the same reasons: a person can read one, review its diff and, when needed, edit it.

<!-- format: editor -->
### Editor view state

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `.game/editor.yaml` (`editor.ViewStateFile`) | `version: 1` | The editor (`Editor.SaveViewState`, and opening a scene) | [Editor](editor.md#navigating-the-3d-view-891) |

`scenes` lists each scene the editor has shown, sorted by `path`, the scene file's slash-separated path from the project's root. `camera3D` is the 3D viewport's camera when the scene was last left, saved or closed: the point it looks at (`target`), its `yaw` and `pitch` in radians, its `distance` from the target, its vertical field of view `fov` in radians, its `near` and `far` clip planes and, when set, `orthographic`. Optional `snap` holds the project's editor snapping preferences (`grid`, `gridSize`, `pixel`, `rotation`, `rotationStepDeg`, `scale`, `scaleStep`, `showGrid`), omitted when all values match their defaults. A scene listed twice, a path outside the project, a negative snap step, and a value that is not finite are refused; a file that does not parse is written afresh, while one with a newer `version` is left as it is. A camera holding a value that is not finite is saved as the default camera.

```yaml
version: 1
scenes:
  - path: levels/courtyard.scene.yaml
    camera3D:
      target: [12, 0, -40]
      yaw: 0.61
      pitch: 0.49
      distance: 320
      fov: 1.0471976
      near: 0.5
      far: 10000
      orthographic: true
```

<!-- format: editor#recent -->
### Editor recent projects

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `recent-projects.yaml` in the user config directory (`<UserConfigDir>/nsgo-game/recent-projects.yaml`, outside any project) | `version: 1` | `editor.ParseRecentProjects`, `editor.EncodeRecentProjects` | [Editor & Scene Format](editor.md#start-screen-922) |

```yaml
version: 1
projects:
  - name: Dungeon Run
    path: /Users/alex/games/dungeon-run
    lastOpened: "2026-11-25T05:00:00Z"
  - name: Kart Test
    path: /Users/alex/games/kart-test
    lastOpened: "2026-11-24T12:30:00Z"
```

<!-- format: editor#agent-thread -->
### Editor agent threads

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `.nsgo/threads/<id>.yaml` (`editor.AgentThreadsDir`, ignored by `.nsgo/.gitignore`) | `version: 1` | `editor.ParseAgentThreadYAML`, `editor.EncodeAgentThreadYAML` | [Editor](editor.md#agent-threads-per-line-of-work-grouped-by-scene-1019); schema `https://game.nsgo.dev/schemas/agent-thread.v1.json` |

Each line of work is one strict versioned YAML file storing its `id`, `title` (named from its first request), `scenePath` (empty for `Project · no scene` threads), `sessionId` (so follow-up requests continue the external agent's session), RFC3339 timestamps, and chronological `requests` with `steps`, review `outcome` (`pending`, `kept`, `reverted`), change counts, changed `objects` (driving the Inspector footer link), and `historyStep` references.

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/agent-thread.v1.json
version: 1
id: door-lever-pulls
title: Door opens after two lever pulls
scenePath: scenes/level_01.scene.yaml
sessionId: sess_claude_01
createdAt: "2026-11-26T13:40:00Z"
updatedAt: "2026-11-26T14:18:00Z"
requests:
  - id: req-001
    title: Door opens after two lever pulls
    prompt: Keep the door shut until the lever is pulled twice, then let it open.
    createdAt: "2026-11-26T13:40:00Z"
    updatedAt: "2026-11-26T13:41:00Z"
    outcome: kept
    changesTotal: 3
    changesKept: 3
    files:
      - prefabs/door.prefab.yaml
      - scenes/level_01.scene.yaml
    objects:
      - Door
      - lever_01
    detail: Door.locked, Door.opens_after_pulls, lever_01.pulls
    historyStep: 'Agent: Door opens after two lever pulls'
    historyIndex: 1
    steps:
      - id: step-001
        label: Read level_01.scene.yaml and door.prefab.yaml
        status: done
```

<!-- format: editor#checkpoint -->
### Editor turn checkpoints

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `.nsgo/checkpoints/<id>/manifest.yaml` (`editor.CheckpointsDir`, `editor.CheckpointManifestFile`), with saved pre-turn file contents stored verbatim under `.nsgo/checkpoints/<id>/files/<path>` and ignored by Git | `version: 1` | `editor.ParseCheckpointManifest`, `editor.EncodeCheckpointManifest`, `Editor.BeginCheckpointTurn`, `Editor.RestoreCheckpoint` | [Editor & Scene Format](editor.md#persistent-per-turn-checkpoints-1018) |

Each agent turn, autonomous task, or automation records the pre-turn bytes of every project file before its first write (scenes, prefabs, data tables, `game.yaml`, Go code, and binary media alike). `files` is sorted by slash-separated `path` relative to the project root. When `existed` is `true`, `size` and `sha256` describe the raw bytes stored under `files/<path>`, with optional 4-digit octal `mode` when non-default; when `existed` is `false`, restoring deletes the file that the turn created. A size cap (`Editor.CheckpointMaxBytes`, defaulting to 128 MiB) drops the oldest completed checkpoints.

```yaml
version: 1
id: 20261126T144100Z-0001
createdAt: "2026-11-26T14:41:00Z"
who: agent
kind: turn
title: Coin spin, bob and sparkle
detail: 2 files
files:
  - path: fx/sparkle.particles.yaml
    existed: false
  - path: prefabs/coin.prefab.yaml
    existed: true
    size: 142
    sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  - path: scenes/level_01.scene.yaml
    existed: true
    size: 512
    sha256: fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
```

<!-- format: editor#layout -->
### Editor panel layout

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `layout.yaml` in the user config directory (`<UserConfigDir>/nsgo-game/layout.yaml`, outside any project) | `version: 1` | `editor.ParseLayout`, `editor.EncodeLayout`, `Editor.SetLayoutPath` | [Editor & Scene Format](editor.md#dockable-panels-and-layout-919) |

Stores the user's dockable panel arrangement across sessions: `center` (a tab group or split tree holding `viewport` and any panels docked beside it), optional `left`, `right` and `bottom` docks (each with `size` in points, optional `closed`, and `root`), and optional sorted `hidden` panel keys. Each node is either a tab group (`panels` and optional `active` tab index) or a split (`axis`: `"horizontal"` or `"vertical"`, optional `sizes` in points, and `children`). Built-in panels use keys such as `viewport`, `scene_tree`, `layers`, `assets`, `files`, `library`, `inspector`, `problems`, `output`, `profiler`, `animation`, `search`, `states`, `tileset`, `terrain`, `rig`, `particles`, `material`, `environment`, `datatable`, `navmesh`, and `localization`; plugin panels use `plugin:<plugin>:<id>`. Unknown fields, duplicate keys, duplicate panel entries, and unsupported versions are refused; a saved panel that no longer exists when the editor starts is dropped on restore.

```yaml
version: 1
center:
  panels:
    - viewport
left:
  size: 444
  root:
    axis: horizontal
    sizes: [220, 224]
    children:
      - axis: vertical
        sizes: [0, 200]
        children:
          - panels:
              - scene_tree
          - panels:
              - layers
      - axis: vertical
        children:
          - panels:
              - assets
          - panels:
              - files
              - inspector
            active: 1
bottom:
  size: 164
  root:
    panels:
      - problems
      - output
hidden:
  - layers
```

<!-- format: tooling/assetbuild -->
### Asset build cache

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `.game/assets-cache.yaml`, or the path `assets.cache` in `game.yaml` names | `version: 1` | `game assets` | [Command Line](cli.md#assets) |

```yaml
version: 1
targets:
  - target: atlas:ui
    key: 3f79bb7b435b05321651daefd374cdc681dc06faa65e374e38337b88ca046dea
    outputs:
      - path: assets/atlases/ui.json
        sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
      - path: assets/atlases/ui.png
        sha256: 60303ae22b998861bce3b28f33eec1be758a213c86c93c076dbe9f558c11c752
```

<!-- format: tooling/bench -->
### Benchmark baseline

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `benchmarks/baseline.yaml`, written by `game bench -update` | `version: 1` | `bench.Decode`, `bench.Baseline.Encode` | [Command Line](cli.md) |

```yaml
version: 1
note: Update with go tool game bench -update.
max_regression_pct: {ns_per_op: 25, bytes_per_op: 30, allocs_per_op: 12.5}
benchmarks:
  BenchmarkBatchDraw1000: {ns_per_op: 3500000, bytes_per_op: 80128, allocs_per_op: 1000}
  BenchmarkRaycast: {ns_per_op: 420.5, bytes_per_op: 0, allocs_per_op: 0}
```

<!-- format: online/achievements#progress -->
### Achievement progress

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key in `core.Storage`, kept by `achievements.Tracker.Persist` | `version: 1` | `achievements.ParseProgress`, `achievements.EncodeProgress` | [achievements](achievements.md) |

```yaml
version: 1
achievements:
  - id: first-match
    steps: 1
  - id: gem-collector
    steps: 120
    reported:
      gameCenter: 120
      playGames: 100
```

<!-- format: rhythm#calibrations -->
### Latency calibrations

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key in `core.Storage`, kept by `rhythm.OffsetStore` | `version: 1` | `rhythm.ParseCalibrations`, `rhythm.EncodeCalibrations` | [rhythm](rhythm.md) |

```yaml
version: 1
devices:
  android/Google Pixel 8:
    audioMs: 41.25
    videoMs: -3
```

<!-- format: replay -->
### Input recording

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.replay.yaml`, written by a `replay.Recorder` | `version: 2` | `replay.Decode`, `replay.Recording.Encode` | [replay](replay.md) |

```yaml
version: 2
seed: 99537266
rate: 60
actions: [left, right, jump]
axes: [move_x]
total_steps: 140
steps:
  - {step: 6, press: [right], axes: {move_x: 1}}
  - {step: 85, press: [jump]}
  - {step: 95, release: [jump]}
  - {step: 130, release: [right], axes: {move_x: 0}}
```

<!-- format: vehicle3d#ghost -->
### Ghost lap

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.ghost.yaml`, written from a `vehicle3d.GhostRecorder` | `version: 1` | `vehicle3d.ParseGhost`, `vehicle3d.EncodeGhost` | [vehicle3d](vehicle3d.md) |

```yaml
version: 1
name: reference lap
rate: 60
interval: 2
lapSteps: 2
frames:
  - p: [0, 0.5, 0]
    r: [0, 0, 0, 1]
  - p: [0, 0.5, 1.25]
    r: [0, 0.0499792, 0, 0.99875]
```

<!-- format: undo -->
### Undo log

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Wherever the game keeps it, such as a save slot; the moves are the game's own move type | `version: 2` (1 is read) | `undo.DecodeLog`, `undo.EncodeLog` | [undo](undo.md) |

```yaml
version: 2
applied: 2
forgotten: 0
moves:
  - dir: right
  - dir: down
  - dir: down
```

<!-- format: statedump -->
### State dump

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.state.yaml`, written by `statedump.WriteFile` | `version: 1` | `statedump.Parse`, `statedump.Encode` | [statedump](statedump.md) |

```yaml
version: 1
seed: 42
rate: 60
steps: 90
scene:
  depth: 1
  current: level
values:
  score: 40
entities:
  - index: 0
    generation: 1
    components:
      - type: pos
        data:
          x: 4
          y: 12
```

<!-- format: tooling/playtest -->
### Playtest configuration

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `playtest.yaml`, read by `go tool game playtest` and `game_playtest` | `version: 1` | `playtest.ParseConfig`, `playtest.EncodeConfig` | [playtest](playtest.md) |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/playtest.v1.json
version: 1
scope:
  project: dungeon-run
  scene: level_01
  template: platformer2d
runs:
  count: 20
  seedStart: 1
  rate: 60
  speed: 1
  maxDuration: 1m30s
bot:
  profile: platformer-runner
  reactionMin: 120ms
  reactionMax: 220ms
metrics:
  - name: finished
    label: Finished
    measure:
      kind: outcome
      key: finished
      aggregate: count
    goal:
      summary: most first-timers clear floor 1
      source: brief.yaml
      min: 16
  - name: deaths
    label: Deaths
    measure:
      kind: event
      key: death
      aggregate: sum
      heatMap: true
```

<!-- format: tooling/playtest#report -->
### Playtest report

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `playtest.report.yaml`, written by `go tool game playtest` and `playtest.SaveReportFile` | `version: 1` | `playtest.ParseReport`, `playtest.EncodeReport` | [playtest](playtest.md) |

```yaml
version: 1
scope:
  project: dungeon-run
  scene: level_01
bot:
  profile: platformer-runner
  reactionMin: 120ms
  reactionMax: 220ms
rate: 60
speed: 1
seedCount: 1
passed: false
metrics:
  - name: finished
    label: Finished
    aggregate: count
    value: 0
    goal:
      summary: most first-timers clear floor 1
      source: brief.yaml
      min: 1
    status: missed
heatMaps:
  - metric: deaths
    event: death
    points:
      - seed: 1
        step: 140
        x: 300
        "y": 144
        tag: slime_pit
runs:
  - index: 1
    seed: 1
    outcome: failed
    reason: out of lives
    steps: 140
    durationSeconds: 2.3333333333333335
    recording: recordings/run_001_seed_1.replay.yaml
    events:
      - kind: death
        step: 140
        timeSeconds: 2.3333333333333335
        x: 300
        "y": 144
        tag: slime_pit
```

<!-- format: tilemap#chunk -->
### Tilemap chunk

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `chunk_<x>_<y>.yaml` (or `.json`), written by `tilemap.DirChunkStore` and `tilemap.EncodeChunkYAML` / `EncodeChunkJSON` | `version: 1` | `tilemap.DecodeChunkYAML`, `tilemap.EncodeChunkYAML`, `tilemap.DecodeChunkJSON`, `tilemap.EncodeChunkJSON` | [tilemap](tilemap.md#sparse-chunked-layers-and-streaming) |

```yaml
version: 1
x: -2
"y": 1
size: 4
runs:
  - 0*5
  - "1"
  - "2147483650"
  - 3*4
  - 0*5
```

## JSON files

These formats are JSON: a program writes most of them for itself, and the rest are what other services and tools exchange. Each guide shows their fields.

<!-- format: save -->
### Save slot

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<slot>.json` and `<slot>.json.bak` in a directory, or a key in `core.Storage` | the envelope's `version`, the game's own schema version, upgraded by its migrations | `save.Store.Save`, `save.Store.Load` | [save](save.md) |

```json
{"version":2,"data":{"level":4,"coins":120,"name":"Ann"}}
```

`data` holds the game's own value as `encoding/json` writes it, such as a struct with a `quest.Snapshot` and an inventory snapshot.

<!-- format: save#settings -->
### Player settings

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| The `settings` slot of a save backend, such as `settings.json`, or a key in `core.Storage`, apart from the save slots | the envelope's `version`, upgraded by the settings store's migrations | `save.SettingsStore` | `save.Settings`: `audio`, `bindings`, `display`, `locale`, `accessibility` and `custom`, or the game's own type |

```json
{"version":1,"data":{"audio":{"master":1,"groups":{"music":1,"sfx":1,"ui":1},"muted":false},"bindings":{"jump":["Space"]},"display":{"fullscreen":false,"display_index":0,"scale_mode":"fit","integer_scale":false,"frame_cap":0,"vsync":true,"auto_drop_low_power":true,"quality":"auto"},"locale":"en","accessibility":{"captions":false,"caption_scale":1,"color_blind_mode":"none","reduce_motion":false,"high_contrast":false,"screen_shake":1,"game_speed":1}}}
```

<!-- format: worldmap#state -->
### World map discovery

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Inside a save slot | `version: 1` | `worldmap.State` as JSON | [worldmap](worldmap.md) |

```json
{"version":1,"current":"hall","visited":["cave","hall"],"revealed":["crypt"],"markers":[{"id":"m1","kind":"pin","room":"hall","x":12,"y":4,"label":"Key"}]}
```

<!-- format: quest#snapshot -->
### Quest progress

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Inside a save slot, as JSON or YAML | `version: 1` | `quest.Log.Snapshot`, `quest.Log.Restore` | [quest](quest.md) |

```json
{"version":1,"quests":[{"id":"find-key","state":"active","progress":{"rats":3}},{"id":"intro","state":"completed"}]}
```

`state` is `active`, `completed`, `failed` or `abandoned`; a quest that has not started is left out, as is an objective with no progress.

<!-- format: fog -->
### Fog of war

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Inside a save slot | `version: 1` | `fog.Fog` as JSON | [fog](fog.md) |

```json
{"version":1,"width":3,"height":2,"tileWidth":8,"tileHeight":8,"opaque":[".#.","..."],"nextSource":2,"sources":[{"id":1,"team":2,"col":2,"row":1,"radius":1},{"id":2,"team":1,"col":0,"row":0,"radius":0}],"teams":[{"team":1,"explored":["#..","..."]},{"team":2,"explored":[".##",".##"]}]}
```

<!-- format: entities -->
### World snapshot

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Inside a save slot; only components registered with `RegisterCodec` | none | `entities.World.Snapshot`, `entities.World.Restore` | [entities](entities.md) |

```json
{"entities":[{"index":0,"generation":1,"components":[{"type":"health","data":{"hp":10}},{"type":"position","data":{"x":3,"y":4}}]},{"index":1,"generation":1,"components":[{"type":"position","data":{"x":-1,"y":0.5}}]}]}
```

`type` is the name a component's codec was registered under with `RegisterCodec`, and `data` is the component as `encoding/json` writes it.

<!-- format: input -->
### Input bindings

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key in `core.Storage` | `version: 1` | `input.Actions.Save`, `input.Actions.Load` | [input](input.md) |

```json
{"version":1,"actions":{"fire":[{"kind":"mouse_button","button":"left"}],"jump":[{"kind":"key","key":"space"},{"kind":"gamepad_button","button":"south"}]},"axes":{"move_x":{"negative":"left","positive":"right"}}}
```

<!-- format: tooling/gamedebug -->
### Console tunables

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key in `core.Storage`, `gamedebug.console.tunables` by default | none | `gamedebug.Console.SaveTunables`, `gamedebug.Console.LoadTunables` | An object of each tunable's name and value |

```json
{"cheat.god":false,"level.name":"cave_1","player.lives":3,"player.speed":1.5}
```

The names are those the game registered with `RegisterFloat`, `RegisterInt`, `RegisterBool` and `RegisterString`; `LoadTunables` skips a name nothing registered, refuses a value outside its range or of the wrong JSON type, and applies none of the values when it refuses one.

<!-- format: tooling/devstate -->
### Dev session snapshot

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A file or a key in `core.Storage` that a game restores its state from after a rebuild | a hash of the state's Go type, which skips the restore when it changed | `devstate.Store.Save`, `devstate.Store.LoadSnapshot` | `devstate.SessionSnapshot` |

```json
{"scene_id":"level-1","schema_hash":"90b889efbda2b1fbe5fd7122b8919ac57b14cef258a4a60664a39e4ee3caee5b","loop_tick":1200,"loop_elapsed":20000000000,"payload":"eyJsZXZlbCI6Mn0=","saved_at":"2026-11-24T09:15:02Z"}
```

`payload` is the game's state as `encoding/json` writes it, in base64, and `loop_elapsed` is in nanoseconds.

<!-- format: online/liveops -->
### Cloud save payload

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<prefix><slot>.json` in a platform's cloud storage, such as `save.main.json` | `version: 1` | `liveops.EncodeCloudSave`, `liveops.DecodeCloudSave` | [liveops](liveops.md) |

```json
{
  "format": "nsgo-cloud-save",
  "version": 1,
  "revision": 3,
  "hash": "sha256:b30e459ab9ad862b330d89d42e3422a3e3193071345b1de6b9e4038b27f2ee01",
  "modified": "2026-11-24T09:15:02Z",
  "device": "phone-1",
  "data": "{\"version\":2,\"data\":{\"level\":4}}"
}
```

`hash` is the SHA-256 of the save's bytes, which `data` holds as text, or `data_base64` in base64 when they are not UTF-8.

<!-- format: online/liveops#sync -->
### Cloud sync state

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key per slot in `core.Storage`, `<StateKey>.<slot>`, such as `liveops.cloudsync.main` | `version: 1` | `liveops.CloudSyncer`, which keeps it for itself | [liveops](liveops.md) |

```json
{"version":1,"revision":3,"hash":"sha256:b30e459ab9ad862b330d89d42e3422a3e3193071345b1de6b9e4038b27f2ee01","local_modified":"2026-11-24T09:15:02Z","local_hash":"sha256:b30e459ab9ad862b330d89d42e3422a3e3193071345b1de6b9e4038b27f2ee01"}
```

It records the cloud revision and content hash both sides held after the last sync, and when this device last wrote the local copy.

<!-- format: online/liveops#review -->
### Review prompt state

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key in `core.Storage`, `StorageKey` (default `liveops.review_prompt`) | `version: 1` | `liveops.ReviewPrompt`, which keeps it for itself | [liveops](liveops.md#review-prompts) |

```json
{"version":1,"appVersion":"1.2.0","firstLaunch":"2026-03-01T10:00:00Z","sessions":4,"positiveMoments":3,"lastPromptedAt":"2026-03-05T14:30:00Z","lastPromptedVersion":"1.2.0"}
```

It records the current app version, first-launch timestamp, lifetime session count, per-version positive moments, and the timestamp and version of the last store rating request. Any unrecognised field, invalid timestamp or unsupported version fails closed without overwriting the key.

<!-- format: online/leaderboard/selfhost#records -->
### Leaderboard records

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<dir>/<board>/<period>.jsonl`, a line per new best score | none | `selfhost.FileStore` | [leaderboard/selfhost](leaderboard-selfhost.md) |

```json
{"account":"3f9a...","score":4200,"achieved":"2026-11-24T09:15:02.5Z"}
```

<!-- format: online/netplay -->
### Packet capture

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.capture.jsonl` / `*.jsonl`, one JSON object per captured `Send` or `Recv` | `version: 1` | `netplay.NewCaptureTransport`, `netplay.ReadCapture`, `netplay.ParseCaptureRecord`, `netplay.EncodeCaptureRecord` | [netplay](netplay.md#packet-capture-and-inspection) |

```json
{"version":1,"t":16000000,"dir":"send","peer":2,"ch":1,"seq":1,"payload":"IwEAAAAB"}
```

Each line records one plaintext transport packet (`t` elapsed nanoseconds since capture start, `dir` `"send"` or `"recv"`, `peer`, `ch`, `seq`, and base64 `payload`) for inspection with `game net decode` and `game net stats`. Capture files contain player inputs and game state.

<!-- format: online/audit -->
### Audit log

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<yyyy-mm-dd>.jsonl` in the directory `audit.Options.Dir` names, one file per UTC day, appended to only | `v: 1` on each line | `audit.Open`, `audit.Writer.Log` | [gameserver](gameserver.md#audit-log) |

```json
{"v":1,"time":"2026-11-28T09:15:02.500Z","actor":"admin:ada","action":"peer.kick","subject":"peer:42","data":{"reason":"cheating"}}
```

`time` is UTC, RFC 3339 with milliseconds; `actor` is an account ID, `"system"` or an admin token subject; `subject` names the peer, account or board/slot the `action` acted on; `data` holds the action's details and is omitted when empty.

<!-- format: l10n#catalog -->
### String table

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A `.json` file per locale, flat, nested or with CLDR plural forms | none | `l10n.Catalog.LoadJSON` | [l10n](l10n.md) |

```json
{
	"menu": {"play": "Play"},
	"hud": {"score": "Score: {score}"},
	"coins": {"zero": "No coins", "one": "{count} coin", "other": "{count} coins"}
}
```

<!-- format: anim3d -->
### Clip events

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A JSON sidecar beside a model, or the `extras` of a glTF animation | `version: 1` (`anim3d.EventsSidecarVersion`) in a sidecar; unknown fields and duplicate keys refused | `anim3d.LoadEventsSidecar`, `anim3d.MarshalEventsSidecar`, `anim3d.LoadEventsFromGLTFExtras` | [anim3d](anim3d.md), [3D Models from Blender](3d-models-blender.md) |

```json
{
  "version": 1,
  "clips": {
    "attack": [
      {
        "time": 0.4,
        "name": "hit",
        "data": {
          "damage": 12
        }
      }
    ],
    "run": [
      {
        "time": 0.25,
        "name": "footstep",
        "payload": "left"
      },
      {
        "time": 0.75,
        "name": "footstep",
        "payload": "right"
      }
    ]
  }
}
```

<!-- format: assets -->
### DLC pack manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A `.json` file the game downloads, listing the packs it can install | none; each pack has its own `version` | `assets.ParsePackManifest` | [assets](assets.md) |

```json
{"packs": [{"id": "forest", "url": "https://cdn.example.com/forest.zip", "version": "1.0.0", "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}]}
```

<!-- format: assets#remote -->
### Remote asset manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `remote.yaml`, written by `game assets` when `assets.remote` is configured | `version: 1` | `assets.ParseRemoteManifest`, `assets.EncodeRemoteManifest` | [assets](assets.md#remote-assets) |

```yaml
version: 1
base: https://cdn.example.com/game/
files:
  - uri: asset:music/theme.m4a
    path: music/theme.9f86d081.m4a
    size: 48120
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  - uri: asset:sprites/hero.png
    path: sprites/hero.60303ae2.png
    size: 8192
    sha256: 60303ae22b998861bce3b28f33eec1be758a213c86c93c076dbe9f558c11c752
```

<!-- format: assets#remote-cache -->
### Remote asset cache index

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `index.yaml` inside a `DirRemoteCache` directory | `version: 1` | `assets.DirRemoteCache` | [assets](assets.md#remote-assets) |

```yaml
version: 1
entries:
  - sha256: 60303ae22b998861bce3b28f33eec1be758a213c86c93c076dbe9f558c11c752
    size: 8192
    accessed: 1732742400000000001
  - sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
    size: 48120
    accessed: 1732742400000000002
```

<!-- format: tooling/sizecheck -->
### WebAssembly size budget

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A `.json` file passed to `game size -budget` when it checks a `.wasm`, such as `benchmarks/web-budget.json` | none | `sizecheck.LoadBudget` | [Package Size Budgets](package-size-budgets.md) |

```json
{
  "target": "./internal/sizeprobe",
  "ldflags": "-s -w",
  "max_raw_bytes": 8388608,
  "max_gzip_bytes": 2359296,
  "note": "WebAssembly size budget for ./internal/sizeprobe"
}
```

<!-- format: tooling/sizecheck#mobile -->
### Mobile package size budget

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A `.json` file passed to `game size -budget` when it checks an `.aab`, `.apk` or `.ipa`, such as `benchmarks/android-aab-budget.json` | none | `sizecheck.LoadMobileBudget` | [Package Size Budgets](package-size-budgets.md) |

```json
{
  "max_total_bytes": 52428800,
  "max_per_abi_bytes": 20971520,
  "max_category_bytes": {
    "native_libs": 36700160,
    "dex": 5242880
  },
  "note": "Release budget for MyGame Android AAB"
}
```

<!-- format: internal/apiref -->
### API reference

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `api.json`, published at `https://game.nsgo.dev/api.json` | `version: 1` | `apiref.Marshal`, `apiref.Unmarshal` | [Packages](packages.md) |

```json
{
  "version": 1,
  "module": "example.com/mygame",
  "packages": [
    {
      "importPath": "example.com/mygame/score",
      "name": "score",
      "dir": "score",
      "page": "score",
      "synopsis": "Package score keeps a player's score.",
      "doc": "Package score keeps a player's score.\n",
      "consts": [],
      "vars": [],
      "funcs": [
        {
          "name": "Add",
          "anchor": "Add",
          "decl": "func Add(a, b int) int",
          "doc": "Add returns a plus b.\n",
          "file": "score/score.go",
          "line": 4
        }
      ],
      "types": []
    }
  ]
}
```

## Other tools' formats

These are read as the tools that make them write them, never converted into a form of this repository's own.

<!-- format: tilemap -->
### Tiled map

| Files | Read by | Guide |
| --- | --- | --- |
| Tiled XML (`.tmx`) and JSON (`.tmj`) maps with embedded or external (`.tsx`, `.tsj`) tilesets and CSV, XML, or base64 (`uncompressed`, `zlib`, `gzip`) tile data | `tilemap.LoadTiled`; `rooms.FromTiledLayer` makes rooms from an object layer | [tilemap](tilemap.md), [rooms](rooms.md) |

<!-- format: tilemap#world -->
### Tiled world

| Files | Read by | Guide |
| --- | --- | --- |
| Tiled world files (`.world`), listing map placements and regex filename patterns with multipliers and offsets | `tilemap.ParseTiledWorld`, `tilemap.LoadTiledWorld` | [tilemap](tilemap.md) |

<!-- format: tilemap#ldtk -->
### LDtk project

| Files | Read by | Guide |
| --- | --- | --- |
| LDtk projects (`.ldtk`) | `tilemap.LoadLDtk`, `tilemap.LoadLDtkAll`, `tilemap.LoadLDtkWorld`; `rooms.FromLDtk` makes rooms from a world's levels | [tilemap](tilemap.md), [rooms](rooms.md) |

<!-- format: sprite -->
### Sprite atlas

| Files | Read by | Guide |
| --- | --- | --- |
| TexturePacker "JSON (Hash)" and "JSON (Array)", Aseprite `json-hash` and `json-array`; `game pack` writes TexturePacker's JSON (Hash) | `sprite.LoadAtlas` | [sprite](sprite.md) |

<!-- format: text -->
### Fonts

| Files | Read by | Guide |
| --- | --- | --- |
| AngelCode BMFont in text, XML or JSON; msdf-atlas-gen JSON for signed-distance-field fonts | `text.LoadBMFont`, `text.LoadSDFFont` | [text](text.md) |

<!-- format: gltf -->
### glTF model

| Files | Read by | Guide |
| --- | --- | --- |
| glTF 2.0 (`.gltf` with its buffers, or `.glb`), including Draco and meshopt compression | `gltf.Load`, `gltf.LoadFS` | [3D Models from Blender](3d-models-blender.md) |

<!-- format: world3d -->
### Grid map mesh library

| Files | Read by | Guide |
| --- | --- | --- |
| A glTF 2.0 JSON document whose nodes with a mesh become the library's tiles, bounded by their `POSITION` accessors | `world3d.LoadMeshLibraryFromGLTF` | [world3d](world3d.md) |

<!-- format: tooling/gamedebug#trace -->
### Profiler trace

| Files | Written by | Guide |
| --- | --- | --- |
| Chrome's trace event JSON, opened in Perfetto or `chrome://tracing` | `gamedebug.Profiler.WriteChromeTrace` | `gamedebug.Profiler` |

<!-- format: flow#vars -->
### Game variables and switches

| Files | Version | Read / written by | Guide |
| --- | --- | --- | --- |
| `vars.yaml` or `*.vars.yaml` declaring project-wide switches (`bool`) and variables (`int`, `float`, `string`), defaults, optional ranges and docs | `1` (`flow.VarsFormatVersion`) | `flow.ParseVars`, `flow.LoadVarsFS`, `flow.EncodeVars` | [flow](flow.md) |

```yaml
version: 1
vars:
  - name: bridge_repaired
    kind: bool
    doc: True once the east bridge has been repaired.
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 9999
    doc: Player gold coins.
```

<!-- format: flow#mapevent -->
### Map events

| Files | Version | Read / written by | Guide |
| --- | --- | --- | --- |
| A map event's pages, each with `when` conditions on self switches, switches and variables, a trigger (`action`, `touch`, `autorun`, `parallel`) and `say`, `wait`, `set` and `self` commands | `1` (`flow.MapEventVersion`) | `flow.ParseMapEvent`, `flow.EncodeMapEvent` | [flow](flow.md#map-events-parsemapevent-eventrunner) |

```yaml
version: 1
pages:
  - when:
      self:
        opened: false
    trigger: action
    commands:
      - say:
          text: chest.found_potion
      - self:
          name: opened
          value: true
  - when:
      self:
        opened: true
    trigger: action
```

<!-- format: dialogue -->
### Yarn dialogue script

| Files | Read by | Guide |
| --- | --- | --- |
| Yarn Spinner scripts (`.yarn`), written by hand: nodes of lines, `->` choices and `<<commands>>`, each under a `title:` header between `---` and `===` | `dialogue.ParseYarn`; `vn.Presenter` plays one | [dialogue](dialogue.md) |

```yarn
title: Start
---
Aiko: Welcome to the old observatory. #line:obs_01
-> Look through the telescope #line:obs_c1
    Aiko: Careful, the lens is old. #line:obs_05
    <<jump Telescope>>
-> Leave #line:obs_c2
===
title: Telescope
---
<<set $looked = true>>
Ren: The sky is the same size. #line:obs_06
===
```

## Binary files

Media that is binary by nature stays binary: images (PNG), sound (WAV, AAC, MP3), font pages, glTF buffers and `.glb` files, and KTX2 textures that `game assets` encodes. So do the packets of `online/netplay`, which are data on the wire, and a fighting recognizer's rollback state, which lives in memory. Every one of them is described by a text file above where it needs settings: an import settings sidecar, `game.yaml` or a glTF document.

## Keeping this page current

Each entry starts with a line `<!-- format: <package>[#<name>] -->`. Each YAML format's conformance test (`internal/formattest`) names its entry so and checks that every YAML example in it, on this page and its translation, loads and saves to the same bytes. `TestFormatsPage` in `internal/repocheck` checks the rest: that the two languages list the same entries in the same order; that every other entry is listed in the test with the declaration that reads its format, which the package must still have, and that its JSON and Yarn examples load with the format's reader; that every package that decodes JSON has an entry unless the test says why it needs none; and that every exported integer constant whose name ends in `Version` is the version of a format listed here. A format none of these finds, such as an unversioned text format in a package that already has an entry, can still be missed, so a new format adds its entry here in the same change.
