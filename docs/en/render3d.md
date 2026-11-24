# render3d

`game/render3d` provides mesh buffers for the NSGo GPU view and the GPU-agnostic, zero-steady-state-allocation CPU 3D render pipeline and developer tools: frustum culling, opaque/transparent draw sorting, automatic instancing, LOD selection with hysteresis, camera-facing billboards and 3D ribbon trails, 3D debug wireframes and transform gizmos, frame profiling, and mobile/desktop GPU budgets.

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

1. **Frustum culling**: extracts six planes via `math3d.FrustumFromMat4NO` and skips off-screen `WorldBounds`. The `[-1, 1]` form is exact for OpenGL-style projections (`math3d.PerspectiveNO`, `math3d.OrthoNO`) and conservative for `[0, 1]` ones, so one queue serves both; `Queue.FrustumNO` is kept for source compatibility and ignored.
2. **Occlusion culling** (optional): when `Queue.Occluder` is set, frustum-visible items whose `WorldBounds` fail `OcclusionBuffer.TestAABB` are skipped and counted in `RenderStats.Occluded`. See [Occlusion culling](#occlusion-culling-occlusionbuffer).
3. **LOD selection with hysteresis**: evaluates each item's `LODs` (`[]LODLevel`, finest to coarsest) using distance and `ProjectScreenRatio`, remembering the prior LOD per `DrawItem.ID` (`SelectLOD`). Call `queue.ResetLODHistory()` on camera cuts.
4. **Opaque sorting & instancing**: orders `BlendOpaque` and `BlendAlphaMask` items by `(PipelineID, MaterialID, MeshID, Blend, front-to-back view depth)` to minimize state switches and early-Z overdraw, then groups adjacent matching items into `InstanceBatch`es (unless `DisableInstancing` is set).
5. **Transparent sorting**: places `BlendAlphaBlend` draws into `plan.Transparent`, ordered strictly back-to-front by view depth. Both sorts are stable, so draws whose keys tie keep their submission order from frame to frame.

## Occlusion culling (`OcclusionBuffer`)

`OcclusionBuffer` is a small CPU depth buffer with a max-Z pyramid. Rasterize large static occluders (walls, floors, buildings) into it once per view, then let the queue skip items hidden behind them. It never culls a visible item: a box that crosses the near plane, or one tested against a view other than the one rasterized, is always reported visible.

```go
occ := render3d.NewOcclusionBuffer(0, 0) // 128x72 by default
walls := []render3d.Occluder{
	render3d.NewBoxOccluder(wallBounds, wallTransform),
	render3d.NewQuadOccluder(a, b, c, d, true),
	render3d.OccluderFromMesh(vertices, indices, transform),
}

// Every frame, with the same viewProj the queue uses:
occ.Clear()
occ.Rasterize(viewProj, walls...)
occ.BuildPyramid()
plan := queue.BuildWithOcclusion(viewProj, camPos, drawItems, occ)
```

Set `OcclusionBuffer.DepthNO` for an OpenGL-style `[-1, 1]` projection so the near plane matches. Occluders must be opaque and closed or double-sided; only meshes that fully hide what is behind them belong here.

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

## Mesh buffers (`Mesh`, `GPUMesh`)

A `Mesh` is triangle-list geometry on the CPU: `Positions` (required), `Normals` and `UVs` (each empty or one per position) and `Indices` (empty for a non-indexed mesh). The fields have the same types as a glTF primitive's `Positions`, `Normals`, `TexCoords0` and `Indices`, and front faces wind counter-clockwise as in glTF and [math3d](math3d.md). `UploadMesh` validates the mesh and creates its vertex buffer and, when indexed, its index buffer on the [NSGo GPU view](https://github.com/nsgo-dev/nsgo/blob/main/docs/architecture/gpu.md)'s `gpu.Device`. The buffers belong to the scope you pass and are released when it is disposed or when you call `GPUMesh.Close`:

```go
gpu.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady: func(dev *gpu.Device) {
		if mesh == nil { // OnReady runs again after a device loss; NSGo has already restored the retained buffers
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
		mesh.Draw(pass) // or mesh.Bind(pass) and your own DrawIndexedInstanced
		pass.End()
		frame.Submit()
	},
})
```

Every mesh uses one interleaved little-endian layout of 32 bytes per vertex, so one pipeline draws any of them; a missing normal or UV is written as zeros:

| Attribute | `@location` | Format | Offset |
| --- | --- | --- | --- |
| position | 0 (`MeshPositionLocation`) | `float32x3` | 0 |
| normal | 1 (`MeshNormalLocation`) | `float32x3` | 12 |
| UV | 2 (`MeshUVLocation`) | `float32x2` | 24 |

Indices are written as `uint16` while the mesh has at most 65,535 vertices and as `uint32` beyond that, so the index 0xFFFF, which WebGL 2 always treats as a primitive restart, is never used in a `uint16` buffer; index data is zero-padded to a multiple of four bytes as WebGPU requires. Only formats that every GPU view backend accepts are used: Metal on macOS and iOS, Direct3D 11 on Windows, OpenGL 3.3 on Linux, WebGPU or WebGL 2 on the web and OpenGL ES 3.0 on Android. The package's tests draw meshes on the software device of headless `nsgotest`, with both the full capabilities and those of the WebGL 2 and OpenGL ES 3.0 backends (`nsgotest.NoGPUCompute`); drawing on each hardware backend is not yet covered by an automated test.

- `Mesh.Validate` rejects an empty mesh, attribute counts that do not match the positions, non-finite values, partial triangles and out-of-range indices; every error wraps `ErrInvalidMesh`, and `UploadMesh` creates nothing for an invalid mesh.
- `Mesh.AppendVertexData` and `Mesh.AppendIndexData` write the buffer bytes into a slice you own without allocating when it has capacity, for streaming geometry through `Device.WriteBuffer` or a ring buffer. They do not validate: call `Mesh.Validate` first, since the bytes written for an invalid mesh are unspecified.
- `MeshUpload.Retained` keeps a CPU copy so NSGo recreates the buffers after a device loss and the same `GPUMesh` draws again, so upload a retained mesh only once, as in the example; leave it off when `OnReady` uploads the meshes again.
- `GPUMesh.Draw` and `GPUMesh.Bind` record nothing for a mesh that is not `Valid` (nil after a failed upload, closed, its scope disposed, or not retained across a device loss), so it never draws with buffers an earlier mesh left bound.
- `GPUMesh.Bounds` is the object-space box to transform into `DrawItem.WorldBounds`, and `GPUMesh.ByteSize` the GPU memory for `RenderStats` and budgets.

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

Debug lines and gizmos clip at the near plane of a `[0, 1]` depth projection, which `math3d.Perspective` and `math3d.Ortho` produce. For an OpenGL-style `[-1, 1]` projection set `DebugDraw.DepthNO` and `Gizmo.DepthNO`, or call `ProjectSegmentNO`, so the near half of its depth range is not hidden. Each planar translate handle is drawn as the square that `PickAxis` tests. `DrawGizmo` allocates nothing; to read the segments yourself without allocating, reuse a slice with `gizmo.AppendSegments(buf[:0], objPos, gizmoScale, mode)`.

## Billboards and ribbon trails

A `Billboard` is a quad that turns toward the camera, and a `Trail` is a ribbon through a moving object's recent positions that twists about its centerline to face the viewer. Both take a `CameraBasis`, the camera's position and unit axes, built from matrices or filled from a `scene3d.Camera`:

```go
basis, _ := render3d.NewCameraBasis(cam.ViewMatrix(), cam.ProjMatrix())
// or: render3d.CameraBasis{Pos: cam.Pos, Right: cam.Right(), Up: cam.Up(), Forward: cam.Forward(), Orthographic: cam.Projection.Kind == scene3d.ProjectionOrthographic}
```

`Billboard.Mode` picks how the quad turns:

| Mode | Faces | Use for |
| --- | --- | --- |
| `BillboardViewPlane` (default) | the view plane, along the camera's right and up | particles, sprites, impostors |
| `BillboardViewPoint` | the camera's position, rolled by the camera's up | large quads near the screen edge |
| `BillboardAxial` | the camera, rotating only about `Axis` (+Y when zero) | trees, grass, flames, beams |

The view-plane and view-point modes face the camera at every angle, including looking straight up and down; an axial quad faces it as far as its axis allows and turns edge-on when the camera looks along the axis. `Pivot` places the quad relative to `Center` (`{Y: -0.5}` stands it on its bottom edge) and `Rotation` rolls it counter-clockwise on screen.

```go
smoke := render3d.Billboard{Center: pos, Width: 1, Height: 1, Rotation: spin, Color: core.RGBA(200, 200, 200, 0.6)}
tree := render3d.Billboard{Center: base, Width: 2, Height: 4, Pivot: math3d.Vec2{Y: -0.5}, Mode: render3d.BillboardAxial, Texture: treeImage}

quad, ok := smoke.Quad(basis)                                   // corners BL, BR, TR, TL, counter-clockwise toward the camera
item := quad.DrawItem(render3d.DrawItem{ID: 7, MeshID: unitQuad}) // Transform maps the unit quad; Blend defaults to BlendAlphaBlend
```

Until GPU submission lands, `BillboardRenderer` fills billboards into a `*core.Drawing` back to front, clipping them against the near plane. Textured quads are mapped with an affine transform, which is exact under an orthographic camera and close for quads that are small on screen; a textured quad that crosses the near plane is skipped.

```go
var billboards render3d.BillboardRenderer // reuse across frames
billboards.Draw(d, []render3d.Billboard{smoke, tree}, basis, cam.ViewProj(), viewportBounds)
```

`Trail` mirrors the point-recording, lifetime, width and color options of `particles.Trail` in world units and reuses its `particles.Curve` and `particles.ColorCurve` for width and color from head (`u = 0`) to tail (`u = 1`). It has no `Texture` and no `Attach`, `Detach` or `Follow`: call `AddPoint` with the object's position each frame. Each point's edge runs along `tangent × toCamera`, and a trail heading straight at the camera keeps its previous edge direction instead of collapsing:

```go
sword := render3d.NewTrail(render3d.TrailConfig{
	Width:         0.3,
	Lifetime:      250 * time.Millisecond,
	WidthCurve:    particles.Curve{{T: 0, Value: 1}, {T: 1, Value: 0}},
	ColorGradient: particles.ColorCurve{{T: 0, Color: core.RGB(0xffe080)}, {T: 1, Color: core.RGBA(255, 80, 0, 0)}},
})

// In Update:
sword.AddPoint(bladeTip)
sword.Update(dt)

// In Draw, or hand Build's triangle strip (two vertices per point, tail to head) to a GPU:
verts := sword.Build(basis)
sword.Draw(d, basis, cam.ViewProj(), viewportBounds)
```

`Build` reuses its storage and does not allocate; `Bounds` and `DrawItem` give the transparent queue a camera-independent box for sorting.

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

## Hot-reloading shaders and materials

Run the game with [`go tool game dev`](cli.md#dev) and attach `hotreload.ShaderReloader` and `hotreload.MaterialReloader` (from `github.com/nsgo-dev/game/packages/game/tooling/hotreload`) to `hotreload.NewDevWatcher` so edited WGSL (`.wgsl`), Kage (`.kage`), and material YAML (`.material.yaml`, `.mat.yaml`, `.material.yml`, `.mat.yml`) files reload within a second without restarting:

```go
w := hotreload.NewDevWatcher(cx) // nil outside game dev and in release builds
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
w.WatchMaterials(materials) // watches both material YAML and referenced .wgsl/.kage shaders

_, _, err := materials.Register("crystal", "materials/crystal.material.yaml", buildPipeline)
```

Every reload compiles a candidate `*gpu.ShaderModule` and builds a candidate `*gpu.Pipeline` before swapping:

- **Safe fallback on errors**: if a shader edit fails to compile, misses an entry point, or fails pipeline creation — or if a material YAML has invalid syntax, an unknown field, or an out-of-range value — the previous working shader, material, and `*gpu.Pipeline` remain active without crashing. The failure is recorded as a `ReloadError` (unwrapping to `*loaderr.Error` with file, line, and column when reported) accessible via `LastError` and `Errors`, and clears automatically once a subsequent save compiles cleanly.
- **Shader-to-material dependencies**: editing a shader referenced by one or more materials automatically rebuilds and swaps every dependent material's pipeline, closing replaced pipelines cleanly.
- **Text material format (`MaterialDef`)**: a versioned YAML document (`version: 1`, refusing unknown fields and round-tripping deterministically via `ParseMaterial` / `EncodeMaterial`) declaring `shader` (or split `vertexShader` / `fragmentShader`), entry points (`vertexEntry`, `fragmentEntry`), GPU render state (`blend`, `cull`, `frontFace`, `depthWrite`, `depthCompare`, applied via `ApplyToPipelineDesc`), and typed uniform parameters (`scalars`, `vec2`, `vec3`, `vec4`, `colors`, `textures`). For shared 2D/PBR `resource.Material` files (`type: material`), use `w.WatchResources` ([resource](resources.md#reloading-on-edit)).
- **Direct shader watching**: for standalone pipelines without material YAML, use `ShaderReloader.RegisterPipeline` with `w.WatchShaders(shaders)`, or `w.WatchShader(relPath, onReload)` for a single `.wgsl` or `.kage` file.
- **Release builds**: under `-tags release`, `NewDevWatcher` returns nil and `WatchShader`, `WatchShaders`, and `WatchMaterials` compile to no-ops while `ShaderReloader` and `MaterialReloader` continue serving initial loads.
