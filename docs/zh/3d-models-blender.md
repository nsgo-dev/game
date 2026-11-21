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

将运行时加载的 `.glb` 文件放入 `resources/`，这样 `nsgo build` 会自动把它们打包到六个目标平台，并在代码中通过 `"asset:..."` 读取。体积较大的制作源文件（`.blend`、`.fbx`、`.obj`、`.psd`）请放在 `resources/` 之外（例如 `art/`），避免被打包进发布产物：

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
- **Data → Compression**：默认**不要勾选 Draco 网格压缩**，以便纯 Go glTF 解析器直接读取而无需额外引入 C++/Wasm Draco 解码器。

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

## 往返校验示例模型

本仓库在 [`docs/assets/3d-sample/`](../assets/3d-sample/) 中提交了两个极简、字节级确定的 glTF 2.0 示例模型，由 [`packages/game/internal/samplegltf/`](../../packages/game/internal/samplegltf/samplegltf.go) 生成并完成往返校验。

### 1. 静态二进制木箱（`crate.glb`）

文件：[`docs/assets/3d-sample/crate.glb`](../assets/3d-sample/crate.glb)（1,920 字节，GLB 2.0）

- **几何尺寸**：`1 m × 1 m × 1 m` 立方体，原点位于底面中心（`X ∈ [-0.5, 0.5]`、`Y ∈ [0, 1]`、`Z ∈ [-0.5, 0.5]`），右手系 `+Y` 向上、`-Z` 向前。
- **网格属性**：24 个顶点（每面 4 顶点以保持清晰硬法线）、12 个三角形（36 个 `UNSIGNED_SHORT` 索引）、严格 CCW 外法线绕序，包含带精确 `min`/`max` 的 `POSITION`、单位长度 `NORMAL` 与逐面 `TEXCOORD_0`。
- **材质**：`CrateMaterial`，采用 `pbrMetallicRoughness`（`baseColorFactor: [0.72, 0.52, 0.32, 1]`、`metallicFactor: 0.05`、`roughnessFactor: 0.78`、`doubleSided: false`）。
- **容器布局**：12 字节 GLB 文件头（`glTF` 魔数 `0x46546C67`、版本 `2`、4 字节对齐的总长度）、空格填充的 `JSON` 块（`0x4E4F534A`）以及零字节填充的 `BIN` 块（`0x004E4942`）。

### 2. 蒙皮与动画双骨机械臂（`robot_arm.gltf`）

文件：[`docs/assets/3d-sample/robot_arm.gltf`](../assets/3d-sample/robot_arm.gltf)（内嵌 base64 缓冲区，便于直接阅读 JSON）

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

### 运行往返校验测试

[`packages/game/internal/samplegltf/samplegltf_test.go`](../../packages/game/internal/samplegltf/samplegltf_test.go) 会解包两个已提交的示例资产并逐项校验：

- GLB 魔数、版本 `2`、4 字节对齐、JSON/BIN 块边界与 BIN 零填充
- 内嵌 data URI 的 base64 解码与缓冲区精确字节长度
- Accessor 对齐、`POSITION` 的 `min`/`max` 边界、单位法线，以及每个三角形的 CCW 外向绕序
- 4 骨骼 `JOINTS_0`/`WEIGHTS_0` 索引范围与每顶点权重之和等于 `1.0`
- `skins` 中每根骨骼精确满足 `worldBind × inverseBindMatrix == Identity`
- 严格递增的动画时间戳、单位旋转四元数与时间范围内的 `extras.events`
- 与确定性 Go 生成器输出的逐字节一致

在 `packages/game/` 目录下运行测试：

```sh
go tool nsgo test ./internal/samplegltf
```

修改生成器后，可用一条命令重新生成并校验两个提交的示例文件：

```sh
go test ./internal/samplegltf -update
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
