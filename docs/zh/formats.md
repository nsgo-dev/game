# 文件格式

用这些包制作的游戏读写的每一种文件：它的文件名、它声明的版本、读写它的 Go 函数、说明其字段的指南，以及一个示例，让人或 AI 不读代码也能编辑它。

除了下文“JSON 文件”和“其他工具的格式”中列出的文件，游戏作者编写的文件都是 YAML。本页的每个示例都由测试检查：YAML 示例是其格式的规范形式，即其编码器写出的字节，加载再保存后不变，所以没有一个示例含有格式会拒绝的键或会被改写的排版；JSON 和 Yarn 示例能被该格式的读取函数加载。

## 每种 YAML 格式的共同规则

- **版本。** 文档顶层的 `version` 键是一个整数，表示文档采用该格式的哪个版本；模组清单中这个键叫 `manifestVersion`。缺少它、写成 `version: 0` 或版本比代码能读的更新的文件都会被拒绝。
- **严格的键。** 格式未定义的键会被拒绝，文件中的第二个 YAML 文档和重复给出的键也一样，错误会指出文件、行号和字段并说明如何修复（[错误](errors.md)）。
- **唯一的规范形式。** 保存时按固定顺序写出键，对本身没有顺序的内容排序，并且不写时间戳或随机 ID，因此同一个值总是得到相同的字节，规范形式的文件加载再保存后不会改变。手写文件第一次保存时会丢掉注释并把排版改写成规范形式；此后每次保存只改动真正变化的部分。
- **Schema。** 场景文件和导入设置旁注文件有已发布的 JSON Schema，数据表的 Schema 由其行类型生成，因此编辑器在编写时就能补全和检查它们（[YAML 文件的 Schema](schemas.md)）。

## 项目与资源

