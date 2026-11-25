# scenemodel

`game/scenemodel` 为[场景文件](editor.md)中的对象提供 3D 外观，让 3D 关卡和 2D 关卡一样，是一个人和 AI 都能编辑、能在代码审查中阅读的文本文件，而不必在 Go 代码里拼装。对象在 `mesh` 中指定一个内置基本形体，在 `model` 中指定一个 glTF 2.0 模型，或在 `gridMap` 中指定一个模块化 3D 网格地图，`scenemodel.Build` 把场景变成一棵 `scene3d` 节点树，节点上带着各自的几何体、材质和包围盒，可直接交给 `render3d.Queue` 或本包自己的 `Renderer`。

```go
import "github.com/nsgo-dev/game/packages/game/scenemodel"
```

## 字段

```yaml
version: 2
name: Courtyard
layers:
  - name: level
    objects:
      - id: floor
        scaleX: 6
        scaleZ: 6
        mesh: plane
        material: materials/stone.material.yaml
      - id: crate
        x: -1
        "y": 0.5
        rotY: 0.4
        model: models/crate.glb
      - id: lid
        x: 1.5
        "y": 0.1
        model: models/crate.glb
        modelNode: Lid
      - id: ball
        x: 0.9
        "y": 0.5
        mesh: sphere
        material: materials/teal.material.yaml
```

