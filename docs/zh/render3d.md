# render3d

`game/render3d` 提供 NSGo GPU 视图的网格缓冲、纹理与采样器、金属度-粗糙度 PBR 材质以及相机 uniform，以及与后端无关、稳态零分配的 CPU 侧 3D 渲染管线与开发工具：视锥体裁剪、不透明/半透明绘制排序、自动实例化合批、带滞回防抖动的 LOD 选择、面向相机的公告板与 3D 条带拖尾、3D 调试线框与变换手柄（Gizmo）、帧性能分析器，以及移动端/桌面端 GPU 预算检查。

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
- **KTX2 纹理**：`LoadKTX2` 上传 `game assets` 和 `game tex` 写出的那种 2D KTX2 文件，包括它含有的每一级 mip，保持其 sRGB 或线性格式，并返回绘制它所用的采样器：即 `KTX2Upload.Sampler`，其过滤方式按文件的 `game.filter` 键（`KTX2FilterKey`）设置，这个键由图片[导入设置附属文件](cli.md#资源旁的导入设置)的 `filter` 写入，因此 `nearest` 让像素画清晰锐利，`linear` 则做混合。BC1、BC3 和 ETC2 层级在 `Device.SupportsCompressedFormat` 允许时以压缩格式上传，否则在 CPU 上解码为 RGBA8，因此在所有后端都能加载；`KTX2Sampler` 无需设备即可把过滤方式应用到采样器选项上。不是这类纹理的文件或未知的过滤方式会返回包装 `ErrInvalidTexture` 的错误：

  ```go
  data, err := fs.ReadFile(assets, "albedo.ktx2")
  albedo, opts, err := render3d.LoadKTX2(cx, dev, data, render3d.KTX2Upload{Label: "albedo", Retained: true, Sampler: render3d.SamplerLinearRepeat()})
  samp, err := render3d.NewGPUSampler(cx, dev, opts)
  ```

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
- **Khronos 参考验证**：`render3d` 的测试将 Khronos `DamagedHelmet` 样本渲染并与黄金参考图（`render3d/testdata/golden/damaged_helmet_reference.png`）比对，同时在 `gpu.View` 上验证完整能力与 `nsgotest.NoGPUCompute`（WebGL 2 / GLES 3.0）配置的像素一致性，以及 `Retained` 设备丢失恢复。

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
