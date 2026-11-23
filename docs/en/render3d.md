# render3d

`game/render3d` provides the GPU-agnostic, zero-steady-state-allocation CPU 3D render pipeline and developer tools: frustum culling, opaque/transparent draw sorting, automatic instancing, LOD selection with hysteresis, 3D debug wireframes and transform gizmos, frame profiling, and mobile/desktop GPU budgets.

```go
import "github.com/nsgo-dev/game/packages/game/render3d"
```

All matrices, bounding boxes, rays, and frustums follow [math3d](math3d.md) conventions.

## Frustum culling, draw sorting, instancing, and LOD (`Queue`)

Reuse a `Queue` across frames and pass your frame's `[]DrawItem` into `Queue.Build(viewProj, camPos, items)`:

```go
queue := &render3d.Queue{
	LODHysteresis: 0.1, // 10% deadband prevents LOD popping at thresholds
}

plan := queue.Build(cam.ViewProj(), cam.Pos, drawItems)

// 1. Submit instanced opaque batches (contiguous Mat4 transform slices):
for _, batch := range plan.Batches {
	submitInstanced(batch.PipelineID, batch.MaterialID, batch.MeshID, batch.Transforms)
}
// 2. Submit transparent draws strictly back-to-front:
for _, draw := range plan.Transparent {
	submitDraw(draw)
}
```

In one allocation-free steady-state pass, `Queue.Build` performs:

1. **Frustum culling**: extracts six planes via `math3d.FrustumFromMat4` (or `FrustumFromMat4NO` when `Queue.FrustumNO` is `true`) and skips off-screen `WorldBounds`.
2. **LOD selection with hysteresis**: evaluates each item's `LODs` (`[]LODLevel`, finest to coarsest) using distance and `ProjectScreenRatio`, remembering the prior LOD per `DrawItem.ID` (`SelectLOD`). Call `queue.ResetLODHistory()` on camera cuts.
3. **Opaque sorting & instancing**: orders `BlendOpaque` and `BlendAlphaMask` items by `(PipelineID, MaterialID, MeshID, Blend, front-to-back distance)` to minimize state switches and early-Z overdraw, then groups adjacent matching items into `InstanceBatch`es (unless `DisableInstancing` is set).
4. **Transparent sorting**: places `BlendAlphaBlend` draws into `plan.Transparent`, ordered strictly back-to-front.

## Camera uniform on the GPU (`CameraBuffer`)

`CameraBuffer` keeps one camera's uniform buffer on an NSGo `gpu.Device`, with the bind group layout and bind group that expose it to vertex and fragment shaders at `CameraGroup` (group 0). `CameraWGSL` declares the matching block, so a 3D shader prepends it and places vertices with `camera.view_proj`:

```wgsl
struct Camera {
	view: mat4x4<f32>,
	proj: mat4x4<f32>,
	view_proj: mat4x4<f32>,
	eye: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: Camera;
```

`CameraUniform` is the block's data (`NewCameraUniform(cam)` fills it from a `scene3d.Camera`) and `Encode` writes its `CameraUniformSize` (208) bytes without allocating. Create the buffer once the device is ready, upload the frame's camera before recording draws, and list `Layout()` at `CameraGroup` in every 3D pipeline's `Layouts`:

```go
camBuf, err := render3d.NewCameraBuffer(cx, dev)
// ...
layouts := []*gpu.BindGroupLayout{render3d.CameraGroup: camBuf.Layout()}
pipe, err := dev.CreatePipeline(cx, gpu.PipelineDesc{Layouts: layouts /* shaders, vertex buffers, depth */})

// Each frame, in game.Loop.Draw:
camBuf.WriteCamera(&drawn)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
```

The reference tests render the same scene headless through `gpu.NewSoftwareRenderer` with a perspective and an orthographic camera orbiting on `game.Loop`, and compare each frame with `render3d/testdata/golden/camera_perspective.png` and `camera_orthographic.png`; `UPDATE_GOLDEN=1` rewrites them.

## 3D debug draw and transform gizmos

`DebugDraw` projects 3D wireframes with near-plane clipping directly into an NSGo `*core.Drawing` overlay:

```go
var dbg render3d.DebugDraw
dbg.Grid(math3d.Vec3Zero(), 10, 20, core.RGBA(120, 130, 150, 0.4))
dbg.AABB(enemyBounds, core.RGB(255, 80, 80))
dbg.Sphere(alertCenter, 4, core.RGB(255, 200, 60), 24)
dbg.Axes(selectedPos, 1.5)
dbg.Flush(d, cam.ViewProj(), viewportBounds)
dbg.Clear()
```

For level editors and inspectors, `Gizmo` handles translate, rotate, and scale axes (`GizmoTranslate`, `GizmoRotate`, `GizmoScale`):

```go
var gizmo render3d.Gizmo
axis, _ := gizmo.PickAxis(ray, objPos, gizmoScale, render3d.GizmoTranslate)
if axis != render3d.AxisNone {
	delta := gizmo.DragTranslate(objPos, axis, startRay, currentRay, 0.25) // 0.25m snap
	objPos = objPos.Add(delta)
}
gizmo.DrawGizmo(d, cam.ViewProj(), viewportBounds, objPos, gizmoScale, render3d.GizmoTranslate, axis)
```

## Render stats, profiler, and GPU budgets

`plan.Stats` (`RenderStats`) reports exact culled/visible counts, draw calls, instanced batches, triangles, vertices, material/pipeline switches, and GPU memory. Track rolling frame timing with `NewFrameProfiler(120)` (`Record`, `AverageFrameTime`, `P95FrameTime`, `FPS`, `PeakDrawCalls`, `PeakTriangles`) and assert budgets in dev builds or tests with `CheckBudget`:

| Preset | MaxDrawCalls | MaxTriangles | MaxVertices | MaxMaterialSwitches | MaxPipelineSwitches | MaxTextureBytes | MaxBufferBytes |
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
