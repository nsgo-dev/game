# Agent Note: Clustered Forward Lighting in render3d

Status: implemented

## Problem

`render3d` needs to render scenes with hundreds of local point and spot lights without leaving forward rendering, and without breaking mobile and browser targets (WebGL 2 / OpenGL ES 3.0) that lack compute shaders and storage buffers (`Features.Compute == false`, `Features.StorageBuffers == false`, simulated in tests via `nsgotest.NoGPUCompute`).

The scene lighting model already exists: `light.go` defines `LightRig` (ambient, up to 4 directional, 16 point and 8 spot lights) at `LightGroup` binding 0, the Cook-Torrance `pbr_brdf`, `point_light_attenuation`, `spot_cone_attenuation`, `shade_pbr`, and `LitPBRWGSL` with sRGB output. Clustered lights must combine with that rig in one shader and look identical to rig lights, not introduce a second shading model or a second binding at `@group(1) @binding(0)`.

Constraints:

- NSGo v0.3.0's software GPU VM (headless `nsgotest`, `gpu.NewSoftwareRenderer`) has no case for `log`, `log2`, `exp` or `exp2` in `evalCall` (`internal/driver/gpu_software.go`) and silently returns zero, so the textbook `floor(log2(z / near) * scale)` slice formula would shade every fragment from slice 0 in software tests. Upstream main still lacks them.
- WebGL 2 and OpenGL ES 3.0 disallow storage buffers, so the system must fall back to uniform buffers when `DeviceSupportsClusteredLighting(dev)` is false.
- Retained GPU resources survive device loss by retargeting buffers; holding a stale `*gpu.Device` breaks updates after `RestoreGPUDevice`.

## Decision

`packages/game/render3d/clustered.go` makes `ClusteredLightSystem` own the whole `LightGroup` bind group:

- **One light group, one shading model**: binding 0 is the `LightRig` uniform `LightWGSL` declares, encoded with `packLightUniforms` / `LightUniforms.Encode`; binding 1 is the cluster parameters (clustered) or the fallback light array (fallback); bindings 2 to 4 are the light, cell and index storage buffers. `ClusteredBindingsWGSL` and `ClusteredFallbackBindingsWGSL` are `LightWGSL` plus those bindings and `shade_pbr_clustered`, which returns `shade_pbr` plus each local light through `pbr_brdf`, `point_light_attenuation` and `spot_cone_attenuation`. `ClusteredLitWGSL` / `ClusteredFallbackWGSL` are `LitPBRWGSL` with `shade_pbr_clustered`, writing `linear_to_srgb`, and `NewClusteredLitPBRPipeline` builds them with `newLitPBRPipeline`. `Bind(pass)` matches `GPULightBuffer.Bind`.
- **Lights from the existing types**: `ClusterLight` stays as the packed record the grid bins, but `ClusterLightFromPoint` / `ClusterLightFromSpot` build it from `PointLight` / `SpotLight`, and `EvaluateLightAttenuation` reuses `PointLightAttenuation` and `SpotConeAttenuation` (with the same `minSpotConeCosDelta`). Clustering needs a bounding sphere, so `Range` must be positive: the converters derive one for `Range == 0` (where `Intensity * max(Color) / d^2` falls to `ClusterRangeCutoff`, 1/256), and `Update` returns an error for an invalid light instead of dropping it.
- **Zero-allocation CPU clustering (`ClusterGrid`)**: logarithmic depth splits for perspective, linear for orthographic, cluster AABBs cached by `(proj, near, far)`, conservative tile and slice bounds, sphere-vs-AABB plus cone rejection for spot lights, and packed offsets and indices with no steady-state allocations. Overflow past `MaxLights` or `MaxLightsPerCluster` drops lights in input order and is reported in the stats.
- **Binary-search depth slicing in WGSL**: up to 65 slice boundaries in a 17 × `vec4<f32>` uniform table and a fixed 6-step binary search, instead of `log2` / `exp`.
- **Fallback selection**: `SelectFallbackLights` frustum-culls light bounding spheres before ranking, so off-screen lights never take one of the 16 uniform slots, and sorts with `slices.SortFunc` so the per-frame path allocates nothing.
- **Bounded sizes**: `Validate` caps `MaxLights` (65,536), `MaxLightsPerCluster <= MaxLights`, and index slots (`MaxClusterIndexEntries`); `NewClusteredLightSystem` refuses a storage buffer larger than the device's binding limit instead of clamping it.
- **Explicit `*gpu.Device` on `Update` / `UpdateCamera`**, as `GPUTexture` and `GPUMorphMesh` do.

## Alternatives considered

### Clustered bindings next to a separate `GPULightBuffer`

Keeping `GPULightBuffer` at group 1 and putting the cluster buffers in another group would avoid touching the rig upload, but group 2 is `PBRDrawGroup` and group 3 is a custom material's extra layout, so the clustered pass would need a fourth group that WebGL 2 pipelines with materials cannot spare. One bind group with the rig at binding 0 keeps `LightWGSL` unchanged and fits every existing pipeline layout.

### A Lambert-only clustered shader

The first version shaded clustered lights with `color * intensity * atten * ndotl` and wrote linear color. It was simpler, but a scene mixing it with `LitPBRWGSL` would be darker and inconsistent, and it duplicated the attenuation math. Reusing `pbr_brdf` costs a few more ALU operations per light and keeps one shading model.

### Compute-shader light assignment on the GPU

Assigning lights in a compute pass avoids uploading cell and index buffers, but the software GPU runs compute workgroups sequentially, native backends need atomic append counters, and capability-limited targets still need a CPU path. `ClusterGrid.AssignLights` handles 256 lights with no allocations (`BenchmarkClusterAssign256Lights`).

### `log2(view_z / near)` slice indexing in WGSL

It would save 272 bytes of uniforms, but NSGo's software VM evaluates `log2` to zero, and a split table also covers orthographic slicing.

### Packing light lists into textures for WebGL 2

Emulating storage buffers with integer textures needs `Features.ExtendedFormats` and costs texture fetches per light on mobile tile GPUs. A 16-light uniform fallback uses only core WebGL 2 / GLES 3.0 features.

## Consequences

- **Benefits**: clustered and rig lights share one BRDF, one attenuation curve and one sRGB output; GPU pixels match `ShadeClusteredPoint` / `ShadeClusteredGridPoint` on the full and `NoGPUCompute` profiles, including a 256-light scene; glTF lights with no range work through the converters.
- **Costs**: cluster assignment runs on the CPU and uploads cell and index buffers each frame. The shader-side cost of 256 lights at desktop resolution on native GPUs has not been measured here; only the software renderer and the CPU benchmark run in CI. Depth slices are capped at 64 and fallback lights at 16. The tile lookup assumes the viewport starts at the attachment origin, so callers pass `DynamicResolutionTarget.ScaledSize()` under dynamic resolution.
