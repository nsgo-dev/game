# worldmap

`game/worldmap` 记录玩家已发现的游戏世界房间，并把它们绘制成当前区域的小地图和带标记的世界地图。已发现的内容写入存档槽，读档后原样恢复。

```go
import "github.com/nsgo-dev/game/pkg/worldmap"
```

## 地图定义

地图是一个带 `version` 字段的 YAML 文件。每个房间有 ID 和世界坐标下的范围，并可选填显示名称、所属区域（一层楼或一片地区，单独绘制成一张地图）以及相连房间。存档点、首领等固定标记位于某个房间内，房间被发现后才显示。

```yaml
version: 1
rooms:
  - id: entrance
    name: Castle Gate
    x: 0
    "y": 0
    width: 320
    height: 180
    links: [hall, cellar]
  - id: hall
    x: 320
    "y": 0
    width: 320
    height: 180
  - id: cellar
    area: crypt
    x: 0
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

`Parse` 拒绝未知字段、其他版本、重复 ID、指向不存在房间的连接（或同一对房间列了两次）、非正尺寸（或在房间所在位置被 float32 舍入抹掉的尺寸）、超出 float32 范围的区域，以及同一区域内互相重叠的房间；不同区域的房间可以重叠。`Encode` 以固定键序写出规范的 YAML：它解析回同一个 `Doc`，再次编码得到相同的字节。手写文件里的注释、流式写法等格式不会保留，用 `Encode` 保存的文件会丢掉它们。YAML 1.1 会把裸写的 `y` 读成布尔值，所以编码器给键 `"y"` 加引号；两种写法都能解析。

`Load(data)` 解析文件并构建 `*Map`；`New(doc)` 从代码里构造的 `Doc` 构建。

## 发现

```go
m, err := worldmap.Load(data)

// 每个固定步长，传入玩家的世界坐标：
if id, first := m.Visit(math2d.Vec2{X: px, Y: py}); first {
	toast("发现 " + id)
}

m.Enter("hall")          // 房间切换把玩家移进了 hall
m.Reveal("tower")        // 地图道具显示房间但不算到访
m.RevealArea("crypt")    // 或显示整个区域
```

| 方法 | 结果 |
| --- | --- |
| `Visit(p)` | 把 `p` 所在房间标为已到访并设为当前房间；返回其 ID 以及是否首次到访。不在任何房间内时保留当前房间。 |
| `Enter(id)` | 同上，按 ID；未知 ID 返回 `ErrUnknownRoom`。 |
| `Reveal(ids...)`、`RevealArea(area)` | 未知房间变为 `Revealed`；已到访房间保持 `Visited`。有错误 ID 时不做任何改动。 |
| `Status(id)` | `Unknown`、`Revealed` 或 `Visited`。 |
| `Visited()`、`Revealed()` | 排好序的房间 ID。 |
| `Explored(area)` | 区域内已到访房间数与总房间数，用于计算完成度。 |
| `RoomAt(p)` | 包含 `p` 的房间。区域重叠时依次优先当前房间、当前区域、定义顺序；没有当前房间时只看定义顺序。`Visit` 以同样的方式选择房间。 |

## 标记

玩家可以放置自己的标记。标记需要未被占用的 ID、种类，以及位于某个已有房间内（含边缘）的位置：

```go
err := m.AddMarker(worldmap.Marker{ID: "pin-1", Kind: "note", Room: "hall", X: 400, Y: 90, Label: "锁着的门"})
m.RemoveMarker("pin-1")
```

`Markers()` 按 ID 排序返回固定标记和玩家标记，`VisibleMarkers()` 只返回所在房间已显示或已到访的标记，`PlayerMarkers()` 只返回玩家标记。固定标记不能删除。无效标记返回 `ErrInvalidMarker`。

## 存档

`m.State()` 返回一个 `State`：版本、当前房间、排好序的已到访和已显示房间 ID 以及玩家标记。相等的状态编码出相同的 JSON。把它和游戏其他数据一起存进 `save.Store` 的存档槽，读档后交给 `Restore`：

```go
type Slot struct {
	Gold int            `json:"gold"`
	Map  worldmap.State `json:"map"`
}

store.Save("slot1", Slot{Gold: gold, Map: m.State()})

var s Slot
if err := store.Load("slot1", &s); err == nil {
	err = m.Restore(s.Map)
}
```

`Restore` 先检查整个状态：版本不同、房间未知或重复、当前房间未到访或标记无效时返回 `ErrInvalidState`，地图保持不变。从 JSON 或 YAML 解码 `State` 时拒绝未知字段。存档里出现定义中已不存在的房间会被拒绝，所以游戏重命名或删除房间时，要添加一个 `save.WithMigration` 步骤改写存档里的 ID。`Reset` 清空所有发现。

## 绘制

两种视图都绘制到 `core.Drawing` 中并裁剪到目标矩形，只显示某一区域内已显示和已到访的房间；未知房间及其中的标记永远不会被绘制。

```go
style := worldmap.DefaultStyle()

core.Canvas{
	Style: core.Style{Width: core.Pt(160), Height: core.Pt(160)},
	Draw: func(d *core.Drawing, size core.Size) {
		// 以玩家为中心的当前区域，每个世界单位 0.2 个地图点。
		m.DrawMinimap(d, core.Bounds{Size: size}, player, 0.2, &style)
	},
}

