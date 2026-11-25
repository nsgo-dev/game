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
| `occluder` | 版本 2。将对象标记为 `render3d.OcclusionBuffer` 的 3D 遮挡体：`{ mode: model }` 使用对象自身的 `mesh` 或 `model` 几何体（存在多级 LOD 时取最粗糙的一级）；`{ mode: box, center: [x, y, z], size: [w, h, d] }` 使用对象局部空间中的显式包围盒（默认居中 `[1, 1, 1]`）。可选的 `doubleSided: true` 表示正反两面均光栅化。 |

覆盖项（`overrides`）可以在被实例化或被继承的场景的对象上替换上面每个字段。合并后的对象仍要遵守上面的规则，所以把基本形体换成模型或网格地图的覆盖项要在 `model` 或 `gridMap` 旁边写上 `mesh: ""`；只改 `model` 而保留 `modelNode` 或 `modelMesh` 的覆盖项，新模型里也要有那个部分。`scene.v2.json` 为编辑器和 AI 工具描述了这些字段（[schemas](schemas.md)）。

## LOD 级别与遮挡体（#904）

`scenemodel.Build` 会自动填充 `DrawItem.LODs` 并提取遮挡体几何体，使 YAML 编写的 3D 关卡无需编写 Go 胶水代码即可同时获得细节层次切换与软件遮挡剔除：

- **节点/网格命名约定**：拥有相同基础名并带有 `_LOD1`、`_LOD2`……后缀的同级 glTF 节点或网格（由 `gltf.ParseLODNodeName` 解析，不区分大小写，并忽略 Blender 的 `.001` 后缀）会自动与基础（`_LOD0` 或无后缀）网格配对。未显式配置阈值时使用 `scenemodel.DefaultLODScreenRatio`（`0.25`、`0.10`、`0.04`、`0.015`，最粗糙一级为 `0`）。
- **导入附属文件（`*.import.yaml`）**：当解析器实现 `ImportSettingsResolver`（`scenemodel.Assets` 已实现）时，`Build` 会读取 `<模型>.import.yaml`（`importsettings.Model.LODs` 与 `LODHysteresis`）。每条 LOD 记录可指定 `minScreenRatio`（投影包围球直径与视口高度之比）和/或 `maxDistance`，既可覆盖命名约定发现的 `_LODn` 阈值，也可显式指向同模型内的 `node`/`mesh` 或独立的 `.gltf`/`.glb` `model` 文件。
- **遮挡体**：带有 `occluder` 的对象会生成 `Built.Occluders`。可调用 `Built.AppendOccluders(dst)` 或 `Built.RasterizeOccluders(buf, viewProj)` 直接供给 `render3d.OcclusionBuffer`；`scenemodel.Renderer` 在 `Prepare` 中会自动把 `Built.Occluders` 光栅化到内部 `render3d.OcclusionBuffer`（除非设置了 `RendererOptions.DisableOcclusion`），并通过 `Renderer.Plan()` 暴露包含 `Stats.Occluded` 的 `render3d.RenderPlan`。

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

`Build` 像 `sceneformat.BuildNodes3D` 一样构建图层和对象，然后为每个对象补上外观、光源、相机与场景环境。它通过 `ModelResolver` 加载模型，就像 `sceneformat.Build` 通过 `MaterialResolver` 加载材质；有对象指定 `gridMap` 文件时，解析器还必须是 `GridMapResolver`；有对象指定材质文件时，还必须是 `sceneformat.MaterialResolver`；材质文件指定纹理时还必须是 `ImageResolver`；读取模型 LOD 边车时还可使用 `ImportSettingsResolver`；当 `environment.sky.kind` 为 `hdr` 时还必须是 `HDRResolver`。`Assets` 基于项目的文件同时实现这六者：模型用 `gltf.LoadFS` 加载，网格地图用 `world3d.ParseGridMapWith` 加载，材质文件通过 `resource.Library` 加载，图片按 PNG 或 JPEG 解码，边车用 `importsettings.Parse` 解析，Radiance HDR 天空通过 `render3d.DecodeHDR` 解码，每个只加载一次。实例化或继承了其他场景的场景必须先经过 `sceneformat.Resolve`。出现任何错误时（例如模型无法加载、HDR 天空解码失败、模型里没有 `modelNode` 指定的节点，或网格地图引用了不存在的瓦片），`Build` 返回带有图层和对象的错误，并且不改动父节点。

