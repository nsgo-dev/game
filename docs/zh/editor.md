# 编辑器与场景格式

本文档记录 NSGo 游戏编辑器的范围决策（[#159](https://github.com/nsgo-dev/game/issues/159)），并说明 `github.com/nsgo-dev/game/packages/game/sceneformat` 实现的第 1 版 YAML 场景格式（[#160](https://github.com/nsgo-dev/game/issues/160)）。

## 编辑器范围决策（#159）

NSGo 采用**方案（b）并向（c）演进**：保持 Go 代码与 Tiled 的一等公民地位，定义一套清晰、适合人手编写的 YAML 场景格式供手写场景与工具共用，并在此基础上逐步构建一个轻量级 NSGo 原生场景编辑器。

### 为什么选择这一范围

- **代码与 Tiled 仍是一等公民。** 纯 Go 编写的游戏，或使用 Tiled `.tmj` 地图与 TexturePacker/Aseprite 图集的项目，永远不需要被迫经过编辑器。`sceneformat` 只是一个普通库包，没有任何运行时绑定。
- **单一文本格式，易于 Diff。** 场景使用带版本号的纯文本 YAML（`version: 1`），配合确定性的 2 空格缩进编码。手写修改与可视化编辑器保存都会产生清晰、易于版本控制的文本 diff。
- **用 NSGo 自己造工具（Dogfooding）。** 轻量编辑器不包装第三方桌面壳，也不追求庞大的一体化 IDE，而是直接用 NSGo 自己的 `ui` 视图与 `Canvas` 构建。属性面板、树形列表、快捷键与画布工具的每一项打磨都会反哺 NSGo 本身。

### 轻量级编辑器的规划能力

NSGo 原生编辑器直接读写 `sceneformat.SceneDoc` 文件，聚焦于可视化操作比手写坐标更高效的环节：

1. **对象摆放与变换**：在世界图层与屏幕图层中放置、拖拽、旋转、缩放、翻转和调整对象的 Z 顺序。
2. **瓦片绘制**：直接绘制内联瓦片网格，以及分层网格的各个瓦片图层（含翻转的瓦片），或预览引用的 Tiled `.tmj` 与 LDtk `.ldtk` 地图。
3. **带类型的属性与组件编辑**：查看并编辑 `components` 与 `properties`，并按场景规范实时校验。
4. **撤销 / 重做**：基于命令栈的场景文档编辑历史。
5. **编辑器内运行（Play-in-Editor）**：不离开工具，直接在编辑器的 `Canvas` 中运行真实的 `game.Loop` 与 `BuiltScene`。
6. **资源浏览**：浏览项目中的图集、帧、动画与瓦片地图，并拖放到图层中。

不在初期编辑器范围内：自定义脚本语言、专用着色器/材质连线图，以及私有二进制工程格式。

---

## 场景格式（`version: 4`）（#160）

```go
import "github.com/nsgo-dev/game/packages/game/sceneformat"
```

### 顶层字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `version` | `int` | 必填的格式版本号：`4`（`CurrentVersion`），未使用版本 4 3D 光源/环境/相机的版本 3 视差/重复场景为 `3`，仅使用版本 2 字段的场景为 `2`，未使用版本 2–4 新增字段的场景为 `1`。`Encode` 原样写回。 |
| `uid` | `string` | 可选的稳定 ID，文件改名或移动后保持不变，由 `nsgo game index` 写入（所有版本均可用，见[稳定引用](project.md#稳定引用)）。`Resolve` 保留场景自身的 uid，而不是基础场景的。 |
| `inherits` | `string` | 可选，从项目根目录起、以斜杠分隔的基础场景路径，本场景继承该场景（版本 2 及以上，见下文“实例化与继承”）。 |
| `name` | `string` | 可选的场景可读名称。 |
| `navMesh` | `string` | 可选，从项目根目录起、以斜杠分隔的场景烘焙[导航网格文件](navbake3d.md)路径，由 `nsgo game navmesh` 写入（所有版本均可用）。继承其他场景时，它会替换基础场景的值。 |
| `virtualWidth`、`virtualHeight` | `float32` | 虚拟视口尺寸（单位为点）。两者必须同时为 `0` 或同时为正数。 |
| `backgroundColor` | `string` | 可选的 CSS 颜色字符串，由 `core.ParseColor` 解析（如 `"#141824"`）。在绘制各图层前填充 `(0, 0, virtualWidth, virtualHeight)`。 |
| `ambientColor` | `string` | 可选的 CSS 颜色字符串，由 `core.ParseColor` 解析（如 `"rgba(12, 16, 30, 0.85)"`）。设置场景 2D 光照通道的环境暗场颜色（`lights2d.Scene.Ambient`，两个版本均可用）。 |
| `camera` | 对象 | 可选的默认 2D 相机配置，作用于世界空间图层（`x`、`y`、`zoom`、`rotation`，以及可选的 `bounds`）。 |
| `environment` | 对象 | 可选的 3D 场景环境（版本 4）：`sky`（`kind` 为 `color`、`gradient` 或 `hdr`，分别配合 `color`、`zenith`/`horizon`/`ground` 或 `hdr` 路径）、`ambientColor`、`ambientIntensity`、`iblIntensity`、`fog`（`mode` 为 `off`、`linear`、`exp`、`exp2`，含 `color`、`start`、`end`、`density`、`maxOpacity` 及高度雾 `heightEnabled`、`heightBase`、`heightFalloff`、`heightDensity`）、`tonemapping`（`none`、`reinhard`、`aces`、`neutral`、`agx`）与 `exposure`。 |
| `tilemaps` | 映射 | 可选的命名瓦片地图定义，供图层通过 `tilemap.ref` 引用。 |
| `layerOverrides` | 映射 | 按名称修改继承来的图层：`space`、`order`、`visible`、`tilemap`（版本 2 及以上），以及 `parallaxX`、`parallaxY`、`repeatX`、`repeatY`、`repeatWidth`、`repeatHeight`（版本 3，需要 `inherits`）。 |
| `overrides` | 映射 | 按 ID 修改继承来的对象（版本 2 及以上，需要 `inherits`）。 |
| `layers` | 列表 | 有序图层列表，按 `order` 从小到大绘制，`order` 相同时保持文档顺序。 |
| `connections` | 列表 | 可选，把一个对象的事件连接到另一个对象的处理函数，写在图层之后（所有版本均可用，见[连接](#连接490)）。 |

### 相机（Camera）

- `x`、`y`：对齐到虚拟视口中心 `(virtualWidth/2, virtualHeight/2)` 的世界坐标（虚拟尺寸为 `0` 时对齐到原点 `(0, 0)`）。
- `zoom`：正缩放倍数，省略时默认为 `1`。
- `rotation`：相机在屏幕上顺时针旋转的弧度；世界图层会绕 `(x, y)` 反向旋转 `-rotation`。
- `bounds`：可选的 `{x, y, width, height}` 世界边界矩形，用于限制可见范围（`width > 0`、`height > 0`）。
- 省略 `camera` 时，世界图层按单位变换绘制。

### 瓦片地图（Tilemaps）

瓦片地图（位于顶层 `tilemaps` 或内联在图层上）只能使用以下模式之一：

- **引用（`ref`，仅限图层）**：指向顶层 `tilemaps` 中的命名条目。
- **外部地图（`source`）**：从项目根目录起以斜杠分隔的路径，指向 Tiled 地图（`.tmj` 或 `.tmx`）或 LDtk 项目（`.ldtk`）；对 LDtk，`level` 指定要构建的关卡（版本 2），不写则构建项目的第一个关卡。
- **内联网格**：包含 `tileWidth`、`tileHeight`、`width`、`height`、`tileset`（帧名按从 1 开始的瓦片 ID `"1"`、`"2"`…… 索引的图集名）、可选的 `offsetX` / `offsetY`（版本 3，以图层点数偏移 `(0, 0)` 单元格），以及长度为 `width * height` 的行主序 `data`（`0` 表示空瓦片）。
- **分层网格**（版本 2）：包含 `tileWidth`、`tileHeight`、`width`、`height`、可选的 `offsetX` / `offsetY`（版本 3）、`tilesetFile`（YAML 图块集 `*.tileset.yaml` 的路径，见 [YAML 图块集](tilemap.md#yaml-图块集)，单元格放置的就是它的瓦片），以及 `tileLayers`：最多 `MaxTileLayers` 个按顺序绘制的命名瓦片图层。

分层网格的图块集只定义一次每个瓦片的标志、属性、碰撞形状、动画和地形，地图只负责放置瓦片，与 Godot 的 TileSet 之于 TileMapLayer 相同。每个瓦片图层有 `name`、可选的 `visible`（默认 `true`）和 `opacity`（默认 `1`），以及 `rows`：从上到下共 `height` 行，每行 `width` 个以空格分隔的单元格。单元格为空时写 `.`，否则写瓦片在图块集中的局部 ID（`0`、`1`……），后面可以跟 `h`、`v`、`d` 中的任意几个，分别表示水平、垂直和沿对角线翻转，与 Tiled 的翻转位相同，因此 `3hv` 是旋转了半圈的瓦片 3。一行可以用多余的空格对齐单元格，字母也可以任意顺序书写；`Encode` 统一写成单个空格、字母按 `h`、`v`、`d` 的顺序，于是每一行在 diff 中都是一行。

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Cave
virtualWidth: 320
virtualHeight: 180
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 4
      height: 2
      tilesetFile: tilesets/cave.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 0 0 0
            - . . 1h 3hvd
        - name: deco
          opacity: 0.5
          rows:
            - 2 . . .
            - . . . .
```

`TileLayerDoc.Cell`、`SetCell` 和 `Cells` 以瓦片值读写图层的单元格，即构建后它们的 GID：空单元格为 `0`，否则为局部 ID 加 1，并带上 `tilemap.FlipHorizontal`、`FlipVertical` 和 `FlipDiagonal` 翻转位；`ParseTileRow` 和 `FormatTileRow` 对单独一行做同样的事。

`Build` 把图集帧组成的单一网格变成精灵批次（`BuiltLayer.Tilemap`），把分层网格或外部地图变成 `tilemap.Tilemap`（`BuiltLayer.Map`）。这个地图带有图块集的标志、属性、形状、动画和地形，因此在图层坐标中可以回答 `FlagsAt`、`TileProperties`、`TilesInRect` 和 `Raycast`，也可以交给 `tileshape.New` 生成斜坡与遮挡体。`BuiltScene.Draw` 只绘制虚拟视口内的地图瓦片（世界坐标图层按相机看到的范围），除非场景没有虚拟尺寸或设置了 `DrawAllTiles`，编辑器的编辑视图就会设置它；`BuiltScene.Update(dt)` 推进动画瓦片。

`Build` 通过解析器的 `FileResolver`（`ReadFile`）读取 YAML 图块集、Tiled 地图及其外部图块集和 LDtk 项目，Tiled 或 LDtk 文件中的路径相对于该文件所在目录；图片在解析器实现了 `ImageResolver`（`Image`）时由它加载，否则按路径命名（`core.ImageSource{URI: path}`）。`FSAssets` 从 `fs.FS` 读取项目文件，因此引用 `.tmj`、`.ldtk` 或 `.tileset.yaml` 的场景无需游戏自己编写解析器即可构建。实现了 `TilemapResolver` 的解析器仍会像以前一样自行把每个 `source` 加载为 `TilemapDoc`，但指定了 LDtk `level` 的 source 除外：`Tilemap(source)` 无法接收关卡名，因此 `Build` 通过 `FileResolver` 读取它。

### 图层与对象

每个图层包含：

- `name`（必填，在场景内唯一）
- `space`：`"world"`（省略时的默认值，受相机变换影响）或 `"screen"`（直接在画布坐标中绘制，忽略相机）
- `order`：整数绘制顺序（越小越先绘制）
- `visible`：布尔值，省略时默认为 `true`
- `parallaxX`、`parallaxY`（版本 3）：可选的有限视差滚动系数，作用于 `"world"` 图层的相机位置，与 `scene.Layer` 一致；省略时默认为 `1`，`0` 使图层固定不随相机平移，`0.5` 以半速滚动，大于 `1` 用于前景层。继承场景可通过 `layerOverrides` 覆盖。
- `repeatX`、`repeatY`（版本 3）：可选布尔值，使 `"world"` 图层按 `repeatWidth` / `repeatHeight` 在相机可见区域内无缝平铺重复（非负图层点数；为 `0` 时依次回退到图层瓦片地图的像素尺寸或场景的虚拟视口尺寸）。继承场景可通过 `layerOverrides` 覆盖。
- `tilemap`：可选瓦片地图，绘制在本图层所有对象之下
- `objects`：放置在本图层中的对象列表

每个对象包含：

- `id`（必填，在整个场景内唯一）
- `name`、`prefab`（可选）
- 变换：`x`、`y`、`originX`、`originY`、`rotation`（弧度，顺时针）、`scaleX`（默认 `1`）、`scaleY`（默认 `1`）、`flipX`、`flipY`、`alpha`（范围 `[0, 1]`，默认 `1`）、`z`（图层内绘制顺序，越小越先绘制）
- `sprite`：可选的 `{atlas, frame}`，引用 `sprite.Atlas` 及其中的帧名
- `particles`：可选的 `{file, atlas}`（版本 2），在对象上放置一个 2D 粒子发射器 YAML 文件（`file` 为从项目根目录起以斜杠分隔的路径；可选 `atlas` 用于解析发射器的 `frameNames`）。`sceneformat.Build` 在解析器实现 `ParticlesResolver` 时由它加载 `file`，否则回退到 `FileResolver` + `particles.UnmarshalConfigYAML`（因此 `FSAssets` 开箱即用），创建 `BuiltObject.Emitter`（并汇总在 `BuiltLayer.Emitters` 中），按对象的复合世界变换定位（`BuiltObject.Sync` 会同步更新），在 `BuiltScene.Update(dt)` 中推进，并在 `BuiltScene.Draw(d)` 中按 Z 顺序与精灵交错绘制
- `material`：可选，从项目根目录起、以斜杠分隔的共享材质文件路径，例如 `materials/stone.material.yaml`，在构建时通过 `MaterialResolver` 解析（见 [resource](resources.md)）
- 3D 外观：`mesh` 是内置基本形体 `box`、`sphere`、`plane`、`capsule`、`cylinder` 之一；`model`（版本 2）是从项目根目录起、以斜杠分隔的 glTF 2.0 文件路径（`.gltf` 或 `.glb`），可以用 `modelNode` 缩小到其中一个节点，或用 `modelMesh` 缩小到其中一个网格；`gridMap`（版本 2）是从项目根目录起、以斜杠分隔的 `*.gridmap.yaml` / `*.gridmap.yml` 文件路径；对象的 `material` 会替换模型或网格地图的材质，或作为基本形体的表面；可选的 `occluder`（版本 2）将对象标记为 3D 遮挡体（`mode: model` 或 `mode: box`，可配 `center`、`size` 与 `doubleSided`）。`scenemodel.Build` 把它们变成几何体、LOD 与遮挡体（[scenemodel](scenemodel.md)）。`CheckObjectModel` 检查单个对象的这些字段，编辑器的 `PlaceObject` 会拒绝它拒绝的对象；检查器的 Mesh 和 GridMap 字段彼此互斥，且都与 `model` 互斥
- 3D 光源（`light`，版本 4）：附着在对象变换上的可选 3D 光源（`kind`：`directional`、`point` 或 `spot`；CSS `color`、`intensity`、`range`、以角度表示的聚光 `innerCone` 和 `outerCone` 半角，以及 `shadows`）。方向光和聚光沿对象局部 `-Z` 轴照射。`CheckObjectLight` 校验单个对象的光源。
- 3D 相机（`camera`，版本 4）：附着在对象变换上的可选 3D 相机（`projection`：`perspective` 或 `orthographic`、以角度表示的 `fov` 或 `orthoHeight`、`near`、`far` 以及 `current`）。同一场景中最多只能有一个 3D 相机设置 `current: true`。`CheckObjectCamera3D` 校验单个对象的相机。
- `components`：可选的映射，键是组件类型名，值是它的字段；未提供组件注册表时内容不受约束（见[组件类型](#组件类型)）
- `properties`：可选的 `map[string]any`，存放自定义元数据
- `scene`：可选，从项目根目录起、以斜杠分隔的场景文件路径，本对象实例化该场景（版本 2，见下文“实例化与继承”）
- `overrides`：可选，按 ID 修改被实例化场景中的对象（版本 2，需要 `scene`）
- `children`：可选的子对象列表，挂在本对象之下（见下文“节点树”）

### 节点树（#484）

对象可以在 `children` 下包含子对象，最多嵌套 `MaxObjectDepth`（64）层。子对象的变换相对于父对象：`x`、`y`、`rotation`、`scaleX`、`scaleY`（3D 中还有 `posZ`、`rotX`、`rotY`、`rotZ`、`scaleZ`）会与所有祖先的变换复合，`alpha` 会与祖先的相乘。`originX`、`originY`、`flipX`、`flipY` 只属于对象自身；`z` 在整个图层内排序（包括子对象），相同时父对象先于其子对象绘制。子对象属于父对象所在的图层，其 ID 与整个场景共用同一命名空间，因此树中任意位置用过的 ID 在其他任何位置都会被拒绝。

```yaml
layers:
  - name: ships
    objects:
      - id: ship
        x: 100
        y: 50
        rotation: 1.5707964
        scaleX: 2
        scaleY: 2
        sprite: { atlas: actors, frame: hull }
        children:
          - id: turret
            x: 10        # 落在 (100, 70)：随 ship 旋转四分之一圈并放大两倍
            sprite: { atlas: actors, frame: turret }
            children:
              - id: barrel
                x: 5     # 落在 (100, 80)
```

同一棵树可以用三种方式加载：

- `Build` 把每个 `BuiltObject` 与其 `Parent`、`Children` 关联起来，把 `World` 设为复合后的 2D 仿射变换、`WorldAlpha` 设为复合后的透明度，并按世界位置、旋转和缩放绘制每个子对象的精灵。复合变换带有切变（非等比缩放的父对象下有旋转的子对象）的精灵无法作为精灵绘制，`Validate` 会拒绝它，`Parse`、`Encode` 和 `Build` 因此也会拒绝；`CheckObjectTree` 对单棵树做同样的检查以及嵌套层数检查。父对象在某一轴上缩放为零（例如 `scaleX: 0`）不算切变：其子对象的精灵以零缩放绘制。没有精灵的对象则在 `World` 中保留精确矩阵。`BuiltLayer.Objects` 列出图层的顶层对象，`BuiltScene.Objects` 按 ID 索引所有对象。修改已构建对象的文档字段后，`BuiltObject.Sync` 会重新复合它及其后代的 `World`、`WorldAlpha` 和精灵，若修改会使精灵产生切变则返回 false。
- `SpawnEntities(doc, world)` 为每个对象在 `entities.World` 中生成一个实体，按对象的嵌套关系在其 `Hierarchy2D` 中设置父子关系，并写入对象的局部 `Transform2D` 和一个记录对象 ID 及所在图层的 `ObjectRef` 组件。隐藏图层的顶层对象生成时即为隐藏，因此其整棵子树在世界中不可见。
- `BuildNodes3D(doc, scene.Root())` 构建 `scene3d` 节点树：每个图层一个分组节点（带有图层的可见性），每个对象一个以其 ID 命名的节点，`UserData` 指向对应的 `ObjectDoc`。`ObjectLocalTransform3D` 给出每个节点的局部变换；`rotX`、`rotY`、`rotZ` 按此顺序绕固定轴旋转，与编辑器 3D 视口使用的顺序相同。
- `scenemodel.Build(doc, scene.Root(), assets)` 构建同样的节点树，并把每个对象 `mesh`、`model` 或 `gridMap` 的几何体和材质挂到它的节点上，用几何体设置节点的 `LocalBounds`，可直接交给 `render3d.Queue`（[scenemodel](scenemodel.md)）。

```go
spawned, err := sceneformat.SpawnEntities(doc, world)
if err != nil {
	return err
}
world.Hierarchy2D().Sync()
wt, _ := world.Hierarchy2D().WorldTransform(spawned["barrel"]) // wt.X == 100, wt.Y == 80

nodes, err := sceneformat.BuildNodes3D(doc, scene.Root())
if err != nil {
	return err
}
pos := nodes.Objects["barrel"].WorldTransform().Pos
```

编辑器在移动、删除和撤销对象时会连同其子对象一起处理，并且不会分配已被嵌套子对象占用的 ID。它会拒绝放置嵌套超过 `MaxObjectDepth` 层或含有切变精灵的树，也会拒绝在祖先变换之下会让精灵（对象自身或其后代的）产生切变的缩放或旋转。两个视口都能绘制、拾取和编辑嵌套的子对象，[场景树](#场景树861)可以选择、重新指定父对象、调整顺序和重命名它们。

### 实例化与继承（#483）

版本 2 的场景可以像 Godot 的场景那样复用其他场景文件。每个引用都和材质引用一样，是从项目根目录起、以斜杠分隔的路径；`Parse` 与 `Encode` 会原样保留引用和覆盖：由实例组成的关卡保存后仍是同样的实例，而不是这些实例中对象的副本。

带有 `scene` 的对象会实例化该文件。被实例化场景中每个图层的顶层对象按文档顺序成为该对象最前面的子对象，排在它自己的 `children` 之前，因此该对象的变换决定整个实例的位置。这些对象的 ID 会加上该对象的 ID 和一个斜杠作为前缀，因此同一个敌人的十个实例分别持有 `enemy_1/body` 到 `enemy_10/body`。`overrides` 以被实例化场景内部的 ID 为键修改其中的对象；该场景内部再实例化的场景通过带前缀的 ID 访问，例如 `gun/barrel`。被实例化场景的图层设置、相机、背景色和虚拟尺寸不会带入，图层中含有瓦片地图的场景不能被实例化。

```yaml
# scenes/enemy.scene.yaml
version: 2
name: Enemy
layers:
  - name: root
    objects:
      - id: body
        sprite: { atlas: actors, frame: hero }
        properties: { health: 3, speed: 40 }
        children:
          - id: torch
            x: 6
            sprite: { atlas: actors, frame: torch }

# level.scene.yaml
version: 2
layers:
  - name: actors
    objects:
      - id: enemy_1
        x: 32
        "y": 40
        scene: scenes/enemy.scene.yaml
        overrides:
          body:
            alpha: 0.5
      - id: enemy_2
        x: 64
        "y": 40
        scene: scenes/enemy.scene.yaml
        overrides:
          torch:
            x: -6
```

带有 `inherits` 的场景以其基础场景的副本为起点，只修改自己声明的内容：

- `name`、`backgroundColor`、虚拟尺寸和 `camera` 设置后替换基础场景的值，`tilemaps` 按名称添加或替换条目。
- `overrides` 按 ID 修改继承来的对象，包括嵌套的子对象和实例化得到的对象（`enemy_1/body`）。
- `layerOverrides` 按名称修改继承来的图层。
- 与基础场景某个图层同名的图层会把自己的对象添加到该图层，且不能设置其他任何字段；其他图层添加在基础场景的图层之后。

继承来的对象或图层不能删除，其 ID 也不能修改。

```yaml
# scenes/fast_enemy.scene.yaml：上面的敌人，速度加倍
version: 2
inherits: scenes/enemy.scene.yaml
overrides:
  body:
    properties:
      speed: 80
```

覆盖可以设置对象除 `id`、`children`、`scene` 和 `overrides` 以外的任何字段；未写出的字段保留基础场景的值，`0` 或 `false` 同样算作修改。`components` 和 `properties` 按键逐个替换，对象的其他键保持不变。

`Resolve(doc, scenes)` 通过 `SceneResolver`（例如 `FSScenes{FS: os.DirFS(root)}`）加载被引用的文件，返回一份自包含的副本，其中没有任何对象再实例化其他场景，也没有继承。每次调用中每个文件只加载一次，并且不会修改 `doc` 或解析器返回的文档。它会拒绝互相实例化或继承而形成的循环、深度超过 `MaxSceneNesting` 个文件的引用链、超过 `MaxResolvedObjects` 个对象的结果、指向所修改场景中不存在的对象或图层的覆盖，以及无法通过校验的结果，例如带前缀的 ID 与其他 ID 冲突。当 `AssetResolver` 同时实现了 `SceneResolver` 时，`Build` 会自行解析场景，此时 `BuiltScene.Doc` 是解析后的副本；`SpawnEntities` 和 `BuildNodes3D` 会拒绝仍引用其他场景的场景，需要先解析。

```go
type assets struct {
	sceneformat.AtlasMap
	sceneformat.FSScenes
}

built, err := sceneformat.Build(level, assets{atlases, sceneformat.FSScenes{FS: os.DirFS("project")}})
```

在编辑器中，`PlaceScene(layer, path, x, y)` 以可撤销命令放置一个场景文件的实例，其 ID 以文件名命名。编辑器的 `Resolver` 必须实现 `SceneResolver`；`PlaceScene`，以及 `PlaceObject` 在放置的对象树中含有 `scene` 时，会先按放置后的样子解析整个场景，解析失败则拒绝放置：文件缺失或无效、override 指向不存在的对象或取值无效、展开实例后 ID 冲突。把编辑器的 `ScenePath` 设为当前场景相对项目根目录的路径（`OpenYAML` 会清空它），这样直接或经由其他文件回到当前场景的实例会在保存出自我实例化的文件之前，作为场景循环被拒绝。在版本 1 的场景中放置实例会把场景升级到版本 2（撤销时恢复）。新 ID 不仅与场景自身的 ID 比较，还与继承的基础场景和实例带来的 ID 比较。视口通过实例的根对象选择和移动整个实例；运行（Play）时使用解析后的场景副本，因此通过 `BuiltObject.Doc` 所做的改动在停止（Stop）恢复之前会反映在编辑器的文档中。

### 示例

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Cave Room
virtualWidth: 320
virtualHeight: 180
backgroundColor: "#141824"
camera:
  x: 160
  y: 90
  zoom: 1
  bounds:
    x: 0
    y: 0
    width: 320
    height: 180
tilemaps:
  floor_grid:
    tileWidth: 16
    tileHeight: 16
    width: 4
    height: 2
    tileset: cave_tiles
    data: [1, 2, 2, 1, 1, 0, 0, 1]
layers:
  - name: floor
    space: world
    order: 0
    tilemap:
      ref: floor_grid
  - name: actors
    space: world
    order: 10
    objects:
      - id: hero
        name: Hero
        prefab: prefabs/hero
        x: 48
        y: 40
        originX: 8
        originY: 8
        z: 5
        sprite:
          atlas: actors
          frame: hero
        components:
          collider: { w: 12, h: 14 }
        properties:
          spawn: true
  - name: hud
    space: screen
    order: 100
    objects:
      - id: heart_1
        x: 8
        y: 8
        sprite:
          atlas: ui
          frame: heart
```

### 在 Go 中加载、构建与绘制

```go
doc, err := sceneformat.Parse(yamlBytes)
if err != nil {
	return err
}

built, err := sceneformat.Build(doc, sceneformat.FSAssets{
	FS: os.DirFS(projectDir), // YAML 图块集、Tiled 与 LDtk 地图
	Atlases: sceneformat.AtlasMap{
		"cave_tiles": tilesAtlas,
		"actors":     actorsAtlas,
		"ui":         uiAtlas,
	},
})
if err != nil {
	return err
}

// 每个更新步骤中：
built.Update(dt)
// 在 core.Canvas 的 Draw 回调中：
built.Draw(d)
```

`Parse` 会拒绝未知 YAML 字段、多余的 YAML 文档、不支持的版本号、旧版本场景中的新版本字段（版本 1 中的版本 2 字段、版本 1 和版本 2 中的版本 3 视差/重复字段、版本 1–3 中的版本 4 `environment`/`light`/`camera` 字段）、多个 `current: true` 的 3D 相机、不是从项目根目录起以斜杠分隔的场景引用路径、缺少所修改的 `scene` 或 `inherits` 的覆盖、重复的图层名或对象 ID（包括嵌套的子对象）、深度超过 `MaxObjectDepth` 的对象树、非法坐标系、错误颜色值以及非有限或越界的变换参数、光源/相机/环境参数、视差系数或重复周期。`Encode` 会先校验再输出确定性的 2 空格缩进 YAML，通过 `Parse` 可无损往返还原。

### Schema

场景格式的 JSON Schema 发布在 `https://game.nsgo.dev/schemas/scene.v1.json`（`sceneformat.SchemaURL`）、`https://game.nsgo.dev/schemas/scene.v2.json`（`sceneformat.SchemaURLV2`，用于实例化或继承其他场景文件、引用 glTF 模型、含分层网格或 LDtk 关卡的场景）、`https://game.nsgo.dev/schemas/scene.v3.json`（`sceneformat.SchemaURLV3`，用于支持视差和重复图层的版本 3 场景），以及支持 3D 光源、场景环境和 3D 相机的版本 4 Schema `https://game.nsgo.dev/schemas/scene.v4.json`（`sceneformat.SchemaURLV4`）。`Encode` 在每个文件开头写入与其版本对应的 `# yaml-language-server: $schema=…` 这一行（`sceneformat.SchemaCommentFor`），VS Code 的 YAML 扩展以及其他带 YAML 语言服务器的编辑器可借此在编写场景时校验和补全。参见 [YAML 文件的 Schema](schemas.md)。

### 在 Git 中合并场景文件（#1058）

`sceneformat.Merge(base, ours, theirs)` 对场景文档执行基于身份的三路合并：图层按 `name` 匹配，对象在整个场景树中按 `id` 匹配（包括跨父节点和跨图层移动），瓦片地图与覆盖按主键匹配，连接按 `(from, event, to, handler)` 匹配。各个字段（包括嵌套的 `components`、`properties`、`overrides`、`camera` 和 `args` 映射）逐一合并；无冲突的结果会经过 `Validate` 校验并通过 `Encode` 以规范格式写出。

当双方把同一字段改成不同值、一方删除而另一方修改、双方以相同 ID 新增不同对象、重设父节点冲突或形成环，或合并结果无法通过 `Validate` 时，`MergeResult.ConflictDoc(path)` 会生成带版本号、确定性的 YAML 冲突文档 `ConflictDoc`（`version: 1`，由 `ParseConflict` / `EncodeConflict` 读写），记录每个冲突的对象、字段与 `base`/`ours`/`theirs` 三个值，以及已自动合并的条目。项目通过 `.gitattributes`（`*.scene.yaml merge=nsgo-scene`）和 `go tool game merge -setup` 将其接入 Git（参见 [`game merge`](cli.md#merge) 与[场景合并冲突报告](formats.md#场景合并冲突报告)）。

### 组件类型

`ComponentRegistry` 用 Go 代码显式声明场景文件可以使用的组件类型，运行时不依赖反射：每个类型的名称和说明，以及每个字段的名称、种类（`FieldBool`、`FieldInt`、`FieldFloat`、`FieldString`、`FieldStringList`、`FieldPoint`、`FieldRect`、`FieldCircle` 或 `FieldPolygon`）、可选的允许取值 `Enum`（仅用于 `FieldString` 字段）、默认值、可选的闭区间范围、供检查器使用的可选 `Step` 和说明。`Register` 会拒绝空名称或重复名称、非数值字段上的范围或步长、非 `FieldString` 字段上的 `Enum`、最小值大于最大值、`FieldInt` 上的小数范围或步长、负步长，以及种类不符或超出范围的默认值。

四种 2D 形状字段（`FieldKind.IsShape`）在对象局部坐标系下描述世界几何形状，并在 `scene.v1.json`、`scene.v2.json` 与 `scene.v3.json` 中以 `Point`、`Rect`、`Circle` 和 `Polygon` 定义发布：

| 字段种类 | Go 类型 | YAML 形式 | 校验规则 |
| --- | --- | --- | --- |
| `FieldPoint` | `sceneformat.Point` | `{x, y}` | 有限数值 `x`、`y`；拒绝未知键 |
| `FieldRect` | `sceneformat.Rect` | `{x, y, width, height}` | 有限数值，`width >= 0`、`height >= 0`；拒绝未知键 |
| `FieldCircle` | `sceneformat.Circle` | `{x, y, radius}` | 有限数值，`radius >= 0`；拒绝未知键 |
| `FieldPolygon` | `[]sceneformat.Point` | `[{x, y}, ...]` | 有效有限点列表 |

`ParsePoint`、`ParseRect`、`ParseCircle` 和 `ParsePolygon` 将解码后的 YAML 值规范化为对应 Go 类型，`MarshalYAML` 按字母序输出键名，保证保存后的形状字段往返加载与保存完全不变。

```go
reg := sceneformat.NewComponentRegistry()
reg.MustRegister(sceneformat.ComponentType{
	Name: "body",
	Doc:  "A moving body that collides with the level.",
	Fields: []sceneformat.Field{
		{Name: "solid", Kind: sceneformat.FieldBool, Default: true, Doc: "Blocks other solid bodies."},
		{Name: "speed", Kind: sceneformat.FieldFloat, Default: 32.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(500), Doc: "Top speed in points per second."},
		{Name: "layer", Kind: sceneformat.FieldInt, Default: 1, Min: sceneformat.Float64(0), Max: sceneformat.Float64(31), Doc: "Collision layer."},
	},
})

doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/room.scene.yaml", Components: reg})
```

传入注册表后，`ParseWith` 会检查每个对象（包括嵌套的子对象）的组件，拒绝未注册的组件类型、拼错或未知的字段、种类不符的值（`FieldInt` 不接受 `3.0`，`FieldFloat` 接受整数）、非有限数值以及超出范围的值。错误类型是 `*ComponentError`，带有出错的键或值所在的文件、行号和列号，以及对象 ID、组件名和字段名，并列出已注册的名称：

```text
scenes/room.scene.yaml:11:13: sceneformat: object "hero" component "body" field "sped": unknown field (registered: solid, speed, layer)
```

默认值不会写回文档，因此加载后再保存场景不会改变任何内容；`reg.Resolve("body", fields)` 返回补齐默认值后的全部字段。`ValidateComponents` 用于检查在 Go 中构建的文档（没有位置信息），`CheckField` 检查单个值并把它规范化为 `bool`、`int`、`float64` 或 `string`。不带注册表的 `Parse` 仍不限制 `components` 的内容。

在编辑器中，`Editor.SetComponentRegistry(reg)` 会先检查当前打开场景的组件，并像 `OpenFile` 一样检查其连接，之后 `OpenFile` 报告组件错误时会带上文件和行号，`SetComponentField` 与 `PlaceObject` 会拒绝注册表不允许的字段。更换注册表会清空撤销/重做历史，因此新注册表不允许的已撤销编辑无法再重做。检查器按声明顺序列出组件的每个已注册字段，场景中未设置的字段显示其默认值；字段标签的提示信息给出说明、种类、范围和默认值，枚举字段显示下拉选择框，字符串列表字段按逗号分隔编辑，数值输入框受范围限制，并按字段的 `Step` 步进（未设置时整数为 1、浮点数为 0.1）。浮点数按完整精度显示，注册表拒绝的编辑会显示在状态栏中。没有注册类型的组件，或者没有设置注册表时的所有组件，仍按文件中的值显示（内置物理组件的结构定义会在检查器中自动可用）。

### 内置物理组件（#817）

`sceneformat` 内置六个带版本号的物理组件（`PhysicsComponentVersion = 1`），可通过 `RegisterPhysicsComponents(reg)` 或 `PhysicsComponentRegistry()` 一起注册：

| 组件 | 维度 | 主要字段 |
| --- | --- | --- |
| `body2d` | 2D | `version`（`1`）、`kind`（`static`、`kinematic`、`dynamic`）、`mass`、`gravityScale`、`vx`、`vy`、`friction`、`restitution`、`linearDamping`、`angularDamping`、`material` |
| `collider2d` | 2D | `version`（`1`）、`shape`（`box`、`circle`）、`width`、`height`、`radius`、`offsetX`、`offsetY`、`layer`、`mask`（`[]string`）、`isTrigger`；声明事件 `enter`、`stay`、`exit` |
| `trigger2d` | 2D | `version`（`1`）、`shape`（`box`、`circle`）、`width`、`height`、`radius`、`offsetX`、`offsetY`、`layer`、`mask`（`[]string`）；声明事件 `enter`、`stay`、`exit` |
| `body3d` | 3D | `version`（`1`）、`kind`（`static`、`kinematic`、`dynamic`、`trigger`）、`mass`、`gravityScale`、`vx`、`vy`、`vz`、`friction`、`restitution`、`linearDamping`、`angularDamping`、`lockRotation`、`layer`、`mask`（`[]string`）、`material`、`physicsMaterial`；声明事件 `enter`、`stay`、`exit` |
| `collider3d` | 3D | `version`（`1`）、`shape`（`box`、`sphere`、`capsule`、`cylinder`、`convex`、`trimesh`、`heightfield`）、`width`、`height`、`depth`、`radius`、`offsetX`、`offsetY`、`offsetZ`、`rotX`、`rotY`、`rotZ`、`terrain`、`maxVertices`、`layer`、`mask`（`[]string`）、`isTrigger`；声明事件 `enter`、`stay`、`exit` |
| `trigger3d` | 3D | `version`（`1`）、`shape`（`box`、`sphere`、`capsule`、`cylinder`、`convex`）、`width`、`height`、`depth`、`radius`、`offsetX`、`offsetY`、`offsetZ`、`rotX`、`rotY`、`rotZ`、`maxVertices`、`layer`、`mask`（`[]string`）；声明事件 `enter`、`stay`、`exit` |

- **具名碰撞层**：`layer` 与 `mask` 直接引用 [`game.yaml`](project.md) 中 `physics.layers` 声明的易读层名，而非原始位掩码（适用于 `collider2d`、`trigger2d`、`body3d`、`collider3d` 和 `trigger3d`）。向 `sceneformat.ParseWith` 传入 `ParseOptions{File: ..., PhysicsLayers: proj.PhysicsLayerTable()}`（`project.File.LoadAutoloads` 与 `Editor.OpenProject` 会自动传入）：任何拼错或未知的层名都会加载失败，并返回带有精确文件路径、从 1 开始的行号、列号、字段路径及合法层列表的 `*loaderr.Error`。
- **构建与步进（`BuildPhysics`）**：`ps, err := sceneformat.BuildPhysics(built, proj.PhysicsSettings(), lib)` 会构造匹配的 `physics.World`、`physics.RigidWorld` 和 `physics3d.World`（应用项目重力、求解器迭代次数、具名 `Layer`/`Mask` 位掩码、通过 `PhysicsMaterialResolver` 解析的 `*.physics.yaml` 资源、通过 `ModelResolver` 或 `FileResolver` 加载的 `convex`/静态 `trimesh` 模型网格以及通过 `TerrainResolver` 或 `FileResolver` 加载的静态 `*.terrain.yaml` 高度场，并记录到 `ps.Bodies3D`、`ps.TriMeshes3D` 和 `ps.Heightfields3D`）。调用 `ps.Step(dt)` 会推进 2D 运动学与刚体、2D 触发器以及 3D 刚体与触发器，将更新后的局部坐标位姿写回 `BuiltObject.Doc`，并把 `PhysicsTriggerEvent`（`TriggerEnter`、`TriggerStay`、`TriggerExit`）派发到 `ps.OnTrigger` 及已连接的 `ps.Signals`（`enter` / `stay` / `exit`）。
- **编辑器内运行、视口调试线框、3D 碰撞体手柄与贴合模型包围盒（#902）**：`Editor.PlayIn` 会自动构建并步进 `PhysicsScene`（`Editor.PlayPhysics`、`Editor.PlayTriggerEvents`、`Editor.OnPlayTrigger`），因此包含玩家刚体、墙壁碰撞体与金币触发器的场景无需编写碰撞 Go 代码即可直接在编辑器中运行并触发碰撞与触发事件。`Editor.ShowPhysicsDebug` 开启时（默认开启，可用 `SetShowPhysicsDebug` 切换），2D 与 3D 视口均会绘制每个 `collider2d`、`trigger2d`、`collider3d` 和 `trigger3d` 的线框轮廓（包含圆柱、凸包、三角网格与地形高度场）。将 3D 变换控件切换为 `Gizmo3DCollider`（工具栏 **Collider**、`View > 3D Gizmo > Collider` 或快捷键 `shift+c`）即可通过 3D 控件 API 拖拽碰撞体尺寸手柄（`ColliderHandles3D`、`PickGizmo3D`、`BeginGizmoDrag3D`、`UpdateGizmoDrag3D`、`EndGizmoDrag3D`），每次拖拽作为单个撤销步骤提交；点击检查器中的 **Fit to Model Bounds** 按钮（或调用 `Editor.FitCollider3DToBounds`）即可在单个撤销步骤中将碰撞体尺寸和局部偏移贴合到对象网格或 glTF 模型的包围盒。

### 内置 2D 光照与遮挡体组件（#887）

`sceneformat` 内置两个带版本号的 2D 光照组件（`Lights2DComponentVersion = 1`），可通过 `RegisterLights2DComponents(reg)` 或 `Lights2DComponentRegistry()` 一起注册（编辑器检查器中默认自动可用）：

| 组件 | 主要字段 |
| --- | --- |
| `light2d` | `version`（`1`）、`kind`（`point`、`spot`）、`color`（CSS 颜色）、`radius`、`intensity`、`falloff`、`direction`（弧度，与对象旋转叠加）、`angle`（聚光灯张角弧度）、`castShadows`、`enabled`、`offsetX`、`offsetY` |
| `occluder2d` | `version`（`1`）、`shape`（`rect`、`polygon`、`tilemap`）、`width`、`height`、`offsetX`、`offsetY`、`points`（`[][2]float64`，流式 YAML `[[x, y], ...]`）、`layer`、`tileLayers`（`[]string`）、`enabled` |

- **自动构建 `lights2d.Scene`**：`sceneformat.Build` 会把 `SceneDoc.AmbientColor`、可见世界空间图层上的所有 `light2d` 与 `occluder2d`，以及 `shape: "tilemap"` 遮挡体（通过 [`tileshape.View`](tileshape.md) 从瓦片地图碰撞形状提取）组装为 `BuiltScene.Lights2D`（`*lights2d.Scene`）。除非设置了 `SkipLights2D`，`BuiltScene.Draw` 会在场景相机变换下将 `Lights2D` 绘制在世界空间图层之上（屏幕空间 HUD 图层之下）；运行时移动光源或遮挡体后调用 `BuiltScene.SyncLights2D()` 同步。
- **2D 光照预览与交互手柄**：2D 视口工具栏提供 **Lighting** 切换按钮（`Editor.ShowLightingPreview`、`SetShowLightingPreview`、命令 `CmdToggleLightingPreview`），在编辑模式下实时预览与游戏完全一致的 `lights2d.Scene` 阴影与环境暗场。选中 `light2d` 对象时会显示半径（`Handle2DLightRadius`）、聚光灯张角（`Handle2DLightSpotAngle`）与聚光灯方向（`Handle2DLightSpotDirection`）手柄；选中 `occluder2d` 对象时，矩形遮挡体显示四边缩放手柄（`Handle2DOccluderRectN/E/S/W`），多边形遮挡体显示可拖动顶点手柄（`Handle2DOccluderVertex`）与边中点插点手柄（`Handle2DOccluderEdgeMid`，以及 `SetOccluderPolygonPoints`、`InsertOccluderVertex`、`RemoveOccluderVertex`、`ConvertOccluderToPolygon`）。每次拖动记录为一个可撤销步骤，按 Escape 可取消；`SetAmbientColor`（或检查器中的 Scene Lighting 区域）可撤销地编辑 `ambientColor`。

### 内置 2D 导航区域组件（#888）

`sceneformat` 内置带版本号的 2D 导航区域组件（`NavRegion2DComponentVersion = 1`），可通过 `RegisterNavRegion2DComponent(reg)` 或 `NavRegion2DComponentRegistry()` 注册（在编辑器检查器以及 `scene.v1.json` / `scene.v2.json` / `scene.v3.json` Schema 中默认自动可用）：

| 组件 | 主要字段 |
| --- | --- |
| `navregion2d` | `version`（`1`）、`mode`（`walkable`、`obstacle`）、`points`（`[][2]float64`，流式 YAML `[[x, y], ...]`）、`offsetX`、`offsetY`、`subtractTileCollision`（`bool`）、`tileLayer`（`string`）、`tileLayers`（`[]string`）、`agentRadius`（`float`，`>= 0`）、`enabled`（`bool`） |

- **自动构建 `ai.NavMesh`（`BuildNavMesh2D`）**：`sceneformat.Build` 会收集可见世界空间图层上所有启用的 `navregion2d` 组件（统一绕序，确保重叠的可行走区域求并、障碍物区域挖洞），按需通过 [`tileshape.View`](tileshape.md) 及非矩形实心瓦片形状扣除场景瓦片地图中的实心碰撞，附加场景中声明的 2D `nav_link` 离网连接，并调用 [`game/navbake`](navbake.md) 烘焙凸多边形，填充 `BuiltScene.NavBake2D`（`*navbake.Mesh`）与 `BuiltScene.NavMesh2D`（`*ai.NavMesh`）。不含 `navregion2d` 的场景会将两字段保持为 `nil`；运行时修改后可调用 `BuiltScene.SyncNavMesh2D()` 重新同步。
- **2D 导航网格叠加层、多边形手柄与路径测试工具**：开启 `Editor.ShowNavMesh2D`（默认开启，可通过工具栏 `view-navmesh2d-overlay`、`SetShowNavMesh2D` 或 `CmdToggleNavMesh2D` 切换）时，2D 视口会绘制已构建 `NavBake2D` 的凸多边形、相邻门户以及作者编辑的 `navregion2d` 轮廓。选中 `navregion2d` 对象会显示可拖动顶点手柄（`Handle2DNavRegionVertex`）与边中点插点手柄（`Handle2DNavRegionEdgeMid`），并提供 `PlaceNavRegion2D`、`InitNavRegionPolygon`、`SetNavRegionPoints`、`InsertNavRegionVertex` 与 `RemoveNavRegionVertex`；拖动顶点时叠加层实时刷新，松手后记录为单个可撤销步骤（按 Escape 取消）。选择 **Test Path** 工具（`ToolNavTestPath`，工具栏 `tool-nav-test-path`，命令 `CmdToolNavTestPath`）后，在视口依次点选起点与终点即可绘制 `BuiltScene.NavMesh2D.FindPath` 找到的路径（随顶点或瓦片修改实时重算，按 Escape 或调用 `ClearNavPathPreview2D` 清除）。

### 连接（#490）

场景文件可以把一个对象发出的事件连接到另一个对象的处理函数，就像 Godot 在 `.tscn` 文件里写的 `[connection]` 条目，于是按钮能打开门，而不需要任何点名这两个对象的 Go 代码：

```yaml
layers:
  - name: props
    objects:
      - id: button
        components:
          button: {}
      - id: door
        components:
          door: {}
connections:
  - from: button
    event: pressed
    to: door
    handler: open
    args:
      speed: 2.5
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `from` | `string` | 必填，发出事件的对象 ID。 |
| `event` | `string` | 必填，事件名，如 `pressed`；对象有多个组件声明了同名事件时写成 `button.pressed` 指明组件。 |
| `to` | `string` | 必填，运行处理函数的对象 ID，可以就是 `from` 本身。 |
| `handler` | `string` | 必填，处理函数名，如 `open`，或 `door.open`。 |
| `args` | 映射 | 可选，按名称给处理函数的参数赋值，每个值是布尔、数字或字符串；没写的参数取默认值。 |
| `once` | `bool` | 事件第一次触发后断开连接，相当于 Godot 的一次性连接。 |

事件和处理函数来自组件注册表：`ComponentType` 列出带有该组件的对象会发出的 `Events` 和能运行的 `Handlers`，每个处理函数有带类型的 `Params`（声明与检查方式同字段）以及运行它的 `Func`。游戏只需为每种组件注册一次它做什么，哪些对象连在一起由场景文件决定。

```go
reg.MustRegister(sceneformat.ComponentType{
	Name:   "button",
	Events: []sceneformat.Event{{Name: "pressed", Doc: "The player pressed the button."}},
})
reg.MustRegister(sceneformat.ComponentType{
	Name:   "door",
	Fields: []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool}},
	Handlers: []sceneformat.Handler{{
		Name:   "open",
		Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0)}},
		Func: func(c sceneformat.Call) {
			c.Target.Components["door"].(map[string]any)["open"] = true
		},
	}},
})

doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/hall.scene.yaml", Components: reg})
signals, err := sceneformat.Connect(doc, reg, game)
// 在游戏处理“按下带 button 组件的对象”的地方：
err = signals.Emit(objectID, "pressed", nil)
```

`Connect(doc, reg, ctx)` 检查场景的组件和连接，返回一个 `*Signals`。`Signals.Emit(from, event, payload)` 按文件中列出的顺序调用该事件每条连接的处理函数，已经运行过的 `once` 连接会跳过。每个处理函数收到一个 `Call`：`Signals` 本身（处理函数可以继续发出事件，最多嵌套 `MaxEmitDepth` 层）、传给 `Connect` 的 `ctx`（如游戏状态或 `*BuiltScene`）、场景中源对象与目标对象的 `*ObjectDoc`、事件与处理函数所属的组件和名称、包含全部参数的 `Args`（连接给的值或默认值），以及 `payload`。场景中没有的对象、其已注册组件都没有声明的事件，以及有两个组件都声明了的裸名称（应写成 `component.event`），`Emit` 都会拒绝。`Signals` 是 `Connect` 时场景对象的快照：在 `doc` 中添加或删除对象后（这可能使其他对象在内存中移动），要重新调用 `Connect`。实例化或继承了其他场景文件的场景要在 `Resolve` 之后连接（或使用 `BuiltScene.Doc`）；`Signals` 不能并发使用。

`Parse` 检查连接的形式：四个名称都已填写，`event` 和 `handler` 写成名称或 `component.name`，参数是布尔、有限数字或字符串，没有重复的连接；场景不引用其他场景文件时，`from` 和 `to` 还必须是场景里的对象。提供注册表时，`ParseWith` 还会按注册表检查这些名称，并以带文件、行号和列号的 `*ConnectionError` 报告：对象的已注册组件都没有声明的事件或处理函数、被对象的两个组件同时声明的裸名称、处理函数没有对应参数的实参，以及类型不对或超出范围的实参。错误会列出该对象实际声明了什么：

```text
scenes/hall.scene.yaml:16:14: sceneformat: connection 0 ("button" pressed -> "door" opne): no registered component of object "door" declares handler "opne" (it has: door.open)
```

`ValidateConnections` 对已解析的文档做同样的检查，但不带位置。`Register` 拒绝名称为空、含有点号或与该类型已有名称重复的事件和处理函数，处理函数参数的检查规则与字段相同；只用于检查文件的注册表（如编辑器的）可以不设 `Func`，但 `Connect` 会拒绝它。

`Resolve` 会带上连接：实例化场景的连接随实例一起进来，其中的对象名与对象 ID 一样加上前缀（`door_1/button`），因此预制件的每个副本都保留自己的连线；继承的场景保留基础场景的连接，并把自己的连接加在后面。场景可以连接它所实例化或继承的场景中的对象，这些名称在解析之后才检查。在编辑器中，`DeleteObject` 会同时删除来自或指向被删对象及其后代的连接，撤销删除时把它们放回原处。

#### 排队的事件

`Emit` 在返回前就运行处理函数，所以发出事件的处理函数会在自身内部运行下一批处理函数。要让一步之内的顺序简单明了，改用 `Signals.Enqueue(from, event, payload)` 发出事件，并在每一步调用一次 `Signals.Flush()`，例如在 `game.Loop` 的 `Update` 末尾：

```go
loop.Update = func(dt time.Duration) {
	updateSystems(dt) // the game's own input and systems, which call signals.Enqueue
	if err := signals.Flush(); err != nil {
		log.Print(err)
	}
}
```

`Enqueue` 在排入任何东西之前就拒绝 `Emit` 会拒绝的情况，所以拼错的事件在发出的地方就会失败。`Flush` 按入队顺序运行它开始之前入队的事件，每个都像 `Emit` 那样运行；处理函数入队的事件（包括它自己的事件）要等到下一次 `Flush`，所以一次 `Flush` 总会结束，处理函数也不会重入自身。在处理函数中调用 `Flush` 会被拒绝，`Pending` 返回等待中的事件数。处理函数的 panic 会穿过 `Flush` 传出，发生 panic 的事件之后的事件仍留在队列中，等待下一次 `Flush`。队列增长到一步的事件量之后，每一步的分配不会多于直接发出这些事件。

#### 生成连接好的实体

`SpawnConnected(doc, world, reg, ctx)` 像 `Connect` 那样检查场景，像 `SpawnEntities` 那样生成它的对象，然后返回每个对象 ID 对应的实体，以及连接这些实体的 `*Signals`。所有检查都在生成第一个实体之前完成，所以连接指向不存在的对象、事件、处理函数或 `Func`，或者组件不符合其类型的场景，什么都不会生成。`EmitEntity(e, event, payload)` 和 `EnqueueEntity` 用实体指定对象，`Entity(id)` 返回对象的实体，每个 `Call` 都带有 `SourceEntity` 和 `TargetEntity`，处理函数可以用 `entities.Get` 取到目标的组件。被销毁的实体不再发出事件：`Emit`、`Enqueue` 及其实体形式会拒绝它，`Flush` 丢弃它之前入队的事件，指向它的连接会被跳过。实例化或继承其他场景的场景在 `Resolve` 之后生成，实例的连接作用于实例的实体（`west/door`）。

#### 检查连接

`nsgo game check` 会解析每个实例化或继承其他场景的场景。场景无法解析时（例如循环引用，或覆盖项指向不存在的对象）它会失败，并报告实例所在行或 `inherits` 行；连接的 `from` 或 `to` 指向解析后场景中不存在的对象时也会失败，并报告写有该名称的行。事件和处理函数在 Go 中声明，命令行无法加载它们，所以游戏在自己的测试中用 `assetcheck.CheckWith` 和自己的注册表检查它们。它会在文件和行上报告指向对象未声明的事件、处理函数或参数的连接，以及不符合类型的组件；对实例化或继承其他场景的场景，它还会检查覆盖项写入的组件和解析后场景的每个连接，所以覆盖项破坏了被实例化或被继承的场景带来的连接时（例如再加一个声明同名事件的组件），会在该覆盖项处失败：

```go
func TestScenesCheck(t *testing.T) {
	report, err := assetcheck.CheckWith(os.DirFS("."), assetcheck.Options{Components: Components})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range report.Problems {
		t.Error(p)
	}
}
```

```text
scenes/level.scene.yaml:12: sceneformat: connection 0 ("lever" held -> "west/door" open): no registered component of object "lever" declares event "held" (it has: button.pressed)
```

场景从所实例化或继承的场景原样带来的问题在那个文件上报告，它的覆盖项造成的问题在覆盖项上报告，所以每个问题只报告一次。问题只给出行号，不给出列号。

### 检查器（#865）

检查器可以直接设置对象，无需编辑 YAML。它的每次编辑都是一个可撤销的步骤，并且都有一个 `Editor` 方法在 Go 中完成同样的事；运行（Play）期间这些方法返回 `ErrPlaying`，控件也会被禁用。编辑器拒绝的编辑会显示在状态栏中。

- **精灵**：Sprite 字段从资源浏览器列出的图集帧（`Assets`）中选取一帧，按图集分组并可搜索，清除按钮会解除绑定；旁边的按钮使用资源浏览器中选中的帧。`SetObjectSprite(id, ref)` 要求同时给出图集和帧，设置了解析器时图集必须能解析并包含该帧；`ref` 为 nil 时解除精灵绑定。
- **原点与翻转**：精灵帧内的枢轴点 `originX` 与 `originY` 用 `SetObjectOrigin(id, x, y)` 设置，它拒绝非有限值；`flipX` 与 `flipY` 用 `SetObjectFlip(id, flipX, flipY)` 设置。
- **材质**：Material 字段从当前打开项目的 `*.material.yaml` 文件中选取一个，清除按钮则不指定材质；`SetObjectMaterial(id, path)` 接受相对项目根目录、以斜杠分隔的路径，或空路径。材质必须能通过编辑器的解析器加载，因此解析器必须实现 `sceneformat.MaterialResolver`（`resource.Library` 即实现了它），否则场景无法构建；使用其他解析器时，该字段不列出任何文件，只在对象已指定材质时保持可用，以便清除它。
- **模型（#893）**：在 3D 模式下，Model 字段从当前打开项目的 glTF 模型中选取一个（`ProjectModels`：项目中的 `.gltf` 和 `.glb` 文件，以及 `game.yaml` 资源部分中模型目标的输出），可搜索，清除按钮则不指定模型。`SetObjectModel(id, path)` 拒绝无法加载的模型，也拒绝为绘制内置基本形体的对象指定模型，正如对象绘制模型时 Mesh 字段不可用；更换模型会清除 `modelNode` 和 `modelMesh`，因为它们指向旧模型的部件；指定模型会把版本 1 的场景提升到版本 2（第一个支持模型的版本），更高版本的场景保持不变。撤销会恢复这三个字段和版本。
- **属性**：Add Property 接受一个键和一种类型（`string`、`int`、`float` 或 `bool`），添加的属性取该类型的零值（`AddProperty(id, key, kind)`）；该字段会去掉键首尾的空白，空键、首尾带空白的键或对象已有的键会被拒绝。值为整数的浮点属性保存时带小数点（`2.0`），因此再次打开时仍是浮点数。每个属性的删除按钮会删除该属性（`DeleteProperty`）。
- **组件**：Add Component 字段会筛选对象尚未拥有的已注册类型，按下其中一个即添加该组件，并写入每个已注册字段的默认值，让文件能看出组件的作用（`AddComponent(id, name)`）。按回车会添加字段中写出的类型，或唯一匹配的那个类型。对象已有的组件、注册表不认识的名称，以及首尾带空白或含有 `.`（连接在 `component.event` 中使用它）的名称都会被拒绝；没有注册表时，该字段可以添加任意名称、不含字段的组件。如果新组件声明的事件或处理器被某个连接以裸名称引用，而对象的另一个组件也声明了它，添加也会被拒绝，因为连接将无法说明指的是哪一个：请先把它写成 `component.event`。在实例化或继承其他场景的场景中，这项检查针对通过编辑器的解析器解析后的场景进行，包括它所引用场景的连接；如果解析器无法加载场景文件，或者即使不加新组件场景也无法解析和绑定，则跳过检查。每个组件的移除按钮会移除该组件（`RemoveComponent(id, name)`），连同所有需要它的连接：从该对象发出、事件由该组件声明的连接，以及指向该对象、处理器由该组件声明的连接，无论写成裸名称还是 `component.name`；裸名称通过组件的注册类型来归属，因此没有注册表、或注册表不认识该类型时，只会移除写成 `component.name` 的连接。`ComponentConnections(id, name)` 列出这些连接，撤销会把组件和连接放回原处。没有注册类型的组件仍按文件中的值显示，字段照常可编辑；由于没有结构定义，其字段中值为整数的浮点数保存为 `2`，再次打开时是整数。
- **连接（#872）**：Connections 区域列出当前选中对象的已注册组件声明的全部事件（`ObjectEvents`），以及所有与该对象相关的发出连接和传入连接（`ObjectConnections`）。点击 **Connect** 会展开内联表单：选择事件、从可搜索的对象列表（`SceneObjectIDs`）选择目标对象或开启拾取模式直接从 2D/3D 视口或场景树点选目标（`BeginConnectionPick` / `CancelConnectionPick`，拾取期间保持当前选中对象不变）、选择目标对象的已注册处理函数（`ObjectHandlers`）、使用组件注册表提供的带类型控件填写参数，并切换 `once`。每条连接都支持跳转到另一端（`JumpToConnectionOtherEnd`）、编辑（`UpdateConnection`）或移除（`RemoveConnection`）。添加、编辑与移除连接（`AddConnection`、`UpdateConnection`、`RemoveConnection`）都是可撤销命令，在进入 `History` 前先用 `sceneformat.Validate` 和 `sceneformat.ValidateConnections` 校验：无效连接（例如目标对象缺少该处理函数、重复连接、参数类型不符或超出范围）会被拒绝并在状态栏报告原因，同时保持场景与撤销历史不变。未设置组件注册表时，已有连接依然可以查看、跳转、编辑和移除。
- **场景设置（#871、#884）**：没有选中对象、房间、图层或资源文件时（`ShowingSceneSettings`），检查器会显示当前打开场景自身的设置，而不是空白占位：场景 `name`（`SetSceneName`）、虚拟尺寸 `virtualWidth` 与 `virtualHeight`（`SetVirtualSize`）、背景色 `backgroundColor`（结合 NSGo 颜色选择器 `ui.ColorWell`、CSS 十六进制输入框与清除按钮，`SetBackground`），以及默认 2D 相机 `camera`（`SetCamera`、`SetCameraPosition`、`SetCameraZoom`、`SetCameraRotation`）和可开关的限制边界 `bounds`（`SetCameraBounds`、`ClearCameraBounds`）。每次修改都会在改动文档或撤销历史之前通过 `sceneformat.Validate` 校验——非正虚拟尺寸、非正缩放、非正边界或非法颜色字符串会被拒绝并在状态栏报告——实时更新视口，保存与重新加载后保持不变，并完整接入撤销/重做。在 2D 编辑模式下显示场景设置时，视口会高亮绘制相机的初始视口框与虚线边界，在相机位置绘制可拖拽手柄（`CameraHandleCanvasPos`、`HitTestCameraHandle`、`BeginCameraDrag`、`UpdateCameraDrag`、`EndCameraDrag`、`CancelCameraDrag`），并在 `camera.bounds` 上绘制 8 个角/边缩放手柄（`CameraBoundsHandleCanvasPos`、`HitTestCameraBoundsHandle`、`BeginCameraBoundsDrag`、`UpdateCameraBoundsDrag`、`EndCameraBoundsDrag`、`CancelCameraBoundsDrag`），拖拽手柄可实时调整边界大小（拖拽边框或空白内部可平移边界），按 Escape 取消，松手以单个可撤销步骤提交。
- **房间地图（`*.rooms.yaml`，#884）**：可从文件浏览器在当前场景旁打开已有 `*.rooms.yaml`（`OpenRoomsProjectFile`、`OpenRoomsFile`、`OpenRoomsYAML`），或在场景设置检查器中新建房间地图（`NewRoomMap`）。所有房间都会连同其 ID、名称、过渡标记以及选中房间的 8 个缩放手柄直接绘制在 2D 场景视口之上。开启 **Draw Room**（`SetRoomCreateMode`）后可在视口拖拽绘制新房间，也可点击 **Add Room**（`AddRoom`、`AddRoomAtViewCenter`）添加；在视口中可通过手柄选中、移动和缩放房间（`HitTestRoom`、`HitTestRoomHandle`、`BeginRoomDrag`、`UpdateRoomDrag`、`EndRoomDrag`、`CancelRoomDrag`），并通过 Delete 按钮或 Delete/Backspace 键删除房间（`DeleteRoom`）。开启 **Grid Snap**（默认开启，`SetRoomSnapToGrid`）时，房间边缘会自动吸附到场景瓦片网格（`RoomGridSize`，取自场景的内联或外部瓦片地图，或通过 `SetRoomGridSize` 显式指定）。选中房间后检查器切换为房间检查器，可编辑房间的 `id`（`SetRoomID`）、`name`（`SetRoomName`）、边界（`SetRoomRect`）以及房间级 `transition`（`SetRoomTransition`：继承、`cut`、`slide`、`fade` 及时长、缓动和淡入淡出颜色），也可编辑地图级默认过渡（`SetRoomMapTransition`）。所有房间编辑都接入 `Editor.History` 撤销栈、`HasUnsavedChanges` 与 `Save`，并通过 `rooms.Encode` 确定性保存（`SaveRooms`、`SaveRoomsFile`、`SaveRoomsYAML`），保存的文件可由 `rooms.Parse` 读取并保持逐字节往返一致。
- **图层检查器（#885、#877）**：未选中对象但选中图层时（例如在场景树中点击图层行、在图层面板右键菜单选择 **Inspect Layer**、在空瓦片调色板点击 **Set Up Tilemap**，或调用 `SetActiveLayer`），检查器显示该图层的视差系数（`Parallax X`、`Parallax Y`）、平铺重复设置（`Repeat` X/Y、`Repeat W`、`Repeat H`）以及 **Tilemap** 瓦片地图配置。在 Go 中，`SetLayerParallax(layer, x, y)` 与 `SetLayerRepeat(layer, repeatX, repeatY, width, height)` 编辑视差与重复，`AddLayerTilemap(layer, tileset, tileW, tileH, w, h)`、`SetLayerTilemapTileset(layer, tileset)`、`SetLayerTilemapTileSize(layer, tileW, tileH)`、`ResizeLayerTilemap(layer, left, right, top, bottom)`、`ExtractLayerTilemap(layer, refName)`、`InlineLayerTilemap(layer)` 与 `RemoveLayerTilemap(layer)` 创建并编辑图层瓦片地图，全部通过 `History` 支持撤销与重做。从左边缘或顶边缘扩展或收缩网格时会平移已绘制单元格并调整 `offsetX`/`offsetY`，使每个已绘制瓦片保持原有世界坐标（写入非零偏移时会把版本 1 或版本 2 场景升级到版本 3，撤销时恢复）。非法尺寸以及缺少已绘制瓦片 ID 对应帧的图集会被拒绝并在状态栏提示。将图层瓦片地图移至顶层 `tilemaps` 会创建 `ref` 引用，移回内联时若不再有其他图层或 `layerOverrides` 引用则一并清理顶层条目。屏幕空间图层禁用视差与重复控件，继承自基础场景的图层禁用全部图层检查器编辑。

### 项目

编辑器也能打开游戏的[项目文件](project.md)：`OpenProject` 读取 `game.yaml` 并打开其中的主场景，`EditProject` 以可撤销的步骤修改它，`SaveProject` 写入它并同步旁边的 `nsgo.yaml`。运行 `nsgo game editor <dir>` 即可打开一个项目目录。设置了 `ProjectAssets`（`editorcmd` 包的编辑器命令会设置它）时，打开项目会把 `Resolver` 换成项目目录上的 `project.Resolver`：场景用项目自己的精灵图集绘制，图集按名称从离当前场景最近处查找，方式与 `game check` 相同；资源浏览器列出这些图集，`PlaceScene` 实例化项目的场景文件，材质、物理材质、YAML 图块集以及 Tiled 和 LDtk 地图也从项目中加载，其中图块集和地图像 `FSAssets` 一样通过它的 `ReadFile` 读取。场景引用的图集从引用它的文件处就近查找，因此从其他目录实例化或继承的场景文件，会用 `game check` 为它找到的图集绘制。编辑器每次做出可能增加、删除或改写图集和材质文件的改动（例如安装或移除包、移动文件、`ReloadAssets` 或 `RefreshSearch`）后，都会重新读取它们；保存场景则保留已经读取的内容。`OpenProjectScene(path, scene)` 打开项目时用场景文件 `scene` 代替主场景，`nsgo game editor` 打开位于项目目录中的场景文件时就是这样做的，因此 `ScenePath` 会指向它，在它自身中放置它的实例会作为循环被拒绝。`MoveFile(from, to)` 移动项目中的文件或目录，并像 `nsgo game mv` 一样改写所有指向它的引用，包括磁盘上的文件和当前打开的场景与项目（见[稳定引用](project.md#稳定引用)）；这是一个步骤，可以撤销，也可以重做。撤销会把移动改写过的每个文件逐字节恢复原样；如果某个文件在移动之后又被改过，则只把其中的引用改写回去。

打开项目后，资源浏览器下方的文件浏览器（`FilesView`）列出项目中的文件和目录（`ProjectFiles`），不列出 `game index` 和 `game mv` 同样跳过的目录：名称以 `.` 或 `_` 开头的目录和名为 `testdata` 的目录。点击一项即可选中它，并把它的路径填入列表下方的输入框；改好路径后点击“Move”，就会通过 `MoveFile` 重命名或移动它；选中 `.yaml`/`.yml` 文件时也可以点击“Open Table”，在数据表编辑器中打开它（`OpenDataTableProjectFile`）；点击“Find Refs”则列出整个项目中指向该文件的所有引用（`ShowReferences`）。编辑器之外的操作改动了文件后，点击“Refresh”会重新列出文件并重新扫描项目搜索与引用索引（`RefreshSearch`）。

## 数据表编辑器（#918）

数据表面板（`DataTablePanelView`）可以从文件浏览器把带版本号的 YAML 数据表（[`tooling/datatable`](datatable.md)）以电子表格网格的形式打开在视口下方：

- **列与值类型**：列是所有行键名的并集，按首次出现的确定顺序排列（注册了 Go 行类型时，也会补齐任何行尚未设置的字段）。每一列的值类型（`string`、`number`、`bool`、`duration` 或 `list`）根据单元格的值推断，或直接取自已注册的 Go 行类型；布尔单元格渲染为切换按钮，标量和列表单元格在按回车或失焦时提交。
- **行操作与视图控制**：支持添加（`AddDataTableRow`）、删除（`DeleteDataTableRow`）和重排行（`MoveDataTableRow`），点击任意列头即可排序（`SetDataTableSort`），并可按不区分大小写的子串过滤可见行（`SetDataTableFilter`）。排序和过滤只改变视图显示，不会改动底层 YAML 文档的行顺序。
- **撤销与无损往返**：打开、关闭、添加、删除、重排和编辑单元格全部进入编辑器共用的 `History` 撤销栈。打开数据表后不做修改直接保存，文件内容逐字节完全一致；修改单个单元格时只改动那一行，同时保留注释、格式以及每一行自己的键顺序。
- **Go 行类型校验**：游戏自己的编辑器命令可以用 `editor.RegisterDataTableType[T](ed, pattern)`（或 `Editor.RegisterDataTableRowType`）按路径或文件名模式注册 Go 结构体行类型。匹配的数据表在打开和每次编辑单元格或行时，都会按与 `datatable.LoadYAML` 完全相同的规则校验，在网格中拒绝未知列、重复字段和无效单元格值，并在面板和状态栏中显示 `*datatable.CellError`。

```go
if err := editor.RegisterDataTableType[Enemy](ed, "data/enemies/*.yaml"); err != nil {
	log.Fatal(err)
}
```

### 2D 视口导航（#864）

2D 视口的画布填满所在面板，并通过编辑器自己的视图（`View2D`：画布中心对应的世界坐标点、缩放和旋转）显示场景，它与场景的 `camera` 相互独立，因此比一屏更大的关卡在任何位置都能看到和编辑。视图从不保存到场景中，也不记入撤销历史。

- 鼠标滚轮、双指捏合，或按住 Control 或 Command 的触控板滚动，以指针为中心缩放，保持指针下的世界坐标点不动（`ZoomView2DAt`），缩放范围在 `MinView2DZoom` 与 `MaxView2DZoom` 之间。
- 触控板双指滚动、鼠标滚轮的横向滚动（Shift+滚轮或可左右拨动的滚轮）、鼠标中键拖动、按住 Space 拖动，或在触摸屏上双指拖动，都会平移视图（`PanView2D`）。
- F 或 Frame Selection 按钮框住选中的对象（`FrameSelection2D`，没有选中时框住全部）；Home 或 Frame All 按钮框住所有可见图层的对象、它们的内联瓦片地图以及相机画面（`FrameAll2D`）；Mod+0 或 Scene Camera View 按钮回到场景相机的视图（`ResetView2D`）。

在作者平移或缩放之前，以及 `ResetView2D` 或打开另一个场景之后，视图就是场景相机的视图：虚拟屏幕以相机的缩放和旋转居中于画布，并随场景变化跟随相机（`View2DFollowsCamera`）。编辑模式下整块画布都填充场景的 `backgroundColor`，远离相机的对象也以游戏的清屏颜色为底显示；画布会描出相机画面（即玩家看到的虚拟屏幕）的轮廓，并以虚线画出相机的 `bounds`。屏幕空间图层绘制在该画面内。在 Play 模式下只显示这个画面，无论作者留下的是什么视图，都按相机的视图居中，与游戏中一致；在 Stop 恢复作者的视图之前，视图不会平移或缩放。

命中测试、放置、移动、瓦片绘制、选中轮廓和插件工具都经过该视图，并在 2D 视图平移时平滑应用每个世界空间图层的视差偏移（并预览其平铺重复）。`CanvasToWorld` 与 `WorldToCanvas` 在画布坐标与世界坐标之间转换；`CanvasToScreen` 与 `ScreenToCanvas` 在画布坐标与场景的虚拟屏幕（屏幕空间图层所在的空间）之间转换；`ScreenToWorld` 与 `WorldToScreen` 只应用场景相机，与游戏中相同。`CanvasSize` 是画布布局后的尺寸，在视口完成布局（`SetCanvasSize`）之前为场景的虚拟尺寸；3D 视口填满同一块画布。

### 2D 变换控件（#863）

使用 Select 或 Move 工具时，可以在 2D 视口中拖动来编辑对象的变换，与 Godot 和 Unity 的 2D 变换工具相同：

- 拖动任意对象会选中并移动它，并保持按下时指针与对象之间的偏移，对象不会跳到指针处。
- 选中的对象带有一个控件，在画布上以固定尺寸绘制：原点处的方块自由移动对象；红色和绿色箭头沿对象自身的 X 轴和 Y 轴移动它，两轴随对象的旋转而转动；对象外的圆环绕原点旋转它；当对象轮廓在画布上每条边都至少有 24 点时（小对象请先放大），四角和四边中点的手柄以原点为中心缩放它，边手柄缩放一个轴，角手柄同时缩放两个轴。在所缩放的某个轴上位于原点所在直线上或紧挨着它的手柄不会提供，例如原点在左上角的精灵的上边和左边手柄；移动方块与某个手柄重叠时，取移动方块。缩放时按住 Shift 保持宽高比。把缩放手柄拖过原点会以负缩放镜像对象。
- 每次拖动是一个可撤销的步骤，在拖动结束时记录（"Move hero"、"Rotate hero"、"Scale hero"）；撤销会精确恢复对象的字段，原本未设置的 `scaleX` 或 `scaleY` 仍保持未设置，边手柄也不会改动另一个轴的字段。在起点结束的拖动不记录任何步骤。
- 按 Escape、系统取消手势或放下第二根手指，都会把对象恢复到拖动前的样子且不记录任何步骤；该手势剩下的部分不再起作用。拖动过程中执行撤销、重做、Play、保存、删除、复制，或切换工具、视图模式，都会先取消拖动。
- 会使缩放为零或接近零（小于 0.001）的缩放，以及会让对象所在树中的精灵产生切变（包括祖先的影响）的缩放或旋转（非均匀缩放的父对象下有旋转的子对象，`ScaleObject` 和 `RotateObject` 同样拒绝），不会被采用：对象保持上一次有效的变换，状态栏说明原因。
- 嵌套的子对象同样带有控件，并在其父对象的空间中拖动，因此在旋转或缩放过的父对象下也会跟随指针。在场景树中隐藏或锁定的对象，或位于这类对象之下的对象，即使被选中也没有控件，也不会被拖动。

拖动经由对象所在的图层换算：世界空间图层中的对象按指针移动的世界距离移动，与 2D 视图或场景相机的缩放和旋转无关；屏幕空间图层中的对象按虚拟屏幕上的距离移动。单击仍然用于选择，使用 Move 工具时单击仍会把选中的对象放到按下的位置。按住 Space 拖动或用鼠标中键拖动则平移视图。

在 Go 中，`BeginDrag2D(x, y)` 在画布上的一点开始拖动并返回它取得的手柄（`GizmoHandle2D`），`UpdateDrag2D(x, y, keepAspect)` 跟随指针，`EndDrag2D` 或 `CancelDrag2D` 结束拖动；`PickGizmo2D` 给出画布上某点下的手柄，`Dragging2D` 给出正在拖动的手柄，`Gizmo2DShown` 表示是否绘制控件。

### 2D 组件形状手柄（#882）

在 2D 编辑模式下，视口会在对象的局部坐标系中绘制选中对象的每一个已注册形状字段（`FieldPoint`、`FieldRect`、`FieldCircle` 和 `FieldPolygon`，自动计入父对象变换、旋转、缩放、图层空间与视差）。切换 2D 视图工具栏中的 **Shapes** 按钮（`SetShowAllShapes`、`CmdToggleAllShapes`、`Editor.ShowAllShapes`）可同时绘制场景中所有可见对象的形状字段，并保持选中对象高亮显示在最上层。

使用 Select 或 Move 工具时，选中对象的形状字段会显示可交互手柄：

- **点（Point）**：拖动手柄移动 `(x, y)`。
- **矩形（Rect）**：拖动 4 个角手柄或 4 个边中点手柄缩放尺寸（拖过对边时平滑翻转并保持 `width >= 0`、`height >= 0`）；当中心手柄不与对象原点移动方块重叠时，拖动中心手柄可整体平移矩形。
- **圆（Circle）**：拖动 4 个正交圆周手柄调整 `radius`（限制 `>= 0`）；当圆心偏离对象原点时，拖动圆心手柄可平移圆。
- **多边形（Polygon）**：拖动任意顶点手柄移动顶点；点击或拖动边中点手柄插入新顶点（`AddPolygonVertex`）；右键点击或 Alt+点击顶点手柄删除顶点（`RemovePolygonVertex`，至少保留 3 个顶点）。
- **网格吸附**：切换 2D 视图工具栏中的 **Snap** 按钮（`SetSnapToGrid`、`CmdToggleSnapGrid`、`Editor.SnapToGrid`），可将手柄拖动与插入顶点吸附到 `GridSize` 网格步长（`SetGridSize`，默认 `8`，`SnapValue`）。

每次拖动、添加顶点和删除顶点均通过 `SetComponentField` 提交为一个可撤销步骤；按 Escape、手势取消、双指介入、撤销/重做、Play、保存或切换工具会取消进行中的拖动并无损恢复字段原值。在 Go 中，可通过 `VisibleShapes2D`、`ShapeHandles2D`、`PickShapeHandle2D`、`BeginShapeDrag2D`、`UpdateShapeDrag2D`、`EndShapeDrag2D`、`CancelShapeDrag2D`、`DraggingShape2D`、`ActiveShapeHandle2D`、`AddPolygonVertex`、`RemovePolygonVertex` 和 `HandleCanvasSecondaryPress` 驱动同一流程，检查器也为每种形状字段提供对应的可编辑控件。

## 3D 视口（#890）

在 3D 模式下，视口按游戏绘制的样子显示场景：一个 `gpu.View` 填满面板，用 render3d 绘制场景的 3D 构建结果，叠在它上面的画布绘制地面网格、原点坐标轴、选中轮廓和变换控件。构建结果来自 `scenemodel.Build`（`Scene3D`）：每个对象的内置图元（`mesh`）或 glTF 模型（`model`、`modelNode`、`modelMesh`）及其材质和纹理，包括实例化场景加入的对象。编辑器的 `Resolver` 实现了 `scenemodel.ModelResolver`、`sceneformat.MaterialResolver` 或 `scenemodel.ImageResolver` 时，模型、材质文件和图片通过它加载，否则从打开的项目的文件中加载。场景无法构建时（例如它引用的模型加载失败），视口角落会有一行文字说明原因（`Viewport3DError`），每个对象仍然显示包围盒。

- **光照与天空**：当版本 4 场景定义了光源或 `environment` 时，视口通过 `scenemodel.Renderer`（`PrepareScene`、`RecordShadows`、`DrawSky`、`ClearColor`）直接渲染场景自身的光源、阴影、天空（纯色、渐变或经由 `scenemodel.HDRResolver` 加载的 HDR 立方体贴图）、环境光/IBL、雾效、色调映射与曝光，与游戏渲染路径逐像素一致。场景既无光源也无环境时，视口回退到渐变天空与 `DefaultLights3D`。基于本包构建的编辑器仍可用 `SetLights3D`（`Editor.Lights3D`）覆盖光照，此时背景为场景的 `backgroundColor`。
- **着色**：工具栏的 3D Shading 组、`View > 3D Shading` 以及命令 `view.shading.lit`、`view.shading.unlit`、`view.shading.wireframe` 切换 `Shading3D`（`SetShading3D`）：Lit（受光）、Unlit（每个表面带纹理的基础色加自发光色，不受光照）和 Wireframe（受光，并在上面画出三角形的边）。着色是视图设置，不可撤销，也不会保存。
- **拾取**：按下会选中其下方的几何体。绘制网格或模型的对象按其三角形命中，经由构建结果上的 `scene3d` 拾取，所以在球体旁边、但在其包围盒以内按下会穿过它；没有几何体的对象按其包围盒命中，包围盒大小取自精灵帧，且只在按下处没有命中任何几何体时才参与拾取，因为这种包围盒的大小不随场景尺度变化，可能把相机或整个关卡都包在里面。按在实例化场景加入的几何体上会选中该实例。隐藏和锁定的对象照旧被跳过。
- **实时编辑**：只有场景的外观变化时（对象的外观、它在树中的位置、实例化的场景或覆盖项，或者项目文件）才会重新构建。变换控件拖动、撤销移动、打开的动画片段在播放头处的姿态，以及运行中游戏的改动，都直接移动已构建的节点，所以拖动时不会重新构建或上传任何网格。对象有几何体时，`ObjectCorners3D` 和 `ObjectAABB3D` 给出的包围盒是其几何体的边界，聚焦选中对象（`f`）也按这个边界取景，所以一米大小的模型会充满视图。
- **没有 GPU 时**：在不支持 GPU 视图的目标平台上、GPU 视图的第一帧之前以及设备丢失之后，画布改为绘制完整的线框视图：场景背景、网格、每个对象的包围盒（有几何体时为其边界）和变换控件。`Viewport3DOnGPU` 表明当前显示的是哪一种。

`OrbitCamera3D.SceneCamera` 把编辑器的环绕相机转换为 GPU 视图绘制所用的 `scene3d.Camera`，其视点、投影和裁剪面与画布投影所用的相同，所以叠加层与几何体对齐。

### 在 3D 视图中导航（#891）

导航只移动编辑器的相机（`Camera3D`，一个 `OrbitCamera3D`）：它从不算作编辑，不进入撤销历史，也不改动场景文件。相机直接跳到目标位置，不播放动画。

| 操作 | 鼠标 | 触控板或触摸屏 | 键盘或按钮 |
| --- | --- | --- | --- |
| 绕注视点环绕 | 拖动 | 单指拖动 | 坐标轴控件下方的“向左环绕 15°”和“向右环绕 15°”按钮 |
| 平移 | 中键拖动，或按住 Shift 拖动 | 双指滚动，或在触摸屏上双指拖动 | |
| 向注视点缩放 | 滚轮 | 捏合，或按住 Control 或 Command 滚动 | |
| 飞行 | 按住右键：移动指针环顾，W、A、S、D 移动，Q 和 E 下降和上升，按住 Shift 加速，滚轮调整速度 | | |
| 聚焦选中对象 | | | `f` 或视图栏的 Focus 按钮，按其边界取景；未选中任何对象时取景整个场景 |
| 轴向视图 | | 点按右上角坐标轴控件上的轴端 | `1` 前视、`ctrl+1` 后视、`3` 右视、`ctrl+3` 左视、`7` 顶视、`ctrl+7` 底视 |
| 透视或正交 | | | `5`，或视图栏中的 Persp 与 Ortho |
| 回到默认视图 | | | 视图栏的 Reset view 按钮 |

- **视图栏与状态栏**显示视图的名称：轴向视图的名称（Front、Top 等）或 Perspective，正交时附加“ · ortho”，不沿任何轴的正交视图为“User · ortho”（`ViewName3D`）。状态栏还会以度为单位显示偏航角和俯仰角。
- **正交**（`SetOrthographic3D`、`OrbitCamera3D.Orthographic`）以平行投影显示。视图高度为 `OrthoHeight`，即透视视图在注视点处显示的高度，因此切换时位于注视点的物体大小不变，缩放会改变这个高度。`Project`、`ScreenRay`、`SceneCamera`、拾取和变换控件在两种投影下行为一致：正交时每个像素的射线都沿视线方向，因此按下会选中其下方的物体，拖动变换控件时对象沿轴移动的距离等于指针沿该轴移动的距离。正交下的轴向视图就是真正的平面图或立面图。
- **飞行**（`BeginFly3D`、`FlyLook3D`、`FlyMove3D`、`EndFly3D`）让相机绕视点转动，并让视点和注视点一起移动。飞行期间 W、A、S、D、Q、E（无论是否按住 Shift）移动相机，而不执行它们平时的快捷键（变换控件的 W 和 S、擦除瓦片的 E）；平台支持时指针会锁定在视口内。飞行以透视方式观察，所以正交视图会切换为透视；既没有转动也没有移动相机的一次右键点击会保持正交。飞行速度 `FlySpeed3D` 以世界单位每秒计，初始为相机到注视点的距离，滚轮每次将其加倍或减半（`AdjustFlySpeed3D`）。
- **轴向视图**（`SetView3D`，命令 `view.3d.front`、`view.3d.back`、`view.3d.right`、`view.3d.left`、`view.3d.top`、`view.3d.bottom`，以及切换投影的 `view.3d.orthographic`）保持注视点和距离不变。前视沿 -Z 方向看，右视沿 -X 方向看，顶视垂直向下看，视图上方为 -Z。
- **按场景保存**：项目中的每个场景都会以上次离开时的相机打开。编辑器在打开另一个场景、保存场景和关闭窗口时把视图写入项目中的 `.game/editor.yaml`（`ViewStateFile`，见[文件格式](formats.md)）；以其他方式结束编辑器的应用需先调用 `SaveViewState`。没有保存过视图的场景以 `DefaultOrbitCamera3D` 打开。编辑器创建 `.game` 文件夹时会在其中写入一个忽略整个文件夹的 `.gitignore`，使视图不进入版本控制。较新版本编辑器写出的文件（`version` 更高）既不会被读取，也不会被覆盖。

### 在 3D 中放置模型（#893）

资源浏览器在图集下方列出可放到 3D 中的模型（`Models`）：先是内置基本形体，然后按路径列出当前打开项目的 glTF 模型，`game assets` 模型目标的输出即使尚未构建也会列出（`ModelEntry.Output`）。每个模型都有一张离屏渲染的缩略图（`ModelThumbnail`）：只有模型本身，从前上方取景，由 `DefaultLights3D` 照明，经由 scenemodel 在 NSGo 的软件 GPU 上绘制。每个模型在其缩略图第一次绘制时于 UI goroutine 上加载，与编辑器 `Resolver` 提供的一切资源相同，且不进入场景自身的模型缓存；渲染在 UI goroutine 之外进行，所以列表立即显示，图片随后补上。无法加载的模型没有缩略图。

- **放置工具**：按下一个模型会选中它（`SelectModel`、`SelectedModel`），在 3D 中还会启用放置工具，3D 工具栏在 Select 旁边显示 Place。之后在 3D 中按下，就会添加一个绘制该模型、以它命名的对象：位置是指针射线命中场景几何体的地方；没有命中时，是射线与地平面（高度为 `PlaceHeight3D` 的水平面）相交的地方（`PlacementAt3D`、`PlaceModelAt3D`）。对象落在该点上：它沿表面法线抬高其几何体在原点以下延伸的距离，所以放在地面上的单位立方体是立在地面上，而不是一半埋在地下。隐藏和锁定的对象会被跳过，与拾取相同。
- **吸附与对齐**：工具栏的 Snap 让地平面上的点的 X 和 Z 吸附到视口所绘网格的间距（`PlaceSnap3D`）；场景几何体上的点不吸附，因为网格间距可能比指针下的对象还宽；Align 让放在场景几何体上的模型转动，使其 +Y 轴与表面法线一致（`PlaceAlign3D`），地面高度字段设置 `PlaceHeight3D`。它们是视图设置，不是编辑（`SetPlaceOptions3D`）。
- **拖放**：把模型从资源浏览器拖到 3D 视口上，会按放置工具的规则把它放在落下的位置，不论当前是哪个工具。从其他应用拖到视口上的 `.gltf` 或 `.glb` 文件，只要位于项目文件夹内、且不在资源浏览器略过的文件夹中（名称以 `.` 或 `_` 开头的文件夹，以及 `testdata`），就会被放置（`DropModelFiles`），多个文件算作一步；项目之外的文件、其他类型的文件以及来自网页、没有路径的文件会被拒绝，原因显示在状态栏中。文件拖放不带位置，所以拖入的文件落在视图中心所指的地方。
- **一步操作**：每次放置都是一个可撤销的步骤，撤销会取回，重做会再次放置。播放时、当前图层被锁定或位于屏幕空间时，以及模型无法加载时，放置会被拒绝；在版本 1 的场景中放置模型会把场景提升到当前版本，与 `PlaceObject` 相同。

### 3D 光源、环境与场景相机（#895）

版本 4 场景自带 3D 光照、环境与 3D 相机对象，并可在 3D 视口和面板中直接编辑：

- **图标与拾取**：可见的方向光、点光、聚光和 3D 相机在 3D 视口叠加层中绘制清晰、按光源着色的图标（不再显示通用占位方块）；点击图标即可选中对应对象（`Pick3D`）。
- **可撤销的交互手柄**：选中光源或 3D 相机后会显示辅助线框与可拖拽手柄（`Handle3DKind`、`HandlePosition3D`、`PickHandle3D`、`BeginHandleDrag3D`、`UpdateHandleDrag3D`、`EndHandleDrag3D`、`CancelHandleDrag3D`）：
  - 方向光：方向箭头与指向手柄（`Handle3DLightDirection`）。
  - 点光：三个正交衰减球圆环与范围半径手柄（`Handle3DLightRange`）。
  - 聚光：方向手柄（`Handle3DLightDirection`）、射程手柄（`Handle3DLightRange`）以及独立的外锥角（`Handle3DLightOuterCone`）与内锥角（`Handle3DLightInnerCone`）圆环手柄。
  - 3D 相机：视锥线框、垂直视场角/正交高度手柄（`Handle3DCameraFOV`）与远裁剪面手柄（`Handle3DCameraFar`）。
  拖拽期间就地更新 GPU 视口且不重建网格，松手时作为单个可撤销步骤提交到 `Editor.History`。
- **场景环境面板（`EnvironmentPanelView`）**：通过 3D 视图栏的 **Env** 按钮、`View > Scene Environment` 或 `CmdEnvironmentPanel`（`EnvironmentPanelOpen` / `SetEnvironmentPanelOpen`）开关，以经过校验且可撤销的 `SetEnvironment` 编辑天空（`none`、`color`、`gradient`、`hdr`）、环境光颜色与强度、IBL 强度、距离雾与高度雾、色调映射（`none`、`reinhard`、`aces`、`neutral`、`agx`）及曝光（对低于版本 4 的场景会自动升级到版本 4，撤销时恢复原版本）。
- **检查器与创建辅助**：在 3D 视图下（或选中任何带光源/3D 相机的对象时），检查器显示 `LIGHT 3D`（`SetObjectLight`）与 `CAMERA 3D`（`SetObjectCamera3D`，切换 `current` 时在同一步撤销中原子清除其他相机的 `current`）。在 Go 中，`AddLight3D(kind)` 与 `AddCamera3D(proj)` 在当前世界空间图层放置预配置的光源和 3D 相机，`ProjectHDRs()` 列出当前打开项目中的 `.hdr`/`.pic` 天空文件。
- **透过场景相机观看**：按 `0`、点击 3D 视图栏的 **Camera** 按钮或执行 `CmdView3DCamera`（`SetLookThroughCamera3D` / `ToggleLookThroughCamera3D`）即可透过场景当前的 3D 相机（`ActiveSceneCamera3D`）观察 3D 视口，与游戏第一帧逐像素一致。环绕、平移、缩放、飞行或切换轴向视图会自动退出相机预览模式。

## 瓦片调色板（#874）

在 2D 视图中启用 Paint、Erase、Fill、Pick 或 Terrain 工具时，视口右侧会显示瓦片调色板。它把当前图层瓦片集的瓦片按瓦片 ID 顺序排成网格，每个瓦片按瓦片地图的瓦片尺寸显示（比面板还宽的瓦片会缩小到面板宽度）。这些瓦片是瓦片集图集中名称为瓦片 ID（`"1"`、`"2"`……）的帧，与场景格式查找瓦片的方式一致；其他帧不会列出。点击一个瓦片，或用方向键在瓦片之间移动，即可把它设为 Paint 和 Fill 写入的瓦片（`SetActiveTileID`）；所有方向键都按 ID 顺序逐个移动，左键和上键移到上一个瓦片，右键和下键移到下一个，而不是按网格的行移动。选中的瓦片带有一圈高亮，使用 Paint 或 Fill 时状态栏显示它的 ID。选择瓦片不算编辑，不进入撤销历史；之后的绘制是一个可撤销的步骤。

调色板跟随当前图层：图层改变时，调色板显示新图层的瓦片集；如果该瓦片集没有所选 ID 的瓦片，就选中它的第一个瓦片。没有内联瓦片地图的图层（没有瓦片地图，或使用外部 Tiled 或 LDtk 地图）、解析器无法解析的瓦片集、没有瓦片帧的瓦片集，都会显示一条说明原因的消息，而不是空网格。在 Go 中，`TilePalette()` 返回调色板为当前图层显示的内容：瓦片集、瓦片尺寸和瓦片，或那条消息。

对分层网格，调色板显示它的 YAML 图块集中的瓦片，图块集通过解析器的 `sceneformat.FileResolver`（例如 `FSAssets`）读取，瓦片 ID 按 `PaintTile` 接受的方式从 1 开始：瓦片 ID `n` 绘制图块集的局部瓦片 `n - 1`。调色板的标签和状态栏用局部 ID 称呼每个瓦片，也就是行和图块集的 `tiles[].id` 写的数字（`PaletteTile.Number`）。瓦片上方每个瓦片图层有一个切换按钮，用来选择 Paint、Erase、Fill 和 Terrain 修改的图层（`SetActiveTileLayer`、`ActiveTileLayer`，默认为第一个），H、V、D 切换按钮设置 Paint 和 Fill 放置瓦片时的翻转（`SetActiveTileFlip`、`ActiveTileFlip`），地形切换按钮选择 Terrain 绘制的地形集（`SetActiveTerrain`、`ActiveTerrain`），**Edit Tileset** 按钮会在图块集编辑器中打开该图层的 `*.tileset.yaml`。这些编辑按 `Encode` 保存的形式写入图层的行，每次都是一个可撤销的步骤；Fill 只连通瓦片和翻转都相同的单元格。

## 绘制、擦除与拾取瓦片（#875）

使用 Paint 或 Erase 时，用鼠标或单指拖动会绘制或擦除拖动经过的每一个格子，无论移动多快都不会漏格，整笔是一个可撤销的步骤，所以一次 Undo 就能撤回一次拖动画出的整行。单击或轻点只绘制或擦除它所在的那一格。使用 Paint 时，按住右键拖动会擦除。从地图外开始的一笔在进入地图后才开始绘制，没有改变任何格子的一笔不会记入历史，按住 Space 拖动则是平移视图。第二根手指会取消单指的一笔并恢复它改过的格子，所以双指平移永远不会绘制。

启用瓦片工具时，鼠标下的格子会显示对齐网格的轮廓：Paint 和 Fill 在格子里半透明地显示所选瓦片，Terrain 以翠绿轮廓半透明预览自动匹配的地形瓦片，Erase 把格子染成红色，按下会拾取时轮廓为琥珀色。

拾取会把某个格子里的瓦片设为要绘制的瓦片，调色板也会选中它：在任何瓦片工具下 Alt+单击（或 Alt+拖动），或使用 Pick 工具，拾取后会切换到 Paint。拾取空格子时所选瓦片保持不变。拾取不算编辑，不进入撤销历史。

在 Go 中，`BeginTileStroke(mode, x, y)`、`ContinueTileStroke` 和 `EndTileStroke` 在画布坐标上画一笔（`TileStrokePaint`、`TileStrokeErase`、`TileStrokePick`、`TileStrokeTerrain` 或 `TileStrokeEraseTerrain`），`CancelTileStroke` 撤回这一笔，`TileStrokeActive` 报告是否有一笔正在进行；绘制与擦除会拒绝锁定或隐藏的图层，选择工具或图层、修改图层、切换视图、保存、Undo、Redo 和 Play 都会先结束正在进行的一笔（Revert 则会取消它）。`PickTile(layer, col, row)` 拾取瓦片，`SetTileHover` 和 `ClearTileHover` 移动轮廓，`HoveredTile` 返回轮廓所在的格子。

## 地形绘制与图块集编辑器（#879）

分层瓦片地图（`*.tileset.yaml`）在运行时（`tilemap.ApplyTerrain`、`tilemap.SetTerrainTile`）与编辑器中共用同一套自动瓦片地形集：

- **地形画笔（`ToolTerrain`，快捷键 `t`）**：左键拖动放置地形（`TileStrokeTerrain`），右键拖动擦除地形（`TileStrokeEraseTerrain`），按与 `tilemap.Tilemap.SetAutotile` 完全一致的 4 位正交（`cardinal`）和 8 位角门控（`blob`）规则更新所经过的格子及其周围邻居。整笔（含放置/擦除的格子与所有受影响的邻居）记为一个可撤销步骤，一次 Undo 即可完整还原。在 Go 中，`PaintTerrain(layer, col, row, terrain)` 与 `EraseTerrain(layer, col, row, terrain)` 执行单格地形编辑。
- **图块集编辑器（`TilesetEditorPanelView`）**：可通过瓦片调色板的 **Edit Tileset** 按钮（`OpenActiveLayerTileset`）、文件浏览器的 **Edit Tileset** 按钮（`OpenTilesetProjectFile`）或 `OpenTilesetFile` / `OpenTilesetYAML` 打开 `*.tileset.yaml`。支持新建或删除 4 位（`cardinal`）和 8 位（`blob`）地形集（`AddTilesetTerrain`、`RemoveTilesetTerrain`、`SetTilesetTerrainMode`）、设置显式默认瓦片（`SetTilesetTerrainDefault`），以及通过点击中心、四条边（`N`、`E`、`S`、`W`）和 8 位 `blob` 模式下的四个角（`NW`、`NE`、`SW`、`SE`）为瓦片分配邻居掩码（`ToggleTilesetEdgeOrCorner`、`ToggleTilesetTileCenter`、`SetTilesetTileMask`）。点击角会自动满足角门控规则（启用角时自动启用相邻两条边；关闭边时自动清除依赖该边的角）。若中间点击产生的掩码已被另一个瓦片占用，编辑器会保持原瓦片不变，便于继续点击到达其他掩码或点击 **Reassign**（`ReassignTilesetEditMask`）一步重新分配。所有图块集编辑都可撤销，立即生效于地形画笔，并以规范 YAML 写回文件（`SaveTileset`、`SaveTilesetYAML`）。

## 3D GridMap 绘制（#899）

在 3D 视图（`ViewMode3D`）下，编辑器可以直接打开并在可调高度的地板网格上绘制模块化 3D [`*.gridmap.yaml`](world3d.md#模块化-3d-gridmap) 文件：

- **打开与关联**：在检查器中设置对象的 `gridMap` 字段（`SetObjectGridMap`），或直接打开一个 `*.gridmap.yaml` 文件（`OpenGridMap`、`OpenGridMapFS`、`OpenGridMapDoc`、`NewGridMap`）。在 3D 工具栏中切换到任一 GridMap 工具（**Paint**、**Erase**、**Pick** 或 **Box**）时，若尚未打开网格地图则会自动打开所选对象引用的 `gridMap`；`MoveFile` 也会同步跟踪打开的 GridMap 路径及其 `meshLibrary` 引用。
- **带 3D 缩略图的调色板**：在 3D 视图启用 GridMap 工具时，视口右侧会显示 `GridMapPaletteView`（`GridMapPalette()`），为 glTF `meshLibrary` 中的每个瓦片渲染等轴测着色 3D 缩略图，并提供当前层高与 24 朝向控制按钮。
- **层高与 24 种朝向**：按 `]` / `PageUp` 与 `[` / `PageDown`（`SetGridMapFloor`、`StepGridMapFloor`）将当前水平绘制平面上下移动一格。按 `X`、`Y`、`Z`（配合 `Shift` 反向旋转）或 `Q` / `Shift+Q`（`RotateGridMapOrientation`、`StepGridMapOrientation`、`SetGridMapOrientation`）在全部 24 种立方体正交朝向之间旋转当前瓦片（`world3d.RotateGridOrientation`）。
- **绘制、擦除、拾取与矩形填充**：指针悬停在 3D 视口上时会将视线射线投射到当前层高平面（`ScreenToGridMapCell`），并实时绘制悬停格预览（拖动 **Box** 填充时绘制虚线矩形预览）。单击或拖动会把经过的所有格子作为单个可撤销笔画绘制或擦除（`BeginGridMapStroke`、`ContinueGridMapStroke`、`EndGridMapStroke`、`CancelGridMapStroke`，按 `Escape` 取消）；**Pick**（或 Alt+单击）拾取指针下格子的瓦片与朝向；**Box** 在当前层高上拖出填充矩形（`FillGridMapBox`）。
- **撤销与规范保存**：每一次绘制、擦除、矩形填充与格子旋转（`PaintGridMapCell`、`EraseGridMapCell`、`FillGridMapBox`、`RotateGridMapCell`）都进入编辑器共用的 `History`（撤销/重做）。`Save`（`SaveGridMap`、`SaveGridMapFile`）通过 `world3d.GridMap.ToDoc` 与 `world3d.EncodeGridMap` 写出确定性的规范 `.gridmap.yaml`，保存后的文件可逐字节无损往返，并在 `scenemodel.Build` 中一致渲染。

## 矩形、直线、图章与随机瓦片画笔（#876）

四种扩展瓦片画笔同时支持单网格和分层内联瓦片地图：每次拖拽在松手前都会在 2D 视口中显示实时预览，按 **Escape**（或放下第二根手指）可无损取消且不改动场景，松手时将所有变更格子合并为 `Editor.History` 中的**单个可撤销步骤**：

- **矩形（`ToolRectTile`，快捷键 `u`）**：在两个格子之间拖拽以 `ActiveTileID` 填充矩形（`TileStrokeRect`、`FillTileRect`），或按住右键拖拽擦除矩形（`TileStrokeRectErase`、`EraseTileRect`）。
- **直线（`ToolLineTile`，快捷键 `l`）**：在两个格子之间拖拽以 Bresenham 直线绘制 `ActiveTileID`（`TileStrokeLine`、`DrawTileLine`），或按住右键拖拽沿直线擦除（`TileStrokeLineErase`、`EraseTileLine`）。
- **图章与区域剪贴板（`ToolStampTile`，快捷键 `c`）**：单击按左上角锚点盖章、拖拽按起点网格对齐平铺绘制矩形 `TileStamp` 图案（`TileStrokeStamp`、`StampTiles`），同时保留分层瓦片地图每格的翻转位以及复制区域中的空（`0`）格子。图章既可从瓦片调色板框选矩形生成（`SelectPaletteStamp`、`SelectPaletteStampRange`，或在调色板中先点击起始瓦片，再切换 **Rect** 或按住 Shift 点击对角瓦片），也可在地图上右键拖拽选区（`TileStrokeSelectRegion`、`SetTileSelection`）后执行**复制**（`mod+c`、`CopyTileRegion`、`CopySelectedTileRegion`）、**剪切**（`mod+x`、`CutTileRegion`、`CutSelectedTileRegion`，单个撤销步骤）和**粘贴**（`mod+v`、`PasteTileRegion`）。
- **随机（`ToolRandomTile`，快捷键 `n`）**：按固定种子（`SetRandomBrushSeed`、`RandomBrushSeed`）从多个带权重瓦片中确定性地加权抽取绘制（`SetRandomBrushTiles`、`SetRandomBrushTileWeight`、`ToggleRandomBrushTile`）。在瓦片调色板中点击瓦片即可加入或移出权重池，并通过每项的 `-`/`+` 按钮调节权重与 Seed 输入框设置种子；按住右键拖拽可擦除。

在 Go 中，`TilePreview()` 返回进行中画笔在松手前的实时预览格子，`TileStamp` / `TileClipboard` / `TileSelection` 管理图案与选区；四种画笔在单网格与分层 YAML 瓦片地图上行为一致，并保持确定性的 YAML 往返保存。

## 场景树（#861）

编辑器左侧的场景树面板（`SceneTreeView`）列出打开场景的各个图层，并在每个图层下列出其对象及嵌套的子对象，相当于 Godot 的 Scene 面板和 Unity 的 Hierarchy。每次编辑和撤销后它都会随之更新。按下对象所在的行会选中该对象；在任一视口中选中对象时，会选中它的行并展开其上层的各行；按下图层的行会把它设为当前图层。所有对象编辑对嵌套子对象与顶层对象一样有效：`FindObject` 和 `SelectObject` 在整棵树中按 ID 查找对象，检查器编辑所选的子对象，其变换在父对象的空间中编辑，因此移动工具会把它的原点放到按下的位置，3D 操纵器沿世界坐标轴移动它。

- **在树中移动**：把一行拖到另一行上，它就成为该对象的最后一个子对象；拖到那一行的上边缘或下边缘附近，则放在该对象之前或之后；拖到图层的行上，则移到该图层的顶层。行的上下文菜单可以把对象在同级之间上移或下移、设为上方同级对象的子对象、移出父对象，或移到另一个图层。在 Go 中，`ReparentObject(id, parentID, index)`、`MoveObjectToLayer(id, layer, index)`、`ReorderObject(id, index)` 和 `DropObject(id, targetID, place)` 完成同样的操作，每次都是一个可撤销的步骤；index 小于 0 或超出末尾时，对象放在最后。
- **世界变换**：被移动的对象保持其世界变换。它在新父对象下的局部变换会在 2D 中重新计算（`x`、`y`、`rotation`、`scaleX`、`scaleY`），撤销会把每个字段精确还原；翻转过的对象会在原来的轴上保留负的缩放。当对象本身或其新旧祖先设置了 3D 字段时，它的深度（`posZ`、`scaleZ`）也会保持。当其中某个对象设置了 3D 旋转（`rotX`、`rotY`、`rotZ`）时，2D 与 3D 视图对 `x` 和 `y` 指向哪里不再一致，此时只保持 3D 世界变换：对象在 3D 视口中保持不动，在 2D 视口中则可能移动。以下移动会被拒绝：把对象放到它自己或其后代之下、嵌套超过 `MaxObjectDepth` 层，或者在新父对象下得到的变换无法写成平移、旋转和缩放，例如把精灵放到一个经过旋转且沿单轴拉伸的父对象下。在世界图层与屏幕图层之间移动时，保留的是数值，而不是对象在屏幕上的位置。
- **重命名**：F2、双击或上下文菜单中的“Rename”会在所选对象的行内编辑其名称；按回车或离开输入框即保留修改，按 Escape 则放弃。“Change ID”通过 `SetObjectID(oldID, newID)` 编辑其 ID：在同一步中改写场景中引用该对象的连接，以及它实例化的场景中的对象（`enemy_1/body` 变为 `boss/body`），还有打开的动画片段的目标及其各轨道的目标，并拒绝场景已在使用的 ID，包括实例化或继承的场景带来的 ID。实例化或继承本场景、并按旧 ID 覆盖该对象的其他场景文件不会被改写；这类引用请用 `game mv` 移动或手动修改。
- **眼睛与锁**：行上的眼睛按钮在两个视口中隐藏该对象及其后代，锁按钮使两个视口中的按下操作不再拾取它，按下会落到它下面的对象上（`SetObjectHidden`、`SetObjectLocked`）。两个视口都不会移动隐藏或锁定的对象：不显示它的 3D 操纵器，移动工具也不会移动它。二者都属于编辑会话：它们不是编辑，撤销不会影响它们，也从不写入场景文件；运行（Play）时绘制所有对象，打开另一个场景时会清除它们。隐藏或锁定的对象仍可在场景树中选中，并在检查器中编辑。

3D 视口会绘制并拾取所有对象，包括嵌套的子对象：`Objects3D` 按深度优先列出它们，并给出各自父对象的 ID；子对象的几何体和包围盒由其祖先的变换确定位置（[3D 视口](#3d-视口890)）。

## 3D 导航网格烘焙（#901）

在 3D 视图模式下，编辑器可直接从当前打开场景的静态几何体烘焙 Recast 风格的 3D 导航网格（`navbake3d`），并在 3D 视口上实时预览：

- **NavMesh 烘焙面板**（`NavMeshPanelView`，可通过 3D 工具栏的 **NavMesh** 按钮、**View > 3D NavMesh Bake...**、命令 `CmdNavMeshPanel` 或 `SetNavMeshPanelOpen` 开关）：配置代理名称、半径、身高、可攀爬高度（`MaxClimb`）、最大可行走坡度（`MaxSlope`）、体素单元尺寸（`CellSize`）、单元高度（`CellHeight`）和输出 `.navmesh.yaml` 路径（`NavMeshBakeConfig` / `SetNavMeshBakeConfig`）。打开包含 `navigation` 配置的项目时，面板会自动以 `game.yaml` 中的设置为初始值。
- **烘焙与场景关联**（**Bake** 按钮、`CmdBakeNavMesh3D` 或 `BakeNavMesh3D`）：从当前打开场景的 glTF 模型（支持 `modelNode` 与 `modelMesh`）、内置 `mesh` 基本形体、静态非触发器 `collider3d` 形状（跳过动态/运动学 `body3d`、触发器与屏幕空间图层）以及可选回调 `Editor.OnBakeNavMesh3DGeometry` 提供的地形高度图、`GridMap` 或物理碰撞体收集静态 3D 几何体；以原子方式写出确定性的 `.navmesh.yaml`，在未设置或路径变化时以单步可撤销操作更新 `SceneDoc.NavMesh`（`SetSceneNavMesh`，亦可在场景设置检查器中编辑），并立即加载烘焙结果到视口预览。重新打开已关联 `.navmesh.yaml` 的场景时会自动加载预览（`LoadNavMesh3D`）。
- **3D 视口叠加层**（工具栏 **Nav Overlay** 按钮、面板 **Overlay** 开关、**View > Show 3D NavMesh**、命令 `CmdNavMeshOverlay` 或 `ShowNavMesh3D` / `SetShowNavMesh3D`）：通过 `Camera3D` 在 3D 场景视口上绘制半透明青色可行走多边形填充、细节边和清晰的多边形外轮廓。

## 场景标签页、场景文件与未保存的修改（#866，#873）

编辑器在视口上方的标签栏（`SceneTabsView`）中为每个打开的场景保留独立标签页，因此作者可以一边编辑被实例化或被继承的场景，一边编辑使用它的关卡，而不会丢失撤销历史：

- **单标签页独立状态**：每个标签页（`Tabs`、`ActiveTab`、`TabCount`、`SelectTab`、`SelectTabByID`）独立保存自己的 `SceneDoc`、`BuiltScene`、`History`、选中状态（`SelectedID`）、当前图层与瓦片选择、2D 和 3D 视口变换（`View2D`、`Camera3D`）、场景树隐藏/锁定会话标记、`OnSave` 回调以及未保存标记（`TabDirty`）。切换标签页绝不会清空任何标签页的撤销历史；在一个标签页中撤销不会改动其他标签页。
- **打开与复用已有标签页**：从文件浏览器（`*.scene.yaml`、`*.scene.yml` 或 `autoload.scenes` 中列出的场景）、问题面板、搜索与引用面板、命令面板、`NewScene`、`OpenScene` 或 `OpenFile` 打开场景时，会新开一个标签页（若当前只有未动过的初始无标题标签页则直接替换它）；如果该场景已在某个标签页打开，则直接切换到该标签页。**Open Instanced Scene**（`OpenInstancedScene`，位于实例化对象的检查器、场景树右键菜单、File 菜单与命令面板）与 **Open Base Scene**（`OpenBaseScene`，位于继承场景的 Scene Settings 检查器、File 菜单与命令面板）同样在新标签页中打开所引用的场景，或在已打开时直接切换过去。
- **保存时实时重建依赖标签页**：保存场景（`Save`、`SaveFile`、`SaveSceneAs`、`SaveTab`、`SaveAll`）后，会自动重建其他所有实例化或继承了场景的打开标签页（包括 `BuiltScene` 与 `Scene3D`），因此实例与继承对象无需重新打开就能立刻显示最新改动。`MoveFile` 也会同步更新所有打开标签页的路径和引用。
- **共享项目、插件、Play 与关闭标签页**：项目文件（`game.yaml`）、插件与 Play 模式在所有标签页之间共享，插件与 Play 作用于当前活动标签页（Play 运行期间禁止切换或关闭标签页）。关闭未修改的标签页（`CloseTab`、`RequestCloseTab`、`CmdCloseTab` / `mod+w` 或标签页关闭按钮）会立即关闭；关闭有未保存修改的标签页会弹出提示：Save（仅保存该标签页并重建依赖它的标签页）、Discard（仅丢弃该标签页的修改，保留共享项目文件的修改）或 Cancel。关闭最后一个标签页后会替换为一个干净的无标题场景。

工具栏的“File”菜单包含：“New Scene”，把一个最小的有效场景（以文件名命名，大小与项目的 display 或窗口相同，带一个 world 图层）写到作者给出的、从项目根目录算起的路径并在新标签页打开（`NewScene`）；“Save As”，把当前场景另存到另一个路径，并让该文件成为当前标签页的文件（`SaveSceneAs`）；“Revert”，把当前场景恢复为其文件中保存的样子（`RevertScene`）；以及“Open Instanced Scene”、“Open Base Scene”和“Close Tab”。没有 `.yaml` 或 `.yml` 扩展名的路径会加上 `.scene.yaml`（以 `.scene` 结尾的路径加上 `.yaml`），`game check` 和 `game index` 就是按这个名称识别场景的。New Scene 和 Save As 都不会覆盖其他文件，也不会把场景写到项目自身文件的名称 `game.yaml` 和 `nsgo.yaml` 下。Revert 是一个可撤销的步骤，撤销即可找回被恢复掉的修改。

`SceneDirty` 报告当前活动场景是否与打开时或上次保存时的内容不同，它像 `ProjectDirty` 对 `game.yaml` 那样比较编码结果，所以撤销回到已保存的状态后，场景又会变为未修改；`TabDirty` 检查指定标签页，`AnySceneDirty` 报告是否存在任何有未保存修改的标签页。若 `MoveFile` 改写了打开的场景的文件，场景保持原来的已修改或未修改状态；若它只改写了打开的场景中的引用而没有改写其文件（例如 `game mv` 不读取的场景：名称不是 `.scene.yaml`，也没有任何文件引用它），场景会变为已修改，保存后即可保留新的路径。每个有未保存修改的标签页都会显示 `*` 标记，工具栏显示当前活动场景的路径（`SceneLabel`），只要任意打开的标签页或项目有未保存的修改（`HasUnsavedChanges`）就会显示 `*`，窗口标题也随之变化（`WindowTitle`）。窗口标题由 `EditorView` 设置，因此基于本包构建的编辑器（例如游戏自己的编辑器）应通过 `Editor.AppName` 给出自己的名称，而不是自行设置标题；标题以该名称结尾（为空时为“NSGo Scene Editor”）。“Save”按钮、菜单栏的 File > Save、命令面板中的 Save 和 Mod+S（`Save`，即 `CmdSave` 命令）只保存当前活动场景与共享项目中修改过的内容；未修改的文件保持原样，注释也会保留。有任意打开的标签页或项目存在未保存修改时，切换项目、退出编辑器和关闭窗口都会先询问：“Save”保存所有已修改的标签页和项目（`SaveAll`）后继续，“Discard”丢弃修改后继续，“Cancel”保留修改并停留。丢弃项目的修改时，由项目决定的状态也会像撤销这些修改时一样恢复，例如通过这些修改新增的音频总线进行的试听会停止。强制退出（如注销时）无法等待回答，不会询问。

## 图层面板（#862）

场景树下方的图层面板（`LayersView`）按绘制顺序列出打开场景的各个图层，最先绘制的在最上面，每行显示图层的空间（`world` 或 `screen`）和瓦片地图：内联网格显示其瓦片集与尺寸（`tiles 16x12`），引用顶层瓦片地图的显示其名称，外部 Tiled 地图显示其路径。按下图层的行会把它设为当前图层：放置工具和各瓦片工具作用于它，瓦片调色板显示它的瓦片，状态栏显示它的名称，图层隐藏或锁定时名称后面会注明 `hidden` 和 `locked`；在场景树中按下图层的行效果相同。在 Go 中，`Layers()` 返回面板显示的内容。

- **添加与移除**：+ 按钮添加一个名为 `layer_N`、绘制在所有其他图层之上的世界图层，并把它设为当前图层（`AddLayer(name, space)`：取场景所绘制图层中最大的 order（包括其继承的场景的图层），并把图层放在文件末尾）。垃圾桶按钮移除当前图层（`RemoveLayer(name)`），连同它的对象和瓦片地图，以及场景中从其对象发出或指向其对象的连接；撤销会把它们全部放回原处，场景保存后与之前逐字节相同。移除当前图层后，文件中接替其位置的图层（没有时为前一个图层）成为当前图层。
- **重命名**：双击或上下文菜单中的“Rename”会在行内编辑名称；按回车或离开输入框即保留修改，按 Escape 则放弃（`RenameLayer(oldName, newName)`）。空名称和其他图层已有的名称会被拒绝。当前图层和图层的锁定状态会跟随新名称。
- **顺序**：把一行拖到另一行的上半部或下半部，图层就放到那个图层之前或之后；也可以用上下文菜单中的“Move Layer Up”和“Move Layer Down”（`MoveLayer(name, index)`，index 是在绘制顺序中的位置）。图层在文件中移到它要绘制在其后或其前的那个图层旁边；只有当它现有的 `order` 值无法让它绘制在那里时，才会改为那个相邻图层的值；其他图层的位置和 order 都不变。
- **空间**：上下文菜单中的“World Space”和“Screen Space”设置图层的 `space`（`SetLayerSpace(name, space)`）；其对象的坐标保持不变。
- **可见性**：眼睛按钮通过写入图层的 `visible` 字段来隐藏或显示图层（`SetLayerVisible(name, visible)`）。隐藏时写入 `visible: false`；显示时不设置该字段，即取默认值，因此隐藏再显示一个图层会得到相同的文件。这是场景本身的设置，游戏也会读取，并且是可撤销的编辑。隐藏的图层在两个视口中都不会被拾取，它的对象即使已被选中也不会在视口中被移动（不显示 3D 操纵器，移动工具也不拖动它们），与在场景树中隐藏的对象相同。
- **锁定**：锁按钮在本次编辑会话中锁定图层，使视口不能操作它（`SetLayerLocked(name, locked)`）：按下操作会越过它的对象，落到下面的对象上；视口中不会移动它的对象（不显示 3D 操纵器，移动工具也不拖动它们）；它是当前图层时，放置、绘制、擦除和填充工具不做任何修改，并在状态栏中说明原因。与场景树中对象的锁一样，它不是编辑，撤销不会影响它，也从不写入场景文件，打开另一个场景时会清除它；各面板和检查器仍可编辑该图层及其对象。

在继承另一个场景（`inherits`）的场景中，与基础场景某个图层同名的图层是对该图层的扩展，它的名称、order、空间和可见性归基础场景和本场景的 `layerOverrides` 所有：重命名、移动、隐藏这类图层或设置其空间，把图层重命名为基础场景图层的名称，以及用这样的名称添加图层，都会被拒绝，并给出说明原因的错误，面板也会在它的行上停用眼睛按钮、拖动和这些菜单项；移除这类图层只会移除本场景添加到其中的对象。继承本场景并引用其图层名称的其他场景文件不会因重命名而被改写。任何会使场景无法通过校验、或无法与其实例化或继承的场景一起解析的图层编辑，都会在进入历史记录之前被拒绝。

## 动画编辑器

编辑器的动画面板在时间轴上为任何已注册属性打关键帧，并把片段保存为 [`animation` 资源文件](resources.md#animation)，由 [animclip](animclip.md) 在游戏中播放（相当于 AnimationPlayer）。打开片段时，面板显示在视口下方。

- **打开片段**：工具栏的 Animate 按钮为选中的对象新建一个一秒长的片段。在 Go 中，`NewClip(target, name, length)` 作用相同；`NewClipFromAsepriteTag(target, tag)` 根据目标对象图集中的 Aseprite 标签生成匹配帧序列、精确时长和循环设置的片段；`NewClipFromFrames(target, name, frames, fps, loop)` 按固定帧率从一组图集帧生成逐帧片段；`OpenClipFile(path, target)` 和 `OpenClipYAML(data, target)` 打开片段文件，并按组件注册表检查（拼错的属性会带着文件名打开失败）；`CloseClip` 关闭片段。当目标对象有精灵图集时，面板的 Flipbook 行也可以直接选择 Aseprite 标签（From Tag）或帧率（From Frames）创建片段。`nsgo game editor scene.yaml clip.animation.yaml [object-id]` 打开一个场景并选中对象 `object-id`（省略时为场景中的第一个对象），片段文件存在时在该对象上打开片段，面板的 Save Clip 按钮会写回该文件。
- **打关键帧**：选择一个属性（对象字段，如 `x`、`alpha` 或 `sprite.frame`，或已注册的组件字段，如 `body.speed`），按 Key 在播放头处以该属性在场景中的当前值打关键帧；`KeyProperty(target, property, at)` 作用相同。选择 `sprite.frame` 时会出现帧选择器，列出目标对象图集中的帧（`ClipTargetFrames`），可直接为任意帧打关键帧（`KeySpriteFrame(target, frame, at)`）。选中的是其他对象时，关键帧会进入以该对象为目标的轨道。显式指定片段所属对象的轨道与目标为空的轨道视为同一条轨道，两条轨道驱动同一对象同一属性的片段会被拒绝。布尔关键帧保持其值直到下一个关键帧，`sprite.frame` 始终离散步进且不做插值。
- **编辑关键帧**：每条轨道是一行显示其关键帧的通道；`sprite.frame` 轨道以跨越各自持续时间的帧缩略图格显示每个关键帧（`ClipFrameEntry`）。按下关键帧即选中它并把播放头移到该处，然后可以编辑数值轨道的时间、值和插值方式（`linear`、`smooth` 或 `step`），或编辑 `sprite.frame` 轨道的图集帧（`SetKeyFrame`）和帧时长（`-10ms` / `+10ms`，`FrameDuration`、`SetFrameDuration`），或删除它；在 Go 中对应 `SetKeyValue`、`MoveKey`、`SetKeyInterp`、`DeleteKey` 和 `RemoveClipTrack`。值会按属性检查：布尔为 0 或 1，整数为整数值，并且在字段范围内；`sprite.frame` 帧名必须存在于目标对象的图集中。
- **片段设置与事件**：面板可编辑片段的名称、长度和循环（`SetClipName`、`SetClipLength`、`SetClipLoop`），并在播放头处添加具名事件（`AddClipEvent`、`DeleteClipEvent`）。
- **拖动与预览**：拖动条移动播放头（`SeekClip`；在循环片段的末尾显示最后的关键帧），Play 在 `game.Loop` 上播放片段（`PlayClip`、`PauseClip`，或用 `StepClip` 手动步进）。此时视口（2D 与 3D 均如此）显示按片段摆好姿态的场景副本，使用的 `animclip.Player` 和 `BindScene` 与游戏相同，因此正在编辑的场景不会被写入动画值，片段在游戏中的播放效果也与预览一致。场景无法播放的片段（例如轨道目标对象已被删除）仍保持打开，`ClipError` 会说明原因。
- **撤销**：每一次片段编辑，包括打开和关闭，都与场景编辑共用同一个撤销/重做历史。

`SaveClipYAML` 和 `SaveClipFile` 输出确定性的 YAML，因此打开后再次保存的片段不会改变。

### 精灵状态机图（#1054）

打开 `*.states.yaml` 文件（通过文件浏览器、命令面板或 `OpenStatesFile` / `OpenStatesYAML` / `OpenStatesProjectFile`）会在视口下方打开**状态机**图面板：

- **状态图与当前状态**：每个状态以节点卡片展示名称、片段元信息（帧数、帧时长、循环/单次）与精灵缩略图，配有带标签的有向过渡箭头以及 `Any state` 通配徽章。预览期间当前状态节点实时高亮，预览卡片显示当前播放帧。
- **按键驱动（Drive with keys）**：开启 **Drive with keys** 后，预览通过与游戏相同的 `animclip.InputDriver` 和 `animclip.StateMachine` 绑定到项目的输入动作（如 `move_left`、`move_right`、`jump`、`attack`、`hurt`），在 `game.Loop` 的固定步长上推进，使交互预览与运行时播放逐帧一致。
- **出边列表（`LEAVING <STATE>`）**：选中任意状态即可查看其所有出边过渡（含 `Any state` 中断）及条件表达式；所有图编辑操作（`AddStatesState`、`RemoveStatesState`、`AddStatesTransition`、`RemoveStatesTransition`、`SetStatesEntry`、`SetStatesNodePos`、`SetStatesDoc`）均进入编辑器的撤销/重做历史，并以确定性规范 YAML 写回（`SaveStatesYAML` / `SaveStatesFile`）。

## 编辑器插件（#492）

插件为场景编辑器添加自己的面板、自己组件类型的检查器以及 2D 视口工具，作用与 Godot 的 `EditorPlugin` 相同。Go 无法在所有平台上于运行时加载代码，因此插件是编译进编辑器程序的 Go 代码：编辑器程序用 `Editor.RegisterPlugin` 注册插件，项目在其[项目文件](project.md)中列出插件名即可启用：

```yaml
editor:
  plugins: [health]
```

已注册但未被当前项目列出的插件保持关闭；`editor.plugins` 的顺序就是插件面板和工具出现的顺序。`OpenProject` 会在状态栏中列出项目要求、但本程序编译时没有包含的插件（`MissingPlugins`）。`nsgo game editor` 不注册任何插件，也不注册任何组件类型，因此有自己插件的游戏要构建自己的编辑器命令。`nsgo game generate editor` 会在游戏的 `main` 包中写出一个这样的命令，它带 `gameeditor` 标签构建、代替游戏本身，用游戏的 `Components` 和插件调用 `editorcmd.Main`，`nsgo game editor` 会在项目上运行它（[命令行](cli.md#generate)）。`editorcmd` 包是两者共用的命令行：`New(Options)` 用给定的组件类型、插件、名称和库构建编辑器并设置 `ProjectAssets`，按这些组件类型以及注册表本身没有注册的内置物理组件（`body2d`、`collider2d`、`trigger2d` 及其 3D 形式）检查场景，`Open(ed, args)` 像 `nsgo game editor` 一样打开项目、场景或剪辑，`Main` 和 `Run` 解析命令行，没有参数时打开工作目录中的项目，并显示窗口，窗口大小为 1120×680 点，除非 `Options.WindowWidth` 和 `Options.WindowHeight` 设置了其他大小。手写的编辑器命令也以同样的方式调用 `editorcmd.Main`：

```go
// cmd/editor/main.go：游戏自己的编辑器命令。
package main

func main() {
	editorcmd.Main(editorcmd.Options{
		Components:     health.Components(),
		Plugins:        []editor.Plugin{healthplugin.Plugin()},
		AppName:        "Field Medic Editor",
		RequirePlugins: true, // 项目列出本命令没有的插件时拒绝打开
	})
}
```

`editor.Plugin` 有一个 `Name`，规则与 `game.yaml` 中其他名称相同（不含空白和控制字符），以及三个列表：

- **面板**（`Panel`）：停靠在视口左侧（`DockLeft`，位于资源浏览器之后）、检查器右侧（`DockRight`）或视口下方（`DockBottom`）的视图，上方显示其 `Title`。`View(cx, ed, rev)` 在面板出现时构建一次，只要其插件处于启用状态，面板就保持挂载并保留其状态，不受其他面板增减的影响；与编辑器自带的面板一样，它在响应式函数中读取 `rev`，以跟随编辑器状态的每一次变化。没有面板的停靠区不占空间。
- **检查器**（`Inspector`）：检查器用 `View(cx, ed, rev, objectID)` 在组件标题下方绘制 `Component` 指定的组件，取代该组件的通用字段。`Editor.ComponentField` 读取一个字段；场景未设置该字段时返回其注册的默认值，对象没有该组件时返回 false。与通用字段一样，检查器的控件应在 `Playing` 返回 true 时禁用，编辑器拒绝的编辑应通过 `Editor.Report` 显示在状态栏中。两个已启用的插件绘制同一个组件时，`editor.plugins` 中排在前面的插件生效。
- **工具**（`PluginTool`）：工具栏中位于内置 2D 工具旁的一个按钮。当它是当前工具时（`SetPluginTool`，此时 `Tool` 为 `ToolPlugin`），在视口上按下会以画布坐标调用 `Press(ed, x, y)`（用 `CanvasToWorld` 转换为世界坐标，用 `CanvasToScreen` 转换为屏幕空间图层的坐标），拖动会依次调用 `OnDragBegin(ed, x, y)`（返回 `true` 接管拖动）、`OnDragMove(ed, x, y)`、`OnDragEnd(ed, x, y)` 与 `OnDragCancel(ed)`（`BeginPluginDrag2D`、`UpdatePluginDrag2D`、`EndPluginDrag2D`、`CancelPluginDrag2D`、`PluginDragging2D`），指针移动会调用 `OnHover(ed, x, y, active)`（`SetPluginHover2D`、`ClearPluginHover2D`），设置了 `Draw` 时它会在编辑模式下绘制在场景之上。工具至少需要提供 `Press` 或 `OnDragBegin` 之一。选择其他工具或关闭插件都会结束该工具。

插件只通过编辑器的方法修改场景，例如 `SetComponentField`、`PlaceObject` 和 `MoveObject`，或者在 `Editor.History` 上执行自己的 `Command`，因此它的每一次编辑都与其他编辑一起撤销和重做。`History.Group(label, fn)` 把 `fn` 做的编辑记录为一个步骤，按一次撤销即可撤回；`fn` 失败时撤销这些编辑且不记录任何步骤。编辑器的 MCP 服务器（[mcp](mcp.md#编辑器工具)）就这样应用智能体的每次请求。`RegisterPlugin` 会拒绝没有名称或名称已注册的插件、缺少 ID、标题、标签、组件或函数的面板、检查器或工具、含控制字符的 ID 或组件名、未知的停靠位置，以及 ID 或组件重复的面板、检查器或工具。测试 ID 用 `-` 连接插件名和 ID（`plugin-panel-<plugin>-<id>`、`tool-<plugin>-<id>`、`inspector-plugin-<plugin>-<component>`），因此一起使用的插件，其名称和 ID 不应连接成相同的字符串。

`SetPluginEnabled(name, on)` 把已注册的插件加入 `editor.plugins` 或从中移除，作为项目的一次可撤销编辑，由工具栏的“Save”按钮写入；移除最后一个插件时，如果 `editor` 小节中已没有其他内容，就会一并移除该小节（包括作者自己写的 `editor: {}`），撤销会将其恢复。`RegisteredPlugins`、`ActivePlugins` 和 `PluginEnabled` 报告程序包含哪些插件、哪些已启用。

### 随游戏发布带插件的编辑器

游戏把编辑器作为自己模块中、与游戏并列的又一个命令来发布，在项目目录中用 `go run ./cmd/editor` 运行。把插件放在只有编辑器命令导入的独立包中，这样发布的游戏不会链接编辑器；把组件类型放在游戏和编辑器都导入的包中，这样两者用同一组字段检查场景：

| 包 | 导入 | 内容 |
| --- | --- | --- |
| 组件包 | `sceneformat` | 组件类型（`ComponentType`）、供 `ParseWith` 和 `SetComponentRegistry` 使用的注册表，以及按默认值读取组件的辅助函数（`ComponentRegistry.Resolve`） |
| 插件包 | `editor` 和组件包 | 返回 `editor.Plugin` 的 `Plugin()` |
| 游戏（`main`） | 组件包 | 用 `sceneformat.ParseWith` 和注册表加载场景 |
| `cmd/editor` | `editor`、组件包和插件包 | 注册组件类型和插件，然后打开项目 |

`game.yaml` 在 `editor.plugins` 下列出插件，因此游戏的编辑器打开项目时插件总是启用的。不应在缺少插件时启动的编辑器命令，设置 `editorcmd.Options.RequirePlugins`，或在 `OpenProject` 之后检查 `MissingPlugins`，不为空就失败退出。项目有 `cmd/editor` main 包时，`nsgo game editor` 会用 `go run ./cmd/editor` 运行该命令，就像运行 `game generate editor` 写出的命令一样，并把同样的命令行（`-library` 以及项目、场景文件、剪辑和对象 ID）传给它，所以 `cmd/editor` 应当用 `editorcmd.Main` 或 `editorcmd.Run` 解析命令行。没有导入 `editorcmd` 的 `cmd/editor` 不会被运行：`nsgo game editor` 会说明这一点，并自己打开项目。

[Field Medic](https://github.com/nsgo-dev/game/tree/main/examples/fieldmedic)（`examples/fieldmedic`）就是按这种方式组织的可运行游戏。它的病人带有 `health` 组件（`health` 包），它的 `cmd/editor` 注册了 `health` 插件（`healthplugin` 包）：一个停靠在检查器右侧的 Ward 面板，列出每个病人的生命值，其中的 Heal All 按钮在 `Editor.History` 上执行一个可撤销步骤，一次治好所有病人；一个 `health` 组件的检查器，带生命值和上限两个字段以及剩余生命值的条形图；以及一个 Admit 工具，在视口按下处放置一个病人，并在工具启用时为每个病人画出轮廓。它的测试在编辑器中打开游戏自己的项目，操作面板、检查器和工具。编辑器自身的测试中也有一个更小的 `health` 插件（`packages/game/editor/plugin_test.go`）。

## 库面板（#493）

当 `Editor.Library` 设为一个包库（`library.Open`）时（`nsgo game editor -library <索引>` 就会这样设置），资源浏览器旁会出现一个面板，列出库中每个包的类型、版本、描述以及打开的项目已安装的版本。面板上的 Install、Update 和 Remove 按钮调用 `InstallPack` 和 `RemovePack`：把包的文件写到 `game.yaml` 旁边，在项目中记录包及其新增的自动加载条目和插件，然后保存项目；插件包列出的已注册插件会立即启用。正在运行、没有打开项目或项目有未保存修改（`ErrProjectDirty`）时它们会拒绝执行；由于写入的文件无法撤销，它们会清空撤销历史。从 https 库获取包时不会阻塞事件循环，包到达之前按钮显示为忙碌状态（`FetchingPack`）。库的格式以及安装会写入什么，见[资源与插件库](library.md)。

## 项目设置面板（#907）

打开项目后，工具栏的 **Settings** 按钮（`ProjectSettingsOpen` / `SetProjectSettingsOpen`）会在视口下方切换显示 `ProjectSettingsPanelView`，完全通过 `EditProject` 编辑 `game.yaml`：每一次修改都经过 `project.Validate` 校验，纳入撤销/重做历史，并在点击 **Save**（`SaveProject`）时同时写回 `game.yaml` 与同步更新的 `nsgo.yaml`：

- **常规（General）**：编辑 `name`、`id` 和 `appVersion`（`SetProjectName`、`SetProjectID`、`SetProjectAppVersion`）。
- **窗口（Window）**：编辑 `window.title`、`width`/`height`、`minWidth`/`minHeight`/`maxWidth`/`maxHeight` 尺寸限制、`resizable` 和 `appearance`（`SetProjectWindowTitle`、`SetProjectWindowSize`、`SetProjectWindowLimits`、`SetProjectWindowResizable`、`SetProjectWindowAppearance`）。
- **显示（Display）**：开关可选的 `display` 小节（`SetProjectDisplayEnabled`），并编辑虚拟分辨率 `width`/`height`、`mode`（`fit`、`fill`、`stretch`、`expand`）、`integerScale`、`pixelSnap` 与 `barColor`（`SetProjectDisplaySize`、`SetProjectDisplayMode`、`SetProjectDisplayIntegerScale`、`SetProjectDisplayPixelSnap`、`SetProjectDisplayBarColor`）。
- **自动加载（Autoload）**：选择 `autoload.mainScene`（`SetProjectMainScene`），并增改删 `autoload.scenes` 与 `autoload.resources` 条目（`AddProjectAutoloadScene`、`UpdateProjectAutoloadScene`、`RemoveProjectAutoloadScene`、`AddProjectAutoloadResource`、`UpdateProjectAutoloadResource`、`RemoveProjectAutoloadResource`），文件路径可从当前项目文件列表中搜索选择（`ProjectSceneFiles`、`ProjectResourceFiles`）。
- **物理（Physics）**：编辑 `physics.gravity2D`、`physics.gravity3D`、`physics.solverIterations2D` / `solverIterations3D` 以及具名碰撞层 `physics.layers`（`SetProjectPhysicsGravity2D`、`SetProjectPhysicsGravity3D`、`SetProjectPhysicsSolverIterations`、`AddProjectPhysicsLayer`、`RemoveProjectPhysicsLayer`）。
- **编辑器插件（Plugins）**：为每个已注册插件（`RegisteredPlugins`）提供调用 `SetPluginEnabled` 的开关，同时列出 `game.yaml` 中声明但当前编辑器程序未内置的插件（`MissingPlugins`）并支持移除。

输入、音频与导出设置各自拥有独立面板，此处不重复提供。任何被 `project.Validate` 拒绝的值都会在对应字段旁显示校验错误，且不会改动项目或撤销历史。在打开项目首次保存之前（`ProjectCanonicalSavePending`），面板顶部会显示明确提示，说明保存将以规范 YAML 形式写出 `game.yaml`，不会保留注释。

## 音频总线混音器（#909）

打开项目后，工具栏的 **Mixer** 按钮（`AudioMixerOpen` / `SetAudioMixerOpen`）会在视口下方切换显示 `AudioMixerPanelView`，通过 `EditProject` 直接编辑 `game.yaml` 的 `audio` 小节：每一次修改都经过 `project.Validate` 校验，纳入编辑器的撤销/重做历史，并在点击 **Save**（`SaveProject`）时写回文件：

- **主音量与总线**：调节主音量滑块（`SetAudioMasterVolume`），新增总线（`AddAudioBus`）、重命名（`RenameAudioBus`）、删除（`DeleteAudioBus`）、调整顺序（`MoveAudioBus`），以及调节每条总线的音量滑块（`SetAudioBusVolume`）。在未设置主音量时删除最后一条总线会一并移除空的 `audio` 小节，撤销可将其恢复。
- **单总线 DSP 效果链**：为每条总线添加（`AddAudioBusEffect` / `AddAudioBusEffectKind`）、移除（`RemoveAudioBusEffect`）、重排（`MoveAudioBusEffect`）和编辑（`SetAudioBusEffect`）`project.Effect` 支持的全部六种效果（`lowPass`、`highPass`、`delay`、`reverb`、`gain`、`bitcrush`）。数值控件将参数约束在 `project.Validate` 允许的范围内（配合 `ClampAudioEffect`），Go API 则会拒绝越界值且不改动项目或历史。
- **实时试听**：从当前项目中选择音频文件（`ProjectAudioFiles`、`SelectAudioFile`），点击 **Audition**（`AuditionAudioBus`、`StopAudition`、`SetAuditionLoop`）即可经由 `project.File.ConfigureMixer` 配置好的编辑器 `sound.Mixer` 通过选中总线播放。调节主音量或总线音量会立即作用于正在播放的音频而不中断播放；修改效果链或撤销/重做会立即通过 `sound.Mixer.ProcessSource` 重新处理并播放，使听感与游戏运行时完全一致。

## 输入映射编辑器（#908）

打开项目后，工具栏的 **Input** 按钮（`InputMapOpen` / `SetInputMapOpen`）会在视口下方切换显示 `InputMapPanelView`，通过 `EditProject` 直接编辑 `game.yaml` 的 `input` 小节，提供完整的撤销/重做与 `project.Validate` 校验：

- **动作与绑定**：新增（`AddInputAction`）、重命名（`RenameInputAction`）和删除（`DeleteInputAction`）动作，并添加（`AddInputBinding`）、编辑（`SetInputBinding`）或移除（`RemoveInputBinding`）`game/input` 支持的全部绑定类型：`key`、`keyCode`、`gamepadButton`、`mouseButton`、`touchRegion` 与 `mouseRegion`。重命名动作时会在同一步可撤销操作中原子更新 `input.axes` 中所有引用该动作的轴，撤销与重做也会保持一致。
- **按键捕获**：点击 **Press a key**（`BeginKeyCapture`，`project.KindKey`）或 **Press code**（`project.KindKeyCode`）进入捕获模式，将下一次硬件按键（`CaptureKeyEvent`）直接绑定到当前动作，点击 **Cancel**（`CancelKeyCapture`）可退出而不改动项目。
- **虚拟显示尺寸上的区域绘制与拖拽**：区域画布按项目的 `display` 虚拟分辨率等比缩放（未配置 `display` 时回退到 `window` 尺寸，`InputMapDisplaySize`），绘制全部 `touchRegion` 和 `mouseRegion` 区域；可拖拽已有区域移动、拖拽选中区域右下角手柄调整大小，或在空白处拖拽绘制新区域（`BeginRegionDrag` / `UpdateRegionDrag` / `EndRegionDrag` / `SetInputRegion`），每次拖拽作为单个可撤销的 `EditProject` 步骤提交。
- **动作轴、运动轴与手柄摇杆轴**：**Axes** 标签页可编辑动作轴（`AddInputAxis`、`SetInputAxis`、`DeleteInputAxis`）、设备运动传感器轴（`AddInputMotionAxis`、`SetInputMotionAxis`、`DeleteInputMotionAxis`）和手柄模拟摇杆轴（`AddInputGamepadAxis`、`SetInputGamepadAxis`、`DeleteInputGamepadAxis`）。`BuildInputActions` 通过 `project.File.BindInput` 将当前项目的输入映射绑定到实时的 `*input.Actions`。

## 本地化面板（#917）

本地化面板（`LocalizationPanelView`）在视口下方以表格形式并排编辑各语言的 YAML 字符串表：行为消息键，列为项目语言。

- **打开**：点击工具栏的 **Strings** 按钮（打开项目时加载 `<项目>/locales`，否则创建含 `en` 语言的内存表），或在 Go 中调用 `OpenLocalizations(dir)` / `NewLocalizations(dir, locales...)`。`CloseLocalizations` 关闭面板。
- **键与语言**：支持增删语言列（`AddLocalizationLocale`、`RemoveLocalizationLocale`）和消息键行（`AddLocalizationKey`、`RemoveLocalizationKey`），并自动将语言标签规范化为 BCP-47 形式（例如 `zh_hans` -> `zh-Hans`）。
- **普通文本与 CLDR 复数**：可将键设为复数模式（`SetLocalizationKeyPlural`），编辑全部六个 CLDR 复数类别（`zero`、`one`、`two`、`few`、`many`、`other`，对应 `SetLocalizationPlural` / `SetLocalizationPluralForm`），或用 `SetLocalizationString` 编辑普通字符串。
- **缺失翻译高亮**：译文为空（或复数键缺少 `other` 回退形式）的单元格会以警告边框和 `Missing` 标记高亮；可通过 `LocalizationMissing`、`LocalizationMissingCount` 和 `LocalizationIsMissing` 查询。未翻译的键在保存时会被省略，以便运行时 `l10n.Catalog` 沿 BCP-47 链自动回退。
- **撤销与保存**：所有本地化编辑都接入 `Editor.History`（撤销/重做）。点击 **Save Strings**（`SaveLocalizations` / `SaveLocalizationsTo`）通过 `l10n.EncodeYAML` 将每种语言以确定性规范 YAML 写入 `<dir>/<locale>.yaml`，并清理已从工作区移除的语言文件。

## 问题面板（#913）

打开项目后，视口下方的问题面板（`ProblemsView`）会在 UI 协程之外对项目目录运行 `assetcheck.CheckWith`（使用编辑器的组件注册表）和 `project.Check`，并在状态栏实时显示问题总数（`Problems: N`）。打开项目（`OpenProject`）、保存场景（`SaveFile`）、保存项目（`SaveProject`）、保存动画片段（`SaveClipFile`）、移动文件（`MoveFile`，包括撤销与重做）以及更换组件注册表（`SetComponentRegistry`）后都会自动重新检查。在编辑器之外修改了文件后，可点击面板上的 Check 按钮（`CheckProblems`）手动触发检查。

每行显示一条问题的文件路径、从 1 开始的行号（有行号时）和错误信息，与在同一项目上运行 `nsgo game check` 报告的问题一致，并补充 `project.Check` 发现的项目级问题（例如 `nsgo.yaml` 与 `game.yaml` 不一致、已安装包的文件缺失，或非 `.scene.yaml` 扩展名的自动加载场景无效）。点击某一行（`OpenProblem`）时，若该文件是场景（`*.scene.yaml`、`*.scene.yml` 或 `autoload.scenes` 中列出的场景）则直接打开该场景；若是其他项目文件或无法解析的场景，则在文件浏览器中选中该文件（`SelectedFile`、`SelectFile`）。问题位于已打开的场景时只选中该文件，不会重新加载场景；有未保存的修改时打开另一个场景，会像文件浏览器那样先询问保存、丢弃还是取消。在 Go 代码中，可从任意协程安全调用 `Problems`、`ProblemCount`、`ProblemErr`、`CheckingProblems` 和 `WaitForProblems` 读取或等待后台检查结果。

## 导出与发布构建（#916）

打开项目后，工具栏的 **Export** 按钮（`ExportPanelOpen` / `SetExportPanelOpen`）会在视口下方切换显示 `ExportPanelView`，用于在编辑器中直接编辑 `game.yaml` 的 `export` 小节并执行发布构建：

- **预设与输出目录**：编辑 `export.output`（`SetExportOutput`），添加、删除和重排各平台预设（`AddExportPreset`、`RemoveExportPreset`、`MoveExportPreset`），并仅从各平台合法目标 `project.TargetsOf(platform)` 中选择构建目标（`SetExportPresetTargets`、`ToggleExportPresetTarget`）。所有修改都通过 `EditProject` 完成（经 `project.Validate` 校验并支持撤销/重做），在 `export` 为空时自动清理该小节，点击 **Save**（`SaveProject`）时写出规范 YAML 并同步 `nsgo.yaml`，与 `gameproject targets` 保持一致。
- **鸣谢（Credits）预检**：面板打开、刷新（`CheckExportCredits`）或开始构建前，都会执行与 `game release` 相同的 `tooling/credits` 扫描。任何缺少鸣谢信息的资源都会连同其对应的 `*.import.yaml` 边车路径一并列出，并在补全前阻止发布构建；补全鸣谢后刷新即可清除告警，成功构建时会在输出目录写入 `CREDITS.md`。
- **构建、演练与输出流**：支持构建全部预设或勾选指定目标（`ExportSelectedTargets`），并可配置分发渠道（`SetExportChannel`）、发布说明文件（`SetExportReleaseNotes`）以及 **Dry Run** 开关（`SetExportDryRun`）。`StartExportBuild` / `RunExportBuild` 与 `nsgo game release` 共用同一套发布命令规划器，因此演练构建会在 UI 协程之外将完全一致的 `nsgo build` 命令行流式输出到面板的 Output 区域，非演练构建成功后自动打开解析出的输出目录（`OpenOutputFolder` / `OpenExportOutputFolder`）。
## 项目搜索与查找引用（#914）

打开项目后，文件浏览器的 **Search** 按钮（`SearchOpen` / `SetSearchOpen`）会在视口下方切换显示搜索与引用面板（`SearchPanelView`），用于在项目内快速跳转或回答“哪些文件引用了当前文件”：

- **全项目搜索（`Search`、`SetSearchQuery`、`SearchResults`）**：索引项目中的所有文件（遵循与 `ProjectFiles` 和 `refs.Scan` 相同的目录遍历规则）并解析所有场景文件（`*.scene.yaml`、`*.scene.yml`、自动加载场景以及被其他文件引用的场景）。不区分大小写的查询（支持空格分隔的多词组合查询）可匹配文件名与路径（`SearchFile`）、文件头或 `game.index.yaml` 中的稳定 UID（`SearchUID`）、场景名称，以及场景内的对象 ID 与显示名称（`SearchObject`，含嵌套子对象）、组件类型（`SearchComponent`，含覆盖项中的组件）、预制件引用（`SearchPrefab`）和实例化/继承的场景路径（`SearchSceneRef`）。
- **跳转到结果（`OpenSearchResult`）**：激活非场景文件的结果会在文件浏览器中选中它（`SelectedFile`）。激活场景文件或场景对象的结果会打开该场景（同步 `ScenePath` 与 `SelectedFile`）并选中匹配对象（`SelectedID`），嵌套子对象也直接选中；该子对象无法选中时回退选中其顶层祖先。如果该场景已经打开，则直接就地选中对象，不会从磁盘重新加载，也不会丢失未保存编辑与撤销历史。
- **查找引用（`FindReferences`、`ShowReferences`、`ShowSelectedFileReferences`、`References`、`OpenReference`）**：在文件浏览器中选中任意文件或目录并点击 **Find Refs**，即可基于 `refs.Scan` 列出所有入向引用，按 `refs.Scan` 报告的顺序逐行展示引用文件、从 1 开始的行号、字段名和原始值。点击某条引用会打开引用它的场景（若该行归属于某个顶层对象则自动选中该对象），或在文件浏览器中选中非场景引用文件。
- **实时索引刷新（`RefreshSearch`）**：对当前已打开场景的内存编辑（`PlaceObject`、`SetObjectName`、`DeleteObject`、撤销/重做）会立即更新活动搜索结果；保存场景、项目、动画片段、数据表或本地化文件、安装/移除包，以及移动文件（`MoveFile` 及其撤销/重做，同时自动跟踪 `ReferencesTarget`）都会自动失效并刷新索引；编辑器外部的磁盘改动既可通过轻量目录指纹自动感知，也可点击 **Refresh**（`RefreshSearch`）强制重扫。
## 运行游戏与输出面板（#910）

在保留原有编辑器内 **Play** 预览（`Play`、`PlayIn`、`Stop`）的同时，编辑器可以在桌面端直接通过 `go tool game dev` 运行打开的项目，并将输出实时显示在视口下方停靠的输出面板（`OutputPanelView`）中：

- **运行与停止**：点击工具栏的 **Run** 按钮（或按 **F5**）即可在打开的项目目录中启动 `go tool game dev`（`RunGame` / `RunGameWith`）。开启 **Current Scene** 开关（`SetRunStartAtOpenScene`）或传入 `RunGameOptions{StartAtOpenScene: true}` 时，会将当前打开的场景作为 `-scene <路径>` 与 `NSGO_GAME_SCENE`（`hotreload.SceneEnv`）传给游戏。游戏运行期间按钮切换为 **Stop Game (Running)**（**Shift+F5** / `StopGame`），停止时会干净地结束子进程及其启动的全部进程树（Unix 上通过独立进程组结束，Windows 上通过 `taskkill /T /F` 结束）。卸载 `EditorView`、切换项目（`OpenProject`）或调用 `Close()` 也会自动停止正在运行的游戏。自定义工具和测试可注入 `Editor.RunGameCommand`。
- **输出 / 日志面板**：停靠在视口下方（`OutputPanelView`，可通过工具栏 **Output** 按钮或 `SetOutputPanelOpen` 开关），在 UI 协程之外实时流式显示运行中游戏的标准输出（`OutputStdout`）和标准错误（`OutputStderr`），并保留编辑器自身带时间戳的状态与错误历史（`OutputEditor`）。连续多条编辑器错误都会保留在日志中，不会被下一条覆盖。
- **筛选、清空与 `file:line` 跳转**：切换 **Errors**（`SetOutputErrorsOnly`）只看错误日志，点击 **Clear**（`ClearOutput`）清空日志，点击包含 `file:line` 的日志行（`OpenOutputEntry`）时，若该文件是场景（`*.scene.yaml`、`*.scene.yml` 或 `autoload.scenes` 中列出的场景）则在编辑器中打开，否则在文件浏览器中选中该文件。
## 快捷键、菜单栏与命令面板（#920）

所有常用编辑器操作统一注册在同一个命令注册表中（`Editor.Commands`、`Editor.RegisterCommand`、`Editor.ExecuteCommand`），由单一数据源同时驱动键盘快捷键、桌面应用菜单栏和命令面板：

- **跨平台快捷键与文本焦点保护**：快捷键使用 NSGo 的 `"mod"` 修饰键（macOS 为 Command，Windows/Linux 为 Ctrl，可通过 `FormatShortcut` 格式化显示），并全部使用 `core.KeyNotEditing` 和 `core.MenuNotEditing` 绑定，因此当任意文本输入框拥有焦点时（例如检查器的 Name 输入框或搜索框），输入字符或按下 Delete/Backspace/Undo/Save 绝不会误触编辑器快捷键：
  - 文件与编辑：`mod+s`（保存）、`mod+z`（撤销）、`mod+shift+z`（重做）、`mod+d`（复制选中对象）、`delete` / `backspace`（删除选中对象）、`mod+c`（复制瓦片区域）、`mod+x`（剪切瓦片区域）、`mod+v`（粘贴瓦片区域）。
  - 2D 工具：`v`（选择）、`m`（移动）、`p`（放置）、`b`（绘制瓦片）、`e`（擦除瓦片）、`g`（填充瓦片）、`t`（地形）、`u`（矩形瓦片）、`l`（直线瓦片）、`c`（图章瓦片）、`n`（随机瓦片）。
  - 3D 变换控件与视图：`w`（平移）、`r`（旋转）、`s`（缩放）、`shift+c`（碰撞体手柄）、`alt+2`（2D 视图）、`alt+3`（3D 视图）、`View > 3D Shading` 下的 Lit、Unlit、Wireframe 三个 3D 着色命令（无快捷键）、`f`（聚焦选中对象：2D 下通过 `FrameSelection2D` 调整独立编辑器视图 `View2D` 而不改动保存的 `Doc.Camera`，3D 下调整 `Camera3D`）、`1`、`ctrl+1`、`3`、`ctrl+3`、`7` 和 `ctrl+7`（3D 前视、后视、右视、左视、顶视和底视）、`5`（3D 透视或正交）以及 `0`（透过场景相机观看），这些只在 3D 视图中生效、`View > Scene Environment`（开关场景环境面板）、`mod+k`（命令面板）。
  - 项目：`f5`（播放 / 停止）、`mod+r`（运行游戏：优先调用 `Editor.OnRunGame`，未配置且已挂载作用域时回退到编辑器内运行）、`mod+,`（项目设置）、`mod+shift+u`（HUD 与菜单布局）、`mod+shift+e`（导出与发布）、`mod+shift+m`（性能分析器）、`mod+shift+b`（检查问题）。
- **桌面菜单栏**：`EditorView` 通过 `desktop.SetApplicationMenu`（`Editor.ApplicationMenu`）注册原生应用菜单栏（`File`、`Edit`、`View`、`Project`），以实时可用/勾选状态和对应平台的快捷键提示暴露同一套命令；已启用插件的工具列在 `View > Tools` 下，已启用插件的命令列在 `Project > Plugins` 下。
- **命令面板（`ui.CommandDialog`）**：按 `mod+k`（或点击工具栏 **Commands** 按钮 / `View > Command Palette...`）即可打开 `CommandPaletteView`，支持按子串排序与模糊匹配搜索全部内置与自定义命令、已启用插件命令、已启用插件工具（通过 `SetPluginTool` 激活）、已启用插件面板（通过 `FocusPluginPanel` 聚焦）以及当前项目的场景和文件（通过 `OpenProjectFile` 打开场景或选中文件）。
- **插件命令**：插件可通过 `Commands []PluginCommand` 声明带可选 `Shortcut` 的命令。`RegisterPlugin` 会规范化快捷键（`NormalizeShortcut`），并在改动编辑器状态之前拒绝任何在 macOS 或 Windows/Linux 上与内置命令、自定义命令或其他已注册插件冲突的快捷键。
## 资源导入设置与重建（#915）

在文件浏览器中选中图片（`.png`、`.jpg`、`.jpeg`、`.webp`）、音频（`.wav`、`.ogg`、`.mp3`、`.flac`）、字体（`.ttf`、`.otf`）或 3D 模型（`.gltf`、`.glb`）文件时（`SelectedFile`，自动排除构建产物和 `*.import.yaml` 边车文件），检查器（`InspectorView`）会显示该资源的导入设置与鸣谢信息：

- **边车与目标默认值**：`InspectAssetImport` 会读取资源旁的 `*.import.yaml` 边车，并与 `game.yaml` 的 `assets` 小节中匹配目标的默认配置合并展示（图片支持 `filter`、`format`、`mipmaps`、`srgb`、精灵表 `slice` 与帧枢轴 `pivot`；音频支持 `format`、`rate`、`channels`、`bitrate`、`peakDB`/`rmsDB` 响度归一化与 `loop`；字体支持 `size`、`padding` 与 `chars`；模型支持 `scale`、`animations`、`lodHysteresis` 与 `lods`），同时展示通用的 `credit` 鸣谢小节（`author`、SPDX `license`、`source` 与 `notice`）。
- **交互式精灵表切分网格与帧枢轴预览（#880）**：选中图片时，导入面板会在透明棋盘格上渲染实时预览画布（`DrawImageImportPreview`），叠加显示切分网格（`width`、`height`、`margin`、`spacing`、`count`）以及每个切分格子（或未切分整图）的可拖拽枢轴标记。作者可以直接在预览画布上拖拽枢轴（`BeginImagePivotDrag` / `UpdateImagePivotDrag` / `EndImagePivotDrag` / `CancelImagePivotDrag`）、点击默认枢轴预设（`TL`、`Center`、`Bottom`），或按数值编辑/重置 `pivot.default` 与单格 `pivot.cells`（`SelectImportSliceCell`、`SetImageImportPivot`、`SetImageImportDefaultPivot`、`SetImageImportCellPivot`）。每次拖拽在松手时作为单个可撤销步骤写入确定性边车；点击 **Reimport** 或 **Build Assets** 后，打包出的 TexturePacker JSON 会写入对应枢轴并由 `sprite.LoadAtlas` 精确加载为 `Frame.PivotX` 与 `Frame.PivotY`。
- **交互式 glTF/GLB 模型预览与动画播放（#897）**：在文件浏览器中选中 `.gltf` 或 `.glb` 模型时，导入面板会使用轨道相机渲染该模型（`DrawModelPreview`、`RenderModelPreview`、`OrbitModelPreview`、`PanModelPreview`、`ZoomModelPreview`、`ResetModelPreviewCamera`），实时反映当前生效的导入 `scale`，并展示模型的结构与统计信息（`InspectModelImport`）：原始与缩放后的包围盒、总三角形与顶点数，以及节点、网格、材质与动画列表。每个动画都可以选择、播放、暂停、停止、循环或拖动时间轴定位（`SelectModelPreviewAnimation`、`PlayModelPreviewAnimation`、`PauseModelPreviewAnimation`、`StopModelPreviewAnimation`、`SeekModelPreviewAnimation`、`StepModelPreviewAnimation`），同时支持节点 TRS 变换、骨骼蒙皮与变形目标动画。修改 `scale`、`animations`、`lodHysteresis` 或 `credit` 会通过 `importsettings.Encode` 写入确定性边车并显示读取该文件的 `game.yaml` 模型目标，使下一次 `game assets` 运行仅重建该模型。
- **校验写入、撤销与单字段重置**：每次编辑都通过 `importsettings.Encode` 严格校验并原子写入边车文件，作为单个可撤销步骤进入 `Editor.History`（`EditAssetImportSettings`、`SetImageImport*` / `SetAudioImport*` / `SetFontImport*` / `SetModelImport*` 以及 `SetAssetImportCredit`）。**撤销（Undo）** 会逐字节恢复编辑前的边车内容（若此前无边车则删除新建文件）；**重置（Reset）**（`ResetAssetImportField`）将单个覆盖项恢复为 `game.yaml` 目标默认值，当不再有任何覆盖项或鸣谢时自动删除空边车文件。
- **Reimport、Build Assets 与热重载**：检查器中的 **Reimport** 按钮（`ReimportAsset`）与工具栏中的 **Build Assets** 按钮（`BuildAssets` / `BuildAssetsWith`）会在 UI 协程之外运行 `nsgo game assets`，并将标准输出和标准错误实时流式写入停靠的 **Output** 面板（`OutputPanelView`）。构建完成后，`ReloadAssets` 会从磁盘重新加载发生变化的图集与纹理到 `Editor.Resolver`（就地更新 `sceneformat.AtlasMap` 或已有 `*sprite.Atlas`，并在实现或配置了 `AssetReloader` / `OnReloadAssets` 时回调通知），无需重启编辑器即可在资源浏览器和视口中立即生效，并自动重新运行问题检查。自定义工具与测试可注入 `Editor.AssetsCommandRunner`。

## 性能分析器面板与帧耗时条（#912）

在编辑器内运行游戏或通过 MCP 连接正在运行的游戏（`game_perf_readout`、`game_perf_zones`、`game_perf_reset`）时，编辑器会在游戏视口下方显示实时帧耗时条，并可展开严格遵循编辑器设计规范令牌（`docs/design/editor/PlayMode.dc.html` 与 `Spec.dc.html`）的完整性能分析器面板：

- **运行模式 `FRAME TIME · LAST 60 S` 帧耗时条（`ProfilerStripView`）**：在编辑器内 Play 模式或已连接远程游戏（`ConnectProfiler`）期间始终显示在游戏视口下方。显示来自 `game_perf_readout` 的 `avg`、`p99` 与 `allocs/frame`，绘制 `8.3 ms`（`over 8.3 ms`）与 `16.7 ms`（`missed 16.7 ms`）阈值线及图例、`−60 s` / `−30 s` / `now` 时间轴，并在游戏通过 `debug.Overlay.NoteFrameCause` 标注了原因（或发生 GC 卡顿标记为 `"GC pause"`）时以徽章标出尖峰及其原因。点击耗时条上的 **Profiler** 按钮即可打开完整性能分析器面板。
- **连接运行中的游戏**：可通过 `ConnectProfilerMCP`（基于官方 MCP Go SDK 的任意 `mcp.Transport`）、`ConnectProfilerCommand`（启动 `go tool nsgo mcp`）、`ConnectProfilerScope`（已挂载 `mcp.Bridge` 的进程内 `*core.Scope`）或 `NewBridgeProfilerClient` 创建客户端并传给 `Editor.ConnectProfiler`。挂载作用域后，编辑器会按 `Editor.ProfilerPollInterval`（默认 `250 ms`）轮询 `game_perf_readout` 和 `game_perf_zones`，也可通过 `Editor.PollProfiler` 按需轮询。未连接外部客户端时，内置 Play 模式（`Play` / `PlayIn`）也会自动使用 `debug.Overlay`、`debug.Profiler` 和 `debug.GCMonitor` 采集帧耗时与区间数据。
- **完整性能分析器面板（`ProfilerPanelView`）**：可通过耗时条、工具栏 **Profiler** 按钮、`Project > Profiler`、命令面板或 `mod+shift+m`（`CmdProfiler` / `SetProfilerOpen`）开关。面板提供实时指标卡片与可切换的时间序列图表（`SetProfilerMetric`），涵盖 **Frame**（叠加 Update、Draw、总耗时与 GC 卡顿标记及 8.3 ms / 16.7 ms 参考线）、**FPS**、**Allocs & GC**（每帧分配次数、实时堆内存与 GC 卡顿）与 **Memory**（受跟踪资源内存与预算对比及分组统计），右侧列出按耗时排序的 `game_perf_zones` 命名计时区间表。
- **重置与 Chrome/Perfetto Trace 导出**：点击 **Reset**（`Editor.ResetProfiler`）会向已连接的游戏调用 `game_perf_reset` 并清空编辑器已记录的历史帧与区间表。点击 **Export Trace**（或调用 `Editor.ExportProfilerTrace` / `Editor.ExportProfilerTraceFile`，或通过 `Editor.OnExportProfilerTrace` 接管输出）会通过 `debug.Profiler.WriteChromeTrace` 将已记录的帧与区间导出为合法的 Chrome/Perfetto Trace Event Format JSON。
## 开始界面（#922）

不带参数运行 `go tool game editor` 时会打开开始界面（`StartScreenView`，可通过 `SetStartScreenOpen` 切换），视觉与交互遵循 `docs/design/editor/Start.dc.html`、`FirstRun.dc.html` 第 1 步以及 `Spec.dc.html` 设计 Token：

- **最近项目（`RecentProjects`、`ParseRecentProjects`、`EncodeRecentProjects`）**：打开项目时会把名称、目录路径和 UTC RFC3339 `lastOpened` 时间戳写入位于所有项目之外的每用户严格版本化 YAML 文件（默认通过 `DefaultRecentProjectsPath` 存在 `<UserConfigDir>/nsgo-game/recent-projects.yaml`，或由 `Editor.RecentProjectsPath` 指定）。左侧栏的 **RECENT** 列表仅在存在最近项目后出现，显示相对时间，并对磁盘上已不存在目录或 `game.yaml` 的条目（`Missing`）打上缺失标记并提供移除按钮（`RemoveRecentProject`），重启编辑器后仍然保留。
- **打开目录（`BrowseAndOpenProject`）**：通过 NSGo 原生目录选择器（`core.Scope.PickDirectories`）或可注入的 `Editor.PickProjectFolder` 钩子选择项目目录并打开。
- **从模板新建项目（`ProjectTemplates`、`StartCreateProject`）**：模板卡片与 `All` / `2D` / `3D` 筛选标签直接从 `game new` 的模板注册表动态填充，因此新增模板无需修改编辑器代码即可自动出现。项目名称、父目录（默认 `~/games`）和可选模块路径会在输入时实时校验（`ValidateNewProjectInputs`）：检查是否仅含 `a–z`、`0–9` 和 `-`、检查父目录写入权限，以及目标目录是否已存在（已存在时提供 **Open it** 和如 `dungeon-run-2` 的可用名称建议）。点击 **Create** 会在 UI 协程之外运行 `game new`（含 `go get` 和 `go mod tidy`），将标准输出与错误实时展示在界面中，成功后自动打开生成的 `game.yaml`。
- **打开演示（`OpenDemo`、`NewDemo`）**：无需在磁盘上创建或打开项目，直接进入内置的演示房间场景。
- **干净切换项目与未保存修改提示**：从开始界面，或稍后通过 `File > Open Project...`（`mod+o`）、`File > New Project...`（`mod+n`）和 `File > Start Screen` 打开另一个项目时，会干净地替换当前项目、场景、动画片段、过场动画、数据表、本地化工作区、骨骼绑定和撤销历史。只要当前会话存在未保存修改（`HasUnsavedChanges`），编辑器统一的未保存修改确认框就会先弹出，由用户选择 **Save**（`ConfirmSaveAndSwitch`）、**Discard**（`ConfirmDiscardAndSwitch`）或 **Cancel**（`CancelPendingSwitch`）。

## 3D LOD 与遮挡体可视化（#904）

在 3D 视图（`View3D`）中，视口会运行与游戏运行时一致的 `scenemodel.Build` + `render3d.OcclusionBuffer` + `render3d.Queue` 评估流程（打开项目时自动使用 `scenemodel.NewAssets`，也可注入 `Editor.ModelResolver`）：

- **LOD 叠加层（`ShowLODOverlay` / `SetShowLODOverlay`）**：按当前选中的 LOD 级别为每个多级 LOD 对象着色，并在视口中绘制 `LOD<n>` 标签；3D 检查器同时显示选中对象当前的活动 LOD（`ObjectLOD3D`、`ObjectLODCount3D`）。在资源导入检查器中修改模型的 `lods` 或 `lodHysteresis` 会立即使 3D 构建缓存失效，切换效果即时可见。
- **遮挡体可视化与编辑（`ShowOccluders` / `SetShowOccluders`、`SetObjectOccluder`）**：为每个可见遮挡体绘制品红色线框及 `OCC` / `OCC BOX` 标签（包括显式 `box` 遮挡体与 `model` 遮挡体），以弱化虚线标示被遮挡体剔除的对象（`ObjectOccluded3D`），并在 3D 检查器中提供可撤销的 Occluder 模式（`none` / `model` / `box`）、盒体尺寸和双面开关控件（必要时自动将版本 1 场景升级为版本 2）。
- **RenderStats 叠加层（`ShowRenderStats` / `SetShowRenderStats`、`RenderStats3D`）**：在 3D 视口右上角实时显示 `render3d.RenderStats`（`Draws`、`Visible`、`Occluded`、`Tris`、`Verts` 及各多级 LOD 对象当前选中的 `LOD` 级别），并可在 3D 工具栏中与 LOD、Occluders 叠加层一同独立开关。

## 远程场景树与实时检查器（#911）

当接入了 [`game/mcp`](mcp.md) 的游戏带着 `-automation` 运行时，编辑器会作为 MCP 客户端连接（桌面端通过 `github.com/modelcontextprotocol/go-sdk/mcp` 启动 `go tool nsgo mcp`，也可通过 `ConnectRemote` 或 `Editor.RemoteTransport` 传入任意 `mcp.Transport`），实时查看和调校运行中的游戏：

- **连接运行中的游戏**：在 **Remote** 标签页开启 **Auto-connect on Run**（`SetRunWithAutomation`）或调用 `RunGameWith(RunGameOptions{Automation: true})`，即可向 `nsgo game dev` 传递 `-automation` 并在游戏启动后自动连接；也可直接调用 `ConnectRemote` / `ConnectRemoteCommand` 连接到已在运行的游戏。`NewRemoteBridgeServer(bridge, run)` 将 `*mcp.Bridge` 直接包装为 MCP 服务端，便于确定性的进程内或管道测试。
- **场景树旁的 Remote 标签页**：在 `SceneTreeView` 顶部可在 **Scene**（`SceneTreeLocal`）与 **Remote**（`SceneTreeRemote`）之间切换。Remote 标签页列出运行中的场景（`game_scenes_list`）、按绘制顺序排列的图层（`game_layers_list`）以及带父子层级的实时实体（`game_entities_list`），游戏未暂停时按 `Editor.RemoteRefreshInterval`（默认 250 ms）自动刷新，也可通过 `RefreshRemote` 手动刷新。不带场景 `ObjectRef` 的运行时生成实体归入 **SPAWNED AT RUNTIME** 分组；运行中被销毁的场景对象会变暗显示并标出消失时的步数。
- **实时检查器与实时改动（Live Changes）**：在 Remote 标签页选中实时实体（`SelectRemoteEntity`）后，`InspectorView` 会通过 `game_entity_inspect` 展示其全部组件。派生的只读组件（`world_transform2d`、`object_ref`）以实时只读形式显示；可编辑组件渲染对应类型的控件，编辑后立即通过 `game_component_set` 写入运行中的游戏（`SetRemoteComponentField` / `SetRemoteComponentValue`），并在已修改字段旁标出 `"was X"`。每次实时修改都会记录在 **LIVE CHANGES** 列表（`RemoteLiveChanges`）中，断开连接或停止游戏后列表仍保留：可逐行撤回（`RevertRemoteLiveChange`）、全部丢弃（`DiscardRemoteLiveChanges`），或点击 **Keep in scene**（`KeepRemoteLiveChangesInScene`）将所有映射到当前场景对象的改动作为单个可撤销的 `History.Group` 步骤写回场景，一次撤销即可完整还原。
- **实时控制栏、可调参数与控制台**：连接期间顶部会显示实时控制栏，提供 **Pause / Resume**（`mod+shift+p`、`RemotePause` / `RemoteResume`）、单步 **Step**（暂停时按 `.`）与 **Step N** 固定步进（`RemoteStep`）、时间倍率 **Time Scale**（`0.25×`、`0.5×`、`1×`、`2×`、`RemoteSetTimeScale`），并在状态栏显示实时帧数、模拟时间与实体数量。若游戏配置了 `debug.Console`，远程检查器还会展示实时 **Tunables**（`game_tunables_list` / `RemoteSetTunable`）与 **Console** 命令行（`RemoteExecConsole` / `game_console_exec`）。
## 3D 地形雕刻与绘制工具（#900）

在 3D 视图（`ViewMode3D`）中，点击工具栏的 **Terrain** 按钮（或在文件浏览器中双击 / 点击 **Terrain** 打开 `*.terrain.yaml`，或在 Go 代码中调用 `NewTerrain`、`OpenTerrainFile`、`OpenTerrainProjectFile`、`OpenTerrain`），即可在 3D 视口中直接雕刻和绘制 `world3d` 地形，并在视口下方显示停靠的 `TerrainPanelView` 面板：

- **雕刻与绘制画笔**：支持 `TerrainBrushRaise`（抬高）、`TerrainBrushLower`（压低）、`TerrainBrushSmooth`（平滑）、`TerrainBrushFlatten`（整平至起笔处高度，或启用 `UseTargetHeight` 时整平至 `TargetHeight`）、`TerrainBrushSetHeight`（设定高度 `TargetHeight`）、`TerrainBrushPaintLayer`（绘制纹理材质层权重并自动保持各层权重之和归一化为 1）以及 `TerrainBrushPaintFoliage`（将选中植被层的密度向 `[0, 1]` 内的 `TargetDensity` 过渡，`0` 为擦除）。可通过 `SetTerrainBrush` 或面板控件配置半径 `Radius`、强度 `Strength` 和衰减曲线 `Falloff`（`TerrainFalloffSmooth`、`TerrainFalloffLinear`、`TerrainFalloffFlat`）。
- **3D 射线笔触与单步撤销/重做**：在 3D 视口中点击或拖拽会对实时 `world3d.Heightmap` 执行射线检测（`RaycastTerrain3D`、`BeginTerrainStrokeAt` / `ContinueTerrainStrokeAt` / `EndTerrainStroke`，或直接传入世界坐标的 `BeginTerrainStroke` / `ContinueTerrainStroke`），在快速拖拽路径上自动插值平滑落笔，在地形表面投射贴地画笔半径环，并将一整次拖拽笔触作为 **单步撤销记录** 存入 `Editor.History`（按 `Escape` 或调用 `CancelTerrainStroke` 可中止当前笔触并还原）。撤销与重做会精确恢复高度图、材质权重和植被密度数据，并同步更新 `TerrainDirty()` / `HasUnsavedChanges()`。
- **16 位无损保存**：点击 **Save Terrain**（`SaveTerrain`、`SaveTerrainTo` 或脏状态下的 `Save`）会原子写出 16 位灰度 PNG 高度图（`*image.Gray16`，消除 8 位斜坡阶梯伪影）、逐层灰度权重图 PNG（或打包 RGBA `splatMaps`）、植被密度图 PNG 以及确定性 `*.terrain.yaml`，使游戏侧通过 `world3d.LoadTerrainFS`、`TerrainDoc.ScatterFoliage` 与 `render3d.TerrainMaterialFromDoc` 加载并绘制出完全一致的地形。
## 粒子发射器编辑器（#883）

粒子发射器编辑器（`ParticleEditorPanelView`）可通过工具栏的 **Particles** 按钮（`CmdParticleEditor`）、在文件浏览器中双击 `*.particles.yaml` / `*.emitter.yaml` 或点击 **Open Emitter**，或在检查器中点击带有 `particles` 对象的 **Edit Emitter** 按钮打开，支持带实时预览与完整撤销/重做地编辑 2D `particles.Config` 的全部字段：

- **不重启的实时预览**：面板在预览画布上运行实时的 `*particles.Emitter`（`ParticlePreview`），提供 **Play/Pause**（`PlayParticlePreview` / `PauseParticlePreview`）、**Restart**（`RestartParticlePreview`）、**Step**（`StepParticlePreview`）、**Burst**（`BurstParticlePreview`）、**Preview Atlas**（`SetParticlePreviewAtlas`）以及用于测试反弹/停止/销毁碰撞的可选 **Floor** 地板碰撞体（`SetParticlePreviewFloor`）。修改发射速率、曲线、运动等参数时通过 `Emitter.ReloadConfig` 立即生效，不会重启或清除当前存活粒子；未绑定图集或帧时，预览会以彩色圆点回退绘制，便于随时调参。
- **全字段编辑与交互式曲线控件**：覆盖 `space`、位置、旋转、`z`、`seed`、`shape`（`point`、`circle`、`ring`、`rect`）、`rate`、`maxParticles`、定时爆发 `bursts`（增删改 `time`、`count`、`cycles`、`interval`）、`lifetimeMin`/`Max`、`speedMin`/`Max`、`angleMin`/`Max`、`gravityX`/`Y`、`initialRotationMin`/`Max`、`rotationSpeedMin`/`Max`、`collision`、`trail` 与递归 `subEmitters`（通过 `SetParticleTargetPath` / **Edit Child** 进入编辑子发射器配置）。曲线控件在交互画布上绘制 `sizeCurve`、`alphaCurve`、`rotationCurve`、`colorCurve`、`trail.widthCurve` 与 `trail.colorGradient`，支持拖拽控制点、点击新增控制点及数值微调；图集帧选择器可直接切换解析器图集中的 `frameNames`。
- **撤销/重做与确定性保存**：所有编辑均记录在 `Editor.History` 中；点击 **Save Emitter**（`SaveParticleEmitterFile` / `SaveParticleEmitterYAML` / `OnSaveParticleEmitter`）以规范的版本化 YAML（`particles.MarshalConfigYAML`）写出，往返加载保持字节不变，并热重载当前打开场景中引用同一文件的发射器。
- **场景放置**：可通过 `SetObjectParticles`、`PlaceParticles` 或检查器的 **PARTICLES** 区域（`file` 与 `atlas` 选择器）为场景对象绑定粒子发射器。放置的发射器在 2D 编辑视口中实时预览（`StepSceneEmitters`，隐藏对象时同步隐藏），在 Play 模式（`PlayIn`）与独立游戏（`sceneformat.Build`）中正常运行，纳入 `refs` 扫描（`MoveFile` 自动改写 `particles.file`），并由 `assetcheck` 校验文件及帧名。
## 语言与外观设置（#921）

编辑器界面通过内嵌的带版本号 `l10n` YAML 字符串表（`editor/locales/en.yaml`、`editor/locales/zh.yaml`）同时提供英文（`en`）与简体中文（`zh`）界面，并将每名用户的编辑器偏好设置作为确定性的带版本号 YAML 保存在项目之外：

- **用户级设置文件（`EditorSettings`、`SettingsVersion = 1`）**：默认保存在 `DefaultSettingsPath()`（`os.UserConfigDir()` 下的 `nsgo-game/editor.yaml`，位于所有游戏项目之外），通过 `ParseEditorSettings`、`EncodeEditorSettings`、`LoadEditorSettings`、`SaveEditorSettings`、`Editor.LoadSettings`、`Editor.LoadUserSettings` 和 `Editor.SaveSettings` 读写。未知字段、YAML 锚点/别名/合并键、多余文档和非法枚举值都会被拒绝并返回 `*loaderr.Error`。已发布的 JSON Schema 为 `https://game.nsgo.dev/schemas/editor-settings.v1.json`（`SettingsSchemaURL`）。
  ```yaml
  # yaml-language-server: $schema=https://game.nsgo.dev/schemas/editor-settings.v1.json
  version: 1
  language: system
  appearance: system
  ```
- **实时语言切换**：`Language` 支持 `LanguageSystem`（`"system"`）、`LanguageEnglish`（`"en"`）和 `LanguageChinese`（`"zh"`）。调用 `Editor.SetLanguage` 或在**编辑器设置**对话框（`EditorSettingsDialogView`，可通过工具栏的**偏好设置**按钮、`文件 > 编辑器设置…` 或 `mod+shift+,` 打开）中切换语言后，工具栏、桌面菜单栏、文件浏览器、资源浏览器、场景树、图层面板、检查器、底部面板、对话框、命令面板与状态栏都会立即切换，无需重启。`Editor.ResolvedLanguage()` 返回当前生效的语言（`"en"` 或 `"zh"`），并通过 `selection_get` 传递给连接的 MCP 智能体。
- **外观（`core.Scope.SetAppearance`）**：`Appearance` 支持 `AppearanceSystem`（`"system"`）、`AppearanceLight`（`"light"`）和 `AppearanceDark`（`"dark"`）。`Editor.SetAppearance` 会立即通过 NSGo 的 `core.Scope.SetAppearance` 生效（即使系统为浅色也可保持深色，反之亦然），并在设置了 `Editor.SettingsPath` 时自动持久化，重启后保持不变。
- **插件本地化**：插件可提供 `Catalog *l10n.Catalog` 或带版本号的 `LocaleYAML [][]byte` 字符串表（`RegisterPlugin` 会严格校验），并在面板、工具和命令上设置可选的 `TitleKey` / `LabelKey` / `DescriptionKey` / `CategoryKey`。在 `Panel.View` 和 `Inspector.View` 中，插件可调用 `Editor.PluginT(pluginName, key, args...)` 与 `Editor.PluginPlural(...)`，使插件界面实时跟随编辑器当前语言。

## 2D 骨骼绑定编辑器（#886）

编辑器通过视口下方的 `RigPanelView` 直接打开、编辑、预览、保存并在场景中放置版本 1 的 `.skel.yaml` 2D 骨骼文件（`game/skeletal`）：

- **打开与无损往返**：在文件浏览器（`FilesView`）中选中 `*.skel.yaml` 或 `*.skel.yml` 文件后双击或点击 **Open Rig**（`OpenRigProjectFile`、`OpenRigFile`、`OpenRigYAML`），或通过 `NewRig` 创建新骨骼。打开已有骨骼后未做改动（或将所有编辑撤销回打开时的状态）直接保存会原封不动地写出原始字节（`SaveRigYAML`、`SaveRigFile`、`Editor.OnSaveRig`）；经过编辑的骨骼则通过 `skeletal.Encode` 以确定性规范 YAML 保存。
- **交互式骨骼画布与手柄**：`DrawRigViewport` 通过运行时 `skeletal.Skeleton.Draw` 绘制附件，并在其上叠加骨骼层级和交互手柄：
  - **骨骼平移手柄（`RigHandleBoneMove`）**：拖拽骨骼关节圆点，在绑定姿势下修改父骨骼局部坐标，或在选中动画时在当前播放头打下 `translate` 关键帧。
  - **骨骼旋转手柄（`RigHandleBoneRotate`）**：拖拽骨骼周围的旋转环调整旋转角度（绑定姿势或当前动画的 `rotate` 关键帧）。
  - **骨骼长度手柄（`RigHandleBoneLength`）**：拖拽骨骼末端的菱形手柄设置 `BoneData.Length`。
  - **附件原点手柄（`RigHandleAttachmentOrigin`）**：拖拽选中插槽当前皮肤附件上的枢轴十字手柄，调整 `OriginX` 和 `OriginY`。
  - 每次手柄拖拽（`BeginRigDrag`、`BeginRigHandleDrag`、`UpdateRigDrag`、`EndRigDrag`）作为单个可撤销步骤记入 `Editor.History`，`CancelRigDrag`（或在拖拽时按 `Escape`）会恢复拖拽前状态且不写历史。
- **骨骼、插槽、皮肤与附件**：支持增删与重命名骨骼（`AddRigBone`、`RenameRigBone`、`DeleteRigBone`）、编辑变换与长度（`MoveRigBone`、`RotateRigBone`、`ScaleRigBone`、`SetRigBoneLength`）以及更改父骨骼（`ReparentRigBone`，支持保持绑定姿势世界变换并拒绝环路）；增删、重新绑定、设置透明度与重排绘制顺序插槽（`AddRigSlot`、`DeleteRigSlot`、`SetRigSlotBone`、`SetRigSlotAlpha`、`MoveRigSlotOrder`）；管理皮肤（`AddRigSkin`、`DeleteRigSkin`、`SelectRigSkin`）并将图集帧与枢轴原点挂接到插槽（`AttachRigFrame`、`SetRigAttachment`、`SetRigAttachmentOrigin`、`SetRigAttachmentTransform`、`RemoveRigAttachment`）。
- **时间轴、播放与交叉淡入预览**：创建与编辑动画（`AddRigAnimation`、`DeleteRigAnimation`、`SelectRigAnimation`、`SetRigAnimationDuration`、`SetRigAnimationLoop`），在时间轴上为骨骼的 `translate`、`rotate`、`scale` 以及插槽的 `attachment`、`alpha` 打关键帧（`KeyRigBoneTranslate`、`KeyRigBoneRotate`、`KeyRigBoneScale`、`KeyRigSlotAttachment`、`KeyRigSlotAlpha`、`DeleteRigBoneKeyframe`、`DeleteRigSlotKeyframe`），并直接经由运行时 `skeletal.Skeleton`（`RigSkeleton`）进行拖动预览（`SeekRig`）、播放（`PlayRig`、`PauseRig`、`StopRig`、`StepRig`）与交叉淡入混合预览（`CrossfadeRig`、`RigBlending`）。
- **在场景中放置骨骼**：点击 **Place in Scene**（`PlaceOpenRig`、`PlaceRig`）可将骨骼作为带有内置 `"skeletal"` 组件（`sceneformat.SkeletalComponent`，包含 `version: 1`、`rig`、`atlas`、`skin`、`animation`、`loop`）的对象放入场景。`sceneformat.Build` 通过 `SkeletonResolver` 或 `FileResolver`（`FSAssets`）加载骨骼文件并构建带有指定皮肤和动画的实时 `BuiltObject.Skeleton`，在 `BuiltScene.Draw` 与 2D 编辑器视口中按精确 Z 顺序绘制，并在 `BuiltScene.Update(dt)` 中推进动画播放。`refs.Scan` 会追踪 `components.skeletal.rig`（`refs.FieldSkeletalRig`），使 `MoveFile` 重命名骨骼文件时自动更新所有场景引用。

## HUD 与菜单布局界面（#1047）

可通过工具栏 **HUD** 按钮、`Project > HUD & Menu Layout`、命令面板（`mod+shift+u` / `CmdHUDLayout`）、在文件浏览器中双击或打开任意 `*.screen.yaml` 文件，或点击问题面板中的界面诊断条目打开 HUD 与菜单布局界面（`HUDLayoutView`、`OpenHUDScreen` / `OpenHUDScreenFile` / `OpenHUDScreenDoc`）。界面严格遵循编辑器设计规范令牌（`docs/design/editor/HUDLayout.dc.html` 与 `Spec.dc.html`），在所选语言下同时呈现所有屏幕尺寸，并将手动编辑聚焦在高频微调上：

- **顶栏与语言切换**：左侧展示 `<项目名> / screens / <文件名>` 面包屑（点击可返回场景编辑器），中间为读取自 `locales/*.yaml`（并叠加未保存的 `Editor.l10n` 编辑）的 **Language** 分段语言切换器（`SetHUDLocale`），右侧为 **Search ⌘K** 与 **Play**。切换语言会立即重绘全部四个设备尺寸、更新文本宽度表、检查器多语言文本与溢出放大预览以及 **GAME CHECK** 诊断。
- **左侧栏（`SCREENS` 与 `NODES`）**：列出项目中所有 `*.screen.yaml` 文件（`HUDScreens`）及其节点数与当前语言的问题数；下方展示当前界面的层级节点树（`HUDTree`），附带等宽锚点标记（`TL`、`TC`、`TR`、`CL`、`C`、`CR`、`BL`、`BC`、`BR`、`FILL`）与红色问题圆点。
- **四尺寸同屏预览与叠加开关**：通过真实的 `gameui.BuildScreen` 布局（`HUDLayoutFrames`）同时绘制带刘海的 **Phone 19.5:9**（`780 × 360`）、**Tablet 4:3**（`640 × 480`）、**Desktop 16:9**（`640 × 360`）与 **Ultrawide 21:9**（`840 × 360`），提供 **Safe areas**、**Anchors**、**Touch areas** 与 **Focus order** 四个叠加层开关（`HUDOverlays` / `ToggleHUDOverlay`），并在视口右下角浮层展示 **TEXT WIDTH, PX** 表（`HUDTextWidths`），逐语言对比每个文本节点的容器宽度与实际文本宽度并高亮溢出项。
- **检查器、溢出放大视图与 `GAME CHECK`**：展示选中节点的 **ANCHOR**（3×3 锚点按钮网格加 **Fill**、文字描述锚点与偏移量、以及 **Keep inside the safe area** 开关）、**SIZE**（**Fixed** / **Fit text** 切换与触控目标尺寸）、**TEXT**（键名、各语言译文以及文本溢出时的 `1.5×` 放大细节预览卡片），以及基于 `gameui.CheckScreen` 的实时 **GAME CHECK** 问题列表（`HUDProblems`），每条问题附带可直接选中目标节点的 **Show** 按钮与 **Ask the agent to fix N** 按钮；无问题时显示 `"No problems at any size in this language"`。
- **手动微调与智能体输入框**：调整容器内节点顺序（`ReorderHUDNode`）、修改容器间距（`SetHUDNodeSpacing`）、切换锚点/安全区/尺寸模式（`SetHUDNodeAnchor`、`SetHUDNodeSafeArea`、`SetHUDNodeSizeMode`）以及直接拖拽触控摇杆与触控按钮（`BeginHUDTouchDrag`、`UpdateHUDTouchDrag`、`EndHUDTouchDrag`、`CancelHUDTouchDrag`）均在 `Editor.History` 中记录为**单个可撤销步骤**，并将规范 YAML 写回磁盘上的 `*.screen.yaml` 文件（撤销时逐字节恢复原文件）。底部输入框（`SubmitHUDPrompt`、`AskAgentToFixHUDProblems`、`Editor.OnHUDPrompt`）用于向智能体发起其余界面修改请求。

## 过场动画时间轴与智能体审查（#1056）

打开任意 `*.cutscene.yaml` / `*.cutscene.yml` 文件（通过文件浏览器、命令面板、`OpenCutsceneProjectFile`、`OpenCutsceneFile`、`OpenCutsceneYAML` 或智能体的 `cutscene_write` MCP 工具），都会进入占满工作区的过场动画时间轴编辑器（`CutscenePanelView`），完全采用 `Spec.dc.html` 设计 Token 与 `docs/design/editor/Animation.dc.html` 第 2 屏布局：

- **左侧栏（`CUTSCENES` 与 `LINES`）**：列出项目中的所有过场文件（`ProjectCutscenes`），显示时长并标出 `video` 视频类型；下方列出从 `loc/en.yaml` 与 `loc/zh.yaml`（或 `Editor.CutsceneCatalog` / 已打开的本地化工作区）解析出的本地化对白条目（`CutsceneLines`），展示行 ID、英文和中文译文。
- **实时预览舞台与缩略图拖动条**：使用真实的 `cutscene.TimelinePlayer` 驱动隔离的场景副本（`CutscenePreviewScene`、`SeekCutscene`、`PlayCutscene`、`PauseCutscene`、`StepCutscene`），拖动到任意时间点都与运行时播放器摆出的角色位置、相机与字幕完全一致。界面展示当前相机标签（`CutsceneCameraLabel`）、本地化字幕叠加层（`CutsceneActiveDialogue`）、Play / Pause 播放控制、时间读数以及 24 帧可点击缩略图拖动条（`CutsceneThumbs`）。
- **轨道泳道与片段检查器**：为 Camera、每个角色、Dialogue、Sound 和 Music 轨道分别显示泳道与片段块。选中片段（`SelectCutsceneClip`、`SelectCutsceneClipByID`）后，可通过 `±0.1 s` 步进按钮调整 **Start** 和 **Duration**（`AdjustCutsceneClipStart`、`AdjustCutsceneClipDuration`、`SetCutsceneClipTiming`），并在相机和角色轨道上切换 **Easing**（`Linear`、`In`、`Out`、`In-out`、`Hold`，`SetCutsceneClipEasing`）。选中对白片段时会展示行 ID、中英文译文，并提示文本存放在 `loc/en.yaml` 与 `loc/zh.yaml` 中、过场文件只保存 ID。
- **智能体变更审查（`OpenCutsceneReview`）**：在不提前改写磁盘文件的前提下对比修改前（`Before`）与智能体提议（`After`）。支持切换 **Before** / **After**（`SetCutsceneReviewViewMode`）或按住 **B** 键快速窥视修改前状态（`SetCutsceneReviewPeekBefore`、`CutsceneHandleKeyEvent`）；使用虚线/实线样式与图例区分新增（绿色虚线）、修改（琥珀色虚线）与选中（蓝色实线）片段；右侧 `N CHANGES` 列表支持逐条 **Revert** / **Restore**（`ToggleCutsceneReviewChange`）及点击跳转定位（`FocusCutsceneReviewChange`）；审查期间用户的手动微调会自动并入本次审查且可用 `⌘Z` 撤销（`"Your edit joins this review · ⌘Z undoes it"`）；点击 **Revert all**（`RevertAllCutsceneReview`）可放弃全部提议，点击 **Keep N of M**（`KeepCutsceneReview`）则将保留的变更与手动微调作为**单个原子撤销步骤**一并写入内存与磁盘。

## 逐对象场景合并冲突解决（#1059）

当 Git 存在未合并的场景文件（通常由 #1058 的逐对象合并驱动 `go tool game merge` 产生）时，打开项目（`OpenProject`）、点击状态栏中的冲突徽章或执行 `Project > Resolve Scene Merge Conflicts`（`CmdMergeConflicts` / `OpenMergeConflict`）会进入全窗口的**场景合并冲突**界面（`MergeConflictView`，严格采用 `docs/design/editor/Spec.dc.html` 令牌并对应 `VersionControl.dc.html` 第 2 屏）：

- **顶部栏与进度**：居中显示 `Merging <theirs> into <mine> · N of M objects need you`（全部解决后变为 `ready to finish`），右侧提供 **Abort merge**（`AbortMerge`，执行 `git merge --abort` 将分支与工作区恢复原状）和 **Finish merge**（`FinishMerge`，全部冲突解决后可用）。
- **CONFLICTS 与 MERGED BY ID**：按对象 ID 将 `sceneformat.MergeResult.Conflicts` 分组列在 **CONFLICTS** 下，每行显示状态点、对象名、`Unresolved` / `Agent` / `Mine` / `Theirs` / `Custom` 标签及直观摘要（如 `Both moved it: x 560 here, x 600 on main` 或 `Your speed change; main deleted it`）。自动按 ID 合并成功且无冲突的对象列在 **MERGED BY ID** 中并标明来源分支。底部提供一键 **Take the agent's suggestion for all N**（`TakeAllAgentMergeSuggestions`）。
- **MINE、THEIRS 与 RESULT 场景预览**：上方并排展示可点击的 **MINE**（含分支、作者与短提交号）和 **THEIRS** 2D 场景画布，下方展示实时 **RESULT** 画布。未解决的冲突对象在 **RESULT** 中同时以琥珀色虚线轮廓标出双方位置；选择任一方案后 **RESULT** 立即更新。
- **Agent 建议、Mine、Theirs 与手动编辑 YAML**：右侧检查器为当前聚焦对象展示三张单选卡片——**Agent's suggestion**（由 `Editor.MergeSuggester` 或确定性的 `DefaultMergeSuggestion` 生成，可合并双方不冲突的字段或说明为何保留删除/较高 `z` 值）、**Mine** 与 **Theirs**，每张卡片展示对应 YAML 片段与建议理由。点击 **Edit the result YAML by hand**（`SetMergeCustomEditing`、`ApplyMergeCustomYAML`）可直接编辑结果 YAML，并通过 `KnownFields(true)` 与 `sceneformat.Validate` 校验后生效。
- **校验与合并提交（`FinishMerge`）**：以规范的 `sceneformat.Encode` 写出解决后的场景，运行 `game check`（`runProjectCheck` 或 `Editor.MergeCheckRunner`）以及 10 秒确定性短试玩（`sceneformat.Build` + `Update` + `PhysicsScene.Step` 或 `Editor.MergePlaytestRunner`）；任一检查失败都会阻止提交并展示错误，全部通过后执行 `git add` 与 `git commit --no-edit` 生成真正的合并提交，显示 `Merged as <sha>` 并将解决后的场景重新载入编辑器。
- **MCP 工具（`editormcp`）**：提供 `merge_status`、`merge_resolve`、`merge_finish` 和 `merge_abort`，供编程代理检查冲突对象、补充或更新建议与理由、选择方案或提交自定义 YAML，以及完成或中止合并。

## 材质编辑器（#896）

在编辑器中直接为对象指定并编辑共享的 `*.material.yaml` 材质资源文件（见 [resource](resources.md)），支持实时预览与完整的撤销/重做：

- **指定与新建材质**：检查器中的可搜索 **Material** 下拉框（`SetObjectMaterial`）从当前项目的 `*.material.yaml` 文件中选择材质；点击 **New Material**（`CreateObjectMaterial` / `CreateMaterialFile`）会以 `resource.Encode` 的规范形式新建 `.material.yaml` 文件、将其指定给选中对象并在面板中打开；点击 **Edit Material**（`OpenMaterial` / `OpenMaterialProjectFile`）、在文件浏览器中双击材质文件或点击 **Open** / **Edit Material**、在命令面板中选择材质文件（`OpenProjectFile`）或执行 `Project > Material Editor`（`CmdMaterialEditor`）均可打开停靠在视口下方的 `MaterialEditorPanelView`。
- **全字段编辑**：支持编辑 `name`（`SetMaterialName`），使用 NSGo `ColorWell` / `ColorPicker` 拾色器与 CSS 颜色输入框编辑 `baseColor` 和 `emissive`（`SetMaterialBaseColor`、`SetMaterialEmissive`），通过可搜索的项目图片选择器编辑 `texture`（`SetMaterialTexture`、`ProjectTextures`），通过带范围约束的滑块与数值框编辑 `metallic`、`roughness` 和 `opacity`（`SetMaterialMetallic`、`SetMaterialRoughness`、`SetMaterialOpacity`），从 `resource.BlendModes()` 中选择 `blend`（`SetMaterialBlend`），以及开关 `doubleSided` 和 `unlit`（`SetMaterialDoubleSided`、`SetMaterialUnlit`）。任何不合法的值都会在改动状态或历史前被 `resource.Encode` 拒绝。
- **实时预览与视口联动**：面板提供实时预览（`MaterialPreviewImage`、`SetMaterialPreviewShape`），支持使用 `render3d` 渲染的 3D 球体（**Sphere**）与立方体（**Cube**，反映 `unlit`、PBR 金属度/粗糙度、自发光、贴图、透明度与双面渲染）以及 2D 精灵（**Sprite**）预览（2D 视图默认 Sprite，3D 视图默认 Sphere）。每次编辑（包括拾色器拖拽及撤销/重做）都会经由 `resource.Library.Reload`（通过 `project.Resolver.Library()` 与 `scenemodel.Assets.Library()`）实时应用，无需保存即可立刻刷新预览以及视口中所有引用该材质的 2D 精灵（`BuiltScene.SyncMaterials`）与 3D 对象。
- **撤销/重做与确定性保存**：所有材质编辑均记录在 `Editor.History`（`EditMaterial`）中，纳入未保存检测（`MaterialDirty`、`HasUnsavedChanges`）与文件移动跟踪（`MoveFile`）；点击 **Save Material**（`SaveMaterial`、`SaveMaterialFile`、`Save`、`SaveAll` 或 `Editor.OnSaveMaterial`）按 `resource.Encode` 写出确定性的规范 YAML。