<!-- format: project -->
### 项目文件

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 项目根目录的 `game.yaml` | `version: 1` | `project.Parse`、`project.Encode` | [项目文件](project.md)；Schema 为 `https://game.nsgo.dev/schemas/game.v1.json` |

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
### 导入设置旁注文件

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 图片、声音、字体或模型旁边的 `<资源>.import.yaml`，如 `hero.png.import.yaml` | `version: 1` | `importsettings.Parse`、`importsettings.Encode` | [命令行](cli.md#assets)；Schema 为 `https://game.nsgo.dev/schemas/import.v1.json` |

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
### 项目索引

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `game.yaml` 旁边的 `game.index.yaml`，由 `game index`、`game mv` 和编辑器维护 | `version: 1` | `refs.ParseIndex`、`refs.EncodeIndex` | [稳定引用](project.md#稳定引用) |

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
### 资源库包清单

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 资源包或插件包根目录的 `pack.yaml` | `version: 1` | `library.ParseManifest`、`library.EncodeManifest` | [资源与插件库](library.md) |

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
### 资源库索引

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 包库根目录的 `index.yaml` | `version: 1` | `library.ParseIndex`、`library.EncodeIndex` | [资源与插件库](library.md) |

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
### 本地化资源清单

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `assets.yaml` | `version: 1` | `l10n.ParseAssetManifest`、`l10n.EncodeAssetManifest` | [l10n](l10n.md) |

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
### 本地化字符串表（YAML）

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 每种语言一个 `.yaml` 文件，如 `locales/en.yaml` | `version: 1` | `l10n.ParseYAML`、`l10n.EncodeYAML`、`l10n.Catalog.LoadYAML` | [l10n](l10n.md) |

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
### 模组清单

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 模组目录根部的 `mod.yaml` | `manifestVersion: 1` | `modding.ParseManifest`、`modding.EncodeManifest` | [modding](modding.md) |

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

<!-- format: modding#workshop -->
### Steam 创意工坊元数据

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 发布到 Steam 创意工坊的模组目录中、`mod.yaml` 旁的 `workshop.yaml` | `version: 1` | `modding.ParseWorkshopMetadata`、`modding.EncodeWorkshopMetadata` | [modding](modding.md) |

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

<!-- format: tooling/hotreload -->
### 3D 着色器材质

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.material.yaml`，在 `game dev` 中热重载 | `version: 1` | `hotreload.ParseMaterial`、`hotreload.EncodeMaterial` | [render3d](render3d.md) |

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
### 体素世界

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.voxel.yaml` | `version: 1` | `voxel.Parse`、`voxel.Encode` | [voxel](voxel.md)；Schema 为 `https://game.nsgo.dev/schemas/voxel.v1.json` |

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
### 3D 网格地图

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.gridmap.yaml` | `version: 1` | `world3d.ParseGridMap`、`world3d.EncodeGridMap`、`world3d.LoadGridMapFS` | [world3d](world3d.md)；Schema 为 `https://game.nsgo.dev/schemas/gridmap.v1.json` |

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
### 地形定义

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.terrain.yaml` | `version: 1` | `world3d.ParseTerrain`、`world3d.EncodeTerrain`、`world3d.LoadTerrainFS` | [world3d](world3d.md)；Schema 为 `https://game.nsgo.dev/schemas/terrain.v1.json` |

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
### 3D 植被层

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.foliage.yaml` | `version: 1` | `world3d.ParseFoliage`、`world3d.EncodeFoliage`、`world3d.LoadFoliageFS` | [world3d](world3d.md)；Schema 为 `https://game.nsgo.dev/schemas/foliage.v1.json` |

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

## 场景、资源与动画

<!-- format: sceneformat -->
### 场景

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.scene.yaml`，如 `scenes/cave_room.scene.yaml` | `version: 1`；实例化或继承其他场景文件的场景为 `version: 2` | `sceneformat.Parse`、`sceneformat.Encode` | [编辑器与场景格式](editor.md)；Schema 为 `https://game.nsgo.dev/schemas/scene.v1.json` 和 `scene.v2.json` |

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

<!-- format: navbake3d -->
### 3D 导航网格

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.navmesh.yaml`，如 `levels/level1.navmesh.yaml`，由场景的 `navMesh` 引用 | `version: 1` | `navbake3d.Parse`、`navbake3d.Encode`；由 `game navmesh` 烘焙 | [navbake3d](navbake3d.md) |

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

<!-- format: resource -->
### 共享资源

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `<名称>.<类型>.yaml`，如 `stone.material.yaml`；`type` 为 `material`、`curve`、`gradient`、`animation`、`theme`、`busLayout` 或 `physicsMaterial` | `version: 1` | `resource.Parse`、`resource.Encode` | [resource](resources.md) |

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
### 粒子发射器

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `sparks.yaml` | `version: 1` | `particles.UnmarshalConfigYAML`、`particles.MarshalConfigYAML`；相同字段的 JSON 形式用 `UnmarshalConfigJSON` 和 `MarshalConfigJSON` | [particles](particles.md) |

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
### 骨骼绑定

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.skel.yaml` | `version: 1` | `skeletal.Load`、`skeletal.Encode` | [skeletal](skeletal.md) |

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
### 过场动画

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `cutscenes/intro.yaml` | `version: 1` | `cutscene.Parse`、`cutscene.Encode` | [cutscene](cutscene.md) |

```yaml
version: 1
source: asset:cutscenes/intro.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
```

<!-- format: rooms -->
### 房间地图

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `castle.rooms.yaml` | `version: 1` | `rooms.Parse`、`rooms.Encode` | [rooms](rooms.md) |

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
### 世界地图

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `castle.yaml` | `version: 1` | `worldmap.Parse`、`worldmap.Encode` | [worldmap](worldmap.md) |

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
### 卡牌桌面

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `board.yaml` | `version: 1` | `cards.ParseBoardDoc`、`cards.EncodeBoardDoc` | [cards](cards.md) |

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

## 游戏数据

<!-- format: tooling/datatable -->
### 数据表

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `.yaml` 或 `.yml`，如 `weapons.yaml`；也能加载 `.csv` 和 `.json` 导出文件，它们没有版本 | `version: 1` | `datatable.LoadYAML`、`datatable.MarshalYAML`；`datatable.LoadFile` 按扩展名选择读取方式 | [datatable](datatable.md)；Schema 用 `datatable.Schema` 按行类型生成 |

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
### 属性

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `hero.yaml` | `version: 1` | `stats.Parse`、`stats.Encode` | [stats](stats.md) |

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
### 技能集

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `hero.abilities.yaml` | `version: 1` | `ability.Parse`、`ability.Encode` | [ability](ability.md) |

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
### 物品目录

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `items.yaml` | `version: 1` | `inventory.Parse`、`inventory.Catalog.Encode` | [inventory](inventory.md) |

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
### 任务书

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `village.yaml` | `version: 1` | `quest.Parse`、`quest.Encode` | [quest](quest.md) |

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
### 掉落表

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `chest.yaml` | `version: 1` | `loot.Parse`、`loot.Encode` | [loot](loot.md) |

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
### 成就

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏嵌入的任意 `.yaml` 文件，如 `achievements.yaml` | `version: 1` | `achievements.Parse`、`achievements.Encode` | [achievements](achievements.md) |

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
### 排行榜

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏嵌入的任意 `.yaml` 文件，如 `leaderboards.yaml` | `version: 1` | `leaderboard.Parse`、`leaderboard.Catalog.Encode` | [leaderboard](leaderboard.md) |

```yaml
version: 1
leaderboards:
  - name: high-score
    gameCenter: dev.nsgo.game.example.high_score
    playGames: CgkI6v7z8vQREAIQAQ
```

<!-- format: fighting -->
### 招式集

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `moves.yaml` | `version: 1` | `fighting.Parse`、`fighting.Encode` | [fighting](fighting.md) |

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
### 帧数据

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `fighter.yaml` | `version: 1` | `framedata.Parse`、`framedata.Encode` | [framedata](framedata.md) |

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
### 平台跳跃控制器

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `hero.yaml` | `version: 1` | `platformer.ParseConfig`、`platformer.EncodeConfig` | [platformer](platformer.md) |

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
### 载具

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `car.yaml` | `version: 1` | `vehicle.ParseConfig`、`vehicle.EncodeConfig` | [vehicle](vehicle.md) |

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
### 弹幕模式

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `patterns.yaml` | `version: 1` | `bullets.Parse`、`bullets.Encode` | [bullets](bullets.md) |

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
### 阵型

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `formations.yaml` | `version: 1` | `rts.ParseFormations`、`rts.Formations.Encode` | [rts](rts.md) |

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
### 效用 AI

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `guard.utility.yaml` | `version: 1` | `ai.ParseUtility`、`ai.EncodeUtility` | [ai](ai.md#效用-ai) |

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
### GOAP 动作集

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `camp.goap.yaml` | `version: 1` | `ai.ParseGOAP`、`ai.EncodeGOAP` | [ai](ai.md#目标导向行动规划goap) |

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
### 行为树

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.bt.yaml`，如 `guard.bt.yaml` | `version: 1` | `ai.ParseBehaviorTree`、`ai.EncodeBehaviorTree`、`ai.LoadBehaviorTreeFS`、`ai.BuildBehaviorTree` | [ai](ai.md#yaml-行为树) · [schema](schemas.md) |

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

<!-- format: navbake -->
### 导航网格

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `crypt.navmesh.yaml`，通常每个关卡、每种代理一个 | `version: 1` | `navbake.Parse`、`navbake.Mesh.Encode` | [navbake](navbake.md) |

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
### 图块规则

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `ruins.yaml` | `version: 1` | `procgen.ParseRules`、`procgen.EncodeRules` | [procgen](procgen.md) |

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
### 地牢房间模板

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `rooms.yaml` | `version: 1` | `procgen.ParseRoomTemplates`、`procgen.EncodeRoomTemplates` | [procgen](procgen.md) |

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

<!-- format: vn -->
### 视觉小说角色表

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `cast.yaml` | `version: 1` | `vn.ParseCast`、`vn.EncodeCast` | [vn](vn.md) |

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
### 节拍图

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 曲目旁边的 `.yaml` 文件，如 `opening.beatmap.yaml` | `version: 1` | `rhythm.ParseBeatMap`、`rhythm.EncodeBeatMap` | [rhythm](rhythm.md) |

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
### 数字区域设置

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `de.yaml` | `version: 1` | `idle.ParseLocale`、`idle.EncodeLocale` | [idle](idle.md) |

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
### 画质预设

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏加载的任意 `.yaml` 文件，如 `quality.yaml` | `version: 1` | `quality.ParseConfig`、`quality.EncodeConfig` | [quality](quality.md) |

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

## 服务器

<!-- format: accounts -->
### 账号服务

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `accounts.yaml`，传给 `accountsd -config` | `version: 1` | `accounts.ParseConfig`、`accounts.Config.Marshal` | [accounts](accounts.md) |

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
### 令牌密钥集

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `keys.yaml`，由 `accountsd genkey` 写出 | `version: 1` | `token.ParseKeySet`、`token.KeySet.Marshal` | [accounts](accounts.md) |

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
keys:
  - id: 2317febd373fcbcd
    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
```

<!-- format: cloudsave -->
### 云存档服务

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `cloudsave.yaml`，传给 `cloudsaved -config` | `version: 1` | `cloudsave.ParseConfig`、`cloudsave.Config.Marshal` | [cloudsave](cloudsave.md) |

```yaml
version: 1
maxSlotBytes: 1048576
maxSlots: 32
keepVersions: 5
allowedOrigins:
  - https://play.example.com
```

<!-- format: leaderboard/selfhost -->
### 排行榜服务

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `leaderboards.yaml`，传给 `leaderboardd -config` | `version: 1` | `selfhost.ParseConfig`、`selfhost.Config.Marshal` | [leaderboard/selfhost](leaderboard-selfhost.md) |

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

## 程序写出的 YAML 文件

这些文件由程序写给自己用，但出于同样的理由也是 YAML：人能读懂它、在评审中看它的 diff，必要时还能编辑它。

<!-- format: tooling/assetbuild -->
### 资源构建缓存

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `.game/assets-cache.yaml`，或 `game.yaml` 中 `assets.cache` 指定的路径 | `version: 1` | `game assets` | [命令行](cli.md#assets) |

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
### 基准测试基线

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `benchmarks/baseline.yaml`，由 `game bench -update` 写出 | `version: 1` | `bench.Decode`、`bench.Baseline.Encode` | [命令行](cli.md) |

```yaml
version: 1
note: Update with go tool game bench -update.
max_regression_pct: {ns_per_op: 25, bytes_per_op: 30, allocs_per_op: 12.5}
benchmarks:
  BenchmarkBatchDraw1000: {ns_per_op: 3500000, bytes_per_op: 80128, allocs_per_op: 1000}
  BenchmarkRaycast: {ns_per_op: 420.5, bytes_per_op: 0, allocs_per_op: 0}
```

<!-- format: achievements#progress -->
### 成就进度

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `core.Storage` 中的一个键，由 `achievements.Tracker.Persist` 维护 | `version: 1` | `achievements.ParseProgress`、`achievements.EncodeProgress` | [achievements](achievements.md) |

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
### 延迟校准

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `core.Storage` 中的一个键，由 `rhythm.OffsetStore` 维护 | `version: 1` | `rhythm.ParseCalibrations`、`rhythm.EncodeCalibrations` | [rhythm](rhythm.md) |

```yaml
version: 1
devices:
  android/Google Pixel 8:
    audioMs: 41.25
    videoMs: -3
```

<!-- format: replay -->
### 输入录制

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.replay.yaml`，由 `replay.Recorder` 写出 | `version: 2` | `replay.Decode`、`replay.Recording.Encode` | [replay](replay.md) |

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
### 幽灵圈

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.ghost.yaml`，由 `vehicle.GhostRecorder` 的录制结果写出 | `version: 1` | `vehicle.ParseGhost`、`vehicle.EncodeGhost` | [vehicle](vehicle.md) |

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
### 撤销日志

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏存放它的任何地方，如存档槽；其中的操作是游戏自己的操作类型 | `version: 1` | `undo.DecodeLog`、`undo.EncodeLog` | [undo](undo.md) |

```yaml
version: 1
applied: 2
moves:
  - dir: right
  - dir: down
  - dir: down
```

<!-- format: statedump -->
### 状态转储

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `*.state.yaml`，由 `statedump.WriteFile` 写出 | `version: 1` | `statedump.Parse`、`statedump.Encode` | [statedump](statedump.md) |

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

<!-- format: tilemap#chunk -->
### 瓦片地图分块

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `chunk_<x>_<y>.yaml`（或 `.json`），由 `tilemap.DirChunkStore` 及 `tilemap.EncodeChunkYAML` / `EncodeChunkJSON` 写出 | `version: 1` | `tilemap.DecodeChunkYAML`、`tilemap.EncodeChunkYAML`、`tilemap.DecodeChunkJSON`、`tilemap.EncodeChunkJSON` | [tilemap](tilemap.md#稀疏分块图层与流式加载) |

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

## JSON 文件

这些格式是 JSON：大多由程序写给自己用，其余是与其他服务和工具交换的数据。各自的指南给出了它们的字段。

<!-- format: save -->
### 存档槽

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 目录中的 `<槽>.json` 和 `<槽>.json.bak`，或 `core.Storage` 中的一个键 | 外层封装的 `version`，即游戏自己的数据结构版本，由其迁移函数逐级升级 | `save.Store.Save`、`save.Store.Load` | [save](save.md) |

```json
{"version":2,"data":{"level":4,"coins":120,"name":"Ann"}}
```

`data` 存放游戏自己的值，即 `encoding/json` 写出的内容，例如一个包含 `quest.Snapshot` 和背包快照的结构体。

<!-- format: save#settings -->
### 玩家设置

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 存档后端中的 `settings` 槽（如 `settings.json`）或 `core.Storage` 中的一个键，与存档槽分开 | 外层封装的 `version`，由设置存储的迁移函数逐级升级 | `save.SettingsStore` | `save.Settings`：`audio`、`bindings`、`display`、`locale`、`accessibility` 和 `custom`，或游戏自己的类型 |

```json
{"version":1,"data":{"audio":{"master":1,"groups":{"music":1,"sfx":1,"ui":1},"muted":false},"bindings":{"jump":["Space"]},"display":{"fullscreen":false,"display_index":0,"scale_mode":"fit","integer_scale":false,"frame_cap":0,"vsync":true,"auto_drop_low_power":true,"quality":"auto"},"locale":"en","accessibility":{"captions":false,"caption_scale":1,"color_blind_mode":"none","reduce_motion":false,"high_contrast":false,"screen_shake":1,"game_speed":1}}}
```

<!-- format: worldmap#state -->
### 世界地图探索状态

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 存档槽内 | `version: 1` | `worldmap.State` 的 JSON 形式 | [worldmap](worldmap.md) |

```json
{"version":1,"current":"hall","visited":["cave","hall"],"revealed":["crypt"],"markers":[{"id":"m1","kind":"pin","room":"hall","x":12,"y":4,"label":"Key"}]}
```

<!-- format: quest#snapshot -->
### 任务进度

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 存档槽内，JSON 或 YAML 形式 | `version: 1` | `quest.Log.Snapshot`、`quest.Log.Restore` | [quest](quest.md) |

```json
{"version":1,"quests":[{"id":"find-key","state":"active","progress":{"rats":3}},{"id":"intro","state":"completed"}]}
```

`state` 为 `active`、`completed`、`failed` 或 `abandoned`；尚未开始的任务不写出，没有进度的目标也不写出。

<!-- format: fog -->
### 战争迷雾

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 存档槽内 | `version: 1` | `fog.Fog` 的 JSON 形式 | [fog](fog.md) |

```json
{"version":1,"width":3,"height":2,"tileWidth":8,"tileHeight":8,"opaque":[".#.","..."],"nextSource":2,"sources":[{"id":1,"team":2,"col":2,"row":1,"radius":1},{"id":2,"team":1,"col":0,"row":0,"radius":0}],"teams":[{"team":1,"explored":["#..","..."]},{"team":2,"explored":[".##",".##"]}]}
```

<!-- format: entities -->
### 世界快照

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 存档槽内；只包含用 `RegisterCodec` 注册过的组件 | 无 | `entities.World.Snapshot`、`entities.World.Restore` | [entities](entities.md) |

```json
{"entities":[{"index":0,"generation":1,"components":[{"type":"health","data":{"hp":10}},{"type":"position","data":{"x":3,"y":4}}]},{"index":1,"generation":1,"components":[{"type":"position","data":{"x":-1,"y":0.5}}]}]}
```

`type` 是组件的编解码器用 `RegisterCodec` 注册时的名称，`data` 是 `encoding/json` 写出的组件。

<!-- format: input -->
### 输入绑定

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `core.Storage` 中的一个键 | `version: 1` | `input.Actions.Save`、`input.Actions.Load` | [input](input.md) |

```json
{"version":1,"actions":{"fire":[{"kind":"mouse_button","button":"left"}],"jump":[{"kind":"key","key":"space"},{"kind":"gamepad_button","button":"south"}]},"axes":{"move_x":{"negative":"left","positive":"right"}}}
```

<!-- format: debug -->
### 控制台可调参数

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `core.Storage` 中的一个键，默认为 `debug.console.tunables` | 无 | `debug.Console.SaveTunables`、`debug.Console.LoadTunables` | 以各参数名为键、参数值为值的对象 |

```json
{"cheat.god":false,"level.name":"cave_1","player.lives":3,"player.speed":1.5}
```

参数名是游戏用 `RegisterFloat`、`RegisterInt`、`RegisterBool` 和 `RegisterString` 注册的名称；`LoadTunables` 跳过没有注册的名称，拒绝超出范围或 JSON 类型不符的值，且只要拒绝其中一个值，就一个值也不应用。

<!-- format: tooling/devstate -->
### 开发会话快照

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 一个文件或 `core.Storage` 中的一个键，游戏重新构建后从中恢复状态 | 状态 Go 类型的哈希，类型变化时跳过恢复 | `devstate.Store.Save`、`devstate.Store.LoadSnapshot` | `devstate.SessionSnapshot` |

```json
{"scene_id":"level-1","schema_hash":"90b889efbda2b1fbe5fd7122b8919ac57b14cef258a4a60664a39e4ee3caee5b","loop_tick":1200,"loop_elapsed":20000000000,"payload":"eyJsZXZlbCI6Mn0=","saved_at":"2026-11-24T09:15:02Z"}
```

`payload` 是 `encoding/json` 写出的游戏状态，以 base64 编码；`loop_elapsed` 的单位是纳秒。

<!-- format: liveops -->
### 云存档载荷

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 平台云存储中的 `<前缀><槽>.json`，如 `save.main.json` | `version: 1` | `liveops.EncodeCloudSave`、`liveops.DecodeCloudSave` | [liveops](liveops.md) |

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

`hash` 是存档字节的 SHA-256；存档字节是 UTF-8 时以文本存放在 `data` 中，否则以 base64 存放在 `data_base64` 中。

<!-- format: liveops#sync -->
### 云同步状态

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `core.Storage` 中每个槽一个键 `<StateKey>.<槽>`，如 `liveops.cloudsync.main` | `version: 1` | `liveops.CloudSyncer`，仅供其自身使用 | [liveops](liveops.md) |

```json
{"version":1,"revision":3,"hash":"sha256:b30e459ab9ad862b330d89d42e3422a3e3193071345b1de6b9e4038b27f2ee01","local_modified":"2026-11-24T09:15:02Z","local_hash":"sha256:b30e459ab9ad862b330d89d42e3422a3e3193071345b1de6b9e4038b27f2ee01"}
```

它记录上次同步后两端共有的云端修订号和内容哈希，以及本设备上次写入本地副本的时间。

<!-- format: leaderboard/selfhost#records -->
### 排行榜记录

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `<目录>/<榜单>/<周期>.jsonl`，每个新的最好成绩一行 | 无 | `selfhost.FileStore` | [leaderboard/selfhost](leaderboard-selfhost.md) |

```json
{"account":"3f9a...","score":4200,"achieved":"2026-11-24T09:15:02.5Z"}
```

<!-- format: l10n#catalog -->
### 字符串表

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 每种语言一个 `.json` 文件，可以是扁平、嵌套或带 CLDR 复数形式的 | 无 | `l10n.Catalog.LoadJSON` | [l10n](l10n.md) |

```json
{
	"menu": {"play": "Play"},
	"hud": {"score": "Score: {score}"},
	"coins": {"zero": "No coins", "one": "{count} coin", "other": "{count} coins"}
}
```

<!-- format: anim3d -->
### 片段事件

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 模型旁边的 JSON 旁注文件，或 glTF 动画的 `extras` | 旁注文件中为 `version: 1` | `anim3d.LoadEventsSidecar`、`anim3d.MarshalEventsSidecar`、`anim3d.LoadEventsFromGLTFExtras` | [anim3d](anim3d.md)、[从 Blender 导入 3D 模型](3d-models-blender.md) |

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
### DLC 包清单

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 游戏下载的 `.json` 文件，列出可以安装的包 | 无；每个包有自己的 `version` | `assets.ParsePackManifest` | [assets](assets.md) |

```json
{"packs": [{"id": "forest", "url": "https://cdn.example.com/forest.zip", "version": "1.0.0", "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}]}
```

<!-- format: cmd/sizecheck -->
### WebAssembly 体积预算

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 检查 `.wasm` 时传给 `sizecheck -budget` 的 `.json` 文件，如 `benchmarks/web-budget.json` | 无 | `sizecheck` 命令（`cmd/sizecheck`） | [包体积预算](package-size-budgets.md) |

```json
{
  "target": "./cmd/wasmprobe",
  "ldflags": "-s -w",
  "max_raw_bytes": 8388608,
  "max_gzip_bytes": 2359296,
  "note": "WebAssembly size budget for ./cmd/wasmprobe"
}
```

<!-- format: cmd/sizecheck#mobile -->
### 移动端包体积预算

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| 检查 `.aab`、`.apk` 或 `.ipa` 时传给 `sizecheck -budget` 的 `.json` 文件，如 `benchmarks/android-aab-budget.json` | 无 | `sizecheck` 命令（`cmd/sizecheck`） | [包体积预算](package-size-budgets.md) |

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

<!-- format: tooling/apiref -->
### API 参考

| 文件 | 版本 | 读写函数 | 字段 |
| --- | --- | --- | --- |
| `api.json`，发布在 `https://game.nsgo.dev/api.json` | `version: 1` | `apiref.Marshal`、`apiref.Unmarshal` | [包](packages.md) |

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

## 其他工具的格式

这些文件按生成它们的工具所写的样子读取，从不转换成本仓库自己的形式。

<!-- format: tilemap -->
### Tiled 地图

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| Tiled XML（`.tmx`）与 JSON（`.tmj`）地图，图块集可内嵌或外置（`.tsx`、`.tsj`），瓦片数据支持 CSV、XML 或 base64（未压缩、`zlib`、`gzip`） | `tilemap.LoadTiled`；`rooms.FromTiledLayer` 从对象层生成房间 | [tilemap](tilemap.md)、[rooms](rooms.md) |

<!-- format: tilemap#world -->
### Tiled 世界文件

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| Tiled 世界文件（`.world`），列出地图世界坐标或带倍率与偏移的正则文件名模式 | `tilemap.ParseTiledWorld`、`tilemap.LoadTiledWorld` | [tilemap](tilemap.md) |

<!-- format: tilemap#ldtk -->
### LDtk 项目

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| LDtk 项目（`.ldtk`） | `tilemap.LoadLDtk`、`tilemap.LoadLDtkAll`、`tilemap.LoadLDtkWorld`；`rooms.FromLDtk` 从一个世界的关卡生成房间 | [tilemap](tilemap.md)、[rooms](rooms.md) |

<!-- format: sprite -->
### 精灵图集

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| TexturePacker 的“JSON (Hash)”和“JSON (Array)”，Aseprite 的 `json-hash` 和 `json-array`；`game pack` 写出 TexturePacker 的 JSON (Hash) | `sprite.LoadAtlas` | [sprite](sprite.md) |

<!-- format: text -->
### 字体

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| 文本、XML 或 JSON 形式的 AngelCode BMFont；有向距离场字体用 msdf-atlas-gen 的 JSON | `text.LoadBMFont`、`text.LoadSDFFont` | [text](text.md) |

<!-- format: gltf -->
### glTF 模型

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| glTF 2.0（`.gltf` 及其缓冲区文件，或 `.glb`），包括 Draco 和 meshopt 压缩 | `gltf.Load`、`gltf.LoadFS` | [从 Blender 导入 3D 模型](3d-models-blender.md) |

<!-- format: world3d -->
### 网格地图网格库

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| glTF 2.0 JSON 文档，其中带网格的节点成为库中的图块，包围盒取自 `POSITION` 访问器 | `world3d.LoadMeshLibraryFromGLTF` | [world3d](world3d.md) |

<!-- format: debug#trace -->
### 性能分析器跟踪

| 文件 | 写出函数 | 指南 |
| --- | --- | --- |
| Chrome 的跟踪事件 JSON，可在 Perfetto 或 `chrome://tracing` 中打开 | `debug.Profiler.WriteChromeTrace` | `debug.Profiler` |

<!-- format: flow -->
### Yarn 对话脚本

| 文件 | 读取函数 | 指南 |
| --- | --- | --- |
| 手写的 Yarn Spinner 脚本（`.yarn`）：由台词、`->` 选项和 `<<命令>>` 组成的节点，每个节点以 `title:` 头开始，位于 `---` 和 `===` 之间 | `flow.ParseYarn`；由 `vn.Presenter` 播放 | [vn](vn.md) |

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

## 二进制文件

本身就是二进制的媒体仍保持二进制：图片（PNG）、声音（WAV、AAC、MP3）、字体页、glTF 缓冲区和 `.glb` 文件，以及 `game assets` 编码的 KTX2 纹理。`net` 的网络包（传输中的数据）和格斗输入识别器的回滚状态（只存在于内存）也是二进制。它们需要设置时，都由上面的某个文本文件描述：导入设置旁注文件、`game.yaml` 或 glTF 文档。

## 保持本页与代码一致

每个条目以一行 `<!-- format: <包>[#<名称>] -->` 开始。每种 YAML 格式的一致性测试（`internal/formattest`）以此指明它的条目，并检查其中的每个 YAML 示例（本页和英文页）加载再保存后字节不变。其余由 `packages/game` 中的 `TestFormatsPage` 检查：两种语言列出相同的条目且顺序相同；其他每个条目都在该测试中登记了读取其格式的声明，该包必须仍有这个声明，且条目中的 JSON 和 Yarn 示例能被该格式的读取函数加载；每个解码 JSON 的包都有条目，除非测试说明了它为何不需要；每个名称以 `Version` 结尾的导出整数常量都是本页所列某种格式的版本。这些检查都发现不了的格式仍可能漏掉，例如某个已有条目的包中一种没有版本的文本格式，所以新增格式时在同一次改动中把条目加到本页。
