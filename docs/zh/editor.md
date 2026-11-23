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

## 场景格式（`version: 1`）（#160）

```go
import "github.com/nsgo-dev/game/packages/game/sceneformat"
```

### 顶层字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `version` | `int` | 必填的格式版本号，必须为 `1`。 |
| `name` | `string` | 可选的场景可读名称。 |
| `virtualWidth`、`virtualHeight` | `float32` | 虚拟视口尺寸（单位为点）。两者必须同时为 `0` 或同时为正数。 |
| `backgroundColor` | `string` | 可选的 CSS 颜色字符串，由 `core.ParseColor` 解析（如 `"#141824"`）。在绘制各图层前填充 `(0, 0, virtualWidth, virtualHeight)`。 |
| `camera` | 对象 | 可选的默认 2D 相机配置，作用于世界空间图层（`x`、`y`、`zoom`、`rotation`，以及可选的 `bounds`）。 |
| `tilemaps` | 映射 | 可选的命名瓦片地图定义，供图层通过 `tilemap.ref` 引用。 |
| `layers` | 列表 | 有序图层列表，按 `order` 从小到大绘制，`order` 相同时保持文档顺序。 |

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

- `Build` 把每个 `BuiltObject` 与其 `Parent`、`Children` 关联起来，把 `World` 设为复合后的 2D 仿射变换、`WorldAlpha` 设为复合后的透明度，并按世界位置、旋转和缩放绘制每个子对象的精灵。复合变换带有切变（非等比缩放的父对象下有旋转的子对象）的精灵无法作为精灵绘制，`Validate` 会拒绝它，`Parse`、`Encode` 和 `Build` 因此也会拒绝；`CheckObjectTree` 对单棵树做同样的检查以及嵌套层数检查。父对象在某一轴上缩放为零（例如 `scaleX: 0`）不算切变：其子对象的精灵以零缩放绘制。没有精灵的对象则在 `World` 中保留精确矩阵。`BuiltLayer.Objects` 列出图层的顶层对象，`BuiltScene.Objects` 按 ID 索引所有对象。
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

### 示例

```yaml
version: 1
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

`Parse` 会拒绝未知 YAML 字段、多余的 YAML 文档、不支持的版本号、重复的图层名或对象 ID（包括嵌套的子对象）、深度超过 `MaxObjectDepth` 的对象树、非法坐标系、错误颜色值以及非有限或越界的变换参数。`Encode` 会先校验再输出确定性的 2 空格缩进 YAML，通过 `Parse` 可无损往返还原。

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

### 项目

编辑器也能打开游戏的[项目文件](project.md)：`OpenProject` 读取 `game.yaml` 并打开其中的主场景，`EditProject` 以可撤销的步骤修改它，`SaveProject` 写入它并同步旁边的 `nsgo.yaml`。运行 `go tool game editor <dir>` 即可打开一个项目目录。

## 动画编辑器

编辑器的动画面板在时间轴上为任何已注册属性打关键帧，并把片段保存为 [`animation` 资源文件](resources.md#animation)，由 [animclip](animclip.md) 在游戏中播放（相当于 AnimationPlayer）。打开片段时，面板显示在视口下方。

- **打开片段**：工具栏的 Animate 按钮为选中的对象新建一个一秒长的片段。在 Go 中，`NewClip(target, name, length)` 作用相同；`OpenClipFile(path, target)` 和 `OpenClipYAML(data, target)` 打开片段文件，并按组件注册表检查（拼错的属性会带着文件名打开失败）；`CloseClip` 关闭片段。`go tool game editor scene.yaml clip.animation.yaml` 打开一个场景和一个片段，面板的 Save Clip 按钮会写回该文件。
- **打关键帧**：选择一个属性（对象字段，如 `x` 或 `alpha`，或已注册的组件字段，如 `body.speed`），按 Key 在播放头处以该属性在场景中的当前值打关键帧；`KeyProperty(target, property, at)` 作用相同。选中的是其他对象时，关键帧会进入以该对象为目标的轨道。布尔关键帧保持其值直到下一个关键帧。
- **编辑关键帧**：每条轨道是一行显示其关键帧的通道。按下关键帧即选中它并把播放头移到该处，然后可以编辑它的时间、值和插值方式（`linear`、`smooth` 或 `step`），或删除它；在 Go 中对应 `SetKeyValue`、`MoveKey`、`SetKeyInterp`、`DeleteKey` 和 `RemoveClipTrack`。值会按属性检查：布尔为 0 或 1，整数为整数值，并且在字段范围内。
- **片段设置与事件**：面板可编辑片段的名称、长度和循环（`SetClipName`、`SetClipLength`、`SetClipLoop`），并在播放头处添加具名事件（`AddClipEvent`、`DeleteClipEvent`）。
- **拖动与预览**：拖动条移动播放头（`SeekClip`），Play 在 `game.Loop` 上播放片段（`PlayClip`、`PauseClip`，或用 `StepClip` 手动步进）。此时视口显示按片段摆好姿态的场景副本，使用的 `animclip.Player` 和 `BindScene` 与游戏相同，因此正在编辑的场景不会被写入动画值，片段在游戏中的播放效果也与预览一致。场景无法播放的片段（例如轨道目标对象已被删除）仍保持打开，`ClipError` 会说明原因。
- **撤销**：每一次片段编辑，包括打开和关闭，都与场景编辑共用同一个撤销/重做历史。

`SaveClipYAML` 和 `SaveClipFile` 输出确定性的 YAML，因此打开后再次保存的片段不会改变。
