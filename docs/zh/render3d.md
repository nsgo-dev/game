# render3d

`game/render3d` 提供 NSGo GPU 视图的网格缓冲、纹理与采样器、金属度-粗糙度 PBR 材质、相机 uniform、距离雾与高度雾，以及 HDR 渲染目标与色调映射 pass，以及与后端无关、稳态零分配的 CPU 侧 3D 渲染管线与开发工具：视锥体裁剪、不透明/半透明绘制排序、自动实例化合批、带滞回防抖动的 LOD 选择、面向相机的公告板与 3D 条带拖尾、3D 调试线框与变换手柄（Gizmo）、帧性能分析器，以及移动端/桌面端 GPU 预算检查。

```go
import "github.com/nsgo-dev/game/packages/game/render3d"
```

所有矩阵、包围盒、射线与视锥体均遵循 [math3d](math3d.md) 约定。若要在 3D `gpu.View` 上叠加绘制 2D 精灵批次与屏幕空间图层，参见 [`game/scene` 中的 `Overlay3D`](scene.md#3d-视图上的-2d-图层overlay3d)。

## 视锥裁剪、绘制排序、实例化与 LOD（`Queue`）

跨帧复用同一个 `Queue`，把当前帧的 `[]DrawItem` 传入 `Queue.Build(viewProj, camPos, items)`：

```go
queue := &render3d.Queue{
	LODHysteresis: 0.1, // 10% 滞回区间，防止边界处 LOD 反复闪烁
}

plan := queue.Build(cam.ViewProj(), cam.Pos, drawItems)

// 1. 提交不透明实例化批次（连续的 Mat4 变换切片）：
for _, batch := range plan.Batches {
	submitInstanced(batch.PipelineID, batch.MaterialID, batch.MeshID, batch.Transforms)
}
// 2. 严格按由远及近提交半透明绘制：
for _, draw := range plan.Transparent {
	submitDraw(draw)
}
```

`Queue.Build` 在单次稳态零分配流程中完成：

1. **视锥裁剪**：通过 `math3d.FrustumFromMat4NO` 提取六平面并剔除视锥外的 `WorldBounds`。`[-1, 1]` 形式对 OpenGL 风格的投影（`math3d.PerspectiveNO`、`math3d.OrthoNO`）是精确的，对 `[0, 1]` 投影是保守的，因此同一个队列两种都能用；`Queue.FrustumNO` 仅为源码兼容保留，不再生效。
2. **遮挡剔除**（可选）：设置了 `Queue.Occluder` 时，通过视锥但 `WorldBounds` 未通过 `OcclusionBuffer.TestAABB` 的项会被跳过，并计入 `RenderStats.Occluded`。见[遮挡剔除](#遮挡剔除occlusionbuffer)。
3. **带滞回的 LOD 选择**：结合相机距离与 `ProjectScreenRatio` 评估每个物体的 `LODs`（`[]LODLevel`，由精细到粗糙），按 `DrawItem.ID` 记忆上一帧级别（`SelectLOD`）。镜头硬切时调用 `queue.ResetLODHistory()`。
4. **不透明排序与实例化**：将 `BlendOpaque` 与 `BlendAlphaMask` 按 `(PipelineID, MaterialID, MeshID, Blend, 由近及远的视空间深度)` 排序以减少状态切换与早期深度过绘，并将相邻同材质同网格项合并为 `InstanceBatch`（除非设置 `DisableInstancing`）。
5. **半透明排序**：将 `BlendAlphaBlend` 项放入 `plan.Transparent`，按视空间深度严格由远及近排序。两种排序都是稳定排序，键相同的绘制每帧都保持提交顺序。

## 遮挡剔除（`OcclusionBuffer`）

`OcclusionBuffer` 是一个带最大深度金字塔的小型 CPU 深度缓冲。每个视角把大型静态遮挡物（墙、地板、建筑）光栅化进去一次，队列就会跳过被它们挡住的项。它绝不会剔除可见项：穿过近平面的包围盒，或用与光栅化时不同的视角测试的包围盒，一律视为可见。

```go
occ := render3d.NewOcclusionBuffer(0, 0) // 默认 128x72
walls := []render3d.Occluder{
	render3d.NewBoxOccluder(wallBounds, wallTransform),
	render3d.NewQuadOccluder(a, b, c, d, true),
	render3d.OccluderFromMesh(vertices, indices, transform),
}

// 每帧使用与队列相同的 viewProj：
occ.Clear()
occ.Rasterize(viewProj, walls...)
occ.BuildPyramid()
plan := queue.BuildWithOcclusion(viewProj, camPos, drawItems, occ)
```

使用 OpenGL 风格的 `[-1, 1]` 投影时设置 `OcclusionBuffer.DepthNO`，使近平面一致。遮挡物必须不透明，并且是封闭网格或双面网格；只有能完全挡住后方物体的网格才适合放进来。

## GPU 上的相机 uniform（`CameraBuffer`）

`CameraBuffer` 在 NSGo 的 `gpu.Device` 上保存一个相机的 uniform 缓冲区，并提供绑定组布局和绑定组，在 `CameraGroup`（第 0 组）上供顶点和片元着色器读取。`CameraWGSL` 声明与之匹配的数据块，3D 着色器把它放在开头，用 `camera.view_proj` 变换顶点：

```wgsl
struct Camera {
	view: mat4x4<f32>,
	proj: mat4x4<f32>,
	view_proj: mat4x4<f32>,
	eye: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: Camera;
```

`CameraUniform` 是这个数据块的内容（`NewCameraUniform(cam)` 从 `scene3d.Camera` 填充），`Encode` 不分配内存地写出 `CameraUniformSize`（208）字节。设备就绪后创建一次缓冲区，每帧录制绘制命令前上传相机，并在每条 3D 管线的 `Layouts` 中把 `Layout()` 放在 `CameraGroup` 位置：

```go
camBuf, err := render3d.NewCameraBuffer(cx, dev)
// ...
layouts := []*gpu.BindGroupLayout{render3d.CameraGroup: camBuf.Layout()}
pipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{Layouts: layouts /* 着色器、顶点缓冲区、深度 */})

// 每帧，在 game.Loop.Draw 中：
camBuf.WriteCamera(&drawn)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
```

参考测试通过 `gpu.NewSoftwareRenderer` 无头渲染同一个场景，分别使用在 `game.Loop` 上环绕的透视相机和正交相机，并把每一帧与 `render3d/testdata/golden/camera_perspective.png` 和 `camera_orthographic.png` 比较；设置 `UPDATE_GOLDEN=1` 会重写这两张图。

## PBR 着色中的方向光与点光源（`LightRig`、`GPULightBuffer`）

`LightRig` 保存场景的线性 RGB 光照配置：一个 `AmbientLight`、最多 `MaxDirectionalLights`（4）个 `DirectionalLight`，以及最多 `MaxPointLights`（16）个 `PointLight`。

- **`DirectionalLight`**：沿 `Direction` 传播的平行光（例如 `{Y: -1}` 表示竖直向下照向 `+Y` 表面；着色或打包时会自动归一化），包含线性 `Color` 与 `Intensity`。
- **`PointLight`**：位于 `Position` 的全向点光源，包含线性 `Color`、`Intensity` 和可选截止距离 `Range`。当 `Range > 0` 时，距离衰减采用 glTF `KHR_lights_punctual` 平滑窗函数 `max(min(1 - (d/Range)^4, 1), 0)^2 / max(d^2, 1e-4)`，在 `Range` 处平滑归零；`Range == 0` 时采用带下界保护的纯平方反比衰减 `1 / max(d^2, 1e-4)`（`PointLightAttenuation`）。
- **`AmbientLight`**：各向同性的均匀线性环境填充光，按 `Ambient.Color * Ambient.Intensity * BaseColor` 叠加。

`GPULightBuffer` 将校验后的 `LightRig` 上传到 672 字节（`LightUniformSize`）、16 字节对齐的 std140/WGSL uniform 缓冲区，并绑定到 `LightGroup`（第 1 组），归属于传入的 `*core.Scope`，支持通过 `LightBufferOptions.Retained` 在 GPU 设备丢失后自动恢复：

```go
rig := render3d.LightRig{
	Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.16, Y: 0.22, Z: 0.32}, Intensity: 0.22},
	Directional: []render3d.DirectionalLight{
		{Direction: math3d.Vec3{X: 0.55, Y: -0.8, Z: -0.45}, Color: math3d.Vec3{X: 1, Y: 0.96, Z: 0.9}, Intensity: 2.6},
	},
	Point: []render3d.PointLight{
		{Position: math3d.Vec3{X: 1.6, Y: 1.15, Z: 1.5}, Color: math3d.Vec3{X: 0.25, Y: 0.72, Z: 1}, Intensity: 9, Range: 6.5},
	},
}

lightBuf, err := render3d.NewLightBuffer(cx, dev, rig, render3d.LightBufferOptions{Label: "scene", Retained: true})
drawBuf, err := render3d.NewPBRDrawBuffer(cx, dev, nil, render3d.NewPBRDrawUniform(model, render3d.PBRMaterial{
	BaseColor: math3d.Vec3{X: 0.82, Y: 0.14, Z: 0.12},
	Metallic:  0.0,
	Roughness: 0.22,
}), true)
pipe, err := render3d.NewLitPBRPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), gpu.FormatDepth24Plus)

// 每帧：
_ = lightBuf.Update(frame.Device(), rig)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)
```

自定义 3D 着色器只需在开头拼接 `CameraWGSL + LightWGSL`，即可访问 `@group(1) @binding(0) var<uniform> lights: LightRigUniform` 并调用可移植 WGSL 函数 `point_light_attenuation`、`pbr_brdf`、`shade_pbr` 与 `linear_to_srgb`。`LitPBRWGSL` 将它们与 `MeshVertexLayout` 及位于 `PBRDrawGroup`（第 2 组）的逐绘制 `PBRDrawUniform` 组合为完整管线。

着色模型遵循 glTF 2.0 金属度-粗糙度 PBR（电介质 `F0 = 0.04`、Schlick 菲涅尔、Trowbridge-Reitz GGX 法线分布、高度相关 Smith-GGX 可见度项、能量守恒 Lambert 漫反射、感知粗糙度下限 `0.045`，以及 IEC 61966-2-1 线性到 sRGB 输出）。`EvaluatePBR`、`ShadePoint`、`PointLightAttenuation` 与 `LinearToSRGB` 在 CPU 上实现完全一致的公式；`LightRig.AppendUniformData` 与 `LightUniforms.Encode` 零分配打包 uniform 块。测试逐像素比对 GPU 渲染结果与 CPU `ShadePoint`，覆盖完整能力与 WebGL 2 / GLES 3.0（`nsgotest.NoGPUCompute`）配置及 `Retained` 设备丢失恢复，并将光照场景与 `render3d/testdata/golden/lights_pbr_scene.png` 和 `lights_point_falloff.png` 参考图比对。

## 距离雾与高度雾（`Fog`、`GPUFogBuffer`）

`Fog` 配置距离雾（`FogOff`、`FogLinear`、`FogExp`、`FogExp2`）、沿视线解析积分的指数高度雾（`HeightEnabled`、`HeightBase`、`HeightFalloff`、`HeightDensity`），或两者同时启用。`Color` 为线性 RGB（`math3d.Vec3`），`MaxOpacity` 将最终混合系数上限限制在 `(0, 1]`（为零时取 `1.0`）。两者同时启用时，透射率相乘：

```text
factor = clamp(1 - (1 - f_dist) * (1 - f_height), 0, MaxOpacity)
```

高度雾沿视线起点到表面的线段对垂直消光 `HeightDensity * exp(-HeightFalloff * (y - HeightBase))` 做解析积分，接近水平的视线改用四项泰勒展开，因此掠射与垂直视线都保持平滑，高处相机向下看也不会溢出。

`GPUFogBuffer` 持有 48 字节的 `FogUniforms` 块（`FogUniformSize`），可选 `Retained` 在设备丢失后恢复。两条带光照的管线都可以使用它：

- **光源**：`NewLitPBRFogPipeline` 编译 `LitPBRFogWGSL`，即上文 `LitPBRWGSL` 的光照，并在相机、光源与逐绘制组之后的 `FogGroup`（第 3 组）绑定雾缓冲。`GPUFogBuffer.Bind` 绑定到该组。
- **材质**：设置 `MaterialPipelineOptions.FogLayout` 后，`NewMaterialPipeline` 编译 `MaterialFogWGSL`，在相机与材质组之后的 `MaterialFogGroup`（第 2 组）绑定雾缓冲。雾在材质着色器色调映射之前混合线性辐射度，因此雾的颜色会随场景一起被色调映射。

```go
fog := render3d.Fog{
	Mode:          render3d.FogExp,
	Color:         math3d.Vec3{X: 0.62, Y: 0.72, Z: 0.86},
	Density:       0.03,
	MaxOpacity:    0.9,
	HeightEnabled: true,
	HeightBase:    2,
	HeightFalloff: 0.25,
	HeightDensity: 0.05,
}
fogBuf, err := render3d.NewGPUFogBuffer(cx, dev, fog, render3d.FogBufferOptions{Label: "fog", Retained: true})
pipe, err := render3d.NewLitPBRFogPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), fogBuf.Layout(), gpu.FormatDepth24Plus)
matPipe, err := render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
	DepthFormat: gpu.FormatDepth24Plus,
	FogLayout:   fogBuf.Layout(),
})

// 每帧：
_ = fogBuf.Update(frame.Device(), fog)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
fogBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)

pass.SetPipeline(matPipe)
pass.SetBindGroup(render3d.MaterialFogGroup, fogBuf.BindGroup())
gpuMat.Bind(pass)
gltfMesh.Draw(pass)
```

透视相机的视线从眼睛位置出发。正交相机的视线从相机平面上、表面所投影的那一点出发，因此雾随到该平面的距离增长，而不是随到相机位置的距离增长。着色器中由 `FogCameraWGSL` 的 `fog_camera_pos(world_pos)` 计算这个起点，CPU 上由 `FogCameraPos(cameraUniform, worldPos)` 计算同一个点。

- **CPU 参考实现**：`Fog.Factor(camPos, worldPos)` 与 `Fog.Apply(color, camPos, worldPos)` 计算与着色器相同的公式，`camPos` 为 `FogCameraPos` 给出的视线起点。`Fog.Validate` 以 `ErrInvalidFog` 拒绝非有限值或越界字段，`Fog.Sanitized` 将其替换为安全默认值。`AppendFogUniforms` 打包 uniform 块时不分配内存。
- **自定义着色器**：前置 `FogWGSL` 获得 `Fog` 结构体、`fog_factor(cam_pos, world_pos, fog)` 与 `apply_fog(color, cam_pos, world_pos, fog)`，再前置 `FogBindingWGSL`（第 3 组）或 `MaterialFogBindingWGSL`（第 2 组）声明绑定，并在 `CameraWGSL` 之后前置 `FogCameraWGSL` 获得视线起点。`FogWGSL` 本身不声明绑定，因此着色器也可以把 `Fog` 嵌入自己的 uniform 块。

参考测试在软件 GPU 与 `nsgotest`（`full` 与 `nsgotest.NoGPUCompute` 配置）上，将渲染像素与经 `Fog.Apply` 混合后的 `ShadePoint` 和 `EvaluateMaterial` 结果对比，覆盖正交相机、`Update`、`Retained` 设备丢失与作用域释放，并与 `render3d/testdata/golden/fog_linear_distances.png` 和 `fog_height.png` 对比。

## 天空盒与基于图像的照明（`Cubemap`、`SkyboxPipeline`、`GPUIBLBuffer`）

`render3d` 提供六面立方体贴图（`Cubemap`、`GPUCubemap`）、全屏三角形背景天空盒（`SkyboxPipeline`）以及分离求和的基于图像的照明（`IrradianceSH`、`PrefilterEnvironment`、`GenerateBRDFLUT`、`EnvBRDFApprox`、`GPUIBLBuffer`、`IBLWGSL`、`LitPBRIBLWGSL`），可在所有 NSGo GPU 后端上运行，包括 WebGL 2 与 OpenGL ES 3.0（`NoGPUCompute`）。

可从六张贴图（`CubemapFromImages`）、等距柱状全景图（`CubemapFromEquirectangular`；常见的 PNG 或 JPEG 全景图传 `srgb = true`）或程序化三段天空（`GradientSkyCubemap`）构建 `Cubemap`，在 CPU 上烘焙 L2 球谐漫反射辐照度与 GGX 预过滤镜面 mip 链后再上传到 GPU：

```go
env, err := render3d.GradientSkyCubemap(zenith, horizon, ground, 64)
sh, err := render3d.ComputeIrradianceSH(env)
prefilter, err := render3d.PrefilterEnvironment(env, render3d.PrefilterOptions{
	BaseSize:    64,
	MipLevels:   5,
	SampleCount: 64,
})

skyCube, err := render3d.UploadCubemap(cx, dev, env, render3d.CubemapUpload{Label: "sky", Retained: true})
prefCube, err := render3d.UploadCubemap(cx, dev, prefilter, render3d.CubemapUpload{Label: "ibl", Retained: true})

skyPipe, err := render3d.NewSkyboxPipeline(cx, dev, render3d.SkyboxPipelineDesc{
	ColorFormat: dev.SurfaceFormat(),
	DepthFormat: gpu.FormatDepth24Plus,
	Retained:    true,
})
skyGroup, err := skyPipe.BindCubemap(cx, skyCube)

iblBuf, err := render3d.NewGPUIBLBuffer(cx, dev, prefCube, render3d.NewIBLUniform(sh, prefCube, 1.0), render3d.IBLBufferOptions{Label: "ibl", Retained: true})
pipe, err := render3d.NewLitPBRIBLPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), iblBuf.Layout(), gpu.FormatDepth24Plus)

// 每帧：先在 IBLGroup（第 3 组）绑定 IBL 缓冲绘制不透明受光网格，再绘制天空盒，跳过它们覆盖的像素：
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
iblBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)
skyPipe.WriteCamera(cam)
skyPipe.Draw(pass, skyGroup)
```

- **深度与平移无关**：`NewSkyboxUniform` 去除相机平移分量，使天空盒随观察者移动始终处于无限远。`SkyboxWGSL` 使用 `@builtin(vertex_index)` 绘制单个全屏三角形（不需要顶点缓冲），深度为 `1 - 2^-24`（小于 1 的最大 float32），配合 `CompareLessEqual` 与 `DepthWrite: false`：在不透明几何之后绘制时只着色未被覆盖的像素，且只会覆盖位于远平面最后一个 24 位深度级内的几何。之所以不输出恰好 `1.0`，是因为 NSGo 的软件光栅器会丢弃插值深度舍入后大于 1 的片元。
- **带环境光照的受光 PBR**：`LitPBRIBLWGSL` 是在 `LitPBRWGSL` 基础上加入 `evaluate_ibl`：在编码为 sRGB 之前，把球谐漫反射辐照度与预过滤镜面反射加到直接光照上。环境提供环境光项时，请把 `LightRig` 的环境光设为零。IBL 与雾都使用第 3 组（`IBLGroup` 与 `FogGroup`），这是所有后端都保证的四个绑定组中的最后一个，因此一条受光管线只能二选一。
- **自定义着色器**：在着色器前拼接 `IBLWGSL`（第 3 组）或 `IBLWGSLForGroup(group)`，并调用 `evaluate_ibl(normal, view_dir, base_color, metallic, roughness, ao)`。漫反射项计算 9 个预卷积 L2 球谐系数（`evaluate_irradiance_sh`），镜面项按 `roughness * ibl.max_lod` 采样预过滤立方体贴图并乘以 Karis 解析拟合 `env_brdf_approx`（`EnvBRDFApprox`）。需要 2D 查找表的着色器可以用 `GenerateBRDFLUT` 或 `GenerateBRDFLUTWithSamples` 烘焙分离求和 GGX 表。
- **缓冲、校验与内存**：`GPUIBLBuffer` 与 `GPULightBuffer`、`GPUFogBuffer` 一致：`Update(dev, u)` 写入新参数，`Bind` 绑定到 `IBLGroup`，`ByteSize` 报告其 uniform 缓冲供 `RenderStats` 统计，各立方体贴图则由 `GPUCubemap.ByteSize` 报告。`IBLUniform.Validate` 拒绝非有限的球谐系数，以及非有限或为负的强度与最大 LOD，返回 `ErrInvalidIBL`。
- **仅支持低动态范围**：立方体贴图每通道 8 位（`RGBA8Unorm` 或 `RGBA8UnormSRGB`），源贴图、预过滤贴图与辐照度贴图中大于 1 的辐射度都会被截断，明亮的太阳或天空高光会被压平；暂不支持高动态范围环境。
- **CPU 参考实现与设备丢失恢复**：`EvaluateIBL` 在 CPU 上匹配 `evaluate_ibl`，`ShadePoint` 加 `EvaluateIBL` 匹配 `LitPBRIBLWGSL`。在 `CubemapUpload`、`SkyboxPipelineDesc` 与 `IBLBufferOptions` 上设置 `Retained: true` 后，设备丢失时 NSGo 会恢复纹理、采样器、管线与 uniform 缓冲；请在 `OnReady` 中调用 `SkyboxPipeline.SetDevice(dev)`，并把新设备传给 `GPUIBLBuffer.Update`。
- **参考渲染测试**：无头软件 GPU 测试将网格后方的天空盒以及 2×2 金属度/粗糙度球体与 `render3d/testdata/golden/skybox_scene.png` 和 `ibl_spheres.png` 对比（设置 `UPDATE_GOLDEN=1` 可重写），对照 CPU 参考实现检查 `LitPBRIBLWGSL`，检查紧靠远平面内侧的几何不被覆盖，并在完整能力与 `nsgotest.NoGPUCompute` 两种配置下运行。

## 网格缓冲（`Mesh`、`GPUMesh`）

`Mesh` 是 CPU 上的三角形列表几何体：`Positions`（必填）、`Normals` 与 `UVs`（各自为空或与顶点一一对应）以及 `Indices`（非索引网格留空）。这些字段与 glTF 图元的 `Positions`、`Normals`、`TexCoords0`、`Indices` 类型相同，正面按逆时针环绕，与 glTF 和 [math3d](math3d.md) 一致。`UploadMesh` 校验网格后，在 [NSGo GPU 视图](https://github.com/nsgo-dev/nsgo/blob/main/docs/architecture/gpu.md) 的 `gpu.Device` 上创建顶点缓冲，带索引时再创建索引缓冲。缓冲归属于传入的作用域，作用域销毁或调用 `GPUMesh.Close` 时释放：

```go
gpu.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady: func(dev *gpu.Device) {
		if mesh == nil { // 设备丢失后 OnReady 会再次运行，此时 NSGo 已经恢复了保留的缓冲
			mesh, err = render3d.UploadMesh(cx, dev, render3d.Mesh{
				Positions: prim.Positions,
				Normals:   prim.Normals,
				UVs:       prim.TexCoords0,
				Indices:   prim.Indices,
			}, render3d.MeshUpload{Label: "crate", Retained: true})
		}
		pipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
			Vertex:        gpu.ShaderStage{Module: shader, EntryPoint: "vs_main"},
			Fragment:      gpu.ShaderStage{Module: shader, EntryPoint: "fs_main"},
			VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
			Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
			DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
			Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
		})
	},
	OnFrame: func(frame *gpu.Frame) {
		pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
		pass.SetPipeline(pipe)
		mesh.Draw(pass) // 或者 mesh.Bind(pass) 后自行调用 DrawIndexedInstanced
		pass.End()
		frame.Submit()
	},
})
```

所有网格共用同一种交错的小端布局，每个顶点 32 字节，因此一条管线可以绘制任意网格；缺少的法线或 UV 写为零：

| 属性 | `@location` | 格式 | 偏移 |
| --- | --- | --- | --- |
| 位置 | 0（`MeshPositionLocation`） | `float32x3` | 0 |
| 法线 | 1（`MeshNormalLocation`） | `float32x3` | 12 |
| UV | 2（`MeshUVLocation`） | `float32x2` | 24 |

网格最多 65,535 个顶点时索引写为 `uint16`，超过则写为 `uint32`，因此 `uint16` 缓冲里永远不会出现 WebGL 2 始终视为图元重启的索引 0xFFFF；索引数据按 WebGPU 的要求补零到 4 字节的倍数。这里只使用 GPU 视图所有后端都接受的格式：macOS 与 iOS 的 Metal、Windows 的 Direct3D 11、Linux 的 OpenGL 3.3、Web 的 WebGPU 或 WebGL 2、Android 的 OpenGL ES 3.0。本包的测试在无界面 `nsgotest` 的软件设备上绘制网格，分别使用完整能力和 WebGL 2 与 OpenGL ES 3.0 后端的能力（`nsgotest.NoGPUCompute`）；在各硬件后端上的绘制目前还没有自动化测试覆盖。

- `Mesh.Validate` 拒绝空网格、与顶点数不符的属性数、非有限值、不完整的三角形和越界索引；所有错误都包装 `ErrInvalidMesh`，网格无效时 `UploadMesh` 不会创建任何缓冲。
- `Mesh.AppendVertexData` 与 `Mesh.AppendIndexData` 把缓冲字节写入调用方持有的切片，容量足够时不分配内存，可用于通过 `Device.WriteBuffer` 或环形缓冲流式更新几何体。它们不做校验：请先调用 `Mesh.Validate`，无效网格写出的字节没有定义。
- `MeshUpload.Retained` 会保留一份 CPU 副本，设备丢失后 NSGo 重建缓冲，同一个 `GPUMesh` 可以继续绘制，因此保留的网格只需像示例那样上传一次；如果在 `OnReady` 里重新上传网格，则不必开启。
- 网格不是 `Valid` 时（上传失败得到 nil、已关闭、作用域已销毁，或设备丢失时没有保留），`GPUMesh.Draw` 与 `GPUMesh.Bind` 不记录任何命令，因此不会用之前网格留下绑定的缓冲去绘制。
- `GPUMesh.Bounds` 是物体空间包围盒，变换后即为 `DrawItem.WorldBounds`；`GPUMesh.ByteSize` 是占用的显存，用于 `RenderStats` 与预算。

## 纹理与采样器（`GPUTexture`、`GPUSampler`）

`UploadTexture` 把任意 Go `image.Image`（包括非零原点的子图像）转换为非预乘 RGBA8（`PixelBufferFromImage`；`*image.NRGBA` 即使 alpha 为 0 也会按原字节复制，保证法线/金属度等数据贴图不被破坏）并上传到 `gpu.Device`，返回归属于作用域的 `GPUTexture`。`UploadTexturePixels` 直接上传原始 `PixelBuffer` 字节，支持 `FormatRGBA8Unorm`、`FormatRGBA8UnormSRGB`、`FormatRG8Unorm` 与 `FormatR8Unorm`，并支持自定义行步长 `Stride`：

```go
texLayout, err := render3d.TextureBindingLayout(cx, dev)
albedo, err := render3d.UploadTexture(cx, dev, img, render3d.TextureUpload{
	Label:    "crate-albedo",
	SRGB:     true, // 颜色贴图采样时解码 sRGB 到线性空间；法线/金属粗糙度等数据贴图保持 false
	Mipmaps:  true, // 在 CPU 上按面积加权盒式滤波生成各级 mip（sRGB 在线性光下滤波）并逐级上传
	Retained: true, // 设备丢失后与保留的采样器一起自动恢复
})
samp, err := render3d.NewGPUSampler(cx, dev, render3d.SamplerLinearRepeat())
bg, err := render3d.BindTexture(cx, dev, texLayout, albedo, samp)

// 在 OnFrame 中与 CameraGroup（group 0）配合绑定：
pass.SetBindGroup(1, bg)
mesh.Draw(pass)
```

- **Mipmap 链**：开启 `TextureUpload.Mipmaps` 会在 CPU 上生成面积加权盒式滤波的 mipmap 链（`SRGB` 为 true 时在线性光下求平均，避免暗边）并通过 `Device.WriteTextureMip` 逐级上传。这不依赖后端的 GPU mipmap 生成能力，在所有 NSGo 后端行为一致，且在 `Retained` 开启时设备丢失后会自动重放所有层级；可用 `TextureUpload.MaxMipLevels` 限制层数。
- **KTX2 纹理**：`LoadKTX2` 上传 `game assets` 和 `game tex` 写出的那种 2D KTX2 文件，包括它含有的每一级 mip，保持其 sRGB 或线性格式，并返回绘制它所用的采样器：即 `KTX2Upload.Sampler`，其过滤方式按文件的 `game.filter` 键（`KTX2FilterKey`）设置，这个键由图片[导入设置附属文件](cli.md#资源旁的导入设置)的 `filter` 写入，因此 `nearest` 让像素画清晰锐利，`linear` 则做混合。Zstandard 和 ZLIB 超压缩的层级会先解压。BC1、BC3、BC7、ETC2 RGB8 与 RGBA8 以及 ASTC 4x4 层级在 `Device.SupportsCompressedFormat` 允许时以压缩格式上传，否则在 CPU 上解码为 RGBA8，因此在所有后端都能加载；`KTX2Sampler` 无需设备即可把过滤方式应用到采样器选项上。不是这类纹理的文件或未知的过滤方式会返回包装 `ErrInvalidTexture` 的错误：

  ```go
  data, err := fs.ReadFile(assets, "albedo.ktx2")
  albedo, opts, err := render3d.LoadKTX2(cx, dev, data, render3d.KTX2Upload{Label: "albedo", Retained: true, Sampler: render3d.SamplerLinearRepeat()})
  samp, err := render3d.NewGPUSampler(cx, dev, opts)
  ```

- **Basis Universal UASTC**：`LoadKTX2` 也接受 UASTC 4x4 的 KTX2 文件，即 Basis Universal 的高质量模式，并把它转码为设备支持的最佳块格式：依次为 BC7、不透明纹理用的 BC1、BC3、ETC2 RGB8 或 RGBA8（仅线性，因为 NSGo 没有 sRGB ETC2），然后是 ASTC 4x4。设备一种都不支持、基础层级的宽或高不是 4 的倍数，或设置了 `KTX2Upload.ForceRGBA8` 时，解码为 RGBA8。转码会在调用 `LoadKTX2` 的 goroutine 上把每个块解码后再编码：在桌面 CPU（Ryzen 9 3900X）上，一个 1024×1024 层级转为 BC7 约需 0.5 秒，转为 ETC2 约 0.8 秒，转为 ASTC 约 0.16 秒，2048×2048 约为四倍（见 `tooling/ktx2` 的 `BenchmarkTranscodeUASTC`），因此大的 UASTC 纹理应在加载画面中加载。`basisu` 的默认模式 BasisLZ/ETC1S 会被拒绝，不带 alpha 的 BC1（`VK_FORMAT_BC1_RGB_*`）也会被拒绝，因为以 RGBA BC1 上传会把它的穿透纹素画成透明。CPU 上的 ASTC 解码器只处理 `tooling/ktx2` 写出的单分区块，所以其他编码器生成的 ASTC 文件只能在支持 ASTC 的设备上加载。

- **动态更新**：`GPUTexture.UpdateImage` 与 `GPUTexture.UpdatePixels` 原地更新同尺寸、同格式的纹素，若纹理启用了 mipmap 会同时重算并重传各级 mip。
- **采样器预设与校验**：`SamplerLinearRepeat()`、`SamplerLinearClamp()`、`SamplerLinearMirror()`、`SamplerNearestRepeat()` 与 `SamplerNearestClamp()` 返回可修改的 `SamplerOptions`。`SamplerOptions.Validate` 拒绝非法滤波/寻址模式、非有限或倒置的 LOD 范围，以及在非全线性滤波下设置 `MaxAnisotropy > 1`（遵循 WebGPU 校验规则），错误均包装 `ErrInvalidSampler`。
- **生命周期与显存统计**：`GPUTexture.ByteSize` 累加所有已上传的 mip 层级字节数，供 `RenderStats.TextureBytes` 和 `Budget` 预算使用。`GPUTexture` 与 `GPUSampler` 会在所属 `core.Scope` 销毁时释放，也可提前调用 `Close()`，并可通过 `Valid()` 查询是否仍属于存活设备。
- **参考渲染验证**：`render3d` 的测试将带纹理网格、滤波、环绕、sRGB/线性采样与 mipmap 渲染结果与参考图（`render3d/testdata/golden/textured_mesh_scene.png` 和 `textured_filtering_wrapping.png`）比对，并在无界面 `gpu.View` 上同时测试完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置；设置 `UPDATE_GOLDEN=1` 可重写参考图。

### 变形目标网格（`MorphMesh`、`GPUMorphMesh`）

`MorphMesh` 将基础 `Mesh` 与逐顶点 `MorphTarget` 位置/法线位移增量（字段与 `gltf.MorphTarget` 对应）及可选初始 `Weights` 组合在一起。`UploadMorphMesh` 校验基础网格、变形目标与权重（非法数据均包装 `ErrInvalidMesh`），并创建 `GPUMorphMesh`，其底层仍使用与 `GPUMesh` 完全相同的 32 字节 `MeshVertexLayout` 顶点缓冲和可选索引缓冲，因此任何现有 3D 管线无需修改着色器即可在所有后端绘制——包括不具备计算着色器的 WebGL 2 与 OpenGL ES 3.0：

```go
morph, err := render3d.UploadMorphMesh(cx, dev, render3d.MorphMesh{
	Base: render3d.Mesh{
		Positions: prim.Positions,
		Normals:   prim.Normals,
		UVs:       prim.TexCoords0,
		Indices:   prim.Indices,
	},
	Targets: targets,
	Weights: gltf.ResolveWeights(doc, nodeIdx),
}, render3d.MeshUpload{Label: "character", Retained: true})

// 每帧录制绘制命令前：
if sampled, ok := gltf.SampleMorphWeights(anim, nodeIdx, animTime, weights); ok {
	weights = sampled
}
_ = morph.SetWeights(weights) // 仅在权重实际变化时标记脏状态
morph.Update(dev)             // 仅在脏状态时在 CPU 混合并通过 dev.WriteBuffer 上传
morph.Draw(pass)
```

- **脏状态追踪**：`SetWeights` 与当前权重逐项比较，仅当至少一个权重变化时才标记脏状态；未变时 `Update` 会同时跳过 CPU 混合与 `Device.WriteBuffer`。
- **实时物体空间包围盒**：`GPUMorphMesh.Bounds` 会按需执行待处理的 CPU 混合（并缓存结果，确保 `Update` 不会重复混合），同时同步更新底层 `GPUMesh.Bounds` 以供视锥裁剪使用。
- **设备丢失恢复**：开启 `MeshUpload.Retained` 后，设备丢失恢复时 NSGo 会用最近一次上传的变形顶点数据重建缓冲；若设备丢失期间调用了 `SetWeights`，网格会保持 `Dirty`，恢复后的首次 `Update` 会立即上传新姿态。

## GPU 蒙皮（`SkinnedMesh`、`GPUSkinnedMesh`、`GPUSkinPalette`）

`SkinnedMesh` 在 `Mesh` 的基础上增加逐顶点的 glTF 蒙皮属性（`Joints [][4]uint16` 与 `Weights []math3d.Vec4`）以及可选的 `JointCount`。`SkinnedMeshFromGLTF` 与 `SkinnedMeshesFromGLTFNode` 从已加载的 `gltf.Document` 提取并校验三角形图元，检查骨骼索引范围并将每个顶点的四个权重归一化为和等于 1（全零权重回退为 `(1, 0, 0, 0)`）。`UploadSkinnedMesh` 将 64 字节交错顶点缓冲与可选索引缓冲（65,535 个顶点及以下用 `uint16`，超过用 `uint32`）上传为归属于作用域的 `GPUSkinnedMesh`，支持 `Retained` 设备丢失自动恢复。

所有蒙皮网格共用同一种交错小端布局（`SkinnedVertexLayout`），位置 0..2 与 `MeshVertexLayout` 完全一致：

| 属性 | `@location` | 格式 | 偏移 |
| --- | --- | --- | --- |
| 位置 | 0（`SkinnedPositionLocation`） | `float32x3` | 0 |
| 法线 | 1（`SkinnedNormalLocation`） | `float32x3` | 12 |
| UV | 2（`SkinnedUVLocation`） | `float32x2` | 24 |
| 骨骼索引 | 3（`SkinnedJointsLocation`） | `uint32x4` | 32 |
| 骨骼权重 | 4（`SkinnedWeightsLocation`） | `float32x4` | 48 |

`GPUSkinPalette` 在可移植 uniform 缓冲中保存最多 `MaxSkinJoints`（128）个骨骼矩阵（`SkinUniformSize` = 8,192 字节，远低于 WebGL 2 与 OpenGL ES 3.0 的 16 KiB `MAX_UNIFORM_BLOCK_SIZE` 下限），未使用的槽位填充单位矩阵。`AppendJointMatrices` 以及根据 `doc.Nodes[].WorldMatrix` 计算 glTF 2.0 `jointNode.WorldMatrix * InverseBindMatrices[i]` 的 `SkinJointMatrices` / `SkinJointMatricesWithTransform` 均可复用调用方缓冲实现零分配。

有三种管线可以绘制 `GPUSkinnedMesh`，各自把骨骼矩阵组绑定在不同的组上：WebGPU 与 WebGL 2 只保证四个绑定组，而光照与材质管线已经占用了第 0 到第 2 组。

| 管线 | 着色器 | 骨骼矩阵组 | 不能同时使用 |
| --- | --- | --- | --- |
| `NewSkinnedMeshPipeline`（无光照，按法线着色） | `SkinnedMeshWGSL` | `SkinGroup`（1） | |
| `NewLitPBRPipelineWithOptions` 加 `LitPBRPipelineOptions.SkinLayout`（场景光源） | `LitPBRSkinnedWGSL` | `LitPBRSkinGroup`（3） | `FogLayout`、`IBLLayout` |
| `NewMaterialPipeline` 加 `MaterialPipelineOptions.SkinLayout`（带纹理的 glTF 材质） | `MaterialSkinnedWGSL` | `MaterialSkinGroup`（2） | `FogLayout` |

`SkinWGSL` 在第 1 组声明 uniform 数据块，以及与 CPU `SkinVertex` 一致的 `skin_position` / `skin_normal` 顶点辅助函数，供第 1 组不绑定其他资源的自定义着色器使用。

骨骼矩阵有两种约定。`SkinJointMatrices` 返回世界空间的骨骼矩阵，蒙皮后的顶点已在世界空间。anim3d 的 `Player.SkinMatricesForNode` 返回网格局部空间的矩阵 `inverse(meshWorld) * jointWorld * IBM`，且 Player 自己保存世界矩阵，不写入 `doc.Nodes`。网格局部的矩阵要用 `GPUSkinPalette.UpdateWithTransform` 配合网格节点的世界矩阵（`Player.WorldTransform`）上传，或者把该矩阵作为光照 PBR 绘制的 model 矩阵；否则网格节点自身的变换（CesiumMan 的 Z 轴朝上旋转）会丢失。用 anim3d 驱动、带光照的 CesiumMan：

```go
doc, _ := gltf.LoadFS(assetsFS, "CesiumMan.gltf")
const meshNode = 2
meshes, _ := render3d.SkinnedMeshesFromGLTFNode(doc, meshNode)
player, _ := anim3d.NewPlayer(doc)
_ = player.Play(0)

// 在 gpu.View 的 OnReady 中：
camBuf, _ := render3d.NewCameraBuffer(cx, dev)
lights, _ := render3d.NewLightBuffer(cx, dev, rig, render3d.LightBufferOptions{})
draw, _ := render3d.NewPBRDrawBuffer(cx, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat), true)
palette, _ := render3d.NewGPUSkinPalette(cx, dev, render3d.SkinPaletteOptions{Label: "hero", Retained: true})
gpuMesh, _ := render3d.UploadSkinnedMesh(cx, dev, meshes[0], render3d.MeshUpload{Label: "hero", Retained: true})
pipe, _ := render3d.NewLitPBRPipelineWithOptions(cx, dev, camBuf.Layout(), lights.Layout(), draw.Layout(), render3d.LitPBRPipelineOptions{
	DepthFormat: gpu.FormatDepth24Plus,
	SkinLayout:  palette.Layout(),
})

// 每帧：
player.Step(dt)
joints, _ := player.SkinMatricesForNode(meshNode) // 网格局部空间
_ = palette.Update(frame.Device(), joints)
draw.Write(frame.Device(), render3d.NewPBRDrawUniform(player.WorldTransform(meshNode), mat))
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lights.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, draw.BindGroup())
pass.SetBindGroup(render3d.LitPBRSkinGroup, palette.BindGroup())
gpuMesh.Draw(pass)
```

不用 anim3d 时，用 `doc.Animations[i].Apply(doc, t)` 与 `doc.ComputeWorldMatrices()` 摆好文档的姿势，再调用 `palette.UpdateFromGLTF(dev, doc, skinIndex)`（零分配），并以单位矩阵作为 model 绘制。

- **包围盒**：`SkinnedMesh.Bounds` 与 `GPUSkinnedMesh.Bounds` 覆盖的是网格自身空间中未蒙皮的顶点，而不是绘制出来的网格。视锥裁剪和相机取景请用 `SkinnedBounds(gpuMesh.Bounds(), joints)`：它是该包围盒在每个骨骼矩阵下变换结果的并集，在任何姿势下都包含蒙皮后的网格。
- **权重**：权重先除以其中最大的分量再求和，因此很大的有限权重也能正常归一化，不会溢出。
- **测试**：CesiumMan 与 RiggedSimple 在完整与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）两种配置下绘制，并与 CPU `SkinVertex` 的渲染结果比较；CesiumMan 的绑定姿势与网格节点的世界矩阵核对，光照与材质管线分别用两种骨骼矩阵约定验证。

## 金属度-粗糙度材质（`Material`、`MaterialDefaults`、`GPUMaterial`）

`Material`（别名 `MetallicRoughnessMaterial`）描述符合 glTF 2.0 规范的金属度-粗糙度 PBR 表面，涵盖基础色（`BaseColorFactor`、`BaseColorTexture`）、金属度与粗糙度（`MetallicFactor`、`RoughnessFactor`、遵循 glTF 约定的 G=粗糙度、B=金属度的 `MetallicRoughnessTexture`）、切线空间法线贴图（`NormalTexture`、`NormalScale`）、环境光遮蔽（`OcclusionTexture`、`OcclusionStrength`）、自发光（`EmissiveFactor`、`EmissiveTexture`）、透明模式（`AlphaOpaque`、带 `AlphaCutoff` 的 `AlphaMask`、`AlphaBlend`）以及双面渲染（`DoubleSided` 背面法线翻转）。

每个 `gpu.Device` 创建一份共享的 `MaterialDefaults`，持有 `MaterialGroup`（第 1 组）的绑定组布局、默认线性重复采样器以及四张 1×1 回退纹理（缺失基础色用的 sRGB 白色、缺失金属度-粗糙度与遮蔽用的线性白色、缺失法线贴图用的平坦法线 `(128, 128, 255, 255)` 以及缺失自发光用的黑色）。所有 `GPUMaterial` 在省略可选贴图时都会绑定这些回退纹理，因此全部材质共用同一个绑定组布局，无需管线排列组合：

```go
defaults, err := render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Retained: true})
pipe, err := render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
	DepthFormat: gpu.FormatDepth24Plus,
	AlphaMode:   render3d.AlphaOpaque,
})

mat := render3d.DefaultMaterial()
mat.BaseColorFactor = [4]float32{0.95, 0.78, 0.34, 1}
mat.MetallicFactor = 1.0
mat.RoughnessFactor = 1.0
mat.MetallicRoughnessTexture = mrTex // G = 粗糙度，B = 金属度
mat.NormalTexture = normalTex        // 切线空间法线贴图（+X 向右，+Y 向上，+Z 向外）

gpuMat, err := render3d.NewGPUMaterial(cx, dev, defaults, mat, render3d.MaterialUpload{
	Label:    "brushed-gold",
	Retained: true,
})

// 在 OnFrame 中与 CameraGroup（group 0）配合绑定：
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
gpuMat.Bind(pass) // 绑定到 MaterialGroup（group 1）
mesh.Draw(pass)
```

- **零分配 uniform 编码与动态更新**：`MaterialUniforms.AppendUniformData` 与 `Encode` 按 16 字节对齐写出 64 字节 uniform 块（`MaterialUniformSize`），容量足够时零分配。`GPUMaterial.Update(dev, m)` 原地更新标量因子与标志位而不重建绑定组；显式传入 `dev` 使开启 `Retained` 的材质在设备丢失并恢复后仍可持续更新。
- **跨平台法线贴图与 Cook-Torrance GGX 着色**：`MaterialWGSL`（由 `CameraWGSL` 与 `MaterialBindingsWGSL` 组成）利用屏幕空间偏导数 `dpdx`/`dpdy` 按克莱姆法则构建与图形 API 无关的切线空间标架（统一适配 Metal、D3D11、OpenGL、WebGPU、WebGL 2 与 GLES 3.0），并在偏导数或 UV 退化时回退到确定性正交基，使标准 32 字节 `MeshVertexLayout` 网格无需额外顶点属性即可获得准确的法线贴图效果。
- **CPU 参考求值与黄金图验证**：`EvaluateMaterial` 以纯 Go 实现与着色器一致的 Cook-Torrance GGX BRDF、半球环境光、法线扰动、alpha 裁切与 Reinhard + sRGB 色调映射。`render3d` 的测试将 GPU 像素回读与 `EvaluateMaterial` 逐像素校验，比对黄金参考图（`pbr_metallic_roughness_ramp.png` 与 `pbr_textured_normal_maps.png`），并在 `gpu.View` 上同时验证完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置；设置 `UPDATE_GOLDEN=1` 可重写参考图。

### glTF 2.0 材质与纹理（`UploadGLTFMaterials`、`GLTFMaterialSet`）

`UploadGLTFMaterials` 将已加载的 `*gltf.Document` 中的全部材质、纹理、图像与采样器映射到 `render3d` 的 `Material`、`GPUMaterial`、`GPUTexture` 与 `GPUSampler`：

```go
doc, err := gltf.LoadFS(assetsFS, "DamagedHelmet.gltf")
matSet, err := render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{
	Label:    "DamagedHelmet",
	Retained: true, // 设备丢失后自动恢复上传的纹理、采样器与材质
})
defer matSet.Close()

// 按图元解析材质（支持 KHR_materials_variants 变体切换以及 Material == -1 默认材质回退）：
gpuMat := matSet.MaterialForVariant(prim, "NightOps") // 或 matSet.MaterialForPrimitive(prim, -1)
gpuMat.Bind(pass)
gpuMesh.Draw(pass)
```

- **自动色彩空间分离与去重**：`baseColorTexture` 与 `emissiveTexture` 自动以 `SRGB: true`（`FormatRGBA8UnormSRGB`）上传，而 `metallicRoughnessTexture`、`normalTexture` 与 `occlusionTexture` 以线性格式（`FormatRGBA8Unorm`）上传。被引用的 glTF 图像按图像索引仅解码一次，并在 GPU 上按 `(imageIndex, sRGB)` 去重：同时用于 `occlusionTexture` 与 `metallicRoughnessTexture`（或跨多个材质复用）的打包 ORM 贴图仅上传一次；若同一张图既用于颜色槽又用于数据槽，则会分别为两种色彩空间各上传一份纹理。
- **glTF 2.0 采样器与 Mipmap**：`SamplerOptionsFromGLTF` 完整映射全部六种 glTF 滤波模式与三种环绕模式（`Repeat`、`ClampToEdge`、`MirroredRepeat`），省略环绕模式时按规范默认为 `AddressRepeat`（确保 UV 超出 `[0, 1]` 的资产，如 `DamagedHelmet` 的 `V ∈ [1, 2]` 正常环绕），省略滤波模式时默认使用三线性滤波并生成 CPU mipmap 链。未指定采样器的纹理与空采样器对象一样使用 glTF 默认采样器。不带 mipmap 的 `NEAREST` 与 `LINEAR` 缩小滤波会把细节层级限制在基础层，即使同一张图因其他采样器而带有 mip 链上传，也保持清晰。`GLTFMaterialUpload.MaxAnisotropy` 仅对全线性采样器启用各向异性过滤以满足 WebGPU 校验规则；也可通过 `DisableMipmaps` 关闭 mip 链生成。
- **扩展、元数据与默认材质**：`GLTFMaterialSet` 始终创建一份 glTF 2.0 默认材质，通过 `DefaultMaterial()` 或 `Material(-1)` 供未指定材质的图元使用。`KHR_materials_emissive_strength` 已折算进 `EmissiveFactor`；按规范允许的负值 `normalTexture.scale` 会翻转法线贴图的 X 与 Y。`Info(i)` / `InfoForVariant(prim, name)` 返回包含 `KHR_materials_unlit` 标志（`Unlit`）以及各槽位 `GLTFTextureSlot` UV 元数据（`EffectiveTexCoord` 与 `KHR_texture_transform` 的 `Transform`）的 `GLTFMaterialInfo`。
- **`MaterialWGSL` 不会绘制的内容**：该着色器对所有材质都做光照，并且所有纹理都用网格的 `TEXCOORD_0` 采样、不做变换。无光照材质、纹理变换和其他 UV 通道只被记录而不会生效，若不另行处理，这类资产会绘制错误。`GLTFMaterialInfo.Unsupported()` 列出某个材质的这些情况，`GLTFTextureSlot.NeedsUVBake()` 报告需要把 UV 烘焙进网格（例如用 `Document.TransformTexCoords`）或改用自定义着色器的槽位。
- **Khronos 参考验证**：`render3d` 的测试渲染 Khronos `DamagedHelmet` 样本，并以资产自身的基础色图像为准进行校验：用探测纹理读回每个像素的 UV，渲染结果必须在 glTF 的 UV 原点、sRGB 色彩与通道顺序下与解码后的 JPEG 吻合，且明显优于 V 翻转、按线性上传或通道交换的情形。测试还会与 `render3d/testdata/damagedhelmet/reference.png` 比对以捕捉回归，并在 `gpu.View` 上验证完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置的像素一致性，以及 `Retained` 设备丢失恢复。该模型采用 CC-BY-NC-4.0 许可，因此其目录是独立模块，不包含在发布的 `packages/game` 模块中；目录缺失时这些测试会跳过。

## 按预算跨帧异步 GPU 上传（`Uploader`）

`Uploader` 在后台工作协程上完成网格、纹理和原始 GPU 数据的解码与 CPU 打包；每帧调用一次的 `Uploader.Step` 再按每帧字节预算把结果分块写入 `gpu.Device`，加载大型 3D 模型时不会卡住某一帧：

```go
gpu.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady: func(dev *gpu.Device) {
		uploader = render3d.NewUploader(cx, dev, render3d.UploaderOptions{
			BytesPerFrame: 2 * 1024 * 1024, // 每帧 2 MiB（默认 4 MiB）
			ChunkBytes:    256 * 1024,      // 单次写入 256 KiB
			Workers:       2,               // 后台解码协程数
		})

		meshHandle = uploader.QueueMesh(ctx, render3d.PriorityHigh, func(ctx context.Context) (render3d.Mesh, error) {
			return loadAndDecodeMesh(ctx, "hero.glb")
		}, func(g *render3d.GPUMesh, err error) {
			if err == nil {
				heroMesh = g
			}
		}, render3d.MeshUpload{Label: "hero", Retained: true})

		texHandle = uploader.QueueTexture(ctx, render3d.PriorityNormal, func(ctx context.Context) (image.Image, error) {
			return decodeTexturePNG(ctx, "hero_albedo.png")
		}, func(tex *render3d.GPUTexture, err error) {
			if err == nil {
				heroAlbedo, _ = render3d.BindTexture(cx, dev, texLayout, tex, sampler)
			}
		}, render3d.TextureUpload{Label: "hero_albedo", SRGB: true, Mipmaps: true, Retained: true})
	},
	OnFrame: func(frame *gpu.Frame) {
		stats := uploader.Step() // 在引擎协程上最多写入 BytesPerFrame 字节
		_ = stats
		// 用已经就绪的网格与纹理绘制本帧……
	},
})
```

- **可直接替换同步上传**：`QueueMesh` 接受与 `UploadMesh` 相同的 `MeshUpload` 选项，交付 `GPUMesh`；`QueueTexture` 与 `QueueTexturePixels` 接受与 `UploadTexture`、`UploadTexturePixels` 相同的 `TextureUpload` 选项，交付的 `GPUTexture` 纹素相同：同样的直通 alpha 转换（`PixelBufferFromImage`）、同样的 mip 链（sRGB 纹理在线性光下做盒式滤波），因此可以直接用于 `BindTexture`，也计入 `GPUTexture.ByteSize`。每个 `Queue*` 调用另带一个 `UploadPriority`。
- **后台解码，引擎协程上传**：解码回调、网格校验与打包、图像转换和 mip 生成在工作协程上执行；GPU 资源创建、分块写入（`WriteBuffer`、`WriteTextureRegion`、`WriteTexture`）和 `onReady` 回调在调用 `Step` 的协程上执行。`Queue*` 调用能当场报告的错误（解码函数为 nil、上传器已关闭、队列已满）会在调用返回前、在调用方协程上调用 `onReady`。
- **每帧字节预算**：`UploaderOptions.BytesPerFrame`（默认 `DefaultUploadBytesPerFrame`，4 MiB）与 `ChunkBytes`（默认 `DefaultUploadChunkBytes`，256 KiB）限制每次 `Step` 写入的字节数，可用 `SetBytesPerFrame`、`SetChunkBytes` 在运行时调整，例如在加载画面调高预算、在游玩时调低。预算限制的是每帧的上传量，不是时间上限。缓冲按 4 字节对齐的偏移拆分；未压缩纹理按 mip 级别、行带拆分，一行超出剩余预算时再拆成行的一部分；块压缩纹理按块行拆分。
- **原始缓冲与纹理**：`QueueRawBuffer` 流式上传自定义顶点、存储或 uniform 缓冲（`RawBufferUpload`）；`QueueRawTexture` 流式上传预编码或块压缩的 mip 链、数组层和立方体贴图面（`RawTextureUpload`、`TextureMipData`），交付 `gpu.Texture`。
- **优先级与取消**：`PriorityHigh`、`PriorityNormal`、`PriorityLow` 同时决定解码队列和上传队列的顺序（同一优先级先进先出），高优先级资产会在下一个分块抢占上传到一半的低优先级资产。每个 `Queue*` 调用返回 `*UploadHandle`，其 `State()`、`Progress()`、`Err()`、`Done()` 与 `Cancel()` 可在任意协程调用。
- **生命周期**：向 `NewUploader` 传入 `*core.Scope` 后，作用域销毁时会关闭上传器。`Close` 取消未完成的解码、等待全部工作协程退出、释放上传到一半的 GPU 资源，并以 `ErrUploaderClosed` 调用每个未完成上传的 `onReady`；它须在调用 `Step` 的协程上执行，作用域销毁正是如此。无界面测试可传入 `nil` 并自行调用 `Close`。`UploaderOptions.MaxPending` 限制未完成上传的数量，超出时以 `ErrUploadQueueFull` 失败。

## HDR 渲染、色调映射与伽马（`HDRTarget`、`ToneMapPass`）

将场景的线性辐亮度渲染到 `HDRTarget`（`gpu.FormatRGBA16Float`，在 `SupportsHDR(dev)` 为 false 的设备上回退到 `gpu.FormatRGBA8Unorm`），再用 `ToneMapPass` 解析到显示目标。render3d 自带的着色器默认会做色调映射并编码为 sRGB，因此绘制到 HDR 目标时要使用它们的线性输出模式：光照 PBR 用 `LitPBRPipelineOptions.LinearOutput`（通过 `NewLitPBRPipelineWithOptions`，可带雾或 IBL），材质用 `MaterialPipelineOptions.LinearOutput`。不开启时，图像会被色调映射和伽马编码两次，HDR 目标里也只存着已被钳位到 `[0, 1]` 的值。

```go
var (
	hdr      *render3d.HDRTarget
	pipe     *gpu.Pipeline
	tonePass *render3d.ToneMapPass
	setupErr error
)

gpu.View(cx, gpu.ViewProps{
	OnReady: func(dev *gpu.Device) {
		if tonePass != nil {
			tonePass.SetDevice(dev) // 设备丢失后，保留的资源会原地恢复
			return
		}
		w, h := dev.SurfaceSize()
		hdr, setupErr = render3d.NewHDRTarget(cx, dev, render3d.HDRTargetDesc{
			Label:       "scene-hdr",
			Width:       uint32(w),
			Height:      uint32(h),
			DepthFormat: gpu.FormatDepth24Plus,
			Retained:    true,
		})
		if setupErr != nil {
			return
		}
		pipe, setupErr = render3d.NewLitPBRPipelineWithOptions(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
			ColorFormat:  hdr.Format(),
			DepthFormat:  gpu.FormatDepth24Plus,
			LinearOutput: true,
		})
		if setupErr != nil {
			return
		}
		tonePass, setupErr = render3d.NewToneMapPass(cx, dev, render3d.ToneMapPassDesc{
			Label:        "tonemap",
			TargetFormat: dev.SurfaceFormat(),
			Retained:     true,
		})
	},
	OnFrame: func(frame *gpu.Frame) {
		if setupErr != nil || tonePass == nil {
			return
		}
		w, h := frame.Size()
		if err := hdr.Resize(frame.Device(), uint32(w), uint32(h)); err != nil {
			return
		}

		// 1. 以线性 HDR 辐亮度渲染场景（保留大于 1.0 的值）：
		scenePass := frame.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x0a0e18), 1))
		scenePass.SetPipeline(pipe)
		scenePass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(scenePass)
		scenePass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
		mesh.Draw(scenePass)
		scenePass.End()

		// 2. 色调映射并编码为 sRGB 输出到交换链：
		opts := render3d.DefaultToneMapOptions()
		opts.Operator = render3d.ToneMapAgX
		opts.Exposure = render3d.ExposureFromEV(0.5)
		displayPass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "tonemap"})
		err := tonePass.DrawTarget(displayPass, frame.Device(), hdr, opts)
		displayPass.End()
		if err != nil {
			return
		}
		frame.Submit()
	},
})
```

`ToneMapOptions.Operator` 选择高光压缩曲线：

| 算子 | 曲线 | 适用场景 |
| --- | --- | --- |
| `ToneMapNone` | 线性钳位到 `[0, 1]` | 调试线性输入 |
| `ToneMapReinhard` | 逐通道 `c / (1 + c)` | 简单的平滑高光滚降 |
| `ToneMapACES`（`DefaultToneMapOptions`） | Narkowicz ACES 电影拟合曲线 | 高对比度电影观感 |
| `ToneMapNeutral` | Khronos PBR Neutral | 忠实保留中间调色相与 PBR 资产预览 |
| `ToneMapAgX` | Troy Sobotka AgX 默认观感 | 高光平滑去饱和且不偏色 |

- **曝光**：`ToneMapOptions.Exposure` 是线性倍数，可用 `ExposureFromEV(ev)` 按摄影档位计算（`0 -> 1.0`、`+1 -> 2.0`、`-1 -> 0.5`，档位钳位到 `[-126, 126]`，结果总为正数）。曝光为 `0` 时画面全黑，因此淡出到黑可以把它插值到 `0`；负数、NaN 和无穷大按 `1.0` 处理。请从 `DefaultToneMapOptions()` 开始设置，以免该字段的零值让画面变黑。
- **伽马与 sRGB**：当 `ToneMapOptions.Gamma <= 0`（默认）且 `LinearOutput` 为 false 时，`ToneMapPass` 应用精确的 IEC 61966-2-1 线性到 sRGB 传递函数（CPU 端为 `LinearToSRGBChannel` / `LinearToSRGB`，其逆为 `SRGBToLinearChannel` / `SRGBToLinear`）；正数 `Gamma`（如 `2.2`）应用 `pow(c, 1/Gamma)`。当 `ToneMapPassDesc.TargetFormat.IsSRGB()` 为真时，pass 输出线性的色调映射 RGB，由 sRGB 帧缓冲只编码一次，避免双重伽马。以 `LinearOutput` 绘制的材质用 `ToneMapReinhard` 加 `Gamma: 2.2` 解析后，与材质管线自身的输出一致。
- **溢出**：`RGBA16Float` 目标会把大于 65504 的值（例如强烈的镜面高光）存为 `+Inf`。pass 在曲线之前把曝光后的辐亮度钳位到 `[0, 65504]`，并把 NaN 变为 `0`（`ApplyToneMap` 同样如此），因此过曝的高光保持白色，而不会变黑。
- **动态分辨率**：用 HDR 的 `ColorFormat`（`PreferredHDRFormat(dev)`）创建 `DynamicResolutionTarget`，用 `Begin3D` 绘制场景，再调用 `ToneMapPass.DrawDynamicResolution` 代替 `Upscale`。它只对缩放后的子视口做色调映射，并在同一次绘制中用 pass 的采样器放大；不应用锐化滤镜。
- **每次提交一组参数**：每个 `ToneMapPass` 只有一个 uniform 缓冲，而缓冲写入会在本次提交的命令之前生效，因此同一个 pass 在一帧内记录的所有绘制都使用最后一次 `Draw` 的参数。要在一帧内以不同参数对多个视图做色调映射（分屏、画中画相机），请为每个视图各建一个 `ToneMapPass`。
- **分配与设备丢失**：`AppendToneMapUniforms` 零分配地编码 `ToneMapUniformSize`（48 字节）uniform 块，源纹理不变时 pass 复用其绑定组；每次绘制仍通过 `gpu.Device.WriteBuffer` 上传该块，而它会复制数据。在 `HDRTargetDesc` 与 `ToneMapPassDesc` 上开启 `Retained: true` 后，设备丢失时 NSGo 会重建纹理、采样器、uniform 缓冲、布局与管线，pass 会在下一次绘制时重建绑定组。
- **CPU 参考实现与可移植性**：`ApplyToneMap` 在 CPU 端镜像 `ToneMapWGSL`。由于 NSGo v0.3.0 的软件 GPU 对 `exp` 与 `log2` 内建函数返回 0，`ToneMapWGSL` 避开了它们（AgX 使用范围缩减多项式 `log2`），在完整 GPU 配置和 `nsgotest.NoGPUCompute`（WebGL 2 / OpenGL ES 3.0）下均可运行。
- **参考渲染**：测试通过光照 PBR 管线的线性输出模式绘制一个带自发光方块、高光远超 1.0 的场景，并与 `render3d/testdata/golden/tonemap_aces.png`、`tonemap_agx.png`、`tonemap_neutral.png` 比对；设置 `UPDATE_GOLDEN=1` 可重写参考图。另有测试经 `ApplyToneMap` 将光照 PBR 与材质的线性模式与 `ShadePoint`、`EvaluateMaterial` 比对。

## 抗锯齿（`MSAATarget`、`FXAAPass`）

`render3d` 在 GPU 设备支持时提供硬件多重采样抗锯齿（MSAA），并为单采样路径、后处理管线或不支持 MSAA 的设备提供全屏 FXAA 3.11 后处理通道：

| 模式（`AAMode`） | 行为 |
| --- | --- |
| `AANone` | 单采样（`1x`），不执行后处理。 |
| `AAFXAA` | 单采样渲染到离屏目标，随后执行 `FXAAPass`。 |
| `AAMSAA2x` | 请求 `2x` MSAA；由于 WebGPU 与 NSGo 仅暴露 `1x` 与 `4x` 采样数，`ClampSampleCount` 在 `Features.MSAA4x` 为真时提升为 `4x`，否则回退为 `1x`。 |
| `AAMSAA4x` | 请求 `4x` MSAA；`Features.MSAA4x` 为真时使用 `4x`，否则回退为 `1x`。 |

`MSAATarget` 封装了离屏颜色（及可选深度）`gpu.RenderTarget`，并通过 `ClampSampleCount` 自动裁剪采样数。视口尺寸变化时调用 `Resize`（尺寸未变时为零分配空操作），将 `ColorAttachment` / `DepthStencilAttachment`（或 `RenderPassDesc`）传给 `BeginRenderPass`，最后通过 `OutputTexture` 获取可供后续通道采样或回读的单采样结果（多重采样时返回解析纹理，单采样时返回颜色纹理）：

```go
target, err := render3d.NewMSAATarget(cx, dev, render3d.MSAATargetDesc{
	Label:       "scene",
	Width:       uint32(w),
	Height:      uint32(h),
	ColorFormat: gpu.FormatRGBA8Unorm,
	DepthFormat: gpu.FormatDepth24Plus,
	SampleCount: mode.SampleCount(), // 自动通过 ClampSampleCount 裁剪
	Retained:    true,
})
// 在 OnFrame 中：
desc, err := target.RenderPassDesc(clearColor, 1.0)
if err != nil {
	return // 目标已关闭或设备已丢失
}
pass := frame.BeginRenderPass(desc)
// ... 使用 SampleCount: target.SampleCount() 的管线绘制 ...
pass.End()
resolved := target.OutputTexture()
```

多重采样时，`ColorAttachment` 会自动把 `ResolveTarget` 指向解析纹理，并将 `StoreOp` 设为 `gpu.StoreDiscard`，避免移动端/图块 GPU 写回临时多重采样缓冲；回退到 `1x` 时则使用 `gpu.StoreStore` 直接写入颜色纹理。目标已关闭或设备已丢失时，`ColorAttachment` 与 `RenderPassDesc` 返回错误，而不是返回纹理为 nil 的附件（NSGo 会把这样的附件画到视图的交换链上）。

- **深度**：深度附件默认是临时的：在通道开始时清除、结束时丢弃（深度模板格式的模板分量同样如此）。在 `MSAATargetDesc` 上设置 `KeepDepth: true` 可保存它，供后续通道读取 `DepthTexture()`；`4x` 目标的深度纹理是多重采样纹理。
- **跨设备的采样数**：目标会记住请求的采样数，`Resize` 按当前设备重新裁剪；裁剪结果变化时，即使尺寸不变也会重新分配。通过 `SetDevice` 换到 MSAA 支持不同的设备后，请调用 `Resize`，将 `SampleCount()` 与之前的值比较，并重建用旧采样数创建的管线。

需要后处理抗锯齿时，`FXAAPass` 会编译 `FXAAWGSL`（无顶点缓冲的全屏三角形 FXAA 3.11 Quality 着色器，仅使用 `textureSampleLevel(..., 0.0)`，无需计算着色器，可在完整能力与 `NoGPUCompute` / WebGL 2 / OpenGL ES 3.0 档位下运行），并为每个源纹理各缓存一个绑定组，稳态绘制不分配内存。着色器通过 `textureDimensions` 从源纹理读取纹素尺寸，因此同一个 pass 可以在一帧内处理尺寸不同的多个源：

```go
fxaa, err := render3d.NewFXAAPass(cx, dev, render3d.FXAAPassDesc{
	TargetFormat: dev.SurfaceFormat(),
	Options:      render3d.DefaultFXAAOptions(),
	Retained:     true,
})
// 在单采样渲染通道（如交换链）中调用，或使用 fxaa.Encode(enc, src, dst)：
err = fxaa.Draw(swapchainPass, target.OutputTexture())
```

- `FXAAOptions` 可配置 `SubpixelQuality`（默认 `0.75`，设 `DisableSubpixel: true` 仅平滑边缘）、`EdgeThreshold`（默认 `0.166`）和 `EdgeThresholdMin`（默认 `0.0833`）；越界或非有限值会由 `Sanitize()` 规范化。
- **每次提交一组参数**：参数只占一个 uniform 块，而缓冲写入会在本次提交的命令之前生效，因此同一个 `FXAAPass` 在一帧内记录的所有绘制都使用最后一次绘制的参数。要在一帧内以不同参数处理多个视图，请为每组参数各建一个 `FXAAPass`。
- `ApplyFXAA(src, opts)` 在 CPU 上对 `*image.RGBA` 运行与 GPU 一致的纯 Go FXAA 3.11 参考实现，低于对比度阈值的平坦内部区域保持原样，斜边得到平滑抗锯齿。
- 开启 `Retained` 的 `MSAATarget` 或 `FXAAPass` 在设备丢失恢复后，需在 `OnReady` / `OnDeviceRestored` 中调用 `SetDevice(dev)`，以便后续 `Resize` 或 uniform/绑定组更新指向恢复后的设备。
- **色彩空间**：FXAA 的阈值针对感知上的、经过 gamma 编码的亮度调校，`ApplyFXAA` 也直接处理存储的字节。请给该 pass 输入保存 gamma 编码颜色（例如色调映射后的 LDR 颜色）的非 sRGB 源纹理，并绘制到同类目标上。sRGB 源纹理会以线性值被采样，边缘检测因此与 `ApplyFXAA` 不再一致；gamma 编码的源绘制到 sRGB 目标上则会被编码两次。`TargetFormat` 默认取 `dev.SurfaceFormat()`，在某些平台上是 sRGB 格式，因此请显式设置。
- **参考渲染**：软件渲染器测试将场景与 `render3d/testdata/golden/` 中的 `aa_baseline.png`、`aa_msaa4x.png`、`aa_fxaa.png` 比对（设置 `UPDATE_GOLDEN=1` 可重写参考图），并校验 `FXAAPass` 与 `ApplyFXAA` 的一致性。在完整与 `NoGPUCompute` 两种档位下，测试将 MSAA 输出与软件渲染器的参考渲染比对，并将 `FXAAPass` 输出与 `ApplyFXAA` 比对。

## 3D 调试线框与变换手柄（Gizmo）

`DebugDraw` 将 3D 线框经近裁剪面裁剪后直接投影绘制到 NSGo 的 `*core.Drawing` 叠加层：

```go
var dbg render3d.DebugDraw
dbg.Grid(math3d.Vec3Zero(), 10, 20, core.RGBA(120, 130, 150, 0.4))
dbg.AABB(enemyBounds, core.RGB(255, 80, 80))
dbg.Sphere(alertCenter, 4, core.RGB(255, 200, 60), 24)
dbg.Axes(selectedPos, 1.5)
dbg.Flush(d, cam.ViewProj(), viewportBounds)
dbg.Clear()
```

面向关卡编辑器和场景检查器，`Gizmo` 支持平移、旋转与缩放操作柄（`GizmoTranslate`、`GizmoRotate`、`GizmoScale`）：

```go
var gizmo render3d.Gizmo
axis, _ := gizmo.PickAxis(ray, objPos, gizmoScale, render3d.GizmoTranslate)
if axis != render3d.AxisNone {
	delta := gizmo.DragTranslate(objPos, axis, startRay, currentRay, 0.25) // 0.25m 步进吸附
	objPos = objPos.Add(delta)
}
gizmo.DrawGizmo(d, cam.ViewProj(), viewportBounds, objPos, gizmoScale, render3d.GizmoTranslate, axis)
```

调试线框和操作柄按 `[0, 1]` 深度投影的近平面裁剪，`math3d.Perspective` 和 `math3d.Ortho` 生成的就是这种投影。使用 OpenGL 风格的 `[-1, 1]` 投影时，设置 `DebugDraw.DepthNO` 和 `Gizmo.DepthNO`，或改用 `ProjectSegmentNO`，避免其深度范围的近半段被隐藏。每个平面平移手柄都绘制成 `PickAxis` 实际测试的那个正方形。`DrawGizmo` 不分配内存；需要自己读取线段且不分配时，用 `gizmo.AppendSegments(buf[:0], objPos, gizmoScale, mode)` 复用切片。

## 公告板与条带拖尾

`Billboard` 是转向相机的四边形，`Trail` 是穿过运动物体最近位置的条带，会绕自身中心线扭转以面向观察者。两者都接收 `CameraBasis`，即相机的位置与单位坐标轴，可由矩阵构建，也可从 `scene3d.Camera` 填入：

```go
basis, _ := render3d.NewCameraBasis(cam.ViewMatrix(), cam.ProjMatrix())
// 或：render3d.CameraBasis{Pos: cam.Pos, Right: cam.Right(), Up: cam.Up(), Forward: cam.Forward(), Orthographic: cam.Projection.Kind == scene3d.ProjectionOrthographic}
```

`Billboard.Mode` 决定四边形如何转向：

| 模式 | 朝向 | 适用于 |
| --- | --- | --- |
| `BillboardViewPlane`（默认） | 视平面，沿相机的右轴与上轴 | 粒子、精灵、替身 |
| `BillboardViewPoint` | 相机位置，以相机上轴为滚转参考 | 靠近屏幕边缘的大四边形 |
| `BillboardAxial` | 相机，但只绕 `Axis` 旋转（为零时取 +Y） | 树木、草、火焰、光束 |

视平面与视点两种模式在任何角度都面向相机，包括正上方和正下方俯仰；轴向四边形在轴允许的范围内面向相机，当相机沿轴向观看时呈侧面。`Pivot` 决定四边形相对 `Center` 的位置（`{Y: -0.5}` 使其立在底边上），`Rotation` 使其在屏幕上逆时针滚转。

```go
smoke := render3d.Billboard{Center: pos, Width: 1, Height: 1, Rotation: spin, Color: core.RGBA(200, 200, 200, 0.6)}
tree := render3d.Billboard{Center: base, Width: 2, Height: 4, Pivot: math3d.Vec2{Y: -0.5}, Mode: render3d.BillboardAxial, Texture: treeImage}

quad, ok := smoke.Quad(basis)                                   // 角点依次为左下、右下、右上、左上，朝相机逆时针
item := quad.DrawItem(render3d.DrawItem{ID: 7, MeshID: unitQuad}) // Transform 映射单位四边形；Blend 默认为 BlendAlphaBlend
```

在 GPU 提交落地之前，`BillboardRenderer` 将公告板从后到前填充到 `*core.Drawing`，并按近裁剪面裁剪。带纹理的四边形用仿射变换贴图，在正交相机下精确，对屏幕上较小的四边形也足够接近；跨越近裁剪面的带纹理四边形会被跳过。

```go
var billboards render3d.BillboardRenderer // 跨帧复用
billboards.Draw(d, []render3d.Billboard{smoke, tree}, basis, cam.ViewProj(), viewportBounds)
```

`Trail` 以世界单位对应 `particles.Trail` 的记录点、寿命、宽度与颜色选项，复用 `particles.Curve` 和 `particles.ColorCurve` 从头部（`u = 0`）到尾部（`u = 1`）计算宽度与颜色。它没有 `Texture`，也没有 `Attach`、`Detach` 或 `Follow`：每帧用物体位置调用 `AddPoint`。每个点的边沿 `tangent × toCamera` 展开；朝相机正面直冲的拖尾会沿用上一点的边方向，而不会塌缩：

```go
sword := render3d.NewTrail(render3d.TrailConfig{
	Width:         0.3,
	Lifetime:      250 * time.Millisecond,
	WidthCurve:    particles.Curve{{T: 0, Value: 1}, {T: 1, Value: 0}},
	ColorGradient: particles.ColorCurve{{T: 0, Color: core.RGB(0xffe080)}, {T: 1, Color: core.RGBA(255, 80, 0, 0)}},
})

// 在 Update 中：
sword.AddPoint(bladeTip)
sword.Update(dt)

// 在 Draw 中绘制，或把 Build 返回的三角形带（每点两个顶点，从尾到头）交给 GPU：
verts := sword.Build(basis)
sword.Draw(d, basis, cam.ViewProj(), viewportBounds)
```

`Build` 复用内部存储，不产生分配；`Bounds` 与 `DrawItem` 为透明队列提供与相机无关的包围盒用于排序。

## 渲染统计、分析器与 GPU 预算

`plan.Stats`（`RenderStats`）精确记录裁剪/可见物体数、DrawCall 数、实例化批次数、三角形与顶点数、材质/管线切换次数及显存占用。用 `NewFrameProfiler(120)` 统计滑动窗口帧耗时（`Record`、`AverageFrameTime`、`P95FrameTime`、`FPS`、`PeakDrawCalls`、`PeakTriangles`），并用 `CheckBudget` 在开发构建或 CI 测试中校验预算：

| 预设 | MaxDrawCalls | MaxTriangles | MaxVertices | MaxMaterialSwitches | MaxPipelineSwitches | MaxTextureBytes | MaxBufferBytes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `MobileBudget()` | 150 | 150,000 | 300,000 | 60 | 32 | 128 MiB | 64 MiB |
| `DesktopBudget()` | 2,000 | 2,000,000 | 4,000,000 | 500 | 200 | 1 GiB | 512 MiB |

```go
if violations := render3d.CheckBudget(plan.Stats, render3d.MobileBudget()); len(violations) > 0 {
	for _, v := range violations {
		log.Println("budget exceeded:", v)
	}
}
```

## 着色器与材质热重载

用 [`go tool game dev`](cli.md#dev) 运行游戏，并将 `hotreload.ShaderReloader` 和 `hotreload.MaterialReloader`（位于 `github.com/nsgo-dev/game/packages/game/tooling/hotreload`）挂到 `hotreload.NewDevWatcher` 上，保存修改后的 WGSL（`.wgsl`）、Kage（`.kage`）以及材质 YAML（`.material.yaml`、`.mat.yaml`、`.material.yml`、`.mat.yml`）文件时，改动会在一秒内生效而无需重启：

```go
w := hotreload.NewDevWatcher(cx) // 不在 game dev 下运行或在发布构建中时为 nil
dir := hotreload.DevAssetsDir()

shaders := hotreload.NewShaderReloader(cx, dev,
	hotreload.WithShaderDir(dir),
	hotreload.WithOnShaderError(func(re hotreload.ReloadError) {
		log.Printf("shader error (%s): %v", re.Name, re)
	}),
)
materials := hotreload.NewMaterialReloader(cx, shaders,
	hotreload.WithMaterialDir(dir),
	hotreload.WithOnMaterialError(func(re hotreload.ReloadError) {
		log.Printf("material error (%s): %v", re.Name, re)
	}),
)
w.WatchMaterials(materials) // 同时监视材质 YAML 与所引用的 .wgsl/.kage 着色器

_, _, err := materials.Register("crystal", "materials/crystal.material.yaml", buildPipeline)
```

每次重载都会先编译候选 `*gpu.ShaderModule` 并构建候选 `*gpu.Pipeline`，通过后再原子切换：

- **出错保留旧管线**：如果着色器存在编译错误、缺少入口函数或管线创建失败，或者材质 YAML 语法有误、包含未知字段或非法值，上一次可用的着色器、材质与 `*gpu.Pipeline` 会继续生效而不会崩溃。失败原因记录为 `ReloadError`（可解包为带文件名、行号与列号的 `*loaderr.Error`），可通过 `LastError` 和 `Errors` 查询，后续保存修复后自动清除。
- **着色器到材质的依赖跟踪**：修改被多个材质引用的着色器时，所有依赖它的材质都会自动重建并切换管线，旧管线随后安全关闭。
- **纯文本材质格式（`MaterialDef`）**：带版本号的 YAML 文档（`version: 1`，拒绝未知字段，经由 `ParseMaterial` / `EncodeMaterial` 确定性往返），可声明 `shader`（或拆分的 `vertexShader` / `fragmentShader`）、入口函数（`vertexEntry`、`fragmentEntry`）、GPU 渲染状态（`blend`、`cull`、`frontFace`、`depthWrite`、`depthCompare`，通过 `ApplyToPipelineDesc` 应用）以及带类型的参数（`scalars`、`vec2`、`vec3`、`vec4`、`colors`、`textures`）。若使用共享 2D/PBR `resource.Material` 文件（`type: material`），可继续使用 `w.WatchResources`（见 [resource](resources.md#编辑时重载)）。
- **直接监视着色器**：不使用材质 YAML 的独立管线可调用 `ShaderReloader.RegisterPipeline` 配合 `w.WatchShaders(shaders)`，单文件 `.wgsl` 或 `.kage` 可调用 `w.WatchShader(relPath, onReload)`。
- **发布构建**：在 `-tags release` 下，`NewDevWatcher` 返回 nil，`WatchShader`、`WatchShaders` 与 `WatchMaterials` 编译为空操作，而 `ShaderReloader` 与 `MaterialReloader` 仍正常服务初始加载。

## 动态分辨率缩放（`DynamicResolutionScaler`、`DynamicResolutionTarget`）

`DynamicResolutionScaler` 根据 GPU 帧耗时在 `[MinScale, MaxScale]`（默认 `[0.5, 1.0]`）之间自适应调整 3D 渲染线性分辨率比例以维持 `TargetFrameTime`（默认约 16.67 ms，对应 60 FPS）；`DynamicResolutionTarget` 将 3D 场景渲染到离屏 `gpu.RenderTarget` 的缩放子视口中并上采样到原生输出目标，同时让 2D HUD 叠加层保持完整的原生像素清晰度：

```go
target, err := render3d.NewDynamicResolutionTarget(cx, dev, render3d.DynamicResolutionTargetOptions{
	TimingLabel: "world-3d",
	Config: render3d.DynamicResolutionConfig{
		TargetFrameTime: time.Second / 60,
		MinScale:        0.5,
		MaxScale:        1.0,
		Filter:          render3d.DynamicResolutionBilinear, // 或 DynamicResolutionNearest / DynamicResolutionSharpen
	},
})

// 在 gpu.ViewProps.OnFrame 中（配合挂载在 gpu.View 上的 scene.Overlay3D 绘制原生分辨率 HUD）：
w, h := frame.Size()
pass3d, err := target.Begin3D(frame, w, h, core.RGB(0x10141c))
pass3d.SetPipeline(scenePipe)
pass3d.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
mesh.Draw(pass3d)
pass3d.End()

_ = target.Upscale(frame, nil, w, h) // 将缩放后的 3D 子视口上采样到原生表面
frame.Submit()
target.RecordPassTimings(frame.PassTimings()) // 或 target.RecordFrameTime(gpuDuration)
```

- **带滞回的比例填充率控制器**：片元开销与 `scale²` 成正比，因此 `DynamicResolutionScaler.RecordFrameTime` 用指数移动平均（`SmoothingAlpha`）平滑 GPU 耗时，丢弃超过 `HitchLimit` 的加载/切后台卡顿尖峰，在 `±Deadband` 死区内保持稳定，过载时快速下调（`CooldownDown`、`StepDown`），连续富余时平滑回升（`CooldownUp`、`StepUp`）。可通过 `SetFixedScale`、`ClearFixedScale`、`SetEnabled` 与 `SetScaleBounds`（例如将 `MaxScale` 上限绑定到 `quality.Preset.ResolutionScale`）覆盖或约束缩放范围。
- **零重分配子视口缩放**：`DynamicResolutionTarget` 按原生尺寸分配离屏颜色+深度 `gpu.RenderTarget`，每次 `Begin3D` 仅将 `SetViewport` 与 `SetScissor` 设为 `[0, 0, scaledW, scaledH]`，缩放比例逐帧变化时不会反复重建 GPU 纹理；`Upscale` 将采样 UV 钳制在有效子视口向内半纹素范围内，防止未使用区域的边缘纹素渗色。
- **原生分辨率 HUD**：在 `gpu.View` 上挂载 [`scene.Overlay3D`](scene.md#3d-视图上的-2d-图层overlay3d) 以在 1:1 原生 `core.Canvas` 上绘制 2D HUD，或在 `Upscale` 后追加一个 `gpu.LoadLoad` 的原生 GPU HUD 通道，或在单个原生颜色通道内调用 `UpscaleIntoPass` 后紧接着绘制原生 GPU HUD 元素。
- **上采样滤波模式**：可在运行时通过 `SetFilter` 与 `SetSharpness` 在 `DynamicResolutionBilinear`、`DynamicResolutionNearest` 和 `DynamicResolutionSharpen`（受局部邻域最值钳制的 5 点对比度自适应锐化，避免振铃光晕）之间零重分配切换。
