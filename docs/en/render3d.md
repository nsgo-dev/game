# render3d

`game/render3d` provides mesh buffers, textures and samplers, metallic-roughness PBR materials, directional, spot and point light shadow maps, camera uniforms, offscreen render-to-texture cameras and multi-viewport split-screen rendering, distance and height fog, and an HDR render target with a tone-mapping pass for the NSGo GPU view alongside the GPU-agnostic, zero-steady-state-allocation CPU 3D render pipeline and developer tools: frustum culling, opaque/transparent draw sorting, automatic instancing, LOD selection with hysteresis, camera-facing billboards and 3D ribbon trails, 3D debug wireframes and transform gizmos, frame profiling, and mobile/desktop GPU budgets.

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

## Directional, point and spot lights in PBR shading (`LightRig`, `GPULightBuffer`)

`LightRig` holds a scene's linear-RGB lighting: an `AmbientLight`, up to `MaxDirectionalLights` (4) `DirectionalLight`s, up to `MaxPointLights` (16) `PointLight`s, and up to `MaxSpotLights` (8) `SpotLight`s. One pass shades all of them together, so a scene can mix a sun, lamps and a flashlight.

- **`DirectionalLight`**: parallel rays travelling along `Direction` (for example `{Y: -1}` shines straight down onto a `+Y` surface; normalized when shaded or packed), with linear `Color` and `Intensity`.
- **`PointLight`**: omnidirectional emitter at `Position` with linear `Color`, `Intensity`, and optional cutoff `Range`. When `Range > 0`, distance falloff uses the glTF `KHR_lights_punctual` smooth windowing curve `max(min(1 - (d/Range)^4, 1), 0)^2 / max(d^2, 1e-4)` so contribution reaches zero smoothly at `Range`; `Range == 0` uses pure clamped inverse-square `1 / max(d^2, 1e-4)` (`PointLightAttenuation`).
- **`SpotLight`**: a glTF `KHR_lights_punctual` cone at `Position` pointing along `Direction` (normalized when shaded or packed), with linear `Color`, `Intensity` and `Range` falloff exactly as for a `PointLight`, multiplied by the angular falloff between the half-angles `InnerConeAngle` and `OuterConeAngle` (radians, `0 <= inner < outer <= pi/2`): full intensity inside the inner cone, `clamp(cos(angle) * angleScale + angleOffset, 0, 1)^2` between the cones, and zero outside the outer cone or behind the light (`SpotConeAttenuation`, with `angleScale` and `angleOffset` from `SpotLight.ConeScaleOffset`, and `SpotLight.Attenuation` for the combined distance and cone factor). `Validate` refuses a zero outer cone, an outer cone past `pi/2` and an inner cone that is not smaller than the outer one.
- **`AmbientLight`**: uniform isotropic linear fill applied as `Ambient.Color * Ambient.Intensity * BaseColor`.

`GPULightBuffer` uploads a validated `LightRig` into a 1056-byte (`LightUniformSize`), 16-byte-aligned std140/WGSL uniform buffer bound at `LightGroup` (group 1), scoped to `*core.Scope` with optional `LightBufferOptions.Retained` survival across GPU device loss:

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

// Each frame:
_ = lightBuf.Update(frame.Device(), rig)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)
```

Custom 3D shaders can prepend `CameraWGSL + LightWGSL` to read `@group(1) @binding(0) var<uniform> lights: LightRigUniform` and call the portable WGSL helpers `point_light_attenuation`, `spot_cone_attenuation`, `pbr_brdf`, `shade_pbr`, and `linear_to_srgb`. `LitPBRWGSL` combines them with `MeshVertexLayout` and a per-draw `PBRDrawUniform` at `PBRDrawGroup` (group 2).

For more local lights than the rig holds, bind a `ClusteredLightSystem` at `LightGroup` instead of a `GPULightBuffer`: it uploads the same rig and adds any number of clustered point and spot lights, shaded with the same BRDF ([Clustered forward lighting](#clustered-forward-lighting-clusteredlightsystem-clustergrid)).

The shading model follows glTF 2.0 metallic-roughness PBR (dielectric `F0 = 0.04`, Schlick Fresnel, Trowbridge-Reitz GGX normal distribution, height-correlated Smith-GGX visibility, energy-conserving Lambertian diffuse, perceptual roughness floored at `0.045`, and IEC 61966-2-1 linear-to-sRGB output). `EvaluatePBR`, `ShadePoint`, `PointLightAttenuation`, `SpotConeAttenuation`, and `LinearToSRGB` implement the exact same equations on the CPU; `LightRig.AppendUniformData` and `LightUniforms.Encode` pack the uniform block without allocating. Reference tests verify GPU output pixel-for-pixel against CPU `ShadePoint`, exercise both full and WebGL 2 / GLES 3.0 (`nsgotest.NoGPUCompute`) profiles plus `Retained` device loss, and compare lit scenes against `render3d/testdata/golden/lights_pbr_scene.png`, `lights_point_falloff.png` and `spot_lights_scene.png`.

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
- **High dynamic range environments and `.hdr` loading (`HDRCubemap`, `UploadCubemapHDR`)**: `Cubemap` stores 8 bits per channel (`RGBA8Unorm` or `RGBA8UnormSRGB`), clamping radiance above 1. For physically based highlights above 1.0, use `HDRCubemap`, which stores linear `float32` RGB faces and mips: decode a Radiance `.hdr` (RGBE) equirectangular panorama directly with `CubemapFromHDR` (or `DecodeHDR` / `EncodeHDR` + `CubemapFromHDREquirectangular`), generate a procedural HDR sky with `GradientSkyHDRCubemap`, or promote an LDR cubemap with `HDRCubemapFromCubemap`. Bake HDR diffuse irradiance and prefiltered specular with `ComputeIrradianceSHHDR` and `PrefilterEnvironmentHDR`, then upload with `UploadCubemapHDR`, which produces a `GPUCubemap` in `gpu.FormatRGBA16Float` (clamping above `65504` to stay finite in half-float, and falling back to `RGBA8Unorm` only if the device lacks `FormatRGBA16Float`). Draw HDR scenes and skyboxes into an `HDRTarget` resolved by `ToneMapPass` (for example `ToneMapNeutral`), or set `SkyboxPipelineDesc.ToneMap` (`SkyboxToneMapWGSL`) to apply Reinhard + gamma 2.2 when drawing an HDR skybox directly to an LDR surface.
- **Textured glTF PBR materials with IBL (`NewMaterialIBLPipeline`, `MaterialIBLWGSL`)**: `NewMaterialIBLPipeline` pairs `CameraGroup` (group 0), a textured glTF `GPUMaterial` at `MaterialGroup` (group 1 — base color, metallic-roughness, tangent-space normal, occlusion, and emissive maps, plus alpha masking/blending and double-sided normal flipping), and a `GPUIBLBuffer` at `MaterialIBLGroup` (group 2). Set `MaterialIBLPipelineOptions.LinearOutput` (`MaterialIBLLinearWGSL`) to write unclamped linear radiance into an `HDRTarget` for `ToneMapPass`; `EvaluateMaterialIBL` is the matching pure-Go CPU reference.
- **CPU reference and device loss**: `EvaluateIBL` and `EvaluateIBLHDR` match `evaluate_ibl` on the CPU, `ShadePoint` plus `EvaluateIBL` matches `LitPBRIBLWGSL`, and `EvaluateMaterialIBL` matches `MaterialIBLWGSL`. With `Retained: true` on `CubemapUpload`, `SkyboxPipelineDesc`, `IBLBufferOptions`, and `MaterialIBLPipelineOptions`, NSGo restores textures, samplers, pipelines and uniform buffers after a device loss; call `SkyboxPipeline.SetDevice(dev)` in `OnReady` and pass the new device to `GPUIBLBuffer.Update`.
- **Reference renders**: headless software-GPU tests compare a skybox behind a mesh and 2x2 metallic/roughness spheres with `render3d/testdata/golden/skybox_scene.png` and `ibl_spheres.png`, and render the Khronos `DamagedHelmet` (`render3d/testdata/damagedhelmet/reference_ibl.png`, skipped when the CC-BY-NC-4.0 fixture directory is absent) with all five glTF PBR maps under an `.hdr`-decoded `FormatRGBA16Float` HDR environment and skybox resolved through `ToneMapNeutral` (`UPDATE_GOLDEN=1` rewrites them), verifying measurably brighter specular highlights than an LDR-clamped environment, CPU reference parity, far-plane visibility, retained device-loss recovery, and both the full and `nsgotest.NoGPUCompute` profiles.

## Reflection probes and light probes (`ReflectionProbeSet`, `LightProbeVolume`, `GPUProbeIBLBuffer`)

`render3d` extends its IBL pipeline with spatially varying local reflections and interpolated diffuse indirect lighting built directly on `Cubemap`, `GPUCubemap`, `PrefilterEnvironment`, and `IrradianceSH` — plugging into `LitPBRWGSL` at `IBLGroup` (group 3) so metallic, roughness, emissive, `LightRig` direct lights, inverse-transpose normal matrices, and sRGB output work in one pass across every backend including WebGL 2 and OpenGL ES 3.0 (`NoGPUCompute`).

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

// Bake on load or on request (runtime-only; re-baking closes any previous GPUCubemap):
render3d.BakeReflectionProbeGPU(cx, dev, room, render3d.ProbeBakeOptions{
	Size:       64,
	ClearColor: core.RGB(0x101824),
	SRGB:       false, // RGBA8Unorm target; set true for RGBA8UnormSRGB
	Prefilter:  render3d.PrefilterOptions{BaseSize: 64, MipLevels: 5, SampleCount: 64},
}, func(pass *gpu.RenderPass, face gpu.CubeFace, faceCam render3d.CameraUniform) {
	camBuf.Write(faceCam)
	drawStaticRoom(pass)
}, func(err error) {
	// Invoked asynchronously on the engine goroutine once all six face readbacks finish.
})

// Bake a 3D light probe grid of IrradianceSH for dynamic objects:
grid, _ := render3d.NewLightProbeGrid(roomBounds, 4, 2, 4)
_ = grid.Bake(16, sampleRoomRadiance)
lightProbes := &render3d.LightProbeVolume{Grid: grid, FallbackSH: skySH}

// Bind per-object probe IBL at IBLGroup (group 3) alongside LitPBRWGSL:
probeIBL, _ := render3d.NewGPUProbeIBLBuffer(cx, dev, render3d.ProbeIBLBufferOptions{Label: "hero-ibl"})
pipe, _ := render3d.NewLitPBRProbeIBLPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), probeIBL.Layout(), gpu.FormatDepth24Plus)

// Each frame for a moving object:
_ = probeIBL.UpdateForPoint(dev, heroPos, &probes, lightProbes, skyPrefCube, 1.0)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
probeIBL.Bind(pass)
heroMesh.Draw(pass)
```

