# render3d

`game/render3d` provides mesh buffers, textures and samplers, metallic-roughness PBR materials, camera uniforms, distance and height fog, and an HDR render target with a tone-mapping pass for the NSGo GPU view alongside the GPU-agnostic, zero-steady-state-allocation CPU 3D render pipeline and developer tools: frustum culling, opaque/transparent draw sorting, automatic instancing, LOD selection with hysteresis, camera-facing billboards and 3D ribbon trails, 3D debug wireframes and transform gizmos, frame profiling, and mobile/desktop GPU budgets.

```go
import "github.com/nsgo-dev/game/packages/game/render3d"
```

All matrices, bounding boxes, rays, and frustums follow [math3d](math3d.md) conventions. To composite 2D sprite batches and screen-space layers over a 3D `gpu.View`, see [`Overlay3D` in `game/scene`](scene.md#2d-layers-over-a-3d-view-overlay3d).

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

## Directional and point lights in PBR shading (`LightRig`, `GPULightBuffer`)

`LightRig` holds a scene's linear-RGB lighting: an `AmbientLight`, up to `MaxDirectionalLights` (4) `DirectionalLight`s, and up to `MaxPointLights` (16) `PointLight`s.

- **`DirectionalLight`**: parallel rays travelling along `Direction` (for example `{Y: -1}` shines straight down onto a `+Y` surface; normalized when shaded or packed), with linear `Color` and `Intensity`.
- **`PointLight`**: omnidirectional emitter at `Position` with linear `Color`, `Intensity`, and optional cutoff `Range`. When `Range > 0`, distance falloff uses the glTF `KHR_lights_punctual` smooth windowing curve `max(min(1 - (d/Range)^4, 1), 0)^2 / max(d^2, 1e-4)` so contribution reaches zero smoothly at `Range`; `Range == 0` uses pure clamped inverse-square `1 / max(d^2, 1e-4)` (`PointLightAttenuation`).
- **`AmbientLight`**: uniform isotropic linear fill applied as `Ambient.Color * Ambient.Intensity * BaseColor`.

`GPULightBuffer` uploads a validated `LightRig` into a 672-byte (`LightUniformSize`), 16-byte-aligned std140/WGSL uniform buffer bound at `LightGroup` (group 1), scoped to `*core.Scope` with optional `LightBufferOptions.Retained` survival across GPU device loss:

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

// Each frame:
_ = lightBuf.Update(frame.Device(), rig)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)
```

Custom 3D shaders can prepend `CameraWGSL + LightWGSL` to read `@group(1) @binding(0) var<uniform> lights: LightRigUniform` and call the portable WGSL helpers `point_light_attenuation`, `pbr_brdf`, `shade_pbr`, and `linear_to_srgb`. `LitPBRWGSL` combines them with `MeshVertexLayout` and a per-draw `PBRDrawUniform` at `PBRDrawGroup` (group 2).

The shading model follows glTF 2.0 metallic-roughness PBR (dielectric `F0 = 0.04`, Schlick Fresnel, Trowbridge-Reitz GGX normal distribution, height-correlated Smith-GGX visibility, energy-conserving Lambertian diffuse, perceptual roughness floored at `0.045`, and IEC 61966-2-1 linear-to-sRGB output). `EvaluatePBR`, `ShadePoint`, `PointLightAttenuation`, and `LinearToSRGB` implement the exact same equations on the CPU; `LightRig.AppendUniformData` and `LightUniforms.Encode` pack the uniform block without allocating. Reference tests verify GPU output pixel-for-pixel against CPU `ShadePoint`, exercise both full and WebGL 2 / GLES 3.0 (`nsgotest.NoGPUCompute`) profiles plus `Retained` device loss, and compare lit scenes against `render3d/testdata/golden/lights_pbr_scene.png` and `lights_point_falloff.png`.

## Distance and height fog (`Fog`, `GPUFogBuffer`)

`Fog` configures distance fog (`FogOff`, `FogLinear`, `FogExp`, `FogExp2`), analytical exponential height fog along the view ray (`HeightEnabled`, `HeightBase`, `HeightFalloff`, `HeightDensity`), or both. `Color` is linear RGB (`math3d.Vec3`) and `MaxOpacity` caps the final blend factor in `(0, 1]` (zero means `1.0`). With both kinds enabled, their transmittances multiply:

```text
factor = clamp(1 - (1 - f_dist) * (1 - f_height), 0, MaxOpacity)
```

Height fog integrates the vertical extinction `HeightDensity * exp(-HeightFalloff * (y - HeightBase))` analytically along the segment from the ray's start to the surface, switching to a four-term Taylor expansion near horizontal rays, so grazing and vertical rays stay smooth and a high camera looking down never overflows.

`GPUFogBuffer` holds the 48-byte `FogUniforms` block (`FogUniformSize`) with optional `Retained` survival across device loss. Both lit pipelines take it:

- **Lights**: `NewLitPBRFogPipeline` compiles `LitPBRFogWGSL`, the `LitPBRWGSL` lighting above with the fog buffer at `FogGroup` (group 3) after the camera, lights and per-draw groups. `GPUFogBuffer.Bind` binds it there.
- **Materials**: set `MaterialPipelineOptions.FogLayout` and `NewMaterialPipeline` compiles `MaterialFogWGSL` with the fog buffer at `MaterialFogGroup` (group 2), after the camera and material groups. Fog blends the linear radiance before the material shader's tone mapping, so the fog color is tone-mapped with the scene.

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

// Each frame:
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

The view ray starts at the eye for a perspective camera. For an orthographic camera it starts on the camera's plane, at the point the surface projects from, so fog grows with the distance from that plane rather than from the camera's position. `FogCameraWGSL`'s `fog_camera_pos(world_pos)` computes that start point in shaders, and `FogCameraPos(cameraUniform, worldPos)` computes the same point on the CPU.

- **CPU reference**: `Fog.Factor(camPos, worldPos)` and `Fog.Apply(color, camPos, worldPos)` evaluate the shader's formulas, with `camPos` the ray's start from `FogCameraPos`. `Fog.Validate` rejects non-finite or out-of-range fields with `ErrInvalidFog`, and `Fog.Sanitized` replaces them with safe defaults. `AppendFogUniforms` packs the block without allocating.
- **Custom shaders**: prepend `FogWGSL` for the `Fog` struct, `fog_factor(cam_pos, world_pos, fog)` and `apply_fog(color, cam_pos, world_pos, fog)`, then `FogBindingWGSL` (group 3) or `MaterialFogBindingWGSL` (group 2) for the binding, and `FogCameraWGSL` after `CameraWGSL` for the ray start. `FogWGSL` declares no binding of its own, so a shader can also embed `Fog` in its own uniform block.

Reference tests check rendered pixels against `ShadePoint` and `EvaluateMaterial` blended by `Fog.Apply` on the software GPU and in `nsgotest` (`full` and `nsgotest.NoGPUCompute` profiles), cover orthographic cameras, `Update`, `Retained` device loss and scope disposal, and compare against `render3d/testdata/golden/fog_linear_distances.png` and `fog_height.png`.

## Skybox and image-based lighting (`Cubemap`, `SkyboxPipeline`, `GPUIBLBuffer`)

`render3d` provides 6-face cubemaps (`Cubemap`, `GPUCubemap`), a fullscreen-triangle background skybox (`SkyboxPipeline`), and split-sum image-based lighting (`IrradianceSH`, `PrefilterEnvironment`, `GenerateBRDFLUT`, `EnvBRDFApprox`, `GPUIBLBuffer`, `IBLWGSL`, `LitPBRIBLWGSL`) that run on every NSGo GPU backend, including WebGL 2 and OpenGL ES 3.0 (`NoGPUCompute`).

Build a `Cubemap` from six faces (`CubemapFromImages`), an equirectangular panorama (`CubemapFromEquirectangular`; pass `srgb = true` for a typical PNG or JPEG), or a procedural three-band sky (`GradientSkyCubemap`), then bake diffuse L2 spherical harmonic irradiance and a GGX-prefiltered specular mip chain on the CPU before uploading:

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

// Each frame: draw opaque lit meshes with the IBL buffer at IBLGroup (group 3), then the skybox, which skips the pixels they cover:
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
iblBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)
skyPipe.WriteCamera(cam)
skyPipe.Draw(pass, skyGroup)
```

