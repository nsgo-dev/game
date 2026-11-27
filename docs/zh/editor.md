# 编辑器与场景格式

本文档记录 NSGo 游戏编辑器的范围决策（[#159](https://github.com/nsgo-dev/game/issues/159)），并说明 `github.com/nsgo-dev/game/pkg/sceneformat` 实现的第 1 版 YAML 场景格式（[#160](https://github.com/nsgo-dev/game/issues/160)）。

独立桌面版 **NSGo Game Editor**（`cmd/editor`）安装包已在 [GitHub Releases](https://github.com/nsgo-dev/game/releases/latest) 自动发布——包含经组织 Developer ID 签名与 Apple 公证的 macOS `.dmg`（`arm64` / `x64`）、Windows `-setup.exe`（`x64` / `arm64`）与 Linux `.deb` / `.pkg.tar.gz` / `.AppImage`（`x64` / `arm64`），并附带一键安装脚本（`install.sh` / `install.ps1`）。独立版编辑器需要 **Go 1.27 或更高版本**（[go.dev/dl](https://go.dev/dl/)）以创建项目、运行游戏、执行检查和导出构建；从访达（Finder）、程序坞（Dock）、聚焦搜索（Spotlight）或桌面启动器打开时，编辑器会自动在 `PATH`、标准安装路径（`/usr/local/go/bin`、`C:\Program Files\Go\bin`）、Homebrew、版本管理器 shim 以及登录 Shell 的 `PATH` 中查找 `go`。也可通过 `go tool game editor` 或 `go run ./cmd/editor` 直接启动。

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
import "github.com/nsgo-dev/game/pkg/sceneformat"
```

### 顶层字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `version` | `int` | 必填的格式版本号：`4`（`CurrentVersion`），未使用版本 4 3D 光源/环境/相机的版本 3 视差/重复场景为 `3`，仅使用版本 2 字段的场景为 `2`，未使用版本 2–4 新增字段的场景为 `1`。`Encode` 原样写回。 |
| `uid` | `string` | 可选的稳定 ID，文件改名或移动后保持不变，由 `go tool game index` 写入（所有版本均可用，见[稳定引用](project.md#稳定引用)）。`Resolve` 保留场景自身的 uid，而不是基础场景的。 |
| `inherits` | `string` | 可选，从项目根目录起、以斜杠分隔的基础场景路径，本场景继承该场景（版本 2 及以上，见下文“实例化与继承”）。 |
| `name` | `string` | 可选的场景可读名称。 |
| `navMesh` | `string` | 可选，从项目根目录起、以斜杠分隔的场景烘焙[导航网格文件](navbake3d.md)路径，由 `go tool game navmesh` 写入（所有版本均可用）。继承其他场景时，它会替换基础场景的值。 |
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
- **内联网格**：包含 `tileWidth`、`tileHeight`、`width`、`height`，以及 `tileset`（帧名按从 1 开始的瓦片 ID `"1"`、`"2"`…… 索引的图集名）或 `tilesetFile`（版本 2+，YAML 图块集 `*.tileset.yaml` 的路径）、可选的 `offsetX` / `offsetY`（版本 3，以图层点数偏移 `(0, 0)` 单元格），配合长度为 `width * height` 的行主序 `data`（`0` 表示空瓦片；使用 `tilesetFile` 时，ID `n` 放置局部瓦片 `n - 1`）。
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

`Build` 把图集帧组成的单一网格变成精灵批次（`BuiltLayer.Tilemap`），把分层网格、引用 `tilesetFile` 的单层内联网格或外部地图变成 `tilemap.Tilemap`（`BuiltLayer.Map`）。这个地图带有图块集的标志、属性、形状、动画和地形，因此在图层坐标中可以回答 `FlagsAt`、`TileProperties`、`TilesInRect` 和 `Raycast`，可以直接交给 `physics.MoveAndSlide` 参与碰撞，也可以交给 `tileshape.New` 生成斜坡与遮挡体。`BuiltScene.Draw` 只绘制虚拟视口内的地图瓦片（世界坐标图层按相机看到的范围），除非场景没有虚拟尺寸或设置了 `DrawAllTiles`，编辑器的编辑视图就会设置它；`BuiltScene.Update(dt)` 推进动画瓦片。

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
- `SpawnEntities(doc, world)` 为每个对象在 `entities.World` 中生成一个实体，按对象的嵌套关系在其 `Hierarchy2D` 中设置父子关系，并写入对象的局部 `Transform2D` 和一个记录对象 ID 及所在图层的 `ObjectRef` 组件。隐藏图层的顶层对象生成时即为隐藏，因此其整棵子树在世界中不可见。通过 `SpawnEntitiesWith(doc, world, SpawnOptions{Components: reg})` 传入注册表时，会在生成前校验并解析所有组件默认值，并对每个实体调用对应组件类型的 `ComponentType.Spawn` 工厂（出错时回滚已创建的实体）。
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

在编辑器中，`PlaceScene(layer, path, x, y)` 以可撤销命令放置一个场景文件的实例，其 ID 以文件名命名（也可以在文件浏览器中将场景文件拖入视口、点击行内的 `+` 按钮，或选中场景文件后点击 **放置**；资源浏览器中已注册的预制体同样可通过点击或拖入视口经由 `PlacePrefab` 放置）。编辑器的 `Resolver` 必须实现 `SceneResolver`；`PlaceScene`，以及 `PlaceObject` 在放置的对象树中含有 `scene` 时，会先按放置后的样子解析整个场景，解析失败则拒绝放置：文件缺失或无效、override 指向不存在的对象或取值无效、展开实例后 ID 冲突。把编辑器的 `ScenePath` 设为当前场景相对项目根目录的路径（`OpenYAML` 会清空它），这样直接或经由其他文件回到当前场景的实例会在保存出自我实例化的文件之前，作为场景循环被拒绝。在版本 1 的场景中放置实例会把场景升级到版本 2（撤销时恢复）。新 ID 不仅与场景自身的 ID 比较，还与继承的基础场景和实例带来的 ID 比较。

2D 与 3D 视口以及场景树（`SceneTreeView`）均支持直接选中实例化子对象或继承对象（在场景树中以静音灰样式显示，附带 `实例`/`基础` 标签，存在覆盖时显示覆盖圆点）。编辑其变换、精灵、材质、粒子、3D 光源/相机/遮挡体/网格/模型/GridMap、自定义属性或组件字段时，会在所属实例上记录可撤销的覆盖（`ObjectDoc.Overrides`，以被实例化场景内的 ID 为键）或在继承场景上记录覆盖（`SceneDoc.Overrides`，保持基础场景文件不变）；一旦 `sceneformat.Resolve` 失败则拒绝编辑。编辑继承图层的可见性、坐标系、视差或重复平铺时，会写入 `SceneDoc.LayerOverrides`。检查器会显示实例/继承信息栏（包含源场景路径、实例数量与覆盖数量），用提示圆点（悬停显示源值）与单字段 **还原** 按钮（`RevertObjectOverrideField` / `RevertLayerOverrideField`）标出每个被覆盖的字段，并提供 **全部还原**（`RevertObjectOverrides` / `RevertLayerOverrides`，自动清理空覆盖映射以保持 YAML 整洁）、**打开场景** / **打开基础场景**（`OpenSourceScene` / `OpenInstancedScene` / `OpenBaseScene`）、**应用到源场景**（`ApplyOverridesToSource`，单步可撤销地将实例覆盖写回源场景文件并清空实例上的覆盖，撤销时恢复原源场景文件）以及 **本地化**（`MakeInstanceLocal`，单步可撤销地将场景实例替换为解析后的本地对象副本与带前缀连接）。运行（Play）时使用解析后的场景副本，因此通过 `BuiltObject.Doc` 所做的改动在停止（Stop）恢复之前会反映在编辑器的文档中。

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

`sceneformat.Merge(base, ours, theirs)` 对场景文档执行基于身份的三路合并：图层按 `name` 匹配，对象在整个场景树中按 `id` 匹配（包括跨父节点和跨图层移动），瓦片地图与覆盖按主键匹配，连接按 `(from, event, to, handler)` 匹配。各个字段（包括嵌套的 `components`、`properties`、`overrides`、`camera` 和 `args` 映射）逐一合并。模式 `version` 会单调合并为 `base`、`ours` 与 `theirs` 中的最高版本（不低于 `base`），避免两个分支各自提升场景版本时产生冲突；无冲突的结果会经过 `Validate` 校验并通过 `Encode` 以规范格式写出。

当双方把同一字段改成不同值、一方删除而另一方修改、双方以相同 ID 新增不同对象、重设父节点冲突或形成环，或合并结果无法通过 `Validate` 时，`MergeResult.ConflictDoc(path)` 会生成带版本号、确定性的 YAML 冲突文档 `ConflictDoc`（`version: 1`，由 `ParseConflict` / `EncodeConflict` 读写），记录每个冲突的对象、字段与 `base`/`ours`/`theirs` 三个值，以及已自动合并的条目。项目通过 `.gitattributes`（`*.scene.yaml merge=nsgo-scene`）和 `go tool game merge -setup` 将其接入 Git（每个克隆运行一次，可用 `go tool game merge -check` 检查；编辑器打开 Git 仓库中的项目时也会自动检查并在横幅中提供一键配置，参见 [`game merge`](cli.md#merge) 与[场景合并冲突报告](formats.md#场景合并冲突报告)）。

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
- **编辑器内运行、视口调试线框、3D 碰撞体手柄与贴合模型包围盒（#902）**：`Editor.PlayIn` 会自动构建并步进 `PhysicsScene`（`Editor.PlayPhysics`、`Editor.PlayTriggerEvents`、`Editor.OnPlayTrigger`），因此包含玩家刚体、墙壁碰撞体与金币触发器的场景无需编写碰撞 Go 代码即可直接在编辑器中运行并触发碰撞与触发事件。`Editor.ShowPhysicsDebug` 开启时（默认开启，可用 `SetShowPhysicsDebug` 切换），2D 与 3D 视口均会绘制每个 `collider2d`、`trigger2d`、`collider3d` 和 `trigger3d` 的线框轮廓（包含圆柱、凸包、三角网格与地形高度场）。将 3D 变换控件切换为 `Gizmo3DCollider`（`View > 3D Gizmo > Collider`、命令面板或快捷键 `shift+c`）即可通过 3D 控件 API 拖拽碰撞体尺寸手柄（`ColliderHandles3D`、`PickGizmo3D`、`BeginGizmoDrag3D`、`UpdateGizmoDrag3D`、`EndGizmoDrag3D`），每次拖拽作为单个撤销步骤提交；点击检查器中的 **Fit to Model Bounds** 按钮（或调用 `Editor.FitCollider3DToBounds`）即可在单个撤销步骤中将碰撞体尺寸和局部偏移贴合到对象网格或 glTF 模型的包围盒。

### 内置 2D 光照与遮挡体组件（#887）

`sceneformat` 内置两个带版本号的 2D 光照组件（`Lights2DComponentVersion = 1`），可通过 `RegisterLights2DComponents(reg)` 或 `Lights2DComponentRegistry()` 一起注册（编辑器检查器中默认自动可用）：

| 组件 | 主要字段 |
| --- | --- |
| `light2d` | `version`（`1`）、`kind`（`point`、`spot`）、`color`（CSS 颜色）、`radius`、`intensity`、`falloff`、`direction`（弧度，与对象旋转叠加）、`angle`（聚光灯张角弧度）、`castShadows`、`enabled`、`offsetX`、`offsetY` |
| `occluder2d` | `version`（`1`）、`shape`（`rect`、`polygon`、`tilemap`）、`width`、`height`、`offsetX`、`offsetY`、`points`（`[][2]float64`，流式 YAML `[[x, y], ...]`）、`layer`、`tileLayers`（`[]string`）、`enabled` |

- **自动构建 `lights2d.Scene`**：`sceneformat.Build` 会把 `SceneDoc.AmbientColor`、可见世界空间图层上的所有 `light2d` 与 `occluder2d`，以及 `shape: "tilemap"` 遮挡体（通过 [`tileshape.View`](tileshape.md) 从瓦片地图碰撞形状提取）组装为 `BuiltScene.Lights2D`（`*lights2d.Scene`）。除非设置了 `SkipLights2D`，`BuiltScene.Draw` 会在场景相机变换下将 `Lights2D` 绘制在世界空间图层之上（屏幕空间 HUD 图层之下）；运行时移动光源或遮挡体后调用 `BuiltScene.SyncLights2D()` 同步。
- **2D 光照预览与交互手柄**：通过 **View > 2D Lighting Preview** 或命令面板（`Editor.ShowLightingPreview`、`SetShowLightingPreview`、命令 `CmdToggleLightingPreview`）可在编辑模式下实时预览与游戏完全一致的 `lights2d.Scene` 阴影与环境暗场。选中 `light2d` 对象时会显示半径（`Handle2DLightRadius`）、聚光灯张角（`Handle2DLightSpotAngle`）与聚光灯方向（`Handle2DLightSpotDirection`）手柄；选中 `occluder2d` 对象时，矩形遮挡体显示四边缩放手柄（`Handle2DOccluderRectN/E/S/W`），多边形遮挡体显示可拖动顶点手柄（`Handle2DOccluderVertex`）与边中点插点手柄（`Handle2DOccluderEdgeMid`，以及 `SetOccluderPolygonPoints`、`InsertOccluderVertex`、`RemoveOccluderVertex`、`ConvertOccluderToPolygon`）。每次拖动记录为一个可撤销步骤，按 Escape 可取消；`SetAmbientColor`（或检查器中的 Scene Lighting 区域）可撤销地编辑 `ambientColor`。

### 内置 2D 导航区域组件（#888）

`sceneformat` 内置带版本号的 2D 导航区域组件（`NavRegion2DComponentVersion = 1`），可通过 `RegisterNavRegion2DComponent(reg)` 或 `NavRegion2DComponentRegistry()` 注册（在编辑器检查器以及 `scene.v1.json` / `scene.v2.json` / `scene.v3.json` Schema 中默认自动可用）：

| 组件 | 主要字段 |
| --- | --- |
| `navregion2d` | `version`（`1`）、`mode`（`walkable`、`obstacle`）、`points`（`[][2]float64`，流式 YAML `[[x, y], ...]`）、`offsetX`、`offsetY`、`subtractTileCollision`（`bool`）、`tileLayer`（`string`）、`tileLayers`（`[]string`）、`agentRadius`（`float`，`>= 0`）、`enabled`（`bool`） |

- **自动构建 `pathfind.NavMesh`（`BuildNavMesh2D`）**：`sceneformat.Build` 会收集可见世界空间图层上所有启用的 `navregion2d` 组件（统一绕序，确保重叠的可行走区域求并、障碍物区域挖洞），按需通过 [`tileshape.View`](tileshape.md) 及非矩形实心瓦片形状扣除场景瓦片地图中的实心碰撞，附加场景中声明的 2D `nav_link` 离网连接，并调用 [`game/pathfind/navbake`](navbake.md) 烘焙凸多边形，填充 `BuiltScene.NavBake2D`（`*navbake.Mesh`）与 `BuiltScene.NavMesh2D`（`*pathfind.NavMesh`）。不含 `navregion2d` 的场景会将两字段保持为 `nil`；运行时修改后可调用 `BuiltScene.SyncNavMesh2D()` 重新同步。
- **2D 导航网格叠加层、多边形手柄与路径测试工具**：开启 `Editor.ShowNavMesh2D`（默认开启，可通过 **View > 2D NavMesh Overlay**、2D 视口栏 **Overlays** 弹出面板、`CmdToggleNavMesh2D` 或 `SetShowNavMesh2D` 切换）时，2D 视口会绘制已构建 `NavBake2D` 的凸多边形、相邻门户以及作者编辑的 `navregion2d` 轮廓。选中 `navregion2d` 对象会显示可拖动顶点手柄（`Handle2DNavRegionVertex`）与边中点插点手柄（`Handle2DNavRegionEdgeMid`），并提供 `PlaceNavRegion2D`、`InitNavRegionPolygon`、`SetNavRegionPoints`、`InsertNavRegionVertex` 与 `RemoveNavRegionVertex`；拖动顶点时叠加层实时刷新，松手后记录为单个可撤销步骤（按 Escape 取消）。选择 **Test Path** 工具（`ToolNavTestPath`，**View > Tools > Test Nav Path** 或命令 `CmdToolNavTestPath`）后，在视口依次点选起点与终点即可绘制 `BuiltScene.NavMesh2D.FindPath` 找到的路径（随顶点或瓦片修改实时重算，按 Escape 或调用 `ClearNavPathPreview2D` 清除）。

### 内置计时器组件（#1362）

`sceneformat` 内置计时器组件，对应 Godot 的 `Timer` 节点，可通过 `RegisterTimerComponent(reg)` 或 `TimerComponentRegistry()` 注册（在编辑器检查器和场景 JSON Schema 中默认自动可用）：

| 组件 | 主要字段 |
| --- | --- |
| `timer` | `seconds`（`1`，`>= 0`）、`oneShot`（`false`）、`autostart`（`false`）；声明事件 `timeout` 和处理函数 `start`、`stop` |

- **随游戏时钟运行**：`Signals.Update(dt)` 每个更新步推进已连接场景的所有计时器一次——在 `game.Loop` 的 `Update` 中、`Flush` 之前调用——并把每个 `timeout` 按文档顺序入队，留给这次 `Flush` 分发，于是场景不用写任何 Go 代码就能把计时器接到任意处理函数上，计时器顺序也是确定的。单次计时器触发一次后停止；重复计时器把超出部分带进下一个周期，不会漂移；`seconds: 0` 每个更新步触发一次。实体被销毁（`SpawnConnected`）的对象的计时器会停止。
- **`start` 和 `stop` 处理函数**：把任意事件连接到计时器的 `start` 会从完整 `seconds` 重新启动它，连接到 `stop` 则在不重置的情况下停住它，于是文件里就能让一个按钮启动一扇门的计时器。

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

#### 在 Go 中监听事件

场景的连接写在文件里；同一批事件的代码侧——音效、统计、成就、界面——改为在 Go 中订阅，就像 Godot 的 `Signal.connect`，不需要每个场景都造一个目标对象、加一行连接：

```go
stop, err := signals.Listen("", "opened", func(c sceneformat.Call) {
	playSound("door_open")
}, sceneformat.ListenOptions{})
defer stop()
```

`Signals.Listen(from, event, fn, opts)` 像 `Emit` 那样检查 `from` 和 `event`，在调用时就拒绝不存在的对象或事件，拼错的名字会立即失败；`from` 为空时监听场景中所有声明了该事件的对象。监听器在同一事件的文件连接之后、按添加顺序运行，它的 `Call` 带有源对象、事件和负载（`Target`、`Component`、`Handler` 和 `Args` 保持未设置，因为监听器不是连接）。`ListenOptions.Once` 让它只在第一次触发时运行（`CONNECT_ONE_SHOT`），`ListenOptions.Deferred` 把每次运行入队到下一次 `Flush`（`CONNECT_DEFERRED`），返回的 `stop` 结束订阅——幂等，可以在处理函数内调用。对象的实体被销毁（`SpawnConnected`）后，它的监听器不再为它运行，与连接一样。

#### 生成连接好的实体

`SpawnConnected(doc, world, reg, ctx)` 像 `Connect` 那样检查场景，并通过 `SpawnEntitiesWith` 传入 `reg` 生成它的对象（使每个已注册的 `ComponentType.Spawn` 附加强类型 ECS 状态并在出错时回滚），然后返回每个对象 ID 对应的实体，以及连接这些实体的 `*Signals`。所有检查都在生成第一个实体之前完成，所以连接指向不存在的对象、事件、处理函数或 `Func`，或者组件不符合其类型的场景，什么都不会生成。`EmitEntity(e, event, payload)` 和 `EnqueueEntity` 用实体指定对象，`Entity(id)` 返回对象的实体，每个 `Call` 都带有 `SourceEntity` 和 `TargetEntity`，处理函数可以用 `entities.Get` 取到目标的组件。被销毁的实体不再发出事件：`Emit`、`Enqueue` 及其实体形式会拒绝它，`Flush` 丢弃它之前入队的事件，指向它的连接会被跳过。实例化或继承其他场景的场景在 `Resolve` 之后生成，实例的连接作用于实例的实体（`west/door`）。

#### 检查连接

`go tool game check` 会解析每个实例化或继承其他场景的场景。场景无法解析时（例如循环引用，或覆盖项指向不存在的对象）它会失败，并报告实例所在行或 `inherits` 行；连接的 `from` 或 `to` 指向解析后场景中不存在的对象时也会失败，并报告写有该名称的行。当项目拥有自己的编辑器命令（`cmd/editor` 导入 `pkg/editor/editorcmd`，或由 `game generate editor` 生成的 `-tags gameeditor`）时，`go tool game check` 会以 `-check` 运行它，用项目的注册表（加上内置物理和骨骼组件）调用 `assetcheck.CheckWith`，一并检查组件和每条连接的事件、处理函数与参数。没有编辑器命令时，`game check` 只检查对象，并在场景含连接时打印提示说明如何用 `game generate editor` 开启；游戏也可以在自己的测试中直接用 `assetcheck.CheckWith` 和自己的注册表检查它们。两种方式都会在文件和行上报告指向对象未声明的事件、处理函数或参数的连接，以及不符合类型的组件；对实例化或继承其他场景的场景，它还会检查覆盖项写入的组件和解析后场景的每个连接，所以覆盖项破坏了被实例化或被继承的场景带来的连接时（例如再加一个声明同名事件的组件），会在该覆盖项处失败：

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
- **模型（#893）**：在 3D 模式下，检查器在可搜索的 Model 字段（`ProjectModels`：项目中的 `.gltf` 和 `.glb` 文件，以及 `game.yaml` 资源部分中模型目标的输出，清除按钮则不指定模型）上方展示 **MODEL** 摘要卡片（`ObjectModelSummary3D`：离屏缩略图、项目相对路径、三角形数量与不同材质数量），并提供 **MATERIAL** 摘要（`ObjectMaterialSummary3D`：材质文件名及粗糙度、金属度读数）、**COLLIDER** 摘要（`ObjectColliderSummary3D`：例如 `凸包 · static`）、面向编组对象的 **CHILDREN** 子对象摘要卡片（`ObjectChildrenSummary3D`），以及 **让智能体添加组件…**（`OpenComponentAgentPrompt`）。`SetObjectModel(id, path)` 拒绝无法加载的模型，也拒绝为绘制内置基本形体的对象指定模型，正如对象绘制模型时 Mesh 字段不可用；更换模型会清除 `modelNode` 和 `modelMesh`，因为它们指向旧模型的部件；指定模型会把版本 1 的场景提升到版本 2（第一个支持模型的版本），更高版本的场景保持不变。撤销会恢复这三个字段和版本。
- **属性**：Add Property 接受一个键和一种类型（`string`、`int`、`float` 或 `bool`），添加的属性取该类型的零值（`AddProperty(id, key, kind)`）；该字段会去掉键首尾的空白，空键、首尾带空白的键或对象已有的键会被拒绝。值为整数的浮点属性保存时带小数点（`2.0`），因此再次打开时仍是浮点数。每个属性的删除按钮会删除该属性（`DeleteProperty`）。
- **组件**：Add Component 字段会筛选对象尚未拥有的已注册类型，按下其中一个即添加该组件，并写入每个已注册字段的默认值，让文件能看出组件的作用（`AddComponent(id, name)`）。按回车会添加字段中写出的类型，或唯一匹配的那个类型。对象已有的组件、注册表不认识的名称，以及首尾带空白或含有 `.`（连接在 `component.event` 中使用它）的名称都会被拒绝；没有注册表时，该字段可以添加任意名称、不含字段的组件。如果新组件声明的事件或处理器被某个连接以裸名称引用，而对象的另一个组件也声明了它，添加也会被拒绝，因为连接将无法说明指的是哪一个：请先把它写成 `component.event`。在实例化或继承其他场景的场景中，这项检查针对通过编辑器的解析器解析后的场景进行，包括它所引用场景的连接；如果解析器无法加载场景文件，或者即使不加新组件场景也无法解析和绑定，则跳过检查。每个组件的移除按钮会移除该组件（`RemoveComponent(id, name)`），连同所有需要它的连接：从该对象发出、事件由该组件声明的连接，以及指向该对象、处理器由该组件声明的连接，无论写成裸名称还是 `component.name`；裸名称通过组件的注册类型来归属，因此没有注册表、或注册表不认识该类型时，只会移除写成 `component.name` 的连接。`ComponentConnections(id, name)` 列出这些连接，撤销会把组件和连接放回原处。没有注册类型的组件仍按文件中的值显示，字段照常可编辑；由于没有结构定义，其字段中值为整数的浮点数保存为 `2`，再次打开时是整数。
- **连接（#872）**：Connections 区域列出当前选中对象的已注册组件声明的全部事件（`ObjectEvents`），以及所有与该对象相关的发出连接和传入连接（`ObjectConnections`）。点击 **Connect** 会展开内联表单：选择事件、从可搜索的对象列表（`SceneObjectIDs`）选择目标对象或开启拾取模式直接从 2D/3D 视口或场景树点选目标（`BeginConnectionPick` / `CancelConnectionPick`，拾取期间保持当前选中对象不变）、选择目标对象的已注册处理函数（`ObjectHandlers`）、使用组件注册表提供的带类型控件填写参数，并切换 `once`。每条连接都支持跳转到另一端（`JumpToConnectionOtherEnd`）、编辑（`UpdateConnection`）或移除（`RemoveConnection`）。添加、编辑与移除连接（`AddConnection`、`UpdateConnection`、`RemoveConnection`）都是可撤销命令，在进入 `History` 前先用 `sceneformat.Validate` 和 `sceneformat.ValidateConnections` 校验：无效连接（例如目标对象缺少该处理函数、重复连接、参数类型不符或超出范围）会被拒绝并在状态栏报告原因，同时保持场景与撤销历史不变。未设置组件注册表时，已有连接依然可以查看、跳转、编辑和移除。
- **场景设置（#871、#884）**：没有选中对象、房间、图层或资源文件时（`ShowingSceneSettings`），检查器会显示当前打开场景自身的设置，而不是空白占位：场景 `name`（`SetSceneName`）、虚拟尺寸 `virtualWidth` 与 `virtualHeight`（`SetVirtualSize`）、背景色 `backgroundColor`（结合 NSGo 颜色选择器 `ui.ColorWell`、CSS 十六进制输入框与清除按钮，`SetBackground`），以及默认 2D 相机 `camera`（`SetCamera`、`SetCameraPosition`、`SetCameraZoom`、`SetCameraRotation`）和可开关的限制边界 `bounds`（`SetCameraBounds`、`ClearCameraBounds`）。每次修改都会在改动文档或撤销历史之前通过 `sceneformat.Validate` 校验——非正虚拟尺寸、非正缩放、非正边界或非法颜色字符串会被拒绝并在状态栏报告——实时更新视口，保存与重新加载后保持不变，并完整接入撤销/重做。在 2D 编辑模式下显示场景设置时，视口会高亮绘制相机的初始视口框与虚线边界，在相机位置绘制可拖拽手柄（`CameraHandleCanvasPos`、`HitTestCameraHandle`、`BeginCameraDrag`、`UpdateCameraDrag`、`EndCameraDrag`、`CancelCameraDrag`），并在 `camera.bounds` 上绘制 8 个角/边缩放手柄（`CameraBoundsHandleCanvasPos`、`HitTestCameraBoundsHandle`、`BeginCameraBoundsDrag`、`UpdateCameraBoundsDrag`、`EndCameraBoundsDrag`、`CancelCameraBoundsDrag`），拖拽手柄可实时调整边界大小（拖拽边框或空白内部可平移边界），按 Escape 取消，松手以单个可撤销步骤提交。
- **房间地图（`*.rooms.yaml`，#884）**：可从文件浏览器在当前场景旁打开已有 `*.rooms.yaml`（`OpenRoomsProjectFile`、`OpenRoomsFile`、`OpenRoomsYAML`），或在场景设置检查器中新建房间地图（`NewRoomMap`）。所有房间都会连同其 ID、名称、过渡标记以及选中房间的 8 个缩放手柄直接绘制在 2D 场景视口之上。开启 **Draw Room**（`SetRoomCreateMode`）后可在视口拖拽绘制新房间，也可点击 **Add Room**（`AddRoom`、`AddRoomAtViewCenter`）添加；在视口中可通过手柄选中、移动和缩放房间（`HitTestRoom`、`HitTestRoomHandle`、`BeginRoomDrag`、`UpdateRoomDrag`、`EndRoomDrag`、`CancelRoomDrag`），并通过 Delete 按钮或 Delete/Backspace 键删除房间（`DeleteRoom`）。开启 **Grid Snap**（默认开启，`SetRoomSnapToGrid`）时，房间边缘会自动吸附到场景瓦片网格（`RoomGridSize`，取自场景的内联或外部瓦片地图，或通过 `SetRoomGridSize` 显式指定）。选中房间后检查器切换为房间检查器，可编辑房间的 `id`（`SetRoomID`）、`name`（`SetRoomName`）、边界（`SetRoomRect`）以及房间级 `transition`（`SetRoomTransition`：继承、`cut`、`slide`、`fade` 及时长、缓动和淡入淡出颜色），也可编辑地图级默认过渡（`SetRoomMapTransition`）。所有房间编辑都接入 `Editor.History` 撤销栈、`HasUnsavedChanges` 与 `Save`，并通过 `rooms.Encode` 确定性保存（`SaveRooms`、`SaveRoomsFile`、`SaveRoomsYAML`），保存的文件可由 `rooms.Parse` 读取并保持逐字节往返一致。
- **图层检查器（#885、#877）**：未选中对象但选中图层时（例如在场景树中点击图层行、在图层面板右键菜单选择 **Inspect Layer**、在空瓦片调色板点击 **Set Up Tilemap**，或调用 `SetActiveLayer`），检查器显示该图层的视差系数（`Parallax X`、`Parallax Y`）、平铺重复设置（`Repeat` X/Y、`Repeat W`、`Repeat H`）以及 **Tilemap** 瓦片地图配置。在 Go 中，`SetLayerParallax(layer, x, y)` 与 `SetLayerRepeat(layer, repeatX, repeatY, width, height)` 编辑视差与重复，`AddLayerTilemap(layer, tileset, tileW, tileH, w, h)`、`SetLayerTilemapTileset(layer, tileset)`、`SetLayerTilemapTileSize(layer, tileW, tileH)`、`ResizeLayerTilemap(layer, left, right, top, bottom)`、`ExtractLayerTilemap(layer, refName)`、`InlineLayerTilemap(layer)` 与 `RemoveLayerTilemap(layer)` 创建并编辑图层瓦片地图，全部通过 `History` 支持撤销与重做。从左边缘或顶边缘扩展或收缩网格时会平移已绘制单元格并调整 `offsetX`/`offsetY`，使每个已绘制瓦片保持原有世界坐标（写入非零偏移时会把版本 1 或版本 2 场景升级到版本 3，撤销时恢复）。非法尺寸以及缺少已绘制瓦片 ID 对应帧的图集会被拒绝并在状态栏提示。将图层瓦片地图移至顶层 `tilemaps` 会创建 `ref` 引用，移回内联时若不再有其他图层或 `layerOverrides` 引用则一并清理顶层条目。屏幕空间图层禁用视差与重复控件，继承自基础场景的图层禁用全部图层检查器编辑。

### 项目

编辑器也能打开游戏的[项目文件](project.md)：`OpenProject` 读取 `game.yaml` 并打开其中的主场景，`EditProject` 以可撤销的步骤修改它，`SaveProject` 写入它并同步旁边的 `nsgo.yaml`。运行 `go tool game editor <dir>` 即可打开一个项目目录。设置了 `ProjectAssets`（`editorcmd` 包的编辑器命令会设置它）时，打开项目会把 `Resolver` 换成项目目录上的 `project.Resolver`：场景用项目自己的精灵图集绘制，图集按名称从离当前场景最近处查找，方式与 `game check` 相同；资源浏览器列出这些图集，`PlaceScene` 实例化项目的场景文件，材质、物理材质、YAML 图块集以及 Tiled 和 LDtk 地图也从项目中加载，其中图块集和地图像 `FSAssets` 一样通过它的 `ReadFile` 读取。场景引用的图集从引用它的文件处就近查找，因此从其他目录实例化或继承的场景文件，会用 `game check` 为它找到的图集绘制。编辑器每次做出可能增加、删除或改写图集和材质文件的改动（例如安装或移除包、移动文件、`ReloadAssets` 或 `RefreshSearch`）后，都会重新读取它们；保存场景则保留已经读取的内容。`OpenProjectScene(path, scene)` 打开项目时用场景文件 `scene` 代替主场景，`go tool game editor` 打开位于项目目录中的场景文件时就是这样做的，因此 `ScenePath` 会指向它，在它自身中放置它的实例会作为循环被拒绝。`MoveFile(from, to)` 移动项目中的文件或目录，并像 `go tool game mv` 一样改写所有指向它的引用，包括磁盘上的文件和当前打开的场景与项目（见[稳定引用](project.md#稳定引用)）；这是一个步骤，可以撤销，也可以重做。撤销会把移动改写过的每个文件逐字节恢复原样；如果某个文件在移动之后又被改过，则只把其中的引用改写回去。

打开项目后，资源浏览器下方的文件浏览器（`FilesView`）列出项目中的文件和目录（`ProjectFiles`），不列出 `game index` 和 `game mv` 同样跳过的目录：名称以 `.` 或 `_` 开头的目录和名为 `testdata` 的目录。点击一项即可选中它，并把它的路径填入列表下方的输入框；改好路径后点击“Move”，就会通过 `MoveFile` 重命名或移动它；选中 `.yaml`/`.yml` 文件时也可以点击“Open Table”，在数据表编辑器中打开它（`OpenDataTableProjectFile`）；点击“Find Refs”则列出整个项目中指向该文件的所有引用（`ShowReferences`）。编辑器之外的操作改动了文件后，点击“Refresh”会重新列出文件并重新扫描项目搜索与引用索引（`RefreshSearch`）。

## 数据表编辑器（#918）

数据表面板（`DataTablePanelView`）可以从文件浏览器把带版本号的 YAML 数据表（[`datatable`](datatable.md)）以电子表格网格的形式打开在视口下方：

- **列与值类型**：列是所有行键名的并集，按首次出现的确定顺序排列（注册了 Go 行类型时，也会补齐任何行尚未设置的字段）。每一列的值类型（`string`、`number`、`bool`、`duration` 或 `list`）根据单元格的值推断，或直接取自已注册的 Go 行类型；布尔单元格渲染为切换按钮，标量和列表单元格在按回车或失焦时提交。
- **行操作与视图控制**：支持添加（`AddDataTableRow`）、删除（`DeleteDataTableRow`）和重排行（`MoveDataTableRow`），点击任意列头即可排序（`SetDataTableSort`），并可按不区分大小写的子串过滤可见行（`SetDataTableFilter`）。排序和过滤只改变视图显示，不会改动底层 YAML 文档的行顺序。
- **撤销与无损往返**：打开、关闭、添加、删除、重排和编辑单元格全部进入编辑器共用的 `History` 撤销栈。打开数据表后不做修改直接保存，文件内容逐字节完全一致；修改单个单元格时只改动那一行，同时保留注释、格式以及每一行自己的键顺序。
- **Go 行类型校验与面板状态**：游戏自己的编辑器命令可以用 `editor.RegisterDataTableType[T](ed, pattern)`（或 `Editor.RegisterDataTableRowType`）按路径或文件名模式注册 Go 结构体行类型。匹配的数据表在打开和每次编辑单元格或行时，都会按与 `datatable.LoadYAML` 完全相同的规则校验，在网格中拒绝未知列、重复字段和无效单元格值，并在可关闭的面板横幅（`DataTableError` / `ClearDataTableError`）和状态栏中显示 `*datatable.CellError`。异步表加载流程也可通过 `DataTableLoading` / `SetDataTableLoading` 切换加载状态卡片。

```go
if err := editor.RegisterDataTableType[Enemy](ed, "data/enemies/*.yaml"); err != nil {
	log.Fatal(err)
}
```

### 逐行审查智能体的数据表改动（#1033）

当智能体为一个或多个数据表文件提出修改建议时（通过 `Editor.OpenDataTableReview` 或 `datatable_review_propose` MCP 工具），编辑器会打开全窗口的逐行数据表审查界面（`DataTableReviewView`），完全采用设计规范配色与控件令牌：

- **本轮文件与空数据表**：左侧栏列出本轮涉及的每个数据表文件（如 `27 of 40 changed`、`new · 0 records`）；选中尚无记录的新建数据表时显示 `"No records — ask the agent to add some"`（中文界面为“暂无记录 — 请让智能体添加一些”）。
- **筛选标签、搜索与排序**：支持带实时计数的 **All / Changed / Unchanged / Reverted / Yours** 筛选标签（`SetDataTableReviewFilter`）、跨列实时筛选输入框（`SetDataTableReviewSearch`），以及点击任意列头在升序、降序与文件原序之间循环排序（`CycleDataTableReviewSort`，通过 `DataTableReviewAriaSort` 暴露 `aria-sort` 状态）。含改动的数值列以 `Δ` 标注列头并按涨跌幅百分比排序，且不改写底层文件行顺序。
- **旧值 → 新值单元格、就地编辑与拖拽重排**：有改动的单元格采用设计规范 `changed` 虚线胶囊显示删除线旧值 `→` 新值及 `Δ%`，数值 `0` 显示为 `—`（`FormatReviewCellValue`）。每条改动记录提供独立的 **Revert / Restore** 按钮（`ToggleDataTableReviewRecord`）。点击单元格可就地修改新值（`StartDataTableReviewCellEdit`、`SetDataTableReviewCellText`、`StepDataTableReviewCellNumber`），拖动行首手柄可重排记录顺序（`ReorderDataTableReviewRecord`）；两者都会作为你的编辑纳入本次审查，以实线强调边框显示、可由 **Yours** 标签筛选，并在右侧焦点记录的 `+`/`-` YAML 差异面板（`DataTableReviewFocusedYAML`）中标注 `you`。
- **智能体核验指标**：智能体可通过 `SetDataTableReviewChecks` 或 `datatable_review_checks` MCP 工具为本轮审查附上具名核验指标（`DataTableReviewCheck`：指标名、当前值、请求前的值及测算依据）。一旦作者还原、恢复、就地编辑或重排记录，指标会被标记为 **已过期**（`DataTableReviewChecksOutOfDate`），直到智能体重新刷新。
- **暂停审查、无损保留与单步撤销**：按 `Esc` 或点击 **Editor** 会暂停审查（`PauseDataTableReview`），继续暂停自动保存（`HasOpenReview`），并弹出带 **Resume** 按钮的“审查已暂停”提示条，同时在 Agent 面板中展示待继续卡片（`ResumeDataTableReview`）。点击 **Keep N of M**（`KeepDataTableReview`、`⌘↵`）会基于原始 `datatable.Document` 字节仅修补保留与编辑过的单元格行，未改动行与已还原行保持逐字节不变，并将所有文件变更合并为**单个原子撤销步骤**（一次 `Undo` 即可逐字节还原原文件并删除本轮新建的文件）。**Revert all**（`RevertAllDataTableReview`、`⌘⌫`）不触碰磁盘文件并支持通过 `⌘Z` 撤销恢复审查。

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

在 2D 编辑模式下，视口会在对象的局部坐标系中绘制选中对象的每一个已注册形状字段（`FieldPoint`、`FieldRect`、`FieldCircle` 和 `FieldPolygon`，自动计入父对象变换、旋转、缩放、图层空间与视差）。切换 **View > Show All Component Shapes**（或命令面板、`SetShowAllShapes`、`CmdToggleAllShapes`、`Editor.ShowAllShapes`）可同时绘制场景中所有可见对象的形状字段，并保持选中对象高亮显示在最上层。

使用 Select 或 Move 工具时，选中对象的形状字段会显示可交互手柄：

- **点（Point）**：拖动手柄移动 `(x, y)`。
- **矩形（Rect）**：拖动 4 个角手柄或 4 个边中点手柄缩放尺寸（拖过对边时平滑翻转并保持 `width >= 0`、`height >= 0`）；当中心手柄不与对象原点移动方块重叠时，拖动中心手柄可整体平移矩形。
- **圆（Circle）**：拖动 4 个正交圆周手柄调整 `radius`（限制 `>= 0`）；当圆心偏离对象原点时，拖动圆心手柄可平移圆。
- **多边形（Polygon）**：拖动任意顶点手柄移动顶点；点击或拖动边中点手柄插入新顶点（`AddPolygonVertex`）；右键点击或 Alt+点击顶点手柄删除顶点（`RemovePolygonVertex`，至少保留 3 个顶点）。
- **网格吸附**：切换 2D 视口栏中的 **Snap** 开关（`viewport-bar-snap`、**View > Snap to Grid**、`SetSnapToGrid`、`CmdToggleSnapGrid`、`Editor.SnapToGrid`）或在吸附设置弹出面板（`view-snap-settings`、**View > Snap Settings...**、`CmdSnapSettings`）中配置，可将手柄拖动与插入顶点吸附到 `EffectiveGridStep`（开启像素吸附时吸附到整点；拖动期间按住 `Ctrl` / `Cmd` 可临时反转吸附）。

每次拖动、添加顶点和删除顶点均通过 `SetComponentField` 提交为一个可撤销步骤；按 Escape、手势取消、双指介入、撤销/重做、Play、保存或切换工具会取消进行中的拖动并无损恢复字段原值。在 Go 中，可通过 `VisibleShapes2D`、`ShapeHandles2D`、`PickShapeHandle2D`、`BeginShapeDrag2D`、`UpdateShapeDrag2D`、`EndShapeDrag2D`、`CancelShapeDrag2D`、`DraggingShape2D`、`ActiveShapeHandle2D`、`AddPolygonVertex`、`RemovePolygonVertex` 和 `HandleCanvasSecondaryPress` 驱动同一流程，检查器也为每种形状字段提供对应的可编辑控件。

### 吸附与 2D 网格叠加层（#869）

编辑器在对象放置、拖动、2D 手柄（变换控件、组件形状、2D 灯光/遮挡体与 2D 导航区域）以及 3D 操纵杆（平移、旋转与缩放）中提供统一的吸附支持：

- **视口栏与菜单中的吸附设置**：通过 2D 视口栏中的 **Snap** 开关（`viewport-bar-snap`）、**View > Snap to Grid** 或 `CmdToggleSnapGrid`（`SetSnapToGrid`）切换网格吸附，或打开紧凑的吸附设置弹出面板（2D 视口栏中的 `view-snap-settings`、3D 底部栏中的 `view3d-snap-settings`、**View > Snap Settings...** 或 `CmdSnapSettings`）分别开关各项设置：
  - **网格吸附**（`SnapToGrid`、`SetSnapToGrid`）：将位置吸附到 `EffectiveGridStep()`；当 `GridSize` 为 `0` 时默认跟随当前活动图层的瓦片地图瓦片尺寸（支持非正方形瓦片），图层无瓦片地图时回退为 `8`。设置显式步长（`SetGridSize`）会覆盖瓦片尺寸，点击 **自动**（`ResetGridSize`）可恢复自动跟随。
  - **像素吸附**（`SnapToPixel`、`SetSnapToPixel`、`CmdToggleSnapPixel`）：关闭网格吸附时将位置吸附到整点（1 单位步长）。
  - **旋转吸附**（`SnapRotation`、`SetSnapRotation`、`CmdToggleSnapRotation`）：将 2D 与 3D 旋转拖动吸附到 `RotationSnapDeg` 度对应的弧度倍数（`SetRotationSnapDeg`，默认 `15°`）。
  - **缩放吸附**（`SnapScale`、`SetSnapScale`、`CmdToggleSnapScale`）：将 2D 与 3D 缩放拖动吸附到 `ScaleSnapStep` 的倍数（`SetScaleSnapStep`，默认 `0.25`），并防止缩放塌缩为零。
  - **修饰键反转**：拖动或放置点击时按住 `Ctrl` 或 `Cmd` 可临时反转吸附——吸附开启时临时关闭以进行自由精调，吸附关闭时临时开启（`SetSnapInvert`）。
- **可选 2D 网格叠加层**（`ShowGrid`、`SetShowGrid`、`CmdToggleShowGrid`）：即使在瓦片工具之外，也在可见 2D 画布上按当前吸附步长绘制细线网格，跟随 `View2D` 平移、缩放、旋转及当前图层的空间/视差；缩放到很小时会自动倍增绘制间距以免网格遮挡画面。
- **多选对象整体移动吸附**：通过 `SelectObjects`、`ToggleSelectObject` 或在 2D `ToolSelect` 中按住 Shift 点击可选中多个对象（`SelectedIDs`）。拖动其中任一选中对象（或使用 Move 工具与 3D 平移操纵杆）时，主锚点按步长吸附，其余所有非子级选中对象按完全相同的已吸附位移同步平移，保持彼此相对偏移不变，并作为单个可撤销步骤提交（按 Escape 取消并全部恢复）。
- **按项目保存的编辑器偏好**：吸附设置（`SnapSettings`、`SetSnapSettings`）属于编辑器偏好，按项目保存在 `.game/editor.yaml`（`ViewStateFile`）中，从不写入场景文件，也不会使场景变为未保存状态。在同一项目内切换场景会保持当前项目的吸附设置；切换项目时则分别保存与恢复各自项目的偏好。

## 3D 视口（#890）

在 3D 模式下，视口按游戏绘制的样子显示场景：一个 `gpu.View` 填满面板，用 render3d 绘制场景的 3D 构建结果，叠在它上面的画布绘制地面网格、原点坐标轴、选中轮廓和变换控件。构建结果来自 `scenemodel.Build`（`Scene3D`）：每个对象的内置图元（`mesh`）或 glTF 模型（`model`、`modelNode`、`modelMesh`）及其材质和纹理，包括实例化场景加入的对象。编辑器的 `Resolver` 实现了 `scenemodel.ModelResolver`、`sceneformat.MaterialResolver` 或 `scenemodel.ImageResolver` 时，模型、材质文件和图片通过它加载，否则从打开的项目的文件中加载。场景无法构建时（例如它引用的模型加载失败），视口角落会有一行文字说明原因（`Viewport3DError`），每个对象仍然显示包围盒。

- **光照与天空**：当版本 4 场景定义了光源或 `environment` 时，视口通过 `scenemodel.Renderer`（`PrepareScene`、`RecordShadows`、`DrawSky`、`ClearColor`）直接渲染场景自身的光源、阴影、天空（纯色、渐变或经由 `scenemodel.HDRResolver` 加载的 HDR 立方体贴图）、环境光/IBL、雾效、色调映射与曝光，与游戏渲染路径逐像素一致。场景既无光源也无环境时，视口回退到渐变天空与 `DefaultLights3D`。基于本包构建的编辑器仍可用 `SetLights3D`（`Editor.Lights3D`）覆盖光照，此时背景为场景的 `backgroundColor`。
- **着色**：3D 视口栏的着色菜单（`view3d-shading`）、`View > 3D Shading` 以及命令 `view.shading.lit`、`view.shading.unlit`、`view.shading.wireframe` 切换 `Shading3D`（`SetShading3D`）：Lit（受光）、Unlit（每个表面带纹理的基础色加自发光色，不受光照）和 Wireframe（受光，并在上面画出三角形的边）。着色是视图设置，不可撤销，也不会保存。
- **拾取与嵌套子对象（#894）**：每一层嵌套对象（最深 64 层，包括实例化场景与继承场景加入的子对象）都按与 `BuildNodes3D` 一致的组合世界变换（沿父链组合 `ObjectLocalTransform3D`）绘制，覆盖网格、线框包围盒、光源/相机/探针图标、物理调试线框、遮挡体与组件控件。按下会选中指针下最深层的命中对象：绘制网格或模型的对象经由构建结果上的 `scene3d` 拾取按三角形命中；没有几何体的对象只在未命中几何体时按世界空间包围盒命中（同一射线同时命中祖先与后代时优先选中更深层的后代）。再次点击已选中的嵌套子对象会逐级向上选中其父对象（直至所属场景实例根节点），到达顶层后循环回最深层子对象；按住 `Alt`/`Option` 点击（`Select3DAtWithModifiers`、`HandleViewport3DPressWithModifiers`、`HandleCanvasPressWithModifiers`）会直接选中实例化子对象所属的场景实例根节点或本地嵌套子对象的直接父对象（并可与 `Shift` 加选、`Mod` 切换组合）。隐藏和锁定的对象及其后代照旧被跳过。
- **父空间变换控件与实例覆盖（#894）**：选中嵌套子对象时，3D 变换控件（`Translate`、`Rotate`、`Scale`，单选与多选均支持）绘制在子对象的组合世界位置与朝向上；拖拽时对象在世界空间内紧随指针移动或旋转，同时对父链求逆写回子对象在父空间中的局部变换。拖拽来自实例化场景或基础场景的对象时，会在所属实例的 `overrides`（或继承场景的 `SceneDoc.Overrides`）中记录覆盖项而不改动源场景文件，可无损往返 YAML，并作为单个可撤销步骤提交。
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
| 聚焦选中对象 | | | `f` 或底部视图栏的 Focus `F` 按钮，按其边界取景；未选中任何对象时取景整个场景 |
| 轴向视图 | | 点按右上角方向控件上的轴向按钮（`X`、`Y`、`Z` 或负轴端） | `1` 前视、`ctrl+1` 后视、`3` 右视、`ctrl+3` 左视、`7` 顶视、`ctrl+7` 底视，或在方向控件上通过 Tab/Enter 激活 |
| 透视或正交 | | | `5`，或左上角视图栏中的 Persp 与 Ortho |
| 着色模式 | | | 左上角视图栏中的着色菜单（`Lit`、`Unlit`、`Wireframe`） |
| 变换空间（局部 / 全局） | | | `t`、底部视图栏中的 Local / Global 切换，或命令 `gizmo.3d.space` |
| 回到默认视图 | | | 左上角视图栏的 Reset view 按钮 |

- **鼠标导航映射（`Editor.Navigation3D`、`Navigation3D.Resolve`，设置 › 视口 › 3D 导航）**：支持三种桌面鼠标方案（触控板滚动、捏合缩放与双指触控平移在所有模式下保持不变）：
  - **默认（`Navigation3DDefault`）**：左键拖动空白处环绕（落在操纵杆或工具上时拖动它们），Shift+左键拖动或中键拖动平移，右键拖动飞行，滚轮推拉缩放。
  - **Blender（`Navigation3DBlender`）**：中键拖动环绕，Shift+中键拖动平移，右键拖动飞行，滚轮推拉缩放，左键拖动保留给选择、工具和操纵杆。
  - **Maya（`Navigation3DMaya`）**：Alt+左键拖动环绕，中键拖动（或 Alt+中键拖动）平移，Alt+右键拖动或滚轮推拉缩放，普通右键拖动飞行，普通左键拖动保留给选择、工具和操纵杆。
- **视口叠加栏与状态栏**采用编辑器设计规范色板：左上角视图栏包含 Persp/Ortho 分段切换、着色菜单（`Lit`、`Unlit`、`Wireframe`）、当前视图名称（`ViewName3D`：轴向视图名或 `Perspective`，正交时附加 ` · ortho`，偏离坐标轴的正交视图为 `User · ortho`）、Reset view 以及 Camera/Env 切换，正下方显示带键帽的操作提示条（随当前 3D 导航映射实时更新，默认模式下为 `Drag orbit · Middle pan · Right WASD fly · F focus`）。右上角圆形方向控件在“向左/向右环绕 15°”按钮上方提供全部六个轴向端点的可点击、可键盘聚焦按钮。底部栏提供 `Local` / `Global` 变换空间切换（`GizmoSpace3D`、`SetGizmoSpace3D`）、吸附设置弹出框（`view3d-snap-settings`）、3D 叠加层弹出面板（`view3d-overlays`：LOD、Occluders、Render Stats、3D NavMesh）、GridMap 工具（`Paint`、`Erase`、`Pick`、`Box`）与 `Focus F` 按钮。状态栏同时显示以度为单位的偏航角与俯仰角（` · yaw N° · pitch M°`）以及当前操纵杆空间（` · Local` / ` · Global`，中文界面显示 ` · 局部` / ` · 全局`）。
- **正交**（`SetOrthographic3D`、`OrbitCamera3D.Orthographic`）以平行投影显示。视图高度为 `OrthoHeight`，即透视视图在注视点处显示的高度，因此切换时位于注视点的物体大小不变，缩放会改变这个高度。`Project`、`ScreenRay`、`SceneCamera`、拾取和变换控件在两种投影下行为一致：正交时每个像素的射线都沿视线方向，因此按下会选中其下方的物体，拖动变换控件时对象沿轴移动的距离等于指针沿该轴移动的距离。正交下的轴向视图就是真正的平面图或立面图。
- **飞行**（`BeginFly3D`、`FlyLook3D`、`FlyMove3D`、`EndFly3D`）让相机绕视点转动，并让视点和注视点一起移动。飞行期间 W、A、S、D、Q、E（无论是否按住 Shift）移动相机，而不执行它们平时的快捷键（变换控件的 W 和 E、旋转与擦除瓦片的 E）；平台支持时指针会锁定在视口内。飞行以透视方式观察，所以正交视图会切换为透视；既没有转动也没有移动相机的一次右键点击会保持正交。飞行速度 `FlySpeed3D` 以世界单位每秒计，初始为相机到注视点的距离，滚轮每次将其加倍或减半（`AdjustFlySpeed3D`）。
- **轴向视图**（`SetView3D`，命令 `view.3d.front`、`view.3d.back`、`view.3d.right`、`view.3d.left`、`view.3d.top`、`view.3d.bottom`，以及切换投影的 `view.3d.orthographic`）保持注视点和距离不变。前视沿 -Z 方向看，右视沿 -X 方向看，顶视垂直向下看，视图上方为 -Z。
- **按场景保存**：项目中的每个场景都会以上次离开时的相机打开。编辑器在打开另一个场景、保存场景和关闭窗口时把视图写入项目中的 `.game/editor.yaml`（`ViewStateFile`，见[文件格式](formats.md)）；以其他方式结束编辑器的应用需先调用 `SaveViewState`。没有保存过视图的场景以 `DefaultOrbitCamera3D` 打开。编辑器创建 `.game` 文件夹时会在其中写入一个忽略整个文件夹的 `.gitignore`，使视图不进入版本控制。较新版本编辑器写出的文件（`version` 更高）既不会被读取，也不会被覆盖。

### 3D 变换操纵杆（#892）

3D 视口的变换操纵杆复用 `render3d.Gizmo` 完成**全局**（世界）或**局部**坐标空间下的绘制、拾取与约束拖拽：

- **模式与快捷键**：在 3D 视口下按 `w`（平移，`Gizmo3DTranslate`）、`e`（旋转，`Gizmo3DRotate`）与 `r`（缩放，`Gizmo3DScale`）（属于 `ScopeViewport3D` 作用域，不与 2D 瓦片工具的 `e` 冲突），也可在顶栏工具组、`View > 3D Gizmo` 或命令面板中切换。
- **全局 / 局部空间切换**：通过 3D 底部视图栏中的 **Local** / **Global** 切换（`view3d-space-local` / `view3d-space-global`）、快捷键 `t`（`CmdGizmoSpace3D`）、`View > 3D Gizmo > 3D 操纵杆：局部 / 全局空间` 或 `SetGizmoSpace3D` / `ToggleGizmoSpace3D` 在 `GizmoSpaceGlobal` 与 `GizmoSpaceLocal` 间切换（`Editor.GizmoSpace3D`）。局部模式下手柄对齐选中对象经 Gram-Schmidt 正交归一化后的世界朝向——对嵌套子对象包含其所有祖先的旋转（`GizmoOrientation3D`）。
- **绕所绘轴的精确旋转**：拖拽任意旋转圆环（X、Y 或 Z）时，按所绘圆环对应的精确世界轴复合旋转，再把子级局部结果（`RParentᵀ · RDeltaWorld · RWorldStart`）转换回场景欧拉角 XYZ（`Rz·Ry·Rx`），因此即使对象已绕多个轴旋转或嵌套在已旋转、缩放的父级下，每个圆环也严格绕所绘轴旋转。
- **平面手柄、屏幕空间中心手柄与等比缩放**：平移提供 `Axis3DXY`、`Axis3DXZ`、`Axis3DYZ` 平面方块（严格在对应的双轴平面内移动，并带掠射角保护）与中心 `Axis3DAll` 手柄（在相机视平面内平移）；缩放提供中心 `Axis3DAll` 立方体手柄，按比例同时缩放三个轴。
- **吸附与状态栏实时增量**：3D 平移、旋转和缩放共享编辑器吸附设置（`SnapSettings`、`EffectiveGridSize`、`EffectiveRotationSnapDeg`、`EffectiveScaleSnapStep`）及 `Ctrl`/`Cmd` 临时反转（`SetSnapInvert`）。拖拽期间状态栏实时显示变换增量（`GizmoDragDelta3D`：`dx`/`dy`/`dz`、角度 `°` 或缩放倍率 `x`）及当前坐标空间（全局/局部），每次完成的拖拽提交为**单个撤销步骤**并可无损往返场景 YAML（按 `Escape` 取消并恢复拖拽前变换）。

### 在 3D 中放置模型（#893）

资源浏览器在图集下方列出可放到 3D 中的模型（`Models`）：先是内置基本形体，然后按路径列出当前打开项目的 glTF 模型，`game assets` 模型目标的输出即使尚未构建也会列出（`ModelEntry.Output`）。每个模型都有一张离屏渲染的缩略图（`ModelThumbnail`）：只有模型本身，从前上方取景，由 `DefaultLights3D` 照明，经由 scenemodel 在 NSGo 的软件 GPU 上绘制。每个模型在其缩略图第一次绘制时于 UI goroutine 上加载，与编辑器 `Resolver` 提供的一切资源相同，且不进入场景自身的模型缓存；渲染在 UI goroutine 之外进行，所以列表立即显示，图片随后补上。无法加载的模型没有缩略图。

- **放置工具**：按下一个模型会选中它（`SelectModel`、`SelectedModel`），在 3D 中还会启用放置工具（也可通过 `p`、`View > Tools > Place` 或 `CmdToolPlace` 切换）。之后在 3D 中按下，就会添加一个绘制该模型、以它命名的对象：位置是指针射线命中场景几何体的地方；没有命中时，是射线与地平面（高度为 `PlaceHeight3D` 的水平面）相交的地方（`PlacementAt3D`、`PlaceModelAt3D`）。对象落在该点上：它沿表面法线抬高其几何体在原点以下延伸的距离，所以放在地面上的单位立方体是立在地面上，而不是一半埋在地下。隐藏和锁定的对象会被跳过，与拾取相同。
- **吸附与对齐**：在 3D 中启用放置工具时，上下文放置选项栏（`view3d-place-bar`）以及 `View > Tools` 和命令面板（`CmdTogglePlaceSnap3D`、`CmdTogglePlaceAlign3D`）提供 **Snap**（`place3d-snap`，让地平面上的点的 X 和 Z 吸附到视口所绘网格的间距，`PlaceSnap3D`；场景几何体上的点不吸附，因为网格间距可能比指针下的对象还宽）、**Align**（`place3d-align`，让放在场景几何体上的模型转动，使其 +Y 轴与表面法线一致，`PlaceAlign3D`）以及地面高度字段（`place3d-height`，设置 `PlaceHeight3D`）。它们是视图设置，不是编辑（`SetPlaceOptions3D`）。
- **拖放**：把模型从资源浏览器拖到 3D 视口上，会按放置工具的规则把它放在落下的位置，不论当前是哪个工具。从其他应用拖到视口上的 `.gltf` 或 `.glb` 文件，只要位于项目文件夹内、且不在资源浏览器略过的文件夹中（名称以 `.` 或 `_` 开头的文件夹，以及 `testdata`），就会被放置（`DropModelFiles`），多个文件算作一步；项目之外的文件、其他类型的文件以及来自网页、没有路径的文件会被拒绝，原因显示在状态栏中。文件拖放不带位置，所以拖入的文件落在视图中心所指的地方。
- **底部 MODELS 条**：在 3D 视图（非资源抽屉模式）下，3D 视口底部显示 **MODELS** 模型条（`Viewport3DModelsStripView`、`StripModels`，附提示“拖入视口”），按顺序先展示当前项目的离屏渲染模型缩略图再展示内置基本形体，每张卡片附“拖入视口以放置 `<name>`”工具提示，支持点击选中或直接拖入 3D 视口放置；右侧固定 **或让智能体生成一个** 按钮（`OpenModelAgentPrompt`），点击后展开内联提示词栏（“描述一个模型… 例如一个 1 × 0.5 米的矮石花盆，与围墙材质相同”），提交时通过 `SubmitViewport3DAgentPrompt` 向 `Editor.OnAskAgent3D` 分发 `Viewport3DAgentRequest`。
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

### 反射探针与光照探针网格（#903）

版本 4 场景同时支持局部 3D 反射探针（`ObjectDoc.ReflectionProbe`）和 3D 光照探针网格（`ObjectDoc.LightProbeGrid`），可在编辑器中直接放置、编辑边界并烘焙，使 `scenemodel` 运行时着色与编辑器 3D 视口逐像素一致：

- **放置与检查器**：通过 `AddReflectionProbe3D` / `AddLightProbeGrid3D`（或命令面板 `CmdAddReflectionProbe` / `CmdAddLightProbeGrid`）放置探针，并在检查器（`REFLECTION PROBE` 经由 `SetObjectReflectionProbe`，`LIGHT PROBE GRID` 经由 `SetObjectLightProbeGrid`）中以可撤销步骤编辑局部边界盒（`center`、`size`）、混合距离、强度、优先级、盒投影、立方体贴图分辨率、网格采样数与烘焙文件路径。
- **视口图标、线框与 3D 边界手柄**：反射探针绘制青色镜面球图标、外层影响范围线框与内层混合线框；光照探针网格绘制琥珀色点阵图标、边界线框与采样点。选中探针后显示 6 个可拖拽的面中心边界手柄（`Handle3DProbeBoundsPosX`..`Handle3DProbeBoundsNegZ`），拖拽时固定对面调整边界并在松手时合并为单个可撤销步骤（`EndHandleDrag3D`），按 Escape 可取消恢复原值（`CancelHandleDrag3D`）。
- **烘焙**：点击检查器中的 **Bake Cubemap** / **Bake Probes**、场景环境面板中的 **Bake Probes**，或执行 `CmdBakeProbes3D`（`BakeReflectionProbe3D`、`BakeLightProbeGrid3D`、`BakeAllProbes3D`）。反射探针从探针中心渲染 3D 场景并写出 6×1 Radiance `.hdr` 立方体贴图长条（`render3d.EncodeCubemapHDR`）；光照探针网格在每个采样点渲染并把 L2 球谐辐照度系数写出为确定性的带版本 YAML（`*.probes.yaml`，`render3d.EncodeLightProbeGrid`，Schema `https://game.nsgo.dev/schemas/lightprobes.v1.json`）。输出路径校验路径穿越、符号链接与保留文件名后原子写入，以可撤销步骤链接回探针对象，并由 `scenemodel.Build` 与 `scenemodel.Renderer` 加载，使游戏与编辑器着色完全一致。

## 瓦片调色板（#874）

在 2D 视图中启用 Paint、Erase、Fill、Pick 或 Terrain 工具时，视口右侧会显示瓦片调色板。它把当前图层瓦片集的瓦片按瓦片 ID 顺序排成网格，每个瓦片按瓦片地图的瓦片尺寸显示（比面板还宽的瓦片会缩小到面板宽度）。这些瓦片是瓦片集图集中名称为瓦片 ID（`"1"`、`"2"`……）的帧，与场景格式查找瓦片的方式一致；其他帧不会列出。点击一个瓦片，或用方向键在瓦片之间移动，即可把它设为 Paint 和 Fill 写入的瓦片（`SetActiveTileID`）；所有方向键都按 ID 顺序逐个移动，左键和上键移到上一个瓦片，右键和下键移到下一个，而不是按网格的行移动。选中的瓦片带有一圈高亮，使用 Paint 或 Fill 时状态栏显示它的 ID。选择瓦片不算编辑，不进入撤销历史；之后的绘制是一个可撤销的步骤。

调色板跟随当前图层：图层改变时，调色板显示新图层的瓦片集；如果该瓦片集没有所选 ID 的瓦片，就选中它的第一个瓦片。没有内联瓦片地图的图层（没有瓦片地图，或使用外部 Tiled 或 LDtk 地图）、解析器无法解析的瓦片集、没有瓦片帧的瓦片集，都会显示一条说明原因的消息，而不是空网格。在 Go 中，`TilePalette()` 返回调色板为当前图层显示的内容：瓦片集、瓦片尺寸和瓦片，或那条消息。

对引用了 `tilesetFile` 的单层或分层网格，调色板显示它的 YAML 图块集中的瓦片，图块集通过解析器的 `sceneformat.FileResolver`（例如 `FSAssets`）读取，瓦片 ID 按 `PaintTile` 接受的方式从 1 开始：瓦片 ID `n` 绘制图块集的局部瓦片 `n - 1`。调色板的标签和状态栏用局部 ID 称呼每个瓦片，也就是行和图块集的 `tiles[].id` 写的数字（`PaletteTile.Number`），底部提示碰撞、地形和变体规则保存在图块集文件中。瓦片上方每个瓦片图层（分层网格）有一个切换按钮，用来选择 Paint、Erase、Fill 和 Terrain 修改的图层（`SetActiveTileLayer`、`ActiveTileLayer`，默认为第一个），H、V、D 切换按钮设置 Paint 和 Fill 放置瓦片时的翻转（`SetActiveTileFlip`、`ActiveTileFlip`），地形切换按钮选择 Terrain 绘制的地形集（`SetActiveTerrain`、`ActiveTerrain`），**Edit Tileset** 按钮会在图块集编辑器中打开该图层的 `*.tileset.yaml`。在 2D 瓦片工具下选中 YAML 图块集图层且未选中对象时，右侧**瓦片检查器**会展示所选瓦片的缩略图、名称与来源单元格、`COLLISION` 分段开关（`Solid`、`One-way`、`None`）、地形/碰撞/变体权重规则以及打开完整图块集编辑器的快捷按钮。这些编辑按 `Encode` 保存的形式写入图层的行，每次都是一个可撤销的步骤；Fill 只连通瓦片和翻转都相同的单元格。

## 绘制、擦除与拾取瓦片（#875）

使用 Paint 或 Erase 时，用鼠标或单指拖动会绘制或擦除拖动经过的每一个格子，无论移动多快都不会漏格，整笔是一个可撤销的步骤，所以一次 Undo 就能撤回一次拖动画出的整行。单击或轻点只绘制或擦除它所在的那一格。使用 Paint 时，按住右键拖动会擦除。从地图外开始的一笔在进入地图后才开始绘制，没有改变任何格子的一笔不会记入历史，按住 Space 拖动则是平移视图。第二根手指会取消单指的一笔并恢复它改过的格子，所以双指平移永远不会绘制。

启用瓦片工具时，鼠标下的格子会显示对齐网格的轮廓：Paint 和 Fill 在格子里半透明地显示所选瓦片，Terrain 以翠绿轮廓半透明预览自动匹配的地形瓦片，Erase 把格子染成红色，按下会拾取时轮廓为琥珀色。

拾取会把某个格子里的瓦片设为要绘制的瓦片，调色板也会选中它：在任何瓦片工具下 Alt+单击（或 Alt+拖动），或使用 **Pick** 工具（`ToolPickTile`，可通过瓦片调色板的 **Pick** 按钮、`View > Tools > Pick Tile` 或 `CmdToolPickTile` 启用），拾取后会切换到 Paint。拾取空格子时所选瓦片保持不变。拾取不算编辑，不进入撤销历史。

在 Go 中，`BeginTileStroke(mode, x, y)`、`ContinueTileStroke` 和 `EndTileStroke` 在画布坐标上画一笔（`TileStrokePaint`、`TileStrokeErase`、`TileStrokePick`、`TileStrokeTerrain` 或 `TileStrokeEraseTerrain`），`CancelTileStroke` 撤回这一笔，`TileStrokeActive` 报告是否有一笔正在进行；绘制与擦除会拒绝锁定或隐藏的图层，选择工具或图层、修改图层、切换视图、保存、Undo、Redo 和 Play 都会先结束正在进行的一笔（Revert 则会取消它）。`PickTile(layer, col, row)` 拾取瓦片，`SetTileHover` 和 `ClearTileHover` 移动轮廓，`HoveredTile` 返回轮廓所在的格子。

## 地形绘制与图块集编辑器（#878、#879）

由 `*.tileset.yaml` 驱动的内联与分层瓦片地图在运行时（`tilemap.ApplyTerrain`、`tilemap.SetTerrainTile`、`physics.MoveAndSlide`、`tileshape.New`）与编辑器中共用同一套瓦片标志、碰撞形状、动画、自定义数据与自动瓦片地形集：

- **地形画笔（`ToolTerrain`，快捷键 `t`）**：左键拖动放置地形（`TileStrokeTerrain`），右键拖动擦除地形（`TileStrokeEraseTerrain`），按与 `tilemap.Tilemap.SetAutotile` 完全一致的 4 位正交（`cardinal`）和 8 位角门控（`blob`）规则更新所经过的格子及其周围邻居。整笔（含放置/擦除的格子与所有受影响的邻居）记为一个可撤销步骤，一次 Undo 即可完整还原。在 Go 中，`PaintTerrain(layer, col, row, terrain)` 与 `EraseTerrain(layer, col, row, terrain)` 执行单格地形编辑。
- **图块集编辑器（`TilesetEditorPanelView`）**：可通过瓦片调色板的 **Edit Tileset** 按钮（`OpenActiveLayerTileset`）、右侧瓦片检查器、在文件浏览器中双击或点击 **Open** / **Edit Tileset**（`OpenTilesetProjectFile`）或 `OpenTilesetFile` / `OpenTilesetYAML` 打开任意 `*.tileset.yaml`。左侧网格展示每个瓦片的缩略图及实心/单向碰撞、碰撞形状、动画、地形掩码和默认瓦片标记，右侧四个标签页编辑每个瓦片：
  - **Terrain**：新建或删除 4 位（`cardinal`）和 8 位（`blob`）地形集（`AddTilesetTerrain`、`RemoveTilesetTerrain`、`SetTilesetTerrainMode`）、设置显式默认瓦片（`SetTilesetTerrainDefault`），以及通过点击中心、四条边（`N`、`E`、`S`、`W`）和 8 位 `blob` 模式下的四个角（`NW`、`NE`、`SW`、`SE`）为瓦片分配邻居掩码（`ToggleTilesetEdgeOrCorner`、`ToggleTilesetTileCenter`、`SetTilesetTileMask`）。点击角会自动满足角门控规则（启用角时自动启用相邻两条边；关闭边时自动清除依赖该边的角）。若中间点击产生的掩码已被另一个瓦片占用，编辑器会保持原瓦片不变，便于继续点击到达其他掩码或点击 **Reassign**（`ReassignTilesetEditMask`）一步重新分配。
  - **Collision**：切换 `Solid`、`One-way`、`None` 或自定义标志位（`SetTilesetTileCollision`、`SetTilesetTileFlags`、`ToggleTilesetTileFlagBit`），并在放大的瓦片画布上直接绘制矩形（`rect`）或多边形（`polygon`）碰撞形状（`AddTilesetTileShape`、`UpdateTilesetTileShape`、`RemoveTilesetTileShape`、`AddTilesetDraftPolygonPoint`、`CommitTilesetDraftPolygon`）。
  - **Animation**：为瓦片添加、编辑或删除动画帧及其时长（`AddTilesetTileAnimFrame`、`UpdateTilesetTileAnimFrame`、`RemoveTilesetTileAnimFrame`、`SetTilesetTileAnimation`），并提供可播放/暂停的实时预览（`SetTilesetAnimPlaying`、`TilesetAnimPreviewTile`）。动画瓦片在 2D 编辑器视口（编辑模式与 Play 模式）以及游戏中都会实时播放（`StepTileAnimations`）。
  - **Data**：编辑瓦片的显示名称 `name`、变体权重 `weight` 及类型化自定义属性（`bool`、`int`、`float`、`string`）（`SetTilesetTileName`、`SetTilesetTileWeight`、`SetTilesetTileProperty`、`DeleteTilesetTileProperty`）。
  - 所有图块集编辑都进入编辑器共用的 `History`（支持撤销/重做），保存前即实时生效于视口、Play 模式和地形画笔，并以规范 YAML 写回文件（`SaveTileset`、`SaveTilesetYAML`）。智能体可通过编辑器 MCP 工具 `tileset_read` 与 `tileset_edit` 原子读写同一批字段（[MCP](mcp.md#工具)）。

## 3D GridMap 绘制（#899）

在 3D 视图（`ViewMode3D`）下，编辑器可以直接打开并在可调高度的地板网格上绘制模块化 3D [`*.gridmap.yaml`](world3d.md#模块化-3d-gridmap) 文件：

- **打开与关联**：在检查器中设置对象的 `gridMap` 字段（`SetObjectGridMap`），或直接打开一个 `*.gridmap.yaml` 文件（`OpenGridMap`、`OpenGridMapFS`、`OpenGridMapDoc`、`NewGridMap`）。在 3D 底部视口栏中切换到任一 GridMap 工具（**Paint**、**Erase**、**Pick** 或 **Box**）、**View > Tools** 或命令面板（`CmdGridMapPaint`、`CmdGridMapErase`、`CmdGridMapPick`、`CmdGridMapBoxFill`）时，若尚未打开网格地图则会自动打开所选对象引用的 `gridMap`；`MoveFile` 也会同步跟踪打开的 GridMap 路径及其 `meshLibrary` 引用。
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

## 智能体瓦片工具与按区域审查瓦片编辑（#1064）

连接到编辑器 MCP 服务器（[MCP](mcp.md#工具)）的智能体可以直接查询并绘制内联或分层瓦片地图，而作者则以连通区域为单位审查改动，无需逐格检查：

- **查询与绘制工具**：`tile_query`（`Editor.QueryTiles`）按瓦片 ID、YAML 图块集 `tileName`、空单元格或子矩形查询格子与连通区域/空腔（例如 `"an 11 × 5 pocket at columns 2–12, rows 12–16"`）；`tile_paint`（`Editor.AgentPaintTiles`）支持显式格子、闭合矩形以及带固定种子的确定性加权随机瓦片抽取（可选 `noAdjacentDuplicates`）；`tile_autotile`（`Editor.AgentAutotile`）使用与 `ToolTerrain` 相同的 4 位 `cardinal` / 8 位 `blob` 规则重绘或放置 YAML 图块集地形；`tile_erase`（`Editor.AgentEraseTiles`）清除指定格子或矩形。每次调用都会作为一个可撤销编辑并默认纳入当前瓦片审查（`review: true`）。
- **按区域审查（`TileReviewPanelView`）**：待审查的智能体瓦片改动会按类型分组为连通区域——**painted**（`+`，绿色虚线轮廓）、**replaced**（`~`，琥珀色虚线轮廓）和 **erased**（`−`，红色实线轮廓），每项显示格数、包围盒及可读详情（例如 `"from 4 cave tiles"`）。点击区域会在 2D 视口中高亮聚焦，每个区域可独立切换 **Revert** / **Restore**（`SetTileReviewRegionReverted`、`ToggleTileReviewRegion`）；通过 **Before / After** 切换或按住 **`B`** 键（`SetTileReviewViewMode`、`SetTileReviewPeekBefore`、`TileReviewHandleKeyEvent`）可实时预览修改前的瓦片地图。**Keep N of M**（`KeepTileReview`）会将所有保留的区域合并提交为 `History` 中的**单个**可撤销步骤（一次 Undo 即可干净还原到审查前状态），**Revert all**（`RevertAllTileReview`）则立即还原全部改动。智能体与测试也可通过 `tile_review_status`、`tile_review_revert_region`、`tile_review_restore_region`、`tile_review_keep` 与 `tile_review_revert_all` 驱动同一套审查状态。
- **调色板与视口标题**：在 2D 视图选中含内联瓦片地图的图层时，视口左上角会显示图层尺寸标题（`ActiveTileLayerCaption`，例如 `World · 24 × 20 cells · 16 px`），瓦片调色板顶部显示 `TILESET <file>`，并在底部展示所选瓦片的缩略图、名称、图集单元格坐标、碰撞规则与所属地形集。

## 场景树（#861）

编辑器左侧的场景树面板（`SceneTreeView`）列出打开场景的各个图层，并在每个图层下列出其对象及嵌套的子对象，相当于 Godot 的 Scene 面板和 Unity 的 Hierarchy。每次编辑和撤销后它都会随之更新。按下对象所在的行会选中该对象；在任一视口中选中对象时，会选中它的行并展开其上层的各行；按下图层的行会把它设为当前图层。所有对象编辑对嵌套子对象与顶层对象一样有效：`FindObject` 和 `SelectObject` 在整棵树中按 ID 查找对象，检查器编辑所选的子对象，其变换在父对象的空间中编辑，因此移动工具会把它的原点放到按下的位置，3D 操纵器沿世界坐标轴移动它。

- **在树中移动**：把一行拖到另一行上，它就成为该对象的最后一个子对象；拖到那一行的上边缘或下边缘附近，则放在该对象之前或之后；拖到图层的行上，则移到该图层的顶层。行的上下文菜单可以把对象在同级之间上移或下移、设为上方同级对象的子对象、移出父对象，或移到另一个图层。在 Go 中，`ReparentObject(id, parentID, index)`、`MoveObjectToLayer(id, layer, index)`、`ReorderObject(id, index)` 和 `DropObject(id, targetID, place)` 完成同样的操作，每次都是一个可撤销的步骤；index 小于 0 或超出末尾时，对象放在最后。
- **世界变换**：被移动的对象保持其世界变换。它在新父对象下的局部变换会在 2D 中重新计算（`x`、`y`、`rotation`、`scaleX`、`scaleY`），撤销会把每个字段精确还原；翻转过的对象会在原来的轴上保留负的缩放。当对象本身或其新旧祖先设置了 3D 字段时，它的深度（`posZ`、`scaleZ`）也会保持。当其中某个对象设置了 3D 旋转（`rotX`、`rotY`、`rotZ`）时，2D 与 3D 视图对 `x` 和 `y` 指向哪里不再一致，此时只保持 3D 世界变换：对象在 3D 视口中保持不动，在 2D 视口中则可能移动。以下移动会被拒绝：把对象放到它自己或其后代之下、嵌套超过 `MaxObjectDepth` 层，或者在新父对象下得到的变换无法写成平移、旋转和缩放，例如把精灵放到一个经过旋转且沿单轴拉伸的父对象下。在世界图层与屏幕图层之间移动时，保留的是数值，而不是对象在屏幕上的位置。
- **重命名**：F2、双击或上下文菜单中的“Rename”会在所选对象的行内编辑其名称；按回车或离开输入框即保留修改，按 Escape 则放弃。“Change ID”通过 `SetObjectID(oldID, newID)` 编辑其 ID：在同一步中改写场景中引用该对象的连接，以及它实例化的场景中的对象（`enemy_1/body` 变为 `boss/body`），还有打开的动画片段的目标及其各轨道的目标，并拒绝场景已在使用的 ID，包括实例化或继承的场景带来的 ID。实例化或继承本场景、并按旧 ID 覆盖该对象的其他场景文件不会被改写；这类引用请用 `game mv` 移动或手动修改。
- **眼睛与锁**：行上的眼睛按钮在两个视口中隐藏该对象及其后代，锁按钮使两个视口中的按下操作不再拾取它，按下会落到它下面的对象上（`SetObjectHidden`、`SetObjectLocked`）。两个视口都不会移动隐藏或锁定的对象：不显示它的 3D 操纵器，移动工具也不会移动它。二者都属于编辑会话：它们不是编辑，撤销不会影响它们，也从不写入场景文件；运行（Play）时绘制所有对象，打开另一个场景时会清除它们。隐藏或锁定的对象仍可在场景树中选中，并在检查器中编辑。

3D 视口会绘制并拾取所有对象，包括嵌套的子对象：`Objects3D` 按深度优先列出它们，并给出各自父对象的 ID；子对象的几何体和包围盒由其祖先的变换确定位置（[3D 视口](#3d-视口890)）。

### 对象上下文菜单（#1039）

在 2D 视口、3D 视口或场景树行中对对象右键点击或 Control-点击，或者对当前选中对象按下 `Shift+F10` / 菜单键（`contextmenu`），都会打开统一的对象 `ui.ContextMenu`，顶部标题显示对象名称及其类型和所在图层（例如 `Sprite · 图层 actors`）。在 3D 视口按住右键拖拽仍会飞行相机，在 2D 瓦片工具下右键拖拽仍会擦除或框选瓦片区域，在多边形碰撞体顶点上右键仍会删除该顶点；对于当前对象或会话不适用的菜单项会直接省略，而不是显示禁用项。

- **复制为 YAML（`mod+c`）与粘贴（`mod+v`）**：`CopyObjectYAML` / `CopySelectedYAML`（`ObjectYAML`）将对象编码为规范的 `sceneformat` YAML 文档写入剪贴板（通过 `core.Scope.SetClipboard` 或 `Editor.WriteClipboard`），`sceneformat.Parse` 可将其精确还原为同一个对象，并在视口上方显示浮动提示条（`Copied <name> as YAML · N lines`）。`Paste` / `PasteObjectYAML` 从剪贴板粘贴到当前图层，在单个可撤销步骤中分配新的唯一 ID。
- **复制副本（`mod+d`）与编组（`mod+g`）**：`DuplicateObjects` 复制选中对象，`GroupObjects` / `GroupSelected` 在单个可撤销步骤中把选中的对象包装到一个新建父对象之下并保持世界变换。
- **移至子级（Move under）**：子菜单（`MoveUnderCandidates`、`MoveObjectUnder`）列出所有合法的候选父对象——排除自身、后代以及会超出 `MaxObjectDepth` 的目标——并标注当前父对象、锁定状态、子对象数量或所在图层；重设父对象只占一个撤销步骤，一次撤销即可原样还原。
- **选择相同预制体（Select same prefab）**：当对象设置了 `prefab` 或 `scene` 时，`SelectSamePrefab` 会选中场景中所有共享同一引用的对象（`SelectedIDs`、`SelectObjects`、`IsObjectSelected`）。
- **隐藏/显示（`h`）、锁定/解锁、查找用法（`shift+f12`）、重命名（`f2`）与向智能体询问此项（`mod+l`）**：切换会话可见性（`ToggleObjectHidden`）和锁定状态（`ToggleObjectLocked`），在打开的项目中查找对该对象 ID、预制体或实例化场景的引用（`FindObjectUsages` / `ShowObjectUsages`，对场景/预制体路径打开查找用法界面，对普通 ID 打开搜索面板，未打开项目时省略），在场景树中开启行内重命名（`RequestRenameObject`），或将 `AgentPromptRequest` 草稿发送给 `Editor.OnAskAgent`（`AskAgentAboutObject`）。上下文菜单会实时读取自定义快捷键（`EffectiveShortcut`），对场景实例与继承/外部对象保留覆盖操作（打开实例化场景、转为本地对象、将覆盖应用到源场景、打开源场景、还原覆盖）并禁用外部对象的本地结构编辑，同时在场景树行中保留树顺序、剪切、粘贴为子对象、取消编组、修改 ID、移动到图层与删除操作。
- **MCP 对等能力（`pkg/editor/editormcp`）**：每个上下文菜单操作都调用一个导出的 `Editor` 方法，并由编辑器 MCP 服务器同步暴露（`object_copy_yaml`、`object_paste_yaml`、`object_duplicate`、`object_group`、`object_move_under`、`object_select_same_prefab`、`object_visibility` 与 `object_find_usages`，配合既有的 `scene_edit` 以及 `selection_get` 的 `selectedIds`）。

### 多对象选择与批量编辑（#867）

编辑器维护有序的多对象选区集合，并保留 `Editor.SelectedID` 作为主选中对象（`SelectedIDs`、`SelectionCount`、`IsSelected`、`SelectedObjects`、`SelectObjects`、`SelectObjectsWithPrimary`、`AddToSelection`、`RemoveFromSelection`、`ToggleSelection`、`ClearSelection`）：

- **视口选择与框选（Marquee）**：在 2D 和 3D 视口中，按住 `Shift` 点击将对象加入选区，按住 `Mod`（`Cmd`/`Ctrl`）点击切换对象选中状态（`HandleCanvasPressWith`、`HandleViewport3DPressWith`）。在 2D 的选择工具下，在空白画布上拖拽可绘制框选矩形（`BeginMarquee2DWith`、`UpdateMarquee2D`、`EndMarquee2D`、`CancelMarquee2D`、`SelectMarquee2D`），默认选中完全包含的对象，按住 `Alt` 改为选中相交的对象（`MarqueeEnclose`、`MarqueeTouch`）；`Shift` 追加、`Mod` 切换匹配对象。按 `Escape` 优先取消进行中的拖拽或框选，静止时清空选区。
- **场景树同步**：`SceneTreeView` 跟随并驱动同一多选集合，支持 `Shift`+点击与 `Mod`+点击，并自动展开所选对象的祖先节点。
- **公共中心变换、删除与复制**：2D 和 3D 变换操纵器以选中对象的组合包围盒中心为支点整体移动、旋转和缩放（自动排除祖先也已选中的子对象，避免重复变换），`DeleteSelected` 与 `DuplicateSelected` 作用于整个选区；每次操作通过 `History.Group` 记为**单个撤销步骤**。`FrameSelection` 会同时聚焦所有选中对象。
- **共享字段检查器**：选中多个对象时，`InspectorView` 显示“已选中 N 项”，仅展示所有选中对象共同拥有的分组（变换、全部带精灵时的精灵、共有属性与共有组件）。取值不同的字段显示为“混合”（复选框显示半选态），修改任一字段均通过 `History.Group` 在**单个撤销步骤**内写入所有选中对象（`EditSelected`、`SetSelectedComponentField`、`SetSelectedProperty`、`SetSelectedX` 等），一次撤销即可将每个对象恢复到各自原有的值。

### 拷贝、粘贴、复制与编组对象（#868）

场景树支持多选对象（`SelectObjects`、`SelectedIDs`、`ToggleSelectObject`、`IsObjectSelected`），对象剪贴板、创建副本与编组操作均可通过“编辑”菜单、场景树右键上下文菜单、命令面板和快捷键触发：

- **拷贝（`mod+c`、`CopyObjects`、`CopySelected`）与剪切（`mod+x`、`CutObjects`、`CutSelected`）**：将选中对象连同其嵌套子对象、组件、属性、`overrides` 以及两端均落在选中子树内的所有连接，编码为规范的 `sceneformat` YAML 写入系统剪贴板（`core.Scope.SetClipboard` 或可注入钩子 `Editor.WriteClipboard`），可直接粘贴到另一场景或文本编辑器。剪切会在**单个撤销步骤**中移除原对象及其连接。当 2D 瓦片工具下存在活动瓦片选区时，`mod+c` 与 `mod+x` 仍按原行为拷贝或剪切瓦片区域。
- **粘贴（`mod+v`、`Paste`、`PasteWith`、`PasteYAML`、`PasteYAMLWith`）与粘贴为子级（`mod+shift+v`、`PasteInto`）**：从系统剪贴板读取场景格式 YAML（`core.Scope.ReadClipboard` / `Editor.ReadClipboard`），将对象放置到当前图层 `ActiveLayer`（或指定父对象 `ParentID` 下），优先落在视口悬停指针处（`SetViewportPointer2D` / `SetViewportPointer3D`），否则在目标场景已有同名 ID 时相对原位置偏移排列。目标场景未占用的原始 ID 会直接保留，已占用的 ID 会分配新的唯一 ID，同时将内部连接与场景覆盖（含实例化路径 `"enemy_1/body"`）改写为新 ID，并在目标场景已有主 3D 相机时自动清除副本的 `current` 标志。所有粘贴都经过 `PlaceObject` 的完整校验（`sceneformat.Validate`、`ValidateComponents`、`ValidateConnections`、`ValidatePhysicsLayers` 以及实例化场景的 `Resolve`）并作为**单个撤销步骤**提交；无法解析或校验失败的 YAML 会在状态栏报错拒绝，不修改场景与撤销历史。
- **复制副本（`mod+d`、`DuplicateObject`、`DuplicateObjects`、`DuplicateSelected`、`DuplicateSelectedObjects`）**：在**单个撤销步骤**中将选中对象克隆到同一图层与父级下且紧邻原对象的位置，分配唯一 ID 并改写内部连接与覆盖，且不覆盖系统剪贴板。
- **编组（`mod+g`、`GroupObjects`、`GroupSelected`）与取消编组（`mod+shift+g`、`UngroupObject`、`UngroupSelected`）**：编组在选中对象的 2D/3D 世界中心处新建空父对象并将它们归入其下，通过 `keepWorldTransform` 换算局部变换使屏幕位置与世界变换保持不变，记录为**单个撤销步骤**。取消编组在**单个撤销步骤**中将直接子级提升到父级所在层级并保持其世界变换；对于无可视化/组件内容且无连接的空分组容器会自动移除，非空父对象则保留自身。
## 3D 导航网格烘焙（#901）

在 3D 视图模式下，编辑器可直接从当前打开场景的静态几何体烘焙 Recast 风格的 3D 导航网格（`navbake3d`），并在 3D 视口上实时预览：

- **NavMesh 烘焙面板**（`NavMeshPanelView`，可通过 **View > 3D NavMesh Bake...**、命令面板 `CmdNavMeshPanel` 或 `SetNavMeshPanelOpen` 开关）：配置代理名称、半径、身高、可攀爬高度（`MaxClimb`）、最大可行走坡度（`MaxSlope`）、体素单元尺寸（`CellSize`）、单元高度（`CellHeight`）和输出 `.navmesh.yaml` 路径（`NavMeshBakeConfig` / `SetNavMeshBakeConfig`）。打开包含 `navigation` 配置的项目时，面板会自动以 `game.yaml` 中的设置为初始值。
- **烘焙与场景关联**（**Bake** 按钮、`CmdBakeNavMesh3D` 或 `BakeNavMesh3D`）：从当前打开场景的 glTF 模型（支持 `modelNode` 与 `modelMesh`）、内置 `mesh` 基本形体、静态非触发器 `collider3d` 形状（跳过动态/运动学 `body3d`、触发器与屏幕空间图层）以及可选回调 `Editor.OnBakeNavMesh3DGeometry` 提供的地形高度图、`GridMap` 或物理碰撞体收集静态 3D 几何体；以原子方式写出确定性的 `.navmesh.yaml`，在未设置或路径变化时以单步可撤销操作更新 `SceneDoc.NavMesh`（`SetSceneNavMesh`，亦可在场景设置检查器中编辑），并立即加载烘焙结果到视口预览。重新打开已关联 `.navmesh.yaml` 的场景时会自动加载预览（`LoadNavMesh3D`）。
- **3D 视口叠加层**（3D 视口栏 **Overlays** 弹出面板、面板 **Overlay** 开关、**View > Show 3D NavMesh**、命令面板 `CmdNavMeshOverlay` 或 `ShowNavMesh3D` / `SetShowNavMesh3D`）：通过 `Camera3D` 在 3D 场景视口上绘制半透明青色可行走多边形填充、细节边和清晰的多边形外轮廓。

## 场景标签页、场景文件与未保存的修改（#866，#873）

编辑器在视口上方的标签栏（`SceneTabsView`）中为每个打开的场景保留独立标签页，因此作者可以一边编辑被实例化或被继承的场景，一边编辑使用它的关卡，而不会丢失撤销历史：

- **单标签页独立状态**：每个标签页（`Tabs`、`ActiveTab`、`TabCount`、`SelectTab`、`SelectTabByID`）独立保存自己的 `SceneDoc`、`BuiltScene`、`History`、选中状态（`SelectedID`）、当前图层与瓦片选择、2D 和 3D 视口变换（`View2D`、`Camera3D`）、场景树隐藏/锁定会话标记、`OnSave` 回调以及未保存标记（`TabDirty`）。切换标签页绝不会清空任何标签页的撤销历史；在一个标签页中撤销不会改动其他标签页。
- **打开与复用已有标签页**：从文件浏览器（`*.scene.yaml`、`*.scene.yml` 或 `autoload.scenes` 中列出的场景）、问题面板、搜索与引用面板、命令面板、`NewScene`、`OpenScene` 或 `OpenFile` 打开场景时，会新开一个标签页（若当前只有未动过的初始无标题标签页则直接替换它）；如果该场景已在某个标签页打开，则直接切换到该标签页。**Open Instanced Scene**（`OpenInstancedScene`，位于实例化对象的检查器、场景树右键菜单、File 菜单与命令面板）与 **Open Base Scene**（`OpenBaseScene`，位于继承场景的 Scene Settings 检查器、File 菜单与命令面板）同样在新标签页中打开所引用的场景，或在已打开时直接切换过去。
- **保存时实时重建依赖标签页**：保存场景（`Save`、`SaveFile`、`SaveSceneAs`、`SaveTab`、`SaveAll`）后，会自动重建其他所有实例化或继承了场景的打开标签页（包括 `BuiltScene` 与 `Scene3D`），因此实例与继承对象无需重新打开就能立刻显示最新改动。`MoveFile` 也会同步更新所有打开标签页的路径和引用。
- **共享项目、插件、Play 与关闭标签页**：项目文件（`game.yaml`）、插件与 Play 模式在所有标签页之间共享，插件与 Play 作用于当前活动标签页（Play 运行期间禁止切换或关闭标签页）。关闭未修改的标签页（`CloseTab`、`RequestCloseTab`、`CmdCloseTab` / `mod+w` 或标签页关闭按钮）会立即关闭；关闭有未保存修改的标签页会弹出提示：Save（仅保存该标签页并重建依赖它的标签页）、Discard（仅丢弃该标签页的修改，保留共享项目文件的修改）或 Cancel。关闭最后一个标签页后会替换为一个干净的无标题场景。

顶栏的场景文件菜单（`toolbar-file`）包含：“New Scene”，把一个最小的有效场景（以文件名命名，大小与项目的 display 或窗口相同，带一个 world 图层）写到作者给出的、从项目根目录算起的路径并在新标签页打开（`NewScene`）；“Save”；“Save As”，把当前场景另存到另一个路径，并让该文件成为当前标签页的文件（`SaveSceneAs`）；“Revert”，把当前场景恢复为其文件中保存的样子（`RevertScene`）；以及“Open Instanced Scene”、“Open Base Scene”、设置与“Close Tab”。没有 `.yaml` 或 `.yml` 扩展名的路径会加上 `.scene.yaml`（以 `.scene` 结尾的路径加上 `.yaml`），`game check` 和 `game index` 就是按这个名称识别场景的。New Scene 和 Save As 都不会覆盖其他文件，也不会把场景写到项目自身文件的名称 `game.yaml` 和 `nsgo.yaml` 下。Revert 是一个可撤销的步骤，撤销即可找回被恢复掉的修改。

`SceneDirty` 报告当前活动场景是否与打开时或上次保存时的内容不同，它像 `ProjectDirty` 对 `game.yaml`、`ClipDirty` 对打开的动画片段那样比较编码结果，所以撤销回到已保存的状态后，场景、项目或动画片段又会变为未修改；`TabDirty` 检查指定标签页，`AnySceneDirty` 报告是否存在任何有未保存修改的标签页。若 `MoveFile` 改写了打开的场景的文件，场景保持原来的已修改或未修改状态；若它只改写了打开的场景中的引用而没有改写其文件（例如 `game mv` 不读取的场景：名称不是 `.scene.yaml`，也没有任何文件引用它），场景会变为已修改，保存后即可保留新的路径。每个有未保存修改的标签页都会显示 `*` 标记，顶栏显示当前活动场景的面包屑名称，并在任意打开的标签页、项目、动画片段或其他辅助文档有未保存修改（`HasUnsavedChanges`）时显示未保存圆点（`toolbar-unsaved`），窗口标题也随之变化（`WindowTitle`）。窗口标题由 `EditorView` 设置，因此基于本包构建的编辑器（例如游戏自己的编辑器）应通过 `Editor.AppName` 给出自己的名称，而不是自行设置标题；标题以该名称结尾（为空时为“NSGo Scene Editor”）。顶栏或桌面菜单栏的 **File > Save**、命令面板中的 Save 和 Mod+S（`Save`，即 `CmdSave` 命令）只保存当前活动场景与共享项目中修改过的内容；未修改的文件保持原样，注释也会保留。有任意打开的标签页、项目或动画片段存在未保存修改时，切换项目（`RequestOpenProject`）、退出编辑器和关闭窗口都会先询问：“Save”保存所有已修改的标签页和项目（`SaveAll`）后继续，“Discard”丢弃修改后继续，“Cancel”保留修改并停留。丢弃项目的修改时，由项目决定的状态也会像撤销这些修改时一样恢复，例如通过这些修改新增的音频总线进行的试听会停止。强制退出（如注销时）无法等待回答，不会询问。

### 重载磁盘上被修改的文件与保存冲突保护（#906）

绑定 UI 作用域后，编辑器会监视当前打开的场景（`FilePath`）、动画片段（`ClipPath`）、`game.yaml`（`ProjectPath`）以及项目目录树（`DiskWatchInterval`，默认 250ms；在 Go 中可调用 `PollDiskChanges` 立即检查）：

- **无未保存修改时自动重载**：当打开的场景、动画片段或 `game.yaml` 在磁盘上被外部修改且编辑器内没有未保存更改时，编辑器会自动以一个可撤销的 `History` 步骤重载（多文件 git 分支切换会合并为一步，如 `Switched branch · main → boss-room · 2 files`），更新已保存快照以保持未修改状态，并显示可关闭的重载提示（`DiskReloadNotice`），场景重载还提供对象级差异详情（**查看差异**）。
- **有未保存修改时绝不静默覆盖**：当带有未保存编辑的打开文件在磁盘上被修改时，编辑器保留内存中的编辑，并显示外部更改冲突条（`DiskConflicts`、`PrimaryDiskConflict`），提供**从磁盘重新加载**（`ReloadFromDisk`，把作者原有修改记入 `History` 的 `"Unsaved edits before reload"` 以便撤销找回）、**保留我的版本**（`KeepLocalVersion`，把磁盘版本记入 `History` 的 `"Changed outside the editor"` 并确认当前磁盘快照使后续保存可以通过），以及针对场景的**对比**合并器（`SetDiskCompareOpen`、`SetSceneConflictChoice`、`ApplySceneConflictMerge`），可对双端重叠修改的对象逐个选择**我的**或**磁盘**，仅单端修改的对象自动采纳该端结果。
- **保存冲突保护**：`Save`、`SaveFile`、`SaveProject` 与 `SaveClipFile` 在写入前都会将当前磁盘内容与打开或上次保存时记录的 SHA-256 快照比对；若文件已在外部变更，则拒绝覆盖并返回 `ErrDiskConflict`（`*DiskConflictError`）。在 `EditorView` 中触发冲突会弹出确认对话框（**取消**、**从磁盘重新加载**，或通过 `ConfirmOverwriteSave` / `ForceSave` 确认**覆盖**）。
- **文件浏览器自动刷新**：在项目目录中新增、删除或重命名文件时，文件浏览器（`FilesView`）会自动刷新列表，并同步失效项目索引、触发问题检查，无需手动点击刷新。

## 图层面板（#862）

场景树下方的图层面板（`LayersView`）按绘制顺序列出打开场景的各个图层，最先绘制的在最上面，每行显示图层的空间（`world` 或 `screen`）和瓦片地图：内联网格显示其瓦片集与尺寸（`tiles 16x12`），引用顶层瓦片地图的显示其名称，外部 Tiled 地图显示其路径。按下图层的行会把它设为当前图层：放置工具和各瓦片工具作用于它，瓦片调色板显示它的瓦片，状态栏显示它的名称，图层隐藏或锁定时名称后面会注明 `hidden` 和 `locked`；在场景树中按下图层的行效果相同。在 Go 中，`Layers()` 返回面板显示的内容。

- **添加与移除**：+ 按钮添加一个名为 `layer_N`、绘制在所有其他图层之上的世界图层，并把它设为当前图层（`AddLayer(name, space)`：取场景所绘制图层中最大的 order（包括其继承的场景的图层），并把图层放在文件末尾）。垃圾桶按钮移除当前图层（`RemoveLayer(name)`），连同它的对象和瓦片地图，以及场景中从其对象发出或指向其对象的连接；撤销会把它们全部放回原处，场景保存后与之前逐字节相同。移除当前图层后，文件中接替其位置的图层（没有时为前一个图层）成为当前图层。
- **重命名**：双击或上下文菜单中的“Rename”会在行内编辑名称；按回车或离开输入框即保留修改，按 Escape 则放弃（`RenameLayer(oldName, newName)`）。空名称和其他图层已有的名称会被拒绝。当前图层和图层的锁定状态会跟随新名称。
- **顺序**：把一行拖到另一行的上半部或下半部，图层就放到那个图层之前或之后；也可以用上下文菜单中的“Move Layer Up”和“Move Layer Down”（`MoveLayer(name, index)`，index 是在绘制顺序中的位置）。图层在文件中移到它要绘制在其后或其前的那个图层旁边；只有当它现有的 `order` 值无法让它绘制在那里时，才会改为那个相邻图层的值；其他图层的位置和 order 都不变。
- **空间**：上下文菜单中的“World Space”和“Screen Space”设置图层的 `space`（`SetLayerSpace(name, space)`）；其对象的坐标保持不变。
- **可见性**：眼睛按钮通过写入图层的 `visible` 字段来隐藏或显示图层（`SetLayerVisible(name, visible)`）。隐藏时写入 `visible: false`；显示时不设置该字段，即取默认值，因此隐藏再显示一个图层会得到相同的文件。这是场景本身的设置，游戏也会读取，并且是可撤销的编辑。隐藏的图层在两个视口中都不会被拾取，它的对象即使已被选中也不会在视口中被移动（不显示 3D 操纵器，移动工具也不拖动它们），与在场景树中隐藏的对象相同。
- **锁定**：锁按钮在本次编辑会话中锁定图层，使视口不能操作它（`SetLayerLocked(name, locked)`）：按下操作会越过它的对象，落到下面的对象上；视口中不会移动它的对象（不显示 3D 操纵器，移动工具也不拖动它们）；它是当前图层时，放置、绘制、擦除和填充工具不做任何修改，并在状态栏中说明原因。与场景树中对象的锁一样，它不是编辑，撤销不会影响它，也从不写入场景文件，打开另一个场景时会清除它；各面板和检查器仍可编辑该图层及其对象。

在继承另一个场景（`inherits`）的场景中，与基础场景某个图层同名的图层是对该图层的扩展：它的名称和绘制顺序归基础场景所有，因此重命名、移动这类图层，把图层重命名为基础场景图层的名称，以及用这样的名称添加图层都会被拒绝；而隐藏它、修改其空间，或编辑其视差与重复平铺则会以可撤销的方式写入 `layerOverrides`（检查器与图层右键菜单支持单字段还原和全部还原）；移除这类图层只会移除本场景添加到其中的对象。继承本场景并引用其图层名称的其他场景文件不会因重命名而被改写。任何会使场景无法通过校验、或无法与其实例化或继承的场景一起解析的图层编辑，都会在进入历史记录之前被拒绝。

## 动画编辑器

编辑器的动画面板在时间轴上为任何已注册属性打关键帧，并把片段保存为 [`animation` 资源文件](resources.md#animation)，由 [animclip](animclip.md) 在游戏中播放（相当于 AnimationPlayer）。打开片段时，面板显示在视口下方。

- **打开片段**：命令面板的 **Animate** 命令（`CmdAnimate`）为选中的对象新建一个一秒长的片段。在 Go 中，`NewClip(target, name, length)` 作用相同；`NewClipFromAsepriteTag(target, tag)` 根据目标对象图集中的 Aseprite 标签生成匹配帧序列、精确时长和循环设置的片段；`NewClipFromFrames(target, name, frames, fps, loop)` 按固定帧率从一组图集帧生成逐帧片段；`OpenClipFile(path, target)` 和 `OpenClipYAML(data, target)` 打开片段文件，并按组件注册表检查（拼错的属性会带着文件名打开失败）；`CloseClip` 关闭片段。当目标对象有精灵图集时，面板的 Flipbook 行也可以直接选择 Aseprite 标签（From Tag）或帧率（From Frames）创建片段。`go tool game editor scene.yaml clip.animation.yaml [object-id]` 打开一个场景并选中对象 `object-id`（省略时为场景中的第一个对象），片段文件存在时在该对象上打开片段，面板的 Save Clip 按钮会写回该文件。
- **打关键帧**：选择一个属性（对象字段，如 `x`、`alpha` 或 `sprite.frame`，或已注册的组件字段，如 `body.speed`），按 Key 在播放头处以该属性在场景中的当前值打关键帧；`KeyProperty(target, property, at)` 作用相同。选择 `sprite.frame` 时会出现帧选择器，列出目标对象图集中的帧（`ClipTargetFrames`），可直接为任意帧打关键帧（`KeySpriteFrame(target, frame, at)`）。选中的是其他对象时，关键帧会进入以该对象为目标的轨道。显式指定片段所属对象的轨道与目标为空的轨道视为同一条轨道，两条轨道驱动同一对象同一属性的片段会被拒绝。布尔关键帧保持其值直到下一个关键帧，`sprite.frame` 始终离散步进且不做插值。
- **编辑关键帧**：每条轨道是一行显示其关键帧的通道；`sprite.frame` 轨道以跨越各自持续时间的帧缩略图格显示每个关键帧（`ClipFrameEntry`）。按下关键帧即选中它并把播放头移到该处，然后可以编辑数值轨道的时间、值和插值方式（`linear`、`smooth` 或 `step`），或编辑 `sprite.frame` 轨道的图集帧（`SetKeyFrame`）和帧时长（`-10ms` / `+10ms`，`FrameDuration`、`SetFrameDuration`），或删除它；在 Go 中对应 `SetKeyValue`、`MoveKey`、`SetKeyInterp`、`DeleteKey` 和 `RemoveClipTrack`。值会按属性检查：布尔为 0 或 1，整数为整数值，并且在字段范围内；`sprite.frame` 帧名必须存在于目标对象的图集中。
- **片段设置与事件**：面板可编辑片段的名称、长度和循环（`SetClipName`、`SetClipLength`、`SetClipLoop`），并在播放头处添加具名事件（`AddClipEvent`、`DeleteClipEvent`）。
- **拖动与预览**：拖动条移动播放头（`SeekClip`；在循环片段的末尾显示最后的关键帧），Play 在 `game.Loop` 上播放片段（`PlayClip`、`PauseClip`，或用 `StepClip` 手动步进）。此时视口（2D 与 3D 均如此）显示按片段摆好姿态的场景副本，使用的 `animclip.Player` 和 `BindScene` 与游戏相同，因此正在编辑的场景不会被写入动画值，片段在游戏中的播放效果也与预览一致。场景无法播放的片段（例如轨道目标对象已被删除）仍保持打开，`ClipError` 会说明原因。异步片段加载流程可通过 `ClipLoading` / `SetClipLoading` 切换面板加载状态卡片。
- **撤销**：每一次片段编辑，包括打开和关闭，都与场景编辑共用同一个撤销/重做历史。

`SaveClipYAML` 和 `SaveClipFile` 输出确定性的 YAML，因此打开后再次保存的片段不会改变。

### 精灵状态机图（#1054）

打开 `*.states.yaml` 文件（通过文件浏览器、命令面板或 `OpenStatesFile` / `OpenStatesYAML` / `OpenStatesProjectFile`）会在视口下方打开**状态机**图面板：

- **状态图与当前状态**：每个状态以节点卡片展示名称、片段元信息（帧数、帧时长、循环/单次）与精灵缩略图，配有带标签的有向过渡箭头以及 `Any state` 通配徽章。预览期间当前状态节点实时高亮，预览卡片显示当前播放帧。
- **按键驱动（Drive with keys）**：开启 **Drive with keys** 后，预览通过与游戏相同的 `animclip.InputDriver` 和 `animclip.StateMachine` 绑定到项目的输入动作（如 `move_left`、`move_right`、`jump`、`attack`、`hurt`），在 `game.Loop` 的固定步长上推进，使交互预览与运行时播放逐帧一致。
- **出边列表（`LEAVING <STATE>`）**：选中任意状态即可查看其所有出边过渡（含 `Any state` 中断）及条件表达式；所有图编辑操作（`AddStatesState`、`RemoveStatesState`、`AddStatesTransition`、`RemoveStatesTransition`、`SetStatesEntry`、`SetStatesNodePos`、`SetStatesDoc`）均进入编辑器的撤销/重做历史，并以确定性规范 YAML 写回（`SaveStatesYAML` / `SaveStatesFile`）。

## 模组、插件与包（#1268）

引擎有三种扩展游戏的方式，它们不能互相替代：

| | 编辑器（Go）插件 | 脚本模组 | 库包 |
| --- | --- | --- | --- |
| 用什么写 | Go | TSX | YAML 与媒体文件 |
| 谁来写 | 游戏的开发者、第三方工具作者 | 玩家 | 任何人 |
| 何时加载 | 编译进编辑器程序 | 运行时加载，支持热重载（`Manager.Reload`） | 安装进项目 |
| 信任级别 | 完全信任 | 沙箱隔离，按能力授权 | 不含代码 |
| 平台 | 全部，包括 iOS 和 Web | 原生平台；Web 仅在设置 `ManagerOptions.WebUnisolated` 后可用 | 全部 |
| 扩展编辑器 | 可以 | 不能（目前） | 只能启用内置插件 |

编辑器插件是编辑器程序用 `Editor.RegisterPlugin` 注册、项目再在 `game.yaml` 的 `editor.plugins` 下按名字启用的 `editor.Plugin`，下一节详细说明。脚本模组是一份 `mod.yaml` 清单加一个 TSX 入口脚本，由 `modding.Manager` 在沙箱中运行，能做什么仅限于清单申请、宿主授予的能力；见 [modding](modding.md)。库包是 `library.Install` 写入项目并记录在 `game.yaml` 中的一组文件；见[资源与插件库](library.md)。包不得包含 Go 源码或模块文件（`.go`、`go.mod` 等都会被拒绝），所以插件包只能启用编辑器程序编译时已包含的插件，而不会携带它们的代码。

该选哪一种？

- 给编辑器加新的面板、检查器、视口工具或 3D 操纵杆，或让编辑器按某些组件类型检查场景：用编辑器插件，见下文。
- 让玩家在游戏设定的限度内改动已发布的游戏：用脚本模组。
- 分享精灵图、场景、声音或起步模板：用库包。

Go 插件编译进程序而不是在运行时加载，是因为 Go 没有跨平台的运行时加载代码的手段——iOS 和 Web 都不能在运行时加载原生代码——所以插件随它所扩展的编辑器命令一起发布。

## 编辑器插件（#492、#898）

插件为场景编辑器添加自己的面板、自己组件类型的检查器、2D 与 3D 视口工具以及按组件类型注册的 3D 操纵杆，作用与 Godot 的 `EditorPlugin` 相同。Go 无法在所有平台上于运行时加载代码，因此插件是编译进编辑器程序的 Go 代码：编辑器程序用 `Editor.RegisterPlugin` 注册插件，项目在其[项目文件](project.md)中列出插件名即可启用：

```yaml
editor:
  plugins: [health]
```

已注册但未被当前项目列出的插件保持关闭；`editor.plugins` 的顺序就是插件面板和工具出现的顺序。`OpenProject` 会在状态栏中列出项目要求、但本程序编译时没有包含的插件（`MissingPlugins`）。`go tool game editor` 不注册任何插件，也不注册任何组件类型，因此有自己插件的游戏要构建自己的编辑器命令。`go tool game generate editor` 会在游戏的 `main` 包中写出一个这样的命令，它带 `gameeditor` 标签构建、代替游戏本身，用游戏的 `Components` 和插件调用 `editorcmd.Main`，`go tool game editor` 会在项目上运行它（[命令行](cli.md#generate)）。`editorcmd` 包是两者共用的命令行：`New(Options)` 用给定的组件类型、插件、名称和库构建编辑器并设置 `ProjectAssets`，按这些组件类型以及注册表本身没有注册的内置物理组件（`body2d`、`collider2d`、`trigger2d` 及其 3D 形式）检查场景，`Open(ed, args)` 像 `go tool game editor` 一样打开项目、场景或剪辑，`Main` 和 `Run` 解析命令行，没有参数时打开工作目录中的项目，并显示窗口，窗口大小为 1120×680 点，除非 `Options.WindowWidth` 和 `Options.WindowHeight` 设置了其他大小。手写的编辑器命令也以同样的方式调用 `editorcmd.Main`：

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

`editor.Plugin` 有一个 `Name`，规则与 `game.yaml` 中其他名称相同（不含空白和控制字符），以及以下扩展列表：

- **面板**（`Panel`）：停靠在视口左侧（`DockLeft`，位于资源浏览器之后）、检查器右侧（`DockRight`）或视口下方（`DockBottom`）的视图，上方显示其 `Title`。`View(cx, ed, rev)` 在面板出现时构建一次，只要其插件处于启用状态，面板就保持挂载并保留其状态，不受其他面板增减的影响；与编辑器自带的面板一样，它在响应式函数中读取 `rev`，以跟随编辑器状态的每一次变化。没有面板的停靠区不占空间。
- **检查器**（`Inspector`）：检查器用 `View(cx, ed, rev, objectID)` 在组件标题下方绘制 `Component` 指定的组件，取代该组件的通用字段。`Editor.ComponentField` 读取一个字段；场景未设置该字段时返回其注册的默认值，对象没有该组件时返回 false。与通用字段一样，检查器的控件应在 `Playing` 返回 true 时禁用，编辑器拒绝的编辑应通过 `Editor.Report` 显示在状态栏中。两个已启用的插件绘制同一个组件时，`editor.plugins` 中排在前面的插件生效。
- **工具**（`PluginTool`）：在 **View > Tools** 与 **命令面板（`⌘K`）** 中列出的自定义工具，根据所实现的钩子在 2D 和/或 3D 视口中生效。当它是当前工具时（`SetPluginTool`，此时 `Tool` 为 `ToolPlugin`）：
  - **2D 钩子**：在 2D 视口上按下会以画布坐标调用 `Press(ed, x, y)`（用 `CanvasToWorld` 转换为世界坐标，用 `CanvasToScreen` 转换为屏幕空间图层的坐标），拖动会依次调用 `OnDragBegin(ed, x, y)`（返回 `true` 接管拖动）、`OnDragMove(ed, x, y)`、`OnDragEnd(ed, x, y)` 与 `OnDragCancel(ed)`（`BeginPluginDrag2D`、`UpdatePluginDrag2D`、`EndPluginDrag2D`、`CancelPluginDrag2D`、`PluginDragging2D`），指针移动会调用 `OnHover(ed, x, y, active)`（`SetPluginHover2D`、`ClearPluginHover2D`），设置了 `Draw` 时它会在编辑模式下绘制在 2D 场景之上。
  - **3D 钩子（#898）**：在 3D 视口上按下会以编辑器 3D 相机的世界空间拾取射线 `math3d.Ray`（`PickRay3D`）调用 `Press3D(ed, ray)`，拖动会依次调用 `OnDragBegin3D(ed, ray)`（返回 `true` 接管拖动而不旋转相机）、`OnDragMove3D(ed, ray)`、`OnDragEnd3D(ed, ray)` 与 `OnDragCancel3D(ed)`（`BeginPluginDrag3D`、`UpdatePluginDrag3D`、`EndPluginDrag3D`、`CancelPluginDrag3D`、`PluginDragging3D`），指针移动会调用 `OnHover3D(ed, ray, active)`（`SetPluginHover3D`、`ClearPluginHover3D`），设置了 `Draw3D(ed, d, proj)` 时它会通过 `Projection3D`（提供 `Camera`、`Width`、`Height`、`ViewProj`、`Project`、`ProjectSegment`、`ScreenRay`、`GizmoLength`、`DrawPoint`、`DrawLine`、`DrawBox`）在 3D 视口之上绘制。
  - 工具至少需要提供 `Press`、`OnDragBegin`、`Press3D` 或 `OnDragBegin3D` 之一。仅含 2D 钩子的工具在 3D 中保持空闲；含 3D 钩子的工具从命令面板或 **View > Tools** 触发时会自动切换到 3D 视图。选择其他工具或关闭插件都会结束该工具。
- **3D 组件操纵杆**（`Gizmos3D []ComponentGizmo3D`，#898）：按组件类型注册对象空间中的 3D 手柄（点 `GizmoHandlePoint3D`、线段 `GizmoHandleLine3D`、线框盒 `GizmoHandleBox3D`，可标记为 `DisplayOnly`），由 `Handles(ed, objectID)` 返回，并配以 `Drag(ed, drag GizmoDrag3D) error` 拖拽回调。编辑器自动沿完整父子层级将对象空间手柄变换到世界空间并执行屏幕空间拾取（`PickComponentGizmo3D`、`BeginComponentGizmoDrag3D`、`UpdateComponentGizmoDrag3D`、`EndComponentGizmoDrag3D`、`CancelComponentGizmoDrag3D`），并向 `Drag` 传入 `GizmoDrag3D`（含世界与对象空间射线以及 `AxisDelta`、`PlanePoint`、`PlaneDelta`、`ViewPlaneDelta` 辅助方法）。在 `Drag` 中通过 `Editor`（例如 `SetComponentField` 或 `Editor.History`）执行的修改会在每次指针移动时实时预览，按 `Escape` 或取消时干净回滚且不写入历史，松手结束时合并为**单个撤销步骤**提交到 `Editor.History`。两个已启用插件为同一组件注册 3D 操纵杆时，`editor.plugins` 中排在前面的插件生效。编辑器自带的 3D 变换操纵杆（平移、旋转、缩放）与内置 `collider3d` / `trigger3d` 手柄均复用这套共享 3D 手柄 API。

插件只通过编辑器的方法修改场景，例如 `SetComponentField`、`PlaceObject` 和 `MoveObject`，或者在 `Editor.History` 上执行自己的 `Command`，因此它的每一次编辑都与其他编辑一起撤销和重做。`History.Group(label, fn)` 把 `fn` 做的编辑记录为一个步骤，按一次撤销即可撤回；`fn` 失败时撤销这些编辑且不记录任何步骤。编辑器的 MCP 服务器（[mcp](mcp.md#编辑器工具)）就这样应用智能体的每次请求。`RegisterPlugin` 会拒绝没有名称或名称已注册的插件、缺少 ID、标题、标签、组件或函数的面板、检查器或工具、含控制字符的 ID 或组件名、未知的停靠位置，以及 ID 或组件重复的面板、检查器或工具。测试 ID 用 `-` 连接插件名和 ID（`plugin-panel-<plugin>-<id>`、`tool-<plugin>-<id>`、`inspector-plugin-<plugin>-<component>`），因此一起使用的插件，其名称和 ID 不应连接成相同的字符串。

`SetPluginEnabled(name, on)` 把已注册的插件加入 `editor.plugins` 或从中移除，作为项目的一次可撤销编辑，由 **File > Save**（`mod+s`）写入；移除最后一个插件时，如果 `editor` 小节中已没有其他内容，就会一并移除该小节（包括作者自己写的 `editor: {}`），撤销会将其恢复。`RegisteredPlugins`、`ActivePlugins` 和 `PluginEnabled` 报告程序包含哪些插件、哪些已启用。

### 随游戏发布带插件的编辑器

游戏把编辑器作为自己模块中、与游戏并列的又一个命令来发布，在项目目录中用 `go run ./cmd/editor` 运行。把插件放在只有编辑器命令导入的独立包中，这样发布的游戏不会链接编辑器；把组件类型放在游戏和编辑器都导入的包中，这样两者用同一组字段检查场景：

| 包 | 导入 | 内容 |
| --- | --- | --- |
| 组件包 | `sceneformat` | 组件类型（`ComponentType`）、供 `ParseWith` 和 `SetComponentRegistry` 使用的注册表，以及按默认值读取组件的辅助函数（`ComponentRegistry.Resolve`） |
| 插件包 | `editor` 和组件包 | 返回 `editor.Plugin` 的 `Plugin()` |
| 游戏（`main`） | 组件包 | 用 `sceneformat.ParseWith` 和注册表加载场景 |
| `cmd/editor` | `editor`、组件包和插件包 | 注册组件类型和插件，然后打开项目 |

`game.yaml` 在 `editor.plugins` 下列出插件，因此游戏的编辑器打开项目时插件总是启用的。不应在缺少插件时启动的编辑器命令，设置 `editorcmd.Options.RequirePlugins`，或在 `OpenProject` 之后检查 `MissingPlugins`，不为空就失败退出。项目有 `cmd/editor` main 包时，`go tool game editor` 会用 `go run ./cmd/editor` 运行该命令，就像运行 `game generate editor` 写出的命令一样，并把同样的命令行（`-library` 以及项目、场景文件、剪辑和对象 ID）传给它，所以 `cmd/editor` 应当用 `editorcmd.Main` 或 `editorcmd.Run` 解析命令行。没有导入 `editorcmd` 的 `cmd/editor` 不会被运行：`go tool game editor` 会说明这一点，并自己打开项目。

[Field Medic](https://github.com/nsgo-dev/game/tree/main/examples/fieldmedic)（`examples/fieldmedic`）就是按这种方式组织的可运行游戏。它的病人带有 `health` 组件（`health` 包），它的 `cmd/editor` 注册了 `health` 插件（`healthplugin` 包）：一个停靠在检查器右侧的 Ward 面板，列出每个病人的生命值，其中的 Heal All 按钮在 `Editor.History` 上执行一个可撤销步骤，一次治好所有病人；一个 `health` 组件的检查器，带生命值和上限两个字段以及剩余生命值的条形图；以及一个 Admit 工具，在视口按下处放置一个病人，并在工具启用时为每个病人画出轮廓。它的测试在编辑器中打开游戏自己的项目，操作面板、检查器和工具。编辑器自身的测试中也有一个更小的 `health` 插件（`pkg/editor/plugin_test.go`）。

## 库面板（#493）

当 `Editor.Library` 设为一个包库（`library.Open`）时（`go tool game editor -library <索引>` 就会这样设置），资源浏览器旁会出现一个面板，列出库中每个包的类型、版本、描述以及打开的项目已安装的版本。面板上的 Install、Update 和 Remove 按钮调用 `InstallPack` 和 `RemovePack`：把包的文件写到 `game.yaml` 旁边，在项目中记录包及其新增的自动加载条目和插件，然后保存项目；插件包列出的已注册插件会立即启用。正在运行、没有打开项目或项目有未保存修改（`ErrProjectDirty`）时它们会拒绝执行；由于写入的文件无法撤销，它们会清空撤销历史。从 https 库获取包时不会阻塞事件循环，包到达之前按钮显示为忙碌状态（`FetchingPack`）。库的格式以及安装会写入什么，见[资源与插件库](library.md)。

## 统一设置页面：智能体、项目、输入映射、音频总线、导出、通用偏好与快捷键（#907、#908、#909、#916、#1028、#1060、#1061）

所有每台电脑的智能体控制、项目级 `game.yaml` 配置与编辑器偏好均汇集在统一的**设置**视图（`SettingsView`，通过 `OpenSettingsPage(page)` 打开、`SettingsOpen()` / `SettingsPage()` 查询、`CloseSettings()` 或 `Escape` 关闭）中，界面布局严格遵循 `design/editor/Settings.dc.html` 与 `Spec.dc.html`，并在深色与浅色外观下统一使用 `theme.Palette` 令牌。左侧导航将 **`AGENT`**（**Models and connections**、**Tools**、**Usage and privacy**，全部保存在此计算机且为**仅限用户**）、**`PROJECT · game.yaml`**（**Project**、**Input map**、**Audio buses**、**Export**）与 **`EDITOR · THIS COMPUTER`**（**General**、**Shortcuts**）分组展示；每一行包含标签、说明文本、值控件与智能体权限徽章（**Agent may change** 或 **You only**）；全部页面上的交互控件均使用真实可聚焦的 `ui.Button` / `ui.*` 组件，支持 `Tab` 键逐项到达并显示焦点环。

### 智能体设置页：模型与连接、工具、用量与隐私（#1028、#1030）

全部保存在每台电脑的 `EditorSettings`（`editor.yaml` 的 `agent` 小节，从不写入项目目录），且所有修改方法都会拒绝来自智能体的调用（`ErrAgentSettingsYouOnly`）：

- **模型与连接（`SettingsPageModels`）**：展示已连接的本机 MCP 智能体（如 Claude Code）、内置 API 连接（`Anthropic API`，密钥通过 `StoreAgentAPIKey`、`HasAgentAPIKey`、`DeleteAgentAPIKey` 和 `AgentAPIKey` 仅保存在 NSGo 的 `core.Secrets`（`AgentAPIKeySecretAccount`）或系统钥匙串（`AgentKeychainServiceName`）中，绝不写入 `editor.yaml`、任何项目文件或日志）、打开 `ConnectAgentDialogView` 的 **+ 连接其他智能体或模型** 按钮，以及 **任务分工**（`AgentTaskRoute` / `SetAgentTaskRoute` / `ResolveAgentModelRoute`）。导航栏徽章实时显示已连接数量（`AgentActiveConnectionCount`，如 `2 个已连接`）。
- **内置智能体执行器（`SubmitAgentTaskPrompt`、`Editor.AgentTools`、`Editor.BuiltInAgentEndpoint`）**：根据 **任务分工** 将五类任务（`AgentTaskSceneEdit`、`AgentTaskPlaytest`、`AgentTaskProblems`、`AgentTaskAskInPlace`、`AgentTaskArtSound`）路由到已连接的本地 MCP 智能体或内置 Anthropic Messages API 执行器（`DefaultBuiltInModel` / `DefaultBuiltInFastModel`），仅允许 `https://`（或本地测试用的回环 `http://`）端点，且拒绝跨主机或 HTTPS 降级重定向以防泄露 `x-api-key`。`editormcp.New` 会自动绑定 `Editor.AgentTools`，使内置智能体调用的工具与外部 MCP 客户端一样经过完整的工具开关、权限规则、`WithAgentActor` 仅限用户保护以及解析符号链接的外发隐私校验。
- **每周限额排队与模型错误切换（`AgentStatusLimitReached`、`AgentQueuedRequests`、`AgentModelErrorState`）**：达到仅限用户可调整的每周请求上限（`WeeklyRequestLimit` / `SetWeeklyRequestLimit`）后，智能体面板顶栏切换为 `Limit reached · resets 00:00` 与 **Usage** 链接，新提交的请求会进入队列等待（`SendQueuedAgentRequest`、`RemoveQueuedAgentRequest`），直至 `CheckAgentUsageReset` 跨过每周重置时刻或用户调高限额。遇到瞬时模型错误（`429`、`5xx`、`529` 过载）时，按 `AgentModelRetryDelays`（`4 s、8 s、16 s、32 s`）指数退避重试，并展示包含 **Retry now**（`RetryAgentTurn`）与 **Use `<已连接智能体>`**（`SwitchAgentTurnToAlternative`）的错误卡片，一键将该类任务切换到另一已连接智能体继续执行且不重复扣减每周请求数。
- **工具（`SettingsPageTools`）**：列出编辑器 MCP 服务器注册的全部工具（`RegisteredMCPTools`），每项显示等宽工具名、说明与带 `role="switch"` 的开关（`IsMCPToolEnabled` / `SetMCPToolEnabled`），导航栏显示 `"已开启 / 总数"`（`EnabledMCPToolCount`）。`game_delete_file` 默认关闭；关闭任一工具会立即将其从已连接客户端的工具列表中移除（发送 `tools/list_changed`），若智能体仍调用该工具则会收到可读的拒绝原因。下方 **智能体可使用的其他 MCP 服务器** 默认包含 **Running game · nsgo dev**（`nsgo-game` / `go tool nsgo mcp`）并提供 **+ 添加服务器**（`AddAgentMCPServer`、`SetAgentMCPServerEnabled`、`RemoveAgentMCPServer`），已启用的服务器会自动同步到项目的 `.mcp.json` 与 `.codex/config.toml`（`editormcp.InstallWithServers`）。
- **用量与隐私（`SettingsPageUsage`）**：展示 **本周请求** 与用户设定的每周限额进度条（`WeeklyRequestLimit` / `SetWeeklyRequestLimit` / `RecordAgentRequest`）、审查后 **保留率**（`RecordAgentReviewOutcome`）以及本地 **试玩次数**（`RecordPlaytestRun`），下方提供四项 **允许离开此计算机的内容** 开关（`AgentPrivacy` / `SetAgentPrivacy`）：**场景与数据文件**（开）、**视口画面**（开）、**Go 源代码**（关）与 **美术与音频文件**（关）。只要某类内容处于关闭状态，编辑器的 MCP 服务器就会对所有智能体拒绝返回此类内容的工具调用或资源读取，并在错误中说明原因。

### 项目设置页（#907、#1060）

打开项目后，通过 **Project > Project Settings**、`mod+shift+,` 或命令面板（`CmdProjectSettings`、`ProjectSettingsOpen` / `SetProjectSettingsOpen` 或 `OpenSettingsPage(SettingsPageProject)`）会在 `SettingsView` 中打开 **Project** 页面（`ProjectSettingsPanelView`），完全通过 `EditProject` 编辑 `game.yaml`：每一次修改都经过 `project.Validate` 校验，纳入撤销/重做历史，并在点击 **Save**（`SaveProject`）时同时写回 `game.yaml` 与同步更新的 `nsgo.yaml`：

- **常规（General）**：在概览页直接编辑 `name`、`id`、`window` 宽高、`autoload.mainScene` 与 `appVersion`（`SetProjectName`、`SetProjectID`、`SetProjectWindowSize`、`SetProjectMainScene`、`SetProjectAppVersion`）。
- **窗口（Window）**：编辑 `window.title`、`width`/`height`、`minWidth`/`minHeight`/`maxWidth`/`maxHeight` 尺寸限制、`resizable` 和 `appearance`（`SetProjectWindowTitle`、`SetProjectWindowSize`、`SetProjectWindowLimits`、`SetProjectWindowResizable`、`SetProjectWindowAppearance`）。
- **显示（Display）**：开关可选的 `display` 小节（`SetProjectDisplayEnabled`），并编辑虚拟分辨率 `width`/`height`、`mode`（`fit`、`fill`、`stretch`、`expand`）、`integerScale`、`pixelSnap` 与 `barColor`（`SetProjectDisplaySize`、`SetProjectDisplayMode`、`SetProjectDisplayIntegerScale`、`SetProjectDisplayPixelSnap`、`SetProjectDisplayBarColor`）。
- **自动加载（Autoload）**：选择 `autoload.mainScene`（`SetProjectMainScene`），并增改删 `autoload.scenes` 与 `autoload.resources` 条目（`AddProjectAutoloadScene`、`UpdateProjectAutoloadScene`、`RemoveProjectAutoloadScene`、`AddProjectAutoloadResource`、`UpdateProjectAutoloadResource`、`RemoveProjectAutoloadResource`），文件路径可从当前项目文件列表中搜索选择（`ProjectSceneFiles`、`ProjectResourceFiles`）。
- **物理（Physics）**：编辑 `physics.gravity2D`、`physics.gravity3D`、`physics.solverIterations2D` / `solverIterations3D` 以及具名碰撞层 `physics.layers`（`SetProjectPhysicsGravity2D`、`SetProjectPhysicsGravity3D`、`SetProjectPhysicsSolverIterations`、`AddProjectPhysicsLayer`、`RemoveProjectPhysicsLayer`）。
- **编辑器插件（Plugins）**：为每个已注册插件（`RegisteredPlugins`）提供调用 `SetPluginEnabled` 的开关，同时列出 `game.yaml` 中声明但当前编辑器程序未内置的插件（`MissingPlugins`）并支持移除。

任何被 `project.Validate` 拒绝的值都会在对应设置行内显示校验错误，且不会改动项目或撤销历史。在打开项目首次保存之前（`ProjectCanonicalSavePending`），页面顶部会显示明确提示，说明保存将以规范 YAML 形式写出 `game.yaml`，不会保留注释。

### 音频总线页（#909、#1060）

打开项目后，通过 **Project > Audio Bus Mixer**、命令面板（`CmdAudioMixer`）或 **设置 › Audio buses**（`AudioMixerOpen` / `SetAudioMixerOpen` 或 `OpenSettingsPage(SettingsPageAudio)`）会在 `SettingsView` 中打开 **Audio buses** 设置页（`AudioMixerPanelView`），通过 `EditProject` 直接编辑 `game.yaml` 的 `audio` 小节：每一次修改都经过 `project.Validate` 校验，纳入编辑器的撤销/重做历史，并在点击 **Save**（`SaveProject`）时写回文件：

- **主音量与总线**：调节带分贝读数的主音量滑块（`SetAudioMasterVolume`），新增总线（`AddAudioBus`）、重命名（`RenameAudioBus`）、删除（`DeleteAudioBus`）、调整顺序（`MoveAudioBus`），查看并调节每条总线的路由（`Into Master`）与音量滑块（`SetAudioBusVolume`）。在未设置主音量时删除最后一条总线会一并移除空的 `audio` 小节，撤销可将其恢复。
- **单总线 DSP 效果链**：为每条总线添加（`AddAudioBusEffect` / `AddAudioBusEffectKind`）、移除（`RemoveAudioBusEffect`）、重排（`MoveAudioBusEffect`）和编辑（`SetAudioBusEffect`）`project.Effect` 支持的全部六种效果（`lowPass`、`highPass`、`delay`、`reverb`、`gain`、`bitcrush`）。数值控件将参数约束在 `project.Validate` 允许的范围内（配合 `ClampAudioEffect`），Go API 则会拒绝越界值且不改动项目或历史。
- **实时试听**：从当前项目中选择音频文件（`ProjectAudioFiles`、`SelectAudioFile`），点击 **Audition**（`AuditionAudioBus`、`StopAudition`、`SetAuditionLoop`）即可经由 `project.File.ConfigureMixer` 配置好的编辑器 `sound.Mixer` 通过选中总线播放。调节主音量或总线音量会立即作用于正在播放的音频而不中断播放；修改效果链或撤销/重做会立即通过与运行时 `Music.Play`、`Mixer.LoadEffect` 相同的 `sound.Mixer` 总线效果链重新处理。该效果链只处理内存中的 16-bit PCM WAV 来源：发布的游戏对其他格式不加效果播放（并按分组输出一条日志），而在配置了效果的总线上试听这类文件时会直接报告 WAV 解码错误。

### 输入映射页（#908、#1060）

打开项目后，通过命令面板（`CmdInputMap`）或 **设置 › Input map**（`InputMapOpen` / `SetInputMapOpen` 或 `OpenSettingsPage(SettingsPageInput)`）会在 `SettingsView` 中打开 **Input map** 设置页（`InputMapPanelView`），通过 `EditProject` 直接编辑 `game.yaml` 的 `input` 小节，提供完整的撤销/重做与 `project.Validate` 校验：

- **动作概览与编辑**：列出每个已配置动作及其键盘、手柄与触控触发方式，并提供按钮直接跳转到该动作的编辑视图。支持新增（`AddInputAction`）、重命名（`RenameInputAction`）和删除（`DeleteInputAction`）动作，以及添加（`AddInputBinding`）、编辑（`SetInputBinding`）或移除（`RemoveInputBinding`）`game/input` 支持的全部绑定类型：`key`、`keyCode`、`gamepadButton`、`mouseButton`、`touchRegion` 与 `mouseRegion`。重命名动作时会在同一步可撤销操作中原子更新 `input.axes` 中所有引用该动作的轴，撤销与重做也会保持一致。
- **按键捕获**：点击 **Press a key**（`BeginKeyCapture`，`project.KindKey`）或 **Press code**（`project.KindKeyCode`）进入捕获模式，将下一次硬件按键（`CaptureKeyEvent`）直接绑定到当前动作，点击 **Cancel**（`CancelKeyCapture`）可退出而不改动项目。
- **虚拟显示尺寸上的区域绘制与拖拽**：区域画布按项目的 `display` 虚拟分辨率等比缩放（未配置 `display` 时回退到 `window` 尺寸，`InputMapDisplaySize`），绘制全部 `touchRegion` 和 `mouseRegion` 区域；可拖拽已有区域移动、拖拽选中区域右下角手柄调整大小，或在空白处拖拽绘制新区域（`BeginRegionDrag` / `UpdateRegionDrag` / `EndRegionDrag` / `SetInputRegion`），每次拖拽作为单个可撤销的 `EditProject` 步骤提交。
- **动作轴、运动轴与手柄摇杆轴**：**Axes** 标签页可编辑动作轴（`AddInputAxis`、`SetInputAxis`、`DeleteInputAxis`）、设备运动传感器轴（`AddInputMotionAxis`、`SetInputMotionAxis`、`DeleteInputMotionAxis`）和手柄模拟摇杆轴（`AddInputGamepadAxis`、`SetInputGamepadAxis`、`DeleteInputGamepadAxis`）。`BuildInputActions` 通过 `project.File.BindInput` 将当前项目的输入映射绑定到实时的 `*input.Actions`。

## 本地化面板（#917）

本地化面板（`LocalizationPanelView`）在视口下方以表格形式并排编辑各语言的 YAML 字符串表：行为消息键，列为项目语言。

- **打开**：选择 **Project > Localization Strings** 或命令面板中的 **Localization Strings**（`CmdLocalization`，打开项目时加载 `<项目>/locales`，否则创建含 `en` 语言的内存表），或在 Go 中调用 `OpenLocalizations(dir)` / `NewLocalizations(dir, locales...)`。`CloseLocalizations` 关闭面板。
- **键与语言**：支持增删语言列（`AddLocalizationLocale`、`RemoveLocalizationLocale`）和消息键行（`AddLocalizationKey`、`RemoveLocalizationKey`），并自动将语言标签规范化为 BCP-47 形式（例如 `zh_hans` -> `zh-Hans`）。
- **普通文本与 CLDR 复数**：可将键设为复数模式（`SetLocalizationKeyPlural`），编辑全部六个 CLDR 复数类别（`zero`、`one`、`two`、`few`、`many`、`other`，对应 `SetLocalizationPlural` / `SetLocalizationPluralForm`），或用 `SetLocalizationString` 编辑普通字符串。
- **缺失翻译高亮**：译文为空（或复数键缺少 `other` 回退形式）的单元格会以警告边框和 `Missing` 标记高亮；可通过 `LocalizationMissing`、`LocalizationMissingCount` 和 `LocalizationIsMissing` 查询。未翻译的键在保存时会被省略，以便运行时 `l10n.Catalog` 沿 BCP-47 链自动回退。
- **撤销、保存与面板状态**：所有本地化编辑都接入 `Editor.History`（撤销/重做）。点击 **Save Strings**（`SaveLocalizations` / `SaveLocalizationsTo`）通过 `l10n.EncodeYAML` 将每种语言以确定性规范 YAML 写入 `<dir>/<locale>.yaml`，并清理已从工作区移除的语言文件。面板错误会以可关闭横幅展示（`LocalizationError` / `ClearLocalizationError`），异步加载流程可通过 `LocalizationLoading` / `SetLocalizationLoading` 切换加载状态卡片。

## 问题面板与智能体修复建议（#913、#1036）

打开项目后，视口下方的问题面板（`ProblemsView`）会在 UI 协程之外对项目目录叠加所有已打开且未保存的编辑器文档运行 `assetcheck.CheckWith`（使用编辑器的组件注册表）和 `project.Check`，并在状态栏实时显示问题总数（`Problems: N`）。无论是用户还是智能体在编辑器中完成编辑，都会自动防抖重检（`ProblemCheckDebounce`，默认 80 毫秒），无需先保存即可立即发现问题；打开项目（`OpenProject`）、保存文件（`SaveFile`、`SaveProject`、`SaveClipFile`）、移动文件（`MoveFile`，包括撤销与重做）以及更换组件注册表（`SetComponentRegistry`）后也会自动重检。在编辑器之外修改了文件后，可点击面板上的 Check 按钮（`CheckProblems`）手动触发检查。

每行显示一条问题的 `file:line:column` 位置（`assetcheck.Problem.Location`，场景或 YAML 格式能定位时附带从 1 开始的列号）、Spec 告警图标和错误信息，与在同一项目上运行 `go tool game check` 报告的问题一致，并补充 `project.Check` 发现的项目级问题（例如 `nsgo.yaml` 与 `game.yaml` 不一致、已安装包的文件缺失，或非 `.scene.yaml` 扩展名的自动加载场景无效）。点击某一行（`OpenProblem`）时，会在新标签页中打开对应场景而不会丢失其他标签页的未保存修改，或在文件浏览器中选中非场景文件（`SelectedFile`、`SelectFile`）。归属于当前打开场景对象的活动问题还会直接通过 Spec Problem 叠加层绘制在 2D 视口的活动对象上（2 px 实线问题外框加带告警图标和消息的实心胶囊，可通过 `SetProblemOverlayVisible` 切换），并在场景树对应行显示问题计数徽章（`ObjectProblems`、`ObjectProblemCount`）。

- **智能体修复建议（`ProposeProblemFix`、`ApplyProposedFix`、`ProblemAgentPanelView`）**：通过 MCP（`game_check`）看到实时问题的智能体，可以调用 `problem_propose_fix` 提交问题、说明与经校验的结构化编辑。提议修复时仅在克隆的场景文档上模拟校验并计算精确的 `-`/`+` YAML 差异，随后在右侧打开 **Agent** 面板展示问题卡片（`file:line:col`）、说明、差异、**Apply fix**、**Show me**、**Ignore** 与 **"Ask about this problem…"**，**绝不自动应用修改或改写任何文件**。只有用户点击 **Apply fix**（`ApplyProposedFix`）时，编辑器才会把修复作为单个可撤销的 `History` 步骤应用，并弹出带 **Undo** 按钮的 Spec Toast；执行一次撤销即可恢复原有 YAML 并让问题重新出现。
- **定位查看、忽略与追问（`ShowProblem`、`IgnoreProblem`、`AskAboutProblem`）**：**Show me** 会打开问题所在场景、选中并框选对应对象，同时高亮精确的 YAML 代码行。**Ignore** 会从问题面板、状态栏、场景树徽章和视口叠加层中隐藏该问题，直到对应文件再次被修改（基于实时内容哈希追踪）；一旦对该文件进行新编辑且问题仍成立，该问题会自动重新显现。提交 **"Ask about this problem…"** 会将问题、归属对象及已提议的修复一并附带交给回调（`OnAskAboutProblem`）。

在 Go 代码中，可安全使用 `Problems`、`ProblemCount`、`ProblemErr`、`CheckingProblems`、`WaitForProblems`、`IgnoreProblem`、`IsProblemIgnored`、`ProposeProblemFix`、`ProposedFixes`、`ApplyProposedFix`、`ShowProblem` 与 `AskAboutProblem` 管理实时问题与修复建议。

## 导出与发布构建（#916、#1060）

打开项目后，通过 **File > Export & Release...**、`mod+shift+e`、命令面板（`CmdExport`）或 **设置 › Export**（`ExportPanelOpen` / `SetExportPanelOpen` 或 `OpenSettingsPage(SettingsPageExport)`）会在 `SettingsView` 中打开 **Export** 设置页（`ExportPanelView`），用于在编辑器中直接编辑 `game.yaml` 的 `export` 小节并执行发布构建：

- **预设、就绪状态与输出目录**：带行内校验地编辑 `export.output`（`SetExportOutput`），添加、删除和重排各平台预设（`AddExportPreset`、`RemoveExportPreset`、`MoveExportPreset`），并仅从各平台合法目标 `project.TargetsOf(platform)` 中选择构建目标（`SetExportPresetTargets`、`ToggleExportPresetTarget`）。每个预设行都会显示就绪状态（`ExportPresetReadiness`：**Ready**，或在缺少签名/团队配置或资源鸣谢时显示 **Set up**）与权限徽章，左侧导航的 **Export** 条目在存在待配置预设时也会显示警告徽章（`ExportNavWarningMeta`）。所有修改都通过 `EditProject` 完成（经 `project.Validate` 校验并支持撤销/重做），在 `export` 为空时自动清理该小节，点击 **Save**（`SaveProject`）时写出规范 YAML 并同步 `nsgo.yaml`，与 `game project targets` 保持一致。
- **鸣谢（Credits）预检**：面板打开、刷新（`CheckExportCredits`）或开始构建前，都会执行与 `game release` 相同的 `tooling/credits` 扫描。任何缺少鸣谢信息的资源都会连同其对应的 `*.import.yaml` 边车路径一并列出，并在补全前阻止发布构建；补全鸣谢后刷新即可清除告警，成功构建时会在输出目录写入 `CREDITS.md`。
- **构建、演练与输出流**：支持构建全部预设或勾选指定目标（`ExportSelectedTargets`），并可配置分发渠道（`SetExportChannel`）、发布说明文件（`SetExportReleaseNotes`）以及 **Dry Run** 开关（`SetExportDryRun`）。`StartExportBuild` / `RunExportBuild` 与 `go tool game release` 共用同一套发布命令规划器，因此演练构建会在 UI 协程之外将完全一致的 `nsgo build` 命令行流式输出到面板的 Output 区域，非演练构建成功后自动打开解析出的输出目录（`OpenOutputFolder` / `OpenExportOutputFolder`）。

### 构建与发布界面（#1048、#1050）

如需完整的发布前准备与对比，可通过 **Project > 构建与发布…**（`CmdBuildRelease`，快捷键 `mod+shift+r`，或调用 `OpenBuildReleaseScreen` / `CloseBuildReleaseScreen` / `BuildReleaseScreenOpen`）打开采用 Spec 设计令牌渲染的全窗口 `BuildReleaseScreenView`：

- **顶部栏与构建前检查**：显示 `"Release · Version <版本> from <提交> on <分支> · last release <上一版本>"`，提供可编辑版本号（`SetReleaseVersion`）、Dry Run 开关（`SetReleaseDryRun`）以及 **Build** / **Stop** / **Resume** 控制（`StartReleaseBuild`、`StopReleaseBuild`、`ResumeReleaseBuild`）。**BEFORE EVERY BUILD** 汇总 `nsgo check`（问题数与耗时，`RefreshBuildPreflight`）、冒烟试玩摘要以及 `game.yaml` 中 `export.budgets` 声明的预算阈值（`qualityProfile`、`downloadSizeMB`、`frameTimeP95Ms`，默认 Low 档 / 80 MB / 16.7 ms）。
- **导出预设、有序步骤与日志流**：列出每个平台的勾选复选框（`SetBuildPresetIncluded`）、平台摘要、实时状态（`Queued`、`Building`、`Notarizing`、`Done`、`Failed`、`Set up`）、产物大小及相对上一版本的增量（`SetLastReleaseBaseline`）。选中运行中的预设会展示 6 个按序执行的步骤（`Check project`、`Compile Go`、`Pack assets`、`Sign`、`Smoke playtest`、`Measure`）、与 `game release -dry-run` 一致的 `releaseplan` 命令以及持续跟随的日志（`build/<platform>.log`）。多个预设逐一串行构建；**Stop** 会保留已完成步骤，**Resume** 从中断步骤继续执行。
- **失败隔离与代理预设修复**：当某个预设失败（例如在 `Sign` 步骤缺少签名密钥）时，界面会展示错误标题、失败步骤与耗时、失败命令以及 `"Other platforms keep building. Nothing was uploaded."`，同时继续完成其余已勾选预设的构建。代理提出的预设修复差异（`SetBuildPresetFixProposal`）支持一键应用并重新构建（`ApplyBuildPresetFixAndRebuild`）。
- **构建结果与平台就绪检查**：构建完成后展示产物路径、体积与相对上一版本的增减量、`code` / `textures` / `audio` / `other` 分类占比与增长原因、下载体积与帧耗时 p95 的预算达标/超标徽章、冒烟试玩摘要、**Show in Finder**（`ShowBuildArtifactInFinder`）、**Edit preset** 以及 Web 构建的本地测试链接二维码卡片。尚未具备构建条件的平台（例如 iOS）会展示 **READINESS** 清单（`EvaluateBuildReadiness`，基于与 CLI 共享的 `nsgo.yaml` 检查：`id`、`icon.apple` / `icon.png` 或默认 `assets/AppIcon.icon` / `assets/Icon.png`、`sign.identity`、`sign.profile`、与 NSGo 内置隐私清单合并的 `apple.privacy` 与 `resources/PrivacyInfo.xcprivacy`，以及 `*.touch.yaml` 布局或 `game.yaml` 的 `touchRegion` 绑定），明确区分代理可直接代办的事项（`RunBuildReadinessAction` / `RunBuildReadinessAgentSetup`，如同步 `nsgo.yaml`、生成 `resources/PrivacyInfo.xcprivacy`，或根据 `game.yaml` 现有动作生成 `controls.touch.yaml`）与需要开发者本人完成的步骤（描述文件与 Apple 签名凭据）。
- **基于提交与已保留审查自动起草发布说明（#1050）**：**RELEASE NOTES** 会读取自上一发布标签（无标签时自初始提交）以来的 git 提交记录、已保留的审查摘要（`SetReleaseKeptReviews` / `AddReleaseKeptReview`）以及 `changelog.d/*.md` 片段，生成面向玩家的 Markdown 发布说明（`DraftReleaseNotes`、`RedraftReleaseNotes`、`DefaultReleaseNotesDraft`，支持通过 `Editor.ReleaseNotesDrafter` 接入智能体起草，并在 `editormcp` 中提供 `release_notes_draft` 工具），并显示 `"Drafted from N commits since <标签/引用>"` 徽章。在界面中就地编辑发布说明（`SetReleaseNotesText` / `SaveReleaseNotes`）会直接原子写入 `game release -release-notes` 读取的同一个 Markdown 文件（`ReleaseNotesFile` / `SetReleaseNotesFile`，默认为 `release-notes.md`），且仅修改该文件本身。
- **始终先询问的商店上传与系统钥匙串凭据（#1050、#1762）**：点击 **Review uploads…**（`ReviewReleaseUploads` / `OpenReviewUploads`，或在 `editormcp` 中调用 `release_review_uploads`）会打开 **Review uploads** 确认对话框，在发送任何内容之前列出每个产物的名称、路径、大小、目标商店（`itch.io`、`Google Play · internal testing`、`TestFlight`）、精确渠道/轨道、版本号以及系统钥匙串凭据状态。itch.io 目标的渠道标签直接取自 `nsgo.yaml` 的 `itch.channels` 或 NSGo 的按目标默认值（`html5`、`osx-<arch>`、`windows-<arch>`、`linux-<arch>`）；使用 `steam` 渠道构建的桌面产物不会归入 itch.io，而在 Steam 目标接入前标注为 `"upload with SteamPipe"` 且不可勾选自动上传。严格信任边界确保无论权限配置如何（哪怕处于 `PermissionProfileAutonomous` 全权模式），未经开发者本人在 **Review uploads** 中显式确认（单次会话 `ConfirmReleaseUploads`；直接调用 `StartReleaseUpload` 或代理上下文 `WithAgentActor` / `editormcp` 尝试确认均会被拒绝），绝不会启动任何上传。上传凭据仅以 `PublishKeychainService` 为服务名，通过 `Editor.Keychain`（`github.com/nsgo-dev/game/pkg/editor/keychain` 中的 `keychain.Store`）保存（`StorePublishCredential`、`HasPublishCredential`、`DeletePublishCredential`——分别对接 macOS Keychain、Linux Secret Service、Windows Credential Manager 以及测试用 `keychain.NewMemory`），以脱敏的 `PublishCredential` 交给上传器，绝不写入项目文件或序列化到快照中。默认情况下（`Editor.ReleaseUploader` 为 nil 时），确认后的上传会在项目目录中运行 `go tool nsgo publish`（itch.io 调用 `publish itch -target …` 并仅通过环境变量传入 `BUTLER_API_KEY`，Google Play 调用 `publish -target android -track internal` 并传入 `NSGO_PLAY_SERVICE_ACCOUNT`，TestFlight 调用 `publish ios <file.ipa>` 并传入 `NSGO_APP_STORE_KEY` 以及从 JSON `{key, key_id, issuer}` 或 `key_id:issuer` 账号信息解析的 `NSGO_APP_STORE_KEY_ID` / `NSGO_APP_STORE_ISSUER`），凭据绝不出现在命令行参数或文件中，同时流式回报输出进度、对日志与报错中的密钥脱敏，并返回打印的商店链接或处理状态。

### 通过局域网二维码在手机或已连接设备上试玩构建（#1049、#1506）

在构建与发布界面完成 Web 或 Android 构建后，开发者可以直接在本机浏览器、同一 Wi-Fi 下的手机或 USB 连接的 Android 设备上试玩：

- **仅限局域网的 HTTP 服务与纯 Go 二维码**：非演练 Web 构建完成后会自动启动仅限局域网的 HTTP 服务（`StartWebBuildServer` / `StopWebBuildServer` / `WebBuildServerState`），严格绑定在 RFC 1918 私有局域网 IPv4 地址（离线时回退到 `127.0.0.1`，可通过 `Editor.LANAddressResolver` 与 `Editor.WebBuildServePort` 自定义）。公网绑定地址会被拒绝（`ErrNonLANAddress`），非局域网客户端请求返回 `403 Forbidden`，越界符号链接会被拦截；点击 **Stop serving**、关闭编辑器（`Close()`）、切换项目或卸载 `EditorView` 时会立即关闭监听并释放端口。Web 结果卡片使用纯 Go 确定性 ISO/IEC 18004 二维码编码器（`pkg/editor/internal/qrcode`、`EncodeBuildQR`）与 Spec 令牌在高对比度浅色静区卡片上渲染可扫描二维码，并提供局域网地址、**Copy URL**（`CopyWebBuildURL`）以及 **Stop serving** / **Start serving** 按钮。
- **局域网 Play 模式 MCP 桥接**：托管的 Web 构建保持 `index.html` 与资源文件字节完全不变，同时在响应头暴露 `X-NSGo-MCP-Bridge: /__nsgo_mcp/info` 与 `X-NSGo-Play-Mode: debug`。在 `js/wasm` Web 构建中，挂载 `game/mcp`（`mcp.Attach`）或运行 `game.Loop.Run` 会注册由运行中游戏 MCP 工具（`game_state_dump`、`game_scenes_list`、`game_entities_list`、`game_entity_inspect`、`game_loop_*`、`game_input_*`、`game_console_*`、`game_screenshot` 等）驱动的 `window.__nsgoMCP.callTool`（及 `listTools`），并对当前页面发起一次 `HEAD` 探测：检测到 `X-NSGo-Play-Mode: debug` 后自动拉取 `/__nsgo_mcp/info` 并加载 `/__nsgo_mcp/bridge.js`，向 `POST /__nsgo_mcp/session` 握手注册，通过 `/__nsgo_mcp/poll` 与 `/__nsgo_mcp/reply` 长轮询收发调用（并在 `pagehide` 时主动断开）。在编辑器 MCP 服务器（`pkg/editor/editormcp`）侧，代理可通过 `phone_sessions_list` 列出已连接的手机会话，并通过 `phone_call_tool` 经由 `Editor.CallWebBridgeTool` 向手机转发工具调用（遵循 `permissions.yaml` 中 `playtests: phone/<tool>` 运行规则与隐私策略，并将 `game_screenshot` 的 PNG 直接解码为 MCP 图像返回），底层对应 `Editor.WebBridgeSessions`、`Editor.CallWebBridgeTool` 与 `Editor.WebBridgeHandler`。
- **RUN ON DEVICE 设备目标**：在构建结果右侧展示实时设备列表与状态（`BuildDeviceState`）：
  - **本机浏览器**（`LaunchBuildOnDevice("browser")`，可注入 `Editor.OpenBrowserURL`）：在本地浏览器打开局域网测试链接（`ready` -> `running`）。
  - **局域网二维码手机**（`LaunchBuildOnDevice("phone-wifi")`）：实时展示 `serving`、`connected`（接入 MCP 桥接后显示已连接的手机名称）与 `stopped` 状态。
  - **USB 连接的 Android 设备**（`RefreshConnectedDevices`、`RunBuildOnAndroidDevice`、`WaitAndroidDeviceRun`、`StopAndroidDeviceRun`，可注入 `Editor.DeviceDiscoverer` 与 `Editor.DeviceCommandRunner`）：在 Android 构建完成后通过 `go -C <dir> tool nsgo build -target <target> -run -device <serial>` 安装并启动，清晰汇报 `ready`、`installing`、`running`、`failed` 与 `disconnected` 状态。
## 项目搜索与查找引用（#914）

打开项目后，文件浏览器的 **Search** 按钮（`SearchOpen` / `SetSearchOpen`）会在视口下方切换显示搜索与引用面板（`SearchPanelView`），用于在项目内快速跳转或回答“哪些文件引用了当前文件”：

- **全项目搜索（`Search`、`SetSearchQuery`、`SearchResults`）**：索引项目中的所有文件（遵循与 `ProjectFiles` 和 `refs.Scan` 相同的目录遍历规则）并解析所有场景文件（`*.scene.yaml`、`*.scene.yml`、自动加载场景以及被其他文件引用的场景）。不区分大小写的查询（支持空格分隔的多词组合查询）可匹配文件名与路径（`SearchFile`）、文件头或 `game.index.yaml` 中的稳定 UID（`SearchUID`）、场景名称，以及场景内的对象 ID 与显示名称（`SearchObject`，含嵌套子对象）、组件类型（`SearchComponent`，含覆盖项中的组件）、预制件引用（`SearchPrefab`）和实例化/继承的场景路径（`SearchSceneRef`）。
- **跳转到结果（`OpenSearchResult`）**：激活非场景文件的结果会在文件浏览器中选中它（`SelectedFile`）。激活场景文件或场景对象的结果会打开该场景（同步 `ScenePath` 与 `SelectedFile`）并选中匹配对象（`SelectedID`），嵌套子对象也直接选中；该子对象无法选中时回退选中其顶层祖先。如果该场景已经打开，则直接就地选中对象，不会从磁盘重新加载，也不会丢失未保存编辑与撤销历史。
- **查找引用（`FindReferences`、`ShowReferences`、`ShowSelectedFileReferences`、`References`、`OpenReference`）**：在文件浏览器中选中任意文件或目录并点击 **Find Refs**，即可基于 `refs.Scan` 列出所有入向引用，按 `refs.Scan` 报告的顺序逐行展示引用文件、从 1 开始的行号、字段名和原始值。点击某条引用会打开引用它的场景（若该行归属于某个顶层对象则自动选中该对象），或在文件浏览器中选中非场景引用文件。
- **实时索引刷新（`RefreshSearch`）**：对当前已打开场景的内存编辑（`PlaceObject`、`SetObjectName`、`DeleteObject`、撤销/重做）会立即更新活动搜索结果；保存场景、项目、动画片段、数据表或本地化文件、安装/移除包，以及移动文件（`MoveFile` 及其撤销/重做，同时自动跟踪 `ReferencesTarget`）都会自动失效并刷新索引；编辑器外部的磁盘改动既可通过轻量目录指纹自动感知，也可点击 **Refresh**（`RefreshSearch`）强制重扫。异步调用方也可通过 `SearchLoading` / `SetSearchLoading` 切换面板加载状态卡片。
## 运行游戏与输出面板（#910）

在保留原有编辑器内 **Play** 预览（`Play`、`PlayIn`、`Stop`，通过 `mod+p` / **F5** 切换）的同时，编辑器可以在桌面端直接通过 `go tool game dev` 运行打开的项目，并将输出实时显示在视口下方停靠的输出面板（`OutputPanelView`）中：

- **运行与停止**：选择 **Project > Run Game**、`mod+r` 或命令面板（`CmdRunGame`）即可在打开的项目目录中启动 `go tool game dev`（`RunGame` / `RunGameWith`）。开启 **Run From Open Scene**（`CmdRunOpenScene`、`SetRunStartAtOpenScene`）或传入 `RunGameOptions{StartAtOpenScene: true}` 时，会将当前打开的场景作为 `-scene <路径>` 与 `NSGO_GAME_SCENE`（`hotreload.SceneEnv`）传给游戏。游戏运行期间，**Stop Game**（`CmdStopGame`、**Shift+F5** / `StopGame`）会干净地结束子进程及其启动的全部进程树（Unix 上通过独立进程组结束，Windows 上通过 `taskkill /T /F` 结束）。卸载 `EditorView`、切换项目（`OpenProject`）或调用 `Close()` 也会自动停止正在运行的游戏。自定义工具和测试可注入 `Editor.RunGameCommand`。
- **输出 / 日志面板**：停靠在视口下方（`OutputPanelView`，可通过状态栏 **Output** 按钮、**View > Panels > Output**、命令面板 `CmdOutputPanel` 或 `SetOutputPanelOpen` 开关），在 UI 协程之外实时流式显示运行中游戏的标准输出（`OutputStdout`）和标准错误（`OutputStderr`），并保留编辑器自身带时间戳的状态与错误历史（`OutputEditor`）。连续多条编辑器错误都会保留在日志中，不会被下一条覆盖。
- **筛选、清空与 `file:line` 跳转**：切换 **Errors**（`SetOutputErrorsOnly`）只看错误日志，点击 **Clear**（`ClearOutput`）清空日志，点击包含 `file:line` 的日志行（`OpenOutputEntry`）时，若该文件是场景（`*.scene.yaml`、`*.scene.yml` 或 `autoload.scenes` 中列出的场景）则在编辑器中打开，否则在文件浏览器中选中该文件。
## 快捷键、菜单栏与命令面板（#920）

所有常用编辑器操作统一注册在同一个命令注册表中（`Editor.Commands`、`Editor.RegisterCommand`、`Editor.ExecuteCommand`），由单一数据源同时驱动键盘快捷键、桌面应用菜单栏和命令面板：

- **跨平台快捷键与文本焦点保护**：快捷键使用 NSGo 的 `"mod"` 修饰键（macOS 为 Command，Windows/Linux 为 Ctrl，可通过 `FormatShortcut` 格式化显示），并全部使用 `core.KeyNotEditing` 和 `core.MenuNotEditing` 绑定，因此当任意文本输入框拥有焦点时（例如检查器的 Name 输入框或搜索框），输入字符或按下 Delete/Backspace/Undo/Save 绝不会误触编辑器快捷键。当不同作用域下的两条已启用命令共享同一按键时，由更具体的当前作用域胜出（`shortcutScopePriority`）：在 2D 视图未激活瓦片工具时（例如处于选择工具），**视口**优先于**瓦片**，因此 `e` 切换为**旋转**、`r` 切换为**缩放**；一旦激活任意瓦片工具（`b`、`g`、`t`、`u`、`l`、`c`、`n`），**瓦片**作用域获得更高优先级，`e` 随之切换为**擦除瓦片**。在 3D 视图（`alt+3`）中，**3D 视口**接管 `w`、`e`、`r`、`t` 与轴向/相机按键（`1`、`ctrl+1`、`3`、`ctrl+3`、`7`、`ctrl+7`、`5`、`0`）；`View > 3D Shading` 下的 Lit、Unlit、Wireframe 三个着色命令与 `View > Scene Environment` 无快捷键，而 `f`（聚焦选中）在 2D 下通过 `FrameSelection2D` 调整独立编辑器视图 `View2D` 而不改动保存的 `Doc.Camera`（3D 下调整 `Camera3D`）。当右侧栏**智能体**标签页处于活动状态时，**智能体**作用域接管 `mod+n`（`CmdNewAgentThread`）而非 `File > New Project...`；处于运行模式时，**运行**作用域接管 `mod+l`（`CmdAskFrozenFrame`）而非向智能体询问选中对象。
  <!-- BEGIN GENERATED EDITOR SHORTCUTS (EDITOR_UPDATE_SHORTCUT_DOCS=1 go tool nsgo test ./pkg/editor -run TestShortcutDocsMatchRegistry) -->
  - **全局**：`mod+n`（新建项目…）、`mod+o`（打开项目…）、`mod+s`（保存）、`mod+w`（关闭场景标签页）、`mod+z`（撤销）、`mod+shift+z`（重做）、`mod+c`（拷贝）、`mod+x`（剪切）、`mod+v`（粘贴）、`mod+shift+v`（粘贴为子级）、`alt+2`（切换至 2D 视图）、`alt+3`（切换至 3D 视图）、`mod+l`（向智能体询问选中内容）、`mod+1`（智能体面板）、`mod+2`（检查器面板）、`mod+k`（命令面板）、`mod+/`（键盘快捷键）、`mod+shift+t`（后台任务）、`mod+y`（历史）、`mod+shift+,`（项目设置）、`mod+shift+f`（项目搜索与引用）、`mod+shift+u`（HUD 与菜单布局）、`mod+shift+e`（导出与发布）、`mod+shift+r`（构建与发布界面）、`mod+shift+m`（性能分析器）、`mod+shift+b`（检查问题）、`shift+f12`（查找用法）、`mod+,`（编辑器设置）、`mod+shift+y`（试玩报告）、`mod+enter`（保留智能体回合）、`mod+backspace`（还原智能体回合）、`mod+.`（停止智能体）。
  - **视口**：`delete` / `backspace`（删除选中对象）、`mod+d`（复制选中对象）、`mod+g`（编组选中对象）、`mod+shift+g`（取消编组）、`v`（工具：选择）、`w` / `m`（工具：移动）、`e`（工具：旋转）、`r`（工具：缩放）、`space`（工具：平移）、`p`（工具：放置）、`f`（聚焦选中）。
  - **2D 视口**：`d`（在视口上绘制草图）、`mod+enter`（草图：继续执行）、`shift+m`（草图：修正标记）、`o`（审查：切换草图叠加层）、`mod+shift+enter`（审查：保留修改并清空草图）、`mod+backspace`（审查：撤销修改并保留草图）。
  - **3D 视口**：`w`（3D 操纵杆：平移）、`e`（3D 操纵杆：旋转）、`r`（3D 操纵杆：缩放）、`shift+c`（3D 操纵杆：碰撞体）、`1`（3D 视图：前视）、`ctrl+1`（3D 视图：后视）、`3`（3D 视图：右视）、`ctrl+3`（3D 视图：左视）、`7`（3D 视图：顶视）、`ctrl+7`（3D 视图：底视）、`5`（3D 视图：正交投影）、`t`（3D 操纵杆：局部 / 全局空间）、`0`（3D 视图：场景相机）。
  - **瓦片**：`b`（工具：绘制瓦片）、`e`（工具：擦除瓦片）、`g`（工具：填充瓦片）、`t`（工具：地形）、`u`（工具：矩形瓦片）、`l`（工具：直线瓦片）、`c`（工具：图章瓦片）、`n`（工具：随机瓦片）。
  - **草图**：`p`（草图工具：画笔）、`a`（草图工具：箭头）、`b`（草图工具：方框）、`t`（草图工具：文本）、`i`（草图：在光标处添加标记）、`mod+z`（草图：撤销上一个标记）、`h`（草图：隐藏 / 显示标记）、`mod+shift+backspace`（草图：清空标记）、`shift+d`（草图：完成草图模式）、`mod+enter`（草图：发送给智能体）。
  - **审核**：`j`（审核：下一项更改）、`k`（审核：上一项更改）、`c`（审核：给智能体留言）、`x`（审核：撤回或恢复）、`b`（审核：预览请求前状态）、`mod+enter`（审核：保留更改）、`mod+backspace` / `mod+delete`（审核：全部撤回）。
  - **运行**：`mod+p` / `f5`（播放 / 停止）、`alt+f`（标记运行瞬间）、`mod+l`（画面看起来不对）、`mod+r`（运行游戏）。
  - **智能体**：`mod+n`（新建智能体线程）。
  <!-- END GENERATED EDITOR SHORTCUTS -->
- **桌面菜单栏**：`EditorView` 通过 `desktop.SetApplicationMenu`（`Editor.ApplicationMenu`）注册原生应用菜单栏（`File`、`Edit`、`View`、`Project`、`Help`），以实时可用/勾选状态和对应平台的快捷键提示暴露同一套命令；已启用插件的工具列在 `View > Tools` 下，已启用插件的命令列在 `Project > Plugins` 下，`Help` 菜单包含重播提示（`Replay tips`）与键盘快捷键（`Keyboard Shortcuts`）。
- **命令面板（`ui.CommandDialog`）**：按 `mod+k`（或点击顶栏的 **搜索 `⌘K`** 按钮 / `View > Command Palette...`）即可打开 `CommandPaletteView`，支持按子串排序与模糊匹配搜索全部内置与自定义命令、已启用插件命令、已启用插件工具（通过 `SetPluginTool` 激活）、已启用插件面板（通过 `FocusPluginPanel` 聚焦）以及当前项目的场景和文件（通过 `OpenProjectFile` 打开场景或选中文件）。
- **插件命令**：插件可通过 `Commands []PluginCommand` 声明带可选 `Shortcut` 的命令。`RegisterPlugin` 会规范化快捷键（`NormalizeShortcut`），并在改动编辑器状态之前拒绝任何在 macOS 或 Windows/Linux 上与内置命令、自定义命令或其他已注册插件冲突的快捷键。
## 资源导入设置与重建（#915）

在文件浏览器中选中图片（`.png`、`.jpg`、`.jpeg`、`.webp`）、音频（`.wav`、`.ogg`、`.mp3`、`.flac`）、字体（`.ttf`、`.otf`）或 3D 模型（`.gltf`、`.glb`）文件时（`SelectedFile`，自动排除构建产物和 `*.import.yaml` 边车文件），检查器（`InspectorView`）会显示该资源的导入设置与鸣谢信息：

- **边车与目标默认值**：`InspectAssetImport` 会读取资源旁的 `*.import.yaml` 边车，并与 `game.yaml` 的 `assets` 小节中匹配目标的默认配置合并展示（图片支持 `filter`、`format`、`mipmaps`、`srgb`、精灵表 `slice` 与帧枢轴 `pivot`；音频支持 `format`、`rate`、`channels`、`bitrate`、`peakDB`/`rmsDB` 响度归一化与 `loop`；字体支持 `size`、`padding` 与 `chars`；模型支持 `scale`、`animations`、`lodHysteresis` 与 `lods`），同时展示通用的 `credit` 鸣谢小节（`author`、SPDX `license`、`source` 与 `notice`）。
- **交互式精灵表切分网格与帧枢轴预览（#880）**：选中图片时，导入面板会在透明棋盘格上渲染实时预览画布（`DrawImageImportPreview`），叠加显示切分网格（`width`、`height`、`margin`、`spacing`、`count`）以及每个切分格子（或未切分整图）的可拖拽枢轴标记。作者可以直接在预览画布上拖拽枢轴（`BeginImagePivotDrag` / `UpdateImagePivotDrag` / `EndImagePivotDrag` / `CancelImagePivotDrag`）、点击默认枢轴预设（`TL`、`Center`、`Bottom`），或按数值编辑/重置 `pivot.default` 与单格 `pivot.cells`（`SelectImportSliceCell`、`SetImageImportPivot`、`SetImageImportDefaultPivot`、`SetImageImportCellPivot`）。每次拖拽在松手时作为单个可撤销步骤写入确定性边车；点击 **Reimport** 或 **Build Assets** 后，打包出的 TexturePacker JSON 会写入对应枢轴并由 `sprite.LoadAtlas` 精确加载为 `Frame.PivotX` 与 `Frame.PivotY`。
- **交互式 glTF/GLB 模型预览与动画播放（#897）**：在文件浏览器中选中 `.gltf` 或 `.glb` 模型时，导入面板会使用轨道相机渲染该模型（`DrawModelPreview`、`RenderModelPreview`、`OrbitModelPreview`、`PanModelPreview`、`ZoomModelPreview`、`ResetModelPreviewCamera`），实时反映当前生效的导入 `scale`，并展示模型的结构与统计信息（`InspectModelImport`）：原始与缩放后的包围盒、总三角形与顶点数，以及节点、网格、材质与动画列表。每个动画都可以选择、播放、暂停、停止、循环或拖动时间轴定位（`SelectModelPreviewAnimation`、`PlayModelPreviewAnimation`、`PauseModelPreviewAnimation`、`StopModelPreviewAnimation`、`SeekModelPreviewAnimation`、`StepModelPreviewAnimation`），同时支持节点 TRS 变换、骨骼蒙皮与变形目标动画。修改 `scale`、`animations`、`lodHysteresis` 或 `credit` 会通过 `importsettings.Encode` 写入确定性边车并显示读取该文件的 `game.yaml` 模型目标，使下一次 `game assets` 运行仅重建该模型。
- **校验写入、撤销与单字段重置**：每次编辑都通过 `importsettings.Encode` 严格校验并原子写入边车文件，作为单个可撤销步骤进入 `Editor.History`（`EditAssetImportSettings`、`SetImageImport*` / `SetAudioImport*` / `SetFontImport*` / `SetModelImport*` 以及 `SetAssetImportCredit`）。**撤销（Undo）** 会逐字节恢复编辑前的边车内容（若此前无边车则删除新建文件）；**重置（Reset）**（`ResetAssetImportField`）将单个覆盖项恢复为 `game.yaml` 目标默认值，当不再有任何覆盖项或鸣谢时自动删除空边车文件。
- **Reimport、Build Assets 与热重载**：检查器中的 **Reimport** 按钮（`ReimportAsset`）与命令面板中的 **Build Assets** 命令（`CmdBuildAssets`、`BuildAssets` / `BuildAssetsWith`）会在 UI 协程之外运行 `go tool game assets`，并将标准输出和标准错误实时流式写入停靠的 **Output** 面板（`OutputPanelView`）。构建完成后，`ReloadAssets` 会从磁盘重新加载发生变化的图集与纹理到 `Editor.Resolver`（就地更新 `sceneformat.AtlasMap` 或已有 `*sprite.Atlas`，并在实现或配置了 `AssetReloader` / `OnReloadAssets` 时回调通知），无需重启编辑器即可在资源浏览器和视口中立即生效，并自动重新运行问题检查。自定义工具与测试可注入 `Editor.AssetsCommandRunner`。

## 性能分析器面板与帧耗时条（#912）

在编辑器内运行游戏或通过 MCP 连接正在运行的游戏（`game_perf_readout`、`game_perf_zones`、`game_perf_reset`）时，编辑器会在游戏视口下方显示实时帧耗时条，并可展开严格遵循编辑器设计规范令牌（`design/editor/PlayMode.dc.html` 与 `Spec.dc.html`）的完整性能分析器面板：

- **运行模式 `FRAME TIME · LAST 60 S` 帧耗时条（`ProfilerStripView`）**：在编辑器内 Play 模式或已连接远程游戏（`ConnectProfiler`）期间始终显示在游戏视口下方。显示来自 `game_perf_readout` 的 `avg`、`p99` 与 `allocs/frame`，绘制 `8.3 ms`（`over 8.3 ms`）与 `16.7 ms`（`missed 16.7 ms`）阈值线及图例、`−60 s` / `−30 s` / `now` 时间轴，并在游戏通过 `gamedebug.Overlay.NoteFrameCause` 标注了原因（或发生 GC 卡顿标记为 `"GC pause"`）时以徽章标出尖峰及其原因。点击耗时条上的 **Profiler** 按钮即可打开完整性能分析器面板。
- **连接运行中的游戏**：可通过 `ConnectProfilerMCP`（基于官方 MCP Go SDK 的任意 `mcp.Transport`）、`ConnectProfilerCommand`（启动 `go tool nsgo mcp`）、`ConnectProfilerScope`（已挂载 `mcp.Bridge` 的进程内 `*core.Scope`）或 `NewBridgeProfilerClient` 创建客户端并传给 `Editor.ConnectProfiler`。挂载作用域后，编辑器会按 `Editor.ProfilerPollInterval`（默认 `250 ms`）轮询 `game_perf_readout` 和 `game_perf_zones`，也可通过 `Editor.PollProfiler` 按需轮询。未连接外部客户端时，内置 Play 模式（`Play` / `PlayIn`）也会自动使用 `gamedebug.Overlay`、`gamedebug.Profiler` 和 `gamedebug.GCMonitor` 采集帧耗时与区间数据。
- **完整性能分析器面板（`ProfilerPanelView`）**：可通过耗时条、状态栏 **Debugger** 按钮、`Project > Profiler`、命令面板或 `mod+shift+m`（`CmdProfiler` / `SetProfilerOpen`）开关。面板提供实时指标卡片与可切换的时间序列图表（`SetProfilerMetric`），涵盖 **Frame**（叠加 Update、Draw、总耗时与 GC 卡顿标记及 8.3 ms / 16.7 ms 参考线）、**FPS**、**Allocs & GC**（每帧分配次数、实时堆内存与 GC 卡顿）与 **Memory**（受跟踪资源内存与预算对比及分组统计），右侧列出按耗时排序的 `game_perf_zones` 命名计时区间表。
- **重置与 Chrome/Perfetto Trace 导出**：点击 **Reset**（`Editor.ResetProfiler`）会向已连接的游戏调用 `game_perf_reset` 并清空编辑器已记录的历史帧与区间表。点击 **Export Trace**（或调用 `Editor.ExportProfilerTrace` / `Editor.ExportProfilerTraceFile`，或通过 `Editor.OnExportProfilerTrace` 接管输出）会通过 `gamedebug.Profiler.WriteChromeTrace` 将已记录的帧与区间导出为合法的 Chrome/Perfetto Trace Event Format JSON。
## 开始界面（#922）

不带参数运行 `go tool game editor` 时会打开开始界面（`StartScreenView`，可通过 `SetStartScreenOpen` 切换），视觉与交互遵循 `design/editor/Start.dc.html`、`FirstRun.dc.html` 第 1 步以及 `Spec.dc.html` 设计 Token：

- **最近项目（`RecentProjects`、`ParseRecentProjects`、`EncodeRecentProjects`）**：打开项目时会把名称、目录路径和 UTC RFC3339 `lastOpened` 时间戳写入位于所有项目之外的每用户严格版本化 YAML 文件（默认通过 `DefaultRecentProjectsPath` 存在 `<UserConfigDir>/nsgo-game/recent-projects.yaml`，或由 `Editor.RecentProjectsPath` 指定）。左侧栏的 **RECENT** 列表仅在存在最近项目后出现，显示相对时间，并对磁盘上已不存在目录或 `game.yaml` 的条目（`Missing`）打上缺失标记并提供移除按钮（`RemoveRecentProject`），重启编辑器后仍然保留。
- **打开目录（`BrowseAndOpenProject`）**：通过 NSGo 原生目录选择器（`core.Scope.PickDirectories`）或可注入的 `Editor.PickProjectFolder` 钩子选择项目目录并打开。
- **从模板新建项目（`ProjectTemplates`、`StartCreateProject`）**：模板卡片与 `All` / `2D` / `3D` 筛选标签直接从 `game new` 的模板注册表动态填充，因此新增模板无需修改编辑器代码即可自动出现。项目名称、父目录（默认 `~/games`）和可选模块路径会在输入时实时校验（`ValidateNewProjectInputs`）：检查是否仅含 `a–z`、`0–9` 和 `-`、检查父目录写入权限，以及目标目录是否已存在（已存在时提供 **Open it** 和如 `dungeon-run-2` 的可用名称建议）。点击 **Create** 会在 UI 协程之外运行 `game new`（含 `go get` 和 `go mod tidy`），将标准输出与错误实时展示在界面中，成功后自动打开生成的 `game.yaml`。
- **打开演示（`OpenDemo`、`NewDemo`）**：无需在磁盘上创建或打开项目，直接进入内置的演示房间场景。
- **使用 AI 开始：从一句话到首次审查（#1051、#1505）**：点击 **Start with AI**（`StartWithAI`）由已连接智能体与编辑器 MCP 服务器真实驱动 `design/editor/FirstRun.dc.html` 中的五步流程：
  - **后续步骤与原地连接**：开始界面列出三步（**Connect an agent**、**Approve its plan**、**Review the first scene**）。当尚未连接智能体时，**Start with AI** 会在开始界面原地展开 **Connect an agent**（`FirstRunStageConnect`），同时保留已输入的创意与项目名称（点击 **Edit** 或 **Later** 可完整返回第一步）。它会检测 `PATH` 上的 `claude` 与 `codex`（或通过 `Editor.FirstRunDetectAgents` 注入），通过查询编辑器 MCP 服务器启用的工具数（`Editor.AgentTools`）并经由 `Editor.AgentRunner` / `CLIAgentRunner` 发起就绪探测完成连接（无法应答时明确报错并提供 **Copy diagnostics**，可通过 `Editor.FirstRunConnectAgent` 替换），或向 `BuiltInAgentEndpoint` 发送一次最小请求立即校验粘贴的 API 密钥（绝不写入 YAML，可通过 `Editor.FirstRunValidateAPIKey` 替换）。
  - **首个计划（`FirstRunStagePlan`、`OpenFirstRunPlan`）**：请求已连接智能体通过 `plan_propose`（或结构化 JSON 计划回复，可通过 `Editor.FirstRunPlanner` 替换）针对作者创意提出计划，在只读模板预览视口旁展示澄清问题、编号步骤与紫色虚线幽灵（`FirstRunGhost`），并提供 **Just do it**（`JustDoItFirstRunPlan`）与 **Approve ⌘↵**（`ApproveFirstRunPlan`）。挂载 UI 时计划请求在后台协程执行以避免阻塞 MCP `plan_propose` 回调，失败时展示可重试的错误卡片。在点击 **Approve** 之前不会在磁盘上创建项目目录。
  - **构建中（`FirstRunStageBuilding`）**：点击 **Approve** 创建全新项目目录后，编辑器搭建模板脚手架、打开目标场景并调用已连接智能体通过 `scene_edit` 真实构建场景（根据场景变化实时上报对象数量与已放置对象标签），随后运行 `project.Check` 与 `assetcheck`，并在项目包含 `playtest.yaml` 时运行 `go tool game playtest`（或 `Editor.FirstRunPlaytest`），若模板无 `playtest.yaml` 则如实标注为 **Skipped**。按 **Cancel** 或 `Esc` 会先暂停并确认（**Keep building**、**Stop, keep the files**，以及仅当目录系本次流程刚刚新建时才提供的 **Stop and delete the folder**，绝不删除同级或已有目录）。若 `Check` 阶段失败，会在进入审查前停住并展示修复方案（**Fix it** / **Review as is**）。
  - **首次审查与一次性提示（`FirstRunStageReview`）**：直接根据磁盘上实际写入的项目文件与场景对象生成变更列表、计数、示意图与偏差摘要，顶部显示计划完成情况，提供 **Before / After** 切换与按住 `B` 键预览（`Before: an empty folder`）、逐项撤销/恢复、**Revert all** 以及 **Keep N of M**（`KeepFirstRunReview`）。三条引导提示（逐项保留或撤回、按住 `B` 预览、选中后按 `⌘L` 提问）按次序单条展示且每人仅展示一次，记录在 `EditorSettings`（`editor.yaml`）的 `seenTips` 字段中；**Skip tips** 会一次性关闭全部三条，**`帮助 › 重播提示`**（`Help › Replay tips`、`CmdReplayTips` / `ReplayTips`）可清除记录并重新显示。
- **干净切换项目与未保存修改提示**：从开始界面，或稍后通过 `File > Open Project...`（`mod+o`）、`File > New Project...`（在右侧栏**智能体**标签页之外按 `mod+n`；当**智能体**标签页激活时，`ScopeAgent` 会将 `mod+n` 绑定为 `CmdNewAgentThread`）和 `File > Start Screen` 打开另一个项目时，会干净地替换当前项目、场景、动画片段、过场动画、数据表、本地化工作区、骨骼绑定和撤销历史。只要当前会话存在未保存修改（`HasUnsavedChanges`），编辑器统一的未保存修改确认框就会先弹出，由用户选择 **Save**（`ConfirmSaveAndSwitch`）、**Discard**（`ConfirmDiscardAndSwitch`）或 **Cancel**（`CancelPendingSwitch`）。

## 3D LOD 与遮挡体可视化（#904）

在 3D 视图（`View3D`）中，视口会运行与游戏运行时一致的 `scenemodel.Build` + `render3d.OcclusionBuffer` + `render3d.Queue` 评估流程（打开项目时自动使用 `scenemodel.NewAssets`，也可注入 `Editor.ModelResolver`）：

- **LOD 叠加层（`ShowLODOverlay` / `SetShowLODOverlay`）**：按当前选中的 LOD 级别为每个多级 LOD 对象着色，并在视口中绘制 `LOD<n>` 标签；3D 检查器同时显示选中对象当前的活动 LOD（`ObjectLOD3D`、`ObjectLODCount3D`）。在资源导入检查器中修改模型的 `lods` 或 `lodHysteresis` 会立即使 3D 构建缓存失效，切换效果即时可见。
- **遮挡体可视化与编辑（`ShowOccluders` / `SetShowOccluders`、`SetObjectOccluder`）**：为每个可见遮挡体绘制品红色线框及 `OCC` / `OCC BOX` 标签（包括显式 `box` 遮挡体与 `model` 遮挡体），以弱化虚线标示被遮挡体剔除的对象（`ObjectOccluded3D`），并在 3D 检查器中提供可撤销的 Occluder 模式（`none` / `model` / `box`）、盒体尺寸和双面开关控件（必要时自动将版本 1 场景升级为版本 2）。
- **RenderStats 叠加层（`ShowRenderStats` / `SetShowRenderStats`、`RenderStats3D`）**：在 3D 视口右下角实时显示 `render3d.RenderStats`（`Draws`、`Visible`、`Occluded`、`Tris`、`Verts` 及各多级 LOD 对象当前选中的 `LOD` 级别），并可通过 3D 视口栏的 **Overlays** 弹出面板（`view3d-overlays`）、`View > Overlays` 或命令面板（`CmdToggleLODOverlay`、`CmdToggleOccluders`、`CmdToggleRenderStats`）与 LOD、Occluders 叠加层一同独立开关。

## 远程场景树与实时检查器（#911）

当接入了 [`game/mcp`](mcp.md) 的游戏带着 `-automation` 运行时，编辑器会作为 MCP 客户端连接（桌面端通过 `github.com/modelcontextprotocol/go-sdk/mcp` 启动 `go tool nsgo mcp`，也可通过 `ConnectRemote` 或 `Editor.RemoteTransport` 传入任意 `mcp.Transport`），实时查看和调校运行中的游戏：

- **连接运行中的游戏**：在 **Remote** 标签页开启 **Auto-connect on Run**（`SetRunWithAutomation`）或调用 `RunGameWith(RunGameOptions{Automation: true})`，即可向 `go tool game dev` 传递 `-automation` 并在游戏启动后自动连接；也可直接调用 `ConnectRemote` / `ConnectRemoteCommand` 连接到已在运行的游戏。`NewRemoteBridgeServer(bridge, run)` 将 `*mcp.Bridge` 直接包装为 MCP 服务端，便于确定性的进程内或管道测试。
- **场景树旁的 Remote 标签页**：在 `SceneTreeView` 顶部可在 **Scene**（`SceneTreeLocal`）与 **Remote**（`SceneTreeRemote`）之间切换。Remote 标签页列出运行中的场景（`game_scenes_list`）、按绘制顺序排列的图层（`game_layers_list`）以及带父子层级的实时实体（`game_entities_list`），游戏未暂停时按 `Editor.RemoteRefreshInterval`（默认 250 ms）自动刷新，也可通过 `RefreshRemote` 手动刷新。不带场景 `ObjectRef` 的运行时生成实体归入 **SPAWNED AT RUNTIME** 分组；运行中被销毁的场景对象会变暗显示并标出消失时的步数。
- **实时检查器与实时改动（Live Changes）**：在 Remote 标签页选中实时实体（`SelectRemoteEntity`）后，`InspectorView` 会通过 `game_entity_inspect` 展示其全部组件。派生的只读组件（`world_transform2d`、`object_ref`）以实时只读形式显示；可编辑组件渲染对应类型的控件，编辑后立即通过 `game_component_set` 写入运行中的游戏（`SetRemoteComponentField` / `SetRemoteComponentValue`），并在已修改字段旁标出 `"was X"`。每次实时修改都会记录在 **LIVE CHANGES** 列表（`RemoteLiveChanges`）中，断开连接或停止游戏后列表仍保留：可逐行撤回（`RevertRemoteLiveChange`）、全部丢弃（`DiscardRemoteLiveChanges`），或点击 **Keep in scene**（`KeepRemoteLiveChangesInScene`）将所有映射到当前场景对象的改动作为单个可撤销的 `History.Group` 步骤写回场景，一次撤销即可完整还原。
- **实时控制栏、可调参数与控制台**：连接期间顶部会显示实时控制栏，提供 **Pause / Resume**（`mod+shift+p`、`RemotePause` / `RemoteResume`）、单步 **Step**（暂停时按 `.`）与 **Step N** 固定步进（`RemoteStep`）、时间倍率 **Time Scale**（`0.25×`、`0.5×`、`1×`、`2×`、`RemoteSetTimeScale`），并在状态栏显示实时帧数、模拟时间与实体数量。若游戏配置了 `gamedebug.Console`，远程检查器还会展示实时 **Tunables**（`game_tunables_list` / `RemoteSetTunable`）与 **Console** 命令行（`RemoteExecConsole` / `game_console_exec`）。
## 3D 地形雕刻与绘制工具（#900）

在 3D 视图（`ViewMode3D`）中，选择 **Project > 3D Terrain Editor** 或命令面板（`CmdTerrainEditor`）——或在文件浏览器中双击 / 点击 **Terrain** 打开 `*.terrain.yaml`，或在 Go 代码中调用 `NewTerrain`、`OpenTerrainFile`、`OpenTerrainProjectFile`、`OpenTerrain`——即可在 3D 视口中直接雕刻和绘制 `world3d` 地形，并在视口下方显示停靠的 `TerrainPanelView` 面板：

- **雕刻与绘制画笔**：支持 `TerrainBrushRaise`（抬高）、`TerrainBrushLower`（压低）、`TerrainBrushSmooth`（平滑）、`TerrainBrushFlatten`（整平至起笔处高度，或启用 `UseTargetHeight` 时整平至 `TargetHeight`）、`TerrainBrushSetHeight`（设定高度 `TargetHeight`）、`TerrainBrushPaintLayer`（绘制纹理材质层权重并自动保持各层权重之和归一化为 1）以及 `TerrainBrushPaintFoliage`（将选中植被层的密度向 `[0, 1]` 内的 `TargetDensity` 过渡，`0` 为擦除）。可通过 `SetTerrainBrush` 或面板控件配置半径 `Radius`、强度 `Strength` 和衰减曲线 `Falloff`（`TerrainFalloffSmooth`、`TerrainFalloffLinear`、`TerrainFalloffFlat`）。
- **3D 射线笔触与单步撤销/重做**：在 3D 视口中点击或拖拽会对实时 `world3d.Heightmap` 执行射线检测（`RaycastTerrain3D`、`BeginTerrainStrokeAt` / `ContinueTerrainStrokeAt` / `EndTerrainStroke`，或直接传入世界坐标的 `BeginTerrainStroke` / `ContinueTerrainStroke`），在快速拖拽路径上自动插值平滑落笔，在地形表面投射贴地画笔半径环，并将一整次拖拽笔触作为 **单步撤销记录** 存入 `Editor.History`（按 `Escape` 或调用 `CancelTerrainStroke` 可中止当前笔触并还原）。撤销与重做会精确恢复高度图、材质权重和植被密度数据，并同步更新 `TerrainDirty()` / `HasUnsavedChanges()`。
- **16 位无损保存**：点击 **Save Terrain**（`SaveTerrain`、`SaveTerrainTo` 或脏状态下的 `Save`）会原子写出 16 位灰度 PNG 高度图（`*image.Gray16`，消除 8 位斜坡阶梯伪影）、逐层灰度权重图 PNG（或打包 RGBA `splatMaps`）、植被密度图 PNG 以及确定性 `*.terrain.yaml`，使游戏侧通过 `world3d.LoadTerrainFS`、`TerrainDoc.ScatterFoliage` 与 `render3d.TerrainMaterialFromDoc` 加载并绘制出完全一致的地形。
## 粒子发射器编辑器（#883）

粒子发射器编辑器（`ParticleEditorPanelView`）可通过 **Project > Particle Emitter Editor**、命令面板（`CmdParticleEditor`）、在文件浏览器中双击 `*.particles.yaml` / `*.emitter.yaml` 或点击 **Open Emitter**，或在检查器中点击带有 `particles` 对象的 **Edit Emitter** 按钮打开，支持带实时预览与完整撤销/重做地编辑 2D `particles.Config` 的全部字段：

- **不重启的实时预览**：面板在预览画布上运行实时的 `*particles.Emitter`（`ParticlePreview`），提供 **Play/Pause**（`PlayParticlePreview` / `PauseParticlePreview`）、**Restart**（`RestartParticlePreview`）、**Step**（`StepParticlePreview`）、**Burst**（`BurstParticlePreview`）、**Preview Atlas**（`SetParticlePreviewAtlas`）以及用于测试反弹/停止/销毁碰撞的可选 **Floor** 地板碰撞体（`SetParticlePreviewFloor`）。修改发射速率、曲线、运动等参数时通过 `Emitter.ReloadConfig` 立即生效，不会重启或清除当前存活粒子；未绑定图集或帧时，预览会以彩色圆点回退绘制，便于随时调参。
- **全字段编辑与交互式曲线控件**：覆盖 `space`、位置、旋转、`z`、`seed`、`shape`（`point`、`circle`、`ring`、`rect`）、`rate`、`maxParticles`、定时爆发 `bursts`（增删改 `time`、`count`、`cycles`、`interval`）、`lifetimeMin`/`Max`、`speedMin`/`Max`、`angleMin`/`Max`、`gravityX`/`Y`、`initialRotationMin`/`Max`、`rotationSpeedMin`/`Max`、`collision`、`trail` 与递归 `subEmitters`（通过 `SetParticleTargetPath` / **Edit Child** 进入编辑子发射器配置）。曲线控件在交互画布上绘制 `sizeCurve`、`alphaCurve`、`rotationCurve`、`colorCurve`、`trail.widthCurve` 与 `trail.colorGradient`，支持拖拽控制点、点击新增控制点及数值微调；图集帧选择器可直接切换解析器图集中的 `frameNames`。
- **撤销/重做与确定性保存**：所有编辑均记录在 `Editor.History` 中；点击 **Save Emitter**（`SaveParticleEmitterFile` / `SaveParticleEmitterYAML` / `OnSaveParticleEmitter`）以规范的版本化 YAML（`particles.MarshalConfigYAML`）写出，往返加载保持字节不变，并热重载当前打开场景中引用同一文件的发射器。
- **场景放置**：可通过 `SetObjectParticles`、`PlaceParticles` 或检查器的 **PARTICLES** 区域（`file` 与 `atlas` 选择器）为场景对象绑定粒子发射器。放置的发射器在 2D 编辑视口中实时预览（`StepSceneEmitters`，隐藏对象时同步隐藏），在 Play 模式（`PlayIn`）与独立游戏（`sceneformat.Build`）中正常运行，纳入 `tooling/refs` 扫描（`MoveFile` 自动改写 `particles.file`），并由 `assetcheck` 校验文件及帧名。
## 语言与外观设置（#921）

编辑器界面通过内嵌的带版本号 `l10n` YAML 字符串表（`editor/locales/en.yaml`、`editor/locales/zh.yaml`）同时提供英文（`en`）与简体中文（`zh`）界面，并将每名用户的编辑器偏好设置作为确定性的带版本号 YAML 保存在项目之外：

- **用户级设置文件（`EditorSettings`、`SettingsVersion = 1`、#921、#1005、#1006、#1028）**：默认保存在 `DefaultSettingsPath()`（`os.UserConfigDir()` 下的 `nsgo-game/editor.yaml`，位于所有游戏项目之外），通过 `ParseEditorSettings`、`EncodeEditorSettings`、`LoadEditorSettings`、`SaveEditorSettings`、`Editor.LoadSettings`、`Editor.LoadUserSettings` 和 `Editor.SaveSettings` 读写。文件保存 `version`、`language`、`appearance`、可选的 `autoSave`（省略时默认为 `true`）、可选的 `interfaceSize`（`compact`、`default` 或 `large`）、可选的 `snap`（用户级默认吸附设置：`grid`、`gridSize`、`pixel`、`rotation`、`rotationStepDeg`、`scale`、`scaleStep` 与 `showGrid`，当项目未在 `.game/editor.yaml` 单独覆盖吸附设置时自动生效）、可选的 `showAgentChanges`（省略时默认为 `true`）、可选的 `navigation3D`（`default`、`blender` 或 `maya`）、可选的 `viewportOverlays`（按字典序排列的已启用 2D 视口叠加层 ID 列表；省略时使用默认组合，`[]` 表示全部关闭）、可选的 `overlayLegend`（按字典序排列的已启用 Spec 视口叠加图例类型：`selection`、`agent`、`added`、`changed`、`removed`、`problem`；省略时全部开启，`[]` 表示全部隐藏）、可选的 `shortcuts`、可选的 `seenTips` 以及可选的 `agent` 偏好（任务路由、MCP 工具开关、外部 MCP 服务器、每周请求限额、出站隐私开关与本地用量统计；API 密钥等机密绝不写入 `editor.yaml`）。未知字段、YAML 锚点/别名/合并键、多余文档、负数/非有限吸附步长和非法枚举值都会被拒绝并返回 `*loaderr.Error`。输出为带 `SettingsSchemaComment` 头的确定性 2 空格 YAML，加载后重新保存字节完全不变。已发布的 JSON Schema 为 `https://game.nsgo.dev/schemas/editor-settings.v1.json`（`SettingsSchemaURL`）。
  ```yaml
  # yaml-language-server: $schema=https://game.nsgo.dev/schemas/editor-settings.v1.json
  version: 1
  language: system
  appearance: system
  autoSave: true
  interfaceSize: default
  snap:
    grid: true
    gridSize: 16
    rotation: true
    rotationStepDeg: 15
    showGrid: true
  showAgentChanges: true
  navigation3D: default
  shortcuts:
    view.frame: g
  ```
- **统一设置窗口（`SettingsView`、`⌘,` / `Esc`、#1005）**：
  - **打开、关闭与焦点恢复**：按 `⌘,`（`mod+,`、`CmdEditorSettings`）可随时打开全窗口设置视图（无论是否打开项目），按 `Esc` 关闭并将键盘焦点精确恢复到打开前所在的控件。
  - **分组导航与页面结构**：左侧导航列表按 **`AGENT`**（模型与连接、权限、游戏简报、项目规则、工具、用量与隐私）、**`PROJECT · game.yaml`**（项目、输入映射、音频总线、导出；未打开项目时不显示空分组）与 **`EDITOR · THIS COMPUTER`**（通用、视口、快捷键）分组，每项附带实时元信息且不显示空分组标题。每个页面均显示标题、单行副标题与作用域标签（`此计算机`、`项目 · 共享` 或 `项目 · game.yaml`），每行均包含标签、说明文本、值控件以及 `智能体可修改` / `仅限用户` 策略列。
  - **搜索与智能体转交**：在顶部搜索框（`搜索设置，或告诉智能体你想做什么`）输入关键词会跨页面匹配设置标签与说明，同时收窄左侧导航（隐藏无匹配页面与空分组）以及当前页设置行；当没有任何设置匹配且智能体面板处于启用状态时，可直接将该查询发送给智能体（`AskAgentFromSettings`）。
  - **通用页面（`SettingsPageGeneral`）**：配置**语言**（`Editor.SetLanguage`）、**外观**（`Editor.SetAppearance`）、**自动保存**（`Editor.SetAutoSave`，仅自动保存已保留的更改，延迟 2 秒）和**界面尺寸**（`Editor.InterfaceSize`、`Editor.SetInterfaceSize`：`紧凑`、`默认`、`较大`，文本与控件同步缩放）。
  - **视口页面（`SettingsPageViewport`、`OpenViewportSettings`）**：配置**吸附**（开关、像素步长 `px` 与旋转角度 `°`，按住 `⌘` 临时忽略）、**网格**（按吸附步长显示，`SetShowGrid`）、**显示智能体更改**（`Editor.ShowAgentChanges`、`Editor.SetShowAgentChanges`，审阅打开时在视口中显示轮廓）、**3D 导航**（`Editor.Navigation3D`、`Editor.SetNavigation3D`、`Navigation3D.Resolve`：`默认` 为左键环绕 / Shift+左键或中键平移 / 右键飞行；`Blender` 为中键环绕 / Shift+中键平移 / 右键飞行 / 左键选择与操纵杆；`Maya` 为 Alt+左键环绕 / 中键或 Alt+中键平移 / Alt+右键推拉 / 普通右键飞行 / 普通左键选择与操纵杆；所有模式下滚轮推拉与触控/触控板手势保持不变），以及按计算机保存的全部**叠加层图例**开关（`ViewportOverlays`）。
  - **键盘与无障碍**：左侧列表支持 `ArrowDown` / `ArrowUp` / `Home` / `End` 方向键导航；所有开关均使用 `SpecSwitch`（`RoleSwitch`、`aria-checked` 与 2 px Spec 强调色焦点环）；每一行均可经由 `Tab` 键访问。
- **自动保存与状态栏保存状态（#1016）**：
  - **通用 › 自动保存（`Editor.AutoSave`、`Editor.SetAutoSave`）**：默认启用，并以 `autoSave` 字段保存在 `EditorSettings` 中。对有对应文件的场景与 `game.yaml` 的已保留更改会在 2 秒防抖延迟后自动保存（`DefaultAutoSaveDelay`，可通过 `Editor.AutoSaveDelay` 覆盖）。处于打开审查中的编辑（`Editor.HasOpenReview()`，涵盖 `BeginReview` / `KeepReview`、过场审查、试玩指标提案审查、安全重命名审查、合并冲突与未解决的外部磁盘冲突）尚未保留，在确认保留前不会自动保存。
  - **状态栏保存状态项（`Editor.SaveStatus`，`SaveStatusSaved` / `SaveStatusSaving` / `SaveStatusFailed`）**：已保存时以柔和 Spec 样式显示 `已保存`（悬停提示显示上次保存时间）；仅当保存耗时超过 300 毫秒（`DefaultSavingIndicatorDelay`，可通过 `Editor.SavingIndicatorDelay` 配置）后才显示 `正在保存…`，避免快速保存闪烁；自动保存失败时会持续停留在状态栏中（从不以 toast 弹出），显示可读原因（如 `无法保存：文件夹为只读` 或 `无法保存：磁盘已满 · 剩余 12 MB`）以及**重试**（`Editor.RetrySave`）和**另存为…**操作，直到问题解决。
  - **保存失败期间**：场景名称及失败标签页旁的未保存圆点变为 Spec 问题红（`#FF6B6B`），退出前会先询问（`Editor.HasUnsavedChanges()` 保持为 `true`），且 `editormcp` 会在后续每次工具结果中警告智能体当前更改未落盘，直到保存成功。
- **实时语言切换**：`Language` 支持 `LanguageSystem`（`"system"`）、`LanguageEnglish`（`"en"`）和 `LanguageChinese`（`"zh"`）。调用 `Editor.SetLanguage` 或在统一**设置**视图的 **`EDITOR · THIS COMPUTER` > General** 页面（`SettingsView` / `EditorSettingsDialogView`，可通过 `文件 > 编辑器设置…`、命令面板 `CmdEditorSettings` 或 `mod+,` 打开）中切换语言后，顶栏、桌面菜单栏、文件浏览器、资源浏览器、场景树、图层面板、检查器、底部面板、对话框、命令面板与状态栏都会立即切换，无需重启。`Editor.ResolvedLanguage()` 返回当前生效的语言（`"en"` 或 `"zh"`），并通过 `selection_get` 传递给连接的 MCP 智能体。
- **外观（`core.Scope.SetAppearance`）与 Spec 深/浅色主题（#1062）**：`Appearance` 支持 `AppearanceSystem`（`"system"`）、`AppearanceLight`（`"light"`）和 `AppearanceDark`（`"dark"`）。`EditorView` 会在根作用域挂载内嵌的 YAML 规范主题（`theme.yaml`，经由 `core.ParseTheme` 解析并通过 `theme.Core`、`theme.Dark`、`theme.Light`、`theme.PaletteFor` 与 `Editor.Palette` 暴露），使所有 NSGo `ui` 组件、面板、视口叠加层、叠加图例（`SpecOverlayLegendView` / `SetOverlayLegendOpen`）与轻提示 Toast（`ShowToast` / `DismissToast` / `ToastView`）在深色与浅色外观下都严格遵循 Spec 令牌，不再回退到 NSGo 默认主题。深浅两套调色板中的每一组前景/背景、强调按钮文字、警告卡片与视口叠加层均按同一张 WCAG 2.2 对比度表进行校验（`theme.ContrastRatio`、`theme.ContrastTable`：正文、胶囊文字及强调/智能体/问题填充上的文字均 >= 4.5:1 AA；焦点环与视口叠加层描边 >= 3:1 UI 对比度）。当设置为 `AppearanceSystem` 时，系统外观变化会实时重绘编辑器而无需重启；显式选择的 `AppearanceLight` 或 `AppearanceDark` 会持久化到 `Editor.SettingsPath` 并在重启后保持生效。
- **快捷键：按作用域搜索与重绑（#1061）**：
  - **命令作用域**：每个 `EditorCommand` 和 `PluginCommand` 都带有 `Scope`（`ScopeGlobal`、`ScopeViewport`、`ScopeViewport2D`、`ScopeViewport3D`、`ScopeSceneTree`、`ScopeTile`、`ScopeAnimation`、`ScopePlay`，未指定时默认为 `ScopeGlobal`）。
  - **设置 › 快捷键（`EditorSettingsTabShortcuts`、`OpenShortcutsSettings`、`CmdShortcutsSettings`）**：列出所有已注册的内置、自定义与已启用插件命令及其作用域和当前按键，支持按命令名称、按键或作用域搜索（`FilterShortcutCommands`）。点击命令的按键按钮即可进入按键录制模式（`ShortcutFromKeyEvent`，按 `Escape` 可取消录制而不关闭设置对话框）。若按键在**同一作用域**中已被占用（`ShortcutConflict` / `*ShortcutConflictError`），会在行内原地提示并提供**交换**按钮（`SwapShortcut`，交换后两条命令互换按键且均保持绑定）；若按键仅在**不同作用域**中被使用（`RebindShortcut`），则会立即绑定而不弹出冲突提示。支持单行**重置**（`ResetShortcut`）与顶部**全部重置**（`ResetAllShortcuts`）。
  - **持久化与全界面即时同步**：自定义按键保存在 `EditorSettings` 的 `shortcuts` 字段（`editor.yaml`，位于所有项目之外），重绑后立即同步到 `EffectiveShortcut`、顶栏提示与无障碍标签、桌面菜单栏（`ApplicationMenu`）、命令面板（`CommandPaletteView`）、实时按键调度以及 **`⌘/` 快捷键速查表**（`ShortcutsSheetView`、`CmdShortcutsSheet` / `mod+/`）。
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

可通过 `Project > HUD & Menu Layout`、命令面板（`mod+shift+u` / `CmdHUDLayout`）、在文件浏览器中双击或打开任意 `*.screen.yaml` 文件，或点击问题面板中的界面诊断条目打开 HUD 与菜单布局界面（`HUDLayoutView`、`OpenHUDScreen` / `OpenHUDScreenFile` / `OpenHUDScreenDoc`）。界面严格遵循编辑器设计规范令牌（`design/editor/HUDLayout.dc.html` 与 `Spec.dc.html`），在所选语言下同时呈现所有屏幕尺寸，并将手动编辑聚焦在高频微调上：

- **顶栏与语言切换**：左侧展示 `<项目名> / screens / <文件名>` 面包屑（点击可返回场景编辑器），中间为读取自 `locales/*.yaml`（并叠加未保存的 `Editor.l10n` 编辑）的 **Language** 分段语言切换器（`SetHUDLocale`），右侧为 **Search ⌘K** 与 **Play**。切换语言会立即重绘全部四个设备尺寸、更新文本宽度表、检查器多语言文本与溢出放大预览以及 **GAME CHECK** 诊断。
- **左侧栏（`SCREENS` 与 `NODES`）**：列出项目中所有 `*.screen.yaml` 文件（`HUDScreens`）及其节点数与当前语言的问题数；下方展示当前界面的层级节点树（`HUDTree`），附带等宽锚点标记（`TL`、`TC`、`TR`、`CL`、`C`、`CR`、`BL`、`BC`、`BR`、`FILL`）与红色问题圆点。
- **四尺寸同屏预览与叠加开关**：通过真实的 `gameui.BuildScreen` 布局（`HUDLayoutFrames`）同时绘制带刘海的 **Phone 19.5:9**（`780 × 360`）、**Tablet 4:3**（`640 × 480`）、**Desktop 16:9**（`640 × 360`）与 **Ultrawide 21:9**（`840 × 360`），提供 **Safe areas**、**Anchors**、**Touch areas** 与 **Focus order** 四个叠加层开关（`HUDOverlays` / `ToggleHUDOverlay`），并在视口右下角浮层展示 **TEXT WIDTH, PX** 表（`HUDTextWidths`），逐语言对比每个文本节点的容器宽度与实际文本宽度并高亮溢出项。
- **检查器、溢出放大视图与 `GAME CHECK`**：展示选中节点的 **ANCHOR**（3×3 锚点按钮网格加 **Fill**、文字描述锚点与偏移量、以及 **Keep inside the safe area** 开关）、**SIZE**（**Fixed** / **Fit text** 切换与触控目标尺寸）、**TEXT**（键名、各语言译文以及文本溢出时的 `1.5×` 放大细节预览卡片），以及基于 `gameui.CheckScreen` 的实时 **GAME CHECK** 问题列表（`HUDProblems`），每条问题附带可直接选中目标节点的 **Show** 按钮与 **Ask the agent to fix N** 按钮；无问题时显示 `"No problems at any size in this language"`。
- **手动微调与智能体输入框**：调整容器内节点顺序（`ReorderHUDNode`）、修改容器间距（`SetHUDNodeSpacing`）、切换锚点/安全区/尺寸模式（`SetHUDNodeAnchor`、`SetHUDNodeSafeArea`、`SetHUDNodeSizeMode`）以及直接拖拽触控摇杆与触控按钮（`BeginHUDTouchDrag`、`UpdateHUDTouchDrag`、`EndHUDTouchDrag`、`CancelHUDTouchDrag`）均在 `Editor.History` 中记录为**单个可撤销步骤**，并将规范 YAML 写回磁盘上的 `*.screen.yaml` 文件（撤销时逐字节恢复原文件）。底部输入框（`SubmitHUDPrompt`、`AskAgentToFixHUDProblems`、`Editor.OnHUDPrompt`）用于向智能体发起其余界面修改请求。

## 过场动画时间轴与智能体审查（#1056）

打开任意 `*.cutscene.yaml` / `*.cutscene.yml` 文件（通过文件浏览器、命令面板、`OpenCutsceneProjectFile`、`OpenCutsceneFile`、`OpenCutsceneYAML` 或智能体的 `cutscene_write` MCP 工具），都会进入占满工作区的过场动画时间轴编辑器（`CutscenePanelView`），完全采用 `Spec.dc.html` 设计 Token 与 `design/editor/Animation.dc.html` 第 2 屏布局：

- **左侧栏（`CUTSCENES` 与 `LINES`）**：列出项目中的所有过场文件（`ProjectCutscenes`），显示时长并标出 `video` 视频类型；下方列出从 `loc/en.yaml` 与 `loc/zh.yaml`（或 `Editor.CutsceneCatalog` / 已打开的本地化工作区）解析出的本地化对白条目（`CutsceneLines`），展示行 ID、英文和中文译文。
- **实时预览舞台与缩略图拖动条**：使用真实的 `cutscene.TimelinePlayer` 驱动隔离的场景副本（`CutscenePreviewScene`、`SeekCutscene`、`PlayCutscene`、`PauseCutscene`、`StepCutscene`），拖动到任意时间点都与运行时播放器摆出的角色位置、相机与字幕完全一致。界面展示当前相机标签（`CutsceneCameraLabel`）、本地化字幕叠加层（`CutsceneActiveDialogue`）、Play / Pause 播放控制、时间读数以及 24 帧可点击缩略图拖动条（`CutsceneThumbs`）。
- **轨道泳道与片段检查器**：为 Camera、每个角色、Dialogue、Sound 和 Music 轨道分别显示泳道与片段块。选中片段（`SelectCutsceneClip`、`SelectCutsceneClipByID`）后，可通过 `±0.1 s` 步进按钮调整 **Start** 和 **Duration**（`AdjustCutsceneClipStart`、`AdjustCutsceneClipDuration`、`SetCutsceneClipTiming`），并在相机和角色轨道上切换 **Easing**（`Linear`、`In`、`Out`、`In-out`、`Hold`，`SetCutsceneClipEasing`）。选中对白片段时会展示行 ID、中英文译文，并提示文本存放在 `loc/en.yaml` 与 `loc/zh.yaml` 中、过场文件只保存 ID。
- **智能体变更审查（`OpenCutsceneReview`）**：在不提前改写磁盘文件的前提下对比修改前（`Before`）与智能体提议（`After`）。支持切换 **Before** / **After**（`SetCutsceneReviewViewMode`）或按住 **B** 键快速窥视修改前状态（`SetCutsceneReviewPeekBefore`、`CutsceneHandleKeyEvent`）；使用虚线/实线样式与图例区分新增（绿色虚线）、修改（琥珀色虚线）与选中（蓝色实线）片段；右侧 `N CHANGES` 列表支持逐条 **Revert** / **Restore**（`ToggleCutsceneReviewChange`）及点击跳转定位（`FocusCutsceneReviewChange`）；审查期间用户的手动微调会自动并入本次审查且可用 `⌘Z` 撤销（`"Your edit joins this review · ⌘Z undoes it"`）；点击 **Revert all**（`RevertAllCutsceneReview`）可放弃全部提议，点击 **Keep N of M**（`KeepCutsceneReview`）则将保留的变更与手动微调作为**单个原子撤销步骤**一并写入内存与磁盘。

## 变更与提交面板（#1057）

点击状态栏中的 **变更** 入口（实时显示工作区已改动文件数量）、执行 `项目 > 变更与提交` 或通过命令面板（`CmdChanges` / `OpenChangesPanel` / `CloseChangesPanel`）即可打开全窗口的**变更与提交**界面（`ChangesPanelView`），布局对应 `design/editor/VersionControl.dc.html` 第 1 屏并严格采用 `Spec.dc.html` 设计令牌：

- **分支切换、领先/落后计数与 Pull**：顶部栏显示 `<项目名> / <场景名>`、分支切换下拉菜单（`GitSwitchBranch`，同时显示当前分支与 `ahead N` / `behind M` 计数）、**Refresh**（`RefreshChanges`）、**Pull**（`GitPull`，仅快进拉取）与 **Play**。
- **按类别分组的 CHANGES 列表**：将相对于 `HEAD` 的工作区改动（`git status --porcelain=v1 -uall`）按 **SCENES**、**DATA**、**CODE** 与 **ASSETS**（`ChangesCategory`）分组展示，每行提供纳入提交勾选框（`SetChangeIncluded`、`ToggleChangeIncluded`）、状态徽章（`M`、`A`、`D`、`R`）以及简明摘要（如 `4 added · 3 changed objects`、`+7 -4 lines` 或音频时长与大小）。
- **Review 布局下的逐文件 `HEAD` 对比**：
  - **场景（`*.scene.yaml` / `*.scene.yml`）**：对比 `HEAD`（`git show HEAD:<path>`）与工作区场景文档，提供并排的 **HEAD** 与 **Now** 2D 场景预览（亦可通过 `SetChangesSceneView` 切换单视图 **HEAD** / **Now**），用绿色虚线标出新增对象、琥珀色虚线标出改动对象、红色虚线标出移除对象，并在下方显示逐对象变更徽章（如 `added · ...`、`changed · Door (x 584 → 560)`、`removed · ...`）。
  - **数据与 Go 代码**：顶部展示最近一次 `nsgo check` 与 `nsgo test` 的结果及相对运行时间（`RunChangesChecks`，可通过 `Editor.ChangesCheckRunner` 注入），下方展示带增删着色的逐行统一差异（`git diff HEAD -- <path>`，未跟踪文件则显示完整新增行）以及 **Open in editor** 按钮（`OpenChangedFileInEditor`）。
  - **音频资源**：提供可播放的试听卡片（`AuditionChangedAudio`、`StopChangedAudio`），展示时长、格式/采样率、60 条波形柱以及来自项目引用索引和场景对象的入向引用（`Used by ...`）。
- **选择性提交卡片与历史里程碑**：结合已勾选文件、审查与试玩上下文（`SetChangesContext`）以及检查结果，通过 `Editor.CommitMessageDrafter` 或确定性的 `DefaultCommitDraft` 起草提交摘要与描述，支持 **Redraft**（`RedraftCommitMessage`）、可编辑摘要与描述输入框（`SetCommitSummary`、`SetCommitDescription`）、`"Made from N agent requests, reviewed and kept"` 归属行、已勾选文件与增删行数统计、**Commit**（`CommitSelectedChanges`）、**Commit and push**（`CommitAndPushSelectedChanges`）以及 **Push N commits**（`PushCommits`）。提交时**仅**暂存并提交已勾选文件（`git add -A -- <included>` + `git commit --only -m ... -- <included>`），未勾选文件始终保留为未提交状态；提交后在 `Editor.History`（`History.Milestones` / `CommitMilestones`）中记为一个里程碑。未经用户点击按钮，编辑器绝不会自动提交或推送。
- **SHARE**：提供 **Copy as PR description**（`CopyPRDescription` / `BuildPRDescription`，经由 `core.Scope.SetClipboard` 或 `Editor.ChangesClipboard` 复制包含摘要、变更文件、检查结果与试玩数据的结构化 Markdown）和 **Open pull request**（`OpenPullRequest`，当 `PATH` 中存在 `gh` 或注入 `Editor.GHRunner` 时启用）。

## 逐对象场景合并冲突解决（#1059、#1531）

当 Git 存在未合并的场景文件时——无论是 `git merge`、`git rebase`、`git pull --rebase`、`git cherry-pick`、`git revert` 还是 `git stash pop` 所产生（既支持 #1058 的逐对象合并驱动 `go tool game merge` 输出的冲突报告，也支持 Git 标准文本冲突标记）——打开项目（`OpenProject`）、点击状态栏中的冲突徽章或执行 `Project > Resolve Scene Merge Conflicts`（`CmdMergeConflicts` / `OpenMergeConflict`）都会进入全窗口的**场景合并冲突**界面（`MergeConflictView`，严格采用 `design/editor/Spec.dc.html` 令牌并对应 `VersionControl.dc.html` 第 2 屏）：

- **Git 操作识别与阶段映射**：根据 `MERGE_HEAD`、`REBASE_HEAD`（或 `.git/rebase-merge` / `rebase-apply`）、`CHERRY_PICK_HEAD`、`REVERT_HEAD` 以及无 `*_HEAD` 文件时的未合并索引条目（`git stash pop` / `apply`）自动识别当前操作。在 `rebase` 或 `pull --rebase` 期间会自动对调索引阶段 `:2:` 与 `:3:`，使作者自己的提交（`:3:`）显示为 **Mine (commit being replayed)**，上游目标（`:2:`）显示为 **Upstream**；`cherry-pick` 与 `revert` 则标明被拣选或还原的提交号。
- **顶部栏与进度**：居中按操作展示进度（`Merging <theirs> into <mine>`、`Rebasing <mine> onto <upstream>`、`Cherry-picking <sha> onto <mine>`、`Reverting <sha> on <mine>` 或 `Applying stash@{0} onto <mine>` 及 `N of M objects need you`），右侧提供对应的中止按钮（`AbortMerge`，按操作分别执行 `git merge --abort`、`git rebase --abort`、`git cherry-pick --abort`、`git revert --abort` 或对储藏冲突执行 `git reset --merge`）和完成按钮（`FinishMerge`，全部冲突解决后可用）。
- **CONFLICTS 与 MERGED BY ID**：按对象 ID 将 `sceneformat.MergeResult.Conflicts` 分组列在 **CONFLICTS** 下，每行显示状态点、对象名、`Unresolved` / `Agent` / `Mine` / `Theirs`（或 `Upstream` / `Picked` / `Revert` / `Stash`）/ `Custom` 标签及直观摘要（如 `Both moved it: x 560 here, x 600 on main` 或 `Your speed change; main deleted it`）。自动按 ID 合并成功且无冲突的对象列在 **MERGED BY ID** 中并标明来源侧。底部提供一键 **Take the agent's suggestion for all N**（`TakeAllAgentMergeSuggestions`）。
- **MINE、THEIRS 与 RESULT 场景预览**：上方并排展示可点击的 **MINE**（变基时显示 **MINE (COMMIT BEING REPLAYED)**，含分支、作者与短提交号）和 **THEIRS**（按操作显示 **UPSTREAM** / **PICKED COMMIT** / **REVERT** / **STASH**）2D 场景画布，下方展示实时 **RESULT** 画布。未解决的冲突对象在 **RESULT** 中同时以琥珀色虚线轮廓标出双方位置；选择任一方案后 **RESULT** 立即更新。
- **Agent 建议、Mine、Theirs 与手动编辑 YAML**：右侧检查器为当前聚焦对象展示三张单选卡片——**Agent's suggestion**（由 `Editor.MergeSuggester` 或确定性的 `DefaultMergeSuggestion` 生成，可合并双方不冲突的字段或说明为何保留删除/较高 `z` 值）、**Mine** 与 **Theirs**（按操作命名），每张卡片展示对应 YAML 片段与建议理由。点击 **Edit the result YAML by hand**（`SetMergeCustomEditing`、`ApplyMergeCustomYAML`）可直接编辑结果 YAML，并通过 `KnownFields(true)` 与 `sceneformat.Validate` 校验后生效。
- **校验与 Git 收尾（`FinishMerge`）**：以规范的 `sceneformat.Encode` 写出解决后的场景，运行 `game check`（`runProjectCheck` 或 `Editor.MergeCheckRunner`）以及 10 秒确定性短试玩（`sceneformat.Build` + `Update` + `PhysicsScene.Step` 或 `Editor.MergePlaytestRunner`）；任一检查失败都会阻止完成并展示错误，全部通过后执行 `git add` 并按操作收尾（合并执行 `git commit --no-edit`，变基执行 `git rebase --continue`，拣选执行 `git cherry-pick --continue`，还原执行 `git revert --continue`，储藏冲突仅暂存而不创建提交），随后将解决后的场景重新载入编辑器。
- **MCP 工具（`editormcp`）**：`merge_status`、`merge_resolve`、`merge_finish` 和 `merge_abort` 会返回当前 `operation`，供编程代理检查冲突对象、补充或更新建议与理由、选择方案或提交自定义 YAML，以及完成或中止合并、变基、拣选、还原或储藏冲突。

## 查找用法与安全重命名（#1041）

按下 `Shift+F12`（`CmdFindUsages`）、从菜单栏或命令面板执行 **Project > 查找用法**、在文件浏览器右键文件（**查找用法** / **安全重命名…**）、在场景树右键实例化场景或预制体对象（**查找用法**），或调用 `OpenUsagesScreen(path)`，即可打开全窗口的**查找用法与安全重命名**界面（`ReferencesScreenView`，严格采用 `design/editor/Spec.dc.html` 令牌并对应 `References.dc.html`）：

- **分组用法列表（`FindUsages`）**：顶部栏显示返回编辑器按钮（`Esc`）、`"Usages of <file>"` 与 `"N usages in M files"`，左侧按 **SCENES**（每个场景实例的坐标与覆盖数）、**PREFAB VARIANTS**（`inherits:` 继承目标的文件）、**DATA TABLES**（引用该路径的数据表行）与 **GO CODE**（通过 `go/parser` + `go/ast` 解析项目 `*.go` 文件找到的字符串常量/字面量，标注 `not rewritten`）四类分组。支持 `↑`/`↓` 遍历用法、点击场景卡片在缩略图网格上聚焦该场景实例，以及按 `Enter` 打开场景并选中、聚焦对应对象。
- **安全重命名预览与审查（`PreviewSafeRename` / `StartSafeRename` / `SetSafeRenameInput`）**：点击 **Rename…**（`F2`）后在固定的目录前缀与复合后缀之间显示 **NEW NAME** 输入框，按 `game mv` / `MoveFile` 路径规则实时校验（非空、合法相对路径、目标空闲），在不写入任何磁盘文件的前提下计算干跑计划。审查界面展示被移动的文件（`"content unchanged"`）、每个会被改写的 YAML 文件精确的 `−`（旧行）与 `+`（新行）差异并显示实例覆盖保持不变，同时将未改写的 Go 引用列为问题（`"<file> loads the old path and is not rewritten"`），提供 **Ask the agent to update it**（`AskAgentToUpdateUsage`，触发 `Editor.OnAskAgentUpdateRef`）与 **Open `<file>`**（`Editor.OnOpenSourceFile`）。
- **单步保留与撤销（`ApplySafeRename` / `UndoSafeRename`）**：点击 **Keep · Rename N files**（`Mod+Enter`）仅通过 `MoveFile` 改写 YAML 并记录为 `Editor.History` 中的单个撤销步骤，完成后提供 **Undo · ⌘Z** 与 **Back to `<scene>`**。一次撤销即可按字节还原磁盘上全部移动与改写的文件以及所有打开的场景标签页。
- **MCP 工具（`editormcp`）**：`refs_find` 返回跨场景、预制体变体、数据表与 Go 代码的分组用法（并可选择打开界面），`refactor_rename` 在编辑器中准备同样的安全重命名审查及精确行差异与 Go 未改写警告，除非显式传入 `apply: true`，否则在用户确认保留前不会改写磁盘文件。

## 材质编辑器（#896）

在编辑器中直接为对象指定并编辑共享的 `*.material.yaml` 材质资源文件（见 [resource](resources.md)），支持实时预览与完整的撤销/重做：

- **指定与新建材质**：检查器中的可搜索 **Material** 下拉框（`SetObjectMaterial`）从当前项目的 `*.material.yaml` 文件中选择材质；点击 **New Material**（`CreateObjectMaterial` / `CreateMaterialFile`）会以 `resource.Encode` 的规范形式新建 `.material.yaml` 文件、将其指定给选中对象并在面板中打开；点击 **Edit Material**（`OpenMaterial` / `OpenMaterialProjectFile`）、在文件浏览器中双击材质文件或点击 **Open** / **Edit Material**、在命令面板中选择材质文件（`OpenProjectFile`）或执行 `Project > Material Editor`（`CmdMaterialEditor`）均可打开停靠在视口下方的 `MaterialEditorPanelView`。
- **全字段编辑**：支持编辑 `name`（`SetMaterialName`），使用 NSGo `ColorWell` / `ColorPicker` 拾色器与 CSS 颜色输入框编辑 `baseColor` 和 `emissive`（`SetMaterialBaseColor`、`SetMaterialEmissive`），通过可搜索的项目图片选择器编辑 `texture`（`SetMaterialTexture`、`ProjectTextures`），通过带范围约束的滑块与数值框编辑 `metallic`、`roughness` 和 `opacity`（`SetMaterialMetallic`、`SetMaterialRoughness`、`SetMaterialOpacity`），从 `resource.BlendModes()` 中选择 `blend`（`SetMaterialBlend`），以及开关 `doubleSided` 和 `unlit`（`SetMaterialDoubleSided`、`SetMaterialUnlit`）。任何不合法的值都会在改动状态或历史前被 `resource.Encode` 拒绝。
- **实时预览与视口联动**：面板提供实时预览（`MaterialPreviewImage`、`SetMaterialPreviewShape`），支持使用 `render3d` 渲染的 3D 球体（**Sphere**）与立方体（**Cube**，反映 `unlit`、PBR 金属度/粗糙度、自发光、贴图、透明度与双面渲染）以及 2D 精灵（**Sprite**）预览（2D 视图默认 Sprite，3D 视图默认 Sphere）。每次编辑（包括拾色器拖拽及撤销/重做）都会经由 `resource.Library.Reload`（通过 `project.Resolver.Library()` 与 `scenemodel.Assets.Library()`）实时应用，无需保存即可立刻刷新预览以及视口中所有引用该材质的 2D 精灵（`BuiltScene.SyncMaterials`）与 3D 对象。
- **撤销/重做与确定性保存**：所有材质编辑均记录在 `Editor.History`（`EditMaterial`）中，纳入未保存检测（`MaterialDirty`、`HasUnsavedChanges`）与文件移动跟踪（`MoveFile`）；点击 **Save Material**（`SaveMaterial`、`SaveMaterialFile`、`Save`、`SaveAll` 或 `Editor.OnSaveMaterial`）按 `resource.Encode` 写出确定性的规范 YAML。
## 试玩报告界面（#1044）

通过 `Project > 试玩报告`、命令面板（`mod+shift+y` / `CmdPlaytestReport`）、状态栏试玩徽章、在文件浏览器中双击任意 `*.report.yaml` 文件、调用 `OpenPlaytestReport` / `OpenPlaytestReportFile` 或代理调用 `playtest_report`，可打开全窗口的**试玩报告**界面（`PlaytestReportView`）。界面严格遵循 `design/editor/Spec.dc.html` 颜色与对比度令牌（包括 `#3B7BFF` 主强调按钮上的 `#101216` 文字）并对应 `design/editor/Playtest.dc.html`：

- **顶部栏与回放联动**：展示 `"Playtest · <scope>"`、项目名与运行摘要（如 `"20 headless runs by the agent · 1 m 40 s"`）、**Watch run N**（`WatchPlaytestRun` / `WatchPlaytestRunIn`，通过 `replay.LoadFile` 加载该轮次的 `.replay.yaml` 录制并经 `PlayReplayIn` 在播放模式中以 `*replay.Player` 回放，同时提供 **Back to Playtest** 返回条）以及 **Ask the agent to tune it**（`AskAgentToTunePlaytest` / `Editor.OnPlaytestPrompt`）。
- **BOT PROFILE 卡片与 `playtest_define` 审查**：展示机器人配置名称/切换组（`BotProfileDisplayName`）、带 **Open** 按钮的 `"playtest.yaml from template <id>"`（`OpenPlaytestConfigPreview` / `Editor.OnOpenPlaytestConfig`）以及一句话策略。当代理通过 `playtest_define` 提议新指标时，界面展示带 **Review** 按钮的紫色横幅，点击后展开统一 diff 审查抽屉，确认 **Apply** 前不会改动磁盘上的 `playtest.yaml`（`ApplyPlaytestProposal`、`DismissPlaytestProposal`）。
- **指标卡片**：每张卡片包含标签、带图标与文字的 **Met** / **Missed** 徽章（不依赖纯色区分）、格式化数值、辅助说明行，以及 `"Goal · <goal>"` 与等宽来源文件名（`brief.yaml`、`game.yaml` 或 `playtest.yaml`）。
- **按指标自动选择图表（全部 4 种画布图表）**：`SelectPlaytestChart` 与 `DrawPlaytestChart` 按报告内容自动选择并绘制一种图表：
  1. **关卡热力图**（`PlaytestChartHeatMap`）：展示死亡或每格停留时间热区聚簇与标注胶囊。
  2. **按波次柱状图与目标虚线**（`PlaytestChartBars`）：对比目标上限并标记超标波次。
  3. **按关卡折线图**（`PlaytestChartLine`）：展示关卡难度/解法曲线并标出异常峰谷。
  4. **胜率矩阵**（`PlaytestChartMatrix`）：对超过阈值（默认 60%）的单元格以边框、标记点及 `"above 60%"` 文字标出。
- **Agent 解读、可执行建议与脚注**：展示数句诊断结论（`DefaultPlaytestAgentRead` 或 `SetPlaytestAgentRead`）与可点击的调优建议卡片（`RequestPlaytestSuggestion`，点击后作为审查请求发送给代理），底部附注说明所有试玩均基于回放系统，指标与机器人均来自 `playtest.yaml`。
- **MCP 集成（`editormcp`）**：`game_playtest` 会将生成的报告同步载入编辑器，`playtest_define` 会将待审查指标直接推送到试玩报告横幅并与 Apply/Dismiss 双向同步，新增的 `playtest_report` 工具支持代理打开、检查、写入解读与建议或回放指定轮次。
## 资源抽屉（#1052）

在左侧栏顶部可在 **Scene**（`LeftRailScene`）与 **Assets**（`LeftRailAssets`，也可通过命令面板 `CmdToggleAssetsMode` 切换）之间切换，在视口下方统一浏览、筛选、预览并将各类项目资源直接拖入场景；界面严格使用 `design/editor/Spec.dc.html` 设计 Token 并对应 `design/editor/Assets.dc.html`（第 1 屏）布局：

- **左侧栏（`AssetsLeftRailView`）**：顶部提供 **Scene / Assets** 分段开关（`LeftRailSwitchView`），下方列出带资源计数的项目 **FOLDERS**（包含如 `audio/music` 等空资源目录）、带计数的 **SHOW ONLY** 过滤器（**Unused**、**Missing files**、**Generated**、**Proposed**）以及底部文件拖放导入区（`"Drop files anywhere to import"`）。其中 **Unused** 与 **Missing files** 直接由 `refs.Project.UnreferencedAssets` 和 `refs.Project.MissingAssets` 计算得出。
- **视口下方的资源抽屉（`AssetDrawerView`）**：提供类型标签页（**All**、**Sprites**、**Tiles**、**Audio**、**Fonts**）、按名称或路径筛选输入框（`SetAssetDrawerQuery`）、默认按最近修改排序的排序切换（`SetAssetDrawerSort`）以及 **Import**（`TriggerAssetImport` / `ImportAssetFiles`）和 **Generate**（`TriggerAssetGenerate`）按钮。每张卡片显示透明棋盘格预览、状态徽章（`new`、`Missing`、`Generated`、`Not used`）、文件名和元信息行（如 `"6 frames · used 31×"`）。底栏显示 `"N assets · M shown"` 及缺失文件提示（如 `"1 problem · door_open.png missing"`）；空目录显示 `"audio/music is empty · Drop .ogg or .wav files here, or ask the agent."`，筛选无结果时显示 `"Clear a filter to see more assets."`。
- **拖拽到视口（吸附放置或替换精灵）**：将精灵卡片拖到 2D 视口空白处时会显示网格吸附虚线预览与状态胶囊（`"coin.png new Sprite on Actors · snapped to 16 px"`），松手后在活动图层上以**单个撤销步骤**放置新精灵对象（`DropAssetAt`）；拖放到已有对象上则会以**单个撤销步骤**替换该对象的精灵，按一次 **Undo** 即可还原旧精灵。按 **Escape**（`CancelAssetDrag`）可干净取消拖拽，不改动场景。
- **右侧选中资源面板（`AssetInspectorView`）**：在棋盘格预览框中展示尺寸与格式元信息，为多帧精灵提供逐帧可播放的片段预览控件（`SetAssetPreviewPlaying`、`SetAssetPreviewClip`、`SetAssetPreviewFrame`、`StepAssetPreviewFrame`），为音频提供波形图与可播放试听控件（`ToggleAssetAudioPreview`、`StopAssetAudioPreview`），下方展示来自 `InspectAssetImport` 的紧凑 **IMPORT** 摘要以及直接来自 `refs.Project.UsedBy` 的 **USED BY** 引用列表（点击引用条目可跳转打开对应场景或文件）。未被任何文件引用的资源会提示 `"Nothing references it. The agent can propose deleting it; deleting files always asks you."`，缺失资源则提供 **Replace with a file**（`TriggerReplaceMissingAsset` / `ReplaceMissingAsset`）与 **Ask the agent**（`AskAgentAboutAsset`）操作。

## 视口叠加层菜单与智能体视口截图（#1040）

在 2D 编辑模式（`ViewMode2D`）下，视口底部居中的悬浮栏（对应 `design/editor/Inspector.dc.html`，严格采用 `Spec.dc.html` 令牌）将缩放（`−` / `%` / `+`）、**吸附（Snap）**、**适应画面（Fit）** 与显示当前已开启数量的 **叠加层 N（Overlays N）** 按钮组合在一起：

- **`SHOW IN VIEWPORT` / `在视口中显示` 弹出层（`ui.Popover`）**：点击 **Overlays N** 在上方弹出（也可经由 `View > Overlays` 或 `CmdViewportOverlays` 访问），每项叠加层均显示图例色标、本地化名称、当前场景实时数量与 `ui.Switch` 开关，并在底部注明 *“智能体的视口截图使用同一组设置：`viewport_overlays`”*：
  - **碰撞形状**（`collision_shapes`）：已注册的非触发器组件形状字段（#882）与非触发器 2D 物理碰撞体。
  - **触发器**（`triggers`）：触发器/传感器/区域组件形状与 2D 物理触发区域（琥珀色虚线框）。
  - **连接线**（`connections`）：从场景 `connections` 中发送事件的对象指向处理对象的弧形箭头，并标注 `"event → handler"`（如 `"pulled → open"`）。
  - **巡逻路径**（`patrol_paths`）：敌人或角色的巡逻范围与路径点，通过与插件共享的 `ViewportOverlay` 注册机制实现。
  - **导航区域**（`navigation`）：构建好的 2D 导航网格多边形、通道与 `navregion2d` 轮廓（#888），与 `ShowNavMesh2D` 保持同步。
  - **相机边界**（`camera_bounds`）：场景相机边界框与 `"bounds W × H"` 尺寸标注（#884）。
  - **标签**（`labels`）：可见对象下方的名称/ID 胶囊标签。
- **插件自定义叠加层（`ViewportOverlay`）**：插件可通过 `Plugin.Overlays`（或直接在编辑器上调用 `Editor.RegisterViewportOverlay`）注册带 `ID`、`Name`/`NameKey`、可选 `Count` 计数回调与 `Draw func(*Editor, *core.Drawing, *sceneformat.BuiltScene)` 绘制函数的自定义叠加层。启用的插件叠加层会自动出现在弹出层、`View > Overlays` 菜单以及 `editormcp` 的 `viewport_image` 工具中。
- **用户级持久化**：切换任意叠加层（`SetViewportOverlay`、`ToggleViewportOverlay`、`SetViewportOverlays`）都会把 `viewportOverlays` 写入项目之外的 `Editor.SettingsPath`（`EditorSettings` YAML），绝不改动任何项目或场景文件，重启后保持不变（全部关闭时显式写为 `[]`）。
- **与 `editormcp` 的 `viewport_image` 共用**：`Editor.DrawViewportWithOverlays(d, size, overlayIDs)` 将完全一致的叠加层渲染进 `viewport_image`。省略 `viewport_overlays` 时默认使用编辑器当前已开启的叠加层；传入名称列表（或 `[]`）时仅覆盖本次截图而不修改编辑器设置。

## 分节检查器、字段筛选、拖拽微调、打开 YAML 与请智能体添加（#1038）

对象检查器（`InspectorView`）采用 `design/editor/Spec.dc.html` 设计令牌并对应 `Inspector.dc.html`、`Main.dc.html` 与 `Viewport3D.dc.html`，将对象属性组织为紧凑的可折叠标题分区，同时保持每个控件都对应一个 `Editor` 方法与匹配的 `editormcp` 工具：

- **头部与打开 YAML（Open YAML）**：展示按对象类型着色的色块、对象 ID、类型与图层副标题（如 `Sprite · layer actors`）以及 **Open YAML** 按钮（`FindObjectYAMLLine` / `OpenSelectedObjectYAML`），精确定位对象在场景 YAML 中的 1-based 行号（场景无未保存改动时直接读取磁盘文件以匹配注释与行号，存在未保存改动时匹配 `SaveYAML`），若设置了 `Editor.OnOpenFileAtLine(path, line)` 则直接调用并在状态栏报告 `Opened <path>:<line>`。
- **字段筛选（`InspectorFilter` / `SetInspectorFilter`）**：顶部的 **Filter fields** 输入框按不区分大小写的子串匹配筛选字段标签、组件名与分区标题（例如输入 `speed` 仅保留匹配 `speed` 的字段与所在分区），筛选期间自动展开匹配分区并隐藏无关分区。
- **可折叠标题分区与紧凑变换行**：分区标题遵循 Spec 规范样式（大写标题、折叠箭头 `ToggleInspectorSection` 与字段计数徽章，如 `TRANSFORM`、`SPRITE`、`PATROL`、`HEALTH`）；位置与缩放采用紧凑的 `X Y` 行布局（3D 视图含 `Z` 轴以及 3D 旋转 `X Y Z`），并包含透明度与 Z 顺序。
- **可拖拽微调的数值字段**：拖拽任意数值字段标签（`BeginInspectorScrub`、`UpdateInspectorScrub`、`EndInspectorScrub`、`CancelInspectorScrub`）可在视口实时预览并记录为**单步撤销**；拖拽期间按 `Escape` 会立即取消并还原原值且不写入历史。在 3D 视图中按住 `Shift` 拖拽以 `0.01` 精细步进。聚焦输入框支持 `↑`/`↓` 步进、`Shift` ×10 步进、`Enter` 提交与 `Escape` 还原。
- **分组“添加组件”与“请智能体添加”**：对象尚未挂载的已注册组件按 `GAMEPLAY`、`PHYSICS` 与 `AUDIO AND VISUAL` 分组展示（`ComponentCategoryOf`、`AddableComponentGroups`）；输入框旁的 **Ask to add**（`AskToAddComponent`、`ComposerDraft`、`SetComposerDraft`、`Editor.OnComposerPrompt`）会在智能体输入框预填 `"Add a component to <object> that "`。
- **审查提示、问题胶囊与页脚**：处于智能体审查中的字段（`SetInspectorReviewChanges`）会显示琥珀色左侧指示条与 `"was <old> · in review"`；未通过项目检查或范围校验的字段（`SetInspectorFieldProblems` / `Editor.Problems`）会在控件下方与分区标题显示红色问题胶囊；底部页脚提示 `"Everything else about this object is in its YAML. Ask the agent, or open the file."`。
- **配套 MCP 工具（`editormcp`）**：新增 `object_set_field`（复用 `Editor` 命令以单步撤销修改任意对象字段、组件字段或自定义属性）与 `object_locate_yaml`（返回对象的 1-based YAML 行号与代码片段，可选触发 `OnOpenFileAtLine`），并补齐 `scene_edit` `set` 对 `particles`、`model`、`gridMap`、`occluder`、`light` 与 `camera` 的全量支持。

## 通过 MCP 生成器服务器生成资源（#1053）

通过 `Project > 生成资源` 或命令面板（`CmdGeneratedAssets` / `SetAssetGeneratorOpen`）可打开全窗口的**生成资源**界面（`AssetGeneratorView`）。界面严格遵循 `design/editor/Spec.dc.html` 令牌并对应 `design/editor/Assets.dc.html` 第 2 屏，用于连接外部 MCP 图像与音效生成器服务器、依据 `brief.yaml` 校验候选结果，并在用户明确选定前绝不向磁盘写入任何文件：

- **MCP 生成器服务器（`GENERATORS · MCP`）**：在 **设置 › 工具**（`EditorSettingsDialogView`）中配置服务器（`GeneratorServerConfig`、`SetGeneratorServerConfigs`），通过 `ConnectAssetGenerator`（任意 `mcp.Transport`，与 `ConnectRemote` 一致）或 `ConnectAssetGeneratorCommand`（桌面端 stdio 进程）连接。`AssetGenerators` 列出所有已连接或已配置的 `images` / `sound` 服务器及实时连接状态。
- **不信任输出仅在内存中解码**：`GenerateAsset` 严格在内存中将图像负载解码为光栅像素（重新编码为干净的 PNG 字节、限制尺寸并拒绝 SVG/HTML/非图像负载），并在内存中校验 WAV（`audioconv.DecodeWAV`）或 OGG 音频。在用户执行 **Pick** 之前不会向磁盘写入任何内容。
- **背景预览、`brief.yaml` 风格检查与音效试听**：图像候选与参考资源并排显示在可切换的背景上（`PREVIEW ON`：Floor 3、Floor 1、Checker，`SetAssetGenPreviewBackground`），逐项展示**调色板色数（Palette）**、**像素网格对齐（Pixel grid）**、**相对参考资源的尺寸（Size）**以及**相对关卡墙面的对比度（Contrast）**通过/失败结论与原因（`LoadAssetStyleBrief`、`SetAssetStyleBrief`）。音效候选展示波形、时长、RMS 响度（`dBFS`）、峰值与响度检查结果，并支持内存试听（`PlayGeneratedSoundCandidate`、`StopGeneratedSoundCandidate`）。
- **单步原子可撤销的选定与来源旁注文件（`<file>.source.yaml`）**：点击 **Pick**（`PickGeneratedCandidate`）在 `Editor.History` 的**单个可撤销步骤**中原子写出选定的媒体文件及其确定性的版本 1 `<file>.source.yaml` 旁注文件（`importsettings.AssetSource`、`AssetSourceVersion = 1`、Schema `https://game.nsgo.dev/schemas/source.v1.json`），记录 `version`、`generator`、`model`、`prompt`、`seed`、`references`、`license`、`commercialUse` 与 `approvedBy`；执行一次 `Editor.Undo()` 即可同时移除两个文件。**Vary this**（`VaryGeneratedCandidate`）、**Refine**（`RefineGeneratedAsset`）与 **Regenerate N**（`RegenerateAsset`）使用相同参考重新调用生成器，同样不写磁盘。
- **导出许可证门禁（`LICENSES · CHECKED AT EXPORT`）**：`credits.ScanGeneratedLicenses`、`credits.CheckGeneratedLicenses`、`credits.Collect`、`releaseplan.Build`、`game release` 以及编辑器的导出面板（`CheckExportCredits`）会扫描项目中的所有 `*.source.yaml` 文件。任何许可证为空或未知（`license: ""` 或 `license: unknown`）的生成资源都会以 `*credits.UnknownLicenseError` 阻止发布构建并在错误信息中列出其路径，直到通过 `SetGeneratedAssetLicense` / **Set license** 设置合法许可证为止。
## 焦点环与 F6 键盘区域（#1003）

编辑器中的每个交互控件都遵循 `design/editor/Spec.dc.html` 规范，在键盘聚焦时绘制焦点环，并可通过 `Tab`、`F6` 或方向键访问：

- **仅键盘焦点环（`:focus-visible`）**：`EditorView` 安装 `theme.Core()`（`FocusTheme()` 亦返回同一主题），将 `info`、`focus` 与 `ring` 映射为规范强调色（深色 `#3B7BFF`、浅色 `#1D5CE6`）并保持深浅两套外观表面。按钮、切换按钮、下拉选择、复选框以及 `SpecIconButton` / `SpecSwitch` 仅在键盘聚焦时绘制 2 px、`+2 px` 偏移的强调色焦点环，鼠标点击不会绘制焦点环。
- **内嵌焦点环与 `:focus-within`**：树节点行（`SceneTreeView`、`LayersView`）、列表行（`AssetBrowserView`、`FilesView`）、文本与数值输入框（`ui.Input`、`ui.NumberInput`）以及通过 `F6` 聚焦的整个区域均通过 `theme.Palette.FocusRing(-2)` 绘制 `-2 px` 内嵌偏移的强调色焦点环，确保滚动父容器不会裁剪它。同时被选中和聚焦的行会同时显示选中背景填充与内嵌焦点环；输入框/卡片容器（`SpecFocusWithinCard`、`hud-composer-card`）则通过 `:focus-within` 在卡片外圈绘制 `+2 px` 焦点环。
- **固定顺序的键盘区域（`F6` / `Shift-F6`）**：`F6` 移至下一个可见区域，`Shift-F6` 返回上一个可见区域，严格遵循规范顺序：**Header 顶栏**（`RegionHeader`）→ **Scene tree 左侧栏**（`RegionSceneTree`）→ **Viewport 视口**（`RegionViewport`）→ **Agent 与 Inspector 右侧栏**（`RegionInspector`）→ **Status bar 状态栏**（`RegionStatusBar`）→ **Toast 提示**（`RegionToast`，仅在 `ToastView` 通过 `Editor.ShowToast` 显示提示时参与循环）。每次进入区域都会绘制该区域的 `-2 px` 内嵌焦点环，并恢复该区域上一次聚焦的控件；在快捷键录制及全窗口页面期间 `F6` / `Shift-F6` 会自动暂停，并在切换区域时关闭打开的对象右键菜单。
- **视口内焦点即选中对象**：视口画布本身不绘制焦点环；当焦点位于视口（`editor-viewport-frame`）内时，`Tab`（`Editor.SelectNextObject`）与 `Shift-Tab`（`Editor.SelectPreviousObject`）按场景顺序循环选中下一个/上一个可见且未锁定的对象。
- **对话框焦点限制与关闭恢复**：所有编辑器对话框（`EditorSettingsDialogView`、`ShortcutsSheetView`、`CommandPaletteView`、未保存提示、场景路径对话框与保存冲突对话框）会将 `Tab` / `Shift-Tab` 限制在最顶层模态框内（同时不干扰 `Settings › Shortcuts` 的按键录制），在对话框打开期间屏蔽全局快捷键（除用于停止智能体的 `⌘.` / `Ctrl+.`，即 `Editor.OnStopAgent` 外），并在按 `Esc` 取消或点击操作按钮关闭后将焦点精确恢复到打开该对话框的控件。
- **焦点稳定性**：在拖拽或检查器数值微调进行中（`Editor.IsDragging`）或用户正在文本字段中输入时（`Editor.IsTyping`），编辑器不会自动抢占焦点；弹出 Toast 不会抢走焦点；`Editor.ShouldFocusAgentPrompt` 仅在未拖拽、未输入且焦点原本就位于智能体面板内（`Editor.SetAgentPanelRef`）时才返回 `true`。
## 连接智能体与状态胶囊（#1009）

在编辑器内或通过 `go tool game mcp install` 连接本机的外部编程智能体（Claude Code、Codex 或任意 MCP 客户端），界面严格遵循 `design/editor/Spec.dc.html` 设计 Token 并对应 `design/editor/Connect.dc.html`、`design/editor/AgentStates.dc.html` 与 `design/editor/Settings.dc.html`：

- **一次性项目安装（`go tool game mcp install`）**：在项目根目录幂等地写入 `.mcp.json`（Claude Code）和 `.codex/config.toml`（Codex），保留文件中已有的其他 MCP 服务器与设置，并打印每个文件是写入还是未变更。
- **连接智能体对话框（`ConnectAgentDialogView`）**：可通过 `项目 > 连接智能体…`、命令面板（`CmdConnectAgent`）、`编辑器设置 > 模型与连接`（`+ 连接其他智能体或模型`）或智能体面板顶部栏打开。提供三个选项（**本机上的智能体** / 推荐、**内置，使用你的 API 密钥**、**不使用智能体**），展示带 **Copy** 按钮的 `go tool game mcp install` 命令与等待连接状态（首个 MCP 客户端接入后立即切换为绿色已连接状态），提供四项起始权限复选框（编辑场景与数据、运行 `game check`、运行无头试玩、编辑 Go 代码 -> `AgentPermissionRules`），选择 **不使用智能体** 时保持智能体面板关闭，并提供 **Later** 与 **Done** 按钮。
- **智能体标签页面板（`AgentPanelView`）**：在右侧栏的 **检视器**（`RightTabInspector`）与 **智能体**（`RightTabAgent`、`CmdAgentPanel`）之间切换，展示请求对话流（包括终端启动的 MCP 客户端发起的外部回合）、实时流式步骤以及带标签的多行输入框（`Enter` 发送、`Shift+Enter` 换行，并提供 **Send** / **Stop** 按钮）。输入框请求经由 `Editor.AgentRunner` 分发（桌面端默认为 `CLIAgentRunner`，在项目目录中以非交互流式 JSON 模式调用 `claude --print --output-format=stream-json --verbose` 或 `codex exec --json` 并接入项目 MCP 配置）。
- **状态胶囊（`AgentStatusChipView`）**：通过 `core.LiveRegionPolite` 播报状态变化，展示全部五种状态，且手动编辑工具在任何状态下均可正常使用：
  - `Connected · MCP`（悬停提示智能体名称与传输方式，例如 `Claude Code over stdio`）
  - `Working · step n of m`（紫色圆点以 `1.2 s` 周期脉动，开启减弱动态效果时保持静止）
  - `Waiting for you · m:ss`（点击聚焦当前待处理请求卡片）
  - `Reconnecting · try n of 5`（按 `1、2、4、8、16 s` 指数退避重试，可通过 `Editor.AgentScheduleAfter` 注入调度器以确定性测试）
  - `Disconnected` 及 **Reconnect** 与 **Troubleshoot** 按钮（点击 **Troubleshoot** 直接在失败的步骤重新打开连接对话框，而非回到起始步）
  - 离线时显示 `Disconnected · offline`。

## 运行模式标记、滚动回放与冻结帧智能体提问（#1042）

在运行模式（`Play`、`PlayIn`、`StepPlay`、`PlayTick`）或连接运行中游戏期间，编辑器会持续保留最近 60 秒的滚动输入回放，支持在不中断运行的情况下打标记、冻结任意帧向智能体提问，并在输出面板提供崩溃堆栈卡片与一键回放到崩溃前一帧（对应 `design/editor/PlayMode.dc.html`，严格采用 `Spec.dc.html` 设计令牌）：

- **最近 60 秒滚动输入录制**：每次固定步长更新都会将 `Editor.PlayInputSource`（监听 `Editor.PlayInputActions` / `Editor.PlayInputAxes`，默认读取 `game.yaml` 中的输入动作）采样到上限 60 秒的滑动窗口（`60 × PlayRateHz()`，60 Hz 下为 3,600 帧；`PlayRollingFrameCount`、`PlayRollingSummary` 显示 `"Last 60 s kept · N frames"`）。打标记、提问或捕获 panic 时，编辑器会原子写入 `replays/<场景>-<帧号>.replay.yaml` 版本 2 `replay` YAML 文件（`SavePlayRollingReplay`、`PlayLastReplayPath`），并在超过 60 秒窗口时平滑重基准帧号且保留进入窗口时的按住状态与轴值。
- **运行控制栏与标记（`alt+f` / `CmdPlayMark`）**：本地运行模式在主工具栏下方展示 Spec 风格的运行控制栏（`playModeBarView`，不增加 2D 工具栏按钮），包含运行/暂停状态徽章、暂停/继续（`mod+shift+p`、`SetPlayPaused`）、单帧步进（暂停时按 `.`、`StepPlay`）、时间倍率（`0.25×`–`2×`、`SetPlayTimeScale`）、**Mark `⌥F`**（`AddPlayMark`）、**Something looks wrong `⌘L`** 与 **Stop**。按下 `alt+f` 不会停止运行，会保存对应的 `.replay.yaml`、在 60 秒帧耗时条上标注旗标，并在右侧 **Agent** 标签页的 **MARKS · N** 列表中展示时间、帧号与 `"replay attached"`（`PlayInspectorTab`、`PlayMarks`）。停止运行（`Stop`）或提前点击 **Send marks now**（`SendPlayMarks`）会将全部未发送标记打包为**同一条线程**派发给 `Editor.OnPlayMarksThread`（并保存在 `LastPlayMarksThread` 中）。
- **冻结帧提问（`mod+l` / `CmdAskFrozenFrame`）与 MCP 回放步进**：按下 `mod+l` 或点击 **Something looks wrong**（`OpenAskFrozenFrame`）会立即暂停运行、截取当前视口 PNG 并打开 **"Ask about frame N"** 弹窗，展示冻结帧缩略图、附件标签、输入框（`"What looks wrong? e.g. the slime walks through the wall"`）、`"A fix comes back as a review"` 提示及 **Cancel** / **Ask** 按钮。提交（`SubmitAskFrozenFrame`）后会保存滚动回放、附上帧图像与实时实体状态快照、触发 `Editor.OnAskFrozenFrame` 并切到 **Agent** 标签页。智能体通过 `editormcp` 的 `play_frozen_context` 获取冻结帧上下文，并通过 `play_replay_step`（`AgentStepPlayReplay`）步进回放；视口显示智能体徽章（`"agent stepping frames A–B"`），Agent 标签页记录步进日志（`"Stepped frames A–B with game_loop_step…"`），且游戏保持暂停直至用户手动恢复。
- **输出面板崩溃卡片与回放到崩溃前**：`ParsePanicTrace` 会解析游戏运行输出或 Play 模式恢复的 Go panic 消息与调用栈（`OutputPanic`、`SetOutputPanic`）。`OutputPanelView` 以 Spec 风格卡片展示错误消息、可点击跳转到源码文件的游戏调用帧 `file:line` 链接、折叠的运行时调用帧（`"N frames in the game runtime · Show"` / `"Hide"` 切换，`SetOutputPanicRuntimeExpanded`）、**Ask the agent to fix**（`AskAgentToFixPanic`、`Editor.OnAskPanicFix`）以及 **Replay up to the panic**（`ReplayUpToPanic`、`ReplayUpToFrame`），后者重置场景（设置时调用 `Editor.OnPlayReset`）、经由 `PlayActiveInput()` 回放已保存录制，并在崩溃帧前一帧（`panicFrame - 1`）精确停住并暂停而不触发崩溃。

## 权限方案与 MCP 规则强制执行（#1010）

编辑器通过位于 `game.yaml` 旁的版本化 YAML 文件 `permissions.yaml` 控制所有修改类与运行类 MCP 工具调用（将 `.nsgo/` 保留为 Git 忽略的本机编辑器状态，把团队共享的权限规则纳入版本控制）：

- **三种预设方案**：
  - **谨慎（`cautious`）**：每次修改内容、配置、Go 代码、创建文件、运行 `game check` 或试玩前都会先询问，并拒绝删除文件与执行 shell 命令。
  - **平衡（`balanced`，`permissions.yaml` 不存在时的默认方案）**：自行编辑内容（`scenes/**`、`prefabs/**`、`data/**`）并运行 `game check` 与试玩；修改 `game.yaml`、`brief.yaml`、`**/*.go` 或在任意位置创建文件前先询问；拒绝删除任何文件或执行 shell 命令。
  - **自主（`autonomous`）**：端到端完成整个任务并在编辑器中保留可审查的撤销步骤；删除文件与 shell 命令仍默认拒绝，除非显式放宽。
- **首条匹配生效规则表**：每条规则将操作（`edit`、`create`、`delete`、`run`、`code`）与斜杠分隔的文件通配符（`scenes/**`、`**/*.go`、`**`）或运行目标（`game check`、`playtests`、`shell`）映射到决策（`allow`、`ask`、`deny`）。每次判定（`Editor.EvaluatePermission`）都会记录命中的规则序号或方案。
- **设置 › 权限（`PermissionsSettingsView`）**：可通过 `文件 > 权限…`、命令面板（`CmdPermissionsSettings`）或被拦截步骤的链接打开。页面包含三张方案卡片（修改规则后保留当前方案名并显示 **已修改** 标记）、可上下排序和增删的规则表、每行支持方向键切换的 **允许 / 询问 / 拒绝** 单选组，以及右侧的拦截步骤与待审批卡片栏。
- **拦截步骤跳转与“始终允许”提示条**：`deny` 规则绝不会调用底层 MCP 工具，并会记录被拦截步骤，点击其链接（如 `"Settings › Permissions · rule 7 of 7"`）会直接打开 **设置 › 权限** 并聚焦到对应规则行。在 `ask` 请求卡片上选择 **始终允许** 会在询问规则之前插入一条 `allow` 规则、保存到 `permissions.yaml`，并显示带 **查看（Show）** 和 **撤销（Undo）** 按钮的 `"Always permission rule saved"` 提示条。
- **仅作用于编辑器 MCP**：规则仅约束编辑器的 MCP 服务器工具；外部智能体直接写入磁盘的文件仍通过磁盘监视器提示。
## 使用 `⌘L` 就地向智能体提问与智能体面板（#1037）

在任意文本输入框之外按 `⌘L`（Windows/Linux 为 `Ctrl+L`，在 `ScopeGlobal` 中注册为 `CmdAskAgent` 并受 `core.KeyNotEditing` 保护）会在视口选中对象旁打开锚定就地提问框（`AskInPlaceComposerView`，严格遵循 `design/editor/Spec.dc.html` 设计令牌并对应 `AskInPlace.dc.html`）：

- **锚定定位与边界约束（`OpenAskInPlace`、`AskInPlacePlacement`、`ComputeAskInPlacePlacement`）**：默认锚定在选区视口包围盒下方（靠近底部时自动翻转到选区上方），并严格约束在视口容器内部，确保始终位于屏幕内且绝不遮挡右侧面板（检查器 / 智能体面板）。未选中任何对象时锚定在视口内询问当前打开的场景。按 `Esc`（`CloseAskInPlace`）会直接关闭而不发送，并将键盘焦点还原回打开前的控件。在检查器或任何文本框中输入时按 `⌘L` 不会误触发提问框。
- **可播报选区标签、多行输入与上下文建议**：顶部以 `RoleStatus` / `LiveRegionPolite` 标签播报选区数量与对象名称（例如 `"已选中 2 项 · slime_01, slime_02"`）；主体为带无障碍标签的真实多行输入框（`⇧↵` 插入换行，`↵` 或 `⌘↵` 发送）；下方提供最多两条上下文建议（如多选时的“将它们做成预制体变体”与“均匀排列它们”），可用方向键与 `↵` 选取且绝不阻塞自由输入；视口左下角显示本地化提示 `"⌘L 询问选中内容 · ⌘K 搜索 · 右键“向智能体询问此项”"`。
- **右键菜单联动**：视口右键菜单与场景树右键菜单中的**向智能体询问此项**（`AskAgentAboutObject`）都会打开同一个锚定就地提问框（目标对象属于多选时自动带入整个选区）。
- **智能体面板与 MCP 上下文投递**：发送后会创建标记为 `SkipPlan: true` 的 `AgentTurn`（遵循 `Plan.dc.html`：“使用 ⌘L 发起的小修改会跳过计划”），附带选中对象 ID 与名称、打开的场景文件路径与 YAML 以及实时渲染的视口 PNG（`RenderViewportPNG`），将右侧栏（`RightRailView` / `RightRailTab`，可通过 `⌘1` / `⌘2` 切换）切至**智能体**标签页（`AgentPanelView`）展示该条对话与上下文标签，并通过 `editormcp` 的 `editor://agent/turn` 资源与 `agent_turn_get` / `agent_turn_reply` 工具推送给已连接的 MCP 客户端。
## 后台任务列表：排队与并行智能体请求（#1022）

请求、计划步骤与自动化任务共享智能体面板中的同一个后台任务列表（`TasksPanelView`，严格使用 `design/editor/Spec.dc.html` 令牌并对应 `Tasks.dc.html`），可通过状态栏任务指示器（`status-tasks`）、`View > 后台任务` 菜单或 `CmdToggleTasks`（`mod+shift+t`）打开：

- **任务模型与纯文本线程持久化（`AgentTask`、`AgentThreadDoc`）**：任务在五个可见分组之间流转——**需要确认**（`TaskNeedsYou`）、**运行中**（`TaskRunning`）、**待审查**（`TaskReadyToReview`）、**排队中**（`TaskQueued`）与**失败**（`TaskFailed`），并记录已完成（`TaskDone`）与已停止/已还原（`TaskStopped`）状态。项目将任务列表以纯文本 YAML 持久化在 `.nsgo/threads/<id>.thread.yaml`（`AgentThreadVersion = 1`，确定性键序输出，`ParseAgentThread` / `EncodeAgentThread` 拒绝未知字段与重复键，读写往返逐字节无损）。
- **排队（Queue）与并行运行（Run in parallel，`SubmitTask`）**：
  - **排队**（`TaskStartQueue`）：排在当前运行中或未审查的请求之后，**仅在**前一个回合被保留（`KeepTask`）或还原（`RevertTask`）之后才自动启动，确保后续请求始终建立在已审查的场景之上。
  - **并行运行**（`TaskStartParallel`）：立即作为独立的智能体会话启动，并在视口与场景树中拥有自己的编号叠加层（`1`、`2`……）。
  - **默认执行器与未连接拦截**：当 `Editor.AgentTaskRunner` 为 nil 时，`SubmitTask`、`SubmitComposerTask`、`tasks_submit` 以及排队推进会自动沿用智能体输入框的同一路由（`resolveRouteAndRunner`——外部 CLI 或内置 API 密钥执行器），以独立会话和独立 `AgentTurn` 运行任务，并将步骤、**需要确认**（`Needs you`）、**待审查**（`Ready to review`）与**失败**（`Failed`）回写到现有任务状态流转；未连接智能体时提交会弹出 Connect 连接对话框且不创建任务。
- **`editormcp` 对象持有锁与权限规则（`Server.ConnectTask`、`CompleteTaskTurn`、`ReleaseTaskHolds`、`TaskReviewSnapshot`）**：每个并行任务绑定到独立的 MCP 会话（外部 CLI 子进程通过 `go tool game mcp` 传递 `NSGO_EDITOR_TASK_ID` 自动绑定，内置执行器通过 `WithTaskToolContext` 自动绑定）。会话通过 `scene_edit`、`object_set_field`、对象菜单工具或瓦片编辑工具编辑对象时会持有该对象直到当前回合结束；另一个并行会话若尝试编辑同一对象，会按 FIFO 顺序自动等待，在面板、视口和场景树中显示 `"waiting on <object>"` 并标注持有它的任务编号，待前一个任务释放持有后自动继续完成，并为两个回合分别生成独立的 `TaskReview`。命中 `Ask` 权限规则的工具调用会将该任务暂停在**需要确认**（`BeginTaskPermissionAsk`），待用户点击**允许一次**或**拒绝**（`ResolveTaskPermission`）后继续。
- **视口与场景树编号叠加层**：运行中任务会在正在编辑的对象周围绘制实线紫色（`theme.Palette.Agent`）描边与编号胶囊（如 `1 placing the jump arc`），在等待持有的对象周围绘制 45% 不透明度的虚线描边、时钟图标与 `"2 waiting on slime_02"` 胶囊，并在视口左下角显示紧凑图例，在场景树行右侧显示 `"1 · editing"` / `"2 · holding"` 标记（`ObjectTaskBadge`、`SceneTaskOverlays`）。
- **批量忽略与摘要徽章计数**：可在 **待审查** 分组中勾选多项任务（`ToggleTaskDismissSelection`）并一键批量忽略（`DismissSelectedTasks` / `DismissTasks`），绝不还原任何已保留的修改。`TaskSummary` 与 `TaskDockBadgeCount` 提供 `NeedsYou`、`Running`、`Blocked`、`ReadyToReview`、`Queued`、`Failed`、`MostUrgent` 及 `DockBadge = NeedsYou + ReadyToReview + Failed`（运行中与排队中绝不计入徽章），供状态栏（`No tasks`、`1 running · 0:38`、按紧急程度排序的混合计数与持久化的 `Failed`）及后续系统通知/程序坞徽章（#1023）直接复用。
- **配套 MCP 工具（`editormcp`）**：提供 `tasks_list`（查询任务、对象持有锁与摘要计数）、`tasks_submit`（排队或并行提交任务）与 `tasks_control`（执行 `keep`、`revert`、`dismiss`、`stop`、`retry`、`open`、`focus`、`allow`、`deny`）。
## 游戏简报与项目规则（#1026）

设置界面（`EditorSettingsDialogView`，对应 `design/editor/Settings.dc.html` 并严格使用 `Spec.dc.html` 设计令牌）在 **智能体（AGENT）** 分组下新增两个智能体每次请求前都会读取的共享项目页面：

- **游戏简报（`brief.yaml`，`OpenBriefSettings`）**：以带标签的表单编辑项目根目录的版本化 `brief.yaml`（`project.Brief`、`project.ParseBrief`、`project.EncodeBrief`，JSON Schema 为 `https://game.nsgo.dev/schemas/brief.v1.json`），包含 **核心简介（Pitch）**、**类型（Genre）**、**标签（Tags）**、**平台（Platforms）**、**美术方向（Art direction）**、**手感与难度（Feel and difficulty）**（含可供 `playtest.TargetsFromBrief` / `playtest.ApplyBriefTargets` / `Editor.BriefPlaytestTargets` 直接读取的结构化试玩目标）以及 **氛围与音频（Tone and audio）**。缺少 `brief.yaml` 的项目显示空状态；填写保存后写出可按字节无损往返的规范 `brief.yaml`，未知字段会连同 1-based 行号被拒绝（`loaderr`），缺失栏目在侧栏显示 `"1 gap"` / `"N gaps"`（中文为 `"1 项缺口"` / `"{count} 项缺口"`），且每次编辑均为 `Editor.History` 中的可撤销步骤（`EditBrief`）。
- **项目规则（`AGENTS.md`，`OpenRulesSettings`）**：在等宽代码输入框中以纯 Markdown 编辑项目根目录的 `AGENTS.md`（Claude Code、Codex 与内置智能体共同读取的同一份文件），所有编辑均为可撤销步骤（`SetProjectRules`），并在侧栏显示规则条数（`ProjectRulesCount`）。根据近期审查中被反复撤销的模式（`RecordReviewOutcome` / `SuggestedProjectRules`）生成规则建议（例如 `"Suggested from your last 30 reviews: you reverted every change that moved the camera bounds."`）；点击 **添加为规则（Add as a rule）**（`ProposeSuggestedRule`）会以可审查差异卡片（**保留** / **撤销**）呈现，在确认保留前不会改写磁盘上的 `AGENTS.md`。
- **智能体编辑走审查流**：智能体通过 `editormcp` 的 `brief_edit` 与 `rules_edit` 提出的修改会排队为待审阅的 `DocReview` 卡片（`ProposeBriefReview` / `ProposeProjectRulesReview`、`KeepDocReview` / `RevertDocReview`），确认保留前绝不改写磁盘；保留后会作为单个可撤销步骤记入 `Editor.History`。
- **本页智能体卡片与设置搜索栏**：两个智能体页面右侧均显示 **本页智能体（Agent on this page）** 卡片，包含上下文提示、可点击的建议提问与内联输入框；点击建议提问或在顶部设置搜索栏提交需求会调用 `AskAgentFromSettings`，将带当前页面上下文的请求写入 `ComposerDraft` 并触发 `Editor.OnSettingsAgentRequest`（及 `Editor.OnComposerPrompt`）。

## 历史面板：你的编辑与智能体回合共用一条时间线（#1017）

历史面板（`HistoryPanelView`）仅使用 `design/editor/Spec.dc.html` 设计令牌并对应 `AskInPlace.dc.html` 与 `Checkpoints.dc.html`，将手动编辑、保留的智能体回合、外部 Git 重载以及保存点统一呈现于同一条时间线上：

- **丰富的历史条目（`History.Entries`、`Editor.HistoryEntries`）**：每条 `HistoryEntry` 记录操作来源（`HistoryAuthorYou`、`HistoryAuthorAgent` 或 `HistoryAuthorGit`）、摘要与详情（例如 `You · moved Knight` / `x 416 → 452`，或 `Agent · slimes and coins` / `6 changes · kept 5`）、涉及的文件列表、变更与保留计数以及由可注入时钟（`Editor.SetHistoryClock` / `History.SetClock`）提供的时间戳，便于确定性测试。保存操作会写入一条带快照的 `HistoryEntrySaved` 标记（`Saved · level_01.scene.yaml`）。
- **统一的多文件智能体回合（`Editor.RecordAgentTurn`）**：将场景、项目与额外改动的项目文件打包为单个可撤销的 `HistoryAuthorAgent` 步骤；同时 `editormcp` 工具调用（`EnterAgentContext` / `LeaveAgentContext`）、`KeepTileReview` 与 `KeepCutsceneReview` 都会自动归属为智能体条目。
- **打开与筛选**：按 `⌘Y`（Windows/Linux 为 `Ctrl+Y`，`CmdHistoryPanel`）、选择 **视图 > 历史**，或点击工具栏的历史图标按钮（提示 `"History · ⌘Y"`）即可打开。条目按最新优先并按日期分组（`TODAY`、`YESTERDAY` 或日期）展示在顶部 **Now**（有未保存改动时标记 `unsaved`）之下，并提供 **All** / **You** / **Agent** 筛选标签（`Editor.SetHistoryFilter`）。
- **只读时间点预览（`PreviewHistoryEntry`、`ExitHistoryPreview`）**：点击任意历史条目会在 2D/3D 视口中只读预览该时刻的场景快照，并显示 `"Previewing <相对时间> · editing is off"` 横幅，不修改当前打开的 `Editor.Doc`，也不移动 `History` 撤销游标。按 `Esc`、**Back** 或 **Now** 即可退出预览并返回实时编辑。
- **非破坏性恢复（`RestoreHistoryEntry`）**：点击 **Restore** 会将当前场景、项目以及期间改动的全部额外文件作为**一个新的可撤销步骤**回滚到该时间点；后续历史步骤全部保留且仍可预览或恢复，执行一次 `Undo` 即可立即回到恢复前的最新状态。
- **智能体读取接口（`editormcp`）**：只读工具 `history_list`（`ActionRead`）向已连接的智能体暴露同一份按最新优先排列的历史列表，支持 `author`（`all`、`you`、`agent`）与 `limit` 筛选以及未保存和当前预览状态。

## 持久化回合检查点（#1018）

智能体回合、自主后台任务与自动化流程的检查点会持久化在项目的 `.nsgo/checkpoints/` 目录中，重启编辑器后仍可保留，并与当前会话编辑一同显示在统一历史面板（`⌘Y`，`CmdHistoryPanel`）中：

- **首次写入前快照（`.nsgo/checkpoints/`）**：在智能体回合（`BeginCheckpointTurn` / `RunCheckpointTurn`，或任意会修改文件的 `editormcp` `ActionEdit` 工具调用）首次修改项目文件——包括场景、预制体、数据表、`game.yaml`、Go 代码及按原字节保存的二进制媒体——之前，编辑器会将其修改前的字节原样保存到 `.nsgo/checkpoints/<id>/files/<path>`，并写入严格版本化的 YAML 清单（`manifest.yaml`，`editor.CheckpointVersion = 1`，JSON Schema `https://game.nsgo.dev/schemas/checkpoint.v1.json`）。回合新建的文件记为 `existed: false`，恢复时会将其删除。编辑器会自动写入 `.nsgo/checkpoints/.gitignore` 与 `.nsgo/.gitignore` 以保持被 Git 忽略，并在总占用超过 `Editor.CheckpointMaxBytes`（默认 `128 MiB`）时自动清理最旧的已完成检查点。
- **后台任务与自动化**：自主后台任务（`TakeCheckpointBeforeTask`、`RunTaskWithCheckpoint` 及 `SubmitTask` 启动的执行器）与自动化流程（`TakeCheckpointBeforeAutomation`、`RunAutomationWithCheckpoint`、`game_playtest` 及自动化模式下的 MCP 编辑）在开始前总会先持久化创建一个检查点。
- **安全与完整性保护**：检查点的记录、写入与恢复严格排除 `.git/`、`.nsgo/` 以及全部仅限用户配置文件（`MatchYouOnlyPath` / `CheckYouOnlyTarget`），拒绝路径上任何层级的符号链接，限制清单与单文件大小上限（`2 MiB` / `32 MiB`），并在恢复写入前同时校验文件字节数与 SHA-256 摘要。
- **统一历史预览与单步可撤销恢复**：`OpenProject` 会将已持久化的检查点重新载入 `HistoryPanelView`（`UnifiedHistoryEntries`）。点击任意检查点可只读预览该时刻的场景快照（`PreviewCheckpoint`），点击 **恢复（Restore）**（`RestoreCheckpoint`）会原子恢复所有受影响文件、同步打开的标签页与 `game.yaml`，并将其作为单个可撤销步骤（`checkpointRestoreCmd`）记入 `History`，按一次 `Undo`（`⌘Z`）即可逐字节还原整个回合。
- **配套 MCP 工具（`editormcp`）**：`checkpoint_list`（`ActionRead`）列出统一历史与持久化检查点；`checkpoint_restore`（`ActionEdit`）会先针对检查点清单中记录的每一个文件评估权限规则（`edit`、`code`、`delete`）及仅限用户边界，通过后再作为单个可撤销步骤执行恢复。

## 仅限用户信任边界与提议删除（#1015）

控制 AI 智能体行为边界的四项设置——**权限**（`permissions.yaml`）、**工具**（`tools.yaml` / `mcp.yaml`）、**模型**（`models.yaml`）与**用量**（`usage.yaml`）——严格遵循**仅限用户（You only）规则**（对应 `design/editor/Permissions.dc.html`、`Settings.dc.html`）：

- **MCP 服务器硬拒绝（`editormcp`）**：编辑器 MCP 服务器上的所有非只读工具都会拒绝任何写入、重命名（含无扩展名短名称的 `refactor_rename`）或删除仅限用户文件（`permissions.yaml`、`.nsgo/permissions.yaml`、`tools.yaml`、`models.yaml`、`usage.yaml` 及用户级 `editor.yaml`，含符号链接与大小写不敏感路径别名）的调用，并返回结构化的 `*editormcp.YouOnlyError`；即使处于 `Autonomous` 配置档也不例外。
- **权限草案卡片（`permissions_draft` / `PermissionsDraftCardView`）**：智能体只能通过 `permissions_draft`（`Editor.SubmitPermissionsDraft`）提交草案。草案以带焦点限制与关闭恢复的独立模态 `ui.Dialog` 打开，展示每条规则的 `NOW → DRAFT` 决策、实时 `permissions.yaml · if applied` YAML 差异预览、**默认全部不勾选**的逐条复选框、**丢弃草案**与**应用 n 条规则**按钮。`⌘↵`、**全部保留**、自动化流程及任意配置档（含 `Autonomous`）都无法应用该草案；已应用的规则在历史记录（`History`）中归属于用户本人（`HistoryActorPerson`），且可用 `⌘Z` 逐字节撤销。
- **外部磁盘变更的收紧生效与放宽暂挂（`ReconcileExternalPermissions`）**：当 `permissions.yaml` 在编辑器外部被修改（如克隆仓库、`git pull` 或其他工具编辑）时，**收紧的配置档与规则会立即生效**；而**放宽的配置档或规则**（例如将 `Balanced` 改为 `Autonomous`、将 `Deny` 改为 `Allow`，或删掉一条 `Deny` 规则）会被暂挂且不会启用，直到用户在 **设置 › 权限** 中逐条确认（`ConfirmHeldPermissionLoosening`）或保持更严格规则（`RejectHeldPermissionLoosening`）。
- **“全部保留”永不自动执行的提议删除（`game_delete_file` / `AgentReviewPanelView`）**：`game_delete_file` 默认关闭（`Editor.ToolEnabled("game_delete_file") == false`），调用后始终只在**审查更改**面板中提议删除（`Editor.ProposeFileDeletion`），展示文件精确行数（或二进制摘要）、红色差异预览以及是否有场景引用该文件（基于 `Editor.FindUsages`），并拒绝符号链接、`.git/**`、`game.yaml`、已打开的场景标签页及大于 1 MiB 的文件。执行**全部保留**（`Editor.KeepAllReviewChanges` / `⌘↵`）会保留其他改动并将待确认删除的文件原样留在磁盘上；用户就地点击**删除此文件**（`Editor.ConfirmProposedDeletion`）后会通过带检查点的可逆命令（`deleteFileCmd`）删除文件，按 **`⌘Z`（`Editor.Undo`）** 可从检查点逐字节恢复原文件。
## 轻提示 Toast 与反馈通道（#1004）

编辑器中的瞬时反馈统一使用同一个符合 Spec 规范的 Toast 组件（`ToastView`、`WindowToastView` 与 `SpecToastCard`，对应 `design/editor/Toasts.dc.html` 与 `Spec.dc.html`，严格仅使用 `theme.Palette` 令牌）。需要用户做决定的事项绝不放入 Toast：权限请求与提问留在智能体面板卡片中，检查失败进入 Problems 面板，完成的智能体回合打开审查流。

- **五种语义类型，统一 40 px 高、12 圆角、单行胶囊**：
  - `ToastSuccess`（圆圈对勾图标，`Added.Fg`）、`ToastInfo`（圆圈 `i` 图标，`TextMuted`）、`ToastWarning`（三角感叹图标，`Warning.Fg`，置于 `Warning.Fill` 与 `Warning.Edge` 表面上）、`ToastError`（带竖杠圆圈图标，`Removed.Fg`）以及 `ToastProgress`（进度环 + 底部 2 px 进度条 + 实时百分比）。
  - 最多包含一个动词操作按钮（可附带快捷键提示，例如 `Undo ⌘Z`）；除 `ToastProgress` 外，所有类型都提供关闭按钮。
- **规范事件 API（`Editor.EmitToastEvent`、`ToastHandle`、`SpecFeedbackChannels`）**：各页面通过强类型的 `ToastEvent`（`ToastEventReviewKept`、`ToastEventReviewReverted`、`ToastEventFixApplied`、`ToastEventAgentStopped`、`ToastEventPermissionSaved`、`ToastEventCopied`、`ToastEventDiskChanged`、`ToastEventSaveFailed`、`ToastEventExportRunning`、`ToastEventExportFailed`）发出提示，不再各自编造色调、时长或去重键。`EmitToastEvent` 返回 `*ToastHandle`，使 `export_panel.go` 导出等长耗时任务可以原地更新百分比（`SetProgress`）、完成后原地转为成功提示（`Complete`，如 `"Exported <path>"` 附带 **Show in Finder**）或持久化错误提示（`Fail` 附带 **Retry**），以及取消正在进行的工作（`Cancel`）。
- **定位规则**：当视口在屏幕上可见时，锚定在视口底部居中、距视口工具条上方 8 px，绝不遮挡场景树、检查器或智能体面板；当没有视口在屏幕上时（启动页、设置、导出及其他全窗口页面），`WindowToastView` 锚定在窗口底部居中、距底边 24 px 处。
- **队列、`+N` 等待计数、重复合并与顺序**：同一时间只显示一条 Toast，其余按 FIFO 顺序排队并在当前 Toast 上显示 `+N` 等待计数徽章。重复的同一事件会原地合并到等待条目中而不改变队列顺序，且任何新事件都不会插队到错误提示或运行中的进度提示之前。
- **时长、暂停、动效与无障碍**：
  - `ToastDuration` 严格执行规范时长：成功与信息为 4 秒（带操作按钮时为 6 秒），警告为 8 秒，错误与进度保持显示直到重试、关闭、取消或完成。鼠标悬停、触摸、键盘聚焦在 `RegionToast` 内，或窗口离开 `core.LifecycleActive`（退到后台）时都会暂停倒计时（`Editor.ToastTimerPaused`、`Editor.ToastRemaining`）。
  - 入场动效为 180 ms 淡入并向上浮起 8 px，退场为 120 ms 淡出；开启减少动态效果（`ReduceMotion`）后立即出现与消失，无淡入淡出和位移。
  - 两个 Toast 宿主容器均为礼貌实时区域（`core.RoleStatus`、`core.LiveRegionPolite`）。弹出 Toast 绝不抢占焦点；`F6` / `Shift-F6` 可切换进入 `RegionToast`，`Tab` 在操作与关闭按钮间移动，聚焦时按 `Esc` 关闭该 Toast 并将焦点还原回进入 Toast 前的控件。
- **Spec 反馈通道映射表（`SpecFeedbackChannels`）**：

| 事件 | 通道 | 操作 / 说明 |
| --- | --- | --- |
| 保留或撤销审查（Review kept or reverted） | Toast | `Undo ⌘Z` |
| 从 Problems 应用修复（Fix applied from Problems） | Toast | `Undo ⌘Z` |
| 用 `⌘.` 停止智能体（Agent stopped with `⌘.`） | Toast | `Review` |
| 保存“始终允许”权限规则（“Always” permission rule saved） | Toast | `Show` |
| 复制配置、路径或代码片段（Copied a config, path or snippet） | Toast | 信息提示（已接入连接命令、复制 YAML、复制对象、Web 预览链接、PR 描述与诊断信息） |
| 磁盘文件在编辑器外被修改（A file changed on disk outside the editor） | Toast | 警告 · `Show`（已接入磁盘监视器重载） |
| 保存或导出失败（Save or export failed） | Toast | 错误 · `Retry`，持续停留（仅显式 `⌘S`/保存与导出失败弹出 Toast；自动保存失败按 `Checkpoints.dc.html` 留在状态栏中，从不弹 Toast） |
| 导出进行中（Export running） | Progress toast | `Cancel`，完成后原地转为 `"Exported <path>"` 与 `Show in Finder` |
| 智能体请求权限或提问（Agent asks permission or a question） | Agent panel card | 绝不使用 Toast |
| 智能体完成请求（Agent finished a request） | Review opens | 绝不使用 Toast |
| 检查失败或损坏引用（Check failed, broken reference） | Problems | 绝不使用 Toast |
| 编辑器在后台时智能体提问、完成或停止 | + System notification | 与编辑器内通道配合 |
## 智能体自动化（#1029）

项目自动化（位于 `game.yaml` 旁的 `automations.yaml`，JSON Schema 为 `https://game.nsgo.dev/schemas/automations.v1.json`）允许智能体在更改发生后自动执行后续检查与任务，同时确保每项结果均经过人工审查（对应 `design/editor/Settings.dc.html`、`Tasks.dc.html`、`Checkpoints.dc.html`）：

- **带版本的 YAML 格式（`AutomationsFile`、`ParseAutomations`、`EncodeAutomations`）**：`version: 1` 及有序的 `automations` 列表（每项包含 `id`、`name`、`enabled`、`trigger`、`instruction`）。拒绝未知字段、多余 YAML 文档、重复键、YAML 锚点/别名、重复 ID 与无效触发器参数，并返回带行号与列号的 `*loaderr.Error`；编码输出确定且无损往返。
- **五种触发器**：
  - `file_changed`（`glob`）：匹配的项目文件保存时（手动保存、自动保存或标签页保存）触发，支持 `*`、`**` 与 `?`。
  - `review_kept`（可选 `glob`）：保留审查时（`KeepReview`、`EndReview(true)`、`KeepTileReview`、`KeepCutsceneReview`、`KeepAllReviewChanges`、`KeepAgentTurnReview`）触发，并自动跳过触发来源的同一自动化 ID 以避免循环。
  - `file_added`（`kind`，可选 `glob`）：新增指定类型文件（`prefab`、`scene`、`tileset`、`cutscene`、`material`、`particles`、`rig`、`screen`、`datatable`、`script`、`asset` 或 `any`）或磁盘监视（`PollDiskChanges`）发现新文件时触发。
  - `before_export`（可选 `platform`）：在 `StartExportBuild` 或 `StartReleaseBuild` 开始前触发。
  - `daily`（24 小时制 `HH:MM` 的 `time`）：编辑器打开期间每天到达或超过指定时间后最多触发一次（由 UI 调度器定期检查，也可通过 `Editor.StepAutomations` 确定性驱动）。
- **运行前检查点、权限、后台任务与待审查机制**：每次自动化运行开始前，编辑器都会先将受追踪的项目文本/YAML/Go 文件快照（跳过符号链接、内部状态与仅限用户文件，并受文件数与总字节上限约束）写入 `.nsgo/checkpoints/<id>/`（已被 git 忽略，可通过 `AutomationCheckpoints` 查看、通过 `RestoreAutomationCheckpoint` 在路径与符号链接隔离校验下恢复）。任务在有界并发与超时保护下运行于 `WithAgentActor` 与项目权限规则（`Editor.Permissions` / `CheckAutomationActionPermission`）约束内，同步登记到共享后台任务列表（`TaskSourceAutomation`）并以 `AgentTurn` 落入智能体面板的 **待审查**（`AgentReviewReady`），提供 **保留**、**忽略** 与 **恢复检查点** 按钮。`automations.yaml` 与 `.nsgo/automations-state.yaml` 受“仅限用户”边界（`YouOnlyAutomations`）保护，自动化永远无法直接应用“仅限用户”草案（`PermissionsDraft`、`AutomationDraft`）、确认暂挂的权限放宽或提议删除，或自行保留审查（返回 `ErrAutomationYouOnly`）。
- **设置 › 自动化页面（`SettingsPageAutomations`）**：以 `WHEN → THE AGENT` 卡片展示每项自动化、本周运行次数（`AutomationWeeklyRuns`，保存在被 git 忽略的 `.nsgo/automations-state.yaml` 中）及 `SpecSwitch`（`core.RoleSwitch`）开关，支持 **+ 用一句话新建自动化**（`DraftAutomationFromWords` 生成草稿卡片并由用户点击 `KeepAutomationDraft` 保留）、**今天暂停全部自动化**（`PauseAutomationsForToday` / `ResumeAutomations`，暂停所有触发直至次日自动恢复）以及 **本页智能体** 卡片，深浅色外观均严格使用 `theme.Palette` 令牌。
## 逐项审核智能体回合改动（#1014）

当智能体完成一轮编辑（`Editor.StartTurnReview`）后，全窗口的**审核**界面（`ReviewScreenView`，对应 `design/editor/Review.dc.html`，严格遵循 `Spec.dc.html` 深浅双主题令牌）允许作者按文件和改动类型逐条检查场景、预制体、数据表与 Go 文件的改动，最后一步保留或整体撤回：

- **顶栏与阻塞项**：展示 **← Editor**（`Esc`）、引用的用户请求、阻塞提示胶囊（当存在冲突、YAML 错误等阻塞项时）、**Revert all**（`⌘⌫` / `Ctrl+Backspace`）与 **Keep N of M**（`⌘↵` / `Ctrl+Enter`）。存在阻塞项时，点击 **Keep** 不会提交，而是直接跳转聚焦到首个阻塞改动（`JumpToReviewBlocker`）。
- **左侧分组列表与筛选**：包含按名称、字段或文件路径实时过滤的搜索框（`SetReviewSearch`）、带进度条的 `"N of M seen"` 计数及上/下一项按钮，以及 6 个带计数的过滤标签（**All**、**Added**、**Changed**、**Removed**、**Unseen**、**Notes**，`SetReviewFilter`）。改动先按**文件**分组（路径、`+n ~n −n` 计数、折叠、**Revert file** / **Restore file**），再按**类型**分组（**ADDED**、**CHANGED**、**REMOVED**，支持折叠及 **Revert n** / **Restore n**），每条改动卡片显示已读标记、语义类型徽章、标题、详情与 **Revert** / **Restore**（`X`）；聚焦某条改动即自动标为已读。
- **审核键盘作用域（`ScopeReview`）与无障碍导航**：在 `ScopeReview` 中注册 `J`（`CmdReviewNext`）、`K`（`CmdReviewPrev`）、`X`（`CmdReviewToggleRevert`，切换当前聚焦改动、类型分组或文件分组的撤回/恢复）、`B`（`CmdReviewPeekBefore`，按住 `B` 预览请求前的场景）、`⌘↵`（`CmdReviewKeep`）与 `⌘⌫`（`CmdReviewRevertAll`）。所有控件均显示 2 px 规范焦点环并可通过 `Tab`、`F6` / `Shift-F6`（在顶栏 → 列表 → 中央预览 → 原因与 YAML 间循环）或方向键访问；在筛选输入框中输入不会误触单字母快捷键。
- **自适应中央预览区**：
  - **场景文件**：渲染当前聚焦改动所属场景（`BuiltScene`），左上角提供可点击开关的叠加层图例（`ToggleReviewOverlayLegend`）；未聚焦改动以 `1.5 px` 绘制，当前聚焦改动以其**自身**语义颜色和虚实线型以 `2.5 px` 加粗绘制并附描边胶囊（绝不使用留给选中态的强调色），已撤回的改动按请求前的原样绘制；按 `J`/`K` 自动将视口聚焦到对应对象，按住 `B` 显示请求前场景与 `"Before this request"` 提示胶囊。
  - **预制体文件**：展示旧值 → 新值及数值变化百分比 `Δ%`，并结合本次审核中的场景与 `Editor.FindUsages` 项目引用，说明受影响的全部实例（如本次审核中的场景实例以及项目中其它关卡已有实例）。
  - **小型数据表**：以表格形式渲染（`ReviewDataTablePreviewFor`），高亮改动行并在修改单元格中展示 `旧值 → 新值 Δ%`。
- **右侧面板（Why 与 YAML）**：在顶部显示智能体给出该改动的原因（**WHY**），下方展示带行号与 `+` / `−` 符号的精确 YAML 差异行。
- **按 `Esc` 挂起与恢复审核**：按 `Esc`（`PauseReview`）回到编辑器并保持审核挂起——主视口继续绘制待审核改动叠加层（`drawPendingReviewOverlay`），右侧 **Agent** 标签页（`AgentPanelView`）展示 `"Review waiting · N changes, F files"`、已读计数、阻塞数与 **Resume review** 按钮（`ResumeReview`），点击后精确回到离开时的聚焦改动与已读状态。
- **保留、全部撤回与单步撤销**：**Revert all** 弹出带焦点限制的确认对话框，列出将还原的文件并提示 `⌘Z` / `Ctrl+Z` 可全部恢复。**Keep**（`KeepTurnReview`）与 **Revert all**（`ConfirmReviewRevertAll`）均记录为 `Editor.History` 中的**单个步骤**，更新磁盘 YAML 文件与当前打开场景、弹出带 **Undo** 的规范 Toast，并在 Agent 面板显示 `"Kept K · reverted R · one undo step"`；执行一次 `Editor.Undo()` 即可按字节完整还原全部改动文件。
- **MCP 只读接口（`editormcp`）**：只读工具 `review_status`（`ToolReviewStatus`、`ActionRead`）供智能体读取每项改动的已读/撤回状态、冲突合并选择（`mine`、`agent`、`both`）、标注为 `"you"` 的就地手改 YAML 字段与代码行、当前阻塞项以及保留/撤回结果（保留后仍可在第二轮读取），但保留与撤回决定权始终属于开发者（绝不暴露未发送的草稿批注，并强制执行 `SceneAndData` / `GoSource` 隐私规则）。

### 审核批注与第二轮局部重做（#1031）

当审核中只需微调一两处改动而不想全部撤回或手改 YAML 时，作者可以在当前回合的任意改动上添加批注并交由智能体在同一审核内仅重做带批注的改动作为 **Round 2**（`design/editor/Review.dc.html`）：

- **行内批注编辑器（`C` / `CmdReviewNote`）与视口图钉**：在聚焦改动上按 `C`（或点击批注图标按钮）会在卡片下方展开行内批注框（`"Note for the agent · not sent"`），提供 **Add note**（`CommitReviewNote`）、**Revert and redo with this note**（`RevertAndRedoReviewChange`）与 **Cancel**（`CancelReviewNoteComposer` 或 `Esc`）。已保存的批注会显示在卡片上（支持 **Edit** 与 **Remove** / `RemoveReviewNote`），并在审核视口与挂起后的 2D 主视口中以规范配色图钉标注在对应对象旁（`ReviewViewportPins`）。批注仅保存在当前回合的内存状态中（且限制长度与数量），绝不写入任何场景、预制体或数据文件；发送前被移除的批注绝不会到达智能体。
- **发送批注与仅重做批注项（`SendReviewNotes`、`ApplyReviewRedo`、`editormcp`）**：点击顶栏的 **Send N notes**（存在未发送批注时切换为主强调色按钮）会通过 MCP（`review_notes_get`）将所有未发送批注连同每项改动的 `change_id`、`file`、`object`、`field`、`kind` 与 `note` 发给智能体。智能体在同一回合内调用 `review_redo` 仅重做带有批注的改动（未加批注的改动以及同一文件中被夹带修改的未批注对象均会被原子拒绝，同时强制执行 You-only、权限规则与隐私边界）；重做后的改动在 Round 2+ 中重新标为未读，并在 **WHY** 中显示 `"Redone from your note: …"`、在卡片下方显示 `"Revised in round N. Your note: …"`。
- **轮次标签页、横幅、过滤、挂起卡片与单步撤销**：进入 Round 2 后，顶栏展示 **Round 1 | Round 2** 切换标签（`SetReviewViewRound`），过滤标签下方展示轮次横幅（`"Round 2 · the agent redid only the N changes you left notes on. Both rounds keep and undo as one step."`）。**Round 1** 以只读方式保留智能体最初版本供对照（编辑、撤回或批注操作返回 `ErrReviewReadOnlyRound`）；**Keep**（`⌘↵`）始终将**最新一轮**合并为 `Editor.History` 中的**单个步骤**，按一次 **Undo**（`⌘Z`）即可将全部受影响文件按字节还原至请求前状态。**Notes** 标签用于筛选带批注的改动，挂起时的 **Agent** 面板卡片会显示未发送批注数（`"N notes not sent"`），**Revert all** 对话框也会提示将丢弃未发送批注。

### 审核冲突合并与就地手改 YAML（#1032）

在逐项审核界面中，以下两种情况会清晰阻止 **Keep** 而不会丢失开发者或智能体任何一方的工作：

- **三方冲突检测与合并（`syncReviewAfterMutate`、`SyncReviewPersonEdits`、`ResolveReviewConflict`）**：当审核回合挂起期间开发者通过编辑器命令（如 `MoveObject`、`SetProperty`、`SetComponentField` 等）手动编辑场景时，编辑器会自动通过 `sceneformat.Merge` 对基线、开发者修改与智能体提案做三方合并。对同一对象**不同字段**的修改会自动无感合并；对**同一字段或列表**的重叠修改（例如同时向 `door_1` 的 `on_open` 列表追加条目）会在改动卡片内展示双方行并标明作者（`you`、`agent`），提供 **Mine / Agent's / Both** 三选一控件（`ReviewConflictMine`、`ReviewConflictAgent`、`ReviewConflictBoth`），其中 **Both** 会保留双方条目且**你的条目在前**。在所有冲突未作出选择前，**Keep** 处于禁用状态（返回包装 `ErrReviewBlocked` 的 `*ReviewBlockedError`）并在顶栏提示 `"Choose how <对象> merges"`，点击 **Keep** 或顶栏阻塞胶囊会直接跳转到该冲突卡片。
- **右侧 YAML 面板就地编辑与视口实时更新（`BeginReviewYAMLEdit`、`SetReviewYAMLDraft`、`FinishReviewYAMLEdit`）**：对任意未撤回的新增或修改场景对象点击 **Edit** 即可在右侧面板就地编辑 YAML，输入过程中保持输入框焦点稳定且绝不误触单字母审核快捷键。草稿受大小上限（256 KiB）保护，严格使用 `KnownFields(true)` 解码并拒绝未知字段、重复键与仅限用户文件，同时通过 `sceneformat.Validate` 与组件注册表校验。合法修改会实时刷新审核视口（`ReviewEffectiveSceneDoc`）并将手改行标为 `you`。
- **YAML 解析错误、一键修复与撤销输入（`ApplyReviewYAMLFix`、`UndoReviewYAMLTyping`）**：输入无效 YAML（例如漏写冒号 `x 444`）会高亮标记出错的场景行，在顶栏显示 `"Fix YAML on line N"`、在告警卡片显示 `"Line N isn't valid YAML"`，禁用 **Done**（返回 `ErrReviewYAMLInvalid`）与 **Keep**，在解析器明确指出缺失项时提供经过校验的一键修复按钮（**Add the colon**、**Close the quote**、**Add the closing brace**、**Add the closing bracket**、**Replace tabs with spaces**），并始终提供 **Undo my typing** 回退本次输入。
- **单步撤销与第二轮 MCP 可见性**：冲突合并选择与合法手改 YAML 都属于该回合的一部分——点击 **Keep**（`KeepTurnReview`）时合并为 `Editor.History` 中的**单个撤销步骤**，按一次 `Undo` 即可整体撤销智能体改动、合并选择与手改内容（同时保留开发者在审核外的手动编辑），并通过 `editormcp` 的 `review_status` 工具（`Editor.ReviewStatus`）以 `"you"` 标签供智能体在第二轮读取。

## 智能体任务的系统通知、Dock 角标与播放模式提示点（#1023）

编辑器通过 NSGo 原生通知与角标 API（`core.Scope.ShowNotification`、`core.Scope.RequestNotificationPermission`、`core.Scope.OnNotificationPress` 与 `core.Scope.SetBadge`）为后台任务（`tasks.go` / #1022）与智能体面板回合提供编辑器外提醒，界面严格遵循 `design/editor/Tasks.dc.html`、`design/editor/AgentStates.dc.html` 与 `design/editor/Spec.dc.html` 令牌规范：

- **仅三类事件发送系统通知**：**等待权限**（`TaskNeedsYou`）、**待审查**（`TaskReadyToReview`）与**任务失败**（`TaskFailed`），标题与正文包含 `"NSGo Editor · <项目名>"`、任务名称、详情与主要操作（`Allow once`、`Review` 或 `Open`），并对控制字符进行净化与长度限制。已完成的步骤、排队启动、从磁盘恢复的历史任务以及普通智能体回复绝不发送系统通知。
- **按需请求通知权限与 30 秒规则**：仅在首次真正需要发送通知时调用 `cx.RequestNotificationPermission`（在未检查通知权限前保持 macOS Dock 角标可用）。当编辑器窗口处于后台（`cx.Env().Lifecycle() != core.LifecycleActive`）时，三类事件立即通知；在前台时，未答复的等待权限请求会在 `AgentWaitingNotifyDelay`（`30 s`）后发送系统通知，若提前处理则取消。
- **点击通知回到对应场景中的任务**：`cx.OnNotificationPress`（经由 `Editor.HandleNotificationPress` / `Editor.OpenAgentTask`）将编辑器窗口置前（并触发可选的 `Editor.OnFocusWindow` 钩子）、若在播放中先停止播放、关闭遮挡面板（设置、历史、问题智能体面板、就地提问、资源模式）、切换到或严格在项目根目录内打开该任务所在的场景标签页（拒绝路径遍历与符号链接逃逸）、打开任务或智能体面板并聚焦对应任务及待处理请求卡片。
- **Dock 图标角标（`cx.SetBadge`）**：`Editor.AgentDockBadgeCount` 仅统计等待中（`TaskNeedsYou`）、待审查（`TaskReadyToReview`）与失败（`TaskFailed`）任务的总数（`AgentTaskCounts.BadgeCount()`）；运行中与排队中的任务绝不计入角标，归零或在作用域清理时自动清除。
- **播放模式角落提示点与延后通知（`PlayTaskDotView`）**：在播放模式（`Editor.Playing()`）期间，运行中的游戏上方不会弹出任何智能体任务通知，系统通知会暂存（`Editor.HeldAgentNotifications`）直至 `Editor.Stop()` 停止播放后才对仍需处理的任务发出。视口右上角稳定挂载一个带 3 pt 光晕的 8 pt 圆点，颜色取当前最紧急状态的 Spec 令牌色（**失败** `Removed.Fg` > **等待处理** `Warning.Fg` > **待审查** `Accent.Fg`），仅在悬停或键盘聚焦时显示提示胶囊（例如 `"1 task needs you · stop Play to open"`）。
## 在视口与场景树中实时显示智能体编辑（#1013）

当智能体通过 `editormcp` 或待定智能体回合（`Editor.BeginAgentTurn` / `Editor.RunAsAgent`）编辑场景时，编辑器会在视口与场景树中实时呈现其未确认改动（对应 `design/editor/Working.dc.html`、`Main.dc.html` 与 `Spec.dc.html`），且绝不会替智能体移动 2D 或 3D 视口相机：

- **实时视口叠加层（`ViewportLiveOverlays` / `VisibleViewportLiveOverlays`）**：
  - **新增（Added）**：1.5 px 绿色虚线框（`theme.Palette.Added`）与 `"+ <name>"` 等宽胶囊标签。
  - **修改（Changed）**：1.5 px 琥珀色虚线框（`theme.Palette.Changed`）与 `"<field> <old> → <new>"` 等宽胶囊标签（同时自动同步到检查器的琥珀色审查提示）。
  - **移除（Removed）**：位于对象原边界处的 25% 灰色虚影、1.5 px 红色实线框（`theme.Palette.Removed`）以及带删除线名称的 `"<name> removed"` 胶囊标签。
  - **智能体处理中（Agent working）**：1.5 px 紫色脉冲实线框（`theme.Palette.Agent`，1.6 秒在 45%–100% 不透明度间循环；开启“减少动态效果”时固定 100% 实线），不带缩放手柄，上方显示带紫色星芒图标与活动描述的胶囊（如 `playing level_01 headless`）。
  - **问题（Problem）**：2 px 红色实线框与实心高对比度告警胶囊。
  - 胶囊排布自动避让碰撞并向上错开，避免相邻对象或叠加状态互相遮挡。
- **选中对象共存与步骤级只读保护**：当用户选中智能体当前正在操作的对象（`Editor.BeginAgentStep` / `editormcp.Server.BeginStep`）时，选中框保留其变换手柄，智能体轮廓自动再向外偏移 3 px（`OutsetPx: 6`）。在该步骤离开此对象（`EndAgentStep`）之前，视口、2D/3D 手柄、检查器（顶部显示紫色只读提示条）、场景树（重命名与拖拽重挂）、右键菜单及直接调用的 `Editor` 修改方法均会以 `ErrAgentTouchingObject` 拒绝手动修改；未被智能体触及的其他对象仍可自由手动编辑。
- **场景树标记**：新增节点显示 `NEW` 徽章，修改节点显示 `MOD` 徽章，被移除节点以带删除线的虚影行与 `DEL` 徽章保留在树中，智能体正在操作的节点显示紫色圆点，直至执行 **Keep** 或 **Revert**。
- **顶栏状态胶囊**：随智能体状态自动切换（`Editor.SetAgentState` / `Editor.AgentState`）：工作中显示紫色胶囊（`"Agent is <activity> · manual edits stay available"`），等待确认显示警告胶囊（`"Waiting for you · <detail>"`），待审查状态显示改动数量与 **Keep**（`KeepAgentTurn`，将整个回合合并为单步撤销）/**Revert**（`RevertAgentTurn`，回滚回合改动并还原已删除对象）按钮，空闲时自动隐藏。
- **左上角可交互叠加层图例**：当存在任意实时叠加层（或 `OverlayLegendOpen` 为真）时自动显示。图例中的每一行（**Selection**、**Agent working**、**Added**、**Changed**、**Removed**、**Problem**）都是可点击的 `RoleSwitch` 开关（`ToggleOverlayLegendKind` / `SetOverlayLegendKind`），隐藏项按用户持久化到项目之外的 `EditorSettings.HiddenOverlayKinds`（`editor.yaml` 中的 `hiddenOverlayKinds`，Schema 为 `editor-settings.v1.json`），绝不污染项目文件。

## 按工作线与场景分组的智能体线程（#1019）

右侧栏（`RightRailView`）可在**智能体**（`AgentPanelView` / `AgentThreadsView`）、**检查器**（`InspectorView`，默认标签）与**历史记录**（`AgentHistoryView`）之间切换，全部使用 `design/editor/Spec.dc.html` 的深浅双主题规范令牌并对应 `Threads.dc.html`：

- **`.nsgo/threads/` 下每条线程一个 YAML 文件（`AgentThread`、`AgentThreadFileVersion = 1`）**：每条线程以严格版本化的 YAML 文档保存在 `.nsgo/threads/<id>.yaml`（同时自动写出 `.nsgo/.gitignore` 使其被 git 忽略），记录 `id`、`title`（自动取自首条请求）、`scenePath`、外部智能体会话 `sessionId`（`SetAgentThreadSessionID`）、时间戳以及按顺序排列的 `requests`（包含 `steps`、审查结果 `outcome`（`pending`、`kept`、`reverted`）、修改计数、受影响的 `objects` 与 `historyStep` 链接）。通过 `ParseAgentThreadYAML` 严格解析并以 `EncodeAgentThreadYAML` 写出规范形式（JSON Schema：`https://game.nsgo.dev/schemas/agent-thread.v1.json`）。
- **分组线程切换器、搜索与 `⌘N`（`AgentThreadGroups`、`CmdNewAgentThread`）**：切换器按`<当前场景> · 当前场景`、其他场景以及`项目 · 无场景`分组展示线程，每行附带相对时间与状态（如“刚刚 · 3 项待审查修改”、“1 小时前 · 已保留 5/6 项”、“昨天 · 已还原”或涉及文件）。打开或切换场景会自动进入该场景最近使用的线程（或新的空白草稿），绝不把上一场景的线程带到新场景；当**智能体**标签页处于活动状态时，按 `mod+n` / `⌘N`（`ScopeAgent`）会新建空白线程而非触发 `File > New Project...`。
- **折叠较早请求、跳转历史记录与跳到最新**：已审查的较早请求折叠为单行（`N 条较早请求`），点击展开显示完整提示词、变更明细、步骤列表以及**在历史记录中打开此步骤**（`OpenHistoryStep`）链接，点击后在统一历史时间线中高亮该步骤；展开较早请求时底部浮现**跳到最新**按钮（`JumpToLatestAgentRequest`）。
- **空白线程视图（`AgentAlwaysReadItems`、`AgentBriefSuggestions`）**：空白线程会列出智能体每次请求必读的上下文（`<场景> · 整个场景`、`brief.yaml`、`AGENTS.md`、`视口图像`），并安全读取项目 `brief.yaml` 中的建议提示（`来自项目简报`）。
- **检查器页脚来源链接（`LastAgentChangeForObject`、`OpenAgentRequest`）**：当选中对象曾被未还原的智能体请求修改（无论是通过 `SendAgentRequestWith`、`RecordAgentStep` 还是 `editormcp` 的 `scene_edit`），检查器页脚会显示“修改自“<请求标题>” · <时间>”，点击即可打开对应线程并定位/展开该请求。
## 面向智能体的视口草图层（#1025）

在 2D 视口上直接绘制会话级编号草图，指向关卡中尚不存在对象的区域再交给智能体构建（对应 `design/editor/Sketch.dc.html`，严格使用 `design/editor/Spec.dc.html` 令牌）：

- **场景坐标下的四种草图工具**：按 `D`（`CmdSketchToggle` 或 `视图 > 草图`）开启/关闭草图模式，不向紧凑的 2D `editor-toolbar` 额外增加按钮。在 `ScopeSketch` 作用域下可切换 **画笔（Pen，`P`，自由折线）**、**箭头（Arrow，`A`，起点 → 终点）**、**方框（Box，`B`，矩形区域）** 与 **文本（Text，`T`，锚点 + 标签）**，也可按 `I`（`CmdSketchAddMarkAtCursor`）在光标处用键盘放置标记。每个标记自动分配连续编号徽章（`1`、`2`、`3`…）和可选标签（例如 `"3 spikes"`、`"platform, back and forth"`、`"locked until the lever"`）。所有几何坐标均通过 `CanvasToWorld` 严格保存在场景坐标中，跨缩放与平移保持精准，且**绝不**写入 `SceneDoc`、`*.scene.yaml` 或场景撤销 `History`。
- **悬浮草图栏、场景树行与状态栏**：顶部悬浮草图栏提供四种工具、**撤销上一个标记**（`ScopeSketch` 下的 `⌘Z`）、**隐藏 / 显示标记**（`H`）、`"N marks not saved"` 计数、**Clear** 与 **Done**。按 `Esc` 结束草图绘制模式并保留已绘标记，直到发送、清空或审查保留。场景树底部展示紧凑的 `SKETCH LAYER` 行（`N marks · not saved`），状态栏实时显示场景坐标与当前草图工具。
- **将草图发送给智能体（`SendSketch` / `Editor.OnSendSketch`）**：输入框附带 `"Sketch · N marks"` 上下文芯片（点击可展开 `"Sent with this request"` 场景坐标预览）。发送时通过 `SketchAgentRequest` 传递结构化标记（编号、类型、场景坐标几何、标签）以及叠加了编号标记的视口渲染 PNG（`RenderSketchViewportPNG`）。
- **智能体解读卡片与 `scene_edit` 确认闸门**：发送草图后立即启用确认闸门（`SketchHoldActive() == true`），在用户确认前 `scene_edit` 等变更场景的 MCP 工具会被拒绝。智能体通过 `sketch_read` 读取草图，并通过 `sketch_reading`（`PostSketchReading`）提交复述卡片（`"I read your sketch as: …"` 与待确认假设）。点击 **Go ahead**（`ApproveSketchReading`、`⌘↵`）放行修改；点击 **Correct a mark**（`CorrectSketchMark`、`⇧M` 或点击任意编号条目）会重新进入草图模式并精确选中该标记，同时保持确认闸门生效；直接输入文字回复（`ReplyToSketchReading`）同样有效。
- **审查叠加层与保留 / 撤销行为**：确认后智能体的场景修改会打开审查卡片（`SketchReviewOpen`），内置 **Sketch overlay** 开关（`O`、`ToggleSketchReviewOverlay`，瓦片审查 `TileReview` 中同样支持）将草图以半透明点线叠加在结果之上。**Keep**（`KeepSketchReview` / `KeepTileReview`）保留修改并清空草图层；**Revert**（`RevertSketchReview` / `RevertAllTileReview`）撤销修改但保留草图标记以便再次发送。
## 智能体输入框中的参考图与固定到简报（#1027）

在智能体输入框中为单次请求附加 PNG、JPEG 或 WebP 参考图，或将其固定到项目的游戏简报，界面严格遵循 `design/editor/Spec.dc.html` 设计令牌并对应 `design/editor/Sketch.dc.html`（*Paste or drop an image*、*Pin to brief*）：

- **拖放、粘贴或选取 PNG、JPEG 与 WebP**：将文件拖到智能体输入框上方时会显示符合 Spec 规范的 **“Drop to attach”** 拖放层（`SetAgentComposerDragOver`、`DropAgentReferenceFiles`）；向输入框粘贴图片或图片文件（通过 `core.Input.OnPaste` 调用 `HandleAgentComposerPaste`，或调用 `PasteAgentReferenceImage`），或点击 **附加参考图** 按钮（`PickAndAttachAgentReferenceImages` / `CmdAttachReferenceImage`）会在内存中校验真实的 PNG、JPEG 或 WebP 文件头与像素（`DecodeAgentReferenceImage`）。不支持的文件类型（如 GIF、SVG、BMP、文本或损坏的图片）会被拒绝并在无障碍提示横幅中说明具体原因（`AgentAttachError`）。
- **带悬停预览的单次请求标签**：每张附加的图片都会显示为包含缩略图、文件名、**Pin to brief** 按钮与 **×** 移除按钮的标签（`RemoveAgentReferenceImage`）。悬停或聚焦标签会在输入框上方弹出预览卡片，展示解码后的图片、格式、尺寸及作用范围（仅随本次请求发送）。发送请求（`SubmitAgentPrompt`）会将附加图片随 `AgentRequest.AttachedImages` 与 `AgentRequest.Images` 传入（并在 `AgentTurn.Images` 中记录缩略图），随后立即清空单次附加标签，确保下一次请求不再携带。
- **固定到简报（`PinAgentReferenceImage`）**：点击 **Pin to brief** 会将图片复制到 `art/reference/<name>`（存在不同内容同名文件时自动去重而不覆盖），并通过 YAML AST 编辑在 `brief.yaml` 的 `art.references` 下新增一条记录（完整保留原有字段与注释），整个操作作为 `Editor.History` 中的**单个原子可撤销步骤**执行。两个文件会立即出现在审查/变更列表（`RefreshChanges`）中，确认卡片会展示带 `+` 高亮的 YAML 差异及 **Undo** 按钮（`UndoLastBriefPin`）；执行一次撤销即可同时移除复制的图片文件与新增的 YAML 行（若 `brief.yaml` 与空 `art/reference/` 目录是本次创建的也会一并清理）。
- **始终附带标签（`brief.yaml`）**：只要项目存在 `brief.yaml`，输入框中就会显示 **Always** 行及带锁图标的 `brief.yaml` 标签（有固定参考图时显示数量）。悬停或点击该标签可预览全部已固定参考图，后续每次智能体请求都会通过 `AgentRequest.BriefImages` 与 `AgentRequest.Images` 自动携带这些图片。
## 智能体回合：跨文件待确认改动与单步撤销（#1011）

智能体在一次请求起止之间（从右侧 Agent 面板发起，或通过 MCP 的 `turn_start` 开启、`turn_end` 结束）经由编辑器 MCP 服务器所做的全部编辑，都会归入**同一个待确认智能体回合**，统一涵盖它触及的所有文件——场景、预制体、数据表、`game.yaml` 与动画片段：

- **内存实时生效、保留保存前零落盘**：所有编辑立即在内存中生效，视口、场景树、检查器与 `scene_read` 都能实时看到（`Editor.StagedFileYAML`），但在用户保留回合并保存之前绝不向磁盘写入任何文件。存在待确认回合时，直接保存（`Save` / `SaveAll`）会被拒绝，自动保存也会暂停。
- **带 `why` 说明的结构化派生差异**：编辑器不是记录原始命令流水，而是把每个被触及的文件与回合前状态做结构化对比来派生变更（场景与预制体按对象 ID 与字段/组件/属性/图层/父级，数据表按记录，`game.yaml` 按点分键，动画片段按头信息/轨道/事件）。每条派生的 `AgentTurnChange` 带有 `Kind`（`added`、`changed`、`removed`）、`Title`、简明 `Detail`（如 `z 10 → 12`）、`File`、从 1 开始的 YAML `Line`、可选的 `Why` 说明（来自 MCP 编辑工具的 `why` 参数）与 `Reverted` 标记，并附带每个文件精确的 `+added −removed` 行数统计。
- **待确认期间按单条或按文件还原与恢复**：回合处于待确认或已停止状态（`⌘.` / `CmdStopAgentTurn`）时，用户可以随时还原或恢复任意单条变更（`RevertAgentTurnChange` / `RestoreAgentTurnChange` / `ToggleAgentTurnChange`）或某个文件中的全部变更（`RevertAgentTurnFile` / `RestoreAgentTurnFile`）。被还原的变更立即从内存状态移除，恢复时干净回到原位，且不影响未触及对象上的其他改动（包括用户并发做的编辑）。
- **Keep 与 Revert 的单步撤销及 Spec 提示条**：
  - **Keep**（`KeepAgentTurn` / `⌘↵` / `CmdKeepAgentTurn`）把所有文件上剩余的改动合并提交为 `Editor.History` 中的**单个** `AuthorAgent` / `HistoryAuthorAgent` 撤销步骤（可通过 `History.Entries()`、`History.UndoAuthor()`、`History.RedoAuthor()` 区分用户与智能体）。一次 `⌘Z` 即可把所有被改动文件按字节还原，一次 `⇧⌘Z` 即可重做整个保留的回合，并弹出带 **Undo（`⌘Z`）** 按钮的 Spec `ToastSuccess`。
  - **Revert**（`RevertAgentTurn` / `⌘⌫` / `CmdRevertAgentTurn`）把每个被触及的文件按字节还原到请求前状态，并弹出带 **Undo** 按钮的 Spec `ToastInfo`，点击后（`UndoRevertAgentTurn`）可将回合重新恢复为待确认状态。
  - **Checkpoints `⌘Z` 规则**：智能体正在工作（`AgentTurnWorking`）时，`⌘Z` 仅撤销用户自己的编辑；在打开的审查（`AgentTurnReviewOpen`）中，`⌘Z` 先撤销用户在审查期间所做的编辑，再还原整个审查；保留或空闲后，`⌘Z` 按普通历史栈撤销。
- **右侧 Agent 标签页与摘要卡片（`RightRailView` / `AgentPanelView`）**：右侧栏顶部支持在 **Agent**（`mod+1` / `CmdAgentTab`）与 **Inspector**（`mod+2` / `CmdInspectorTab`）之间切换，并显示待确认状态点与 MCP 连接状态。界面严格使用 `Spec.dc.html` 令牌并对应 `Main.dc.html` 布局（完整支持中英文界面），展示请求气泡、进度步骤、工作中的 **Stop（`⌘.`）** 按钮、完成摘要、精简摘要卡片（`"6 changes · 1 file · +38 −0"`、按类型展示的变更列表与单条/按文件还原恢复按钮、**Keep** / **Review** / **Revert**）、已保留/已还原状态（`"one undo step (⌘Z)"`）以及带 `"Edits stay unsaved until you keep them"` 提示的输入框。
- **切换场景/标签页/项目、关闭与安全边界保护**：存在待确认回合时，打开其他场景（`RequestOpenScene`）、切换场景标签页（`RequestSelectTab`）、切换项目（`RequestOpenProject`）、关闭窗口或退出应用都会弹出 **Keep / Revert / Cancel** 确认框（`pendingTurnDialog`），与未保存修改的行为保持一致。所有暂存文件均强制校验项目根目录符号链接包含性、拒绝 `.git/**` 与“仅限本人”配置文件、限制单文件 2 MiB 上限，并在 `OpenProject` 与 `OpenDemo` 时彻底重置状态。
- **MCP 工具（`editormcp`）**：提供 `turn_start`、`turn_end`、`turn_status`（只读，如实报告已还原的变更）和 `file_edit`（在内存中校验并编辑 `game.yaml`、数据表、动画片段、预制体与场景），并为 `scene_edit` 与 `object_set_field` 增加 `why` 参数；按设计不提供可由智能体调用的保留或还原回合工具。
## 智能体面板的失败、恢复与离线状态（#1024）

当智能体请求未能直接成功完成时，`AgentPanelView` 会严格使用 `theme.Palette` 设计 Token（支持深浅外观）渲染 `design/editor/AgentStates.dc.html` 定义的六种状态卡片，并且在所有状态下保持选择、移动、检视器、`CheckProblems` 与 Play 等手动编辑工具正常工作：

- **部分完成（`AgentPartialState`）**：展示 `Done N of M · couldn't place <target>` 与原因说明，提供 **Keep these**（以可撤销修改保留已完成部分）、**Let me point**（`ToggleAgentPointing`，在 2D 视口点击通过 `PickAgentPointWorld` 选取世界坐标，按 `Esc` 取消）与 **Try another way**（`TryAnotherWayAgentTurn`），并在下方展示变更统计条（`N changes · 1 file | +X −Y | Review`）。
- **卡住暂停（`AgentStuckState`）**：当同一 MCP 工具（如 `scene_edit`）连续失败三次（`AgentStuckFailureThreshold = 3`）时，编辑器会立即暂停并取消当前请求而不再盲目重试，将状态胶囊切换为 `Waiting for you`，以等宽日志列出三次被拒绝的调用及其错误，并提供 **Hint for the agent** 输入框与 **Continue with hint**（`ContinueAgentWithHint`）或 **Stop here**（`StopStuckAgentTurn`，被拒编辑未触碰场景或磁盘文件）。
- **连接断开或模型失败（`AgentLostState`）**：当 MCP 连接或模型 API 在请求中途失败时，已完成的步骤保留在未采纳的 `AgentTurnReview` 中，中断步骤标记为 `cut off, nothing written` 且磁盘上不留半成品文件，卡片显示断开时间与所在步骤，并提供 **Reconnect**、**Review N changes** 与 **Troubleshoot**（模型错误如 529 过载时提供 **Retry now** 与 **Use Claude Code** 等回退切换）。重新连接后智能体不会自动继续，而是先询问用户是否从下一步接续（`ConfirmResumeLostAgentTurn` / `DismissResumeLostAgentTurn`）。
- **已停止（`AgentStoppedState`）**：停止运行中的请求后，卡片分区列出 `DONE` 与 `NOT DONE` 步骤，提供 **Resume**（`ResumeAgentTurn`，从首个未完成步骤开始继续，跳过已完成步骤不重复执行）与 **Review N changes**；已生成的改动在审查采纳前保持未保留状态。
- **摘要与差异不一致（`AgentMismatchReport`）**：编辑器会自动将每条智能体摘要与实际 diff 比对（`CheckAgentSummaryAgainstDiff`），当两者不一致时展示 `WHAT · SUMMARY · REVIEW` 对比表与已知原因（例如某对象在第 3 步被添加、第 5 步被移除），并提供 **Ask why**（`AskAgentWhyMismatch`）与 **Open review**（`OpenAgentTurnReview`）。点击 **Keep** 始终仅采纳实际审查差异，绝不采纳摘要中的虚构数量。
- **离线队列（`AgentStatusOffline`、`SetAgentOffline`）**：展示 `"You're offline"`，并在保持选择、移动、检视器、游戏检查与 Play 全部可用的同时，将提交的请求连同时间戳加入队列（`AgentQueuedRequests`、`QueueAgentRequest`、`RemoveQueuedAgentRequest`）。恢复联网后队列中的请求不会自动执行，而是逐条等待用户手动点击 **Send**（`SendQueuedAgentRequest`）发送。
## 规范视口叠加层与可切换图例（#1006）

2D 与 3D 视口中的所有语义叠加层都通过统一绘制辅助函数 `DrawObjectOverlay` 绘制，完全使用 `theme.Palette` 令牌（对应 `design/editor/Spec.dc.html` 中的 “Overlay legend”），且每一种类型都配备非颜色的第二视觉线索（角点手柄、胶囊图标/前缀、灰色幽灵填充、删除线或实心警告徽章），在灰度下依然彼此可辨：

| 类型（`OverlayKind`） | 令牌与线型 | 第二视觉线索 |
| --- | --- | --- |
| **选中**（`selection`） | `Accent`，1.5 px 实线 | 四个 8×8 角点手柄与强调色名称标签（仅表示用户的选中） |
| **智能体处理中**（`agent`） | `Agent`，1.5 px 实线，无手柄 | 1.6 秒周期在 100% 与 45% 之间脉冲，深色胶囊配四角星智能体图标（或编号任务徽章）说明正在执行的操作 |
| **已添加**（`added`） | `Added`，1.5 px 虚线 | 带 `"+ <名称>"` 文本的深色胶囊 |
| **已修改**（`changed`） | `Changed`，1.5 px 虚线 | 显示字段名及旧值与新值的深色胶囊（如 `"z 10 → 12"`） |
| **已移除**（`removed`） | `Removed`，1.5 px 实线 | 25% 透明度灰色幽灵块与带删除线对象名 + `"removed"` 的深色胶囊 |
| **问题**（`problem`） | `Problem`，活体对象上的 2 px 实线 | 实心填充胶囊（`Problem.Fg` / `Problem.OnFill`），带三角警告图标与错误信息，绝不会与已移除幽灵混淆 |
| **审查中聚焦的改动**（`focused`） | 沿用对应改动类型自身颜色与虚/实线，线宽 2.5 px（绝不使用 `Accent`） | 与该改动同色描边的醒目胶囊 |

- **同对象叠加、动效与减弱动态效果**：当**选中**与**智能体处理中**同时落在同一对象上（`PairedWithSelection`）时，选中框保留四个角点手柄，智能体外框向外偏移 3 px（`OverlaySelectionAgentGap`），其胶囊自动上移堆叠。已添加、已修改、已移除叠加层在 240 ms 内淡入（`OverlayChangeFadeDuration` / `OverlayFadeAlpha`），智能体处理中外框以 1.6 s 周期脉冲（`OverlayAgentPulsePeriod` / `OverlayAgentPulseAlpha`）。开启减弱动态效果（`core.Environment.ReduceMotion` 或 `Editor.SetReduceMotion(true)`）后，变更叠加层立即以 100% 出现，智能体外框保持 100% 实线且不再启动动画定时器。
- **左上角可切换图例（`viewport-overlay-legend`）**：所有编辑态视口（2D 与 3D）在左上角挂载同一张图例卡片（`viewportOverlayLegendView`），提供 6 行无障碍 `role="switch"` 开关（**选中**、**智能体处理中**、**已添加**、**已修改**、**已移除**、**问题**），每行都绘出对应线型与第二线索。切换任一行（`Editor.ToggleOverlayKind`、`Editor.SetOverlayKindVisible`、`Editor.SetOverlayLegendKinds`）会立即在所有视口隐藏或显示该类叠加层，并将 `overlayLegend` 持久化到项目之外的用户级 `EditorSettings` YAML 文件（`Editor.SettingsPath`，绝不修改项目或场景文件）。
- **实时场景叠加层接口与问题对象关联**：实时智能体编辑、审查与计划通过 `Editor.SetSceneOverlays`、`AddSceneOverlay`、`RemoveSceneOverlay` 和 `ClearSceneOverlays`（`SceneOverlayEntry`）将叠加层附加到场景对象或已移除幽灵边界上。`assetcheck.Problem` 现包含 `Object` 字段（同时通过 `editormcp` 的 `game_check` 返回），覆盖场景对象、组件、覆写与连接诊断，使问题面板中指向当前打开场景对象的诊断会自动在 2D 与 3D 视口中绘制 Spec **问题**叠加层。
- **中性视口参考线**：相机取景框、相机边界、相机连线、瓦片网格与对齐网格统一迁移到中性 `theme.Palette` 令牌（`TextMuted` / `TextFaint` 1 px 细线与 `2, 4` 点线），与所有语义图例线型完全区分。
## 智能体输入框：@-提及、上下文标签与上下文计量环（#1020）

在统一右侧栏的 **智能体** 标签页（`RightRailView` / `AgentPanelView`，可通过 `⌘1` / `⌘2` 经 `CmdAgentTab` / `CmdInspectorTab` 切换）中，`AgentComposerView`（对应 `design/editor/Threads.dc.html`，严格遵循 `Spec.dc.html` 令牌）提供 `@`-提及、显式上下文标签、精确悬停预览与实时上下文计量环：

- **基于项目搜索索引的 `@`-提及选择器**：在输入框中输入 `@` 会打开实时选择器，底层复用 `search.go`（`ensureProjectIndex`）、`Editor.Problems`、`PlaytestSession` / `AddComposerPlaytestRun` 与 `Editor.History`，显示匹配数量与 8 个分组（**对象**、**文件**、**预制体**、**资源**、**问题**、**试玩运行**、**历史步骤**、**规则**）及 9 个标签页（**全部**及各分组）。支持 `↑` / `↓` 移动（`MoveMentionSelection`）、`↵` 添加（`AcceptActiveMention`）、`Tab` / `Shift+Tab` 切换分组（`CycleMentionPickerGroup`）以及 `Esc` 关闭（`CloseMentionPicker`），已附加的项目会自动从候选列表中排除。
- **上下文标签与精确悬停预览**：输入框上方依次展示已附加标签（点击 `×` 通过 `RemoveComposerChip` 移除，或在空输入框中按 `⌫` 通过 `RemoveLastComposerChip` 移除）、**始终附加**标签（项目根目录中的 `brief.yaml` 与 `AGENTS.md`）以及可开关的 **视口图像** 标签（`SetViewportImageIncluded` / `ToggleViewportImage`）。在视口或场景树中选中对象只会显示虚线建议标签 `+ <名称> · 已选中`（`SuggestedChip`），除非主动添加（`AcceptSuggestedChip`），否则绝不会随请求发送；`TriggerAskAgentShortcut` 会在选中对象时附加该对象，未选中任何对象时附加整个打开的场景（`AttachWholeSceneChip`）并聚焦输入框。悬停任意标签（`SetHoveredChip` / `HoveredChipPayload`）都会通过 `ResolveChipPayload` 显示实际发送的精确头部（如 `# level_01.scene.yaml · lines 72–78`）及对应的 YAML 文本或图像。
- **单一数据源的请求组装与各执行器投递（`RenderAgentModelInput`，#1742）**：`AssembleAgentRequest` 仅从当前生效的标签组装 `AgentRequest`（调用与悬停预览完全相同的 `ResolveChipPayload`），并统一供 `SendComposerPrompt`（`Editor.OnSendAgentComposerRequest`）与 `SubmitAgentPrompt`（`Editor.AgentRunner`）使用。所有生产环境执行器统一通过 `RenderAgentModelInput` 渲染 `AgentRequest`，确保悬停预览的内容原样到达模型：
  - **Claude Code（`CLIAgentRunner`）**：通过 `--append-system-prompt` 传入**始终附加**标签（`brief.yaml`、`AGENTS.md`），在 `--` 之后传入本轮上下文块（压缩/新开摘要、显式附加标签与视口标题）及用户输入，并将视口 PNG 与临时参考图写入私有临时目录列于 `Reference images:` 中，执行结束后自动清理。
  - **Codex（`CLIAgentRunner`）**：在 `--` 之后传入完整上下文块（始终附加标签、压缩/新开摘要、显式附加标签与视口标题）及用户输入；全新 `codex exec` 会通过 `-i <path>` 传入校验后的项目图片路径与临时视口 PNG，`codex exec resume` 则将其列在 `Reference images:` 中。
  - **内置 Anthropic API 执行器**：将 `editormcp.Server` 服务说明（经 `AgentToolInstructionsProvider`）与**始终附加**标签放入 `system` 提示词，将本轮上下文块、用户输入以及参考图、图片资源标签和视口 PNG 的 base64 `image` 块（遵循 `PrivacyViewportImages` 隐私开关）作为用户消息发送，并为每个线程维护有界的对话历史（保留工具调用/结果并剔除早期回合的原始图片字节），从而支持连续追问、随回退/重做裁剪与恢复，并在**压缩线程**或**使用摘要新开**时清空旧历史。
- **上下文计量环、明细、压缩与新开线程**：20×20 环形进度指示器根据智能体回报的 token 用量（`SetAgentTokenUsage` 或 `AgentResponse.Usage`）呈现三种规范状态（正常 `≤ 70%`、超过 70% 警告 `71–89%`、以及 `≥ 90%` 的“立即压缩”状态）。点击计量环可展开 6 项明细（**Conversation**、**Open scene**、**Attached**、**Always**、**Tools and instructions**、**Viewport image**），并提供 **压缩线程**（`CompactAgentThread`，将较早请求折叠为摘要并完整保留最后一条请求，同时降低计量环占比）与 **使用摘要新开**（`StartFreshAgentThread`，以该摘要启动新线程）。
## AI 优先的主界面布局（#1007）

编辑器主窗口（`main_layout_ui.go`、`ui.go`、`agent_ui.go`、`overlays_ui.go`、`scenetree.go`）按 `design/editor/Main.dc.html` 实现 AI 优先工作区，并严格使用 `design/editor/Spec.dc.html` 的颜色令牌、尺寸比例与无障碍控件：

- **44 px 顶部栏（`mainHeaderView`）**：左侧展示 `<项目> / <场景>` 面包屑（`ProjectDisplayName`、`SceneBreadcrumbName`）、场景文件菜单及未保存圆点（`aria-label` 为 `"unsaved changes"` / `"save failed"`）；居中放置工具单选组（`Select V`、`Move W`、`Rotate E`、`Scale R`、`Pan Space`，纯图标 1.8 px@15 px `currentColor` Lucide 图标，悬停提示 `"名称 · 快捷键"`）与 `2D / 3D` 分段单选组；右侧为 `History (⌘Y)` 图标按钮、`Search ⌘K` 命令面板按钮，以及唯一主按钮 `Play (⌘P)`。
- **232 px 左侧栏（`leftRailSceneView`）**：包含 `Scene / Assets` 切换与 `SCENE · N nodes`（`SceneNodeCount`）标题、场景树快捷筛选开关（`SceneTreeFilter` / `SetSceneTreeFilter` / `SetSceneTreeFilterOpen`，按名称、ID 或类型即时筛选并自动展开祖先节点）、快捷添加 `+` 菜单（`AddSceneRailObject` 添加空对象、触发器或光源，以及 `AddSceneRailLayer` 添加图层，全部可经由 `History` 撤销）、带类型色点（`ObjectKindColor`）、类型标签（`SceneRailObjectKind`、`LayerKindLabel`）以及待审查新增对象 `NEW` 标记（`IsObjectProposedNew`、`SetProposedNewObjectIDs`）且支持省略截断不裁剪显隐/锁定按钮的场景树行；底部 `FILES` 区域列出当前打开的场景、场景引用的预制体/实例场景（`SceneUsedPrefabs`）以及 `game.yaml`。选中场景树中的节点会自动将右侧栏切换至**检视器（Inspector）**。
- **视口点阵、无精灵对象标记、标题、底部控制条与叠加层图例**：在 2D 编辑模式下，`DrawViewport` 在未设置场景背景色时绘制中性 Spec 16×16 点阵底纹（`theme.Palette.ViewportDot`），并为可见的无精灵对象绘制中性 Spec 标记方块，使空节点、无精灵预制体与触发器清晰可见且可直接点选，同时绝不与语义叠加层图例颜色混淆。`viewportCameraCaptionView` 订阅 `Editor.SubscribeCanvasSize`（`CanvasSizeRevision`），使 `Camera · W × H` 在首次布局与每次缩放窗口时紧贴相机框上方且避开左上角叠加层图例，并配以底部控制条（缩小、百分比、放大、`Snap` 开关 `base.Switch`、`Fit` 与 `Overlays N`）。
- **命令面板（`⌘K`、`PaletteItems`、`ExecutePaletteItem`）**：统一检索内置/自定义/插件命令、插件工具与面板、项目场景与文件、**当前场景节点**（`node:<id>`，选中节点、切换至检查器并自动聚焦）及 **设置页面**（`setting:<page>`）；支持作用域前缀（`>` 命令、`@` 节点、`#` 文件）、空查询时的**最近使用**分组（`PaletteRecent`），以及面向自然语言输入的**询问智能体**条目（`ask:<query>`，一键将提示词填入智能体面板输入框）。
- **380 px 右侧面板（`RightRailView`）**：承载 `Agent`（`⌘1`）与 `Inspector`（`⌘2`）单选标签、线程历史记录切换按钮及实时连接状态胶囊（`Connected · MCP`、`Working`、`Waiting for you`、`Reconnecting`、`Disconnected`）；点击场景树行会自动切到 **Inspector**。
- **32 px 状态栏（`mainStatusBarView`）**：在 `bg` 表面以弱化文本呈现左侧的活动图层按钮（可切换图层面板）、实时 2D 指针坐标与当前工具，以及右侧的问题数胶囊（打开问题面板）、输出（Output）与调试器（Debugger）。原工具栏中的其余面板（设置、混音器、输入映射、多语言、导出、资源库、数据表、项目搜索、文件、资源、动画、粒子、材质、HUD 布局、地形、导航网格）全部保留，可通过命令面板（`⌘K`）、菜单栏、设置窗口与状态栏访问。
- **空状态、加载状态与错误状态（`WorkspaceState`、`SceneOpenError`）**：各区域均为 `WorkspaceNoProject`、`WorkspaceNoScene`、`WorkspaceLoading`（`SetProjectLoading`）和 `WorkspaceSceneError`（`SceneOpenError`、`RetryOpenScene`、`ClearSceneOpenError`）提供专门的界面状态。打开缺失或 YAML 格式错误的场景文件时，视口会展示带有路径、行/列号、错误描述、修复建议、**重试**和**关闭**按钮的告警卡片，而非空白视口。
- **规范控件与键盘可达性**：遵循 Spec 尺寸（行与工具按钮 `28 px`、按钮 `30–32 px`、顶部栏 `44 px`、状态栏 `32 px`，无小于 `28 × 28` 的交互热区）、间距（`4 / 8 / 12 / 16 / 24`）、圆角（`6 / 8`）、`base.RadioGroup` / `base.Radio` 与 `base.Switch` 语义、2 px Spec 焦点环、完整无障碍名称，以及覆盖全部五个区域的 `F6` 循环导航（顶部栏、场景树、视口、智能体/检视器、状态栏）。
## 智能体面板实时步骤、权限确认卡与停止（#1012）

**智能体**面板（`RightRailView` 中的 `AgentPanelView`，`⌘1` / `⌘2` 切换）基于项目的 `permissions.yaml` 规则与“仅限你操作”边界，使用 `design/editor/Spec.dc.html` 令牌（`Working.dc.html`、`Permissions.dc.html`、`Main.dc.html`、`AgentStates.dc.html`）实时展示步骤进度、交互式权限确认卡、补充指令与全局停止控制：

- **实时步骤流与 `agent_report_step`**：发起请求（`Editor.StartAgentRequest` 或 `Editor.SubmitAgentPrompt`）后会立即显示灰色占位步骤（保证 300 ms 内可见）。当 `editormcp.Server` 执行工具调用时，每一步都会以平实语言实时流式呈现：当前唯一进行中的步骤显示紫色旋转图标，完成后切换为绿色对勾。智能体也可以调用 `agent_report_step`（`text`、可选 `objects`、可选 `status`）用自己的话汇报步骤、列出涉及的场景对象 ID，并接收用户通过「补充到此请求」排队的追加指令。
- **基于 `permissions.yaml` 的交互式权限确认卡**：在先通过“仅限你操作”边界检查后，当工具调用按 `permissions.yaml` 规则求值为 `ask` 时，`editormcp.Server` 会在其自身 goroutine 上暂停该调用而不阻塞编辑器 UI，并在智能体面板中渲染 `alertdialog`（`LiveRegionPolite`）权限确认卡，展示标题、请求原因、影响说明（例如 *“不会保存任何内容。”*）、规则预览与命中规则来源，以及**仅允许一次**、**始终允许**与**拒绝**按钮。
- **焦点纪律与键盘规则（规范规则 02、04、05）**：除非焦点已在智能体面板内部（`Editor.ShouldFocusAgentPrompt`），否则权限确认卡绝不抢夺焦点；点击顶部**等待你确认**状态条可按需聚焦到卡片。卡片获得焦点时，`⌘↵` 仅允许一次，`⌥↵` 通过 `Editor.ApproveAlwaysPermission` 将**始终允许**规则持久化写入 `permissions.yaml`（并弹出带**查看**与**撤销**按钮的提示条），后续同类调用不再弹窗，`⌘⌫` 拒绝，`↑`/`↓` 切换按钮，`Esc` 保持卡片未回答状态而智能体继续等待。
- **拒绝后永不自动重试**：拒绝权限确认卡会在当前请求中记录对应的 `(action, target)` 键，将该步骤显示为 *“你拒绝了 … · 已跳过继续”* 并提供显式的**立即允许**操作，在回合审查摘要中标明未检查项（例如 *“Not playtested”*），并对同一请求中重复发起的相同调用立即返回非致命的结构化 `denied` 结果而不再次弹出卡片。发起新请求时才会清除单次请求的拒绝记录。
- **补充到此请求、停止（`⌘.`）与并发手动编辑**：智能体工作或等待期间，手动编辑始终可用，底部状态栏会实时显示智能体当前动作（*“manual edits stay available”*）。工作态页脚的「补充到此请求」输入框（`Editor.AddToAgentRequest`）在步骤流式更新时保持焦点稳定。从任何位置（包括模态对话框内）点击**停止**或按下 `⌘.` 都会立即取消运行中的请求与等待中的权限确认（`Editor.StopAgent`），保留已完成步骤与场景编辑等待审查（期间暂停自动保存，直到保留或还原），并弹出带**审查**操作的**代理已停止**提示条；`Esc` 绝不会停止智能体。
## 可停靠面板与布局（#919）

`EditorView` 基于 NSGo 的 `ui.DockArea` 构建编辑器工作区，将场景树、图层、资源浏览器、文件、资源库、检查器（含右侧栏智能体、草图、任务、历史、问题修复与瓦片审查视图）、问题、输出、性能分析器、动画、状态机、瓦片集、3D 地形、2D 骨骼、粒子发射器、材质编辑器、场景环境、数据表、搜索、3D 导航网格、本地化以及所有已启用的插件面板统一为围绕视口（视口画布上方保留 `SceneTabsView` 场景标签栏）的可停靠、可调整大小的面板：

- **拖拽缩放、移动与标签页合并**：拖动任意分割或停靠区手柄调整面板尺寸，拖动任意面板标签页到另一面板组内合并为标签页或拖至边缘进行分割，并通过标签页关闭按钮隐藏面板（`viewport` 视口面板保持不可关闭）。
- **菜单显示、隐藏与重置**：通过 **View > Panels** 菜单勾选或取消显示任意内置面板与已启用插件面板（`PanelVisible`、`SetPanelVisible`、`TogglePanel`、`Panels`），或选择 **View > Reset Layout** / **View > Panels > Reset Layout**（`CmdResetLayout`、`ResetLayout`）恢复默认布局。
- **按用户保存布局（`LayoutFile`、`ParseLayout`、`EncodeLayout`）**：布局改动会自动以严格版本化 YAML 保存在所有项目之外的用户配置目录 `<UserConfigDir>/nsgo-game/layout.yaml`（`DefaultLayoutPath`，通过 `Editor.LayoutPath` / `Editor.SetLayoutPath` 配置，并由 `game editor` 与 `editorcmd` 自动接入）。下次启动时恢复已保存的分割、标签组、当前活动标签页、尺寸与隐藏面板；不再存在的面板（例如已卸载插件的面板）会在恢复时自动丢弃。
- **插件面板位置**：插件面板（`Plugin.Panels`）首次使用时出现在其声明的默认 `Dock`（`DockLeft`、`DockRight` 或 `DockBottom`）；一旦用户将其移动或合并到其他位置，之后每次启动都会恢复到用户移动后的位置。

## 逐文件审查智能体的 Go 代码更改（#1034）

当智能体在一个回合中同时修改了 Go 代码与场景、预制体或数据 YAML 文件时，编辑器会打开专用的逐文件 Go 代码审查界面（`CodeReviewView`，对照 `design/editor/ReviewCode.dc.html` 与 `Review.dc.html`，在深色与浅色外观下均仅使用 `theme.Palette` 令牌）：

- **待确认 Go 代码隔离与 Go 构建覆盖层检查**：回合中提交的 Go 文件（`ProposeCodeReviewFiles` 或 `editormcp` 的 `code_review_propose`）在确认并保留之前仅暂存于内存中，磁盘上的项目文件与运行模式（Play）仍继续执行旧代码。`RunCodeReviewChecks` / `RunCodeReviewChecksAsync` 会生成临时 Go 构建覆盖层 JSON（`BuildGoOverlay`，同时映射规范路径与符号链接解析后的路径，兼容 macOS `/var` → `/private/var`），并通过 `GOFLAGS=-overlay=<overlay.json>` 在不触碰磁盘项目文件的前提下执行 `go tool nsgo check <pkg>` 与 `go tool nsgo test -json <pkg> -count=1`（可通过 `Editor.CodeReviewCheckRunner` 注入替换）。
- **分组文件列表与统一 / 分栏差异视图**：左侧栏拆分为 **PREFABS · IN KEEP**（随保留一同应用的 YAML/预制体文件）和 **GO · CONFIRM EACH FILE**（每个 Go 文件显示 `+n −n` 行数变化与 **Needs OK** / **OK** 徽章），底部附带说明卡片（*“为什么 Go 代码需要单独确认”*）。Go 差异支持 **Unified（统一）**（`u`）与 **Split（分栏）**（`s`）切换，展示旧/新行号、右侧行标签（`fails test`、`failed here`、`agent edited`、`round 2`）以及在外部 IDE 打开按钮（`Editor.ExternalEditorName`、`OpenCodeReviewInExternalEditor`，依次调用 `Editor.OnOpenSourceFile`、`Editor.OnOpenFileAtLine` 或系统默认打开器）。
- **检查状态条、失败测试卡片与同回合第 2 轮修复**：状态条（`RoleStatus` + `LiveRegionPolite`）展示 `nsgo check`、`nsgo test` 通过/失败计数、耗时与包路径、**Run again**（`r`）以及运行中的实时进度（`"nsgo test running · M of N"` / `"Agent is fixing · nsgo test running · M of N"` 及 `"Agent edited <file>:<line>"` 徽章）。失败测试卡片会展示测试名、`file:line` 错误输出和智能体的原因说明，并提供 **Ask the agent to fix**（`AskAgentToFixCodeReview`、`Editor.OnAskCodeReviewFix`，在同一回合内作为 Round 2 修复并重新运行检查）与 **Open the test**（`OpenCodeReviewTestFile`）。
- **逐文件确认、保留、等待卡片与单步撤销**：在最新待定代码通过 `nsgo check` 和 `nsgo test` 之前，**Confirm `<file>`**（`ConfirmCodeReviewFile`）保持禁用并返回 `ErrCodeReviewChecksNotPassed`；检查通过后解锁，确认后切换为 **Confirmed · undo**。**Keep**（`KeepCodeReview` / `⌘↵`）会将保留的 YAML 文件与**已确认**的 Go 文件作为 `Editor.History` 中的**单个撤销步骤**原子写入磁盘，一次 `Editor.Undo()` 即可还原旧 YAML/Go 文件并移除新建的 Go 文件。若保留时仍有未确认的 Go 文件，则仅写入 YAML 更改并在 **Agent** 面板保留一张等待卡片（说明哪些文件等待确认及其在运行模式中的影响，并附带 **Review** 按钮直接回到未确认的 Go 文件）。按 `Esc`（`CloseCodeReview`）可暂停审查而不丢弃更改，**Revert all**（`RevertCodeReview` / `⌘⌫`）则直接丢弃待定更改且不触碰磁盘。
- **命令与 MCP 工具**：命令 `CmdReviewCode`（`agent.reviewCode`）用于打开或恢复进行中的 Go 审查。`editormcp` 提供 `code_review_propose`、`code_review_status` 与 `code_review_run_checks`，使智能体可在不阻塞 UI goroutine 的情况下提交待审 Go 代码、读取失败详情、在同一回合提交 Round 2 修复并重新运行覆盖层检查。
## 回退智能体对话线程（#1021）

可将智能体对话回退到任意已发送消息之前（对应 `design/editor/Checkpoints.dc.html`，全部界面仅使用 `design/editor/Spec.dc.html` 设计 Token）：

- **每轮文件与场景检查点**：每次通过输入框发送请求（`SubmitAgentPrompt`）前，编辑器会为项目磁盘文件（跳过 `.git`、`.nsgo`、`node_modules`、符号链接以及仅限用户配置文件）以及当前打开的场景与 `game.yaml` 拍摄前快照，并在回合结束或停止时压缩为差异检查点（`AgentTurn.ChangedFiles`、`AgentTurn.SessionIDBefore`、`AgentTurn.SessionIDAfter`）。
- **悬停或聚焦显示“回退到此处”与三种选项**：在 `AgentPanelView` 中悬停或键盘聚焦任意已发送的用户消息即可显示 **Rewind to here** / **回退到此处**（`OpenAgentRewindPopover` / `ToggleAgentRewindPopover`），打开“回退到此消息之前”弹层（`AgentRewindPreview`），提供三个选项（`AgentRewindMode`）、**Cancel** / **取消**（或按 `Esc`）与 **Rewind** / **回退**（`ConfirmAgentRewind` / `RewindAgentTurn`）：
  - **文件与对话（`AgentRewindBoth`）**：从检查点还原目标回合及之后所有受影响的文件（并同步刷新当前场景、其他打开的场景标签页、`game.yaml` 与文件树），从对话中移除后续请求与回复，并将 `AgentSessionID` 恢复至 `SessionIDBefore`，使下一次提问直接从该消息之前继续会话。
  - **仅文件（`AgentRewindFilesOnly`）**：从回合检查点还原文件，同时保持对话记录与会话不变。
  - **仅对话（`AgentRewindConversationOnly`）**：移除目标及后续回合与会话推进，同时保持磁盘文件现状不变。
- **回退横幅、重做与消息回到输入框**：回退完成后，被回退的消息会重新填入输入框并以规范强调色边框高亮（`AgentComposerRewound`），方便修改后重新发送；输入框上方会显示状态横幅（`AgentRewindBanner`）说明已回退的范围，并提供 **Redo** / **重做** 按钮（`RedoAgentRewind` / `CanRedoAgentRewind`）一键恢复文件、对话回合、会话 ID 与原有输入框草稿。
## 智能体执行前提出计划、澄清问题与规划虚影（#1035）

对于较大规模的修改（例如依据项目 `permissions.yaml` 规则会修改 Go 代码、新建文件或跨多个文件的操作，`EvaluatePlanPermissions`、`PlanNeededForEdit`），智能体会在修改任何场景或文件前先提出结构化计划（对应 `design/editor/Plan.dc.html`，严格遵循 `Spec.dc.html` 深色与浅色令牌）：

- **MCP 计划提案与进度工具（`plan_propose` 与 `plan_step_update`）**：调用 `editormcp` 的 `plan_propose`（`ActionRead`，批准前绝不修改任何文件）会在编辑器中以草稿态（`PlanPhaseDraft`）展示 `AgentPlan`，包含标题、动态摘要（如 `"5 steps · 3 files · 10 objects · about 2 min · ~35k tokens"`）、带可视化预览（房间草图 `room` 或时序条 `strip`）的澄清问题、带文件清单/新增说明/带上下限步进器（`PlanStepField`）/权限徽章（如 `Edit Go`、`Run the game`）/规划场景树节点与视口虚影的编号步骤，并等待用户决定后返回结构化的 `AgentPlanDecision`（包含回答、修改后的数量、步骤备注与移除步骤）。`plan_step_update` 用于更新执行进度并在完成时将计划步骤清单（`AgentPlanReview`）写入该回合的审查视图。
- **智能体面板中的交互式计划卡片（`agentPlanCardView`）**：
  - 澄清问题以单选选项卡片呈现，选中后自动折叠为一行摘要并提供**修改（Change）**按钮（`ReopenPlanQuestion`）。所有问题回答完毕前**批准执行（`⌘↵`）**保持禁用并拒绝提交（`ApproveAgentPlan`）；**直接执行（Just do it）**（`JustDoItAgentPlan`）会自动采纳每个未答问题的推荐选项并显示 `"Picked for you: …"`。
  - 每个步骤支持展开内联编辑器修改数值与备注（`EditPlanStep`、`AdjustPlanStepField`、`SetPlanStepNote`），以及移除与恢复（`RemovePlanStep`、`RestorePlanStep`），并在**本计划涉及的文件**列表中为仅属于已移除步骤的文件添加删除线。
  - 底部提示“批准前不会更改任何文件”，说明触发计划的权限原因，并提供可点击的**权限设置**链接直接打开 **设置 › 权限**（`SettingsPagePermissions`）。
- **视口规划虚影与场景树标记**：
  - `drawPlanGhostsOverlay` 以智能体紫虚线框和带步骤编号的胶囊标签绘制规划虚影（`ResolvedPlanGhosts`），当回答改变选项（通过 `PlanGhostOverride` 移动门或拉杆）或步进器调整数量（`RepeatField`）时实时更新；视口左上角展示图例（`"planned · not in the file yet"`），底部悬浮栏提供**计划虚影（Plan ghosts）**开关（`TogglePlanGhosts`）。
  - `SceneTreeView` 顶部显示规划节点计数，在受影响的现有图层旁显示步骤徽章，列出带虚线紫色标记的规划节点行，并在底部固定显示**本计划涉及的文件**（`ResolvedPlanFiles`）。
- **执行中暂停手动编辑、停止与回合审查**：
  - 进入执行态（`PlanPhaseRunning`）后，顶部标题栏显示状态胶囊（`"Agent is working · step k of N · manual edits to planned objects are paused"`），当前活动步骤的虚影变为实线，并暂停对被规划对象的手动修改（`IsObjectPausedByPlan`）。
  - 在第 1 步写入任何内容前点击**停止**、按 `Esc` 或 `⌘.`（`StopAgentPlan`），所有文件保持原样（`NothingWritten: true`），底部显示“已在第 1 步前停止，未写入任何内容”并提供**编辑计划**（`EditStoppedPlan`）与**重新运行**（`RunStoppedPlanAgain`）。
  - 计划完成后（`CompleteAgentPlan` 或 `plan_step_update` 传入 `complete: true`），该回合的审查视图（`AgentTurn.Plan`、`agentTurnPlanReviewView`）会列出计划的全部步骤并标明已完成或已跳过的条目。使用 `⌘L` 发起的小修改仍通过 `SkipPlan: true` 跳过计划。

## 设计令牌（#1002）

编辑器中的每一处色彩、字号、间距阶梯、圆角、高度与动效时长都统一来自嵌入的 YAML 主题文件与常量定义（`pkg/editor/theme/theme.yaml` 与 `pkg/editor/theme/theme.go`），与 `design/editor/Spec.dc.html` 及 `design/editor/Viewport3D.dc.html` 保持一致：

- **嵌入 YAML 主题（`theme.yaml`、`theme.Core`、`theme.Palette`）**：通过 `core.ParseTheme` 解析并在 `EditorView` 根作用域通过 `cx.SetTheme(theme.Core())` 生效。NSGo 的所有 `core.Palette` 与 `ui.ColorsOf` 角色（`Background`、`Surface`、`Raised`、`Line`、`Border`、`Text`、`TextMuted`、`Accent`、`AccentText`、`Success`、`Warning`、`Danger`、`Focus`）均在深色与浅色外观下直接映射到规范调色板；`Editor.Palette(cx)`（以及 `theme.Dark()`、`theme.Light()`、`theme.PaletteFor(dark)`）同时提供 NSGo 未命名的规范角色（`TextPrimary` / 强文本、`TextSecondary` / 正文、`TextMuted` / 次级文本、`TextFaint` / 弱化文本、`OnTextPrimary`、`Viewport`、`ViewportDot`、`PillBg`、`HoverOverlay`、`ActiveOverlay`、`PrimaryHover`、`AccentHover`、`AccentPressed`、`Viewport3DGround`、`Viewport3DGrid`、`Accent`、`Agent`、`Added`、`Changed`、`Removed`/`Problem`、含专用 `Fill` 与 `Edge` 的 `Warning`，以及 3D 坐标轴令牌 `AxisX`、`AxisY`、`AxisZ`）。
- **深色模式色彩令牌（`Spec.dc.html` 与 `Viewport3D.dc.html`）**：
  - 表面与边框：`bg #101216`、`panel #12141A`、`raised #171A21`、`line #1F232A`、`line raised #262B35`、`viewport #15181D`、`viewportDot #2A2F38`。
  - 四级文本：`text strong #FFFFFF`、`text #D9DDE5`、`text secondary #B4BAC6`、`text muted #8B92A0`。已退役的 `#6F7684`、`#5D6472` 与 `#4A505C` 不再用于文本。
  - 语义角色：`accent #3B7BFF`（填充上的文字使用 `#101216` 且字重 600，绝不在深色强调色或智能体色填充上使用 `#FFFFFF`）、`agent #9B7BFF`、`added #4FD38A`、`changed #E9A84A`、`removed · problem #FF6B6B`、`warning #F2C14E`（填充 `#2A2414`、描边 `#4A3F1F`）。
  - 3D 坐标轴（`Viewport3D.dc.html`）：`axisX #FF6B6B`、`axisY #4FD38A`、`axisZ #3B7BFF`（浅色模式对应通过 WCAG 校验的 `#C92A2A`、`#0D7A43`、`#1D5CE6`）。
- **实测 WCAG 2.2 对比度表（`theme.ContrastTable`、`theme.ContrastRatio`）**：`TestContrastTableDarkAndLight` 直接从 `theme.yaml` 计算深色与浅色两套主题中每一组前景/背景、实心填充文字、警告卡片、胶囊标签、视口叠加层与 3D 坐标轴的 WCAG 2.2 亮度对比度；常规文本及填充文字低于 `4.5:1`、UI 边界与叠加层低于 `3.0:1` 即报错，并包含回归测试验证将深色 `text_muted` 退回 `#6F7684` 时测试必然失败。
- **排版、间距、圆角、高度与动效常量**：
  - 排版：Inter 标题 `14 / 600`（`theme.FontTitleSize`、`theme.FontTitleWeight`）、正文 `12.5 / 400`（`theme.FontBodySize`、`theme.FontBodyWeight`）、分区标题 `11 / 600` 加 `+6%` 字距（`theme.FontSectionSize`、`theme.FontSectionWeight`、`theme.FontSectionTrack`）、代码 JetBrains Mono `11.5`（`theme.FontCodeSize`、`theme.FontFamilyMono`）。
  - 间距：仅使用 `4`、`8`、`12`、`16`、`24` px（`theme.Space4`–`theme.Space24`）。
  - 圆角与几何：控件 `6`（`theme.RadiusControl`）、按钮 `8`（`theme.RadiusButton`）、卡片与轻提示 `12`（`theme.RadiusCard`）、对话框 `18`（`theme.RadiusDialog`）；行与工具按钮高 `28`（`theme.RowHeight`，最小命中热区 `theme.MinHitSize = 28`）、按钮高 `30`–`32`（`theme.ButtonHeight`、`theme.ButtonTallHeight`）、顶栏高 `44`（`theme.HeaderHeight`）；`1` px 内描边（`theme.BorderWidth`）。
  - 动效：悬停与按压 `120 ms`（`theme.MotionHover`）、面板/浮层/轻提示 `180 ms`（`theme.MotionPanel`）、智能体叠加层淡入 `240 ms`（`theme.MotionOverlay`）、智能体脉冲 `1.6 s`（`theme.MotionAgentPulse`，透明度 `100%` ↔ `45%`）。开启 `Env.ReduceMotion` 时，`theme.MotionDuration` 返回 `0`，`theme.AgentPulseOpacity` 保持 `1.0` 不闪烁。
- **禁止硬编码 UI 颜色字面量与仅深色包级别名**：`pkg/editor` 中除 `theme/theme.go` 外的所有非测试 `.go` 文件在绘制界面元素、操纵杆、叠加层与调试视图时仅使用响应式的 `theme.Palette` 令牌（`ed.Palette(cx)` / `e.activePalette()`，场景内容数据如 `scene.BackgroundColor` 保留为场景数据）。`TestSpecNoHardcodedColorLiteralsInEditor` 拒绝在 `theme/theme.go` 之外出现任何 `core.RGB`/`core.RGBA` 调用或硬编码 UI 十六进制颜色，`TestSpecNoDarkOnlyPackageColorVarsInEditor` 拒绝由 `theme.Dark()`/`theme.Light()` 初始化的包级颜色变量，确保所有编辑器页面在深色与浅色外观下均可实时重绘。
