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
- `components`：可选的 `map[string]any`，存放结构化组件数据
- `properties`：可选的 `map[string]any`，存放自定义元数据

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

`Parse` 会拒绝未知 YAML 字段、多余的 YAML 文档、不支持的版本号、重复的图层名或对象 ID、非法坐标系、错误颜色值以及非有限或越界的变换参数。`Encode` 会先校验再输出确定性的 2 空格缩进 YAML，通过 `Parse` 可无损往返还原。
