# render3d

`game/render3d` 提供 NSGo GPU 视图的网格缓冲、纹理与采样器、金属度-粗糙度 PBR 材质、方向光、聚光灯与点光源阴影贴图、相机 uniform、离屏渲染到纹理相机与多视口分屏渲染、距离雾与高度雾，以及 HDR 渲染目标与色调映射 pass，以及与后端无关、稳态零分配的 CPU 侧 3D 渲染管线与开发工具：视锥体裁剪、不透明/半透明绘制排序、自动实例化合批、带滞回防抖动的 LOD 选择、面向相机的公告板与 3D 条带拖尾、3D 调试线框与变换手柄（Gizmo）、帧性能分析器，以及移动端/桌面端 GPU 预算检查。

```go
import "github.com/nsgo-dev/game/pkg/render3d"
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

## PBR 着色中的方向光、点光源与聚光灯（`LightRig`、`GPULightBuffer`）

`LightRig` 保存场景的线性 RGB 光照配置：一个 `AmbientLight`、最多 `MaxDirectionalLights`（4）个 `DirectionalLight`、最多 `MaxPointLights`（16）个 `PointLight`，以及最多 `MaxSpotLights`（8）个 `SpotLight`。它们在同一个 pass 里一起着色，因此一个场景可以同时有太阳、灯和手电筒。

- **`DirectionalLight`**：沿 `Direction` 传播的平行光（例如 `{Y: -1}` 表示竖直向下照向 `+Y` 表面；着色或打包时会自动归一化），包含线性 `Color` 与 `Intensity`。
- **`PointLight`**：位于 `Position` 的全向点光源，包含线性 `Color`、`Intensity` 和可选截止距离 `Range`。当 `Range > 0` 时，距离衰减采用 glTF `KHR_lights_punctual` 平滑窗函数 `max(min(1 - (d/Range)^4, 1), 0)^2 / max(d^2, 1e-4)`，在 `Range` 处平滑归零；`Range == 0` 时采用带下界保护的纯平方反比衰减 `1 / max(d^2, 1e-4)`（`PointLightAttenuation`）。
- **`SpotLight`**：位于 `Position`、沿 `Direction`（着色或打包时自动归一化）照射的 glTF `KHR_lights_punctual` 锥形光源，线性 `Color`、`Intensity` 与 `Range` 距离衰减和 `PointLight` 完全相同，再乘以半角 `InnerConeAngle` 与 `OuterConeAngle`（弧度，`0 <= inner < outer <= pi/2`）之间的角度衰减：内锥内为全强度，两锥之间为 `clamp(cos(angle) * angleScale + angleOffset, 0, 1)^2`，外锥外或光源背后为零（`SpotConeAttenuation`，`angleScale` 与 `angleOffset` 由 `SpotLight.ConeScaleOffset` 给出，`SpotLight.Attenuation` 返回距离与锥形的合并衰减）。`Validate` 拒绝为零的外锥角、超过 `pi/2` 的外锥角，以及不小于外锥角的内锥角。
- **`AmbientLight`**：各向同性的均匀线性环境填充光，按 `Ambient.Color * Ambient.Intensity * BaseColor` 叠加。

`GPULightBuffer` 将校验后的 `LightRig` 上传到 1056 字节（`LightUniformSize`）、16 字节对齐的 std140/WGSL uniform 缓冲区，并绑定到 `LightGroup`（第 1 组），归属于传入的 `*core.Scope`，支持通过 `LightBufferOptions.Retained` 在 GPU 设备丢失后自动恢复：

```go
rig := render3d.LightRig{
	Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.16, Y: 0.22, Z: 0.32}, Intensity: 0.22},
	Directional: []render3d.DirectionalLight{
		{Direction: math3d.Vec3{X: 0.55, Y: -0.8, Z: -0.45}, Color: math3d.Vec3{X: 1, Y: 0.96, Z: 0.9}, Intensity: 2.6},
	},
	Point: []render3d.PointLight{
		{Position: math3d.Vec3{X: 1.6, Y: 1.15, Z: 1.5}, Color: math3d.Vec3{X: 0.25, Y: 0.72, Z: 1}, Intensity: 9, Range: 6.5},
	},
	Spot: []render3d.SpotLight{
		{
			Position: math3d.Vec3{X: -1.8, Y: 3.6, Z: 1.2}, Direction: math3d.Vec3{X: 0.7, Y: -3.6, Z: -1.1},
			Color: math3d.Vec3{X: 1, Y: 0.72, Z: 0.38}, Intensity: 28, Range: 9,
			InnerConeAngle: math3d.DegToRad(14), OuterConeAngle: math3d.DegToRad(26),
		},
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

自定义 3D 着色器只需在开头拼接 `CameraWGSL + LightWGSL`，即可访问 `@group(1) @binding(0) var<uniform> lights: LightRigUniform` 并调用可移植 WGSL 函数 `point_light_attenuation`、`spot_cone_attenuation`、`pbr_brdf`、`shade_pbr` 与 `linear_to_srgb`。`LitPBRWGSL` 将它们与 `MeshVertexLayout` 及位于 `PBRDrawGroup`（第 2 组）的逐绘制 `PBRDrawUniform` 组合为完整管线。

如果局部光源多于 rig 的容量，请在 `LightGroup` 上绑定 `ClusteredLightSystem` 来代替 `GPULightBuffer`：它上传同一个 rig，并加入任意数量的集群点光源和聚光灯，使用同一个 BRDF 着色（[集群前向光照](#集群前向光照clusteredlightsystemclustergrid)）。

着色模型遵循 glTF 2.0 金属度-粗糙度 PBR（电介质 `F0 = 0.04`、Schlick 菲涅尔、Trowbridge-Reitz GGX 法线分布、高度相关 Smith-GGX 可见度项、能量守恒 Lambert 漫反射、感知粗糙度下限 `0.045`，以及 IEC 61966-2-1 线性到 sRGB 输出）。`EvaluatePBR`、`ShadePoint`、`PointLightAttenuation`、`SpotConeAttenuation` 与 `LinearToSRGB` 在 CPU 上实现完全一致的公式；`LightRig.AppendUniformData` 与 `LightUniforms.Encode` 零分配打包 uniform 块。测试逐像素比对 GPU 渲染结果与 CPU `ShadePoint`，覆盖完整能力与 WebGL 2 / GLES 3.0（`nsgotest.NoGPUCompute`）配置及 `Retained` 设备丢失恢复，并将光照场景与 `render3d/testdata/golden/lights_pbr_scene.png`、`lights_point_falloff.png` 和 `spot_lights_scene.png` 参考图比对。

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

- **深度与平移无关**：`NewSkyboxUniform` 去除相机平移分量，使天空盒随观察者移动始终处于无限远。`SkyboxWGSL` 使用 `@builtin(vertex_index)` 绘制单个全屏三角形（不需要顶点缓冲），深度为 `1.0`，即远平面，配合 `CompareLessEqual` 与 `DepthWrite: false`：在不透明几何之后绘制时只着色未被覆盖的像素，不会覆盖比远平面更近的任何几何。
- **带环境光照的受光 PBR**：`LitPBRIBLWGSL` 是在 `LitPBRWGSL` 基础上加入 `evaluate_ibl`：在编码为 sRGB 之前，把球谐漫反射辐照度与预过滤镜面反射加到直接光照上。环境提供环境光项时，请把 `LightRig` 的环境光设为零。IBL 与雾都使用第 3 组（`IBLGroup` 与 `FogGroup`），这是所有后端都保证的四个绑定组中的最后一个，因此一条受光管线只能二选一。
- **自定义着色器**：在着色器前拼接 `IBLWGSL`（第 3 组）或 `IBLWGSLForGroup(group)`，并调用 `evaluate_ibl(normal, view_dir, base_color, metallic, roughness, ao)`。漫反射项计算 9 个预卷积 L2 球谐系数（`evaluate_irradiance_sh`），镜面项按 `roughness * ibl.max_lod` 采样预过滤立方体贴图并乘以 Karis 解析拟合 `env_brdf_approx`（`EnvBRDFApprox`）。需要 2D 查找表的着色器可以用 `GenerateBRDFLUT` 或 `GenerateBRDFLUTWithSamples` 烘焙分离求和 GGX 表。
- **缓冲、校验与内存**：`GPUIBLBuffer` 与 `GPULightBuffer`、`GPUFogBuffer` 一致：`Update(dev, u)` 写入新参数，`Bind` 绑定到 `IBLGroup`，`ByteSize` 报告其 uniform 缓冲供 `RenderStats` 统计，各立方体贴图则由 `GPUCubemap.ByteSize` 报告。`IBLUniform.Validate` 拒绝非有限的球谐系数，以及非有限或为负的强度与最大 LOD，返回 `ErrInvalidIBL`。
- **高动态范围环境与 `.hdr` 加载（`HDRCubemap`、`UploadCubemapHDR`）**：`Cubemap` 每通道 8 位（`RGBA8Unorm` 或 `RGBA8UnormSRGB`），会将大于 1 的辐射度截断。若需保留大于 1.0 的物理高光，请使用存储线性 `float32` RGB 六面与 mip 的 `HDRCubemap`：可用 `CubemapFromHDR`（或 `DecodeHDR` / `EncodeHDR` 配合 `CubemapFromHDREquirectangular`）直接加载 Radiance `.hdr`（RGBE）等距柱状全景图，用 `GradientSkyHDRCubemap` 生成程序化 HDR 天空，或用 `HDRCubemapFromCubemap` 提升 LDR 立方体贴图。随后用 `ComputeIrradianceSHHDR` 与 `PrefilterEnvironmentHDR` 烘焙 HDR 漫反射辐照度与预过滤镜面贴图，并通过 `UploadCubemapHDR` 上传为 `gpu.FormatRGBA16Float` 的 `GPUCubemap`（超过 `65504` 的值截断以保持半精度有限值；仅在设备不支持 `FormatRGBA16Float` 时回退到 `RGBA8Unorm`）。将 HDR 场景与天空盒渲染到 `HDRTarget` 后交给 `ToneMapPass`（例如 `ToneMapNeutral`）解析；若需直接把 HDR 天空盒画到 LDR 目标，可启用 `SkyboxPipelineDesc.ToneMap`（`SkyboxToneMapWGSL`）执行 Reinhard 与 gamma 2.2 编码。
- **带 IBL 的纹理 glTF PBR 材质（`NewMaterialIBLPipeline`、`MaterialIBLWGSL`）**：`NewMaterialIBLPipeline` 将 `CameraGroup`（第 0 组）、位于 `MaterialGroup`（第 1 组）的纹理 glTF `GPUMaterial`（基础色、金属度-粗糙度、切线空间法线、遮蔽与自发光五张贴图，以及透明裁剪/混合和双面法线翻转）与位于 `MaterialIBLGroup`（第 2 组）的 `GPUIBLBuffer` 组合在一起。设置 `MaterialIBLPipelineOptions.LinearOutput`（`MaterialIBLLinearWGSL`）可将未截断的线性 HDR 辐射度输出到 `HDRTarget` 再交给 `ToneMapPass`；`EvaluateMaterialIBL` 提供与之匹配的纯 Go CPU 参考实现。
- **CPU 参考实现与设备丢失恢复**：`EvaluateIBL` 与 `EvaluateIBLHDR` 在 CPU 上匹配 `evaluate_ibl`，`ShadePoint` 加 `EvaluateIBL` 匹配 `LitPBRIBLWGSL`，`EvaluateMaterialIBL` 匹配 `MaterialIBLWGSL`。在 `CubemapUpload`、`SkyboxPipelineDesc`、`IBLBufferOptions` 与 `MaterialIBLPipelineOptions` 上设置 `Retained: true` 后，设备丢失时 NSGo 会恢复纹理、采样器、管线与 uniform 缓冲；请在 `OnReady` 中调用 `SkyboxPipeline.SetDevice(dev)`，并把新设备传给 `GPUIBLBuffer.Update`。
- **参考渲染测试**：无头软件 GPU 测试将网格后方的天空盒以及 2×2 金属度/粗糙度球体与 `render3d/testdata/golden/skybox_scene.png` 和 `ibl_spheres.png` 对比，并在 `.hdr` 解码的 `FormatRGBA16Float` HDR 环境与天空盒下用全部五张 glTF PBR 贴图渲染 Khronos `DamagedHelmet`（经 `ToneMapNeutral` 解析，参考图位于 `render3d/testdata/damagedhelmet/reference_ibl.png`，缺少 CC-BY-NC-4.0 素材目录时自动跳过；设置 `UPDATE_GOLDEN=1` 可重写），验证 HDR 镜面高光明显亮于 LDR 截断环境、CPU 参考一致性、远平面内侧几何可见性、保留资源设备丢失恢复，以及完整能力与 `nsgotest.NoGPUCompute` 两种配置。

## 反射探针与光照探针（`ReflectionProbeSet`、`LightProbeVolume`、`GPUProbeIBLBuffer`）

`render3d` 在现有的 `Cubemap`、`GPUCubemap`、`PrefilterEnvironment` 与 `IrradianceSH` 基础上扩展了随空间变化的局部反射与插值漫反射间接光照，并通过 `IBLGroup`（第 3 组）接入 `LitPBRWGSL`，使金属度、粗糙度、自发光、`LightRig` 直接光、逆转置法线矩阵与 sRGB 输出在一次绘制中协同工作，兼容包括 WebGL 2 与 OpenGL ES 3.0（`NoGPUCompute`）在内的所有后端。

```go
var probes render3d.ReflectionProbeSet
room := &render3d.ReflectionProbe{
	ID:            "hall",
	Position:      math3d.Vec3{X: 0, Y: 1.5, Z: 0},
	BoxMin:        math3d.Vec3{X: -6, Y: 0, Z: -6},
	BoxMax:        math3d.Vec3{X: 6, Y: 4, Z: 6},
	BlendDistance: 1.5,
	BoxProjection: true,
}
probes.Add(room)

// 加载时或按需烘焙（仅运行时存在；重烘焙会自动关闭旧的 GPUCubemap）：
render3d.BakeReflectionProbeGPU(cx, dev, room, render3d.ProbeBakeOptions{
	Size:       64,
	ClearColor: core.RGB(0x101824),
	SRGB:       false, // RGBA8Unorm 目标；设为 true 则使用 RGBA8UnormSRGB
	Prefilter:  render3d.PrefilterOptions{BaseSize: 64, MipLevels: 5, SampleCount: 64},
}, func(pass *gpu.RenderPass, face gpu.CubeFace, faceCam render3d.CameraUniform) {
	camBuf.Write(faceCam)
	drawStaticRoom(pass)
}, func(err error) {
	// 全部六个面的回读完成后在引擎 goroutine 上异步调用。
})

// 为动态物体烘焙三维 IrradianceSH 光照探针网格：
grid, _ := render3d.NewLightProbeGrid(roomBounds, 4, 2, 4)
_ = grid.Bake(16, sampleRoomRadiance)
lightProbes := &render3d.LightProbeVolume{Grid: grid, FallbackSH: skySH}

// 在 IBLGroup（第 3 组）绑定逐物体探针 IBL 缓冲并接入 LitPBRWGSL：
probeIBL, _ := render3d.NewGPUProbeIBLBuffer(cx, dev, render3d.ProbeIBLBufferOptions{Label: "hero-ibl"})
pipe, _ := render3d.NewLitPBRProbeIBLPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), probeIBL.Layout(), gpu.FormatDepth24Plus)

// 每帧随移动物体更新：
_ = probeIBL.UpdateForPoint(dev, heroPos, &probes, lightProbes, skyPrefCube, 1.0)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
probeIBL.Bind(pass)
heroMesh.Draw(pass)
```

- **局部反射选择、视差盒投影与平滑混合**：`ReflectionProbeSet.SelectReflectionProbes` 按 `Priority`、基于 `BlendDistance` 的平滑步进权重 `Influence` 与包围盒体积，选取最多两个覆盖当前世界坐标的就绪探针（已启用、已烘焙且非 `Dirty`），返回总和为 1 的归一化权重 `(Weight0, Weight1, SkyWeight)`。开启 `BoxProjection: true` 的探针通过 `BoxProjectDirection`（着色器中为 `box_project_dir`）将反射方向投影到 `[BoxMin, BoxMax]`。未烘焙、标记为 `Dirty` 或禁用的探针会在选择时跳过；缺失探针槽位或 `nil` 天空会绑定 1×1 黑色 `(0, 0, 0, 255)` 回退立方体贴图，保证 CPU `EvaluateProbeIBL` 与 GPU `LitPBRProbeIBLWGSL` 结果严格一致。
- **光照探针（`LightProbe`、`LightProbeGrid`、`LightProbeVolume`）**：存储 `IrradianceSH` 球谐系数（由 `ComputeIrradianceSH` 或 `ComputeIrradianceSHFromFunc` 烘焙，并支持 `Add`、`Scale`、`Lerp`）。`LightProbeGrid.Sample` 做三线性插值，`LightProbeVolume.SampleLightProbes` 将散点 `LightProbe`（可选有限 `Radius`）平滑混合到网格或 `FallbackSH` 上；两者都会校验 `Counts` 与 `len(Probes)` 及非有限坐标，不会 panic。
- **异步 GPU 烘焙与渲染目标格式**：`BakeReflectionProbeGPU` 使用 `ProbeFaceCamera` 将六个面渲染到临时的 `FormatRGBA8Unorm`（`SRGB: false`）或 `FormatRGBA8UnormSRGB`（`SRGB: true`）配 `FormatDepth24Plus` 离屏渲染目标中，通过 `gpu.Device.ReadTexture` 异步回读六个面，翻转行序至标准立方体贴图方向，使用 `PrefilterEnvironment` 预过滤粗糙度 mip 链，经 `UploadCubemap` 上传，关闭旧的 `probe.GPUCubemap`，并在引擎 goroutine 上回调 `onDone`。`BakeReflectionProbe` 提供对应的 CPU 采样烘焙路径。
- **仅运行时数据**：反射探针立方体贴图与光照探针球谐烘焙均属于运行时缓存，在加载时或按需生成，不会持久化到文本场景文件中。
- **自定义着色器与 HDR**：在着色器前拼接 `ProbeIBLWGSL`（`IBLGroup`）或 `ProbeIBLWGSLForGroup(group)`，并调用 `evaluate_probe_ibl(world_pos, normal, view_dir, base_color, metallic, roughness, ao)`。多个逐物体 `GPUProbeIBLBuffer` 可通过 `ProbeIBLBufferOptions.Layout` 共享同一个 `ProbeIBLBindGroupLayout`，搭配 `HDRTarget` 时可使用 `NewLitPBRProbeIBLPipelineWithOptions` 并设置 `LinearOutput: true`。`ShadePoint` 加 `EvaluateProbeIBL`（再经 `LinearToSRGB`）与 `LitPBRProbeIBLWGSL` 在 CPU 上逐像素对应。

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
- **KTX2 纹理**：`LoadKTX2` 上传 `game assets` 和 `game tex` 写出的那种 2D KTX2 文件，包括它含有的每一级 mip，保持其 sRGB 或线性格式，并返回绘制它所用的采样器：即 `KTX2Upload.Sampler`，其过滤方式按文件的 `game.filter` 键（`KTX2FilterKey`）设置，这个键由图片[导入设置附属文件](cli.md#资源旁的导入设置)的 `filter` 写入，因此 `nearest` 让像素画清晰锐利，`linear` 则做混合。BasisLZ、Zstandard 和 ZLIB 超压缩的层级会先解压。BC1、BC3、BC7、ETC2 RGB8 与 RGBA8 以及 ASTC 4x4 层级（包括 `astcenc` 写出的多分区、双平面与 BISE 编码块）在 `Device.SupportsCompressedFormat` 允许时以压缩格式上传，否则在 CPU 上解码为 RGBA8，因此在所有后端都能加载，但有三条限制，防止小文件耗尽内存：各层级解压后合计超过 256 MiB 的 Zstandard 或 ZLIB 文件（例如完整 mip 链的 16384x16384 BC7）不会加载；CPU 不会解码大于 8192x8192 的层级（其 RGBA 图像会超过 256 MiB），也不会把纹理转码成合计超过 256 MiB 的层级（例如完整 mip 链的 8192x8192 RGBA8）；因此超过这些尺寸的纹理只能在支持其自身块格式的设备上加载，而总要转码的 UASTC 或 BasisLZ/ETC1S 纹理则不会加载；`KTX2Sampler` 无需设备即可把过滤方式应用到采样器选项上。不是这类纹理的文件或未知的过滤方式会返回包装 `ErrInvalidTexture` 的错误：

  ```go
  data, err := fs.ReadFile(assets, "albedo.ktx2")
  albedo, opts, err := render3d.LoadKTX2(cx, dev, data, render3d.KTX2Upload{Label: "albedo", Retained: true, Sampler: render3d.SamplerLinearRepeat()})
  samp, err := render3d.NewGPUSampler(cx, dev, opts)
  ```

- **Basis Universal（UASTC 与 BasisLZ/ETC1S）**：`LoadKTX2` 同时接受 UASTC 4x4（高质量模式）和 BasisLZ/ETC1S（`basisu` 的默认模式，含 alpha 切片）的 KTX2 文件，并把它们转码为设备支持的最佳块格式：依次为 BC7、不透明纹理用的 BC1、BC3、ETC2 RGB8 或 RGBA8（仅线性，因为 NSGo 没有 sRGB ETC2），然后是 ASTC 4x4。设备一种都不支持、基础层级的宽或高不是 4 的倍数，或设置了 `KTX2Upload.ForceRGBA8` 时，解码为 RGBA8。转码会在调用 `LoadKTX2` 的 goroutine 上把每个块解码后再编码：在桌面 CPU（Ryzen 9 3900X）上，一个 1024×1024 层级转为 BC7 约需 0.5 秒，转为 ETC2 约 0.8 秒，转为 ASTC 约 0.16 秒，2048×2048 约为四倍（见 `ktx2` 的 `BenchmarkTranscodeUASTC`），因此大的通用压缩纹理应在加载画面中加载。不带 alpha 的 BC1（`VK_FORMAT_BC1_RGB_*`）仍会被拒绝，因为以 RGBA BC1 上传会把它的穿透纹素画成透明。

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
| `NewLitPBRPipelineWithOptions` 加 `LitPBRPipelineOptions.SkinLayout`（场景光源） | `LitPBRSkinnedWGSL` | `LitPBRSkinGroup`（3） | `FogLayout`、`IBLLayout`、`ShadowLayout`、`SpotShadowLayout`、`PointShadowLayout` |
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

每个 `gpu.Device` 创建一份共享的 `MaterialDefaults`，持有 `MaterialGroup`（第 1 组）的绑定组布局（以及无光照材质的 `UnlitLayout`，见下文“无光照材质与顶点色”）、默认线性重复采样器以及四张 1×1 回退纹理（缺失基础色用的 sRGB 白色、缺失金属度-粗糙度与遮蔽用的线性白色、缺失法线贴图用的平坦法线 `(128, 128, 255, 255)` 以及缺失自发光用的黑色）。所有 `GPUMaterial` 在省略可选贴图时都会绑定这些回退纹理，因此全部材质共用同一个绑定组布局，无需管线排列组合：

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
- **金属度-粗糙度与光照 PBR 管线的顶点色（`COLOR_0`）**：设置 `MaterialPipelineOptions.VertexColors: true`（编译 `MaterialColoredWGSL` 或其雾/线性输出变体）或 `LitPBRPipelineOptions.VertexColors: true`（编译 `LitPBRColoredWGSL` 或其雾、IBL、阴影、聚光/点光阴影、CSM 与线性输出变体），即可用 `GPUColoredMesh`（`ColoredVertexLayout`，`@location(3)` 线性 RGBA）绘制带 `COLOR_0` 的受光网格。着色器按 glTF 2.0 §3.7.2.1 在 `AlphaMask` 裁切与 PBR 着色之前将 `base_color`（RGB 与 alpha）乘以线性顶点色。`VertexColors` 不可与 `SkinLayout` 同时启用（`SkinnedVertexLayout` 不含顶点色属性）。
- **CPU 参考求值与黄金图验证**：`EvaluateMaterial`（配合 `MaterialEvalInput.VertexColor` 与 `HasVertexColor`，未设置时默认为 `[1, 1, 1, 1]`）以纯 Go 实现与着色器一致的 Cook-Torrance GGX BRDF、半球环境光、法线扰动、alpha 裁切与 Reinhard + sRGB 色调映射；`PBRMaterial.WithVertexColor` 可将线性顶点色折算进 `PBRMaterial`，供 `ShadePoint`、`EvaluatePBR` 与 `EvaluateIBL` 复用。`render3d` 的测试将 GPU 像素回读与 `EvaluateMaterial` / `ShadePoint` 逐像素校验，端到端渲染 Khronos `VertexColorTest` 样本，比对黄金参考图（`pbr_metallic_roughness_ramp.png` 与 `pbr_textured_normal_maps.png`），并在 `gpu.View` 上同时验证完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置；设置 `UPDATE_GOLDEN=1` 可重写参考图。

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
- **Khronos 参考验证**：`render3d` 的测试渲染 Khronos `DamagedHelmet` 样本，并以资产自身的基础色图像为准进行校验：用探测纹理读回每个像素的 UV，渲染结果必须在 glTF 的 UV 原点、sRGB 色彩与通道顺序下与解码后的 JPEG 吻合，且明显优于 V 翻转、按线性上传或通道交换的情形。测试还会与 `render3d/testdata/damagedhelmet/reference.png` 比对以捕捉回归，并在 `gpu.View` 上验证完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置的像素一致性，以及 `Retained` 设备丢失恢复。该模型采用 CC-BY-NC-4.0 许可，因此其目录是独立模块，不包含在发布的 `github.com/nsgo-dev/game` 模块中；目录缺失时这些测试会跳过。

### 扩展 PBR 材质：自发光强度、清漆层与透射（`GPUExtendedMaterial`、`TransmissionBackground`）

`Material` 同时提供 `KHR_materials_clearcoat`（`ClearcoatFactor`、`ClearcoatRoughnessFactor`、`ClearcoatTexture`、`ClearcoatSampler`、`ClearcoatRoughnessTexture`、`ClearcoatRoughnessSampler`、`ClearcoatNormalTexture`、`ClearcoatNormalSampler`、`ClearcoatNormalScale`）、`KHR_materials_transmission`（`TransmissionFactor`、`TransmissionTexture`、`TransmissionSampler`）以及电介质折射率（`IOR`，默认 `DefaultMaterialIOR = 1.5`）字段；`KHR_materials_emissive_strength` 则直接折算进 HDR `EmissiveFactor`（`> 1.0`），配合 `LinearOutput: true`、`HDRTarget`、`BloomPass` 与 `ToneMapPass` 即可呈现真实的高动态泛光。

普通金属度-粗糙度材质的 `GPUMaterial` 与 `MaterialDefaults` 保持精简的 11 项绑定布局不变。对于启用清漆层、透射或自定义折射率的表面，使用 `ExtendedMaterialDefaults`、`GPUExtendedMaterial`（或 `UploadGLTFExtendedMaterials`）与 `NewExtendedMaterialPipeline`：

```go
extDefs, err := render3d.NewExtendedMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Retained: true})
transBG, err := render3d.NewTransmissionBackground(cx, dev, render3d.TransmissionBackgroundOptions{
	Retained: true,
	SRGB:     true, // 采样 sRGB 编码的 RGBA8Unorm 不透明通道时设为 true；HDRTarget 设为 false
})
pipe, err := render3d.NewExtendedMaterialPipeline(cx, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
	DepthFormat:        gpu.FormatDepth24Plus,
	LightLayout:        lightBuf.Layout(), // 可选：在 ExtendedLightGroup (2) 绑定 LightRig
	TransmissionLayout: transBG.Layout(),  // 可选：在 TransmissionBackgroundGroup (3) 绑定逐帧背景
})

// 上传 glTF 扩展材质（基础 PBR + 清漆层 + 透射 + 自发光强度 + IOR）：
matSet, err := render3d.UploadGLTFExtendedMaterials(cx, dev, extDefs, doc, render3d.GLTFMaterialUpload{
	Label:    "Sunglasses",
	Retained: true,
})

// 每帧先将不透明场景渲染到 opaqueRT，再更新唯一一份逐帧 TransmissionBackground，随后绘制透射物体：
_ = transBG.SetTexture(dev, opaqueRT.Color, true) // 动态分辨率下调用 transBG.SetDynamicResolution(dev, dynTarget, nil, 0, 0, true)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
pass.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
transBG.Bind(pass) // 绑定到 @group(3) TransmissionBackgroundGroup（无 LightLayout 时用 BindAt(..., MaterialTransmissionBackgroundGroup)）
matSet.ExtendedMaterial(prim.Material).Bind(pass) // 绑定到 @group(1) MaterialGroup
mesh.Draw(pass)
```

- **按 UV 对齐的清漆层法线贴图**：`ExtendedMaterialBindingsWGSL` 对 `ClearcoatNormalTexture` 复用与基础法线相同的克莱姆法则 `dpdx`/`dpdy` 切线空间标架（带确定性正交基回退），且独立作用于几何法线、不受基础 `NormalTexture` 干扰，支持正负 `ClearcoatNormalScale`。
- **逐帧透射背景与动态分辨率适配**：`TransmissionBackground` 不把不透明场景纹理烘焙进单个材质的绑定组，因此在窗口缩放或 `DynamicResolutionTarget` 动态缩放时只需更新一次通道级绑定组。着色器通过 `frag_pos.xy / vec2<f32>(textureDimensions(transmission_bg_tex))` 计算屏幕 UV，并由 `SetViewport` / `SetDynamicResolution` 进行比例缩放与半纹素钳位，避免边缘填充纹素渗色。
- **CPU 一致性与 Khronos 黄金图验证**：`EvaluateMaterial`、`EvaluatePBR`、`ShadePoint` 与 `ShadePointWithBackground` 均在 CPU 侧支持清漆层、透射与 IOR 求值以与 GPU 着色器对齐。测试验证了 GPU/CPU 像素一致性、0.5×–1.0× 动态分辨率缩放、`gpu.View` 在完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）下的渲染、`Retained` 设备丢失恢复，以及 Khronos `EmissiveStrengthTest`（`khronos_emissive_strength.png`）、`TextureTransformMultiTest` 清漆面板（`khronos_clearcoat.png`）与 `SunglassesKhronos`（`khronos_transmission.png`）的黄金参考图。

## 无光照材质与顶点色（`UnlitMaterial`、`ColoredMesh`）

`ColoredMesh` 与 `GPUColoredMesh` 在 `MeshVertexLayout`（location 0–2）基础上于 `@location(3)` 扩展了线性 RGBA 顶点色属性（`ColoredVertexLayout`，步长 48 字节），用于 glTF `COLOR_0`。当 `Colors` 为空时，`AppendVertexData` 为每个顶点写入线性不透明白色 `(1, 1, 1, 1)`，使带顶点色与不带顶点色的网格共用同一管线。`ColoredMeshFromGLTF` 从 `gltf.Document` 提取三角形图元，包括 `COLOR_0`（将 VEC3 RGB 的 alpha 补为 `1.0`）以及 `baseColorTexture` 上的 `KHR_texture_transform` UV 变换。

`UnlitMaterial`、`GPUUnlitMaterial`、`UnlitWGSL` 与 `NewUnlitPipeline` 实现不受场景光照影响的 glTF `KHR_materials_unlit` 表面（在线性空间计算 `baseColorFactor * baseColorTexture * vertexColor`，再应用透明模式与 IEC 61966-2-1 线性到 sRGB 编码）。与金属度-粗糙度材质重叠的部分直接复用同一套类型：`AlphaMode`、`MaterialGroup`（group 1）、`MaterialUpload`、`GLTFMaterialUpload`、`SamplerOptionsFromGLTF`，以及每个设备一份的 `MaterialDefaults`，无光照材质的绑定组布局也由它持有（`MaterialDefaults.UnlitLayout`）：

```go
defaults, err := render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Retained: true})

cm, err := render3d.ColoredMeshFromGLTF(doc, 0, 0)
gpuMesh, err := render3d.UploadColoredMesh(cx, dev, cm, render3d.MeshUpload{Label: "sign", Retained: true})

gpuMat, err := render3d.UploadGLTFUnlitMaterial(cx, dev, defaults, doc, 0, render3d.GLTFMaterialUpload{
	Label:    "sign-unlit",
	Retained: true,
})
pipe, err := render3d.NewUnlitPipeline(cx, dev, camBuf.Layout(), defaults.UnlitLayout(), render3d.UnlitPipelineOptions{
	AlphaMode:   gpuMat.Material().AlphaMode,
	DoubleSided: gpuMat.Material().DoubleSided,
	DepthFormat: gpu.FormatDepth24Plus,
	Retained:    true,
})

// 在 OnFrame 中：
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
gpuMat.Bind(pass) // 绑定到 @group(1)（MaterialGroup）
gpuMesh.Draw(pass)
```

- **默认回退纹理与采样器**：当 `UnlitMaterial.BaseColorTexture` 或 `BaseColorSampler` 为 nil 时，`NewGPUUnlitMaterial` 绑定共享的 `MaterialDefaults.WhiteSRGB()` 纹理与 `MaterialDefaults.Sampler()`，因此没有纹理的无光照材质除 32 字节 uniform 缓冲外不再分配任何资源。
- **透明模式与背面剔除**：`AlphaOpaque` 强制输出 alpha 为 `1.0` 并开启深度写入；`AlphaMask` 丢弃合成 alpha `< AlphaCutoff`（默认 `0.5`）的片元，存活片元输出 alpha `1.0`；`AlphaBlend` 启用标准透明混合（`gpu.BlendAlpha`）并关闭深度写入。`DoubleSided` 将剔除模式从 `gpu.CullBack` 切换为 `gpu.CullNone`。`BaseColorFactor` 与 `Material` 一样必须位于 `[0, 1]`。
- **目标色彩空间与 CPU 参考一致**：`NewUnlitPipeline` 对非 sRGB 渲染目标（如 `FormatRGBA8Unorm`）选择 `fs_main`（在着色器内完成线性到 sRGB 编码）；对 sRGB 目标（`ColorFormat.IsSRGB()`，由硬件在写入时编码）或为 `HDRTarget` 设置了 `LinearOutput` 时选择 `fs_linear`（输出线性值）。`NewMaterialPipeline` 与带光照的 PBR 管线总是在着色器内编码，不会根据 sRGB 格式切换，因此无光照与 PBR 绘制共用同一渲染通道时，请使用非 sRGB 目标。`EvaluateUnlit` 提供对应的纯 Go CPU 参考实现（`Linear`、`SRGB`、`Discarded`、`RGBA8()`、`BlendOver()`）。
- **无光照与 PBR 管线均支持顶点色**：`ColoredVertexLayout` 与 `GPUColoredMesh` 既可用于 `NewUnlitPipeline`，也可在设置 `MaterialPipelineOptions.VertexColors: true` 时用于 `NewMaterialPipeline`（`MaterialColoredWGSL`），或在设置 `LitPBRPipelineOptions.VertexColors: true` 时用于 `NewLitPBRPipelineWithOptions`（`LitPBRColoredWGSL` 及其雾、IBL、阴影与 CSM 变体）。未开启 `VertexColors` 时，金属度-粗糙度与光照 PBR 管线保持原有的 `MeshVertexLayout` 不变。
- **零分配 uniform 更新与设备丢失恢复**：`AppendUnlitUniforms` 在切片容量足够时以零分配写出 32 字节 uniform 块；`GPUUnlitMaterial.Update(dev, mat)` 原地更新参数且不缓存 `*gpu.Device` 指针，开启 `Retained: true` 的材质在设备丢失恢复后仍可继续更新与绘制。NSGo 只在有已恢复的绑定组使用某个绑定组布局时才恢复该布局，因此只有存在 retained 的 `GPUUnlitMaterial` 时 `MaterialDefaults.UnlitLayout()` 才会恢复；`MaterialDefaults.Valid` 不检查它。
- **glTF 辅助函数**：`UnlitMaterialFromGLTF` 校验 glTF 材质是否声明 `KHR_materials_unlit`（未声明时返回包装 `ErrInvalidUnlitMaterial` 的错误）。`UploadGLTFUnlitMaterial` 以 sRGB 解码并上传内嵌或外部的 `baseColorTexture` 图像，采样器由 `SamplerOptionsFromGLTF` 转换（因此即使纹理带有 mipmap，非 mipmap 的 `minFilter` 也只采样第 0 级），并像 `UploadGLTFMaterials` 一样应用 `GLTFMaterialUpload` 的 mipmap、各向异性与 `Retained` 选项。若 `baseColorTexture` 的图像只通过 `KHR_texture_basisu` 等扩展提供，则返回 `ErrInvalidUnlitMaterial`，而不是绘制成白色。
- **参考渲染验证**：`render3d` 的测试端到端绘制 Khronos `UnlitTest` 样例（加载器、`ColoredMeshFromGLTF`、`UploadGLTFUnlitMaterial`、`NewUnlitPipeline`），要求每个被覆盖的像素都是样例规定的纯色 `#FF7F00` 或 `#007FFF`；同时将涵盖不透明、顶点色、纹理叠加、镂空裁剪与半透明混合的场景与参考图 `render3d/testdata/golden/unlit_vertex_colors_scene.png` 比对，并在无界面 `gpu.View` 上同时验证完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置与 `EvaluateUnlit` 的像素一致性。

## 卡通着色（`ToonMaterial`、`ToonRenderer`）

`ToonMaterial` 与 `ToonRenderer` 绘制卡通/赛璐璐着色：分档的漫反射光照（或 1D 色阶纹理）、硬边阶梯 Blinn-Phong 高光、菲涅尔边缘光以及反转外壳描边。着色器（`ToonWGSL`、`ToonOutlineWGSL`）只用顶点与片元阶段，因此可在 WebGPU、Metal、Vulkan、Direct3D 11、OpenGL 3.3、OpenGL ES 3.0、WebGL 2 与 NSGo 软件渲染器上运行：

```go
defaults, err := render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Retained: true})
renderer, err := render3d.NewToonRenderer(cx, dev, render3d.ToonRendererOptions{
	CameraLayout: camBuf.Layout(),
	DepthFormat:  gpu.FormatDepth24Plus,
	Retained:     true,
})
spec := render3d.DefaultToonMaterial()
spec.BaseColorFactor = [4]float32{0.94, 0.34, 0.24, 1}
spec.OutlineWidth = 0.045
heroMat, err := renderer.NewMaterial(cx, dev, defaults, spec, "hero")

// 硬边网格（立方体、低模道具）需要共享棱角法线，描边才不会开裂：
boxOutlineCPU, err := render3d.SmoothOutlineNormals(boxMesh)
boxOutline, err := render3d.UploadMesh(cx, dev, boxOutlineCPU, render3d.MeshUpload{Label: "box-outline"})

// 在 OnFrame 中、带深度附件的渲染通道内：
light, err := render3d.ToonLightFromRig(rig) // 或 render3d.DefaultToonLight()
err = renderer.Draw(dev, pass, camBuf.BindGroup(), light,
	render3d.ToonDraw{Mesh: sphereMesh, Material: heroMat, Transform: sphereXform},
	render3d.ToonDraw{Mesh: boxGPU, OutlineMesh: boxOutline, Material: boxMat, Transform: boxXform},
)
```

- **分档与色阶纹理**：`Bands` 选择 2 到 8 档漫反射（`0` 表示 `3`），随 `N·L` 从 `ShadowColor` 过渡到完全受光；`BandSmoothness`（`[0, 0.5]`）柔化档位边界（`0` 为硬切分）。`RampTexture` 按 `u = clamp(N·L, 0, 1)` 采样，用美术绘制的色阶替代分档；未提供 `RampSampler` 时，材质会创建线性 clamp 采样器，避免色阶两端相互回绕。
- **高光**：`SpecularColor`、`SpecularPower`（`0` 表示 `32`）、`SpecularThreshold` 与 `SpecularSmoothness` 在受光面添加阶梯 Blinn-Phong 高光；`RimColor`、`RimThreshold` 与 `RimSmoothness` 基于 `1 - N·V` 添加菲涅尔边缘光。
- **光源**：`ToonLight` 包含一个 `DirectionalLight` 主光（对其 `N·L` 分档）和一个 `AmbientLight` 环境光，约定与 `LightRig` 相同：`Direction` 是光线行进的方向，方向为零是错误，强度 `0` 关闭该光源，错误都包装 `ErrInvalidLight`。`ToonLightFromRig` 取场景光照组的第一个平行光与环境光；点光源不参与。
- **绑定组**：`CameraGroup`（0）、`ToonMaterialGroup`（1，与 `MaterialGroup` 同一槽位）和 `ToonSceneGroup`（2，每次绘制的模型矩阵与主光，在 `ToonRenderer` 中使用动态偏移）。
- **材质**：`NewGPUToonMaterial` 与 `ToonRenderer.NewMaterial` 接收共享的 `MaterialDefaults`，省略的纹理与采样器绑定其中的白色纹理与采样器，与 `NewGPUMaterial` 一致。`GPUToonMaterial.Update` 零分配地重写 uniform，但不能替换纹理或采样器。
- **描边**：描边管线（`NewToonOutlinePipeline`，`gpu.CullFront`）把背面沿法线外推 `OutlineWidth` 并以 `OutlineColor` 填充；描边 alpha 为 `0` 或宽度为 `0` 时关闭描边。宽度以世界单位计；开启 `OutlineClipSpace` 时以 NDC y 单位计，只要相机宽高比与视口一致，在任何距离、任何方向上都是 `OutlineWidth × 视口高度 / 2` 像素。`SmoothOutlineNormals` 对同一位置（坐标按 1e-5 取整后相等）的顶点法线按角的大小加权平均，网格无效时返回包装 `ErrInvalidMesh` 的错误。
- **绘制**：`ToonRenderer.Draw` 先画描边再画卡通着色表面，均为不透明并写入深度。在一次提交前可以调用任意多次，绘入同一个或多个通道：每次调用把每次绘制的 uniform 写在本帧之前调用用过的槽位之后。在 `gpu.Frame` 中，`dev.FrameIndex()` 前进时槽位自动回到起点；直接用 `dev.Submit` 提交的代码每帧调用一次 `ToonRenderer.Begin`（两次调用之间最多 `MaxToonDrawsPerFrame` 次绘制）。渲染器、设备、通道或相机绑定组为 nil 或已关闭时，`Draw` 返回 `gpu.Error`。需要混合的卡通表面，用 `NewToonPipeline` 与 `NewToonOutlinePipeline` 并设置 `ToonPipelineOptions.Blend` 构建管线。
- **CPU 参考与测试**：`EvaluateToon` 与 `ToonQuantizeBand` 用 Go 计算 `ToonWGSL` 的结果。`render3d` 的测试把多物体场景与 `render3d/testdata/golden/toon_scene.png` 比对（`UPDATE_GOLDEN=1` 重写参考图），在无界面 `gpu.View` 上分别以完整配置与 `nsgotest.NoGPUCompute` 绘制，验证 `Retained` 渲染器在设备丢失后恢复、一次提交前绘制两次，以及宽视口上的裁剪空间描边。

## 自定义着色器材质（`CustomMaterial`）

`NewCustomMaterial` 将使用 **WGSL**（`ShaderWGSL`）或 **Kage**（`ShaderKage`）编写并声明强类型参数（`ShaderParamDesc`）的自定义 3D 材质编译到 NSGo `gpu.Device` 上，并在所属 `core.Scope` 内统一管理 std140 uniform 缓冲、`@group(1)` 绑定组（`CustomMaterialGroup`，与 `MaterialGroup` 是同一个槽位）、1×1 占位纹理与渲染管线。`GPUMaterial` 与 `MaterialBindingsWGSL` 为自定义着色器提供固定的 glTF 金属度-粗糙度绑定，`CustomMaterial` 则让着色器声明自己的参数与纹理：

```go
mat, err := render3d.NewCustomMaterial(cx, dev, render3d.CustomShaderDesc{
	Label:        "hologram",
	Language:     render3d.ShaderWGSL, // 或 render3d.ShaderKage
	CameraLayout: camBuf.Layout(),     // 将 CameraWGSL 绑定在 @group(0)；裁剪空间着色器可省略
	DrawLayout:   draw.Layout(),       // 可选：@group(2) 上的 PBRDrawBuffer，提供逐次绘制的模型矩阵
	DepthFormat:  gpu.FormatDepth24Plus,
	Cull:         gpu.CullBack,
	Retained:     true,                // 支持 GPU 设备丢失后自动恢复
	Params: []render3d.ShaderParamDesc{
		{Name: "intensity", Kind: render3d.ParamFloat, Default: float32(1.0)},
		{Name: "light_dir", Kind: render3d.ParamVec3, Default: math3d.Vec3{X: 0.4, Y: 0.85, Z: 0.35}},
		{Name: "tint", Kind: render3d.ParamColor, Default: core.RGB(0xf0c878)},
		{Name: "albedo", Kind: render3d.ParamTexture},
	},
	Source: myWGSLSource,
})

// 运行时更新参数，并在绘制前同步脏数据：
_ = mat.SetFloat("intensity", 1.5)
_ = mat.SetColor("tint", core.RGB(0x55d0ff))
_ = mat.SetTexture("albedo", tex, samp) // tex 为 nil 时恢复 1x1 默认纹理；samp 为 nil 时使用默认线性重复采样器
_ = mat.Sync(dev)                       // 参数未变化时零分配

// 在 OnFrame 中：
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
pass.SetBindGroup(render3d.PBRDrawGroup, draw.BindGroup())
mat.Draw(pass, mesh) // 设置管线、绑定 @group(1) 并绘制网格
```

- **声明式参数类型与 std140 布局**：支持 `ParamFloat`（`f32`）、`ParamVec2`（`vec2<f32>`）、`ParamVec3`（`vec3<f32>`）、`ParamVec4` / `ParamColor`（`vec4<f32>`）、`ParamInt`（`i32`）、`ParamBool`（`u32`，`0`/`1`，满足可移植 uniform 结构体约束）和 `ParamTexture`（`texture_2d<f32>` 与 `<name>_sampler`）。所有非纹理参数按声明顺序以 WGSL/std140 对齐规则打包至 `@group(1) @binding(0)`（`material`，可通过 `ComputeUniformLayout`、`ParamOffset`、`UniformSize` 查询），纹理与采样器紧随其后（`ParamBinding`）。float、int 或 bool 参数不能紧跟在 `ParamVec3` 之后：WGSL 会把下一个标量放进 vec3 的最后 4 字节，而 NSGo 的 Metal 后端把 `vec3` 结构体成员补齐到 16 字节；请把它声明在 vec3 之前或 vec2/vec4 之后，或改用 `ParamVec4`。未绑定的纹理槽位自动使用 1×1 占位纹理（默认不透明白色，可通过 `ShaderParamDesc.Default` 自定义）。
- **WGSL 与 Kage 着色器**：`ShaderWGSL` 在设置了 `CameraLayout` 且源码未在 `@group(0)` 中声明任何绑定时前置 `CameraWGSL`，并前置生成的 `MaterialParams` 与纹理绑定声明，除非设置 `ManualBindings: true`（此时由源码自行声明 `@group(1)`）。`ShaderKage` 解析 Kage 的 `var <Name> float|vec2|vec3|vec4|int|bool` 声明、辅助函数以及 `func Surface(uv vec2, normal vec3, worldPos vec3) vec4`（或 `func Fragment(dst vec4, pos vec2) vec4`），翻译为可移植 WGSL，支持 `sample(<tex>, uv)` 与 `image0At(uv)` 采样。与 Ebitengine 的 2D Kage 不同，`Fragment` 的 `pos` 是网格的归一化 UV 而不是纹素坐标，`dst` 是片元的帧缓冲位置，`imageNAt` 使用纹理的采样器（默认线性重复）采样，因此超出图像范围时会环绕，而不是返回透明。
- **位置与光照**：未设置 `DrawLayout` 时，Kage 顶点阶段原样传递位置与法线，`worldPos` 与 `normal` 处于物体空间，网格绘制在其顶点所在的位置。将 `DrawLayout` 设为 `PBRDrawBuffer` 的布局并在 `PBRDrawGroup`（group 2）绑定其绑定组后，Kage 着色器会用 `PBRDrawUniform` 的模型矩阵变换位置、用其法线矩阵变换法线，`worldPos` 与 `normal` 即处于世界空间；WGSL 着色器则像 `LitPBRWGSL` 一样自行声明该块。`ExtraLayout` 为着色器自行声明的绑定增加 group 3，例如用 `LightWGSLForGroup(3)` 声明的 `GPULightBuffer`（`LightWGSL` 使用的 group 1 已被材质占用），或用 `ShadowWGSL` 声明的 `DirectionalShadowMap`；它要求同时设置 `DrawLayout`。`Bind` 与 `Draw` 只绑定 group 1，group 0、2、3 由调用方绑定。
- **纹理切换**：`Sync` 会保留最近绑定的 8 种纹理/采样器组合的绑定组，因此在少量纹理之间交替（序列帧动画、乒乓渲染目标）时会复用它们而不产生分配；循环使用的不同纹理多于这个数量时，每次切换都会创建一个绑定组。
- **严格校验与全后端编译检查**：强类型设置方法（`SetFloat`、`SetVec2`、`SetVec3`、`SetVec4`、`SetColor`、`SetInt`、`SetBool`、`SetTexture`）及原子批量更新 `ApplyMap` 会拒绝未知参数名、类型不匹配、非有限数值（`NaN`/`Inf`）以及超出 `Min`/`Max` 范围的值，返回 `*CustomMaterialError`，它同时匹配 `ErrInvalidCustomMaterial` 与 `ErrInvalidMaterial`。其 `Line` 是源码行号，由 Kage 翻译出的代码中的 WGSL 错误也同样如此。`ApplyMap` 与采样器为 nil 的 `SetTexture` 一样，使用默认采样器绑定纹理。`CompileCheck` / `CompileCheckDesc` 无需打开 `gpu.View` 即可验证着色器在 MSL、HLSL、GLSL 3.30、GLSL ES 3.00（WebGL 2 / Android）与 WGSL 全部后端上的管线编译。`ByteSize` 为 `RenderStats` 与 `Budget` 报告 uniform 缓冲与占位纹理的大小。
- **参考渲染验证**：`render3d` 的测试将 WGSL 与 Kage 自定义材质的渲染结果与参考图（`render3d/testdata/golden/custom_material_wgsl.png` 和 `custom_material_kage.png`）比对，检查五个后端的 `PipelineCache` 编译产物，并在无界面 `gpu.View` 上同时覆盖 `full` 与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置、`Retained` 设备丢失恢复及稳态零分配更新；设置 `UPDATE_GOLDEN=1` 可重写参考图。

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

## 方向光阴影（`DirectionalShadowMap`、`DirectionalShadowConfig`）

`DirectionalShadowMap` 在 NSGo 的 `gpu.Device` 上管理离屏 `FormatDepth32Float` 阴影贴图渲染目标、仅写深度的投影管线（`ShadowDepthWGSL`）及动态偏移投影变换缓冲、96 字节的 `ShadowUniforms` 缓冲，以及最近邻（硬阴影）和线性（PCF）两种比较采样器。`ShadowWGSL` 在 `ShadowGroup`（第 3 组，位于 `CameraGroup`、`LightGroup` 与 `PBRDrawGroup` 之后）声明对应绑定，并提供可移植的 `sample_directional_shadow(world_pos, world_normal)` 辅助函数。`NewLitPBRShadowPipeline`（或 `LitPBRPipelineOptions.ShadowLayout`）构建带阴影的光照 PBR 管线（`LitPBRShadowWGSL`）：它与 `LitPBRWGSL` 完全相同地着色 `LightRig`，并用阴影衰减光照组的第一个方向光 `Directional[0]`，其他光源、环境光与自发光不受阴影影响。雾、IBL 与蒙皮也使用第 3 组，因此一条光照 PBR 管线只能选其一。

```go
sm, err := render3d.NewDirectionalShadowMap(cx, dev, render3d.ShadowMapOptions{
	Resolution:          1024,
	PCFKernel:           render3d.ShadowFilterPCF3x3, // 或 ShadowFilterHard
	DepthBias:           2,
	DepthBiasSlopeScale: 2.0,
	CullMode:            gpu.CullBack,
	Retained:            true, // GPU 设备丢失后自动恢复
})
litPipe, err := render3d.NewLitPBRShadowPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), sm.Layout(), gpu.FormatDepth24Plus)

// 每帧在 OnFrame(frame *gpu.Frame) 中：
dev := frame.Device()
cfg := render3d.DefaultDirectionalShadowConfig()
cfg.Direction = rig.Directional[0].Direction // 阴影所衰减的光源
cfg.Strength = 1
if !preset.Shadows { // quality 包的 quality.Preset
	cfg.Strength = 0 // 着色器随即跳过阴影贴图
}
_, err = sm.UpdateBounds(dev, cfg, sceneBounds) // 拟合正交视锥并对齐纹素

// 1. 仅写深度的阴影 pass（设备丢失期间或 Close 之后返回 nil）：
if preset.Shadows {
	if shadowPass := sm.BeginShadowPass(dev, frame); shadowPass != nil {
		sm.DrawCaster(shadowPass, pillarMesh, pillarModel)
		sm.DrawCaster(shadowPass, crateMesh, crateModel)
		shadowPass.End()
	}
}

// 2. 主光照 pass：
mainPass := frame.BeginRenderPass(gpu.RenderPassDesc{})
mainPass.SetPipeline(litPipe)
mainPass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
mainPass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
mainPass.SetBindGroup(render3d.ShadowGroup, sm.BindGroup())
mainPass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
floorMesh.Draw(mainPass)
mainPass.End()
```

- **取景与纹素对齐**：`DirectionalShadowConfig.LightViewProj()` 以 `Target` 为中心构建宽 `OrthoSize`、深 `DepthRange` 的正交视锥（`Target` 位于深度 0.5）；`FitBounds(lightDir, sceneBounds)` 包围一个 `AABB`，并可通过 `CasterPadding` 向光源方向延伸。设置 `TexelSnap: true` 后阴影贴图的纹素网格在世界空间中保持固定：视锥尺寸取包围盒外接球直径并向上取整到较粗的步长，因此包围盒移动或光源转动时纹素大小不变，光源空间中心再对齐到整纹素，于是取景移动（例如 `UpdateBounds` 跟随相机）时静止阴影只按整纹素平移，边缘不会闪烁。不设 `TexelSnap` 时 `FitBounds` 紧贴包围盒在光源空间中的范围，每个世界单位得到最多纹素，但包围盒移动时阴影边缘会闪烁。
- **硬阴影单次采样与 3x3 PCF**：`ShadowFilterHard` 使用最近邻比较采样器进行单次 `textureSampleCompareLevel` 采样；`ShadowFilterPCF3x3` 使用线性比较采样器对 9 个比较采样点求平均（每个采样点结合硬件 2x2 PCF 与 3x3 邻域），得到柔和半影。通过 `WriteUniforms`、`Update` 或 `UpdateBounds` 切换 `PCFKernel` 会动态切换 `BindGroup()`，无需分配内存或重建 GPU 资源。
- **消除自阴影伪影与全后端兼容**：着色器中的斜率缩放 `DepthBias`（按 `1 + 2*(1 - NdotL)` 缩放）、世界空间 `NormalBias` 与硬件光栅深度偏移（`ShadowMapOptions.DepthBias` / `DepthBiasSlopeScale`）共同消除平面与掠射角表面的阴影痤疮。自定义着色器只需前置 `ShadowWGSL`（第 3 组已被占用时使用 `ShadowWGSLForGroup(n)`）并调用 `sample_directional_shadow(world_pos, world_normal)`；`LightWGSL` 的 `shade_pbr_shadowed` 接收该可见度并作用于第 0 个方向光。整个流程不依赖计算着色器或存储缓冲，可在包括 WebGL 2 与 OpenGL ES 3.0 在内的所有 NSGo 目标上运行。
- **画质预设**：要遵循 `quality.Preset.Shadows`，在其关闭时跳过阴影 pass 并把 `Strength` 设为 0，如上例所示；`Strength` 为 0 时着色器直接返回完全可见，不再采样阴影贴图。
- **零分配编码与 CPU 参考实现**：`AppendShadowUniforms` 与 `ShadowUniforms.Encode` 无堆分配打包 96 字节 uniform 数据；NaN 或无穷大的矩阵或 `Strength` 会关闭阴影，非有限的偏移量变为 0，未知的 `PCFKernel` 按 `ShadowFilterHard` 处理，因此手工构造的 uniform 不会把 NaN 画到屏幕上。`EvaluateDirectionalShadow` 是在 `ShadowDepthGrid`（可由 `ShadowDepthGridFromBytes` 从 GPU 深度回读解码）上与之对应的纯 Go 参考实现，`ShadePointShadowed` 是带阴影光照的 CPU 参考实现。`render3d` 的测试用这两个参考实现逐一核对硬阴影与 3x3 PCF 渲染中所有露出的地面像素（包括半影），沿光线投射投影体以核对阴影落点，并在 `gpu.View` 的两种配置（`full` 与 `nsgotest.NoGPUCompute`）、`Retained` 设备丢失恢复以及作用域销毁中将渲染结果与参考图（`render3d/testdata/golden/shadow_directional_hard.png` 与 `shadow_directional_pcf.png`）比对。

## 渲染到纹理相机与多视口（`RenderTexture`、`ViewportRenderer`）

`RenderTexture` 分配归属于作用域的离屏 `gpu.RenderTarget`（颜色附件、可选深度/模板附件，以及带自动解析的可选 4x MSAA），并通过 `RenderTexture.GPUTexture()` 将可采样颜色输出直接暴露为 `*GPUTexture`。将其传入 `BindTexture`（或任意材质纹理槽位），即可把副相机渲染出的画面作为材质纹理贴在主场景的 3D 几何体上（如镜面、小地图、监控屏幕或传送门）：

```go
rt, err := render3d.NewRenderTexture(cx, dev, render3d.RenderTextureDesc{
	Label:       "mirror",
	Width:       256,
	Height:      256,
	DepthFormat: gpu.FormatDepth24Plus,
	Retained:    true, // 设备丢失后与保留的采样器和绑定组一起自动恢复
})
mirrorBG, err := render3d.BindTexture(cx, dev, texLayout, rt.GPUTexture(), samp)
```

`ViewportRenderer` 负责编排包含离屏 `RenderTexture` 相机与主表面多视口（左右分屏、上下分屏、四人四分屏或重叠画中画）的整帧渲染：

```go
renderer, err := render3d.NewViewportRenderer(cx, dev) // 在管线 CameraGroup 位置填入 renderer.CameraLayout()

halves := render3d.SplitScreenHorizontal(2) // 左半屏 [0..0.5]，右半屏 [0.5..1]
views := []render3d.CameraView{
	{Name: "mirror", Camera: mirrorCam, Target: rt, LayerMask: layerWorld, ClearColor: core.RGB(0x182030)},
	{Name: "player1", Camera: p1Cam, Viewport: halves[0], LayerMask: layerWorld | layerMirror, ClearColor: core.RGB(0x0c1018)},
	{Name: "player2", Camera: p2Cam, Viewport: halves[1], LayerMask: layerWorld | layerMirror},
}

err = renderer.RenderFrame(frame, render3d.SurfaceDesc{HasDepth: true}, views, func(ctx render3d.ViewportContext) {
	visible = render3d.FilterLayeredItems(sceneItems, ctx.LayerMask, visible[:0])
	plan := queue.Build(ctx.Uniform.ViewProj, ctx.Camera.Pos, visible)
	// ctx.Pass 已设置好当前视口的 SetViewport、SetScissor 与 CameraGroup 绑定组。
	drawPlan(ctx.Pass, plan)
})
```

- **执行顺序**：`ViewportRenderer` 始终先渲染离屏视图（`Target != nil`），再渲染主表面视图（`Target == nil`），每个阶段内按 `CameraView.Priority` 升序稳定排序（`OrderCameraViews`），确保主通道材质采样时离屏纹理已完成更新。
- **独立相机 uniform 与宽高比**：每个活跃视图拥有独立的保留 uniform 缓冲槽位，在录制渲染通道前统一上传并绑定到 `CameraGroup`，避免同一 `gpu.Frame` 或 `gpu.Encoder` 中的多个相机相互覆盖矩阵。除非设置 `CameraView.PreserveAspect`，否则会复制一份 `Camera` 并将 `Projection.Aspect` 调整为解析后的 `PixelViewport` 宽高比，不修改调用方的 `*scene3d.Camera`。这些槽位在每次调用间复用，因此每个提交的帧或命令缓冲最多调用一次 `RenderFrame` 或 `RenderEncoder`（后者需传入开启该编码器的设备）；同一次提交中需要多次调用时，每次调用各用一个 `ViewportRenderer`。
- **每相机图层掩码**：`CameraView.LayerMask` 遵循 `scene3d.Node.Layer` 位掩码约定（零值默认等同于 `scene3d.LayerAll`）。`LayerMatches`、`FilterLayeredItems` 与 `FilterNodesByLayer` 支持复用目标切片以零稳态分配按图层筛选 `LayeredDrawItem` 和 `*scene3d.Node`。这些函数与 `scene3d.Decal.LayerMask` 一样，零掩码不匹配任何对象，因此请传入 `ViewportContext.LayerMask`（已是生效掩码）；不渲染任何内容的相机请设置 `CameraView.Disabled`。`FilterNodesByLayer` 只检查图层，隐藏或禁用的节点也会保留。
- **分屏接缝与重叠视口**：`SplitScreenHorizontal`（`SplitScreenSideBySide`）、`SplitScreenVertical`（`SplitScreenStacked`）、`SplitScreenQuad` 与 `ViewportFromPixels` 生成归一化 `[0, 1]` 的 `ViewportRect`。`ViewportRect.Resolve` 对左右/上下边界分别取整，使相邻分屏视口严丝合缝且无像素重叠；`PixelViewport.Apply` 同时设置 `SetViewport` 与 `SetScissor`，杜绝跨分屏边界溢色。`PixelViewport` 以物理像素为单位；`PixelViewport.Bounds(pixelsPerPoint)` 除以表面的内容缩放比（像素宽度除以视图的点宽度），得到 `DebugDraw.Flush`、`Gizmo.DrawGizmo` 与 `BillboardRenderer.Draw` 所需的逻辑点 `core.Bounds`。同一目标上的连续视口共享单个 GPU 渲染通道，且只有开启通道的视口会清除，之后的视口会忽略自身的 `ClearColor`（需要不同背景时先绘制一个铺满视口的四边形）；若重叠画中画视口需要独立深度测试，设置 `CameraView.ResetDepth` 可在保留已有颜色的前提下清零深度缓冲。若某个视口在其他目标之后重新回到同一离屏目标（例如按 `Priority` 排序时，同一图集的左右两半之间夹着另一个目标），该目标的颜色与深度会被加载而不是再次清除，之前视口的输出得以保留。
- **尺寸调整与设备丢失**：`RenderTexture.Resize` 在尺寸未变时为空操作，尺寸变化时原子替换目标并递增 `Generation()`，方便调用方感知并重建 `BindTexture` 绑定组。开启 `Retained: true` 后，`RenderTexture` 与 `ViewportRenderer` 均可在 GPU 设备丢失恢复后继续工作，并在所属 `core.Scope` 销毁或调用 `Close()` 时释放。

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

用 [`go tool game dev`](cli.md#dev) 运行游戏，并将 `hotreload.ShaderReloader` 和 `hotreload.MaterialReloader`（位于 `github.com/nsgo-dev/game/pkg/tooling/hotreload`）挂到 `hotreload.NewDevWatcher` 上，保存修改后的 WGSL（`.wgsl`）、Kage（`.kage`）以及材质 YAML（`.material.yaml`、`.mat.yaml`、`.material.yml`、`.mat.yml`）文件时，改动会在一秒内生效而无需重启：

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

- **出错保留旧管线**：如果着色器存在编译错误、缺少入口函数或管线创建失败，或者材质 YAML 语法有误、包含未知字段或非法值，上一次可用的着色器、材质与 `*gpu.Pipeline` 会继续生效而不会崩溃。失败原因记录为 `ReloadError`（可解包为带文件名、行号与列号的 `*loaderr.Error`），可通过 `LastError` 和 `Errors` 查询，后续保存修复后自动清除。开启防抖时，`Watcher` 还会为 0 字节读取保留额外的稳定等待时间，避免先截断再写入的编辑器触发误报的空源文件错误；若文件确实持续为空，仍会正常报告该错误。
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

## 烘焙光照贴图（`LightmapMesh`、`LightmapRenderer`）

`LightmapMesh` 与 `GPULightmapMesh` 在 40 字节交错顶点布局（`LightmapVertexLayout`、`LightmapVertex`）中同时保存主材质 UV（`UV0`，对应 glTF `TEXCOORD_0`）与独立的第二套光照贴图 UV（`UV1`，对应 glTF `TEXCOORD_1`）。当 `UV1` 为空时，`UploadLightmapMesh` 会平滑回退到 `UV0`，并通过 `GPULightmapMesh.UsedUV0Fallback()` 告知调用方。在 `game/gltf` 中，`Document.MaterialLightmap` 可从 `MOZ_lightmap`、`EXT_lightmap` 或 `extras.lightmap` 提取烘焙光照贴图绑定（包括 `KHR_texture_transform` / 图集 `uvScale` 与 `uvOffset`），`Document.LightmapTexCoords` 与 `PrimitiveLightmapUVs` 则解析 `TEXCOORD_1` 并在缺失时回退到 `TEXCOORD_0`。

```go
lmRef, err := doc.MaterialLightmap(prim.Material)
lmUVs, _, err := doc.LightmapTexCoords(prim, lmRef)

lmMesh, err := render3d.UploadLightmapMesh(cx, dev, render3d.LightmapMeshFromAttributes(
	prim.Positions, prim.Normals, prim.TexCoords0, lmUVs, prim.Indices,
), render3d.MeshUpload{Label: "room", Retained: true})

renderer, err := render3d.NewLightmapRenderer(cx, dev, camBuf.Layout(), render3d.LightmapRendererOptions{
	DepthFormat: gpu.FormatDepth24Plus,
	Retained:    true,
})

// 每帧在 OnFrame 中调用：
renderer.BeginFrame()
camBuf.WriteCamera(cam)
mat := render3d.DefaultLightmapMaterial()
mat.BaseColorTexture = albedoTex
mat.Lightmap = lightmapTex // RGBM / RGBD：用 TextureUpload{SRGB: false} 上传
mat.Encoding, err = render3d.ParseLightmapEncoding(lmRef.Encoding) // "" 即 LightmapLinear
mat.Intensity = lmRef.Intensity
mat.RGBMMaxRange = lmRef.RGBMMaxRange // 0 表示使用 DefaultRGBMMaxRange
mat.SetUVTransform(render3d.UVTransform{Scale: lmRef.UVScale, Offset: lmRef.UVOffset})
mat.DirectLight = &rig.Directional[0] // 可选：LightRig 中的光源，Direction 为光线传播方向
_ = renderer.Draw(pass, camBuf, lmMesh, worldMat, mat)
```

- **HDR 与 LDR 编码**：支持线性 8 位或 float16 HDR 纹理（`LightmapLinear`，配合 `UploadLightmapRGBA16F` 上传 float16）、sRGB LDR 纹理（`LightmapSRGB`，以 `TextureUpload{SRGB: true}` 上传时自动避免重复解码），以及面向 WebGL 2 与移动端的 8 位打包 HDR 格式（通过 `EncodeRGBM` / `EncodeRGBMImage` 使用 `LightmapRGBM`，以及通过 `EncodeRGBD` / `EncodeRGBDImage` 使用 `LightmapRGBD`）。
- **共享光照图集**：通过 `LightmapMaterial.UVScale` 与 `UVOffset`（或 `SetUVTransform`）指定子矩形变换，借助动态 uniform 偏移在同一渲染通道中让多个网格共享同一张烘焙光照图集。
- **环境回退与动态光叠加**：`LightmapMaterial.Lightmap` 为 nil 时使用 `AmbientFallback` 作为环境光；可将 `LightmapMaterial.DirectLight` 设为 `*DirectionalLight`（与 `LightRig` 中的类型相同，`Direction` 为光线传播方向），把它的 Lambert N·L 项叠加到烘焙光照上。法线经法线矩阵变换，因此非均匀缩放的关卡几何也能得到正确光照。`Intensity` 为 0 表示没有烘焙光；`DefaultLightmapMaterial` 将其设为 1。
- **输出编码**：着色器会把线性结果编码为 sRGB，与 `LitPBRWGSL` 和材质管线一致，因此同一通道中绘制的光照贴图几何与 PBR 光照物体颜色一致。颜色格式（`LightmapRendererOptions.ColorFormat` 或表面格式）为 sRGB 时，编码交给硬件完成。
- **图集变换**：`LightmapRef.UVScale` / `UVOffset` 是唯一生效的图集变换；`LightmapRef.Texture` 不携带 scale 与 offset，因此不要再用 `TransformTexCoords` 对它应用一次。数值来源见[烘焙光照贴图](3d-models-blender.md#烘焙光照贴图)。
- **纹理流式加载**：`BeginFrame` 会丢弃已关闭的光照贴图与反照率纹理对应的缓存绑定组，因此流式加载关卡或热重载光照贴图不会让缓存无限增长。
- **适用范围**：`LightmapRenderer` 是独立的 Lambertian 渲染路径，结果为反照率乘以（烘焙光照加可选的一盏 `DirectionalLight`）。它不支持金属度-粗糙度、法线、遮蔽或自发光贴图，也不读取 `LightRig`。

### PBR 光照贴图路径（`GPUPBRLightmap`、`NewPBRLightmapPipeline`、`UploadGLTFPBRLightmaps`）

如果希望在烘焙几何体上同时保留完整的 glTF 2.0 金属度-粗糙度 PBR 材质（基础色、金属度-粗糙度、法线、遮蔽、自发光、透明模式、双面渲染）以及 `LightRig` 的动态光源（方向光、点光源、聚光灯），请将 `NewPBRLightmapPipeline` 与 `GPUMaterial`、`GPUPBRLightmap` 配合使用：

```go
matSet, err := render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{Retained: true})
lmSet, err := render3d.UploadGLTFPBRLightmaps(cx, dev, doc, render3d.GLTFPBRLightmapUpload{Retained: true})
lmMeshData, err := render3d.LightmapMeshFromGLTF(doc, prim)
lmMesh, err := render3d.UploadLightmapMesh(cx, dev, lmMeshData, render3d.MeshUpload{Retained: true})

pipe, err := render3d.NewPBRLightmapPipeline(
	cx, dev,
	camBuf.Layout(), lightBuf.Layout(), defaults.Layout(), lmSet.Layout(),
	render3d.PBRLightmapPipelineOptions{DepthFormat: gpu.FormatDepth24Plus, Retained: true},
)

// 在 OnFrame 中：
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRLightmapMaterialGroup, matSet.Material(prim.Material).BindGroup())
lmSet.Lightmap(prim.Material).Bind(pass) // 或用 lmSet.NewInstance(...) 为不同节点创建独立变换实例
lmMesh.Draw(pass)
```

- **绑定组与顶点布局**：复用 40 字节 `LightmapVertexLayout`（`Position`、`Normal`、`UV0`、`UV1`），绑定组依次为 `CameraGroup`（0）、`LightGroup`（1，`GPULightBuffer`）、`PBRLightmapMaterialGroup`（2，由 `NewGPUMaterial` 或 `UploadGLTFMaterials` 创建的 `GPUMaterial`）以及 `PBRLightmapGroup`（3，`GPUPBRLightmap`，携带模型/法线矩阵、图集 `UVTransform`、`LightmapEncoding`、`Intensity`、`RGBMMaxRange` 与光照贴图纹理/采样器）。
- **着色公式**：解码后的光照贴图替换环境光 / 间接漫反射项，计算为 `(1 - metallic) * baseColor * decodedLightmap * intensity * occlusion`（因此绑定光照贴图时不会重复叠加 `LightRig.Ambient`）；`LightRig` 的方向光、点光源与聚光灯继续提供 Cook-Torrance GGX 直接光项并加上自发光，输出使用 `linear_to_srgb` 编码（开启 `LinearOutput` 或目标格式为 sRGB 时输出未钳制的线性值）。对于无动态光的非金属（`metallic = 0`）、粗糙（`roughness = 1`）、无贴图材质，结果与 `LightmapRenderer` 一致；加入 `LightRig` 光源时叠加的直接光项与 `LitPBRWGSL` 一致。CPU 参考实现为 `EvaluatePBRLightmap`。
- **glTF 集成**：`UploadGLTFPBRLightmaps` 解析 `MOZ_lightmap`、`EXT_lightmap` 与 `extras.lightmap`（含 `KHR_texture_transform`、`scaleOffset`、`uvScale`、`uvOffset`、`intensity`、`encoding`、`rgbmMaxRange`），对光照贴图纹理与采样器去重，并返回与 `GLTFMaterialSet` 配对的 `GLTFPBRLightmapSet`（多个节点共享同一材质但变换不同时可用 `NewInstance`）。

### 何时使用 `LightmapRenderer`，何时使用 PBR 光照贴图路径

- **使用 `LightmapRenderer`**：适合轻量级静态建筑、移动端/低端设备填充率预算紧张的场景，或只需要反照率贴图（`UV0`）、烘焙光照贴图（`UV1`）以及至多一盏 Lambert 方向光的纯漫反射烘焙几何——每次绘制只绑定两张纹理，并通过 `BeginFrame` / `Draw` 自动管理动态 uniform 环形缓冲。
- **使用 PBR 光照贴图路径（`NewPBRLightmapPipeline` + `GPUPBRLightmap`）**：只要烘焙几何还需要金属度-粗糙度贴图、切线空间法线贴图、环境遮蔽贴图、自发光贴图、Alpha Mask/Blend、双面材质、HDR `LinearOutput`，或需要在同一条管线中叠加 `LightRig` 的方向光、点光源与聚光灯动态直接光，就应选择 PBR 光照贴图路径。

## 点光源与聚光灯阴影（`SpotShadowMap`、`PointShadowMap`）

`SpotShadowMap` 与 `PointShadowMap` 为 `LightRig` 的第一盏聚光灯（`rig.Spot[0]`）和第一盏点光源（`rig.Point[0]`）生成阴影。它们根据光照组自身的 `SpotLight` 与 `PointLight` 渲染纯深度 `FormatDepth32Float` 阴影贴图，带阴影的 PBR 管线再把该光源的贡献乘以阴影可见度，因此带阴影的光源与 `LitPBRWGSL` 中同一光源的着色完全一致（Cook-Torrance GGX、能量守恒的 Lambert、`PointLightAttenuation`、glTF 锥角衰减）。所有功能在每个 NSGo 后端上都能运行，包括 WebGL 2 / OpenGL ES 3.0 配置（`nsgotest.NoGPUCompute`）与软件渲染器：

```go
spot, err := render3d.NewSpotShadowMap(cx, dev, render3d.DefaultSpotShadowConfig())
litPipe, err := render3d.NewLitPBRSpotShadowPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBufs[0].Layout(), spot.Layout(), gpu.FormatDepth24Plus)

// 每帧在 OnFrame 中：
_ = spot.Update(dev, rig.Spot[0]) // 阴影作用的光源
if sp := spot.BeginShadowPass(dev, frame); sp != nil {
	spot.DrawCaster(sp, pillarMesh, pillarModel) // 通道已满时返回 false
	sp.End()
}

mp := frame.BeginRenderPass(gpu.RenderPassDesc{ /* 颜色 + 深度 */ })
mp.SetPipeline(litPipe)
mp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
mp.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
mp.SetBindGroup(render3d.LocalShadowGroup, spot.BindGroup())
for i, mesh := range meshes {
	mp.SetBindGroup(render3d.PBRDrawGroup, drawBufs[i].BindGroup()) // 每次绘制的模型矩阵与材质
	mesh.Draw(mp)
}
mp.End()
```

点光源则用 `rig.Point[0]` 更新，用 `NewLitPBRPointShadowPipeline` 绘制，并渲染各个立方体面：

```go
_ = point.Update(dev, rig.Point[0])
for face := range gpu.CubeFace(render3d.PointShadowFaceCount) {
	if fp := point.BeginFacePass(dev, frame, face); fp != nil {
		point.DrawCaster(fp, blockerMesh, blockerModel)
		fp.End()
	}
}
```

- **聚光灯投影**：从光源 `Position` 沿 `Direction` 看出的正方形 `[0, 1]` 深度透视投影，视场角为 `2 * OuterConeAngle`（`SpotShadowViewProj`）。远平面取光源的 `Range`；`Range` 为 0 的光源取 `SpotShadowConfig.Far`（默认 `DefaultLocalShadowFar`，即 50）；`Near` 默认为 0.1。锥角超过 `MaxSpotShadowConeAngle`（略小于 90°）时无法投影，`Update` 返回 `ErrInvalidLocalShadow`。
- **点光源图集**：6 个 90° 立方体面打包进一张 `(3 * FaceResolution) × (2 * FaceResolution)` 的 2D 深度图集，因为 NSGo 无法把深度立方体贴图的单个面作为附件。`sample_point_shadow` 选取主轴方向的面，直接计算透视深度 `(far/(far-near)) * (1 - near/axisDist)`，并把每次采样钳制在所属瓦片内，使 3×3 PCF 不会渗入相邻面。`BeginFacePass` 渲染第 0 面时清除整个图集，第 1..5 面保留已有内容，因此只刷新部分面时，要么连同其他所有面一起刷新第 0 面，要么不刷新第 0 面。
- **投射体**：`DrawCaster` 把每个投射体的 `(lightViewProj, model)` 写入各自的 256 字节动态偏移槽位，因此一个通道可以绘制多个投射体。`MaxCasters`（默认 256，最大 `MaxLocalShadowCasters`）对聚光灯按每个通道计算，对点光源按每个面计算；每个面有自己的槽位，因此各面可以按任意顺序刷新。通道或面已满、或尚未调用 `Update` 时，`DrawCaster` 返回 false。
- **滤波与偏移**：`LocalShadowFilterHard`（一次最近邻比较采样）或 `LocalShadowFilterPCF3x3`（九次线性采样），配合常量 `DepthBias` 与世界空间 `NormalBias`。
- **第 3 绑定组**：`LocalShadowGroup` 即 `ShadowGroup`（3），方向光阴影、雾、IBL 与蒙皮也使用该组，因此一条带光照的 PBR 管线只能选用其中之一（`LitPBRPipelineOptions.SpotShadowLayout` 或 `PointShadowLayout`）。需要同时使用两种阴影、或第 3 组另有用途的自定义着色器，可用 `SpotShadowWGSLForGroup` / `PointShadowWGSLForGroup` 放置绑定，并用 `LightWGSL` 中的 `shade_pbr_visibility(…, directional0_vis, point0_vis, spot0_vis)` 缩放各光源。
- **设备丢失与 CPU 参考实现**：设置 `Retained: true` 后，深度目标、采样器、缓冲、绑定组与深度管线会在设备丢失后恢复。`EvaluateSpotShadow`、`EvaluatePointShadow` 与 `PointShadowSelectFace` 与 WGSL 一一对应，`ShadePointVisibility` 配合 `LightVisibility` 可复现带光照的像素；测试将它们与 GPU 结果以及参考渲染图 `render3d/testdata/golden/local_shadow_spot.png` 和 `local_shadow_point.png`（`UPDATE_GOLDEN=1` 可重写）进行比较。

## 级联阴影贴图（`CascadedShadowMap`、`CSMConfig`）

`CascadedShadowMap` 用最多四级级联（`MaxCascades`）为 `LightRig` 的第一盏方向光（`rig.Directional[0]`）投射阴影，使阴影在相机附近保持清晰，同时覆盖到地平线。它是 `DirectionalShadowMap` 面向大场景的替代方案：受光 PBR 管线与 `LitPBRWGSL` 完全一样地着色整个光照组，只把这一盏灯乘以级联阴影，其他光源、环境光与自发光不受阴影影响。全部功能在每个 NSGo 后端上运行，包括 WebGL 2 / OpenGL ES 3.0 配置（`nsgotest.NoGPUCompute`）与软件渲染器：

```go
cfg := render3d.DefaultCSMConfig() // 4 级级联、每级 1024 texel、3x3 PCF、texel 对齐
cfg.MaxShadowDistance = 80
cfg.Retained = true
csm, err := render3d.NewCascadedShadowMap(cx, dev, cfg)
litPipe, err := render3d.NewLitPBRCSMPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBufs[0].Layout(), csm.Layout(), gpu.FormatDepth24Plus)

// 每帧在 OnFrame(frame *gpu.Frame) 中：
shadowFrame := render3d.FitCascadesFromCamera(cam, rig.Directional[0].Direction, csm.Config()) // 级联所遮挡的那盏灯
_ = csm.Update(dev, shadowFrame)
for i := range csm.CascadeCount() {
	if pass := csm.BeginCascadePass(frame, i); pass != nil {
		for j, mesh := range casterMeshes {
			csm.DrawCaster(pass, mesh, casterModels[j]) // 占满 MaxCasters 个不同模型矩阵后返回 false
		}
		pass.End()
	}
}

mp := frame.BeginRenderPass(gpu.RenderPassDesc{ /* 颜色 + 深度 */ })
mp.SetPipeline(litPipe)
mp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
mp.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
mp.SetBindGroup(render3d.CSMGroup, csm.BindGroup())
for i, mesh := range meshes {
	mp.SetBindGroup(render3d.PBRDrawGroup, drawBufs[i].BindGroup())
	mesh.Draw(mp)
}
mp.End()
```

- **分割（`ComputeCSMSplits`）**：用实用分割方案划分 `[near, min(far, MaxShadowDistance)]`，以 `SplitLambda`（默认 0.75）混合对数分割与均匀分割。
- **拟合（`FitCascades`、`FitCascadesFromCamera`）**：每级级联在相机视锥中所占的一段按投影自身的近、远平面截取，因此透视与正交相机都适用。`FitCascadesFromCamera` 使用 `Camera.ProjMatrix` 所用的近、远平面；正交相机的近平面位于相机处或其后方时，阴影从视深 0.1 开始。开启 `TexelSnap`（默认）时，每级级联按包围球确定大小并对齐到整 texel，相机移动或转动时阴影边缘不会闪烁。`CasterPadding` 把每级级联的近平面向光源方向拉远，使视野外的投射体仍能把阴影投进视野。拟合与 `CSMFrame.EncodeUniforms` 都不分配内存。
- **单张 2D 深度图集**：各级级联是同一张 `FormatDepth32Float` 纹理中的图块（1 级为 1x1、2 级为 2x1、3 或 4 级为 2x2），因此不需要深度纹理数组。如果某帧拟合时使用的级联数、分辨率或滤波方式与图集不同，`Update` 会以 `ErrInvalidCSM` 拒绝它，因为其图块、texel 大小与滤波方式无法与图集及其采样器对应。`Update` 之后的第一次 `BeginCascadePass` 无论绘制哪一级都会清空整张图集；每次 `Update` 之后请渲染全部级联，顺序不限。
- **投射体**：`DrawCaster` 每帧把每个不同的模型矩阵写入各自的 256 字节动态偏移槽位，并在其他级联中复用，因此 `MaxCasters`（默认 256）统计的是两次 `Update` 之间不同模型矩阵的数量，而不是绘制次数。槽位用尽时它不绘制并返回 false。`DrawCasterGroup` 则使用调用方自有的变换。
- **滤波与混合**：`ShadowFilterHard`（一次最近点比较采样）或 `ShadowFilterPCF3x3`（九次线性采样，限制在该级图块内），与单张方向光阴影贴图使用同一个 `ShadowFilter`。`CascadeBlendRange` 在每级级联的这一比例范围内向下一级交叉淡入，并让最后一级朝 `MaxShadowDistance` 淡出；`DebugCascades` 按级联给表面着色（红、绿、蓝、黄）。
- **绑定组 3**：`CSMGroup` 就是 `ShadowGroup`（3），因此受光 PBR 管线只能在级联阴影贴图与方向光、聚光灯或点光源阴影、雾、IBL、蒙皮中取其一（`LitPBRPipelineOptions.CSMLayout`）。自定义着色器在前面拼接 `CameraWGSL` 与 `CSMWGSL`（绑定组 3 已有他用时用 `CSMWGSLForGroup(n)`），并把 `sample_csm_shadow(world_pos, world_normal)` 传给 `LightWGSL` 中的 `shade_pbr_shadowed`。
- **设备丢失与 CPU 参考实现**：设置 `Retained: true` 时，图集、采样器、缓冲区、绑定组与深度管线会在设备丢失后恢复。`EvaluateCSMShadow` 在通过 `ReadCSMDepthAtlas` / `ReadCSMDepthAtlasAsync` 读回的图集上复现 WGSL，`ShadePointShadowed` 以该可见度计算、再乘以 `CSMFrame.DebugTint`，即可复现受光像素；测试在两种配置下把它们与 GPU 结果以及黄金渲染图 `render3d/testdata/golden/csm_large_scene.png` 比较（`UPDATE_GOLDEN=1` 会重写它）。

## 3D 后处理（`PostStack3D`、`BloomPass`、`SSAOPass`、`DepthOfFieldPass`）

`PostStack3D` 通过离屏 3D 颜色+深度 `gpu.RenderTarget`（默认为 `FormatRGBA16Float` + `FormatDepth32Float`）组合屏幕空间环境光遮蔽（`SSAOPass`）、深度感知圆盘散景景深（`DepthOfFieldPass`）和 HDR 泛光（`BloomPass`），并将最终结果写入交换链或指定的目标纹理：

```go
stack, err := render3d.NewPostStack3D(cx, dev, render3d.PostStack3DOptions{
	Width:        uint32(w),
	Height:       uint32(h),
	OutputFormat: dev.SurfaceFormat(),
	Retained:     true,
	EnableSSAO:   true,
	SSAO:         render3d.SSAOConfigFromCamera(cam),
	EnableDoF:    true,
	DoF:          render3d.DepthOfFieldConfigFromCamera(cam, 5.0, 2.0, 5.0),
	EnableBloom:  true,
	Bloom:        render3d.DefaultBloomConfig(),
})

// 在 OnFrame 中：
pass := stack.BeginScenePass(frame, core.RGB(0x0c1018))
// ... 将 3D 网格绘制到 stack.SceneTarget() ...
pass.End()
_ = stack.ApplyScene(frame.Device(), frame, nil) // SSAO -> 景深 -> Bloom -> 交换链
frame.Submit()

// 视图尺寸变化（或相机缩放）时，刷新 SSAO 与景深所依赖的投影：
_ = stack.Resize(dev, uint32(w), uint32(h))
cam.SetViewport(float32(w), float32(h))
_ = stack.SetCamera(cam)
```

- **可组合管线与独立通道**：`PostStack3D` 按固定顺序执行已启用的阶段——**SSAO → 景深 → Bloom**——中间结果在乒乓离屏渲染目标之间流转，最后一个阶段直接以 `OutputFormat` 输出（全部关闭时执行干净直通复制）。`SetSSAO`、`SetDepthOfField` 与 `SetBloom` 用于开关单个阶段；传入零值配置时保留当前设置，因此 `stack.SetSSAO(false, render3d.SSAOConfig{})` 只会关闭 SSAO。每个通道（`SSAOPass`、`DepthOfFieldPass`、`BloomPass`）也可独立创建，并通过 `RenderPassRecorder`（`*gpu.Frame` 与 `*gpu.Encoder` 均已实现）作用于调用方自建的 `gpu.Texture` 颜色与深度附件。
- **格式与输出目标**：`ColorFormat`（场景与中间渲染目标）可取 `FormatRGBA16Float`（默认）、`FormatRGBA8Unorm`、`FormatRGBA8UnormSRGB`、`FormatBGRA8Unorm` 或 `FormatBGRA8UnormSRGB`，即可过滤采样的四通道格式，因为景深把弥散圆存放在 alpha 通道中。目标为 nil 时以 `OutputFormat` 写入交换链；目标纹理必须是 `ColorFormat` 或 `OutputFormat`，最后一个阶段使用为该格式构建的管线绘制。所有输入与目标纹理的尺寸都必须与通道或后处理栈一致，且目标不能同时是输入，否则 `Apply` 返回 `ErrInvalidPostProcess`。
- **相机投影**：`SSAOConfigFromCamera` 记录 `cam.ProjMatrix()`，`DepthOfFieldConfigFromCamera` 从该矩阵读取近、远裁剪面与投影类型，因此 `scene3d.Projection` 为零值字段填入的默认值（以及位于相机处或其后方的正交近裁剪面）都与深度缓冲一致。两者只在创建配置时记录一次；投影变化时请调用 `PostStack3D.SetCamera`（独立通道则调用 `SetConfig`）。`ProjMatrix` 为零时，SSAO 回退为按通道宽高比、使用配置中 `Near` 与 `Far` 的 60° 透视投影。
- **SSAO（`SSAOConfig`、`SSAOPass`）**：从 3D 深度缓冲重建视空间位置与保边法线，使用带 4×4 分层切线旋转的确定性 4–16 采样半球核计算遮蔽，支持可选的深度感知双边模糊（`BlurRadius`、`DepthThreshold`），并将环境光遮蔽乘入场景颜色（可通过 `AOTexture` 获取 AO 纹理）。
- **景深（`DepthOfFieldConfig`、`DepthOfFieldPass`）**：支持透视与正交 `[0, 1]` 深度线性化，围绕 `FocusDistance` ± `FocusRange` 计算带符号弥散圆（CoC），并通过 18 采样双环圆盘散景模糊平滑虚化近景与远景，同时阻止背景模糊色溢入清晰的焦内轮廓。
- **泛光（`BloomConfig`、`BloomPass`）**：用二次软拐点阈值曲线（`Threshold`、`SoftKnee`）提取 LDR 或 HDR（`> 1.0`）高亮像素，执行 `BlurPasses` 轮可分离 9 采样高斯横纵模糊（可通过 `HalfRes` 启用半分辨率以获得更宽柔和光晕），再以 `Intensity` 加法混合回场景。
- **跨后端兼容性、生命周期与参考渲染**：所有通道均使用纯片元可移植 WGSL、显式 `textureSampleLevel` 以及代数/预计算权重（不依赖计算着色器，也不使用 `exp`/`log2` 指令），同时兼容完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / OpenGL ES 3.0）配置，并支持 `Resize(dev, w, h)` 与 `Retained` 设备丢失恢复。黄金参考图（`render3d/testdata/golden/post3d_bloom.png`、`post3d_ssao.png`、`post3d_dof.png` 与 `post3d_combined.png`）用于验证各个独立通道与组合栈；设置 `UPDATE_GOLDEN=1` 可重写参考图。

## 集群前向光照（`ClusteredLightSystem`、`ClusterGrid`）

`LightRig` 最多着色 16 个点光源和 8 个聚光灯。对于有数百个局部光源的场景，`ClusteredLightSystem` 在 `LightGroup` 上取代 `GPULightBuffer`：它在绑定 0 上传同一个 `LightRig`（环境光、方向光以及 rig 自身的点光源和聚光灯），并把任意数量的额外局部光源（`ClusterLight`）分配到由屏幕图块 × 对数深度切片组成的三维网格中（默认 `16 × 9 × 24`，最多 `512` 个可见光源、每个集群 `64` 个）。每个片元只着色自己所在集群的光源，使用与 `shade_pbr` 相同的 Cook-Torrance BRDF 和 glTF `KHR_lights_punctual` 衰减，因此集群光源与 rig 光源的效果完全一致。由于集群分配在 CPU 上完成，该路径只需要片元存储缓冲区（`Features.StorageBuffers` 与 `Limits.MaxStorageBufferBindingSize`），不依赖计算着色器，因此 Apple Metal 设备可以直接运行完整的集群路径。在不支持存储缓冲区的目标上（WebGL 2、OpenGL ES 3.0、Linux `GtkGLArea`，以及使用 `nsgotest.NoGPUCompute` 的无头测试），它回退为单个 uniform 缓冲区，存放 `FallbackMaxLights`（默认 `16`）个最重要的可见光源：

```go
sys, err := render3d.NewClusteredLightSystem(cx, dev, render3d.DefaultClusterConfig())
pipe, err := render3d.NewClusteredLitPBRPipeline(cx, dev, camBuf.Layout(), sys, drawBuf.Layout(), gpu.FormatDepth24Plus)

// 场景光源只需转换一次；Range 为 0（无界）时会推导出截止范围。
local := make([]render3d.ClusterLight, 0, len(lamps))
for _, l := range lamps {
	cl, err := render3d.ClusterLightFromPoint(l)
	if err != nil {
		return err
	}
	local = append(local, cl)
}

// 每帧录制绘制命令之前：
camBuf.WriteCamera(cam)
stats, err := sys.UpdateCamera(frame.Device(), cam, viewW, viewH, rig, local)

pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
sys.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)
```

- **光源**：`ClusterLight` 是点光源或聚光灯记录，其 `Range` 必须为正，因为集群划分需要包围球。`ClusterLightFromPoint` 和 `ClusterLightFromSpot` 转换 `PointLight` 或 `SpotLight`；`Range == 0` 时，推导出 `Intensity * max(Color) / d^2` 降到 `ClusterRangeCutoff`（1/256）的距离。对于未通过 `Validate` 的光源（包括 `Range == 0`），`Update` 返回包装 `ErrInvalidClusterLight` 的错误；对于格式错误的 rig 返回包装 `ErrInvalidLight` 的错误，而不是丢弃它。`EvaluateLightAttenuation` 复用 `PointLightAttenuation` 和 `SpotConeAttenuation`。
- **视口尺寸**：`viewW` 和 `viewH` 必须是该渲染通道实际渲染区域的尺寸，且区域从原点开始，因为着色器根据 `@builtin(position)` 计算片元所在的图块。使用 `DynamicResolutionTarget` 时，传入 `ScaledSize()`，而不是原生尺寸。
- **着色器**：`NewClusteredLitPBRPipeline` 编译 `ClusteredLitWGSL` 或 `ClusteredFallbackWGSL`（与 `sys.WGSL()` 一致的那个），即把 `LitPBRWGSL` 中的 `shade_pbr` 换成 `shade_pbr_clustered` 的模块，输出 sRGB。自定义着色器在前面拼接 `CameraWGSL` 和 `sys.BindingsWGSL()`（`ClusteredBindingsWGSL` 或 `ClusteredFallbackBindingsWGSL`，两者都已包含 `LightWGSL`），并调用 `shade_pbr_clustered(world_pos, normal, view_dir, base_color, metallic, roughness, frag_pos)`，它在两条路径上签名相同。
- **CPU 集群划分（`ClusterGrid`）**：视图空间的集群 AABB 按 `(proj, near, far)` 缓存，相机移动时复用。`AssignLights` 剔除视锥外的光源，计算紧凑的屏幕图块和深度切片范围，进行球体与 AABB 相交测试（聚光灯另加保守的圆锥与 AABB 剔除），并在稳态下零分配地打包 `Offsets` 和 `LightIndices`。超出 `MaxLights` 的光源，以及单个集群中超出 `MaxLightsPerCluster` 的光源，按输入顺序丢弃（`TruncatedLights`、`TruncatedClusters`），因此当场景可能超出任一上限时，请把重要光源放在前面。
- **回退选择**：`SelectFallbackLights` 先丢弃包围球在视锥外的光源，再按功率、范围和到相机的距离对其余光源排序。回退模式下，`CulledLights` 统计视锥外或没有功率的光源，`TruncatedLights` 统计因上限而未被选中的可见光源。
- **深度切片查找**：着色器在 uniform 切分深度表上做 6 步二分查找来确定片元的深度切片，而不使用 `log2` 和 `exp`，因为 NSGo v0.3.0 的软件 GPU 渲染器会把这些内置函数求值为零。
- **CPU 参考实现**：`ShadeClusteredPoint` 是 `ShadePoint` 加上所有集群光源，`ShadeClusteredGridPoint` 只使用片元所在集群的光源，与 GPU 完全一致。测试在完整能力和 `NoGPUCompute` 配置下将 GPU 像素与它们比对，包括一个 256 光源的场景。
- **上限**：`Validate` 将 `MaxLights` 限制在 `MaxClusterLights`（65,536）以内，要求 `MaxLightsPerCluster` 不超过 `MaxLights`，并将索引槽数（`TilesX * TilesY * SlicesZ * MaxLightsPerCluster`）限制在 `MaxClusterIndexEntries` 以内；当某个存储缓冲区超过设备的 `MaxStorageBufferBindingSize` 时，`NewClusteredLightSystem` 返回 `ErrInvalidClusterConfig`。
- **桌面 GPU 实测性能**：在 MacBook Pro（Apple M4 Pro，Metal 4，macOS arm64，`2560 × 1440` 可绘制表面，120 Hz 显示器）上实测 256 盏动态集群点光源（约 250 盏可见、约 475 个活跃集群）：集群存储缓冲路径持续稳定在 `120.0 fps`，CPU 集群分配与缓冲区上传耗时约 145 µs/帧。
- **生命周期**：设置 `ClusterConfig.Retained` 可在 GPU 设备丢失后恢复缓冲区；`ByteSize()` 报告 GPU 缓冲区字节数，供 `RenderStats` 和预算使用，`Close()` 立即释放资源。

## 权重图地形材质（`TerrainMaterial`、`GPUTerrainMaterial`）

为高度图地形（[world3d](world3d.md)）渲染最多 8 层由 1–2 张 RGBA 权重图（splat map）驱动的 PBR 材质，支持逐层 UV 平铺、基于高度图的锐利过渡、陡坡三平面投影（triplanar），以及与其他 `render3d` 材质一致的动态光照和阴影：

```go
gpuTerrain, err := render3d.NewGPUTerrainMaterial(cx, dev, render3d.TerrainMaterial{
	Layers:              layers, // 1..8 个 TerrainLayer（反照率、法线、金属度/粗糙度、高度图、UVScale、HeightBias）
	SplatMap0:           splatImg0,
	SplatMap1:           splatImg1, // <= 4 层时可省略
	HeightBlendContrast: 0.65,
	Triplanar:           render3d.DefaultTerrainTriplanar(),
}, render3d.TerrainMaterialUpload{Label: "alpine", Mipmaps: true, Retained: true})

pipe, err := render3d.NewTerrainMaterialPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), gpuTerrain.Layout(), render3d.TerrainPipelineOptions{
	DepthFormat:  gpu.FormatDepth24Plus,
	ShadowLayout: shadowMap.Layout(), // 或在 group 3 使用 CSMLayout / FogLayout
})

pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
pass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
gpuTerrain.Bind(pass) // TerrainMaterialGroup（group 2）
pass.SetBindGroup(render3d.ShadowGroup, shadowMap.BindGroup())
terrainMesh.Draw(pass)
```

- **纹理数组布局**：`GPUTerrainMaterial` 将各层反照率（sRGB）、切线空间法线以及打包的高度/粗糙度/金属度贴图装入三张 `gpu.Dim2DArray` 纹理数组（`TerrainAlbedoArrayBinding`、`TerrainNormalArrayBinding`、`TerrainMRHeightArrayBinding`），连同两张 RGBA 权重图一起绑定在 `TerrainMaterialGroup`（group 2）。片元着色器仅占用 5 个纹理槽位（加阴影为 6 个），在 WebGL 2 / GLES 3 上均留有充裕余量，且 1–8 层共享单一管线布局。
- **光照、阴影、雾效与 HDR**：`NewTerrainMaterialPipeline` 按选项编译 `TerrainMaterialWGSL`、`TerrainMaterialShadowWGSL`（`ShadowLayout`）、`TerrainMaterialCSMWGSL`（`CSMLayout`）或 `TerrainMaterialFogWGSL`（`FogLayout`），并支持配合 `HDRTarget` / `ToneMapPass` 的 `LinearOutput` 变体。
- **动态更新与 CPU 参考**：调用 `UpdateUniforms` 可原地更新变换矩阵、高度混合对比度、三平面参数或各层因子与平铺比例而不重建纹理；调用 `UpdateSplatMaps` 可原地上传重新绘制的权重图；`EvaluateTerrainMaterial` 提供与 WGSL 着色器一致的纯 Go CPU 参考求值器。
- **直接从 `*.terrain.yaml` 加载**：`TerrainMaterialFromDoc(fsys, doc)` 自动解码打包 RGBA `splatMaps` 或逐层灰度 `weightMap` PNG，解析可选的 `resource.Material` 材质资源及显式 PBR 覆盖项，并返回可直接交给 `NewGPUTerrainMaterial` 的已校验 `TerrainMaterial`；`MeshFromTerrainMesh` 可将 `world3d.TerrainMesh` 区块直接转换为 `render3d.Mesh`。