- **Local reflection selection, parallax and blending**: `ReflectionProbeSet.SelectReflectionProbes` picks up to two ready (enabled, baked, non-dirty) probes covering a world position by `Priority`, smoothstep `Influence` (over `BlendDistance`), and volume, returning normalized `(Weight0, Weight1, SkyWeight)` summing to 1. Probes with `BoxProjection: true` apply `BoxProjectDirection` (`box_project_dir` in WGSL) to correct reflections against `[BoxMin, BoxMax]`. Unbaked, dirty, or disabled probes are skipped during selection, and any missing probe or `nil` sky binds a 1x1 black `(0, 0, 0, 255)` fallback cubemap so CPU `EvaluateProbeIBL` and GPU `LitPBRProbeIBLWGSL` match identically.
- **Light probes (`LightProbe`, `LightProbeGrid`, `LightProbeVolume`)**: store `IrradianceSH` coefficients (baked via `ComputeIrradianceSH` or `ComputeIrradianceSHFromFunc` and combined with `Add`, `Scale`, `Lerp`). `LightProbeGrid.Sample` interpolates trilinearly and `LightProbeVolume.SampleLightProbes` blends scattered `LightProbe` points (with optional finite `Radius`) smoothly over the grid or `FallbackSH`. Both validate `Counts` against `len(Probes)` and non-finite coordinates without panicking.
- **Asynchronous GPU baking and target formats**: `BakeReflectionProbeGPU` renders the six faces with `ProbeFaceCamera` into temporary `FormatRGBA8Unorm` (when `SRGB` is false) or `FormatRGBA8UnormSRGB` (when `SRGB` is true) plus `FormatDepth24Plus` offscreen render targets, reads back all six faces asynchronously via `gpu.Device.ReadTexture`, flips rows to canonical cubemap orientation, prefilters roughness mips with `PrefilterEnvironment`, uploads via `UploadCubemap`, closes any previous `probe.GPUCubemap`, and invokes `onDone` on the engine goroutine. `BakeReflectionProbe` provides the matching CPU sampler bake.
- **Runtime-only bakes**: reflection probe cubemaps and light probe SH data are runtime-only caches generated on load or on request rather than persisted in text scene files.
- **Custom shaders and HDR**: prepend `ProbeIBLWGSL` (`IBLGroup`) or `ProbeIBLWGSLForGroup(group)` and call `evaluate_probe_ibl(world_pos, normal, view_dir, base_color, metallic, roughness, ao)`. Share one `ProbeIBLBindGroupLayout` across multiple per-object `GPUProbeIBLBuffer` instances via `ProbeIBLBufferOptions.Layout`, and use `NewLitPBRProbeIBLPipelineWithOptions` with `LinearOutput: true` for `HDRTarget`. `ShadePoint` plus `EvaluateProbeIBL` (followed by `LinearToSRGB`) is the exact CPU reference for `LitPBRProbeIBLWGSL`.

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
- **KTX2 textures**: `LoadKTX2` uploads a 2D KTX2 file such as `game assets` and `game tex` write, with every mip level it holds, in its sRGB or linear format, and returns the sampler to draw it with: `KTX2Upload.Sampler` with its filters set from the file's `game.filter` key (`KTX2FilterKey`), which the `filter` of the image's [import settings sidecar](cli.md#import-settings-beside-each-asset) writes, so `nearest` draws pixel art sharp and `linear` blends. BasisLZ, Zstandard and ZLIB supercompressed levels are decompressed first. BC1, BC3, BC7, ETC2 RGB8 and RGBA8, and ASTC 4x4 levels (including multi-partition, dual-plane and BISE-coded blocks written by `astcenc`) are uploaded compressed where `Device.SupportsCompressedFormat` allows and decoded to RGBA8 on the CPU elsewhere, so the texture loads on every backend; `KTX2Sampler` applies the filter to sampler options without a device. A file that is not such a texture, or names an unknown filter, returns an error wrapping `ErrInvalidTexture`:

  ```go
  data, err := fs.ReadFile(assets, "albedo.ktx2")
  albedo, opts, err := render3d.LoadKTX2(cx, dev, data, render3d.KTX2Upload{Label: "albedo", Retained: true, Sampler: render3d.SamplerLinearRepeat()})
  samp, err := render3d.NewGPUSampler(cx, dev, opts)
  ```

- **Basis Universal (UASTC and BasisLZ/ETC1S)**: `LoadKTX2` takes both UASTC 4x4 (high-quality) and BasisLZ/ETC1S (`basisu`'s default mode, including alpha slices) KTX2 files, and transcodes them to the best block format the device supports: BC7, then BC1 for an opaque texture, BC3, ETC2 RGB8 or RGBA8 (linear only, as NSGo has no sRGB ETC2), then ASTC 4x4. It decodes to RGBA8 when the device has none of them, when the base level's width or height is not a multiple of 4, or when `KTX2Upload.ForceRGBA8` is set. Transcoding decodes every block and encodes it again on the goroutine that calls `LoadKTX2`: on a desktop CPU (Ryzen 9 3900X) a 1024×1024 level takes about 0.5 s to BC7, 0.8 s to ETC2 and 0.16 s to ASTC, and four times as long at 2048×2048 (`BenchmarkTranscodeUASTC` in `tooling/ktx2`), so load large universal textures during a loading screen. BC1 without alpha (`VK_FORMAT_BC1_RGB_*`) is refused, because an RGBA BC1 upload would draw its punch-through texels transparent.

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

## GPU skinning (`SkinnedMesh`, `GPUSkinnedMesh`, `GPUSkinPalette`)

`SkinnedMesh` extends `Mesh` with per-vertex glTF skinning attributes (`Joints [][4]uint16` and `Weights []math3d.Vec4`) and optional `JointCount`. `SkinnedMeshFromGLTF` and `SkinnedMeshesFromGLTFNode` extract and validate triangle primitives from a loaded `gltf.Document`, checking joint bounds and normalizing each vertex's four weights so they sum to 1 (falling back to `(1, 0, 0, 0)` when all four weights are zero). `UploadSkinnedMesh` uploads the interleaved 64-byte vertex buffer and optional index buffer (`uint16` up to 65,535 vertices, `uint32` beyond) as a scope-owned `GPUSkinnedMesh` with optional `Retained` device-loss restoration.

Every skinned mesh shares one interleaved little-endian layout (`SkinnedVertexLayout`), extending `MeshVertexLayout` at locations 0..2:

| Attribute | `@location` | Format | Offset |
| --- | --- | --- | --- |
| position | 0 (`SkinnedPositionLocation`) | `float32x3` | 0 |
| normal | 1 (`SkinnedNormalLocation`) | `float32x3` | 12 |
| UV | 2 (`SkinnedUVLocation`) | `float32x2` | 24 |
| joints | 3 (`SkinnedJointsLocation`) | `uint32x4` | 32 |
| weights | 4 (`SkinnedWeightsLocation`) | `float32x4` | 48 |

`GPUSkinPalette` stores up to `MaxSkinJoints` (128) joint matrices in a portable uniform buffer (`SkinUniformSize` = 8,192 bytes, fitting comfortably below WebGL 2 and OpenGL ES 3.0's 16 KiB `MAX_UNIFORM_BLOCK_SIZE` minimum). Unused joint slots are padded with identity matrices. `AppendJointMatrices` and `SkinJointMatrices` / `SkinJointMatricesWithTransform` (which compute glTF 2.0 `jointNode.WorldMatrix * InverseBindMatrices[i]` from `doc.Nodes[].WorldMatrix`) reuse caller storage with zero allocations.

Three pipelines draw a `GPUSkinnedMesh`, each binding the palette at its own group, because WebGPU and WebGL 2 guarantee only four bind groups and the lit and material pipelines already use groups 0 to 2:

| Pipeline | Shader | Palette group | Not combinable with |
| --- | --- | --- | --- |
| `NewSkinnedMeshPipeline` (unlit, colored by normal) | `SkinnedMeshWGSL` | `SkinGroup` (1) | |
| `NewLitPBRPipelineWithOptions` with `LitPBRPipelineOptions.SkinLayout` (scene lights) | `LitPBRSkinnedWGSL` | `LitPBRSkinGroup` (3) | `FogLayout`, `IBLLayout`, `ShadowLayout`, `SpotShadowLayout`, `PointShadowLayout` |
| `NewMaterialPipeline` with `MaterialPipelineOptions.SkinLayout` (glTF material with textures) | `MaterialSkinnedWGSL` | `MaterialSkinGroup` (2) | `FogLayout` |

`SkinWGSL` declares the uniform block at group 1 and the portable `skin_position` / `skin_normal` vertex helpers matching CPU `SkinVertex`, for custom shaders that bind nothing else at group 1.

Joint matrices come in two conventions. `SkinJointMatrices` returns world-space joints, so the skinned vertex is already in world space. anim3d's `Player.SkinMatricesForNode` returns mesh-local joints, `inverse(meshWorld) * jointWorld * IBM`, and the Player keeps its own world matrices instead of writing `doc.Nodes`. Upload mesh-local joints with `GPUSkinPalette.UpdateWithTransform` and the mesh node's world matrix (`Player.WorldTransform`), or pass that matrix as the lit PBR draw's model; otherwise the mesh node's own transform (CesiumMan's Z-up rotation) is lost. A lit CesiumMan animated by anim3d:

```go
doc, _ := gltf.LoadFS(assetsFS, "CesiumMan.gltf")
const meshNode = 2
meshes, _ := render3d.SkinnedMeshesFromGLTFNode(doc, meshNode)
player, _ := anim3d.NewPlayer(doc)
_ = player.Play(0)

// In gpu.View OnReady:
camBuf, _ := render3d.NewCameraBuffer(cx, dev)
lights, _ := render3d.NewLightBuffer(cx, dev, rig, render3d.LightBufferOptions{})
draw, _ := render3d.NewPBRDrawBuffer(cx, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat), true)
palette, _ := render3d.NewGPUSkinPalette(cx, dev, render3d.SkinPaletteOptions{Label: "hero", Retained: true})
gpuMesh, _ := render3d.UploadSkinnedMesh(cx, dev, meshes[0], render3d.MeshUpload{Label: "hero", Retained: true})
pipe, _ := render3d.NewLitPBRPipelineWithOptions(cx, dev, camBuf.Layout(), lights.Layout(), draw.Layout(), render3d.LitPBRPipelineOptions{
	DepthFormat: gpu.FormatDepth24Plus,
	SkinLayout:  palette.Layout(),
})

// Each frame:
player.Step(dt)
joints, _ := player.SkinMatricesForNode(meshNode) // mesh-local
_ = palette.Update(frame.Device(), joints)
draw.Write(frame.Device(), render3d.NewPBRDrawUniform(player.WorldTransform(meshNode), mat))
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lights.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, draw.BindGroup())
pass.SetBindGroup(render3d.LitPBRSkinGroup, palette.BindGroup())
gpuMesh.Draw(pass)
```

Without anim3d, pose the document with `doc.Animations[i].Apply(doc, t)` and `doc.ComputeWorldMatrices()`, then call `palette.UpdateFromGLTF(dev, doc, skinIndex)` (zero allocations) and draw with an identity model.

- **Bounds**: `SkinnedMesh.Bounds` and `GPUSkinnedMesh.Bounds` cover the unskinned positions in the mesh's own space, not the drawn mesh. For culling and camera framing use `SkinnedBounds(gpuMesh.Bounds(), joints)`, the union of that box under every joint matrix, which contains the skinned mesh in any pose.
- **Weights**: weights are divided by their largest component before they are summed, so large finite weights normalize instead of overflowing.
- **Tests**: CesiumMan and RiggedSimple are drawn on the full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles and compared with CPU `SkinVertex` renders; CesiumMan's bind pose is checked against the mesh node's world matrix, and the lit and material pipelines are checked with both joint conventions.

## Metallic-roughness materials (`Material`, `MaterialDefaults`, `GPUMaterial`)

`Material` (aliased as `MetallicRoughnessMaterial`) represents a glTF 2.0 metallic-roughness PBR surface with base color (`BaseColorFactor`, `BaseColorTexture`), metallic-roughness (`MetallicFactor`, `RoughnessFactor`, `MetallicRoughnessTexture` using the glTF G = roughness, B = metallic channel packing), tangent-space normal mapping (`NormalTexture`, `NormalScale`), ambient occlusion (`OcclusionTexture`, `OcclusionStrength`), emissive (`EmissiveFactor`, `EmissiveTexture`), alpha modes (`AlphaOpaque`, `AlphaMask` with `AlphaCutoff`, `AlphaBlend`), and `DoubleSided` back-face normal flipping.

Create one shared `MaterialDefaults` per `gpu.Device` to own the `MaterialGroup` (group 1) bind group layout (plus the unlit one, `UnlitLayout`, described under [Unlit materials and vertex colors](#unlit-materials-and-vertex-colors-unlitmaterial-coloredmesh)), default linear-repeat sampler, and 1×1 fallback textures (white sRGB for missing base color, white linear for missing metallic-roughness/occlusion, flat `(128, 128, 255, 255)` for missing normal maps, and black for missing emissive maps). Because every `GPUMaterial` binds those fallbacks for omitted slots, all materials share one bind group layout with zero pipeline permutations:

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
- **Vertex colors (`COLOR_0`) on metallic-roughness & lit PBR pipelines**: set `MaterialPipelineOptions.VertexColors: true` (compiles `MaterialColoredWGSL`, or its fog / linear-output variant) or `LitPBRPipelineOptions.VertexColors: true` (compiles `LitPBRColoredWGSL`, or its fog, IBL, shadow, spot/point shadow, CSM, or linear-output variant) to draw a `GPUColoredMesh` (`ColoredVertexLayout`, `@location(3)` linear RGBA). Per glTF 2.0 §3.7.2.1, the shader multiplies `base_color` (RGB and alpha) by the linear vertex color before `AlphaMask` cutoff and PBR shading. `VertexColors` cannot be combined with `SkinLayout` (`SkinnedVertexLayout` has no vertex color attribute).
- **CPU reference and golden tests**: `EvaluateMaterial` (with `MaterialEvalInput.VertexColor` and `HasVertexColor`, defaulting to `[1, 1, 1, 1]` when unset) implements the exact same Cook-Torrance GGX BRDF, hemisphere ambient, normal perturbation, alpha cutoff, and Reinhard + sRGB tone mapping in pure Go; `PBRMaterial.WithVertexColor` folds a linear vertex color into `PBRMaterial` for `ShadePoint` / `EvaluatePBR` / `EvaluateIBL`. `render3d`'s tests verify GPU pixel readback directly against `EvaluateMaterial` and `ShadePoint`, render the Khronos `VertexColorTest` sample end-to-end, compare golden reference renders (`pbr_metallic_roughness_ramp.png` and `pbr_textured_normal_maps.png`), and check `gpu.View` execution on both the full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles; `UPDATE_GOLDEN=1` rewrites the golden PNGs.

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

### Extended PBR materials: emissive strength, clearcoat, and transmission (`GPUExtendedMaterial`, `TransmissionBackground`)

`Material` also carries fields for `KHR_materials_clearcoat` (`ClearcoatFactor`, `ClearcoatRoughnessFactor`, `ClearcoatTexture`, `ClearcoatSampler`, `ClearcoatRoughnessTexture`, `ClearcoatRoughnessSampler`, `ClearcoatNormalTexture`, `ClearcoatNormalSampler`, `ClearcoatNormalScale`), `KHR_materials_transmission` (`TransmissionFactor`, `TransmissionTexture`, `TransmissionSampler`), and dielectric index of refraction (`IOR`, defaulting to `DefaultMaterialIOR = 1.5`), while `KHR_materials_emissive_strength` folds directly into HDR `EmissiveFactor` values (`> 1.0`, rendered cleanly into an `HDRTarget` with `LinearOutput: true`, `BloomPass`, and `ToneMapPass`).

Plain `GPUMaterial` and `MaterialDefaults` keep their lean 11-binding layout unchanged for ordinary metallic-roughness assets. For surfaces that use clearcoat, transmission, or custom IOR, use `ExtendedMaterialDefaults`, `GPUExtendedMaterial` (or `UploadGLTFExtendedMaterials`), and `NewExtendedMaterialPipeline`:

```go
extDefs, err := render3d.NewExtendedMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Retained: true})
transBG, err := render3d.NewTransmissionBackground(cx, dev, render3d.TransmissionBackgroundOptions{
	Retained: true,
	SRGB:     true, // true when sampling an sRGB-encoded RGBA8Unorm opaque pass; false for HDRTarget
})
pipe, err := render3d.NewExtendedMaterialPipeline(cx, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
	DepthFormat:        gpu.FormatDepth24Plus,
	LightLayout:        lightBuf.Layout(), // optional LightRig at ExtendedLightGroup (2)
	TransmissionLayout: transBG.Layout(),  // optional per-frame background at TransmissionBackgroundGroup (3)
})

// Upload glTF materials (base PBR + clearcoat + transmission + emissiveStrength + IOR):
matSet, err := render3d.UploadGLTFExtendedMaterials(cx, dev, extDefs, doc, render3d.GLTFMaterialUpload{
	Label:    "Sunglasses",
	Retained: true,
})

// Each frame: render opaque scene to opaqueRT, update the single per-frame TransmissionBackground, then draw transmissive draws:
_ = transBG.SetTexture(dev, opaqueRT.Color, true) // or transBG.SetDynamicResolution(dev, dynTarget, nil, 0, 0, true)
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
pass.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
transBG.Bind(pass) // @group(3) TransmissionBackgroundGroup (or BindAt(..., MaterialTransmissionBackgroundGroup) without LightLayout)
matSet.ExtendedMaterial(prim.Material).Bind(pass) // @group(1) MaterialGroup
mesh.Draw(pass)
```

- **UV-aligned clearcoat normal mapping**: `ExtendedMaterialBindingsWGSL` reuses the same Cramer's-rule `dpdx`/`dpdy` cotangent-frame normal perturbation helper (with deterministic orthonormal fallback) on the geometric normal for `ClearcoatNormalTexture` independently of the base `NormalTexture`, honoring positive and negative `ClearcoatNormalScale`.
- **Per-frame transmission background and dynamic resolution**: `TransmissionBackground` keeps the opaque scene color texture out of per-material bind groups so resizing the swapchain or scaling a `DynamicResolutionTarget` updates only one pass-level bind group. Screen UVs are derived from `frag_pos.xy / vec2<f32>(textureDimensions(transmission_bg_tex))`, scaled and half-texel clamped by `SetViewport` / `SetDynamicResolution` so unused padding texels never bleed.
- **CPU parity and Khronos golden tests**: `EvaluateMaterial`, `EvaluatePBR`, `ShadePoint`, and `ShadePointWithBackground` evaluate clearcoat, transmission, and IOR on the CPU to match the GPU shaders. Tests verify GPU-vs-CPU parity, `DynamicResolutionTarget` resizes across 0.5×–1.0× scales, `gpu.View` execution on both full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles, `Retained` device-loss restoration, and golden renders of the Khronos `EmissiveStrengthTest` (`khronos_emissive_strength.png`), `TextureTransformMultiTest` clearcoat panels (`khronos_clearcoat.png`), and `SunglassesKhronos` (`khronos_transmission.png`).

## Unlit materials and vertex colors (`UnlitMaterial`, `ColoredMesh`)

`ColoredMesh` and `GPUColoredMesh` extend `MeshVertexLayout` (locations 0–2) with a linear RGBA vertex color attribute at `@location(3)` (`ColoredVertexLayout`, stride 48 bytes) for glTF `COLOR_0`. When `Colors` is empty, `AppendVertexData` writes linear opaque white `(1, 1, 1, 1)` per vertex so colored and uncolored meshes share one pipeline. `ColoredMeshFromGLTF` extracts triangle primitives from a `gltf.Document` including `COLOR_0` (expanding VEC3 RGB to alpha `1.0`) and `KHR_texture_transform` UVs on `baseColorTexture`.

`UnlitMaterial`, `GPUUnlitMaterial`, `UnlitWGSL`, and `NewUnlitPipeline` implement glTF `KHR_materials_unlit` surfaces unaffected by scene lighting (`baseColorFactor * baseColorTexture * vertexColor` in linear space, followed by alpha handling and IEC 61966-2-1 linear-to-sRGB encoding). They share the metallic-roughness types where the two overlap: `AlphaMode`, `MaterialGroup` (group 1), `MaterialUpload`, `GLTFMaterialUpload`, `SamplerOptionsFromGLTF`, and the per-device `MaterialDefaults`, which also owns the unlit bind group layout (`MaterialDefaults.UnlitLayout`):

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

// In OnFrame:
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
gpuMat.Bind(pass) // binds @group(1) (MaterialGroup)
gpuMesh.Draw(pass)
```

- **Fallback texture and sampler**: when `UnlitMaterial.BaseColorTexture` or `BaseColorSampler` is nil, `NewGPUUnlitMaterial` binds the shared `MaterialDefaults.WhiteSRGB()` texture and `MaterialDefaults.Sampler()`, so textureless unlit materials allocate nothing beyond their 32-byte uniform buffer.
- **Alpha modes and culling**: `AlphaOpaque` forces output alpha to `1.0` with depth writes enabled; `AlphaMask` discards fragments with combined alpha `< AlphaCutoff` (default `0.5`) and outputs `1.0`; `AlphaBlend` enables straight-alpha blending (`gpu.BlendAlpha`) with depth writes disabled. `DoubleSided` switches culling from `gpu.CullBack` to `gpu.CullNone`. `BaseColorFactor` must lie in `[0, 1]`, as for `Material`.
- **Target color space and CPU parity**: `NewUnlitPipeline` selects `fs_main` (linear-to-sRGB in shader) for non-sRGB targets such as `FormatRGBA8Unorm`, and `fs_linear` (linear output) for sRGB targets (`ColorFormat.IsSRGB()`, where the hardware encodes on write) or when `LinearOutput` is set for an `HDRTarget`. `NewMaterialPipeline` and the lit PBR pipelines always encode in the shader and do not switch on an sRGB format, so when unlit and PBR draws share a pass, give them a non-sRGB target. `EvaluateUnlit` provides the matching pure-Go CPU reference (`Linear`, `SRGB`, `Discarded`, `RGBA8()`, `BlendOver()`).
- **Vertex colors in both unlit and PBR pipelines**: `ColoredVertexLayout` and `GPUColoredMesh` work with `NewUnlitPipeline`, with `NewMaterialPipeline` when `MaterialPipelineOptions.VertexColors` is set (`MaterialColoredWGSL`), and with `NewLitPBRPipelineWithOptions` when `LitPBRPipelineOptions.VertexColors` is set (`LitPBRColoredWGSL` and its fog, IBL, shadow, and CSM variants). Without `VertexColors`, the metallic-roughness and lit PBR pipelines keep `MeshVertexLayout` unchanged.
- **Zero-allocation uniform updates and device-loss recovery**: `AppendUnlitUniforms` writes the 32-byte uniform block with zero allocations when given capacity; `GPUUnlitMaterial.Update(dev, mat)` updates parameters in place without caching a `*gpu.Device` pointer, so materials created with `Retained: true` survive device loss and remain updatable after restoration. NSGo restores a bind group layout only when a restored bind group uses it, so `MaterialDefaults.UnlitLayout()` comes back only while a retained `GPUUnlitMaterial` exists; `MaterialDefaults.Valid` does not cover it.
- **glTF helpers**: `UnlitMaterialFromGLTF` validates that a glTF material declares `KHR_materials_unlit` (returning `ErrInvalidUnlitMaterial` otherwise). `UploadGLTFUnlitMaterial` decodes and uploads embedded or external `baseColorTexture` images in sRGB with a sampler from `SamplerOptionsFromGLTF` (so a non-mipmapped `minFilter` stays on level 0 even when mipmaps exist) and applies `GLTFMaterialUpload`'s mipmap, anisotropy and `Retained` options as `UploadGLTFMaterials` does. A `baseColorTexture` whose image comes only through an extension such as `KHR_texture_basisu` returns `ErrInvalidUnlitMaterial` instead of drawing white.
- **Reference renders**: `render3d`'s tests draw the Khronos `UnlitTest` sample end to end (loader, `ColoredMeshFromGLTF`, `UploadGLTFUnlitMaterial`, `NewUnlitPipeline`) and require every covered pixel to be the sample's flat `#FF7F00` or `#007FFF`, compare a multi-mode unlit + vertex-color scene against `render3d/testdata/golden/unlit_vertex_colors_scene.png`, and verify `gpu.View` pixel parity against `EvaluateUnlit` on both the full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles.

## Toon shading (`ToonMaterial`, `ToonRenderer`)

`ToonMaterial` and `ToonRenderer` draw cel/toon shading: quantized diffuse light bands (or a 1D color ramp texture), hard stepped Blinn-Phong highlights, Fresnel rim light, and inverted-hull outlines. The shaders (`ToonWGSL`, `ToonOutlineWGSL`) use only vertex and fragment stages, so they run on WebGPU, Metal, Vulkan, Direct3D 11, OpenGL 3.3, OpenGL ES 3.0, WebGL 2, and NSGo's software renderer:

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

// Hard-edged meshes (boxes, low-poly props) need shared corner normals for a closed outline:
boxOutlineCPU, err := render3d.SmoothOutlineNormals(boxMesh)
boxOutline, err := render3d.UploadMesh(cx, dev, boxOutlineCPU, render3d.MeshUpload{Label: "box-outline"})

// In OnFrame, inside a render pass with a depth attachment:
light, err := render3d.ToonLightFromRig(rig) // or render3d.DefaultToonLight()
err = renderer.Draw(dev, pass, camBuf.BindGroup(), light,
	render3d.ToonDraw{Mesh: sphereMesh, Material: heroMat, Transform: sphereXform},
	render3d.ToonDraw{Mesh: boxGPU, OutlineMesh: boxOutline, Material: boxMat, Transform: boxXform},
)
```

- **Bands and ramps**: `Bands` selects 2 to 8 diffuse steps (`0` means `3`), blending from `ShadowColor` to fully lit across `N·L`; `BandSmoothness` in `[0, 0.5]` softens the band edges (`0` gives hard steps). A `RampTexture`, sampled at `u = clamp(N·L, 0, 1)`, replaces the bands with an artist-drawn color ramp; without its own `RampSampler` the material creates a linear-clamp sampler so the ramp's ends do not wrap.
- **Highlights**: `SpecularColor`, `SpecularPower` (`0` means `32`), `SpecularThreshold`, and `SpecularSmoothness` add a stepped Blinn-Phong highlight on lit surfaces; `RimColor`, `RimThreshold`, and `RimSmoothness` add a Fresnel rim based on `1 - N·V`.
- **Lights**: `ToonLight` holds one `DirectionalLight` key light, whose `N·L` is banded, and an `AmbientLight` fill, with the same conventions as a `LightRig`: `Direction` is the way the rays travel, a zero direction is an error, intensity `0` turns a light off, and errors wrap `ErrInvalidLight`. `ToonLightFromRig` takes the first directional light and the ambient fill of a scene's rig; point lights are not used.
- **Bind groups**: `CameraGroup` (0), `ToonMaterialGroup` (1, the same slot as `MaterialGroup`) and `ToonSceneGroup` (2, the per-draw model matrix and the key light, with a dynamic offset in `ToonRenderer`).
- **Materials**: `NewGPUToonMaterial` and `ToonRenderer.NewMaterial` take a shared `MaterialDefaults` and bind its white textures and sampler for omitted ones, as `NewGPUMaterial` does. `GPUToonMaterial.Update` rewrites the uniform block without allocating but cannot swap textures or samplers.
- **Outlines**: the outline pipeline (`NewToonOutlinePipeline`, `gpu.CullFront`) pushes back faces out along their normals by `OutlineWidth` and fills them with `OutlineColor`; an outline alpha of `0` or a width of `0` turns it off. The width is in world units, or with `OutlineClipSpace` in NDC y units, which gives `OutlineWidth × viewport height / 2` pixels in every direction at every distance as long as the camera's aspect ratio matches the viewport's. `SmoothOutlineNormals` averages the normals of vertices at the same position (coordinates equal after rounding to 1e-5), each corner weighted by its angle, and returns an error wrapping `ErrInvalidMesh` for an invalid mesh.
- **Drawing**: `ToonRenderer.Draw` draws the outlines first and the cel-shaded surfaces second, opaque and with depth writes. It can run any number of times before a submission, into one pass or several: each call writes its per-draw uniforms after the slots earlier calls of the frame used. Inside a `gpu.Frame` the slots rewind on their own when `dev.FrameIndex()` advances; code that submits with `dev.Submit` directly calls `ToonRenderer.Begin` once per frame (at most `MaxToonDrawsPerFrame` draws between two calls). `Draw` returns a `gpu.Error` for a nil or closed renderer, device, pass, or camera bind group. For blended toon surfaces, build pipelines with `NewToonPipeline` and `NewToonOutlinePipeline` and a `ToonPipelineOptions.Blend`.
- **CPU reference and tests**: `EvaluateToon` and `ToonQuantizeBand` evaluate `ToonWGSL` in Go. `render3d`'s tests compare a multi-object scene with `render3d/testdata/golden/toon_scene.png` (`UPDATE_GOLDEN=1` rewrites it), draw in a headless `gpu.View` on both the full and `nsgotest.NoGPUCompute` profiles, restore a `Retained` renderer after a device loss, draw twice before one submission, and measure the clip-space outline on a wide viewport.

## Custom shader materials (`CustomMaterial`)

`NewCustomMaterial` compiles a custom 3D material written in **WGSL** (`ShaderWGSL`) or **Kage** (`ShaderKage`) with declared, strongly-typed parameters (`ShaderParamDesc`) onto an NSGo `gpu.Device`, managing its std140 uniform buffer, `@group(1)` bind group (`CustomMaterialGroup`, the same slot as `MaterialGroup`), 1×1 fallback textures, and render pipeline inside the owning `core.Scope`. Where `GPUMaterial` and `MaterialBindingsWGSL` give a custom shader the fixed glTF metallic-roughness bindings, `CustomMaterial` lets the shader declare its own parameters and textures:

```go
mat, err := render3d.NewCustomMaterial(cx, dev, render3d.CustomShaderDesc{
	Label:        "hologram",
	Language:     render3d.ShaderWGSL, // or render3d.ShaderKage
	CameraLayout: camBuf.Layout(),     // binds CameraWGSL at @group(0); omit for clip-space shaders
	DrawLayout:   draw.Layout(),       // optional PBRDrawBuffer at @group(2) for a per-draw model matrix
	DepthFormat:  gpu.FormatDepth24Plus,
	Cull:         gpu.CullBack,
	Retained:     true,                // survives GPU device loss and restore
	Params: []render3d.ShaderParamDesc{
		{Name: "intensity", Kind: render3d.ParamFloat, Default: float32(1.0)},
		{Name: "light_dir", Kind: render3d.ParamVec3, Default: math3d.Vec3{X: 0.4, Y: 0.85, Z: 0.35}},
		{Name: "tint", Kind: render3d.ParamColor, Default: core.RGB(0xf0c878)},
		{Name: "albedo", Kind: render3d.ParamTexture},
	},
	Source: myWGSLSource,
})

// Update parameters at runtime and flush dirty bytes before drawing:
_ = mat.SetFloat("intensity", 1.5)
_ = mat.SetColor("tint", core.RGB(0x55d0ff))
_ = mat.SetTexture("albedo", tex, samp) // nil tex restores 1x1 fallback; nil samp uses default linear-repeat
_ = mat.Sync(dev)                       // 0 allocations when unchanged

// In OnFrame:
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
pass.SetBindGroup(render3d.PBRDrawGroup, draw.BindGroup())
mat.Draw(pass, mesh) // sets pipeline, binds @group(1), and draws mesh
```

- **Declared parameter types and std140 layout**: `ParamFloat` (`f32`), `ParamVec2` (`vec2<f32>`), `ParamVec3` (`vec3<f32>`), `ParamVec4` / `ParamColor` (`vec4<f32>`), `ParamInt` (`i32`), `ParamBool` (`u32`, `0`/`1` for portable uniform structs), and `ParamTexture` (`texture_2d<f32>` + `<name>_sampler`). Non-texture parameters are packed automatically in declared order with WGSL/std140 alignment (`ComputeUniformLayout`, `ParamOffset`, `UniformSize`) at `@group(1) @binding(0)` (`material`), followed by each texture/sampler pair (`ParamBinding`). A float, int or bool parameter cannot directly follow a `ParamVec3`, because NSGo's Metal backend pads `vec3` struct members to 16 bytes where WGSL packs the next scalar into the last 4; declare it before the vec3 or after a vec2/vec4, or use `ParamVec4`. Unset texture slots receive an automatic 1×1 fallback texture (opaque white by default, customizable via `ShaderParamDesc.Default`).
- **WGSL and Kage shaders**: `ShaderWGSL` prepends `CameraWGSL` when `CameraLayout` is set and the source declares nothing in `@group(0)`, and prepends the generated `MaterialParams` / texture bindings unless `ManualBindings: true` (then the source declares `@group(1)` itself). `ShaderKage` parses Kage `var <Name> float|vec2|vec3|vec4|int|bool` declarations, helper functions, and `func Surface(uv vec2, normal vec3, worldPos vec3) vec4` (or `func Fragment(dst vec4, pos vec2) vec4`) and translates them into portable WGSL supporting `sample(<tex>, uv)` and `image0At(uv)`. Unlike Ebitengine's 2D Kage, `Fragment`'s `pos` is the mesh's normalized UV rather than a texel position, `dst` is the fragment's framebuffer position, and `imageNAt` samples with the texture's sampler (linear-repeat by default), so it wraps instead of returning transparent outside the image.
- **Placement and lights**: without `DrawLayout`, the Kage vertex stage passes positions and normals through unchanged, so `worldPos` and `normal` are in object space and the mesh draws where its vertices are. Set `DrawLayout` to a `PBRDrawBuffer`'s layout and bind its bind group at `PBRDrawGroup` (group 2): a Kage shader then transforms positions by the `PBRDrawUniform` model matrix and normals by its normal matrix, so `worldPos` and `normal` are in world space, while a WGSL shader declares the block itself as `LitPBRWGSL` does. `ExtraLayout` adds group 3 for bindings the shader declares, such as a `GPULightBuffer` declared with `LightWGSLForGroup(3)` (`LightWGSL` uses group 1, which holds the material) or a `DirectionalShadowMap` with `ShadowWGSL`; it requires `DrawLayout`. `Bind` and `Draw` bind only group 1, so the caller binds groups 0, 2 and 3.
- **Texture swaps**: `Sync` keeps the bind groups of the 8 most recently bound texture/sampler combinations, so alternating between a few textures (a flipbook, ping-pong render targets) reuses them without allocating; a material that cycles through more distinct textures than that creates a bind group per change.
- **Strict validation and cross-backend checks**: Typed setters (`SetFloat`, `SetVec2`, `SetVec3`, `SetVec4`, `SetColor`, `SetInt`, `SetBool`, `SetTexture`) and atomic `ApplyMap` reject unknown parameter names, type mismatches, non-finite values (`NaN`/`Inf`), and out-of-range values against optional `Min`/`Max` bounds with `*CustomMaterialError`, which matches both `ErrInvalidCustomMaterial` and `ErrInvalidMaterial`. Its `Line` is the source line, also for WGSL errors in code translated from Kage. `ApplyMap` binds a texture with the default sampler, as `SetTexture` with a nil sampler does. `CompileCheck` / `CompileCheckDesc` verifies shader lowering and cross-backend pipeline compilation across MSL, HLSL, GLSL 3.30, GLSL ES 3.00 (WebGL 2 / Android), and WGSL without an active `gpu.View`. `ByteSize` reports the uniform buffer and fallback textures for `RenderStats` and `Budget`.
- **Reference renders**: `render3d`'s tests verify both WGSL and Kage custom materials against golden reference PNGs (`render3d/testdata/golden/custom_material_wgsl.png` and `custom_material_kage.png`), cross-backend `PipelineCache` artifacts for all five targets, headless `gpu.View` rendering on both `full` and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles, `Retained` device-loss restoration, and zero-allocation steady-state updates; `UPDATE_GOLDEN=1` rewrites the reference images.

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

## HDR rendering, tone mapping, and gamma (`HDRTarget`, `ToneMapPass`)

Render the scene's linear radiance into an `HDRTarget` (`gpu.FormatRGBA16Float`, falling back to `gpu.FormatRGBA8Unorm` on devices where `SupportsHDR(dev)` is false), then resolve it to the display with `ToneMapPass`. render3d's own shaders tone-map and encode to sRGB by default, so draw into the HDR target with their linear output mode: `LitPBRPipelineOptions.LinearOutput` for lit PBR (with or without fog or IBL, through `NewLitPBRPipelineWithOptions`) and `MaterialPipelineOptions.LinearOutput` for materials. Without it the image is tone-mapped and gamma-encoded twice and the HDR target only stores values already clamped to `[0, 1]`.

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
			tonePass.SetDevice(dev) // retained resources come back in place after a device loss
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

		// 1. Render the scene in linear HDR radiance (values above 1.0 are kept):
		scenePass := frame.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x0a0e18), 1))
		scenePass.SetPipeline(pipe)
		scenePass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(scenePass)
		scenePass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
		mesh.Draw(scenePass)
		scenePass.End()

		// 2. Tone-map and encode to sRGB onto the swapchain:
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

`ToneMapOptions.Operator` chooses the highlight compression curve:

| Operator | Curve | Best for |
| --- | --- | --- |
| `ToneMapNone` | Linear clamp to `[0, 1]` | Debugging linear inputs |
| `ToneMapReinhard` | Per-channel `c / (1 + c)` | Simple soft highlight roll-off |
| `ToneMapACES` (`DefaultToneMapOptions`) | Narkowicz ACES filmic fit | High-contrast cinematic look |
| `ToneMapNeutral` | Khronos PBR Neutral | Faithful mid-tone hues and PBR asset viewing |
| `ToneMapAgX` | Troy Sobotka AgX default look | Smooth highlight desaturation without hue skew |

- **Exposure**: `ToneMapOptions.Exposure` is a linear multiplier; derive it from photographic stops with `ExposureFromEV(ev)` (`0 -> 1.0`, `+1 -> 2.0`, `-1 -> 0.5`, clamped to `[-126, 126]` stops so it is always positive). Exposure `0` renders black, so a fade to black can interpolate it down to `0`; negative, NaN and infinite values count as `1.0`. Start from `DefaultToneMapOptions()` so the field's zero value does not leave the image black.
- **Gamma and sRGB**: when `ToneMapOptions.Gamma <= 0` (the default) and `LinearOutput` is false, `ToneMapPass` applies the exact IEC 61966-2-1 linear-to-sRGB transfer function (`LinearToSRGBChannel` / `LinearToSRGB` on the CPU, with `SRGBToLinearChannel` / `SRGBToLinear` as their inverse); a positive `Gamma` (such as `2.2`) applies `pow(c, 1/Gamma)`. When `ToneMapPassDesc.TargetFormat.IsSRGB()` is true, the pass writes linear tone-mapped RGB so the sRGB framebuffer encodes it once, without double gamma. A material drawn with `LinearOutput` and resolved with `ToneMapReinhard` and `Gamma: 2.2` looks the same as the material pipeline's own output.
- **Overflow**: an `RGBA16Float` target stores values above 65504, such as strong specular highlights, as `+Inf`. The pass clamps exposed radiance to `[0, 65504]` and maps NaN to `0` before the curve (`ApplyToneMap` does the same), so blown highlights stay white instead of turning black.
- **Dynamic resolution**: create the `DynamicResolutionTarget` with an HDR `ColorFormat` (`PreferredHDRFormat(dev)`), draw the scene with `Begin3D`, and call `ToneMapPass.DrawDynamicResolution` in place of `Upscale`. It tone-maps only the scaled sub-viewport and upscales it with the pass's sampler in the same draw; the sharpen filter is not applied.
- **One set of options per submission**: each `ToneMapPass` has one uniform buffer, and buffer writes are applied before the submission's commands, so every draw one pass records into a frame uses the options of its last `Draw`. To tone-map several views with different options in one frame (split screen, a picture-in-picture camera), create one `ToneMapPass` per view.
- **Allocations and device loss**: `AppendToneMapUniforms` encodes the `ToneMapUniformSize` (48-byte) uniform block without allocating, and the pass reuses its bind group while the source texture stays the same; each draw still uploads the block with `gpu.Device.WriteBuffer`, which copies it. With `Retained: true` on `HDRTargetDesc` and `ToneMapPassDesc`, NSGo recreates the textures, sampler, uniform buffer, layout and pipeline after a device loss, and the pass rebuilds its bind group on the next draw.
- **CPU reference and portability**: `ApplyToneMap` mirrors `ToneMapWGSL` on the CPU. `ToneMapWGSL` avoids the `exp` and `log2` builtins (AgX uses a range-reduced polynomial `log2`), because NSGo v0.3.0's software GPU returns 0 for them, and runs on both the full GPU profile and `nsgotest.NoGPUCompute` (WebGL 2 / OpenGL ES 3.0).
- **Reference renders**: the tests draw a scene through the lit PBR pipeline's linear output mode, with emissive boxes and highlights well above 1.0, and compare it with `render3d/testdata/golden/tonemap_aces.png`, `tonemap_agx.png` and `tonemap_neutral.png`; `UPDATE_GOLDEN=1` rewrites them. Other tests check the lit PBR and material linear modes against `ShadePoint` and `EvaluateMaterial` through `ApplyToneMap`.

## Anti-aliasing (`MSAATarget`, `FXAAPass`)

`render3d` supports hardware multisample anti-aliasing (MSAA) where the GPU device offers it and a full-screen FXAA 3.11 post-processing pass for single-sampled paths, post-FX chains, or devices without MSAA:

| Mode (`AAMode`) | Behavior |
| --- | --- |
| `AANone` | Single-sampled (`1x`), no post-processing. |
| `AAFXAA` | Single-sampled render into an offscreen target followed by `FXAAPass`. |
| `AAMSAA2x` | Requests `2x` MSAA; `ClampSampleCount` promotes it to `4x` when `Features.MSAA4x` is true (WebGPU and NSGo expose `1x` and `4x` sample counts) and falls back to `1x` otherwise. |
| `AAMSAA4x` | Requests `4x` MSAA; resolves to `4x` when `Features.MSAA4x` is true and falls back to `1x` otherwise. |

`MSAATarget` wraps an offscreen color (and optional depth) `gpu.RenderTarget` whose sample count is clamped via `ClampSampleCount`. Call `Resize` on viewport size changes (redundant calls at the same dimensions are zero-allocation no-ops), pass `ColorAttachment` / `DepthStencilAttachment` (or `RenderPassDesc`) into `BeginRenderPass`, and read the single-sampled result from `OutputTexture` (the resolved texture when multisampled, or the color texture when single-sampled):

```go
target, err := render3d.NewMSAATarget(cx, dev, render3d.MSAATargetDesc{
	Label:       "scene",
	Width:       uint32(w),
	Height:      uint32(h),
	ColorFormat: gpu.FormatRGBA8Unorm,
	DepthFormat: gpu.FormatDepth24Plus,
	SampleCount: mode.SampleCount(), // clamped automatically via ClampSampleCount
	Retained:    true,
})
// In OnFrame:
desc, err := target.RenderPassDesc(clearColor, 1.0)
if err != nil {
	return // the target is closed or its device is lost
}
pass := frame.BeginRenderPass(desc)
// ... draw with a pipeline using SampleCount: target.SampleCount() ...
pass.End()
resolved := target.OutputTexture()
```

When MSAA is multisampled, `ColorAttachment` sets `ResolveTarget` to the resolve texture and `StoreOp` to `gpu.StoreDiscard` so tile GPUs avoid writing back the transient multisample buffer; when clamped to `1x`, it stores directly into the color texture with `gpu.StoreStore`. `ColorAttachment` and `RenderPassDesc` return an error for a closed target or a lost device rather than an attachment with a nil texture, which NSGo would draw into the view's swapchain.

- **Depth**: the depth attachment is transient by default: it is cleared at the start of the pass and discarded at the end (and so is the stencil of a depth-stencil format). Set `KeepDepth: true` on `MSAATargetDesc` to store it, so later passes can read `DepthTexture()`; the depth texture of a `4x` target is multisampled.
- **Sample count across devices**: the target keeps the requested sample count, and `Resize` clamps it against the current device, reallocating even at the same size when the clamped count changes. After `SetDevice` to a device with different MSAA support, call `Resize`, compare `SampleCount()` with its previous value, and rebuild the pipelines built with the old count.

For post-process anti-aliasing, `FXAAPass` compiles `FXAAWGSL` (a bufferless full-screen triangle FXAA 3.11 Quality shader using only `textureSampleLevel(..., 0.0)`, so it runs without compute shaders on both full and `NoGPUCompute` / WebGL 2 / OpenGL ES 3.0 profiles) and keeps one bind group per source texture, so steady-state draws do not allocate. The shader reads the texel size from the source with `textureDimensions`, so one pass can filter sources of different sizes in the same frame:

```go
fxaa, err := render3d.NewFXAAPass(cx, dev, render3d.FXAAPassDesc{
	TargetFormat: dev.SurfaceFormat(),
	Options:      render3d.DefaultFXAAOptions(),
	Retained:     true,
})
// Inside a single-sampled render pass (e.g. swapchain) or via fxaa.Encode(enc, src, dst):
err = fxaa.Draw(swapchainPass, target.OutputTexture())
```

- `FXAAOptions` tunes `SubpixelQuality` (default `0.75`, set `DisableSubpixel: true` for edge-only smoothing), `EdgeThreshold` (default `0.166`), and `EdgeThresholdMin` (default `0.0833`); non-finite or out-of-range fields are sanitized via `Sanitize()`.
- **One set of options per submission**: the options are one uniform block, and buffer writes are applied before the submission's commands, so every draw one `FXAAPass` records into a frame uses the options of its last draw. To filter several views with different options in one frame, create one `FXAAPass` per option set.
- `ApplyFXAA(src, opts)` runs the matching pure-Go CPU FXAA 3.11 reference on an `*image.RGBA`, leaving flat interiors below the contrast threshold untouched while smoothing diagonal edges.
- If a `Retained` `MSAATarget` or `FXAAPass` survives a device loss, call `SetDevice(dev)` in `OnReady` / `OnDeviceRestored` so subsequent `Resize` or uniform/bind-group updates target the restored device.
- **Color space**: FXAA's thresholds are tuned for perceptual, gamma-encoded luma, and `ApplyFXAA` works on the stored bytes. Feed the pass a non-sRGB source holding gamma-encoded (for example tone-mapped LDR) color, and draw into a target of the same kind. An sRGB source is sampled as linear values, so edge detection no longer matches `ApplyFXAA`, and a gamma-encoded source drawn into an sRGB target is encoded twice. `TargetFormat` defaults to `dev.SurfaceFormat()`, which is sRGB on some platforms, so set it explicitly.
- **Reference renders**: the software-renderer tests compare the scene with `aa_baseline.png`, `aa_msaa4x.png`, and `aa_fxaa.png` under `render3d/testdata/golden/` (`UPDATE_GOLDEN=1` rewrites them) and check `FXAAPass` against `ApplyFXAA`. On both the full and `NoGPUCompute` profiles, the tests check the MSAA output against the software renderer's reference render and the `FXAAPass` output against `ApplyFXAA`.

## Directional light shadows (`DirectionalShadowMap`, `DirectionalShadowConfig`)

`DirectionalShadowMap` manages an offscreen `FormatDepth32Float` shadow map render target, a depth-only caster pipeline (`ShadowDepthWGSL`) with a dynamic-offset caster transform buffer, a 96-byte `ShadowUniforms` buffer, and both nearest (hard) and linear (PCF) comparison samplers on an NSGo `gpu.Device`. `ShadowWGSL` declares the matching bindings at `ShadowGroup` (group 3, after `CameraGroup`, `LightGroup` and `PBRDrawGroup`) together with a portable `sample_directional_shadow(world_pos, world_normal)` helper. `NewLitPBRShadowPipeline` (or `LitPBRPipelineOptions.ShadowLayout`) builds the lit PBR pipeline with the shadow (`LitPBRShadowWGSL`): it shades a `LightRig` exactly like `LitPBRWGSL` and multiplies the rig's first directional light, `Directional[0]`, by the shadow, leaving the other lights, ambient and emission unshadowed. Group 3 is also where fog, IBL and skinning go, so a lit PBR pipeline takes one of them.

```go
sm, err := render3d.NewDirectionalShadowMap(cx, dev, render3d.ShadowMapOptions{
	Resolution:          1024,
	PCFKernel:           render3d.ShadowFilterPCF3x3, // or ShadowFilterHard
	DepthBias:           2,
	DepthBiasSlopeScale: 2.0,
	CullMode:            gpu.CullBack,
	Retained:            true, // survives GPU device loss automatically
})
litPipe, err := render3d.NewLitPBRShadowPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), sm.Layout(), gpu.FormatDepth24Plus)

// Each frame in OnFrame(frame *gpu.Frame):
dev := frame.Device()
cfg := render3d.DefaultDirectionalShadowConfig()
cfg.Direction = rig.Directional[0].Direction // the light the shadow attenuates
cfg.Strength = 1
if !preset.Shadows { // quality.Preset from the quality package
	cfg.Strength = 0 // the shader then skips the shadow map
}
_, err = sm.UpdateBounds(dev, cfg, sceneBounds) // fits the orthographic frustum, texel-snapped

// 1. Depth-only shadow pass (nil while the device is lost or after Close):
if preset.Shadows {
	if shadowPass := sm.BeginShadowPass(dev, frame); shadowPass != nil {
		sm.DrawCaster(shadowPass, pillarMesh, pillarModel)
		sm.DrawCaster(shadowPass, crateMesh, crateModel)
		shadowPass.End()
	}
}

// 2. Main lit pass:
mainPass := frame.BeginRenderPass(gpu.RenderPassDesc{})
mainPass.SetPipeline(litPipe)
mainPass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
mainPass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
mainPass.SetBindGroup(render3d.ShadowGroup, sm.BindGroup())
mainPass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
floorMesh.Draw(mainPass)
mainPass.End()
```

- **Framing and texel snapping**: `DirectionalShadowConfig.LightViewProj()` builds an orthographic frustum `OrthoSize` wide and `DepthRange` deep, centred on `Target` (which sits at depth 0.5). `FitBounds(lightDir, sceneBounds)` encloses an `AABB`, extending toward the light by `CasterPadding`. With `TexelSnap: true` the shadow map's texel grid stays fixed in world space: the frustum is sized from the bounds' bounding sphere, rounded up to a coarse step, so the texel size does not change as the bounds move or the light turns, and the light-space center snaps to whole texels, so moving the framing (for example `UpdateBounds` following the camera) shifts static shadows by whole texels and their edges do not shimmer. Without `TexelSnap`, `FitBounds` hugs the bounds' light-space extent for the most texels per world unit, at the cost of shimmering when the bounds move.
- **Hard 1-tap vs 3x3 PCF filtering**: `ShadowFilterHard` uses a nearest comparison sampler with a single `textureSampleCompareLevel` tap; `ShadowFilterPCF3x3` averages 9 comparison taps with a linear comparison sampler (combining hardware 2x2 PCF per tap with a 3x3 neighborhood) for soft penumbras. Switching `PCFKernel` via `WriteUniforms`, `Update`, or `UpdateBounds` swaps `BindGroup()` dynamically without allocating or recreating GPU resources.
- **Acne control and portability**: `DepthBias` (slope-scaled in the shader by `1 + 2*(1 - NdotL)`), world-space `NormalBias`, and hardware rasterizer bias (`ShadowMapOptions.DepthBias` / `DepthBiasSlopeScale`) suppress shadow acne across flat and grazing surfaces. Custom shaders can prepend `ShadowWGSL` (or `ShadowWGSLForGroup(n)` when group 3 holds something else) and call `sample_directional_shadow(world_pos, world_normal)`; `LightWGSL`'s `shade_pbr_shadowed` takes that visibility for directional light 0. No compute shaders or storage buffers are used, so directional shadows run on every NSGo target including WebGL 2 and OpenGL ES 3.0.
- **Quality presets**: to honour `quality.Preset.Shadows`, skip the shadow pass and set `Strength` to 0 when it is off, as in the example; with `Strength` 0 the shader returns full visibility without sampling the map.
- **Zero-allocation uniforms and CPU reference**: `AppendShadowUniforms` and `ShadowUniforms.Encode` pack the 96-byte uniform block with zero heap allocations; a NaN or infinite matrix or `Strength` disables the shadow, non-finite biases become 0 and an unknown `PCFKernel` is treated as `ShadowFilterHard`, so uniforms built by hand cannot put NaN on screen. `EvaluateDirectionalShadow` is the matching pure-Go CPU reference over a `ShadowDepthGrid` (decoded from GPU depth readback via `ShadowDepthGridFromBytes`), and `ShadePointShadowed` is the CPU reference of the shadowed lighting. `render3d`'s tests check every open floor pixel of hard and 3x3 PCF renders, penumbras included, against these references, check where the shadow falls by projecting a caster along the light, and compare the renders with golden PNGs (`render3d/testdata/golden/shadow_directional_hard.png` and `shadow_directional_pcf.png`) on both `gpu.View` profiles (`full` and `nsgotest.NoGPUCompute`), across `Retained` device-loss restoration, and through scope disposal.

## Render-to-texture cameras and multiple viewports (`RenderTexture`, `ViewportRenderer`)

`RenderTexture` allocates a scope-owned offscreen `gpu.RenderTarget` (color, optional depth/stencil, and optional 4x MSAA with automatic resolve) whose sampleable color output is exposed directly as a `*GPUTexture` via `RenderTexture.GPUTexture()`. Pass that handle to `BindTexture` (or any material texture slot) so mirrors, minimaps, security monitors, and portals render from a secondary camera and appear as textures on 3D geometry in the main pass:

```go
rt, err := render3d.NewRenderTexture(cx, dev, render3d.RenderTextureDesc{
	Label:       "mirror",
	Width:       256,
	Height:      256,
	DepthFormat: gpu.FormatDepth24Plus,
	Retained:    true, // survives device loss alongside retained samplers and bind groups
})
mirrorBG, err := render3d.BindTexture(cx, dev, texLayout, rt.GPUTexture(), samp)
```

`ViewportRenderer` orchestrates frames with any mix of offscreen `RenderTexture` cameras and multiple viewports on the main surface (side-by-side split screen, stacked split screen, 4-player quad split, or overlapping picture-in-picture overlays):

```go
renderer, err := render3d.NewViewportRenderer(cx, dev) // list renderer.CameraLayout() at CameraGroup

halves := render3d.SplitScreenHorizontal(2) // left [0..0.5], right [0.5..1]
views := []render3d.CameraView{
	{Name: "mirror", Camera: mirrorCam, Target: rt, LayerMask: layerWorld, ClearColor: core.RGB(0x182030)},
	{Name: "player1", Camera: p1Cam, Viewport: halves[0], LayerMask: layerWorld | layerMirror, ClearColor: core.RGB(0x0c1018)},
	{Name: "player2", Camera: p2Cam, Viewport: halves[1], LayerMask: layerWorld | layerMirror},
}

err = renderer.RenderFrame(frame, render3d.SurfaceDesc{HasDepth: true}, views, func(ctx render3d.ViewportContext) {
	visible = render3d.FilterLayeredItems(sceneItems, ctx.LayerMask, visible[:0])
	plan := queue.Build(ctx.Uniform.ViewProj, ctx.Camera.Pos, visible)
	// ctx.Pass already has SetViewport, SetScissor, and CameraGroup bound for this view.
	drawPlan(ctx.Pass, plan)
})
```

- **Execution order**: `ViewportRenderer` always renders offscreen views (`Target != nil`) before main-surface views (`Target == nil`), ordering each phase stably by ascending `CameraView.Priority` (`OrderCameraViews`), so offscreen textures are up to date before any main-pass material samples them.
- **Per-view camera uniforms & aspect ratio**: each active view gets its own retained uniform buffer slot at `CameraGroup` uploaded before pass recording so multiple views in one `gpu.Frame` or `gpu.Encoder` never clobber each other's matrices. Unless `CameraView.PreserveAspect` is set, a per-view copy of `Camera` adjusts `Projection.Aspect` to the resolved `PixelViewport` without mutating the caller's `*scene3d.Camera`. The slots are reused by every call, so call `RenderFrame` or `RenderEncoder` (which takes the device the encoder was begun on) at most once per submitted frame or command buffer, and use a separate `ViewportRenderer` for each call that shares one submission.
- **Per-camera layer masks**: `CameraView.LayerMask` matches `scene3d.Node.Layer` bitmasks (zero defaults to `scene3d.LayerAll`). `LayerMatches`, `FilterLayeredItems`, and `FilterNodesByLayer` filter `LayeredDrawItem` and `*scene3d.Node` slices with zero steady-state allocations when reusing the destination buffer. In those functions, as in `scene3d.Decal.LayerMask`, a zero mask matches nothing, so pass `ViewportContext.LayerMask` (already the effective mask); set `CameraView.Disabled` for a camera that renders nothing. `FilterNodesByLayer` tests the layer only and keeps hidden or disabled nodes.
- **Split-screen seams & overlays**: `SplitScreenHorizontal` (`SplitScreenSideBySide`), `SplitScreenVertical` (`SplitScreenStacked`), `SplitScreenQuad`, and `ViewportFromPixels` construct normalized `[0, 1]` `ViewportRect` values. `ViewportRect.Resolve` rounds both edges independently so adjacent split-screen viewports tile with zero pixel gaps or overlap, and `PixelViewport.Apply` sets both `SetViewport` and `SetScissor` so geometry never bleeds across seams. `PixelViewport` is in physical pixels; `PixelViewport.Bounds(pixelsPerPoint)` divides by the surface's content scale (its pixel width over the view's width in points) to give the logical-point `core.Bounds` that `DebugDraw.Flush`, `Gizmo.DrawGizmo` and `BillboardRenderer.Draw` take. Consecutive views on the same target share one GPU render pass, and only the view that opens a pass clears it, so a later view in that pass ignores its own `ClearColor` (draw a full-viewport quad for a distinct background); set `CameraView.ResetDepth` on an overlapping picture-in-picture view to preserve the underlying color while clearing depth. A view that returns to an offscreen target after views on another target (for example two halves of one atlas with a different target between them by `Priority`) loads that target's color and depth instead of clearing them, so the earlier views' output is kept.
- **Resize & device loss**: `RenderTexture.Resize` is a no-op when dimensions match and swaps atomically on change, incrementing `Generation()` so callers know when to rebind `BindTexture` bind groups. With `Retained: true`, `RenderTexture` and `ViewportRenderer` restore automatically after GPU device loss and are released with their owning `core.Scope` or `Close()`.

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

Run the game with [`nsgo game dev`](cli.md#dev) and attach `hotreload.ShaderReloader` and `hotreload.MaterialReloader` (from `github.com/nsgo-dev/game/packages/game/tooling/hotreload`) to `hotreload.NewDevWatcher` so edited WGSL (`.wgsl`), Kage (`.kage`), and material YAML (`.material.yaml`, `.mat.yaml`, `.material.yml`, `.mat.yml`) files reload within a second without restarting:

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

- **Safe fallback on errors**: if a shader edit fails to compile, misses an entry point, or fails pipeline creation — or if a material YAML has invalid syntax, an unknown field, or an out-of-range value — the previous working shader, material, and `*gpu.Pipeline` remain active without crashing. The failure is recorded as a `ReloadError` (unwrapping to `*loaderr.Error` with file, line, and column when reported) accessible via `LastError` and `Errors`, and clears automatically once a subsequent save compiles cleanly. When debouncing is enabled, `Watcher` also gives 0-byte reads extra time to settle so editors that save by truncating before writing do not emit a spurious empty-source error, while a file that genuinely stays empty still reports it.
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

## Baked lightmaps (`LightmapMesh`, `LightmapRenderer`)

`LightmapMesh` and `GPULightmapMesh` add a dedicated secondary UV channel (`UV1`, matching glTF `TEXCOORD_1`) alongside primary material UVs (`UV0`, `TEXCOORD_0`) in a 40-byte interleaved vertex layout (`LightmapVertexLayout`, `LightmapVertex`). When `UV1` is empty, `UploadLightmapMesh` falls back cleanly to `UV0` and reports `GPULightmapMesh.UsedUV0Fallback()`. In `game/gltf`, `Document.MaterialLightmap` extracts baked lightmap bindings from `MOZ_lightmap`, `EXT_lightmap`, or `extras.lightmap` (including `KHR_texture_transform` / atlas `uvScale` and `uvOffset`), and `Document.LightmapTexCoords` / `PrimitiveLightmapUVs` resolve `TEXCOORD_1` with `TEXCOORD_0` fallback.

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

// Each frame inside OnFrame:
renderer.BeginFrame()
camBuf.WriteCamera(cam)
mat := render3d.DefaultLightmapMaterial()
mat.BaseColorTexture = albedoTex
mat.Lightmap = lightmapTex // RGBM / RGBD: upload with TextureUpload{SRGB: false}
mat.Encoding, err = render3d.ParseLightmapEncoding(lmRef.Encoding) // "" is LightmapLinear
mat.Intensity = lmRef.Intensity
mat.RGBMMaxRange = lmRef.RGBMMaxRange // 0 uses DefaultRGBMMaxRange
mat.SetUVTransform(render3d.UVTransform{Scale: lmRef.UVScale, Offset: lmRef.UVOffset})
mat.DirectLight = &rig.Directional[0] // optional: a LightRig light, Direction is the travel direction
_ = renderer.Draw(pass, camBuf, lmMesh, worldMat, mat)
```

- **HDR and LDR encodings**: supports linear 8-bit or float16 HDR textures (`LightmapLinear`, with `UploadLightmapRGBA16F` for float16 upload), sRGB LDR textures (`LightmapSRGB`, avoiding double-decode when uploaded with `TextureUpload{SRGB: true}`), and 8-bit packed HDR formats (`LightmapRGBM` via `EncodeRGBM` / `EncodeRGBMImage` and `LightmapRGBD` via `EncodeRGBD` / `EncodeRGBDImage`) for full WebGL 2 and mobile compatibility.
- **Shared lightmap atlases**: set `LightmapMaterial.UVScale` and `UVOffset` (or `SetUVTransform`) so multiple meshes sample distinct sub-rectangles of one baked lightmap atlas within a single render pass using dynamic uniform offsets.
- **Fallback and dynamic light blend**: leave `LightmapMaterial.Lightmap` nil to shade with `AmbientFallback`, and optionally set `LightmapMaterial.DirectLight` to a `*DirectionalLight` (the same type as a `LightRig`'s, with `Direction` the way the light travels) to add its Lambertian N·L term to the baked irradiance. Normals go through the normal matrix, so non-uniformly scaled level geometry is lit correctly. `Intensity` 0 means no baked light; `DefaultLightmapMaterial` sets 1.
- **Output encoding**: the shader encodes its linear result to sRGB, as `LitPBRWGSL` and the material pipeline do, so lightmapped geometry and PBR-lit props drawn into the same pass match. For an sRGB color format (`LightmapRendererOptions.ColorFormat` or the surface format) the encode is left to the hardware.
- **Atlas transform**: `LightmapRef.UVScale` / `UVOffset` are the authoritative atlas transform; `LightmapRef.Texture` carries no scale or offset, so do not also apply it with `TransformTexCoords`. See [Baked lightmaps](3d-models-blender.md#baked-lightmaps) for where the values come from.
- **Texture streaming**: `BeginFrame` drops cached bind groups of lightmap and albedo textures that have been closed, so streaming levels or hot-reloading lightmaps does not grow the cache.
- **Scope**: `LightmapRenderer` is a standalone Lambertian path, albedo times (baked irradiance plus one optional `DirectionalLight`). It has no metallic-roughness, normal, occlusion, or emissive maps and does not read a `LightRig`.

### PBR lightmap path (`GPUPBRLightmap`, `NewPBRLightmapPipeline`, `UploadGLTFPBRLightmaps`)

To keep full glTF 2.0 metallic-roughness PBR materials (base color, metallic-roughness, normal, occlusion, emissive, alpha modes, double-sided faces) and dynamic `LightRig` lighting (directional, point, and spot lights) on baked geometry, use `NewPBRLightmapPipeline` together with `GPUMaterial` and `GPUPBRLightmap`:

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

// Inside OnFrame:
pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
lightBuf.Bind(pass)
pass.SetBindGroup(render3d.PBRLightmapMaterialGroup, matSet.Material(prim.Material).BindGroup())
lmSet.Lightmap(prim.Material).Bind(pass) // or a per-node instance from lmSet.NewInstance(...)
lmMesh.Draw(pass)
```

- **Bind groups & vertex layout**: reuses `LightmapVertexLayout` (40 bytes: `Position`, `Normal`, `UV0`, `UV1`) with `CameraGroup` (0), `LightGroup` (1, `GPULightBuffer`), `PBRLightmapMaterialGroup` (2, `GPUMaterial` from `NewGPUMaterial` or `UploadGLTFMaterials`), and `PBRLightmapGroup` (3, `GPUPBRLightmap` carrying model/normal matrices, atlas `UVTransform`, `LightmapEncoding`, `Intensity`, `RGBMMaxRange`, and the lightmap texture/sampler).
- **Shading equation**: the decoded lightmap replaces the ambient / indirect diffuse term as `(1 - metallic) * baseColor * decodedLightmap * intensity * occlusion` (so `LightRig.Ambient` is not double-counted on baked geometry), while `LightRig`'s directional, point, and spot lights supply the Cook-Torrance GGX direct term plus emissive, encoded with `linear_to_srgb` (or unclamped linear when `LinearOutput` is set or `ColorFormat` is sRGB). For a dielectric (`metallic = 0`), rough (`roughness = 1`), untextured material with no dynamic lights it matches `LightmapRenderer`; adding a `LightRig` light adds the exact direct term of `LitPBRWGSL`. `EvaluatePBRLightmap` is the CPU reference.
- **glTF integration**: `UploadGLTFPBRLightmaps` reads `MOZ_lightmap`, `EXT_lightmap`, and `extras.lightmap` (including `KHR_texture_transform`, `scaleOffset`, `uvScale`, `uvOffset`, `intensity`, `encoding`, `rgbmMaxRange`), deduplicates lightmap textures/samplers, and returns a `GLTFPBRLightmapSet` matching `GLTFMaterialSet` (use `NewInstance` for multiple nodes sharing one material at different transforms).

### When to use `LightmapRenderer` vs the PBR lightmap path

- **Use `LightmapRenderer`** for lightweight static architecture, low-end/mobile fill-rate budgets, or diffuse-only baked geometry that only needs an albedo texture (`UV0`), a baked lightmap (`UV1`), and at most one dynamic Lambertian directional sun light — it binds only 2 textures per draw and handles per-frame dynamic uniform ring offsets automatically via `BeginFrame` / `Draw`.
- **Use the PBR lightmap path (`NewPBRLightmapPipeline` + `GPUPBRLightmap`)** whenever baked geometry also needs metallic-roughness maps, tangent-space normal maps, ambient occlusion maps, emissive maps, alpha masking/blending, double-sided surfaces, HDR `LinearOutput`, or dynamic directional/point/spot specular and diffuse lighting from a `LightRig` in a single pipeline.

## Point and spot light shadows (`SpotShadowMap`, `PointShadowMap`)

`SpotShadowMap` and `PointShadowMap` shadow the first spot light (`rig.Spot[0]`) and the first point light (`rig.Point[0]`) of a `LightRig`. They render depth-only `FormatDepth32Float` maps from the rig's own `SpotLight` and `PointLight`, and the lit PBR pipelines multiply that light's contribution by the shadow, so a shadowed light is shaded exactly like the same light in `LitPBRWGSL` (Cook-Torrance GGX, energy-conserving Lambert, `PointLightAttenuation`, glTF cone falloff). Everything runs on every NSGo backend, the WebGL 2 / OpenGL ES 3.0 profile (`nsgotest.NoGPUCompute`) and the software renderer included:

```go
spot, err := render3d.NewSpotShadowMap(cx, dev, render3d.DefaultSpotShadowConfig())
litPipe, err := render3d.NewLitPBRSpotShadowPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBufs[0].Layout(), spot.Layout(), gpu.FormatDepth24Plus)

// Each frame in OnFrame:
_ = spot.Update(dev, rig.Spot[0]) // the light the shadow attenuates
if sp := spot.BeginShadowPass(dev, frame); sp != nil {
	spot.DrawCaster(sp, pillarMesh, pillarModel) // false when the pass is full
	sp.End()
}

mp := frame.BeginRenderPass(gpu.RenderPassDesc{ /* color + depth */ })
mp.SetPipeline(litPipe)
mp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
mp.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
mp.SetBindGroup(render3d.LocalShadowGroup, spot.BindGroup())
for i, mesh := range meshes {
	mp.SetBindGroup(render3d.PBRDrawGroup, drawBufs[i].BindGroup()) // per-draw model matrix and material
	mesh.Draw(mp)
}
mp.End()
```

For a point light, update from `rig.Point[0]`, draw with `NewLitPBRPointShadowPipeline`, and render the cube faces:

```go
_ = point.Update(dev, rig.Point[0])
for face := range gpu.CubeFace(render3d.PointShadowFaceCount) {
	if fp := point.BeginFacePass(dev, frame, face); fp != nil {
		point.DrawCaster(fp, blockerMesh, blockerModel)
		fp.End()
	}
}
```

- **Spot projection**: a square `[0, 1]`-depth perspective from the light's `Position` along its `Direction` with a field of view of `2 * OuterConeAngle` (`SpotShadowViewProj`). The far plane is the light's `Range`, or `SpotShadowConfig.Far` (default `DefaultLocalShadowFar`, 50) for a light with `Range` 0; `Near` defaults to 0.1. A cone wider than `MaxSpotShadowConeAngle` (just under 90°) cannot be projected and `Update` returns `ErrInvalidLocalShadow`.
- **Point atlas**: the six 90° cube faces are packed into one `(3 * FaceResolution) × (2 * FaceResolution)` 2D depth atlas, because NSGo cannot attach a single face of a depth cube map. `sample_point_shadow` picks the dominant face, computes the perspective depth `(far/(far-near)) * (1 - near/axisDist)` directly, and clamps every tap inside its tile so 3×3 PCF never bleeds into a neighboring face. `BeginFacePass` with face 0 clears the whole atlas and faces 1..5 keep what is there, so a partial refresh either includes face 0 with every other face or skips it.
- **Casters**: `DrawCaster` writes each caster's `(lightViewProj, model)` into its own 256-byte dynamic-offset slot, so many casters draw in one pass. `MaxCasters` (default 256, at most `MaxLocalShadowCasters`) applies per spot pass and per point face; each face has its own slots, so faces can be refreshed in any order. `DrawCaster` returns false when the pass or face is full or `Update` has not run yet.
- **Filtering and bias**: `LocalShadowFilterHard` (one nearest comparison tap) or `LocalShadowFilterPCF3x3` (nine linear taps), with a constant `DepthBias` and a world-space `NormalBias`.
- **Bind group 3**: `LocalShadowGroup` is `ShadowGroup` (3), the group the directional shadow, fog, IBL and skinning also use, so a lit PBR pipeline takes one of them (`LitPBRPipelineOptions.SpotShadowLayout` or `PointShadowLayout`). A custom shader that needs both, or group 3 for something else, places the bindings with `SpotShadowWGSLForGroup` / `PointShadowWGSLForGroup` and scales its lights with `shade_pbr_visibility(…, directional0_vis, point0_vis, spot0_vis)` from `LightWGSL`.
- **Device loss and CPU references**: with `Retained: true` the depth target, sampler, buffers, bind groups and depth pipeline come back after a device loss. `EvaluateSpotShadow`, `EvaluatePointShadow` and `PointShadowSelectFace` mirror the WGSL, and `ShadePointVisibility` with a `LightVisibility` reproduces the lit pixel; the tests compare them with the GPU and with the golden renders `render3d/testdata/golden/local_shadow_spot.png` and `local_shadow_point.png` (`UPDATE_GOLDEN=1` rewrites them).

## Cascaded shadow maps (`CascadedShadowMap`, `CSMConfig`)

`CascadedShadowMap` shadows the first directional light of a `LightRig` (`rig.Directional[0]`) with up to four cascades (`MaxCascades`), so shadows stay sharp near the camera and still reach the horizon. It is the large-scene alternative to `DirectionalShadowMap`: the lit PBR pipelines shade the rig exactly like `LitPBRWGSL` and multiply that one light by the cascaded shadow, leaving the other lights, ambient and emission unshadowed. Everything runs on every NSGo backend, the WebGL 2 / OpenGL ES 3.0 profile (`nsgotest.NoGPUCompute`) and the software renderer included:

```go
cfg := render3d.DefaultCSMConfig() // 4 cascades, 1024 texels each, 3x3 PCF, texel snapping
cfg.MaxShadowDistance = 80
cfg.Retained = true
csm, err := render3d.NewCascadedShadowMap(cx, dev, cfg)
litPipe, err := render3d.NewLitPBRCSMPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBufs[0].Layout(), csm.Layout(), gpu.FormatDepth24Plus)

// Each frame in OnFrame(frame *gpu.Frame):
shadowFrame := render3d.FitCascadesFromCamera(cam, rig.Directional[0].Direction, csm.Config()) // the light the cascades shadow
_ = csm.Update(dev, shadowFrame)
for i := range csm.CascadeCount() {
	if pass := csm.BeginCascadePass(frame, i); pass != nil {
		for j, mesh := range casterMeshes {
			csm.DrawCaster(pass, mesh, casterModels[j]) // false once MaxCasters distinct models are taken
		}
		pass.End()
	}
}

mp := frame.BeginRenderPass(gpu.RenderPassDesc{ /* color + depth */ })
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

- **Splits (`ComputeCSMSplits`)**: `[near, min(far, MaxShadowDistance)]` is divided with the practical split scheme, blending logarithmic and uniform splits by `SplitLambda` (default 0.75).
- **Fitting (`FitCascades`, `FitCascadesFromCamera`)**: each cascade's slice of the camera frustum is cut along the projection's own near and far planes, so perspective and orthographic cameras both work. `FitCascadesFromCamera` uses the near and far planes `Camera.ProjMatrix` uses; shadows start at view depth 0.1 when an orthographic camera's near plane is at or behind it. With `TexelSnap` (the default) each cascade is sized from a bounding sphere and snapped to whole texels, so shadow edges do not shimmer as the camera moves or turns. `CasterPadding` pulls each cascade's near plane toward the light so casters outside the view still cast into it. Fitting and `CSMFrame.EncodeUniforms` allocate nothing.
- **One 2D depth atlas**: the cascades are tiles of one `FormatDepth32Float` texture (1x1 for one cascade, 2x1 for two, 2x2 for three or four), so no depth texture arrays are needed. `Update` refuses, with `ErrInvalidCSM`, a frame fitted with another cascade count, resolution or filter than the atlas, because its tiles, texel size and filter would not match the atlas and its sampler. The first `BeginCascadePass` after `Update`, whichever cascade it draws, clears the whole atlas; render every cascade after each `Update`, in any order.
- **Casters**: `DrawCaster` writes each distinct model matrix into its own 256-byte dynamic-offset slot once per frame and reuses it in the other cascades, so `MaxCasters` (default 256) counts distinct models between two `Update` calls, not draws. When the slots run out it draws nothing and returns false. `DrawCasterGroup` draws with caller-owned transforms instead.
- **Filtering and blending**: `ShadowFilterHard` (one nearest comparison tap) or `ShadowFilterPCF3x3` (nine linear taps clamped inside the cascade's tile), the same `ShadowFilter` the single directional shadow map uses. `CascadeBlendRange` cross-fades into the next cascade over that fraction of each cascade and fades the last one out toward `MaxShadowDistance`; `DebugCascades` tints surfaces by cascade (red, green, blue, yellow).
- **Bind group 3**: `CSMGroup` is `ShadowGroup` (3), so a lit PBR pipeline takes the cascaded shadow map instead of the directional, spot or point shadow, fog, IBL or skinning (`LitPBRPipelineOptions.CSMLayout`). Custom shaders prepend `CameraWGSL` and `CSMWGSL` (or `CSMWGSLForGroup(n)` when group 3 holds something else) and pass `sample_csm_shadow(world_pos, world_normal)` to `shade_pbr_shadowed` from `LightWGSL`.
- **Device loss and CPU references**: with `Retained: true` the atlas, sampler, buffers, bind groups and depth pipeline come back after a device loss. `EvaluateCSMShadow` mirrors the WGSL over an atlas read back with `ReadCSMDepthAtlas` / `ReadCSMDepthAtlasAsync`, and `ShadePointShadowed` with that visibility, times `CSMFrame.DebugTint`, reproduces the lit pixel; the tests compare them with the GPU on both profiles and with the golden render `render3d/testdata/golden/csm_large_scene.png` (`UPDATE_GOLDEN=1` rewrites it).

## 3D post-processing (`PostStack3D`, `BloomPass`, `SSAOPass`, `DepthOfFieldPass`)

`PostStack3D` composes screen-space ambient occlusion (`SSAOPass`), depth-aware disc bokeh Depth of Field (`DepthOfFieldPass`), and HDR Bloom (`BloomPass`) over an offscreen 3D color+depth `gpu.RenderTarget` (`FormatRGBA16Float` + `FormatDepth32Float` by default) and writes the final image to the swapchain or a chosen destination texture:

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

// In OnFrame:
pass := stack.BeginScenePass(frame, core.RGB(0x0c1018))
// ... draw 3D meshes into stack.SceneTarget() ...
pass.End()
_ = stack.ApplyScene(frame.Device(), frame, nil) // SSAO -> Depth of Field -> Bloom -> swapchain
frame.Submit()

// When the view resizes (or the camera zooms), refresh the projection SSAO and DoF depend on:
_ = stack.Resize(dev, uint32(w), uint32(h))
cam.SetViewport(float32(w), float32(h))
_ = stack.SetCamera(cam)
```

- **Composable order and standalone passes**: `PostStack3D` runs enabled stages in fixed order — **SSAO -> Depth of Field -> Bloom** — routing intermediate results through ping-pong offscreen targets and writing the final stage directly in `OutputFormat` (or blitting cleanly when all stages are disabled). `SetSSAO`, `SetDepthOfField` and `SetBloom` toggle a stage; a zero config keeps its current settings, so `stack.SetSSAO(false, render3d.SSAOConfig{})` only turns SSAO off. Each pass (`SSAOPass`, `DepthOfFieldPass`, `BloomPass`) can also be constructed and applied standalone against any caller-owned `gpu.Texture` color and depth attachments via `RenderPassRecorder` (satisfied by both `*gpu.Frame` and `*gpu.Encoder`).
- **Formats and destinations**: `ColorFormat` (scene and intermediate targets) is `FormatRGBA16Float` (default), `FormatRGBA8Unorm`, `FormatRGBA8UnormSRGB`, `FormatBGRA8Unorm` or `FormatBGRA8UnormSRGB`: four filterable channels, because Depth of Field stores its Circle of Confusion in alpha. A nil destination writes to the swapchain in `OutputFormat`; a destination texture must be in `ColorFormat` or `OutputFormat`, and the last stage draws with the pipeline built for that format. Every input and destination must have the pass or stack size, and the destination cannot also be an input; otherwise `Apply` returns `ErrInvalidPostProcess`.
- **Camera projection**: `SSAOConfigFromCamera` captures `cam.ProjMatrix()` and `DepthOfFieldConfigFromCamera` reads the near and far planes and projection kind from that matrix, so the defaults `scene3d.Projection` fills in for zero fields (and an orthographic near plane at or behind the camera) match the depth buffer. Both are captured once; call `PostStack3D.SetCamera` (or `SetConfig` on a standalone pass) whenever the projection changes. With a zero `ProjMatrix`, SSAO falls back to a 60° perspective at the pass aspect ratio with the config's `Near` and `Far`.
- **SSAO (`SSAOConfig`, `SSAOPass`)**: reconstructs view-space positions and edge-preserving normals from the 3D depth buffer, evaluates a deterministic 4–16-sample hemisphere kernel with stratified 4×4 tangent rotation, applies an optional depth-aware bilateral blur (`BlurRadius`, `DepthThreshold`), and multiplies ambient occlusion into the scene color (`AOTexture` exposes the AO map for inspection).
- **Depth of Field (`DepthOfFieldConfig`, `DepthOfFieldPass`)**: linearizes perspective or orthographic `[0, 1]` depth to compute a signed Circle of Confusion around `FocusDistance` ± `FocusRange`, then gathers an 18-tap two-ring disc bokeh blur that prevents blurred background pixels from bleeding across sharp in-focus silhouettes.
- **Bloom (`BloomConfig`, `BloomPass`)**: extracts bright LDR or HDR (`> 1.0`) pixels with a quadratic `Threshold` / `SoftKnee` curve, runs `BlurPasses` separable 9-tap Gaussian horizontal and vertical passes (optionally at half resolution via `HalfRes`), and composites the glow additively with `Intensity`.
- **Portability, lifecycle, and reference renders**: every pass uses fragment-only portable WGSL with explicit `textureSampleLevel` and algebraic/precomputed weights (no compute shaders or `exp`/`log2` instructions), running on both full and `nsgotest.NoGPUCompute` (WebGL 2 / OpenGL ES 3.0) profiles, supporting `Resize(dev, w, h)` and `Retained` device-loss recovery. Golden reference renders (`render3d/testdata/golden/post3d_bloom.png`, `post3d_ssao.png`, `post3d_dof.png`, and `post3d_combined.png`) verify each pass and the combined stack; `UPDATE_GOLDEN=1` rewrites them.

## Clustered forward lighting (`ClusteredLightSystem`, `ClusterGrid`)

A `LightRig` shades at most 16 point and 8 spot lights. For scenes with hundreds of local lights, `ClusteredLightSystem` takes the place of `GPULightBuffer` at `LightGroup`: it uploads the same `LightRig` (ambient, directional, and the rig's own point and spot lights) at binding 0, and bins any number of extra local lights (`ClusterLight`) into a 3D grid of screen tiles × logarithmic depth slices (default `16 × 9 × 24`, up to `512` visible lights and `64` per cluster). Each fragment then shades only the lights in its own cluster, with the same Cook-Torrance BRDF and glTF `KHR_lights_punctual` falloff as `shade_pbr`, so clustered and rig lights look identical. Because cluster assignment runs on the CPU, only fragment storage buffers (`Features.StorageBuffers` and `Limits.MaxStorageBufferBindingSize`) are required — compute shaders are not needed, so Apple Metal devices use the full clustered path. On targets without storage buffers (WebGL 2, OpenGL ES 3.0, Linux `GtkGLArea`, and headless tests using `nsgotest.NoGPUCompute`) it falls back to one uniform buffer holding the `FallbackMaxLights` (default `16`) most important visible lights:

```go
sys, err := render3d.NewClusteredLightSystem(cx, dev, render3d.DefaultClusterConfig())
pipe, err := render3d.NewClusteredLitPBRPipeline(cx, dev, camBuf.Layout(), sys, drawBuf.Layout(), gpu.FormatDepth24Plus)

// Convert scene lights once; a Range of 0 (unbounded) gets a derived cutoff range.
local := make([]render3d.ClusterLight, 0, len(lamps))
for _, l := range lamps {
	cl, err := render3d.ClusterLightFromPoint(l)
	if err != nil {
		return err
	}
	local = append(local, cl)
}

// Each frame, before recording draws:
camBuf.WriteCamera(cam)
stats, err := sys.UpdateCamera(frame.Device(), cam, viewW, viewH, rig, local)

pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
sys.Bind(pass)
pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
mesh.Draw(pass)
```

- **Lights**: `ClusterLight` is a point or spot record whose `Range` must be positive, because clustering needs a bounding sphere. `ClusterLightFromPoint` and `ClusterLightFromSpot` convert a `PointLight` or `SpotLight` and, for `Range == 0`, derive the distance at which `Intensity * max(Color) / d^2` falls to `ClusterRangeCutoff` (1/256). `Update` returns an error wrapping `ErrInvalidClusterLight` for a light that fails `Validate` (including `Range == 0`) or `ErrInvalidLight` for a malformed rig, instead of dropping it. `EvaluateLightAttenuation` reuses `PointLightAttenuation` and `SpotConeAttenuation`.
- **Viewport size**: `viewW` and `viewH` must be the size of the area the pass renders, starting at its origin, because the shader finds a fragment's tile from `@builtin(position)`. Under a `DynamicResolutionTarget`, pass `ScaledSize()`, not the native size.
- **Shaders**: `NewClusteredLitPBRPipeline` compiles `ClusteredLitWGSL` or `ClusteredFallbackWGSL` (whichever matches `sys.WGSL()`), the `LitPBRWGSL` module with `shade_pbr_clustered` in place of `shade_pbr`, writing sRGB. Custom shaders prepend `CameraWGSL` and `sys.BindingsWGSL()` (`ClusteredBindingsWGSL` or `ClusteredFallbackBindingsWGSL`, both of which include `LightWGSL`) and call `shade_pbr_clustered(world_pos, normal, view_dir, base_color, metallic, roughness, frag_pos)`, which has the same signature on both paths.
- **CPU clustering (`ClusterGrid`)**: view-space cluster AABBs are cached by `(proj, near, far)` and reused across camera motion. `AssignLights` culls off-frustum lights, computes tight screen-tile and depth-slice bounds, tests sphere-vs-AABB (plus conservative cone-vs-AABB rejection for spot lights), and packs `Offsets` and `LightIndices` with zero steady-state allocations. Lights beyond `MaxLights`, and lights beyond `MaxLightsPerCluster` in one cell, are dropped in input order (`TruncatedLights`, `TruncatedClusters`), so put important lights first when a scene can exceed either cap.
- **Fallback selection**: `SelectFallbackLights` drops lights whose bounding sphere is outside the view frustum, then ranks the rest by power, range and distance to the camera. In fallback mode `CulledLights` counts lights outside the frustum or without power, and `TruncatedLights` the visible lights the cap left out.
- **Depth-slice lookup**: the shader finds a fragment's depth slice with a 6-step binary search over a uniform split-depth table rather than `log2` and `exp`, because NSGo v0.3.0's software GPU renderer evaluates those builtins to zero.
- **CPU reference**: `ShadeClusteredPoint` is `ShadePoint` plus every clustered light, and `ShadeClusteredGridPoint` uses only the lights of the fragment's cluster, exactly as the GPU does. Tests compare GPU pixels against them on the full and `NoGPUCompute` profiles, including a 256-light scene.
- **Limits**: `Validate` caps `MaxLights` at `MaxClusterLights` (65,536), keeps `MaxLightsPerCluster` at or below `MaxLights`, and caps index slots (`TilesX * TilesY * SlicesZ * MaxLightsPerCluster`) at `MaxClusterIndexEntries`; `NewClusteredLightSystem` returns `ErrInvalidClusterConfig` when a storage buffer would exceed the device's `MaxStorageBufferBindingSize`.
- **Desktop GPU performance**: measured on a MacBook Pro (Apple M4 Pro, Metal 4, macOS arm64, `2560 × 1440` drawable, 120 Hz display) with 256 animated clustered point lights (~250 visible, ~475 active clusters): sustained `120.0 fps` on the clustered storage-buffer path, with CPU cluster assignment and buffer upload taking ~145 µs/frame.
- **Lifecycle**: set `ClusterConfig.Retained` to restore buffers across GPU device loss; `ByteSize()` reports GPU buffer bytes for `RenderStats` and budgets, and `Close()` releases resources eagerly.

## Splat-map terrain materials (`TerrainMaterial`, `GPUTerrainMaterial`)

Render multi-layer heightmap terrain ([world3d](world3d.md)) blending up to 8 PBR layers from 1 or 2 RGBA weight (splat) maps with per-layer UV tiling, optional height-based contrast blending, optional slope-driven triplanar projection on steep cliffs, and full scene lighting plus shadows:

```go
gpuTerrain, err := render3d.NewGPUTerrainMaterial(cx, dev, render3d.TerrainMaterial{
	Layers:              layers, // 1..8 TerrainLayer values (albedo, normal, metallic-roughness, height, UVScale, HeightBias)
	SplatMap0:           splatImg0,
	SplatMap1:           splatImg1, // optional for <= 4 layers
	HeightBlendContrast: 0.65,
	Triplanar:           render3d.DefaultTerrainTriplanar(),
}, render3d.TerrainMaterialUpload{Label: "alpine", Mipmaps: true, Retained: true})

pipe, err := render3d.NewTerrainMaterialPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), gpuTerrain.Layout(), render3d.TerrainPipelineOptions{
	DepthFormat:  gpu.FormatDepth24Plus,
	ShadowLayout: shadowMap.Layout(), // or CSMLayout / FogLayout at group 3
})

pass.SetPipeline(pipe)
pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
pass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
gpuTerrain.Bind(pass) // TerrainMaterialGroup (group 2)
pass.SetBindGroup(render3d.ShadowGroup, shadowMap.BindGroup())
terrainMesh.Draw(pass)
```

- **Texture-array layout**: `GPUTerrainMaterial` packs layer albedo (sRGB), tangent-space normal, and height/roughness/metallic maps into three `gpu.Dim2DArray` textures (`TerrainAlbedoArrayBinding`, `TerrainNormalArrayBinding`, `TerrainMRHeightArrayBinding`) plus the two RGBA splat maps at `TerrainMaterialGroup` (group 2). Total fragment texture bindings stay at 5 (6 with shadows), well within WebGL 2 / GLES 3 limits with zero pipeline permutations across 1–8 layers.
- **Lighting, shadows, fog, and HDR**: `NewTerrainMaterialPipeline` compiles `TerrainMaterialWGSL`, `TerrainMaterialShadowWGSL` (`ShadowLayout`), `TerrainMaterialCSMWGSL` (`CSMLayout`), or `TerrainMaterialFogWGSL` (`FogLayout`), plus `LinearOutput` variants for `HDRTarget` / `ToneMapPass`.
- **Live updates & CPU reference**: call `UpdateUniforms` to tweak transforms, blend contrast, triplanar settings, or per-layer factors/tiling without reallocating textures, `UpdateSplatMaps` to upload repainted splat maps in place, and `EvaluateTerrainMaterial` as the pure-Go CPU reference matching the WGSL shader.
