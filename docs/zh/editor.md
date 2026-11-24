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
- `BuildNodes3D(doc, scene.Root())` 构建 `scene3d` 节点树：每个图层一个分组节点（带有图层的可见性），每个对象一个以其 ID 命名的节点，`UserData` 指向对应的 `ObjectDoc`。`ObjectLocalTransform3D` 给出每个节点的局部变换；`rotX`、`rotY`、`rotZ` 按此顺序绕固定轴旋转，与编辑器 3D 视口对顶层对象使用的顺序相同。

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

编辑器在移动、删除和撤销对象时会连同其子对象一起处理，并且不会分配已被嵌套子对象占用的 ID。它会拒绝放置嵌套超过 `MaxObjectDepth` 层或含有切变精灵的树，也会拒绝会让子对象精灵产生切变的缩放。2D 视口通过 `Build` 绘制嵌套的子对象；3D 视口只绘制和拾取顶层对象，目前在两个视口中都还不支持选择和编辑嵌套的子对象。

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

在编辑器中，`Editor.SetComponentRegistry(reg)` 会先检查当前打开的场景，之后 `OpenFile` 报告组件错误时会带上文件和行号，`SetComponentField` 与 `PlaceObject` 会拒绝注册表不允许的字段。更换注册表会清空撤销/重做历史，因此新注册表不允许的已撤销编辑无法再重做。检查器按声明顺序列出组件的每个已注册字段，场景中未设置的字段显示其默认值；字段标签的提示信息给出说明、种类、范围和默认值，数值输入框受范围限制，并按字段的 `Step` 步进（未设置时整数为 1、浮点数为 0.1）。浮点数按完整精度显示，注册表拒绝的编辑会显示在状态栏中。没有注册类型的组件，或者没有设置注册表时的所有组件，仍按文件中的值显示。

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

`Connect(doc, reg, ctx)` 检查场景的组件和连接，返回一个 `*Signals`。`Signals.Emit(from, event, payload)` 按文件中列出的顺序调用该事件每条连接的处理函数，已经运行过的 `once` 连接会跳过。每个处理函数收到一个 `Call`：`Signals` 本身（处理函数可以继续发出事件，最多嵌套 `MaxEmitDepth` 层）、传给 `Connect` 的 `ctx`（如游戏状态或 `*BuiltScene`）、场景中源对象与目标对象的 `*ObjectDoc`、事件与处理函数所属的组件和名称、包含全部参数的 `Args`（连接给的值或默认值），以及 `payload`。场景中没有的对象、其已注册组件都没有声明的事件，`Emit` 都会拒绝。实例化或继承了其他场景文件的场景要在 `Resolve` 之后连接（或使用 `BuiltScene.Doc`）；`Signals` 不能并发使用。

`Parse` 检查连接的形式：四个名称都已填写，`event` 和 `handler` 写成名称或 `component.name`，参数是布尔、有限数字或字符串，没有重复的连接；场景不引用其他场景文件时，`from` 和 `to` 还必须是场景里的对象。提供注册表时，`ParseWith` 还会按注册表检查这些名称，并以带文件、行号和列号的 `*ConnectionError` 报告：对象的已注册组件都没有声明的事件或处理函数、被对象的两个组件同时声明的裸名称、处理函数没有对应参数的实参，以及类型不对或超出范围的实参。错误会列出该对象实际声明了什么：

```text
scenes/hall.scene.yaml:16:14: sceneformat: connection 0 ("button" pressed -> "door" opne): no registered component of object "door" declares handler "opne" (it has: door.open)
```

`ValidateConnections` 对已解析的文档做同样的检查，但不带位置。`Register` 拒绝名称为空、含有点号或与该类型已有名称重复的事件和处理函数，处理函数参数的检查规则与字段相同；只用于检查文件的注册表（如编辑器的）可以不设 `Func`，但 `Connect` 会拒绝它。

