# navbake

`game/navbake` 烘焙 `ai.NavMesh` 所搜索的 2D 导航网格：输入可以是关卡的外轮廓加上凹障碍物与洞，也可以是 tilemap 的实心格；按每种代理的半径收缩后，划分为凸多边形。烘焙结果保存为与关卡放在一起的带版本 YAML 文件，`Draw` 可将其绘制为调试叠加层。

```go
import "github.com/nsgo-dev/game/packages/game/navbake"
```

`ai.BuildNavMesh` 接收游戏已经切好的凸多边形，路径紧贴多边形的角走。`navbake` 负责切分，并让路径与每面墙保持代理半径的距离，就像 Godot 烘焙导航多边形、Recast 烘焙 3D 网格那样。

## 从多边形烘焙

`Source` 包含可行走的 `Outlines`（如房间的墙）以及其中的 `Obstacles`（如柱子、箱子和坑）。多边形可以是凹的，可以彼此重叠或与外轮廓重叠，绕序任意：按非零环绕规则，位于外轮廓之内、障碍物之外的点可行走。

```go
src := navbake.Source{
	Outlines: [][]core.Point{{{X: 0, Y: 0}, {X: 200, Y: 0}, {X: 200, Y: 150}, {X: 0, Y: 150}}},
	Obstacles: [][]core.Point{
		{{X: 40, Y: 40}, {X: 60, Y: 40}, {X: 60, Y: 60}, {X: 40, Y: 60}},
		{{X: 120, Y: 30}, {X: 170, Y: 30}, {X: 170, Y: 40}, {X: 130, Y: 40}, {X: 130, Y: 90}, {X: 120, Y: 90}},
	},
}
mesh, err := navbake.Bake(src, navbake.Agent{Name: "guard", Radius: 6}, navbake.Options{})
if err != nil {
	return err
}
nav := mesh.NavMesh() // 一个 *ai.NavMesh
path, ok := nav.FindPath(from, to)
```

`Bake` 按代理的 `Radius` 膨胀障碍物、收缩外轮廓，每个凸角用一个包住真实圆的多边形做圆角；对剩下的可行走区域做约束 Delaunay 三角剖分；再把三角形合并为凸多边形。在结果上用 `FindPath` 找到的路径与每个障碍物和外轮廓边至少保持 `Radius` 的距离，长度最多只比圆形代理的真实最短路径略长一点（多出的部分来自多边形化的圆角）。半径为 0 时得到可行走区域本身。

烘焙会把每个点吸附到间距为 `Options.Precision` 世界单位的网格上（为零时取 `DefaultPrecision`，即 1/256），并在该网格上精确判定每个几何问题，因此相互接触、重叠或共边的障碍物都不会使其出错，同样的输入在每个平台上都得到同样的多边形。坐标以及按智能体半径扩张后的障碍物须在原点 2^24 个网格步长以内，默认精度下为 65536 世界单位，因为这是网格的 `float32` 坐标能精确表示的最大范围；精度越粗，范围越大，超出范围的烘焙会报错。比一个网格步长还窄的缝隙或障碍物细条可能闭合或消失，因此精度应比关心的最小特征更细。

## 多种代理

不同体型的代理需要不同的网格：老鼠钻得过的窄缝，食人魔过不去。`BakeAll` 从同一个源按顺序为每个 `Agent` 烘焙一个网格，与半径无关的计算只做一次；每个 `Agent` 需要唯一的 `Name`，网格会记下它。

```go
meshes, err := navbake.BakeAll(src, []navbake.Agent{
	{Name: "rat", Radius: 2},
	{Name: "ogre", Radius: 12},
}, navbake.Options{})
```

## 从 tilemap 烘焙

`FromTilemap` 把 `tilemap.Tilemap` 转为 `Source`：地图范围是外轮廓，标志与掩码匹配的格子（按 `Tilemap.FlagsAt` 合并各图块层的标志）都是障碍物。带 `solid` 属性的 Tiled 图块和 LDtk IntGrid 墙已经带有 `tilemap.TileSolid`。

```go
m, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
if err != nil {
	return err
}
src, err := navbake.FromTilemap(m, tilemap.TileSolid)
if err != nil {
	return err
}
meshes, err := navbake.BakeAll(src, agents, navbake.Options{})
```

格子采用地图 `Orientation` 给出的世界空间形状：正交地图是正方形，等距与交错地图是菱形，六边形地图是六边形，因此网格与绘制出的图块对齐。相邻的实心格合并为一个障碍物；只在角上相接的两个实心格之间不会留下圆形代理能通过的缝；被实心格围住的空地仍可行走，但从外面到达不了。

## 网格文件

`Mesh` 是烘焙结果：格式版本、代理的名称与半径，以及凸多边形，每个多边形是一组以世界单位表示的 `[x, y]` 顶点，从最低的顶点开始，在 y 轴向上的坐标系中逆时针排列（在 y 轴向下的屏幕上为顺时针）。

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

`Mesh.Encode` 按固定的字段顺序写出，每个多边形占一行，每个坐标用能读回同一 `float32` 的最短形式；`Parse` 读取它，拒绝未知字段、其他版本、非法的离网连接，以及不凸、顺时针环绕、顶点少于三个或坐标非有限值的多边形。可通过 `Source.Links` 传入并在文件中以 `links:` 保存可选的点对点离网连接（跳跃、跌落、梯子、传送门；含 `id`、`start: [x, y]`、`end: [x, y]`、`radius`、`bidirectional`、`cost`、`area`、`flags`、`userData` 字段）。同样的输入烘焙两次得到逐字节相同的文件，加载后再保存也不会有任何改动，因此游戏可以在构建步骤中烘焙网格，与关卡一起提交，并以 diff 审阅每次改动。`Mesh.NavMesh` 构建运行时搜索用的 `ai.NavMesh` 并自动注册所有 `Links`，无需重新烘焙。如需运行时动态障碍物与按瓦片重建，可调用 `Mesh.TiledNavMesh(tileSize)` 构建 `*ai.TiledNavMesh`。

## 调试绘制

`Draw` 在 `core.Drawing` 已有的相机变换下，分别用各自的颜色填充每个多边形、描出其边，并描出相邻多边形之间的门户（路径穿过的边）；透明色会跳过对应的图层。`Mesh.Portals` 返回门户本身。`Mesh.Portals` 把多边形分桶到网格上，耗时与多边形数量近似成线性，但 `Draw` 每次调用仍会重新查找门户；每帧绘制大网格的游戏可以保存 `Portals` 的结果自行描绘。

```go
navbake.Draw(d, mesh, core.RGBA(0x40a0ff, 0.2), core.RGB(0x2060c0), core.RGB(0xffc040), 1)
```

示例游戏 CryptQuest 用 `FromTilemap` 烘焙地下墓穴的网格，按 `N` 键时绘制出来。