- **Depth and translation**: `NewSkyboxUniform` strips camera translation so the sky stays infinitely distant as the viewer moves. `SkyboxWGSL` draws one fullscreen triangle from `@builtin(vertex_index)` with no vertex buffer, at depth `1 - 2^-24` (the largest float32 below 1) with `CompareLessEqual` and `DepthWrite: false`, so drawn after opaque geometry it shades only uncovered pixels and paints over nothing nearer than the far plane's last 24-bit depth step. It does not emit exactly `1.0` because NSGo's software rasterizer drops fragments whose interpolated depth rounds above 1.
- **Lit PBR with environment lighting**: `LitPBRIBLWGSL` is `LitPBRWGSL` plus `evaluate_ibl`: it adds diffuse spherical harmonic irradiance and prefiltered specular to the direct lighting before encoding to sRGB. Set the `LightRig`'s ambient light to zero when the environment supplies the ambient term. IBL and fog both use bind group 3 (`IBLGroup` and `FogGroup`), the last of the four groups every backend guarantees, so a lit pipeline has one or the other.
- **Custom shaders**: prepend `IBLWGSL` (group 3) or `IBLWGSLForGroup(group)` and call `evaluate_ibl(normal, view_dir, base_color, metallic, roughness, ao)`. Diffuse lighting evaluates the 9 pre-convolved L2 coefficients (`evaluate_irradiance_sh`), and specular lighting samples the prefiltered cubemap at `roughness * ibl.max_lod` scaled by Karis's analytical `env_brdf_approx` (`EnvBRDFApprox`). Shaders that prefer a 2D lookup texture can bake the split-sum GGX table with `GenerateBRDFLUT` or `GenerateBRDFLUTWithSamples`.
- **Buffer, validation and memory**: `GPUIBLBuffer` follows `GPULightBuffer` and `GPUFogBuffer`: `Update(dev, u)` writes new parameters, `Bind` sets it at `IBLGroup`, and `ByteSize` reports its uniform buffer for `RenderStats`, while `GPUCubemap.ByteSize` reports each cubemap. `IBLUniform.Validate` refuses non-finite spherical harmonic coefficients and non-finite or negative intensity or max LOD with `ErrInvalidIBL`.
- **Low dynamic range only**: cubemaps are 8 bits per channel (`RGBA8Unorm` or `RGBA8UnormSRGB`), so radiance above 1 is clamped in the source, prefiltered and irradiance faces. Bright sun or sky highlights flatten; high dynamic range environments are not supported yet.
- **CPU reference and device loss**: `EvaluateIBL` matches `evaluate_ibl` on the CPU, and `ShadePoint` plus `EvaluateIBL` matches `LitPBRIBLWGSL`. With `Retained: true` on `CubemapUpload`, `SkyboxPipelineDesc` and `IBLBufferOptions`, NSGo restores textures, samplers, pipelines and uniform buffers after a device loss; call `SkyboxPipeline.SetDevice(dev)` in `OnReady` and pass the new device to `GPUIBLBuffer.Update`.
- **Reference renders**: headless software-GPU tests compare a skybox behind a mesh and 2x2 metallic/roughness spheres with `render3d/testdata/golden/skybox_scene.png` and `ibl_spheres.png` (`UPDATE_GOLDEN=1` rewrites them), check `LitPBRIBLWGSL` against the CPU reference, check that geometry just inside the far plane stays visible, and run on both the full and `nsgotest.NoGPUCompute` profiles.

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