`Resolve` 会带上连接：实例化场景的连接随实例一起进来，其中的对象名与对象 ID 一样加上前缀（`door_1/button`），因此预制件的每个副本都保留自己的连线；继承的场景保留基础场景的连接，并把自己的连接加在后面。场景可以连接它所实例化或继承的场景中的对象，这些名称在解析之后才检查。在编辑器中，`DeleteObject` 会同时删除来自或指向被删对象及其后代的连接，撤销删除时把它们放回原处。

### 项目

编辑器也能打开游戏的[项目文件](project.md)：`OpenProject` 读取 `game.yaml` 并打开其中的主场景，`EditProject` 以可撤销的步骤修改它，`SaveProject` 写入它并同步旁边的 `nsgo.yaml`。运行 `go tool game editor <dir>` 即可打开一个项目目录。`MoveFile(from, to)` 移动项目中的文件或目录，并像 `go tool game mv` 一样改写所有指向它的引用，包括磁盘上的文件和当前打开的场景与项目（见[稳定引用](project.md#稳定引用)）。

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
func main() {
	ed, err := editor.New(nil, atlases)
	if err != nil {
		log.Fatal(err)
	}
	if err := ed.SetComponentRegistry(components); err != nil {
		log.Fatal(err)
	}
	if err := ed.RegisterPlugin(health.Plugin()); err != nil {
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
- **工具**（`PluginTool`）：工具栏中位于内置 2D 工具旁的一个按钮。当它是当前工具时（`SetPluginTool`，此时 `Tool` 为 `ToolPlugin`），在视口上按下会以画布坐标调用 `Press(ed, x, y)`（用 `CanvasToWorld` 转换），设置了 `Draw` 时，它会在编辑模式下绘制在场景之上。选择其他工具或关闭插件都会结束该工具。

插件只通过编辑器的方法修改场景，例如 `SetComponentField`、`PlaceObject` 和 `MoveObject`，或者在 `Editor.History` 上执行自己的 `Command`，因此它的每一次编辑都与其他编辑一起撤销和重做。`RegisterPlugin` 会拒绝没有名称或名称已注册的插件、缺少 ID、标题、标签、组件或函数的面板、检查器或工具、含控制字符的 ID 或组件名、未知的停靠位置，以及 ID 或组件重复的面板、检查器或工具。测试 ID 用 `-` 连接插件名和 ID（`plugin-panel-<plugin>-<id>`、`tool-<plugin>-<id>`、`inspector-plugin-<plugin>-<component>`），因此一起使用的插件，其名称和 ID 不应连接成相同的字符串。

`SetPluginEnabled(name, on)` 把已注册的插件加入 `editor.plugins` 或从中移除，作为项目的一次可撤销编辑，由工具栏的“Save”按钮写入；移除最后一个插件时，如果 `editor` 小节中已没有其他内容，就会一并移除该小节（包括作者自己写的 `editor: {}`），撤销会将其恢复。`RegisteredPlugins`、`ActivePlugins` 和 `PluginEnabled` 报告程序包含哪些插件、哪些已启用。

编辑器的测试中有一个示例插件 `health`：它添加一个列出每个对象生命值的面板（带有一键回满全部对象的按钮）、一个带生命值输入框的 `health` 组件检查器，以及一个在视口按下处放置受伤对象的工具（`packages/game/editor/plugin_test.go`）。

## 库面板（#493）

当 `Editor.Library` 设为一个包库（`library.Open`）时（`go tool game editor -library <索引>` 就会这样设置），资源浏览器旁会出现一个面板，列出库中每个包的类型、版本、描述以及打开的项目已安装的版本。面板上的 Install、Update 和 Remove 按钮调用 `InstallPack` 和 `RemovePack`：把包的文件写到 `game.yaml` 旁边，在项目中记录包及其新增的自动加载条目和插件，然后保存项目；插件包列出的已注册插件会立即启用。正在运行、没有打开项目或项目有未保存修改（`ErrProjectDirty`）时它们会拒绝执行；由于写入的文件无法撤销，它们会清空撤销历史。从 https 库获取包时不会阻塞事件循环，包到达之前按钮显示为忙碌状态（`FetchingPack`）。库的格式以及安装会写入什么，见[资源与插件库](library.md)。