// 暂停菜单里某一区域的世界地图，适配画布大小。
m.DrawWorldMap(d, core.Bounds{Size: size}, "", &player, &style)
```

- `DrawMinimap` 以固定比例把玩家放在矩形中心，绘制当前房间所在区域。比例不是有限正数时按 1 处理；玩家位置为 NaN 或无穷大时什么都不画。
- `DrawWorldMap` 把区域内所有房间（无论是否已发现）适配进按 `Style.Padding` 内缩后的矩形，因此玩家探索时布局不会移动。只有当前房间在该区域内且位置有限时才绘制玩家。
- `Style` 设置背景、已到访/已显示/当前房间的填充色、边框、已发现房间之间的连线、标记颜色（`MarkerKinds` 按种类设置）及尺寸。透明颜色表示不画该部分，`Current` 除外：`Current` 透明时用 `Visited` 填充当前房间。传 `nil` 使用 `DefaultStyle()`。
- `WorldMapProjection` 和 `MinimapProjection` 返回视图使用的 `Projection`。`ToWorld` 把地图上的点击转换成世界坐标，例如配合 `RoomAt` 和 `AddMarker` 放置标记。

同一张地图绘制两次记录的操作完全相同，从存档恢复的地图与存档时的地图绘制结果一致。

## 瓦片小地图

`TileMinimap` 把 `tilemap.Tilemap` 的图层渲染成一张紧凑的小地图图像，适用于 RTS、策略、Roguelike 与俯视角 RPG 游戏，支持叠加 `fog.Fog` 战争迷雾、单位与兴趣点标记、相机可见矩形以及点击平移相机。

```go
mm := worldmap.NewTileMinimap(tm, nil)
style := worldmap.DefaultTileMinimapStyle()

// 在 Canvas 的 Draw 回调中：
mm.Draw(d, core.Bounds{Size: size}, &worldmap.TileMinimapOverlay{
	Fog:    f,
	Team:   playerTeam,
	Camera: cam,
	Markers: []worldmap.TileMarker{
		{Position: heroPos, Kind: "ally"},
		{Position: enemyPos, Kind: "enemy", RequireVisible: true},
		{Position: shrinePos, Color: core.RGB(0xf2c14e)},
	},
}, &style)

// 在指针点击位于 clickPos（与 dst 同一坐标系）时：
mm.PanCamera(cam, core.Bounds{Size: size}, clickPos, &style)
```

### 瓦片颜色与增量更新

每个格子按从下到上的顺序合成所有可见瓦片图层（遵循图层不透明度，或仅合成 `TileMinimapOptions.Layers` 指定的图层索引）。瓦片颜色按以下优先级确定：

1. 若设置了 `TileMinimapOptions.TileColor(tileID)` 且返回 `ok = true`，使用其返回的颜色。
2. 瓦片的自定义颜色属性（`TileMinimapOptions.ColorProperty`，默认为 `DefaultTileColorProperty`，即 `"minimap_color"`），通过 `tilemap.Properties.Color` 解析。
3. 否则计算图块集中该瓦片图像非透明像素的按透明度加权平均色（来源为 `Image.Pixels`、内嵌的 `Image.Data`，或按 URI、去掉 `"asset:"` 前缀的 URI 或图块集名称匹配的 `TileMinimapOptions.Images`）。忽略完全透明的像素可确保等距菱形瓦片精灵保留不透明地形色。

通过 `Tilemap.SetTile`（及小范围 `Tilemap.FillRect`）做的改动会由 `Tilemap.TileRevision` / `Tilemap.TileChangesSince` 记录，因此 `Sync` 和 `Draw` 只重算发生变化的格子，而不重绘整张地图。若直接修改了 `layer.Data`、`layer.Visible`、`layer.Opacity` 或瓦片属性，可调用 `Invalidate` 触发完整重建。

### 战争迷雾、标记、相机框与点击平移

- **战争迷雾**：当 `TileMinimapOverlay.Fog` 非空时，`TileMinimap` 把 `TileMinimapStyle.Fog`（两者皆透明时回退到 `fog.DefaultStyle()`）合成到 `Unseen` 与 `Explored` 格子上，并以 `Fog.Revision(Team)` 为键缓存结果。迷雾未变化的帧会直接跳过重新合成；在迷雾版本号不变的情况下调用 `SetTile` 也只会局部更新合成图中的对应格子。
- **标记**：每个 `TileMarker` 在其世界坐标 `Position` 处绘制方块，颜色依次取自 `Color`、`TileMinimapStyle.MarkerKinds[Kind]` 或 `TileMinimapStyle.Marker`。设置 `RequireVisible: true` 后，敌方标记仅在其所在格子对当前阵营为 `fog.Visible` 时才显示。
- **相机矩形**：当 `TileMinimapOverlay.Camera` 非空时，`Draw` 描边绘出相机的可见区域（未旋转时为轴对齐矩形，`Camera.Rotation != 0` 时为旋转四边形）。
- **正交与等距地图**：正交地图每瓦片 1 像素；`tilemap.OrientationIsometric` 地图渲染为与 `Tilemap.WorldBounds` 对齐的 `(W+H) x (W+H-1)` 实心菱形，菱形外的角落保持透明。
- **点击平移**：`Projection`、`WorldToMinimap` 与 `MinimapToWorld` 同时支持正交与等距地图的世界坐标与小地图坐标互转。当 `click` 落在 `dst` 内时，`PanCamera(cam, dst, click, style)` 把相机中心移到对应的世界点并调用 `cam.Clamp()`。
