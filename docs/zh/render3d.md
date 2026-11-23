# render3d

`game/render3d` 提供 NSGo GPU 视图的网格缓冲，以及与后端无关、稳态零分配的 CPU 侧 3D 渲染管线与开发工具：视锥体裁剪、不透明/半透明绘制排序、自动实例化合批、带滞回防抖动的 LOD 选择、3D 调试线框与变换手柄（Gizmo）、帧性能分析器，以及移动端/桌面端 GPU 预算检查。

```go
import "github.com/nsgo-dev/game/packages/game/render3d"
```

所有矩阵、包围盒、射线与视锥体均遵循 [math3d](math3d.md) 约定。

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

1. **视锥裁剪**：通过 `math3d.FrustumFromMat4`（开启 `Queue.FrustumNO` 时用 `FrustumFromMat4NO`）提取六平面并剔除视锥外的 `WorldBounds`。
2. **带滞回的 LOD 选择**：结合相机距离与 `ProjectScreenRatio` 评估每个物体的 `LODs`（`[]LODLevel`，由精细到粗糙），按 `DrawItem.ID` 记忆上一帧级别（`SelectLOD`）。镜头硬切时调用 `queue.ResetLODHistory()`。
3. **不透明排序与实例化**：将 `BlendOpaque` 与 `BlendAlphaMask` 按 `(PipelineID, MaterialID, MeshID, Blend, 由近及远距离)` 排序以减少状态切换与早期深度过绘，并将相邻同材质同网格项合并为 `InstanceBatch`（除非设置 `DisableInstancing`）。
4. **半透明排序**：将 `BlendAlphaBlend` 项放入 `plan.Transparent`，严格按由远及近排序。

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
