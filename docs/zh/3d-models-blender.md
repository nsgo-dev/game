# 从 Blender 导入 3D 模型

在 Blender 4.x 中准备静态、蒙皮和带动画的 3D 模型，或将其他工具导出的 FBX 与 OBJ 资产转换为符合 NSGo 3D 约定的 glTF 2.0（`.glb`/`.gltf`），并通过仓库内置的两个往返校验示例模型验证管线。

> NSGo 的 3D 渲染层位于[概览](index.md)中的阶段 2（[nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)）。本页给出统一的坐标、导出与 glTF 2.0 资产约定，便于提前制作、批量转换与校验 3D 模型，避免后续返工重导。

## NSGo 3D 约定

NSGo 在所有 3D 模块中统一采用标准 glTF 2.0 坐标系与物理单位：

| 属性 | 约定 | 说明 |
| --- | --- | --- |
| 手性 | 右手坐标系 | 与 glTF 2.0 及 OpenGL/Metal/Vulkan 视图数学一致 |
| 坐标轴 | `+X` 向右、`+Y` 向上、`-Z` 向前 | 角色、载具在局部空间中朝向 `-Z` |
| 单位 | 米（`1.0` = 1 m） | 常规人形角色约 `1.7`–`1.8` 个单位高；物理与光照均按米计算 |
| 正面绕序 | 逆时针（CCW） | 外法线满足 `(v1 - v0) × (v2 - v0) · n > 0` |
| 纹理 UV | 左上角为原点 `(0, 0)` | 标准 glTF 2.0 UV 布局；Blender 的 glTF 导出器会自动翻转 V 轴 |
| PBR 材质 | Metallic-Roughness | 使用 `Principled BSDF` 的基础色、金属度、粗糙度、法线、AO 与自发光 |
| 运行时格式 | 二进制 glTF 2.0（`.glb`） | 单文件自包含且缓冲区按 4 字节对齐；排查 JSON 结构时可使用 `.gltf` |
| 蒙皮权重上限 | 每顶点最多 4 根骨骼影响 | 每个顶点一组 `JOINTS_0` / `WEIGHTS_0` `VEC4` 属性，权重之和归一化为 `1.0` |

### 3D 资产的目录组织