## Textures and samplers (`GPUTexture`, `GPUSampler`)

`UploadTexture` uploads any Go `image.Image` (including non-zero-origin sub-images) to a `gpu.Device` as a scope-owned `GPUTexture`, converting it to straight-alpha RGBA8 (`PixelBufferFromImage`; `*image.NRGBA` bytes are copied verbatim even when alpha is zero so packed data maps stay intact). `UploadTexturePixels` uploads raw `PixelBuffer` bytes in `FormatRGBA8Unorm`, `FormatRGBA8UnormSRGB`, `FormatRG8Unorm`, or `FormatR8Unorm`, with optional `Stride` row padding:

```go
texLayout, err := render3d.TextureBindingLayout(cx, dev)
albedo, err := render3d.UploadTexture(cx, dev, img, render3d.TextureUpload{
	Label:    "crate-albedo",
	SRGB:     true, // color maps decode sRGB to linear on sample; leave false for normal/ORM data maps
	Mipmaps:  true, // CPU area-weighted box chain (in linear light for sRGB) uploaded per level
	Retained: true, // survives device loss alongside retained samplers
})
samp, err := render3d.NewGPUSampler(cx, dev, render3d.SamplerLinearRepeat())
bg, err := render3d.BindTexture(cx, dev, texLayout, albedo, samp)

// In OnFrame, alongside CameraGroup (group 0):
pass.SetBindGroup(1, bg)
mesh.Draw(pass)
```