| 字段 | 含义 |
| --- | --- |
| `mesh` | 内置基本形体：`box`（边长 1）、`sphere`（直径 1）、`plane`（XZ 平面上 1 × 1，朝向 +Y）、`cylinder`（直径 1、高 1）或 `capsule`（直径 1、高 2），以对象原点为中心，圆形的轴沿 Y，尺寸与 Godot 的默认网格一致，因此对象的缩放就是以米为单位的大小。其他取值会被拒绝。 |
| `model` | 版本 2。对象绘制的 glTF 2.0 模型：`.gltf` 文件（它的单独缓冲区和图片从同一目录读取）或 `.glb` 文件，用从项目根目录起、以斜杠分隔的路径指定。没有 `modelNode` 或 `modelMesh` 时，对象绘制模型默认场景（没有默认场景时取第一个场景，没有场景时取所有没有父节点的节点）的每个节点，每个都作为对象的子节点，带有它的 glTF 变换。不能与 `mesh` 同时使用。 |
| `modelNode` | 版本 2，需要 `model`。模型中第一个叫这个名字的节点，画在对象的节点上，它的后代作为子节点；对象自己的变换取代该节点的变换。 |
| `modelMesh` | 版本 2，需要 `model`，不能与 `modelNode` 同时使用。模型中第一个叫这个名字的网格，画在对象原点。 |
| `gridMap` | 版本 2。模块化 3D [`*.gridmap.yaml` 文件](world3d.md#模块化-3d-gridmap)，用从项目根目录起、以斜杠分隔的路径指定。`Build` 加载网格地图及其 glTF `meshLibrary`，调用 `world3d.GridMap.BuildInstances`，为每个瓦片实例创建共用 `MeshID` 和 `MaterialID` 的子节点，并按 `GridMap.Bounds()` 设置对象节点的 `LocalBounds`。不能与 `mesh` 或 `model` 同时使用。 |
| `material` | 一个[材质文件](resources.md)。用在基本形体上时它就是表面，没有时为白色、非金属、完全粗糙；用在模型或网格地图上时它替换模型或瓦片库的所有材质。它的基础色和自发光颜色是 sRGB，会转换为线性光；不透明度乘到基础色的 alpha 上，小于 1 时表面按混合绘制；`metallic`、`roughness`、`doubleSided` 和 `texture` 按原样使用。它的 2D 混合模式和 `unlit` 在 3D 中不适用。 |

覆盖项（`overrides`）可以在被实例化或被继承的场景的对象上替换上面每个字段。合并后的对象仍要遵守上面的规则，所以把基本形体换成模型或网格地图的覆盖项要在 `model` 或 `gridMap` 旁边写上 `mesh: ""`；只改 `model` 而保留 `modelNode` 或 `modelMesh` 的覆盖项，新模型里也要有那个部分。`scene.v2.json` 为编辑器和 AI 工具描述了这些字段（[schemas](schemas.md)）。

## 构建

```go
assets := scenemodel.NewAssets(os.DirFS(projectRoot)) // 或以项目根目录为根的 embed.FS
doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/level.scene.yaml"})
if err != nil {
	return err
}
world := scene3d.NewScene()
built, err := scenemodel.Build(doc, world.Root(), assets)
if err != nil {
	return err
}
```

`Build` 像 `sceneformat.BuildNodes3D` 一样构建图层和对象，然后给每个对象加上外观。它通过 `ModelResolver` 加载模型，就像 `sceneformat.Build` 通过 `MaterialResolver` 加载材质；有对象指定 `gridMap` 文件时，解析器还必须是 `GridMapResolver`；有对象指定材质文件时，还必须是 `sceneformat.MaterialResolver`；材质文件指定纹理时还必须是 `ImageResolver`。`Assets` 基于项目的文件同时实现这四者：模型用 `gltf.LoadFS` 加载，网格地图用 `world3d.ParseGridMapWith` 加载，材质文件通过 `resource.Library` 加载，图片按 PNG 或 JPEG 解码，每个只加载一次。实例化或继承了其他场景的场景必须先经过 `sceneformat.Resolve`。出现任何错误时，例如模型无法加载、模型里没有 `modelNode` 指定的节点，或网格地图引用了不存在的瓦片，`Build` 返回带有图层和对象的错误，并且不改动父节点。

结果 `Built` 包含图层和对象的节点（`Layers`、`Objects`）、按对象 ID 索引的已构建 `*world3d.GridMap`（`GridMaps`），以及 `Drawables`：每个会绘制内容的节点，可能是对象的节点、为其模型的某个节点创建的节点，或为其网格地图的瓦片实例创建的子节点。每个 drawable 列出它的 `Parts`，每个 glTF 三角形图元一个，基本形体则只有一个，包含节点空间中的三角形 `Mesh` 和它的 `Material`。绘制相同几何体的部件共用一个 `MeshID`，材质相同的部件共用一个 `MaterialID`，因此渲染器只需上传一次。每个绘制节点的 `LocalBounds` 由它的几何体设置，它的 `*Drawable` 存放在附件 `scenemodel.AttachmentKey` 下，拾取或编辑器可以借此找到节点画的是什么。

模型以绑定姿势绘制，并应用网格的默认变形权重。三角形带和三角形扇会展开成三角形列表，没有法线的图元按 glTF 的规定做平直着色，基础色纹理的 UV 集和 `KHR_texture_transform` 会应用到 UV 上。蒙皮、相机、灯光、动画、顶点颜色、点和线不会被使用。

## 绘制

`Built.AppendDrawItems` 为每个可见且启用的节点的每个部件追加一个 `render3d.DrawItem`，带有节点的世界矩阵、部件的世界包围盒，以及它的网格、材质和管线 ID，供游戏自己的 `render3d.Queue` 使用。`Built.Part` 把条目的 ID 映射回它的 drawable 和部件。

`Renderer` 在 `gpu.Device` 上绘制一个 `Built`：它把每个网格和纹理上传一次，之后每帧由 `Prepare` 写入相机、`render3d.LightRig` 和节点当前的变换，并用 `render3d.Queue` 规划这一帧（视锥裁剪，不透明部件由近到远，混合部件由远到近），再由 `Draw` 把它录制到带深度附件的渲染通道中。

```go
r, err := scenemodel.NewRenderer(cx, dev, built, scenemodel.RendererOptions{})
if err != nil {
	return err
}
// 每帧：
if _, err := r.Prepare(cam, lights); err != nil {
	return err
}
pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
pass.ClearColor(core.RGB(0x10141c))
pass.ClearDepth(1)
r.Draw(pass)
pass.End()
```

它的着色器（`ShaderWGSL`）是 render3d 的金属度-粗糙度光照，加上基础色纹理、alpha 镂空、从观察者一侧照亮的双面表面以及混合。移动、隐藏或显示节点会从下一次 `Prepare` 起改变绘制的内容；要改变对象的外观，编辑场景文件或材质文件后重新构建。需要自己的着色器（例如带雾效）的游戏可以读取这些部件自行上传，Lantern 3D 示例就是这样做的。

`SetShading` 切换 `Draw` 对部件的着色方式：默认的 `ShadingLit`，或 `ShadingUnlit`，后者以每个表面带纹理的基础色加自发光色绘制、不考虑光源，就像编辑器的无光照视图那样；它的管线在第一次切换到它时创建。编辑器的 3D 视口用 `Renderer` 绘制场景的构建结果（[编辑器](editor.md#3d-视口890)）。

## 检查与移动

- `game check` 会在指定模型或网格地图的场景文件和行号处，报告缺失或无法加载的模型或网格地图，以及模型中没有的 `modelNode` 或 `modelMesh`，覆盖项对被实例化或被继承的场景做的改动则报告在该覆盖项处；模型的加载错误、`.gltf` 文件引用的缓冲区或图片缺失，以及 `.gridmap.yaml` 中无效的 `meshLibrary` 或未知瓦片，则报告在对应资源文件上（[CLI](cli.md)）。
- `game mv`、`game index` 和编辑器的 `MoveFile` 把 `model`、`gridMap` 以及网格地图的 `meshLibrary` 当作引用：移动模型或网格地图会改写所有指定它的场景、覆盖项和 `.gridmap.yaml` 文件，移动 `.gltf` 文件的缓冲区或图片会改写该 `.gltf` 文件，并按 glTF 的要求对新名字做百分号编码（[项目文件](project.md)）。

## 示例

[Lantern 3D](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d) 从 `assets/shrine.scene.yaml` 加载关卡，该文件指定了它的 glTF 场景模型；示例用 `Build` 构建关卡，通过 `Build` 为模型的两个节点创建的 `scene3d` 节点来驱动动画，并用它自己带雾效的着色器绘制这些部件。