将运行时加载的 `.glb` 文件放入 `resources/`，这样 `nsgo build` 会自动把它们打包到六个目标平台，并在代码中通过 `"asset:..."` 读取。体积较大的制作源文件（`.blend`、`.fbx`、`.obj`、`.psd`）请放在 `resources/` 之外（例如 `art/`），避免被打包进发布产物。带 `.bin` 缓冲区和纹理文件的 `.gltf` 导出结果也可以留在 `art/`：[`game assets`](cli.md#assets) 的模型目标会把它打包成 `resources/` 下的单个 `.glb`，并按模型导入设置附属文件的要求缩放、保留或去掉动画：

```text
mygame/
├─ art/                    # 制作源文件，不打入发布包
│  ├─ crate.blend
│  └─ robot_arm.blend
├─ resources/              # 随每个目标平台一起打包（"asset:..."）
│  └─ models/
│     ├─ crate.glb
│     └─ robot_arm.glb
└─ nsgo.yaml
```

如果桌面端与移动端需要分发不同精度或贴图尺寸的模型，可在 `nsgo.yaml` 中使用 `mobile.exclude_resources`，详见[构建与发布](shipping.md)。

## Blender 4.x 导出清单

Blender 默认使用右手 **Z-up** 视口（`+X` 向右、`+Y` 朝屏幕内、`+Z` 向上）。导出 glTF 2.0 时勾选 **`+Y Up`** 后，导出器会自动绕 X 轴旋转 `-90°`，将 Blender 的 `+Z` 映射为 `+Y`，把 Blender 的 `-Y`（正视图小键盘 `1` 的朝向）映射为 glTF 中的 **`-Z` 向前**。

### 导出前通用检查

1. **设置公制单位**：在 *Scene Properties → Units* 中将 *Unit System* 设为 `Metric`，*Unit Scale* 设为 `1.0`。在右侧 *N* 面板的 `Item → Dimensions` 中确认模型尺寸符合真实米数。
2. **在 Blender 中朝向 `-Y`**：让角色、武器和载具的正面朝向 `-Y`（在正交前视图 Numpad `1` 下正对镜头）。开启 `+Y Up` 导出后，它们在 NSGo 中恰好朝向 `-Z`。
3. **对齐原点（Pivot）**：让静态道具底部或角色脚底贴合世界原点 `(0, 0, 0)`，这样在游戏里放到 `y = 0` 时就能稳稳落在地面上。
4. **应用变换**：选中网格（以及带骨骼资产的 Armature），按 `Ctrl+A → Rotation & Scale`（原点对齐的静态道具可一并应用 `Location`）。切勿带着负缩放（如 `-1`）导出，否则三角形绕序会被翻转。
5. **检查面朝向**：打开 *Viewport Overlays → Face Orientation*，所有外表面应显示为蓝色。如果出现红色面，进入编辑模式全选（`A`）并执行 `Mesh → Normals → Recalculate Outside`（`Shift+N`）。
6. **统一使用 `Principled BSDF`**：将贴图连入 `Base Color`、`Metallic`、`Roughness`、`Normal`（经 `Normal Map` 节点）和 `Emission`。Blender 专属的程序化节点（Noise、Voronoi、ColorRamp 等）无法直接进入 glTF，导出前需先烘焙为纹理贴图。

### 静态网格（`.glb`）

打开 `File → Export → glTF 2.0 (.glb/.gltf)`，核对以下选项：

- **Format**：选择 `glTF Binary (.glb)`。
- **Include**：勾选 `Selected Objects`（或 `Active Collection`），避免把场景里的相机、灯光或辅助空物体一并导出。
- **Transform**：勾选 **`+Y Up`**。
- **Data → Mesh**：
  - 勾选 `Apply Modifiers`（自动烘焙 Subdivision、Mirror、Bevel、Weighted Normal 等修改器）。
  - 勾选 `UVs` 和 `Normals`。
  - 使用法线贴图时勾选 `Tangents`，避免不同渲染器计算出的切线空间不一致。
  - 若着色器不需要顶点色，取消勾选 `Vertex Colors`。
- **Data → Material**：`Materials` 选择 `Export`，图像格式选 `Automatic`（PNG/JPEG）或在目标平台支持时使用 `WebP`。
- **Data → Compression**：迭代期间**不勾选 Draco 网格压缩**，文件便于检查；发布的资产在意下载体积时再开启。`game/gltf` 用纯 Go 解码 Draco（见[压缩几何数据](#压缩几何数据)）。

### 蒙皮网格与动画

带骨骼与动画的角色和道具需要额外确认以下事项，以确保 GPU 蒙皮和动画融合表现正常：

1. **单一 Armature 根节点且缩放为单位值**：导出前对 Armature 和蒙皮 Mesh 应用 `Scale = (1, 1, 1)`。保留网格上的 Armature 修改器（**不要**应用 Armature 修改器）。
2. **限制为每顶点 4 根骨骼影响**：
   - 选中蒙皮网格，进入 *Weight Paint* 模式，执行 `Weights → Limit Total` 并将 **Limit 设为 `4`**。
   - 紧接着执行 `Weights → Normalize All`（取消勾选 *Lock Active*），保证每个顶点的权重之和严格等于 `1.0`。
   - 在 glTF 导出器的 *Data → Skinning* 下勾选 `Skinning`，并**取消勾选 `Include All Bone Influences`**，确保只输出一组 `JOINTS_0` 和 `WEIGHTS_0`（4 骨骼上限）。
3. **通过 Actions / NLA 导出多段动画**：
   - 为每段动画赋予清晰的 Action 名称（如 `Idle`、`Walk`、`Jump`、`Wave`）。
   - 在 *Non-Linear Animation*（NLA）编辑器中把每个 Action 推入 NLA 轨道，避免未激活的 Action 被 Blender 丢弃。
   - 在导出器的 *Animation* 面板中，将 *Animation Mode* 设为 **`Actions`**（或 `NLA Tracks`）；如果绑定使用了 IK、约束或驱动器，勾选 **`Always Sample Animations`**，并将 *Sampling Rate* 设为与固定步长匹配的 `30` 或 `60` fps。
   - 开启 `Optimize Animation Size` 以剔除无变化的冗余通道。
4. **根运动（Root Motion）与原地循环（In-Place）**：
   - 在骨架最顶层放置一个位于地面 `(0, 0, 0)` 的 `Root` 骨骼作为 `Hips`/`Pelvis` 的父级。
   - **原地循环动画**（位移由 Go 游戏逻辑驱动）：让 `Root` 保持在 `(0, 0, 0)`，并移除 `Hips` 在水平 `X`/`Z` 方向的漂移。
   - **根运动动画**（如翻滚、突刺）：将水平位移与偏航旋转全部制作在 `Root` 骨骼上，便于运行时按固定步长提取增量位移并交给移动控制器，避免位移被叠加两次。
5. **动画剪辑事件（`extras.events`）**：
   - NSGo 从 `animations[i].extras.events` 读取每个剪辑的事件标记数组，每项包含 `{ "time": <秒>, "name": "<事件名>" }`：
     ```json
     "extras": {
       "events": [
         { "time": 0.0, "name": "wave_start" },
         { "time": 0.5, "name": "wave_apex" },
         { "time": 1.0, "name": "wave_end" }
       ]
     }
     ```
   - 在 Blender 的 *Dope Sheet → Action Editor* 中为 Action 添加 **Pose Markers**（按 `M` 添加，按 `Ctrl+M` 重命名），导出前运行下面的脚本将它们写入 `action["events"]`，并在导出器中勾选 **`Include → Custom Properties`**：

```python
import bpy

fps = bpy.context.scene.render.fps / bpy.context.scene.render.fps_base

for action in bpy.data.actions:
    if not action.pose_markers:
        continue
    start = action.frame_range[0]
    events = [
        {"time": round((m.frame - start) / fps, 6), "name": m.name}
        for m in sorted(action.pose_markers, key=lambda item: item.frame)
    ]
    action["events"] = events
```

## 从其他工具转换 FBX 与 OBJ

来自 Maya、3ds Max、ZBrush、Mixamo 或素材商店的模型通常是 `.fbx` 或 `.obj`，经常带有**厘米单位**（放大了 `100×`）或 **Z-up 朝向**。推荐在资产准备阶段统一离线转换为 `.glb`，不要在游戏运行时解析 FBX 或 OBJ。

### 无头 Blender CLI 批量转换

利用 Blender 的无头模式（`blender --background`）可以在本地或 CI 中稳定复现 `.fbx`/`.obj` → `.glb` 转换。将以下脚本保存为 `tools/convert_to_glb.py`：

```python
import os
import sys
import bpy

argv = sys.argv[sys.argv.index("--") + 1 :]
src_path, dst_path = argv[0], argv[1]
scale_factor = float(argv[2]) if len(argv) > 2 else 1.0  # 厘米转米请传 0.01

bpy.ops.wm.read_factory_settings(use_empty=True)

ext = os.path.splitext(src_path)[1].lower()
if ext == ".fbx":
    bpy.ops.import_scene.fbx(
        filepath=src_path,
        use_anim=True,
        ignore_leaf_bones=True,
        automatic_bone_orientation=True,
    )
elif ext == ".obj":
    bpy.ops.wm.obj_import(
        filepath=src_path,
        forward_axis="NEGATIVE_Z",
        up_axis="Y",
    )
else:
    raise SystemExit(f"unsupported input format: {ext}")

# 源工具使用厘米单位（Maya、Unreal、Mixamo 等）时缩放到米并应用变换。
if scale_factor != 1.0:
    bpy.ops.object.select_all(action="SELECT")
    for obj in bpy.context.selected_objects:
        if obj.parent is None:
            obj.scale = tuple(s * scale_factor for s in obj.scale)
    bpy.ops.object.transform_apply(location=False, rotation=True, scale=True)

# 对所有蒙皮网格统一限制 4 骨骼权重并归一化。
for obj in bpy.data.objects:
    if obj.type == "MESH" and obj.vertex_groups:
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.mode_set(mode="WEIGHT_PAINT")
        bpy.ops.object.vertex_group_limit_total(limit=4)
        bpy.ops.object.vertex_group_normalize_all(lock_active=False)
        bpy.ops.object.mode_set(mode="OBJECT")

bpy.ops.export_scene.gltf(
    filepath=dst_path,
    export_format="GLB",
    export_yup=True,
    export_apply=True,
    export_texcoords=True,
    export_normals=True,
    export_tangents=True,
    export_skins=True,
    export_all_influences=False,
    export_animations=True,
    export_force_sampling=True,
    export_extras=True,
    export_draco_mesh_compression_enable=False,
)
```

在命令行中运行：

```sh
# 转换以米为单位制作的 OBJ 道具
blender --background --python tools/convert_to_glb.py -- art/barrel.obj resources/models/barrel.glb

# 转换以厘米为单位的 Maya / Mixamo FBX（0.01 倍缩放到米）
blender --background --python tools/convert_to_glb.py -- art/character.fbx resources/models/character.glb 0.01
```

### 常见源工具差异处理

- **Maya / 3ds Max / Unreal 的 FBX（厘米 → 米）**：Blender 的 FBX 导入器会读取文件头的 `UnitScaleFactor`，但不少第三方素材文件把 `UnitScaleFactor` 保持为 `1` 却把角色建成了 `180` 个单位高。转换后务必检查包围盒高度（单位为米），必要时传入 `0.01`。
- **Z-up 的 OBJ 文件**（CAD 软件或旧版 3ds Max 导出）：在 `bpy.ops.wm.obj_import(...)` 中传入 `up_axis="Z"`，让 Blender 导入时先转正，再以 `export_yup=True` 导出。
- **Mixamo 多动作 FBX**：先下载一份带蒙皮（`With Skin`）的角色基础文件，再按 `Without Skin` 下载其余动作，把各个 Action 导入同一个 `.blend` 文件并推入 NLA 轨道，最后合并导出为单个 `.glb`。
- **独立检查与优化**：导出后可用 `gltf-transform inspect model.glb` 查看节点树、包围盒与贴图占用，并用 Khronos 官方的 `gltf_validator` 做严格规范校验。

## 压缩几何数据

`game/gltf` 用纯 Go 解码两种常见的 glTF 几何压缩扩展，无需 C++ 或 Wasm 解码器，压缩文件与普通文件一样通过 `gltf.Load` 和 `gltf.LoadFS` 加载：

| 扩展 | 压缩对象 | 常见生成工具 |
| --- | --- | --- |
| `KHR_draco_mesh_compression` | 整个三角形图元：连接关系与量化后的属性 | Blender 导出器（*Data → Compression*）、`gltf-pipeline -d`、`gltf-transform draco` |
| `EXT_meshopt_compression`、`KHR_meshopt_compression` | 单个 buffer view：顶点属性、三角形与索引列表、动画关键帧 | `gltfpack -c`、`gltf-transform meshopt` |
| `KHR_mesh_quantization` | 以整数存储的位置、法线与纹理坐标，通常与 meshopt 搭配 | `gltfpack`、`gltf-transform quantize` |

各读取接口返回的就是解码后的数据：`Primitive.Positions`、`Indices` 等属性、`ReadVec3` 等函数、动画通道与蒙皮看到的都是未压缩的值。标记为 meshopt 回退的 buffer 不会被读取，因此把未压缩回退数据放在独立可选 `.bin` 中的 `.glb` 在缺少该文件时也能加载。

如何选择：

- **Draco** 对密集的静态网格压缩率最高，但解码较慢，且可能改变顶点的顺序和数量，因此与未压缩文件对比的工具必须按数值而非索引匹配。量化有损：Blender 默认 14 位的位置精度足以应付道具，大型网格请检查近景效果。
- **meshopt** 解码快得多，保持顶点顺序，还能压缩动画数据，适合蒙皮角色和由大量小网格组成的场景。它的滤波器（octahedral、quaternion、exponential、color）有损；不加滤波器的属性流、三角形流和索引流无损。

两种解码器在分配之前，就把输出和工作内存与未压缩数据计入同一份单次加载预算，因此声明了巨大数量或大量宽属性的小数据流会立即失败；格式错误的数据流返回错误而不会 panic。`EXT_meshopt_compression` 文件只能使用该扩展定义的内容：属性编码第 0 版，且不能用 `COLOR` 过滤器。其他必需扩展（如 `KHR_texture_basisu`）在加载时仍会被拒绝。

## 纹理变换与材质变体

`game/gltf` 同时支持 `KHR_texture_transform` 和 `KHR_materials_variants`（无论出现在 `extensionsUsed` 还是 `extensionsRequired` 中）：

- **`KHR_texture_transform`**：会在所有 PBR 与材质扩展贴图槽位（`BaseColorTexture`、`MetallicRoughnessTexture`、`NormalTexture`、`OcclusionTexture`、`EmissiveTexture`、`Clearcoat.Texture`、`Clearcoat.RoughnessTexture`、`Clearcoat.NormalTexture`、`Transmission.Texture`）上解析到 `TextureRef.Transform`（`*TextureTransform`）。
  - 默认值严格遵循 Khronos 规范（`offset: [0, 0]`、`rotation: 0`、`scale: [1, 1]`），并在 glTF UV 空间（`+U` 向右、`+V` 向下、正角度逆时针）中按精确的 `T * R * S` 顺序生效，可通过 `TextureTransform.Apply`、`TextureTransform.ApplyUV`、`TextureTransform.Matrix` 或 `TextureRef.ApplyUV` 计算。
  - 当 `KHR_texture_transform.texCoord` 存在时，`TextureRef.EffectiveTexCoord()` 会返回覆盖后的 UV 通道索引，而非 `TextureRef.TexCoord`。
  - `Document.PrimitiveTexCoords(prim, set)` 会直接返回 `TEXCOORD_0`/`TEXCOORD_1`，并按需解码更高的 UV 通道（`TEXCOORD_2` 等）；`Document.TransformTexCoords(prim, ref, out)` 可将图元对应贴图槽位变换后的 UV 写入可复用的切片中。
  - 加载时会拒绝格式错误的变换（非对象载荷、长度不为 2 的 `offset`/`scale`、非有限数值、负值或非整数 `texCoord`），并返回带位置信息的 `*loaderr.Error`。
- **`KHR_materials_variants`**：顶层变体列表加载到 `Document.Variants`（`[]MaterialVariant`），逐图元映射加载到 `Primitive.VariantMappings`（`map[int]int`）。
  - 可用 `Document.VariantIndex(name)` 按名称查找变体索引，并通过 `Document.MaterialForPrimitive(prim, variantIndex)`、`Document.MaterialForVariant(prim, name)`、`Document.MeshMaterials(meshIndex, variantIndex)` 和 `Document.ResolveVariantMaterials(variantIndex)` 解析材质，且不修改 `Primitive.Material`。未映射的图元或越界/负值变体会自动回退到 `Primitive.Material`。
  - 加载时严格校验规范约束：`variants` 与 `mappings` 均须非空、每个变体必须有 `name`、材质与变体索引须在有效范围内，且同一图元上每个变体索引最多映射一次。

## 材质扩展

`game/gltf` 接受出现在 `extensionsUsed` 与 `extensionsRequired` 中的四个材质扩展：

- **`KHR_materials_unlit`**：设置 `Material.Unlit`。
- **`KHR_materials_emissive_strength`**：`Material.EmissiveStrength()` 返回 `EmissiveFactor` 的倍率；扩展或其 `emissiveStrength` 省略时为 1，载荷不是对象或数值不是 >= 0 的有限数时返回错误。
- **`KHR_materials_clearcoat`**：解析到 `Material.Clearcoat`（`*Clearcoat`），包含 `Factor`、`RoughnessFactor`、`Texture`、`RoughnessTexture` 与 `NormalTexture`（完整支持 `Scale`、`TexCoord` 与 `KHR_texture_transform`），加载时拒绝越界因子或无效贴图引用。
- **`KHR_materials_transmission`**：解析到 `Material.Transmission`（`*Transmission`），包含 `Factor` 与 `Texture`（完整支持 `TexCoord` 与 `KHR_texture_transform`），加载时拒绝越界因子或无效贴图引用。

## 烘焙光照贴图

在 Blender（或其他烘焙工具）中把光照烘焙到第二套 UV，导出为 `TEXCOORD_1`，并在材质上用 `MOZ_lightmap`（Hubs Blender 插件写出的格式）、`EXT_lightmap` 或 `extras.lightmap` 对象指向烘焙贴图。两个扩展出现在 `extensionsUsed` 和 `extensionsRequired` 中都会被接受。

- `Document.MaterialLightmap(materialIndex)` 返回 `*LightmapRef`（材质没有光照贴图时为 nil），包含贴图、`Intensity`、`Encoding` 提示（`linear`、`srgb`、`rgbm`、`rgbd`）、`RGBMMaxRange` 以及图集的 `UVScale` / `UVOffset`。
- `UVScale` / `UVOffset` 是唯一生效的图集变换，来源按优先级从低到高依次为：光照贴图 `KHR_texture_transform` 的 scale 与 offset、`scaleOffset` 字段 `[sx, sy, ox, oy]`、单独的 `uvScale` / `uvOffset` 字段。带旋转的光照贴图变换会被拒绝。该变换不会留在 `LightmapRef.Texture.Transform` 中（那里只保留 `texCoord` 覆盖），因此对它调用 `TransformTexCoords` 不会把图集变换应用两次。
- `Document.LightmapTexCoords(prim, ref)` 返回光照贴图所用的 UV，缺少 `TEXCOORD_1` 时回退到 `TEXCOORD_0`，两者都没有时返回错误。

绘制方式见 [render3d](render3d.md#烘焙光照贴图lightmapmeshlightmaprenderer)。

## 变形目标

`game/gltf` 解码 glTF 2.0 变形目标（`POSITION`、`NORMAL`、`TANGENT` 位移增量，支持稀疏访问器）、默认 `mesh.weights` 与 `node.weights`，以及 `weights` 权重动画通道（`STEP`、`LINEAR`、`CUBICSPLINE`）。在 Blender 中通过网格的 **Shape Keys**（形态键）制作变形目标，在 Action 中为其 *Value* 滑块打关键帧，并在 glTF 2.0 导出器中启用 *Data → Shape Keys*（按需勾选 *Shape Key Normals* 和 *Shape Key Tangents*）。

运行时复用权重切片与 `MorphResult`，即可在每帧零分配地求值变形目标：

```go
weights := gltf.ResolveWeights(doc, nodeIdx) // node.Weights > mesh.Weights > 全零
var morphed gltf.MorphResult

// 每帧：
if sampled, ok := gltf.SampleMorphWeights(doc.Animations[0], nodeIdx, animTime, weights); ok {
	weights = sampled
}
gltf.ApplyMorphWeights(prim, weights, &morphed)
// morphed.Positions、morphed.Normals（已重归一化）、morphed.Tangents（XYZ 重归一化、保留 W 手性）、morphed.Bounds
```

- `ResolveWeights` 与 `ResolveWeightsInto` 按 glTF 2.0 优先级解析权重（`node.Weights` 覆盖 `mesh.Weights`；缺省时返回与图元变形目标数量一致的零值切片）。
- `SampleMorphWeights` 按每个关键帧解包 `N` 个目标权重（`CUBICSPLINE` 按 `[inTangent, value, outTangent]` 顺序解包每关键帧 `3 * N` 个浮点值），超出关键帧时间范围时钳制到首尾帧。
- `EvaluateMorphTangents` 混合切线的 `XYZ` 增量并重新归一化，同时原样保留 `W` 手性符号，与 `EvaluateMorphPositions` 和 `EvaluateMorphNormals` 配套。
- 如需在 NSGo `gpu.View` 中绘制带动画的变形网格，可将基础图元与变形目标上传到 `render3d.GPUMorphMesh`（见 [`render3d`](render3d.md#变形目标网格morphmeshgpumorphmesh)）。测试套件使用 Khronos `AnimatedMorphCube` 示例模型验证了完整动画播放与像素回读。

## 碰撞节点（`-colonly` 与 `extras.collider`）

在 Blender 中可以直接通过 Godot 风格的节点名后缀或自定义属性（`extras.collider`）与视觉网格一起制作关卡碰撞体，并在绘制时自动跳过仅碰撞节点：

| 节点名后缀 | 碰撞形状 | 是否绘制？ |
| --- | --- | --- |
| `-col` / `-colonly` | 三角网格（`ColliderTriMesh`） | 是 / 否 |
| `-boxcol` / `-boxcolonly` | 定向盒体（`ColliderBox`） | 是 / 否 |
| `-spherecol` / `-spherecolonly` | 球体（`ColliderSphere`） | 是 / 否 |
| `-capsulecol` / `-capsulecolonly` | 胶囊体（`ColliderCapsule`） | 是 / 否 |
| `-convcol` / `-convcolonly` | 凸包（`ColliderConvex`，当前回退为三角网格） | 是 / 否 |

后缀匹配不区分大小写，并自动兼容 Blender 复制对象时追加的数字后缀（如 `Wall-colonly.001`）。节点也可通过 `extras.collider` 声明碰撞体（支持形状字符串、`true`/`false`，或包含 `shape`/`type`、`draw`/`only`/`visible`、`trigger`、`tag`、`center`、`halfExtents`/`size`、`radius`、`height`、`friction`、`restitution`、`layer`、`mask` 的对象），并支持挂在无网格的空节点上。

- **过滤绘制节点**：调用 `doc.DrawableNodes()`（或 `doc.SceneDrawableNodes(sceneIdx)` / `node.IsDrawable()`）即可排除 `-colonly` 和 `draw: false` 节点。
- **读取元数据或创建物理碰撞体**：调用 `doc.ColliderNodes()` / `gltf.ParseNodeCollider(node)` 读取原始标注，或直接传给 [`physics3d.World.AddGLTFColliders`](physics3d.md#gltfgridmap-与高度图碰撞体)。

## 往返校验示例模型

[占位素材包](placeholder.md)在 [`pkg/placeholder/pack/models/`](../../pkg/placeholder/pack/models/) 中包含三个极简、字节级确定的 glTF 2.0 示例模型，由 [`pkg/placeholder/internal/samplegltf/`](../../pkg/placeholder/internal/samplegltf/samplegltf.go) 生成并完成往返校验。与素材包的其他文件一样，它们以 CC0 1.0 贡献到公有领域，可用 `placeholder.Model` 加载。

### 1. 静态二进制木箱（`crate.glb`）

文件：[`models/crate.glb`](../../pkg/placeholder/pack/models/crate.glb)（1,920 字节，GLB 2.0）

- **几何尺寸**：`1 m × 1 m × 1 m` 立方体，原点位于底面中心（`X ∈ [-0.5, 0.5]`、`Y ∈ [0, 1]`、`Z ∈ [-0.5, 0.5]`），右手系 `+Y` 向上、`-Z` 向前。
- **网格属性**：24 个顶点（每面 4 顶点以保持清晰硬法线）、12 个三角形（36 个 `UNSIGNED_SHORT` 索引）、严格 CCW 外法线绕序，包含带精确 `min`/`max` 的 `POSITION`、单位长度 `NORMAL` 与逐面 `TEXCOORD_0`。
- **材质**：`CrateMaterial`，采用 `pbrMetallicRoughness`（`baseColorFactor: [0.72, 0.52, 0.32, 1]`、`metallicFactor: 0.05`、`roughnessFactor: 0.78`、`doubleSided: false`）。
- **容器布局**：12 字节 GLB 文件头（`glTF` 魔数 `0x46546C67`、版本 `2`、4 字节对齐的总长度）、空格填充的 `JSON` 块（`0x4E4F534A`）以及零字节填充的 `BIN` 块（`0x004E4942`）。

### 2. 蒙皮与动画双骨机械臂（`robot_arm.gltf`）

文件：[`models/robot_arm.gltf`](../../pkg/placeholder/pack/models/robot_arm.gltf)（内嵌 base64 缓冲区，便于直接阅读 JSON）

- **节点层级**：
  ```text
  ArmRoot (node 0)
  ├─ ArmMesh (node 1: mesh 0, skin 0)
  └─ UpperArm (node 2: 位于 0, 0, 0 的骨骼根节点)
     └─ Forearm (node 3: translation 0, 0.5, 0)
  ```
- **几何与蒙皮**：`0.25 m × 1.0 m × 0.25 m` 分段机械臂（`X ∈ [-0.125, 0.125]`、`Y ∈ [0, 1]`、`Z ∈ [-0.125, 0.125]`），32 个顶点、20 个三角形（60 个索引）。位于 `y = 0.5` 的共用肘部环在两根骨骼间平滑过渡（`JOINTS_0 = [0, 1, 0, 0]`、`WEIGHTS_0 = [0.5, 0.5, 0, 0]`），底部与顶部环分别以 `1.0` 权重绑定到 `UpperArm` 和 `Forearm`。
- **蒙皮定义（`ArmSkin`）**：`joints: [2, 3]`、`skeleton: 2`，`inverseBindMatrices` 依次存放 `UpperArm` 的单位矩阵 `I` 与 `Forearm` 的平移矩阵 `T(0, -0.5, 0)`，精确满足 `worldBind × IBM = I`。
- **动画（`Wave`）**：1.0 秒剪辑（`times = [0, 0.5, 1]`），包含两条 `LINEAR` 四元数旋转通道（`UpperArm` 绕 Z 轴 `+15°`，`Forearm` 绕 Z 轴 `+60°`），以及三条 `extras.events` 事件标记（`0.0 s` 的 `wave_start`、`0.5 s` 的 `wave_apex`、`1.0 s` 的 `wave_end`）。

### 3. 平面着色宝石（`gem.glb`）

文件：[`models/gem.glb`](../../pkg/placeholder/pack/models/gem.glb)（1,924 字节，GLB 2.0）

- **几何尺寸**：宽 `0.8 m`、高 `1 m` 的六棱双锥，原点位于底部尖端（`Y ∈ [0, 1]`），腰线在 `y = 0.6`，右手系 `+Y` 向上。
- **网格属性**：36 个顶点、12 个三角形，每个面使用独立的 3 个顶点，因此每个面都有自己的单位长度 `NORMAL`，绕序为 CCW 外向。
- **材质**：`GemMaterial`，采用 `pbrMetallicRoughness`（`baseColorFactor: [0.13, 0.77, 0.45, 1]`、`metallicFactor: 0`、`roughnessFactor: 0.2`）。
- **可复现性**：生成器只使用精确舍入的运算，因此在每种架构上都写出相同的字节。

### 运行往返校验测试

[`pkg/placeholder/internal/samplegltf/samplegltf_test.go`](../../pkg/placeholder/internal/samplegltf/samplegltf_test.go) 会解包已提交的模型并逐项校验：

- GLB 魔数、版本 `2`、4 字节对齐、JSON/BIN 块边界与 BIN 零填充
- 内嵌 data URI 的 base64 解码与缓冲区精确字节长度
- Accessor 对齐、`POSITION` 的 `min`/`max` 边界、单位法线，以及每个三角形的 CCW 外向绕序
- 4 骨骼 `JOINTS_0`/`WEIGHTS_0` 索引范围与每顶点权重之和等于 `1.0`
- `skins` 中每根骨骼精确满足 `worldBind × inverseBindMatrix == Identity`
- 严格递增的动画时间戳、单位旋转四元数与时间范围内的 `extras.events`
- 与确定性 Go 生成器输出的逐字节一致

在仓库根目录运行测试：

```sh
go tool nsgo test ./pkg/placeholder/internal/samplegltf
```

修改生成器后，可用一条命令重新生成并校验已提交的模型：

```sh
go tool nsgo test ./pkg/placeholder/internal/samplegltf -update
```

## 故障排查

| 现象 | 常见原因 | 解决方法 |
| --- | --- | --- |
| 模型过大 100 倍或缩得极小 | 源资产按厘米建模，或对象缩放未应用 | 将 Blender 设为公制（`1.0`），执行 `Ctrl+A → Scale`，或转换 FBX 时传入 `0.01` 缩放 |
| 模型平躺或背对镜头 | 导出未勾选 `+Y Up`，或在 Blender 中朝向了 `+Y` 而非 `-Y` | 在 Blender 中将正面转至 `-Y`，执行 `Ctrl+A → Rotation`，并勾选 **`+Y Up`** |
| 表面透明或里外翻转 | 法线反向，或负缩放（`-1`）导致绕序反转 | 执行 `Ctrl+A → Scale`，打开 *Face Orientation* 视图检查并用 `Shift+N` 重算外法线 |
| 动画播放时模型炸裂或异常拉伸 | 骨骼/网格缩放未应用、单顶点超过 4 根骨骼权重或权重未归一化 | 对网格与骨骼应用缩放，执行 `Limit Total (4)` + `Normalize All`，取消勾选 `Include All Bone Influences` |
| 导出后丢失动画或只剩一条剪辑 | Action 未推入 NLA 轨道，或 IK/约束未烘焙采样 | 把所有 Action 推入 NLA 轨道，*Animation Mode* 设为 `Actions`，并勾选 `Always Sample Animations` |
| 材质发黑、纯白或丢失贴图 | 使用了 Blender 专属程序化节点，或贴图未打包 | 将程序化材质烘焙为 PBR 贴图接入 `Principled BSDF`，并导出为 `.glb` |
| 走路循环播完后角色瞬移回原点 | 原地循环动画里残留了根运动位移 | 原地循环清零 `Root`/`Hips` 水平位移；若使用根运动则在游戏逻辑中消费根骨骼增量 |
| 导出的 glTF 中没有 `extras.events` | 导出时未勾选自定义属性，或属性挂在了 Object 而非 Action 上 | 将事件写入每个 Action 的 `action["events"]`，并在导出时勾选 `Include → Custom Properties` |
| `gltf.Load` 拒绝加载文件 | 导出器写出了规范禁止的数据：索引超出顶点数、属性或变形目标的元素数不一致、顶点数据含 NaN/Inf、`JOINTS_0` 超出蒙皮关节列表、稀疏索引未递增、GLB 含第二个 JSON 块，或 `asset.minVersion` 高于 2.0 | 修正源网格（或重新导出），并用 Khronos `gltf_validator` 检查；错误信息会指出网格、图元和元素 |
| 节点的 `Translation`/`Rotation`/`Scale` 与 `LocalMatrix` 不一致 | 节点的 `matrix` 带有切变，TRS 无法表达，此时 `Node.MatrixNotTRS` 为 true | 使用 `LocalMatrix`，或在 Blender 中应用变换，让节点以普通 TRS 导出 |