- **Mipmaps**: setting `TextureUpload.Mipmaps` builds a CPU area-weighted box mipmap chain (in linear light when `SRGB` is true, avoiding dark halos) and uploads every level via `Device.WriteTextureMip`. This works identically on every NSGo backend without depending on GPU mipmap generation, and every uploaded level is replayed automatically when `Retained` is true. Cap the level count with `TextureUpload.MaxMipLevels`.
- **KTX2 textures**: `LoadKTX2` uploads a 2D KTX2 file such as `game assets` and `game tex` write, with every mip level it holds, in its sRGB or linear format, and returns the sampler to draw it with: `KTX2Upload.Sampler` with its filters set from the file's `game.filter` key (`KTX2FilterKey`), which the `filter` of the image's [import settings sidecar](cli.md#import-settings-beside-each-asset) writes, so `nearest` draws pixel art sharp and `linear` blends. Zstandard and ZLIB supercompressed levels are decompressed first. BC1, BC3, BC7, ETC2 RGB8 and RGBA8, and ASTC 4x4 levels are uploaded compressed where `Device.SupportsCompressedFormat` allows and decoded to RGBA8 on the CPU elsewhere, so the texture loads on every backend; `KTX2Sampler` applies the filter to sampler options without a device. A file that is not such a texture, or names an unknown filter, returns an error wrapping `ErrInvalidTexture`:

  ```go
  data, err := fs.ReadFile(assets, "albedo.ktx2")
  albedo, opts, err := render3d.LoadKTX2(cx, dev, data, render3d.KTX2Upload{Label: "albedo", Retained: true, Sampler: render3d.SamplerLinearRepeat()})
  samp, err := render3d.NewGPUSampler(cx, dev, opts)
  ```

- **Basis Universal UASTC**: `LoadKTX2` also takes a UASTC 4x4 KTX2 file, the high-quality Basis Universal mode, and transcodes it to the best block format the device supports: BC7, then BC1 for an opaque texture, BC3, ETC2 RGB8 or RGBA8 (linear only, as NSGo has no sRGB ETC2), then ASTC 4x4. It decodes to RGBA8 when the device has none of them, when the base level's width or height is not a multiple of 4, or when `KTX2Upload.ForceRGBA8` is set. Transcoding decodes every block and encodes it again on the goroutine that calls `LoadKTX2`: on a desktop CPU (Ryzen 9 3900X) a 1024×1024 level takes about 0.5 s to BC7, 0.8 s to ETC2 and 0.16 s to ASTC, and four times as long at 2048×2048 (`BenchmarkTranscodeUASTC` in `tooling/ktx2`), so load large UASTC textures during a loading screen. BasisLZ/ETC1S files, `basisu`'s default mode, are refused, and so is BC1 without alpha (`VK_FORMAT_BC1_RGB_*`), whose punch-through texels an RGBA BC1 upload would draw transparent. The CPU ASTC decoder handles only the single-partition blocks `tooling/ktx2` writes, so an ASTC file from another encoder loads only on a device with ASTC.

- **Dynamic updates**: `GPUTexture.UpdateImage` and `GPUTexture.UpdatePixels` replace texels in place for a matching dimension and format, regenerating and re-uploading the mip chain when the texture was created with mipmaps.
- **Sampler presets and validation**: `SamplerLinearRepeat()`, `SamplerLinearClamp()`, `SamplerLinearMirror()`, `SamplerNearestRepeat()`, and `SamplerNearestClamp()` return customizable `SamplerOptions`. `SamplerOptions.Validate` rejects invalid filter/address modes, non-finite or inverted LOD bounds, and `MaxAnisotropy > 1` with non-linear filters (matching WebGPU's validation rule) with errors wrapping `ErrInvalidSampler`.
- **Lifecycle and memory**: `GPUTexture.ByteSize` sums every uploaded mip level for `RenderStats.TextureBytes` and `Budget` checks. Both `GPUTexture` and `GPUSampler` are released when their owning `core.Scope` is disposed or earlier via `Close()`, and `Valid()` reports whether they belong to a live device.
- **Reference renders**: `render3d`'s tests verify textured meshes, filtering, wrapping, sRGB/linear sampling, and mipmaps against golden reference PNGs (`render3d/testdata/golden/textured_mesh_scene.png` and `textured_filtering_wrapping.png`) as well as headless `gpu.View` tests on both the full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles; `UPDATE_GOLDEN=1` rewrites the reference images.

### Morph target meshes (`MorphMesh`, `GPUMorphMesh`)

`MorphMesh` pairs a base `Mesh` with per-vertex `MorphTarget` position and normal deltas (matching `gltf.MorphTarget`) and optional initial `Weights`. `UploadMorphMesh` validates the base mesh, targets, and weights (wrapping `ErrInvalidMesh` on malformed data) and creates a `GPUMorphMesh` backed by the same 32-byte `MeshVertexLayout` vertex buffer and optional index buffer as `GPUMesh`, so any existing 3D pipeline draws it without shader changes on every backend—including WebGL 2 and OpenGL ES 3.0 without compute shaders:

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

// Each frame before recording draws:
if sampled, ok := gltf.SampleMorphWeights(anim, nodeIdx, animTime, weights); ok {
	weights = sampled
}
_ = morph.SetWeights(weights) // marks dirty only when a weight actually changes
morph.Update(dev)             // blends on CPU and calls dev.WriteBuffer only when dirty
morph.Draw(pass)
```

- **Dirty tracking**: `SetWeights` compares against active weights and marks the mesh dirty only when at least one weight changes; `Update` skips both CPU blending and `Device.WriteBuffer` when clean.
- **Live object-space bounds**: `GPUMorphMesh.Bounds` evaluates pending CPU blends on demand (caching the result so `Update` does not blend twice) and keeps the underlying `GPUMesh.Bounds` in sync for frustum culling.
- **Device-loss restore**: with `MeshUpload.Retained`, NSGo recreates the vertex buffer with the last uploaded morphed bytes after a device loss; if `SetWeights` is called while the device is lost, the mesh stays `Dirty` so the first `Update` after restore uploads the pending pose.

## Metallic-roughness materials (`Material`, `MaterialDefaults`, `GPUMaterial`)

`Material` (aliased as `MetallicRoughnessMaterial`) represents a glTF 2.0 metallic-roughness PBR surface with base color (`BaseColorFactor`, `BaseColorTexture`), metallic-roughness (`MetallicFactor`, `RoughnessFactor`, `MetallicRoughnessTexture` using the glTF G = roughness, B = metallic channel packing), tangent-space normal mapping (`NormalTexture`, `NormalScale`), ambient occlusion (`OcclusionTexture`, `OcclusionStrength`), emissive (`EmissiveFactor`, `EmissiveTexture`), alpha modes (`AlphaOpaque`, `AlphaMask` with `AlphaCutoff`, `AlphaBlend`), and `DoubleSided` back-face normal flipping.

Create one shared `MaterialDefaults` per `gpu.Device` to own the `MaterialGroup` (group 1) bind group layout, default linear-repeat sampler, and 1×1 fallback textures (white sRGB for missing base color, white linear for missing metallic-roughness/occlusion, flat `(128, 128, 255, 255)` for missing normal maps, and black for missing emissive maps). Because every `GPUMaterial` binds those fallbacks for omitted slots, all materials share one bind group layout with zero pipeline permutations:

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
mat.MetallicRoughnessTexture = mrTex // G = roughness, B = metallic
mat.NormalTexture = normalTex        // tangent-space normal map (+X right, +Y up, +Z out)

gpuMat, err := render3d.NewGPUMaterial(cx, dev, defaults, mat, render3d.MaterialUpload{
	Label:    "brushed-gold",
	Retained: true,
})

// In OnFrame, alongside CameraGroup (group 0):
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
gpuMat.Bind(pass) // binds at MaterialGroup (group 1)
mesh.Draw(pass)
```

- **Zero-allocation uniform encoding and dynamic updates**: `MaterialUniforms.AppendUniformData` and `Encode` write the 16-byte-aligned 64-byte uniform block (`MaterialUniformSize`) with zero allocations when capacity is available. `GPUMaterial.Update(dev, m)` updates scalar factors and flags in place without recreating the bind group; passing `dev` explicitly lets retained materials stay updatable after a device loss and restore.
- **Portable normal mapping and Cook-Torrance GGX shading**: `MaterialWGSL` (built from `CameraWGSL` + `MaterialBindingsWGSL`) derives an API-independent tangent frame from screen-space `dpdx`/`dpdy` derivatives (Cramer's rule across Metal, D3D11, OpenGL, WebGPU, WebGL 2, and GLES 3.0) and falls back to a deterministic orthonormal frame when derivatives or UVs are degenerate, so standard 32-byte `MeshVertexLayout` meshes get accurate normal mapping without extra vertex attributes.
- **CPU reference and golden tests**: `EvaluateMaterial` implements the exact same Cook-Torrance GGX BRDF, hemisphere ambient, normal perturbation, alpha cutoff, and Reinhard + sRGB tone mapping in pure Go. `render3d`'s tests verify GPU pixel readback directly against `EvaluateMaterial`, golden reference renders (`pbr_metallic_roughness_ramp.png` and `pbr_textured_normal_maps.png`), and `gpu.View` execution on both the full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles; `UPDATE_GOLDEN=1` rewrites the golden PNGs.

### glTF 2.0 materials and textures (`UploadGLTFMaterials`, `GLTFMaterialSet`)

`UploadGLTFMaterials` maps every material, texture, image, and sampler in a loaded `*gltf.Document` onto `render3d`'s `Material`, `GPUMaterial`, `GPUTexture`, and `GPUSampler` types:

```go
doc, err := gltf.LoadFS(assetsFS, "DamagedHelmet.gltf")
matSet, err := render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{
	Label:    "DamagedHelmet",
	Retained: true, // recovers uploaded textures, samplers, and materials across device loss
})
defer matSet.Close()

// Resolve per-primitive material (with optional KHR_materials_variants support and -1 default fallback):
gpuMat := matSet.MaterialForVariant(prim, "NightOps") // or matSet.MaterialForPrimitive(prim, -1)
gpuMat.Bind(pass)
gpuMesh.Draw(pass)
```

- **Automatic color-space separation & deduplication**: `baseColorTexture` and `emissiveTexture` upload with `SRGB: true` (`FormatRGBA8UnormSRGB`), while `metallicRoughnessTexture`, `normalTexture`, and `occlusionTexture` upload as linear (`FormatRGBA8Unorm`). Referenced glTF images are decoded once per image index and deduplicated on the GPU by `(imageIndex, sRGB)`, so packed ORM maps shared between `occlusionTexture` and `metallicRoughnessTexture` (or shared across multiple materials) upload only once; if the same image is used as both a color map and a data map, each color space receives its own GPU texture.
- **glTF 2.0 samplers & mipmaps**: `SamplerOptionsFromGLTF` maps all six glTF filter modes and three wrap modes (`Repeat`, `ClampToEdge`, `MirroredRepeat`), defaulting omitted wrap modes to `AddressRepeat` (required by assets whose UVs wrap outside `[0, 1]`, such as `DamagedHelmet`'s `V ∈ [1, 2]`) and omitted filters to trilinear filtering with CPU-generated mipmaps. A texture without a sampler gets the same glTF default sampler as an empty sampler object. The non-mipmapped `NEAREST` and `LINEAR` min filters clamp the level of detail to the base level, so they stay sharp when the same image is also uploaded with mips for another sampler. `GLTFMaterialUpload.MaxAnisotropy` applies anisotropy only to all-linear samplers so nearest samplers stay WebGPU-valid; `DisableMipmaps` turns off mip chains when desired.
- **Extensions, metadata & default material**: `GLTFMaterialSet` always uploads a glTF 2.0 default material returned by `DefaultMaterial()` and `Material(-1)` for unassigned primitives. `KHR_materials_emissive_strength` is folded into `EmissiveFactor`, and a negative `normalTexture.scale` flips the normal map's X and Y as the specification allows. `Info(i)` / `InfoForVariant(prim, name)` expose `GLTFMaterialInfo` with the `KHR_materials_unlit` flag (`Unlit`) and per-slot `GLTFTextureSlot` UV metadata (`EffectiveTexCoord`, `Transform` for `KHR_texture_transform`).
- **What `MaterialWGSL` does not draw**: the shader lights every material and samples every texture with the mesh's `TEXCOORD_0` and no transform. Unlit materials, texture transforms and other UV sets are recorded but not applied, so such assets draw wrong unless you handle them. `GLTFMaterialInfo.Unsupported()` lists these cases for a material and `GLTFTextureSlot.NeedsUVBake()` reports a slot that needs its UVs baked into the mesh (for example with `Document.TransformTexCoords`) or a custom shader.
- **Khronos reference verification**: `render3d`'s tests render the Khronos `DamagedHelmet` sample and check it against the asset's own base color image: a probe texture reads back the UV of every pixel, and the render must match the decoded JPEG under glTF's UV origin, sRGB color and channel order far better than under a flipped V, a linear upload or swapped channels. The tests also compare the render with `render3d/testdata/damagedhelmet/reference.png` to catch regressions and verify `gpu.View` parity across both the full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles as well as `Retained` device-loss restoration. The model is licensed CC-BY-NC-4.0, so its directory is a separate module that is left out of the published `packages/game` module, and these tests skip when it is absent.

## Budgeted async GPU uploads (`Uploader`)

`Uploader` runs the decoding and CPU packing of meshes, textures and raw GPU payloads on background worker goroutines, and `Uploader.Step`, called once per frame, writes the results to the `gpu.Device` in chunks within a per-frame byte budget, so loading a large 3D model does not stall a frame:

```go
gpu.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady: func(dev *gpu.Device) {
		uploader = render3d.NewUploader(cx, dev, render3d.UploaderOptions{
			BytesPerFrame: 2 * 1024 * 1024, // 2 MiB per frame (default 4 MiB)
			ChunkBytes:    256 * 1024,      // 256 KiB per write
			Workers:       2,               // background decode goroutines
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
		stats := uploader.Step() // writes at most BytesPerFrame on the engine goroutine
		_ = stats
		// draw with whichever meshes and textures are ready...
	},
})
```

- **Drop-in for the synchronous uploads**: `QueueMesh` takes the same `MeshUpload` options as `UploadMesh` and delivers a `GPUMesh`; `QueueTexture` and `QueueTexturePixels` take the same `TextureUpload` options as `UploadTexture` and `UploadTexturePixels` and deliver a `GPUTexture` with the same texels, the same straight-alpha conversion (`PixelBufferFromImage`) and the same mip chain (box-filtered in linear light for sRGB), so it works with `BindTexture` and counts in `GPUTexture.ByteSize`. Each `Queue*` call also takes an `UploadPriority`.
- **Decode off-thread, upload on the engine goroutine**: decode callbacks, mesh validation and packing, image conversion and mip generation run on the workers. GPU resource creation, the chunked writes (`WriteBuffer`, `WriteTextureRegion`, `WriteTexture`) and the `onReady` callbacks run on the goroutine that calls `Step`. Errors a `Queue*` call can report at once (a nil decode function, a closed uploader, a full queue) call `onReady` before the call returns, on the caller's goroutine.
- **Per-frame byte budget**: `UploaderOptions.BytesPerFrame` (default `DefaultUploadBytesPerFrame`, 4 MiB) and `ChunkBytes` (default `DefaultUploadChunkBytes`, 256 KiB) cap the bytes written per `Step`, and `SetBytesPerFrame` and `SetChunkBytes` change them at runtime, for example raising the budget on a loading screen and lowering it during play. The budget bounds the upload work in each frame; it is not a time limit. Buffers split at 4-byte aligned offsets; uncompressed textures split by mip level, row band and, when a row exceeds the remaining budget, part of a row; block-compressed textures split by block row.
- **Raw buffers and textures**: `QueueRawBuffer` streams custom vertex, storage or uniform buffers (`RawBufferUpload`), and `QueueRawTexture` streams pre-encoded or block-compressed mip chains, array layers and cube faces (`RawTextureUpload`, `TextureMipData`) and delivers the `gpu.Texture`.
- **Priority and cancellation**: `PriorityHigh`, `PriorityNormal` and `PriorityLow` order both the decode and the upload queue (FIFO within a priority), so a high-priority asset preempts a low-priority one that is partway through its upload at the next chunk. Each `Queue*` call returns an `*UploadHandle` whose `State()`, `Progress()`, `Err()`, `Done()` and `Cancel()` are safe from any goroutine.
- **Lifecycle**: passing a `*core.Scope` to `NewUploader` closes the uploader when the scope is disposed. `Close` cancels pending decodes, joins every worker goroutine, releases partially uploaded GPU resources and calls the `onReady` of each unfinished upload with `ErrUploaderClosed`; call it on the goroutine that calls `Step`, as scope disposal does. Pass `nil` in headless tests and call `Close` yourself. `UploaderOptions.MaxPending` bounds the unfinished uploads, failing further ones with `ErrUploadQueueFull`.

||||||| parent of 6ae11efc (feat(render3d): add HDR render target, tone mapping, and sRGB/gamma pass)

## HDR rendering, tone mapping, and gamma (`HDRTarget`, `ToneMapPass`)

Render 3D lighting and emissive surfaces into a linear floating-point `HDRTarget` (`gpu.FormatRGBA16Float`, with automatic fallback to `gpu.FormatRGBA8Unorm` on devices that do not report `SupportsHDR(dev)`), then resolve it to the display with `ToneMapPass`:

```go
var (
	hdr      *render3d.HDRTarget
	tonePass *render3d.ToneMapPass
)

gpu.View(cx, gpu.ViewProps{
	OnReady: func(dev *gpu.Device) {
		w, h := dev.SurfaceSize()
		if hdr == nil {
			hdr, err = render3d.NewHDRTarget(cx, dev, render3d.HDRTargetDesc{
				Label:       "scene-hdr",
				Width:       uint32(w),
				Height:      uint32(h),
				DepthFormat: gpu.FormatDepth24Plus,
				Retained:    true,
			})
		}
		if tonePass == nil {
			tonePass, err = render3d.NewToneMapPass(cx, dev, render3d.ToneMapPassDesc{
				Label:        "tonemap",
				TargetFormat: dev.SurfaceFormat(),
				Retained:     true,
			})
		} else {
			tonePass.SetDevice(dev)
		}
	},
	OnFrame: func(frame *gpu.Frame) {
		// 1. Render 3D scene in linear HDR radiance (values > 1.0 preserved):
		scenePass := frame.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x0a0e18), 1))
		// ... bind camera, pipelines, and draw meshes ...
		scenePass.End()

		// 2. Tone-map and encode to sRGB onto the swapchain:
		opts := render3d.ToneMapOptions{
			Operator: render3d.ToneMapAgX,
			Exposure: render3d.ExposureFromEV(0),
		}
		displayPass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "tonemap"})
		_ = tonePass.DrawTarget(displayPass, frame.Device(), hdr, opts)
		displayPass.End()
		frame.Submit()
	},
})
```

`ToneMapOptions.Operator` chooses the highlight compression curve:

| Operator | Curve | Best for |
| --- | --- | --- |
| `ToneMapNone` | Linear clamp to `[0, 1]` | Debugging linear inputs |
| `ToneMapReinhard` | Per-channel `c / (1 + c)` | Simple soft highlight roll-off |
| `ToneMapACES` (`DefaultToneMapOptions`) | Narkowicz ACES filmic fit | High-contrast cinematic look |
| `ToneMapNeutral` | Khronos PBR Neutral | Faithful mid-tone hues and PBR asset viewing |
| `ToneMapAgX` | Troy Sobotka AgX default look | Smooth highlight desaturation without hue skew |

- **Exposure**: set `ToneMapOptions.Exposure` directly or derive it from photographic stops with `ExposureFromEV(ev)` (`0 -> 1.0`, `+1 -> 2.0`, `-1 -> 0.5`). Non-positive or non-finite values default to `1.0`.
- **Gamma & sRGB**: when `ToneMapOptions.Gamma <= 0` (the default) and `LinearOutput` is false, `ToneMapPass` applies the exact IEC 61966-2-1 linear-to-sRGB OETF (`LinearToSRGBChannel` / `SRGBToLinearChannel` on the CPU); a positive `Gamma` (such as `2.2`) applies power-law `pow(c, 1/Gamma)`. When `ToneMapPassDesc.TargetFormat.IsSRGB()` is true, `ToneMapPass` automatically emits linear tone-mapped RGB so the GPU's sRGB framebuffer hardware encodes linear-to-sRGB once without double gamma.
- **Zero steady-state allocations & device loss**: `AppendToneMapUniforms` writes the `ToneMapUniformSize` (16-byte) uniform block with zero allocations, and `ToneMapPass` caches its bind group per source texture. Passing `Retained: true` to both `HDRTargetDesc` and `ToneMapPassDesc` lets NSGo recreate the textures, sampler, uniform buffer, cached bind group, and pipeline automatically after a device loss.
- **CPU reference & portability**: `ApplyToneMap` mirrors `ToneMapWGSL` on the CPU. `ToneMapWGSL` avoids `exp` and `log2` builtins (using a range-reduced polynomial `log2` for AgX) and runs cleanly on both the full GPU profile and `nsgotest.NoGPUCompute` (WebGL 2 / OpenGL ES 3.0). Reference tests compare lit HDR renders against `render3d/testdata/golden/tonemap_aces.png`, `tonemap_agx.png`, and `tonemap_neutral.png`; `UPDATE_GOLDEN=1` rewrites them.

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

## Dynamic resolution scaling (`DynamicResolutionScaler`, `DynamicResolutionTarget`)

`DynamicResolutionScaler` adapts a linear 3D render scale in `[MinScale, MaxScale]` (default `[0.5, 1.0]`) to hold `TargetFrameTime` (default ~16.67 ms for 60 FPS), and `DynamicResolutionTarget` renders the 3D scene into a scaled sub-viewport of an offscreen `gpu.RenderTarget` and upscales it onto the native surface while keeping 2D HUD overlays at full native resolution:

```go
target, err := render3d.NewDynamicResolutionTarget(cx, dev, render3d.DynamicResolutionTargetOptions{
	TimingLabel: "world-3d",
	Config: render3d.DynamicResolutionConfig{
		TargetFrameTime: time.Second / 60,
		MinScale:        0.5,
		MaxScale:        1.0,
		Filter:          render3d.DynamicResolutionBilinear, // or DynamicResolutionNearest / DynamicResolutionSharpen
	},
})

// In gpu.ViewProps.OnFrame (with scene.Overlay3D mounted over gpu.View for a native-res HUD):
w, h := frame.Size()
pass3d, err := target.Begin3D(frame, w, h, core.RGB(0x10141c))
pass3d.SetPipeline(scenePipe)
pass3d.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
mesh.Draw(pass3d)
pass3d.End()

_ = target.Upscale(frame, nil, w, h) // blits scaled 3D sub-viewport to native surface
frame.Submit()
target.RecordPassTimings(frame.PassTimings()) // or target.RecordFrameTime(gpuDuration)
```

- **Proportional fill-rate controller with hysteresis**: because fragment work scales with `scale²`, `DynamicResolutionScaler.RecordFrameTime` smooths GPU timings with an EMA (`SmoothingAlpha`), ignores loading/resume spikes above `HitchLimit`, holds steady inside `±Deadband`, steps down promptly on overload (`CooldownDown`, `StepDown`), and recovers smoothly under sustained headroom (`CooldownUp`, `StepUp`). Use `SetFixedScale`, `ClearFixedScale`, `SetEnabled`, and `SetScaleBounds` (for example capping `MaxScale` to `quality.Preset.ResolutionScale`) to override or bound adaptation.
- **Zero-reallocation sub-viewport scaling**: `DynamicResolutionTarget` allocates its offscreen color+depth `gpu.RenderTarget` at native capacity and sets `SetViewport` and `SetScissor` to `[0, 0, scaledW, scaledH]` on each `Begin3D` call, so scale changes never churn GPU textures. `Upscale` clamps UVs to half-texel inset bounds of the active sub-rectangle so unused padding texels never bleed into the edges.
- **Native-resolution HUD**: mount [`scene.Overlay3D`](scene.md#2d-layers-over-a-3d-view-overlay3d) over `gpu.View` so 2D HUD layers draw in a crisp 1:1 native `core.Canvas`, record a follow-up `gpu.LoadLoad` native HUD pass after `Upscale`, or call `UpscaleIntoPass` inside a single native color pass and draw GPU HUD elements right after it.
- **Upscale filters**: switch between `DynamicResolutionBilinear`, `DynamicResolutionNearest`, and `DynamicResolutionSharpen` (a 5-tap contrast-adaptive sharpening pass clamped to the local neighborhood min/max) at runtime via `SetFilter` and `SetSharpness` without reallocating pipelines or textures.
