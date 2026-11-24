# File Formats

Every file a game made with these packages reads or writes, with the names its files go by, the version it declares, the Go functions that read and write it, the guide that describes its fields and an example, so a person or an AI can edit one without reading the code.

The files a game's author writes are YAML. The examples below are in each format's canonical form, the bytes its encoder writes, and each one is loaded and saved by the format's tests, so none shows a key the format refuses or a layout it would rewrite.

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
| `game.yaml` at the project's root | `version: 1` | `project.Parse`, `project.Encode` | [Project File](project.md) |

```yaml
version: 1
name: Cave Run
id: dev.nsgo.caverun
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

<!-- format: tooling/importsettings -->
### Import settings sidecar

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<asset>.import.yaml` beside an image or sound, such as `hero.png.import.yaml` | `version: 1` | `importsettings.Parse`, `importsettings.Encode` | [Command Line](cli.md#assets); schema `https://game.nsgo.dev/schemas/import.v1.json` |

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/import.v1.json
version: 1
image:
  filter: nearest
  slice:
    width: 16
    height: 16
    count: 6
credit:
  author: Kenney
  license: CC0-1.0
  source: https://kenney.nl/assets/kenney-fonts
```

<!-- format: refs -->
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
    packVersion: "1"
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

<!-- format: modding -->
### Mod manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `mod.yaml` at the root of a mod's directory | `manifestVersion: 1` | `modding.ParseManifest`, `modding.EncodeManifest` | [modding](modding.md) |

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

## Scenes, resources and animation

<!-- format: sceneformat -->
### Scene

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.scene.yaml`, such as `scenes/cave_room.scene.yaml` | `version: 1`, or `version: 2` for a scene that instances or inherits other scene files | `sceneformat.Parse`, `sceneformat.Encode` | [Editor & Scene Format](editor.md); schemas `https://game.nsgo.dev/schemas/scene.v1.json` and `scene.v2.json` |

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

<!-- format: particles -->
### Particle emitter

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `sparks.yaml` | `version: 1` | `particles.UnmarshalConfigYAML`, `particles.MarshalConfigYAML`; the same fields as JSON with `UnmarshalConfigJSON` and `MarshalConfigJSON` | [particles](particles.md) |

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
| Any `.yaml` file the game loads, such as `cutscenes/intro.yaml` | `version: 1` | `cutscene.Parse`, `cutscene.Encode` | [cutscene](cutscene.md) |

```yaml
version: 1
source: asset:cutscenes/intro.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
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

<!-- format: tooling/datatable -->
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

<!-- format: achievements -->
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

<!-- format: leaderboard -->
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

<!-- format: vehicle -->
### Vehicle

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Any `.yaml` file the game loads, such as `car.yaml` | `version: 1` | `vehicle.ParseConfig`, `vehicle.EncodeConfig` | [vehicle](vehicle.md) |

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

<!-- format: accounts -->
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
```

<!-- format: accounts/token -->
### Token key set

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `keys.yaml`, written by `accountsd genkey` | `version: 1` | `token.ParseKeySet`, `token.KeySet.Marshal` | [accounts](accounts.md) |

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
keys:
  - id: 2317febd373fcbcd
    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
```

<!-- format: cloudsave -->
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

<!-- format: leaderboard/selfhost -->
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
```

## Files programs write in YAML

A program writes these for itself, but they are YAML for the same reasons: a person can read one, review its diff and, when needed, edit it.

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

<!-- format: achievements#progress -->
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

<!-- format: vehicle#ghost -->
### Ghost lap

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `*.ghost.yaml`, written from a `vehicle.GhostRecorder` | `version: 1` | `vehicle.ParseGhost`, `vehicle.EncodeGhost` | [vehicle](vehicle.md) |

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
| Wherever the game keeps it, such as a save slot; the moves are the game's own move type | `version: 1` | `undo.DecodeLog`, `undo.EncodeLog` | [undo](undo.md) |

```yaml
version: 1
applied: 2
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

<!-- format: worldmap#state -->
### World map discovery

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| Inside a save slot | `version: 1` | `worldmap.State` as JSON | [worldmap](worldmap.md) |

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

<!-- format: input -->
### Input bindings

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key in `core.Storage` | `version: 1` | `input.Actions.Save`, `input.Actions.Load` | [input](input.md) |

<!-- format: debug -->
### Console tunables

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A key in `core.Storage`, `debug.console.tunables` by default | none | `debug.Console.SaveTunables`, `debug.Console.LoadTunables` | An object of each tunable's name and value |

<!-- format: tooling/devstate -->
### Dev session snapshot

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A file or a key in `core.Storage` that a game restores its state from after a rebuild | a hash of the state's Go type, which skips the restore when it changed | `devstate.Store.Save`, `devstate.Store.LoadSnapshot` | `devstate.SessionSnapshot` |

<!-- format: liveops -->
### Cloud save payload

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<prefix><slot>.json` in a platform's cloud storage, such as `save.main.json` | `version: 1` | `liveops.EncodeCloudSave`, `liveops.DecodeCloudSave` | [liveops](liveops.md) |

<!-- format: leaderboard/selfhost#records -->
### Leaderboard records

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `<dir>/<board>/<period>.jsonl`, a line per new best score | none | `selfhost.FileStore` | [leaderboard/selfhost](leaderboard-selfhost.md) |

```json
{"account":"3f9a...","score":4200,"achieved":"2026-11-24T09:15:02.5Z"}
```

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
| A JSON sidecar beside a model, or the `extras` of a glTF animation | `version: 1` in a sidecar | `anim3d.LoadEventsSidecar`, `anim3d.MarshalEventsSidecar`, `anim3d.LoadEventsFromGLTFExtras` | [anim3d](anim3d.md), [3D Models from Blender](3d-models-blender.md) |

<!-- format: assets -->
### DLC pack manifest

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A `.json` file the game downloads, listing the packs it can install | none; each pack has its own `version` | `assets.ParsePackManifest` | [assets](assets.md) |

```json
{"packs": [{"id": "forest", "url": "https://cdn.example.com/forest.zip", "version": "1.0.0", "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}]}
```

<!-- format: cmd/sizecheck -->
### Package size budget

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| A `.json` file passed to `sizecheck -budget` | none | the `sizecheck` command (`cmd/sizecheck`) | [Package Size Budgets](package-size-budgets.md) |

```json
{
  "max_total_bytes": 52428800,
  "max_per_abi_bytes": 20971520,
  "note": "Release budget for MyGame Android AAB"
}
```

<!-- format: tooling/apiref -->
### API reference

| Files | Version | Read and written by | Fields |
| --- | --- | --- | --- |
| `api.json`, published at `https://game.nsgo.dev/api.json` | `version: 1` | `apiref.Marshal`, `apiref.Unmarshal` | [Packages](packages.md) |

## Other tools' formats

These are read as the tools that make them write them, never converted into a form of this repository's own.

<!-- format: tilemap -->
### Tiled map

| Files | Read by | Guide |
| --- | --- | --- |
| Tiled JSON maps (`.tmj`) with embedded or external (`.tsj`) tilesets | `tilemap.LoadTiled`; `rooms.FromTiledLayer` makes rooms from an object layer | [tilemap](tilemap.md), [rooms](rooms.md) |

<!-- format: tilemap#ldtk -->
### LDtk project

| Files | Read by | Guide |
| --- | --- | --- |
| LDtk projects (`.ldtk`) | `tilemap.LoadLDtk`; `rooms.FromLDtk` makes rooms from a world's levels | [tilemap](tilemap.md), [rooms](rooms.md) |

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

<!-- format: debug#trace -->
### Profiler trace

| Files | Written by | Guide |
| --- | --- | --- |
| Chrome's trace event JSON, opened in Perfetto or `chrome://tracing` | `debug.Profiler.WriteChromeTrace` | `debug.Profiler` |

## Binary files

Media that is binary by nature stays binary: images (PNG), sound (WAV, AAC, MP3), font pages, glTF buffers and `.glb` files, and KTX2 textures that `game assets` encodes. So do the packets of `net`, which are data on the wire, and a fighting recognizer's rollback state, which lives in memory. Every one of them is described by a text file above where it needs settings: an import settings sidecar, `game.yaml` or a glTF document.

## Keeping this page current

Each YAML format's conformance test (`internal/formattest`) names the format's entry here by its line `<!-- format: <package>[#<name>] -->` and checks that every YAML example in the entry, on this page and its translation, loads and saves to the same bytes. `TestFormatsPage` in `packages/game` fails when a package defines a format this page does not list, when a package that decodes JSON has no entry here, or when the two languages list different entries. A new format adds its entry here in the same change.
