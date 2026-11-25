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
2. **瓦片绘制**：直接绘制内联瓦片网格，或预览引用的 Tiled `.tmj` 瓦片地图。
3. **带类型的属性与组件编辑**：查看并编辑 `components` 与 `properties`，并按场景规范实时校验。
4. **撤销 / 重做**：基于命令栈的场景文档编辑历史。
5. **编辑器内运行（Play-in-Editor）**：不离开工具，直接在编辑器的 `Canvas` 中运行真实的 `game.Loop` 与 `BuiltScene`。
6. **资源浏览**：浏览项目中的图集、帧、动画与瓦片地图，并拖放到图层中。

不在初期编辑器范围内：自定义脚本语言、专用着色器/材质连线图，以及私有二进制工程格式。

---

## 场景格式（`version: 2`）（#160）

```go
import "github.com/nsgo-dev/game/packages/game/sceneformat"
```

### 顶层字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `version` | `int` | 必填的格式版本号：`2`（`CurrentVersion`），或者对未使用任何版本 2 新增字段的场景为 `1`。`Encode` 原样写回。 |
| `uid` | `string` | 可选的稳定 ID，文件改名或移动后保持不变，由 `go tool game index` 写入（两个版本均可用，见[稳定引用](project.md#稳定引用)）。`Resolve` 保留场景自身的 uid，而不是基础场景的。 |
| `inherits` | `string` | 可选，从项目根目录起、以斜杠分隔的基础场景路径，本场景继承该场景（版本 2，见下文“实例化与继承”）。 |
| `name` | `string` | 可选的场景可读名称。 |
| `navMesh` | `string` | 可选，从项目根目录起、以斜杠分隔的场景烘焙[导航网格文件](navbake3d.md)路径，由 `go tool game navmesh` 写入（两个版本均可用）。继承其他场景时，它会替换基础场景的值。 |
| `virtualWidth`、`virtualHeight` | `float32` | 虚拟视口尺寸（单位为点）。两者必须同时为 `0` 或同时为正数。 |
| `backgroundColor` | `string` | 可选的 CSS 颜色字符串，由 `core.ParseColor` 解析（如 `"#141824"`）。在绘制各图层前填充 `(0, 0, virtualWidth, virtualHeight)`。 |
| `camera` | 对象 | 可选的默认 2D 相机配置，作用于世界空间图层（`x`、`y`、`zoom`、`rotation`，以及可选的 `bounds`）。 |
| `tilemaps` | 映射 | 可选的命名瓦片地图定义，供图层通过 `tilemap.ref` 引用。 |
| `layerOverrides` | 映射 | 按名称修改继承来的图层：`space`、`order`、`visible`、`tilemap`（版本 2，需要 `inherits`）。 |
| `overrides` | 映射 | 按 ID 修改继承来的对象（版本 2，需要 `inherits`）。 |
| `layers` | 列表 | 有序图层列表，按 `order` 从小到大绘制，`order` 相同时保持文档顺序。 |
| `connections` | 列表 | 可选，把一个对象的事件连接到另一个对象的处理函数，写在图层之后（两个版本均可用，见[连接](#连接490)）。 |

### 相机（Camera）

- `x`、`y`：对齐到虚拟视口中心 `(virtualWidth/2, virtualHeight/2)` 的世界坐标（虚拟尺寸为 `0` 时对齐到原点 `(0, 0)`）。
- `zoom`：正缩放倍数，省略时默认为 `1`。
- `rotation`：相机在屏幕上顺时针旋转的弧度；世界图层会绕 `(x, y)` 反向旋转 `-rotation`。
- `bounds`：可选的 `{x, y, width, height}` 世界边界矩形，用于限制可见范围（`width > 0`、`height > 0`）。
- 省略 `camera` 时，世界图层按单位变换绘制。

### 瓦片地图（Tilemaps）

瓦片地图（位于顶层 `tilemaps` 或内联在图层上）只能使用以下三种模式之一：

- **引用（`ref`，仅限图层）**：指向顶层 `tilemaps` 中的命名条目。
- **外部 Tiled 地图（`source`）**：指向 `.tmj` 文件路径，构建时由 `TilemapResolver` 解析。
- **内联网格**：包含 `tileWidth`、`tileHeight`、`width`、`height`、`tileset`（帧名按从 1 开始的瓦片 ID `"1"`、`"2"`…… 索引的图集名），以及长度为 `width * height` 的行主序 `data`（`0` 表示空瓦片）。

### 图层与对象

每个图层包含：

- `name`（必填，在场景内唯一）
- `space`：`"world"`（省略时的默认值，受相机变换影响）或 `"screen"`（直接在画布坐标中绘制，忽略相机）
- `order`：整数绘制顺序（越小越先绘制）
- `visible`：布尔值，省略时默认为 `true`
- `tilemap`：可选瓦片地图，绘制在本图层所有对象之下
- `objects`：放置在本图层中的对象列表

每个对象包含：

- `id`（必填，在整个场景内唯一）
- `name`、`prefab`（可选）
- 变换：`x`、`y`、`originX`、`originY`、`rotation`（弧度，顺时针）、`scaleX`（默认 `1`）、`scaleY`（默认 `1`）、`flipX`、`flipY`、`alpha`（范围 `[0, 1]`，默认 `1`）、`z`（图层内绘制顺序，越小越先绘制）
- `sprite`：可选的 `{atlas, frame}`，引用 `sprite.Atlas` 及其中的帧名
- `material`：可选，从项目根目录起、以斜杠分隔的共享材质文件路径，例如 `materials/stone.material.yaml`，在构建时通过 `MaterialResolver` 解析（见 [resource](resources.md)）
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

built, err := sceneformat.Build(doc, sceneformat.AtlasMap{
	"cave_tiles": tilesAtlas,
	"actors":     actorsAtlas,
	"ui":         uiAtlas,
})
if err != nil {
	return err
}

// 在 core.Canvas 的 Draw 回调中：
built.Draw(d)
```

`Parse` 会拒绝未知 YAML 字段、多余的 YAML 文档、不支持的版本号、版本 1 场景中的版本 2 字段、不是从项目根目录起以斜杠分隔的场景引用路径、缺少所修改的 `scene` 或 `inherits` 的覆盖、重复的图层名或对象 ID（包括嵌套的子对象）、深度超过 `MaxObjectDepth` 的对象树、非法坐标系、错误颜色值以及非有限或越界的变换参数。`Encode` 会先校验再输出确定性的 2 空格缩进 YAML，通过 `Parse` 可无损往返还原。

### Schema

场景格式的 JSON Schema 发布在 `https://game.nsgo.dev/schemas/scene.v1.json`（`sceneformat.SchemaURL`），实例化或继承其他场景文件的版本 2 场景则用 `https://game.nsgo.dev/schemas/scene.v2.json`（`sceneformat.SchemaURLV2`）。`Encode` 在每个文件开头写入与其版本对应的 `# yaml-language-server: $schema=…` 这一行（`sceneformat.SchemaCommentFor`），VS Code 的 YAML 扩展以及其他带 YAML 语言服务器的编辑器可借此在编写场景时校验和补全。参见 [YAML 文件的 Schema](schemas.md)。

### 组件类型

`ComponentRegistry` 用 Go 代码显式声明场景文件可以使用的组件类型，运行时不依赖反射：每个类型的名称和说明，以及每个字段的名称、种类（`FieldBool`、`FieldInt`、`FieldFloat` 或 `FieldString`）、默认值、可选的闭区间范围、供检查器使用的可选 `Step` 和说明。`Register` 会拒绝空名称或重复名称、非数值字段上的范围或步长、最小值大于最大值、`FieldInt` 上的小数范围或步长、负步长，以及种类不符或超出范围的默认值。

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

在编辑器中，`Editor.SetComponentRegistry(reg)` 会先检查当前打开场景的组件，并像 `OpenFile` 一样检查其连接，之后 `OpenFile` 报告组件错误时会带上文件和行号，`SetComponentField` 与 `PlaceObject` 会拒绝注册表不允许的字段。更换注册表会清空撤销/重做历史，因此新注册表不允许的已撤销编辑无法再重做。检查器按声明顺序列出组件的每个已注册字段，场景中未设置的字段显示其默认值；字段标签的提示信息给出说明、种类、范围和默认值，数值输入框受范围限制，并按字段的 `Step` 步进（未设置时整数为 1、浮点数为 0.1）。浮点数按完整精度显示，注册表拒绝的编辑会显示在状态栏中。没有注册类型的组件，或者没有设置注册表时的所有组件，仍按文件中的值显示。

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

`go tool game check` 会解析每个实例化或继承其他场景的场景。场景无法解析时（例如循环引用，或覆盖项指向不存在的对象）它会失败，并报告实例所在行或 `inherits` 行；连接的 `from` 或 `to` 指向解析后场景中不存在的对象时也会失败，并报告写有该名称的行。事件和处理函数在 Go 中声明，命令行无法加载它们，所以游戏在自己的测试中用 `assetcheck.CheckWith` 和自己的注册表检查它们。它会在文件和行上报告指向对象未声明的事件、处理函数或参数的连接，以及不符合类型的组件；对实例化或继承其他场景的场景，它还会检查覆盖项写入的组件和解析后场景的每个连接，所以覆盖项破坏了被实例化或被继承的场景带来的连接时（例如再加一个声明同名事件的组件），会在该覆盖项处失败：

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
- **属性**：Add Property 接受一个键和一种类型（`string`、`int`、`float` 或 `bool`），添加的属性取该类型的零值（`AddProperty(id, key, kind)`）；该字段会去掉键首尾的空白，空键、首尾带空白的键或对象已有的键会被拒绝。值为整数的浮点属性保存时带小数点（`2.0`），因此再次打开时仍是浮点数。每个属性的删除按钮会删除该属性（`DeleteProperty`）。
- **组件**：Add Component 字段会筛选对象尚未拥有的已注册类型，按下其中一个即添加该组件，并写入每个已注册字段的默认值，让文件能看出组件的作用（`AddComponent(id, name)`）。按回车会添加字段中写出的类型，或唯一匹配的那个类型。对象已有的组件、注册表不认识的名称，以及首尾带空白或含有 `.`（连接在 `component.event` 中使用它）的名称都会被拒绝；没有注册表时，该字段可以添加任意名称、不含字段的组件。如果新组件声明的事件或处理器被某个连接以裸名称引用，而对象的另一个组件也声明了它，添加也会被拒绝，因为连接将无法说明指的是哪一个：请先把它写成 `component.event`。在实例化或继承其他场景的场景中，这项检查针对通过编辑器的解析器解析后的场景进行，包括它所引用场景的连接；如果解析器无法加载场景文件，或者即使不加新组件场景也无法解析和绑定，则跳过检查。每个组件的移除按钮会移除该组件（`RemoveComponent(id, name)`），连同所有需要它的连接：从该对象发出、事件由该组件声明的连接，以及指向该对象、处理器由该组件声明的连接，无论写成裸名称还是 `component.name`；裸名称通过组件的注册类型来归属，因此没有注册表、或注册表不认识该类型时，只会移除写成 `component.name` 的连接。`ComponentConnections(id, name)` 列出这些连接，撤销会把组件和连接放回原处。没有注册类型的组件仍按文件中的值显示，字段照常可编辑；由于没有结构定义，其字段中值为整数的浮点数保存为 `2`，再次打开时是整数。

### 项目

编辑器也能打开游戏的[项目文件](project.md)：`OpenProject` 读取 `game.yaml` 并打开其中的主场景，`EditProject` 以可撤销的步骤修改它，`SaveProject` 写入它并同步旁边的 `nsgo.yaml`。运行 `go tool game editor <dir>` 即可打开一个项目目录。`MoveFile(from, to)` 移动项目中的文件或目录，并像 `go tool game mv` 一样改写所有指向它的引用，包括磁盘上的文件和当前打开的场景与项目（见[稳定引用](project.md#稳定引用)）；这是一个步骤，可以撤销，也可以重做。撤销会把移动改写过的每个文件逐字节恢复原样；如果某个文件在移动之后又被改过，则只把其中的引用改写回去。

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

命中测试、放置、移动、瓦片绘制、选中轮廓和插件工具都经过该视图。`CanvasToWorld` 与 `WorldToCanvas` 在画布坐标与世界坐标之间转换；`CanvasToScreen` 与 `ScreenToCanvas` 在画布坐标与场景的虚拟屏幕（屏幕空间图层所在的空间）之间转换；`ScreenToWorld` 与 `WorldToScreen` 只应用场景相机，与游戏中相同。`CanvasSize` 是画布布局后的尺寸，在视口完成布局（`SetCanvasSize`）之前为场景的虚拟尺寸；3D 视口填满同一块画布。

## 瓦片调色板（#874）

在 2D 视图中启用 Paint、Erase 或 Fill 工具时，视口右侧会显示瓦片调色板。它把当前图层瓦片集的瓦片按瓦片 ID 顺序排成网格，每个瓦片按瓦片地图的瓦片尺寸显示（比面板还宽的瓦片会缩小到面板宽度）。这些瓦片是瓦片集图集中名称为瓦片 ID（`"1"`、`"2"`……）的帧，与场景格式查找瓦片的方式一致；其他帧不会列出。点击一个瓦片，或用方向键在瓦片之间移动，即可把它设为 Paint 和 Fill 写入的瓦片（`SetActiveTileID`）；所有方向键都按 ID 顺序逐个移动，左键和上键移到上一个瓦片，右键和下键移到下一个，而不是按网格的行移动。选中的瓦片带有一圈高亮，使用 Paint 或 Fill 时状态栏显示它的 ID。选择瓦片不算编辑，不进入撤销历史；之后的绘制是一个可撤销的步骤。

调色板跟随当前图层：图层改变时，调色板显示新图层的瓦片集；如果该瓦片集没有所选 ID 的瓦片，就选中它的第一个瓦片。没有内联瓦片地图的图层（没有瓦片地图，或使用外部 Tiled 地图）、解析器无法解析的瓦片集、没有瓦片帧的瓦片集，都会显示一条说明原因的消息，而不是空网格。在 Go 中，`TilePalette()` 返回调色板为当前图层显示的内容：瓦片集、瓦片尺寸和瓦片，或那条消息。

## 场景树（#861）

编辑器左侧的场景树面板（`SceneTreeView`）列出打开场景的各个图层，并在每个图层下列出其对象及嵌套的子对象，相当于 Godot 的 Scene 面板和 Unity 的 Hierarchy。每次编辑和撤销后它都会随之更新。按下对象所在的行会选中该对象；在任一视口中选中对象时，会选中它的行并展开其上层的各行；按下图层的行会把它设为当前图层。所有对象编辑对嵌套子对象与顶层对象一样有效：`FindObject` 和 `SelectObject` 在整棵树中按 ID 查找对象，检查器编辑所选的子对象，其变换在父对象的空间中编辑，因此移动工具会把它的原点放到按下的位置，3D 操纵器沿世界坐标轴移动它。

- **在树中移动**：把一行拖到另一行上，它就成为该对象的最后一个子对象；拖到那一行的上边缘或下边缘附近，则放在该对象之前或之后；拖到图层的行上，则移到该图层的顶层。行的上下文菜单可以把对象在同级之间上移或下移、设为上方同级对象的子对象、移出父对象，或移到另一个图层。在 Go 中，`ReparentObject(id, parentID, index)`、`MoveObjectToLayer(id, layer, index)`、`ReorderObject(id, index)` 和 `DropObject(id, targetID, place)` 完成同样的操作，每次都是一个可撤销的步骤；index 小于 0 或超出末尾时，对象放在最后。
- **世界变换**：被移动的对象保持其世界变换。它在新父对象下的局部变换会在 2D 中重新计算（`x`、`y`、`rotation`、`scaleX`、`scaleY`），撤销会把每个字段精确还原；翻转过的对象会在原来的轴上保留负的缩放。当对象本身或其新旧祖先设置了 3D 字段时，它的深度（`posZ`、`scaleZ`）也会保持。当其中某个对象设置了 3D 旋转（`rotX`、`rotY`、`rotZ`）时，2D 与 3D 视图对 `x` 和 `y` 指向哪里不再一致，此时只保持 3D 世界变换：对象在 3D 视口中保持不动，在 2D 视口中则可能移动。以下移动会被拒绝：把对象放到它自己或其后代之下、嵌套超过 `MaxObjectDepth` 层，或者在新父对象下得到的变换无法写成平移、旋转和缩放，例如把精灵放到一个经过旋转且沿单轴拉伸的父对象下。在世界图层与屏幕图层之间移动时，保留的是数值，而不是对象在屏幕上的位置。
- **重命名**：F2、双击或上下文菜单中的“Rename”会在所选对象的行内编辑其名称；按回车或离开输入框即保留修改，按 Escape 则放弃。“Change ID”通过 `SetObjectID(oldID, newID)` 编辑其 ID：在同一步中改写场景中引用该对象的连接，以及它实例化的场景中的对象（`enemy_1/body` 变为 `boss/body`），还有打开的动画片段的目标及其各轨道的目标，并拒绝场景已在使用的 ID，包括实例化或继承的场景带来的 ID。实例化或继承本场景、并按旧 ID 覆盖该对象的其他场景文件不会被改写；这类引用请用 `game mv` 移动或手动修改。
- **眼睛与锁**：行上的眼睛按钮在两个视口中隐藏该对象及其后代，锁按钮使两个视口中的按下操作不再拾取它，按下会落到它下面的对象上（`SetObjectHidden`、`SetObjectLocked`）。两个视口都不会移动隐藏或锁定的对象：不显示它的 3D 操纵器，移动工具也不会移动它。二者都属于编辑会话：它们不是编辑，撤销不会影响它们，也从不写入场景文件；运行（Play）时绘制所有对象，打开另一个场景时会清除它们。隐藏或锁定的对象仍可在场景树中选中，并在检查器中编辑。

3D 视口会绘制并拾取所有对象，包括嵌套的子对象：`Objects3D` 按深度优先列出它们，并给出各自父对象的 ID；子对象的包围盒由其祖先的变换确定位置。

## 场景文件与未保存的修改（#866）

编辑器一次编辑一个场景，无需重启即可在项目的场景之间切换。在文件浏览器中选中一个场景（`*.scene.yaml` 或 `*.scene.yml` 文件，或 `autoload.scenes` 中列出的场景）后点击“Open”，或者连按两下，即可打开它（`OpenScene`）。无论从文件浏览器、Problems 面板、搜索与引用面板、命令面板还是通过 `OpenScene` 打开场景，都会清除 `OnSave`，因为它负责保存的是之前打开的场景。`OpenFile`、`SaveFile` 和 `OpenProject` 会在场景文件位于打开的项目中时设置 `ScenePath`，否则将其清空。工具栏的“File”菜单中有：“New Scene”，把一个最小的有效场景（以文件名命名，大小与项目的 display 或窗口相同，带一个 world 图层）写到作者给出的、从项目根目录算起的路径（`NewScene`）；“Save As”，把场景另存到另一个路径，并让该文件成为场景的文件（`SaveSceneAs`）；“Revert”，把场景恢复为其文件中保存的样子（`RevertScene`）。没有 `.yaml` 或 `.yml` 扩展名的路径会加上 `.scene.yaml`（以 `.scene` 结尾的路径加上 `.yaml`），`game check` 和 `game index` 就是按这个名称识别场景的。New Scene 和 Save As 都不会覆盖其他文件，也不会把场景写到项目自身文件的名称 `game.yaml` 和 `nsgo.yaml` 下。Revert 是一个可撤销的步骤，撤销即可找回被恢复掉的修改。

`SceneDirty` 报告场景是否与打开时或上次保存时的内容不同，它像 `ProjectDirty` 对 `game.yaml` 那样比较编码结果，所以撤销回到已保存的状态后，场景又会变为未修改。若 `MoveFile` 改写了打开的场景的文件，场景保持原来的已修改或未修改状态；若它只改写了打开的场景中的引用而没有改写其文件（例如 `game mv` 不读取的场景：名称不是 `.scene.yaml`，也没有任何文件引用它），场景会变为已修改，保存后即可保留新的路径。工具栏显示打开的场景的路径（`SceneLabel`），场景或项目有未保存的修改（`HasUnsavedChanges`）时还会显示一个 `*`，窗口标题也随之变化（`WindowTitle`）。窗口标题由 `EditorView` 设置，因此基于本包构建的编辑器（例如游戏自己的编辑器）应通过 `Editor.AppName` 给出自己的名称，而不是自行设置标题；标题以该名称结尾（为空时为“NSGo Scene Editor”）。“Save”按钮、菜单栏的 File > Save、命令面板中的 Save 和 Mod+S（`Save`，即 `CmdSave` 命令）只保存修改过的内容：项目、场景或两者；未修改的文件保持原样，注释也会保留。有未保存的修改时，打开另一个场景（从文件浏览器、Problems 面板、搜索与引用结果或命令面板）、New Scene、退出编辑器和关闭其窗口都会先询问：“Save”保存修改后继续，“Discard”丢弃修改（项目的修改也一并丢弃）后继续，“Cancel”保留修改并停留在当前场景。丢弃项目的修改时，由项目决定的状态也会像撤销这些修改时一样恢复，例如通过这些修改新增的音频总线进行的试听会停止。强制退出（如注销时）无法等待回答，不会询问。

## 动画编辑器

编辑器的动画面板在时间轴上为任何已注册属性打关键帧，并把片段保存为 [`animation` 资源文件](resources.md#animation)，由 [animclip](animclip.md) 在游戏中播放（相当于 AnimationPlayer）。打开片段时，面板显示在视口下方。

- **打开片段**：工具栏的 Animate 按钮为选中的对象新建一个一秒长的片段。在 Go 中，`NewClip(target, name, length)` 作用相同；`OpenClipFile(path, target)` 和 `OpenClipYAML(data, target)` 打开片段文件，并按组件注册表检查（拼错的属性会带着文件名打开失败）；`CloseClip` 关闭片段。`go tool game editor scene.yaml clip.animation.yaml [object-id]` 打开一个场景并选中对象 `object-id`（省略时为场景中的第一个对象），片段文件存在时在该对象上打开片段，面板的 Save Clip 按钮会写回该文件。
- **打关键帧**：选择一个属性（对象字段，如 `x` 或 `alpha`，或已注册的组件字段，如 `body.speed`），按 Key 在播放头处以该属性在场景中的当前值打关键帧；`KeyProperty(target, property, at)` 作用相同。选中的是其他对象时，关键帧会进入以该对象为目标的轨道。显式指定片段所属对象的轨道与目标为空的轨道视为同一条轨道，两条轨道驱动同一对象同一属性的片段会被拒绝。布尔关键帧保持其值直到下一个关键帧。
- **编辑关键帧**：每条轨道是一行显示其关键帧的通道。按下关键帧即选中它并把播放头移到该处，然后可以编辑它的时间、值和插值方式（`linear`、`smooth` 或 `step`），或删除它；在 Go 中对应 `SetKeyValue`、`MoveKey`、`SetKeyInterp`、`DeleteKey` 和 `RemoveClipTrack`。值会按属性检查：布尔为 0 或 1，整数为整数值，并且在字段范围内。
- **片段设置与事件**：面板可编辑片段的名称、长度和循环（`SetClipName`、`SetClipLength`、`SetClipLoop`），并在播放头处添加具名事件（`AddClipEvent`、`DeleteClipEvent`）。
- **拖动与预览**：拖动条移动播放头（`SeekClip`；在循环片段的末尾显示最后的关键帧），Play 在 `game.Loop` 上播放片段（`PlayClip`、`PauseClip`，或用 `StepClip` 手动步进）。此时视口（2D 与 3D 均如此）显示按片段摆好姿态的场景副本，使用的 `animclip.Player` 和 `BindScene` 与游戏相同，因此正在编辑的场景不会被写入动画值，片段在游戏中的播放效果也与预览一致。场景无法播放的片段（例如轨道目标对象已被删除）仍保持打开，`ClipError` 会说明原因。
- **撤销**：每一次片段编辑，包括打开和关闭，都与场景编辑共用同一个撤销/重做历史。

`SaveClipYAML` 和 `SaveClipFile` 输出确定性的 YAML，因此打开后再次保存的片段不会改变。

## 编辑器插件（#492）

插件为场景编辑器添加自己的面板、自己组件类型的检查器以及 2D 视口工具，作用与 Godot 的 `EditorPlugin` 相同。Go 无法在所有平台上于运行时加载代码，因此插件是编译进编辑器程序的 Go 代码：编辑器程序用 `Editor.RegisterPlugin` 注册插件，项目在其[项目文件](project.md)中列出插件名即可启用：

```yaml
editor:
  plugins: [health]
```

已注册但未被当前项目列出的插件保持关闭；`editor.plugins` 的顺序就是插件面板和工具出现的顺序。`OpenProject` 会在状态栏中列出项目要求、但本程序编译时没有包含的插件（`MissingPlugins`）。`go tool game editor` 不注册任何插件，因此有自己插件的游戏要构建自己的编辑器命令：

```go
// cmd/editor/main.go：游戏自己的编辑器命令。
package main

func main() {
	ed, err := editor.New(nil, atlases) // 游戏的精灵图集，作为 sceneformat.AssetResolver
	if err != nil {
		log.Fatal(err)
	}
	if err := ed.SetComponentRegistry(health.Components()); err != nil {
		log.Fatal(err)
	}
	if err := ed.RegisterPlugin(healthplugin.Plugin()); err != nil {
		log.Fatal(err)
	}
	if err := ed.OpenProject("."); err != nil {
		log.Fatal(err)
	}
	nsgo.Run(func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgo.WindowSize(1280, 800))
}
```

`editor.Plugin` 有一个 `Name`，规则与 `game.yaml` 中其他名称相同（不含空白和控制字符），以及三个列表：

- **面板**（`Panel`）：停靠在视口左侧（`DockLeft`，位于资源浏览器之后）、检查器右侧（`DockRight`）或视口下方（`DockBottom`）的视图，上方显示其 `Title`。`View(cx, ed, rev)` 在面板出现时构建一次，只要其插件处于启用状态，面板就保持挂载并保留其状态，不受其他面板增减的影响；与编辑器自带的面板一样，它在响应式函数中读取 `rev`，以跟随编辑器状态的每一次变化。没有面板的停靠区不占空间。
- **检查器**（`Inspector`）：检查器用 `View(cx, ed, rev, objectID)` 在组件标题下方绘制 `Component` 指定的组件，取代该组件的通用字段。`Editor.ComponentField` 读取一个字段；场景未设置该字段时返回其注册的默认值，对象没有该组件时返回 false。与通用字段一样，检查器的控件应在 `Playing` 返回 true 时禁用，编辑器拒绝的编辑应通过 `Editor.Report` 显示在状态栏中。两个已启用的插件绘制同一个组件时，`editor.plugins` 中排在前面的插件生效。
- **工具**（`PluginTool`）：工具栏中位于内置 2D 工具旁的一个按钮。当它是当前工具时（`SetPluginTool`，此时 `Tool` 为 `ToolPlugin`），在视口上按下会以画布坐标调用 `Press(ed, x, y)`（用 `CanvasToWorld` 转换为世界坐标，用 `CanvasToScreen` 转换为屏幕空间图层的坐标），设置了 `Draw` 时，它会在编辑模式下绘制在场景之上。选择其他工具或关闭插件都会结束该工具。

插件只通过编辑器的方法修改场景，例如 `SetComponentField`、`PlaceObject` 和 `MoveObject`，或者在 `Editor.History` 上执行自己的 `Command`，因此它的每一次编辑都与其他编辑一起撤销和重做。`RegisterPlugin` 会拒绝没有名称或名称已注册的插件、缺少 ID、标题、标签、组件或函数的面板、检查器或工具、含控制字符的 ID 或组件名、未知的停靠位置，以及 ID 或组件重复的面板、检查器或工具。测试 ID 用 `-` 连接插件名和 ID（`plugin-panel-<plugin>-<id>`、`tool-<plugin>-<id>`、`inspector-plugin-<plugin>-<component>`），因此一起使用的插件，其名称和 ID 不应连接成相同的字符串。

`SetPluginEnabled(name, on)` 把已注册的插件加入 `editor.plugins` 或从中移除，作为项目的一次可撤销编辑，由工具栏的“Save”按钮写入；移除最后一个插件时，如果 `editor` 小节中已没有其他内容，就会一并移除该小节（包括作者自己写的 `editor: {}`），撤销会将其恢复。`RegisteredPlugins`、`ActivePlugins` 和 `PluginEnabled` 报告程序包含哪些插件、哪些已启用。

### 随游戏发布带插件的编辑器

游戏把编辑器作为自己模块中、与游戏并列的又一个命令来发布，在项目目录中用 `go run ./cmd/editor` 运行。把插件放在只有编辑器命令导入的独立包中，这样发布的游戏不会链接编辑器；把组件类型放在游戏和编辑器都导入的包中，这样两者用同一组字段检查场景：

| 包 | 导入 | 内容 |
| --- | --- | --- |
| 组件包 | `sceneformat` | 组件类型（`ComponentType`）、供 `ParseWith` 和 `SetComponentRegistry` 使用的注册表，以及按默认值读取组件的辅助函数（`ComponentRegistry.Resolve`） |
| 插件包 | `editor` 和组件包 | 返回 `editor.Plugin` 的 `Plugin()` |
| 游戏（`main`） | 组件包 | 用 `sceneformat.ParseWith` 和注册表加载场景 |
| `cmd/editor` | `editor`、组件包和插件包 | 注册组件类型和插件，然后打开项目 |

`game.yaml` 在 `editor.plugins` 下列出插件，因此游戏的编辑器打开项目时插件总是启用的。不应在缺少插件时启动的编辑器命令，在 `OpenProject` 之后检查 `MissingPlugins`，不为空就失败退出；而用 `go tool game editor` 打开同一个项目仍然可以打开，它会在状态栏中列出缺少的插件，并显示通用字段。

[Field Medic](https://github.com/nsgo-dev/game/tree/main/examples/fieldmedic)（`examples/fieldmedic`）就是按这种方式组织的可运行游戏。它的病人带有 `health` 组件（`health` 包），它的 `cmd/editor` 注册了 `health` 插件（`healthplugin` 包）：一个停靠在检查器右侧的 Ward 面板，列出每个病人的生命值，其中的 Heal All 按钮在 `Editor.History` 上执行一个可撤销步骤，一次治好所有病人；一个 `health` 组件的检查器，带生命值和上限两个字段以及剩余生命值的条形图；以及一个 Admit 工具，在视口按下处放置一个病人，并在工具启用时为每个病人画出轮廓。它的测试在编辑器中打开游戏自己的项目，操作面板、检查器和工具。编辑器自身的测试中也有一个更小的 `health` 插件（`packages/game/editor/plugin_test.go`）。

## 库面板（#493）

当 `Editor.Library` 设为一个包库（`library.Open`）时（`go tool game editor -library <索引>` 就会这样设置），资源浏览器旁会出现一个面板，列出库中每个包的类型、版本、描述以及打开的项目已安装的版本。面板上的 Install、Update 和 Remove 按钮调用 `InstallPack` 和 `RemovePack`：把包的文件写到 `game.yaml` 旁边，在项目中记录包及其新增的自动加载条目和插件，然后保存项目；插件包列出的已注册插件会立即启用。正在运行、没有打开项目或项目有未保存修改（`ErrProjectDirty`）时它们会拒绝执行；由于写入的文件无法撤销，它们会清空撤销历史。从 https 库获取包时不会阻塞事件循环，包到达之前按钮显示为忙碌状态（`FetchingPack`）。库的格式以及安装会写入什么，见[资源与插件库](library.md)。

## 项目设置面板（#907）

打开项目后，工具栏的 **Settings** 按钮（`ProjectSettingsOpen` / `SetProjectSettingsOpen`）会在视口下方切换显示 `ProjectSettingsPanelView`，完全通过 `EditProject` 编辑 `game.yaml`：每一次修改都经过 `project.Validate` 校验，纳入撤销/重做历史，并在点击 **Save**（`SaveProject`）时同时写回 `game.yaml` 与同步更新的 `nsgo.yaml`：

- **常规（General）**：编辑 `name`、`id` 和 `appVersion`（`SetProjectName`、`SetProjectID`、`SetProjectAppVersion`）。
- **窗口（Window）**：编辑 `window.title`、`width`/`height`、`minWidth`/`minHeight`/`maxWidth`/`maxHeight` 尺寸限制、`resizable` 和 `appearance`（`SetProjectWindowTitle`、`SetProjectWindowSize`、`SetProjectWindowLimits`、`SetProjectWindowResizable`、`SetProjectWindowAppearance`）。
- **显示（Display）**：开关可选的 `display` 小节（`SetProjectDisplayEnabled`），并编辑虚拟分辨率 `width`/`height`、`mode`（`fit`、`fill`、`stretch`、`expand`）、`integerScale`、`pixelSnap` 与 `barColor`（`SetProjectDisplaySize`、`SetProjectDisplayMode`、`SetProjectDisplayIntegerScale`、`SetProjectDisplayPixelSnap`、`SetProjectDisplayBarColor`）。
- **自动加载（Autoload）**：选择 `autoload.mainScene`（`SetProjectMainScene`），并增改删 `autoload.scenes` 与 `autoload.resources` 条目（`AddProjectAutoloadScene`、`UpdateProjectAutoloadScene`、`RemoveProjectAutoloadScene`、`AddProjectAutoloadResource`、`UpdateProjectAutoloadResource`、`RemoveProjectAutoloadResource`），文件路径可从当前项目文件列表中搜索选择（`ProjectSceneFiles`、`ProjectResourceFiles`）。
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

每行显示一条问题的文件路径、从 1 开始的行号（有行号时）和错误信息，与在同一项目上运行 `go tool game check` 报告的问题一致，并补充 `project.Check` 发现的项目级问题（例如 `nsgo.yaml` 与 `game.yaml` 不一致、已安装包的文件缺失，或非 `.scene.yaml` 扩展名的自动加载场景无效）。点击某一行（`OpenProblem`）时，若该文件是场景（`*.scene.yaml`、`*.scene.yml` 或 `autoload.scenes` 中列出的场景）则直接打开该场景；若是其他项目文件或无法解析的场景，则在文件浏览器中选中该文件（`SelectedFile`、`SelectFile`）。问题位于已打开的场景时只选中该文件，不会重新加载场景；有未保存的修改时打开另一个场景，会像文件浏览器那样先询问保存、丢弃还是取消。在 Go 代码中，可从任意协程安全调用 `Problems`、`ProblemCount`、`ProblemErr`、`CheckingProblems` 和 `WaitForProblems` 读取或等待后台检查结果。

## 导出与发布构建（#916）

打开项目后，工具栏的 **Export** 按钮（`ExportPanelOpen` / `SetExportPanelOpen`）会在视口下方切换显示 `ExportPanelView`，用于在编辑器中直接编辑 `game.yaml` 的 `export` 小节并执行发布构建：

- **预设与输出目录**：编辑 `export.output`（`SetExportOutput`），添加、删除和重排各平台预设（`AddExportPreset`、`RemoveExportPreset`、`MoveExportPreset`），并仅从各平台合法目标 `project.TargetsOf(platform)` 中选择构建目标（`SetExportPresetTargets`、`ToggleExportPresetTarget`）。所有修改都通过 `EditProject` 完成（经 `project.Validate` 校验并支持撤销/重做），在 `export` 为空时自动清理该小节，点击 **Save**（`SaveProject`）时写出规范 YAML 并同步 `nsgo.yaml`，与 `gameproject targets` 保持一致。
- **鸣谢（Credits）预检**：面板打开、刷新（`CheckExportCredits`）或开始构建前，都会执行与 `game release` 相同的 `tooling/credits` 扫描。任何缺少鸣谢信息的资源都会连同其对应的 `*.import.yaml` 边车路径一并列出，并在补全前阻止发布构建；补全鸣谢后刷新即可清除告警，成功构建时会在输出目录写入 `CREDITS.md`。
- **构建、演练与输出流**：支持构建全部预设或勾选指定目标（`ExportSelectedTargets`），并可配置分发渠道（`SetExportChannel`）、发布说明文件（`SetExportReleaseNotes`）以及 **Dry Run** 开关（`SetExportDryRun`）。`StartExportBuild` / `RunExportBuild` 与 `go tool game release` 共用同一套发布命令规划器，因此演练构建会在 UI 协程之外将完全一致的 `nsgo build` 命令行流式输出到面板的 Output 区域，非演练构建成功后自动打开解析出的输出目录（`OpenOutputFolder` / `OpenExportOutputFolder`）。
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
  - 文件与编辑：`mod+s`（保存）、`mod+z`（撤销）、`mod+shift+z`（重做）、`mod+d`（复制选中对象）、`delete` / `backspace`（删除选中对象）。
  - 2D 工具：`v`（选择）、`m`（移动）、`p`（放置）、`b`（绘制瓦片）、`e`（擦除瓦片）、`g`（填充瓦片）。
  - 3D 变换控件与视图：`w`（平移）、`r`（旋转）、`s`（缩放）、`alt+2`（2D 视图）、`alt+3`（3D 视图）、`f`（聚焦选中对象：2D 下通过 `FrameSelection2D` 调整独立编辑器视图 `View2D` 而不改动保存的 `Doc.Camera`，3D 下调整 `Camera3D`）、`mod+k`（命令面板）。
  - 项目：`f5`（播放 / 停止）、`mod+r`（运行游戏：优先调用 `Editor.OnRunGame`，未配置且已挂载作用域时回退到编辑器内运行）、`mod+,`（项目设置）、`mod+shift+e`（导出与发布）、`mod+shift+b`（检查问题）。
- **桌面菜单栏**：`EditorView` 通过 `desktop.SetApplicationMenu`（`Editor.ApplicationMenu`）注册原生应用菜单栏（`File`、`Edit`、`View`、`Project`），以实时可用/勾选状态和对应平台的快捷键提示暴露同一套命令；已启用插件的工具列在 `View > Tools` 下，已启用插件的命令列在 `Project > Plugins` 下。
- **命令面板（`ui.CommandDialog`）**：按 `mod+k`（或点击工具栏 **Commands** 按钮 / `View > Command Palette...`）即可打开 `CommandPaletteView`，支持按子串排序与模糊匹配搜索全部内置与自定义命令、已启用插件命令、已启用插件工具（通过 `SetPluginTool` 激活）、已启用插件面板（通过 `FocusPluginPanel` 聚焦）以及当前项目的场景和文件（通过 `OpenProjectFile` 打开场景或选中文件）。
- **插件命令**：插件可通过 `Commands []PluginCommand` 声明带可选 `Shortcut` 的命令。`RegisterPlugin` 会规范化快捷键（`NormalizeShortcut`），并在改动编辑器状态之前拒绝任何在 macOS 或 Windows/Linux 上与内置命令、自定义命令或其他已注册插件冲突的快捷键。
