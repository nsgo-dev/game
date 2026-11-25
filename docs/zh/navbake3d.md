# navbake3d

`game/navbake3d` 从关卡几何体烘焙 3D 导航网格，做法与 Recast、Godot 的 `NavigationRegion3D` 和 Unity 的 `NavMeshSurface` 相同，因此 3D 游戏不必再手写 [world3d](world3d.md) `NavMesh` 的凸多边形。它从 glTF 模型、场景文件、地形高度图、`GridMap` 和物理碰撞体读取关卡，为每种代理类型烘焙一个网格，并保存在场景所引用的带版本 YAML 文件里。2D 关卡请改用 [navbake](navbake.md)，它烘焙 `ai.NavMesh` 多边形。

```go
import "github.com/nsgo-dev/game/packages/game/navbake3d"
```

## 烘焙关卡

把关卡的三角形收集到一个 `Geometry` 中，然后用 `Bake` 为每种代理类型烘焙：

```go
var g navbake3d.Geometry
g.AddGLTF(levelModel, math3d.Mat4Identity(), 1)   // 一个 *gltf.Document
g.AddHeightmap(terrain, 1)                        // 一个 *world3d.Heightmap
g.AddPhysicsWorld(physicsWorld, navbake3d.LayerAll) // physics3d 的静态碰撞体

file, err := navbake3d.Bake(&g, navbake3d.Settings{CellSize: 0.2, CellHeight: 0.1}, []navbake3d.AgentType{
	{Name: "humanoid", Radius: 0.4, Height: 1.8, MaxSlope: 45, MaxClimb: 0.4},
	{Name: "rat", Radius: 0.1, Height: 0.3, MaxClimb: 0.1},
})
if err != nil {
	return err
}
data, err := navbake3d.Encode(file) // 写入 levels/level1.navmesh.yaml
```

烘焙沿用 Recast 的流程：

1. 把三角形光栅化成由实心区段组成的高度场，每列宽和深为 `CellSize`，高为 `CellHeight`。
2. 保留所在三角形坡度不超过代理 `MaxSlope` 的区段顶面，允许代理跨上 `MaxClimb` 以内的低矮障碍，并去掉落差超过 `MaxClimb` 的边缘上的顶面以及净空不足 `Height` 的顶面，因此低于代理身高的天花板下不可行走。
3. 把每个可行走顶面与代理能跨过去的相邻顶面连接起来，再按 `Radius` 收缩可行走区域，使网格与墙壁和边缘保持代理半径的距离。
4. 把可行走区域划分为单调区域，并去掉小于 `MinRegionArea` 的孤岛。
5. 描出每个区域的轮廓并加以简化：两个区域之间的边只保留相邻区域发生变化的拐角，使两侧共享这些拐角；墙边与格子的偏差不超过 `MaxSimplificationError`，长度不超过 `MaxEdgeLength`。
6. 对每个轮廓做三角剖分，再把三角形合并成顶点数不超过 `MaxVertsPerPoly` 的凸多边形。
7. 添加高度细节：沿每个多边形的边和内部每隔 `DetailSampleDistance`（至少一个 `CellSize`；为零时取 1.8，单元更大时取单元大小）采样一次，把拟合最差的采样点加为多边形三角形的顶点，直到三角形与地面的偏差在 `DetailSampleMaxError` 以内，使多边形贴合地形和台阶。

踢面高度在 `MaxClimb` 以内的台阶会烘焙成一条经过各级台阶边沿的坡道，代理可以爬上去；更高的台阶会使其上方的地面无法到达。下方净空达到 `Height` 的桥会烘焙成互不相连的桥面和桥下地面，两者之间的路径会绕行连接它们的台阶或坡道。

`Settings` 或 `AgentType` 中为零的字段取默认值（`DefaultCellSize`、`DefaultAgentRadius` 等），烘焙出的文件记录实际使用的值。`BakeMesh` 只烘焙一种代理类型。用相同设置烘焙相同的几何体，在任何 CPU 架构上都写出相同的字节，因此该文件应与场景一起纳入版本控制，没有改动的重新烘焙不会产生差异。Go 允许编译器把乘法和加法融合为一次舍入，arm64 会这样做而 amd64 不会，所以烘焙对每个乘积都显式舍入，并有测试在 Linux x86 和 macOS arm64 上把参考关卡的烘焙结果与已提交的文件逐字节比较。游戏在加入几何体之前自己算出的部分，例如 `math3d` 的节点变换或 `world3d` 的地形，不在烘焙范围内，其坐标的最后一位本身就可能因架构而不同。

### 精度

墙壁、地面和收缩都以整格计量，因此网格与墙壁保持的代理半径误差在一格以内，高度向上取整到整数个 `CellHeight`。更小的 `CellSize` 和 `MaxSimplificationError` 更贴合关卡，但烘焙更慢；超过 `MaxCells` 个格子的关卡会被拒绝，并提示使用更大的 `CellSize`；身高、可攀爬高度或半径所跨格数超出高度场容量的代理类型同样会被拒绝。

## 关卡几何体

`Geometry` 保存世界空间的三角形，每个三角形位于若干层上，由 `Settings.LayerMask` 选择；层为 0 表示所有层。三角形从正面看按逆时针绕序，与 glTF 和本模块的网格相同：朝上的三角形是地面，而任何三角形无论朝向如何都是实心的。