结果 `Built` 包含图层和对象的节点（`Layers`、`Objects`）、按对象 ID 索引的已构建 `*world3d.GridMap`（`GridMaps`）、已构建的遮挡体（`Occluders`）、`Drawables`，以及 3D 光源（`Lights`）、3D 相机（`Cameras`）和构建好的场景环境（`Environment`：天空立方体贴图或清屏色、预滤波 IBL 与球谐辐照度、雾效、曝光与色调映射）：每个会绘制内容的节点，可能是对象的节点、为其模型的某个节点创建的节点，或为其网格地图的瓦片实例创建的子节点。`Built.ActiveCamera(aspect)` 为当前可见的 3D 相机（`current: true`，或第一个可见 3D 相机）生成 `scene3d.Camera`，`Built.LightRig()` 收集可见光源当前的世界位置与局部 `-Z` 方向为 `render3d.LightRig`，`Built.HasSceneLighting()` 报告场景是否定义了自己的光源或环境。每个 drawable 列出它的 `Parts`，每个 glTF 三角形图元一个，基本形体则只有一个，包含节点空间中的三角形 `Mesh` 和它的 `Material`。绘制相同几何体的部件共用一个 `MeshID`，材质相同的部件共用一个 `MaterialID`，因此渲染器只需上传一次。每个绘制节点的 `LocalBounds` 由它的几何体设置，它的 `*Drawable` 存放在附件 `scenemodel.AttachmentKey` 下，拾取或编辑器可以借此找到节点画的是什么。

模型以绑定姿势绘制，并应用网格的默认变形权重。三角形带和三角形扇会展开成三角形列表，没有法线的图元按 glTF 的规定做平直着色，基础色纹理的 UV 集和 `KHR_texture_transform` 会应用到 UV 上。蒙皮、相机、灯光、动画、顶点颜色、点和线不会被使用。

## 绘制

`Built.AppendDrawItems` 为每个可见且启用的节点的每个部件追加一个 `render3d.DrawItem`，带有节点的世界矩阵、部件的世界包围盒，以及它的网格、材质和管线 ID，供游戏自己的 `render3d.Queue` 使用。`Built.Part` 把条目的 ID 映射回它的 drawable 和部件。

`Renderer` 在 `gpu.Device` 上绘制一个 `Built`：它把每个网格、纹理、天空立方体贴图、IBL 预滤波贴图与阴影贴图上传一次。每帧调用 `PrepareScene(cam)` 上传相机、场景光源（`Built.LightRig`）、阴影、雾效、IBL 和色调映射（或调用 `Prepare(cam, lights)` 覆盖为自定义 `render3d.LightRig`），在开启阴影时于主渲染通道前调用 `RecordShadows(frame)`，以 `ClearColor(fallback)` 清屏，并在主通道中依次调用 `DrawSky(pass)` 与 `Draw(pass)`：

```go
r, err := scenemodel.NewRenderer(cx, dev, built, scenemodel.RendererOptions{})
if err != nil {
	return err
}
// 每帧：
cam, _ := built.ActiveCamera(aspect)
if _, err := r.PrepareScene(cam); err != nil {
	return err
}
r.RecordShadows(frame)
pass := frame.BeginRenderPass(gpu.RenderPassDesc{
	ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: r.ClearColor(core.RGB(0x10141c))}},
	DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
})
r.DrawSky(pass)
r.Draw(pass)
pass.End()
```

它的着色器（`ShaderWGSL`）集成了 render3d 的金属度-粗糙度直接光照、方向光/聚光/点光阴影贴图、基于图像的光照（IBL）、距离雾与高度雾，以及场景色调映射和曝光（`none`、`reinhard`、`aces`、`neutral`、`agx`），并支持基础色纹理、alpha 镂空、从观察者一侧照亮的双面表面与半透明混合。移动、隐藏或显示节点（包括光源或相机节点）会在下一次 `PrepareScene` 或 `Prepare` 中生效。编辑器的 3D 视口复用同一条 `Renderer` 管线，因此光源、阴影、天空、雾效、色调映射与当前 3D 相机在编辑器中的画面与游戏第一帧保持一致（[编辑器](editor.md#3d-视口890)）。

`SetShading` 切换 `Draw` 对部件的着色方式：默认的 `ShadingLit`，或 `ShadingUnlit`，后者以每个表面带纹理的基础色加自发光色绘制、不考虑光源，就像编辑器的无光照视图那样；它的管线在第一次切换到它时创建。

## 检查与移动

- `game check` 会在指定模型、网格地图或天空的场景文件和行号处，报告缺失或无法加载的模型或网格地图、模型中没有的 `modelNode` 或 `modelMesh`，以及缺失或无法解码的 `environment.sky.hdr` 图像，覆盖项对被实例化或被继承的场景做的改动则报告在该覆盖项处；模型的加载错误、`.gltf` 文件引用的缓冲区或图片缺失，以及 `.gridmap.yaml` 中无效的 `meshLibrary` 或未知瓦片，则报告在对应资源文件上（[CLI](cli.md)）。
- `game mv`、`game index` 和编辑器的 `MoveFile` 把 `model`、`gridMap`、网格地图的 `meshLibrary` 和 `environment.sky.hdr` 当作引用：移动模型、网格地图或 HDR 天空会改写所有指定它的场景、覆盖项和 `.gridmap.yaml` 文件，移动 `.gltf` 文件的缓冲区或图片会改写该 `.gltf` 文件，并按 glTF 的要求对新名字做百分号编码（[项目文件](project.md)）。

## 示例

[Lantern 3D](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d) 从 `assets/shrine.scene.yaml` 加载关卡，该文件指定了它的 glTF 场景模型；示例用 `Build` 构建关卡，通过 `Build` 为模型的两个节点创建的 `scene3d` 节点来驱动动画，并用它自己带雾效的着色器绘制这些部件。