| 方法 | 添加的内容 |
| --- | --- |
| `AddTriangle`、`AddMesh` | 三角形，或按世界矩阵变换的带索引三角形列表；镜像矩阵会把绕序翻转回来 |
| `AddGLTF` | glTF 文档默认场景中的每个网格，按其节点的世界变换放置；展开三角形带和三角扇，跳过点和线 |
| `AddScene` | 每个不在已禁用节点子树中、`Layer` 与掩码有共同位的已启用 `scene3d` 节点，三角形来自一个 `NodeMesh`（`ColliderMesh` 读取 `*scene3d.MeshCollider`） |
| `AddSceneFile` | 一个[场景文件](editor.md)：解析其实例化和继承的场景，构建 3D 节点树，并加载每个对象的 `model` 指定的 glTF 模型（`SceneMeshes`） |
| `AddHeightmap` | 一个 `world3d.Heightmap`，按其 `BuildMesh` 的方式三角化 |
| `AddGridMap` | `world3d.GridMap` 每个已占用格子的碰撞盒 |
| `AddPhysicsWorld` | `physics3d.World` 中层与掩码有共同位的静态刚体、三角网格、高度场和体素碰撞体；动态刚体、运动学刚体和触发器不计入，网格未实现 `physics3d.VoxelBoundsGrid` 的体素碰撞体也不计入，因为无法列出其实心体素 |

与 Recast 一样，三角形只是表面，所以高到能站进去的封闭网格是中空的，其内部地面会烘焙成一个没有路径能到达的孤岛，除非它小于 `MinRegionArea`。`AddPhysicsWorld` 和 `AddGridMap` 添加的碰撞形状（盒、球、胶囊和体素）则是实心的：它们经过的每一列从最低点到最高点都是实心的。

## 导航网格文件

场景在 `navMesh` 字段中以项目根目录起的路径指定其烘焙文件，`game navmesh` 会写到那里。文件保存烘焙时的设置以及每种代理类型的一个网格：

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
    polygons:
      - vertices: [0, 1, 2, 3]
        detail:
          - [2, 0.75, 2]
        triangles:
          - [0, 1, 4]
          - [1, 2, 4]
          - [2, 3, 4]
          - [3, 0, 4]
```

| 字段 | 含义 |
| --- | --- |
| `version` | Schema 版本，`1` |
| `settings` | 烘焙这些网格时使用的 `Settings`，每个字段都会写出 |
| `meshes[].agent` | 该网格对应的 `AgentType`，每个字段都会写出；名称唯一 |
| `meshes[].vertices` | 多边形在世界空间中的拐角 `[x, y, z]`，由相邻多边形共享 |
| `meshes[].polygons[].vertices` | 多边形各拐角在 `vertices` 中的索引，3 到 `maxVertsPerPoly` 个，沿多边形排列 |
| `meshes[].polygons[].detail` | 在地面起伏处于多边形的边上和内部采样得到的额外顶点 |
| `meshes[].polygons[].triangles` | 覆盖该多边形的三角形，从上方看为逆时针；小于多边形顶点数的索引 `i` 指它的第 `i` 个拐角，索引 `n + i` 指 `detail[i]` |

`Parse` 拒绝未知字段、缺失或不支持的版本、非有限坐标和越界索引，并给出行号、字段和修复方法；`Encode` 按上面的固定键顺序写出，加载后再保存不会改变文件。[文件格式](formats.md#3d-导航网格)把它与其他格式列在一起。

## 寻路

加载场景的文件，把某种代理类型的网格转成 `world3d.NavMesh`：

```go
file, err := navbake3d.LoadFS(assets, scene.NavMesh)
if err != nil {
	return err
}
mesh, ok := file.Mesh("humanoid")
if !ok {
	return errors.New("no humanoid navigation mesh")
}
nav := mesh.NavMesh()
path, ok := nav.FindPath(start, goal)
```

`Mesh.NavMesh` 把每个细节三角形变成 `NavMesh` 的一个可行走多边形，并通过共享边与相邻多边形相连，因此 `FindPath` 会按烘焙出的高度沿坡道、台阶和地形前进，并让叠放的楼层保持分离。其 `VerticalTolerance` 取代理身高与可攀爬高度中的较大者，所以 `FindPolygon` 会把一个点吸附到其上方或下方该距离以内的表面上。

## 调试绘制

`DrawDebug` 通过 `[0, 1]` 深度投影（例如 `math3d.Perspective` 或 render3d 相机的投影）把网格的多边形轮廓和细节三角形描到 `core.Drawing` 上，并在近平面处裁剪；`DebugStyle` 设置颜色、线宽以及线条离地抬高的距离。`Mesh.DebugLines` 返回同样的线段，供自己的渲染器使用。发布构建中不绘制任何内容。

```go
navbake3d.DrawDebug(d, viewProj, viewport, mesh, navbake3d.DefaultDebugStyle())
```

## 命令行

```sh
nsgo game navmesh scenes/level1.scene.yaml
```

[`game navmesh`](cli.md#navmesh) 为[项目文件](project.md)中 `navigation` 部分的每种代理类型烘焙场景的 glTF 模型，并写入场景的 `navMesh` 指定的文件。
