# Agent Note: Clustered Forward Lighting in render3d

Status: implemented

## Problem

`render3d` needs to render scenes with hundreds of local point and spot lights (256+ lights within a 60 fps desktop frame budget) without switching away from forward rendering or breaking mobile and browser targets (WebGL 2 / OpenGL ES 3.0) that lack compute shaders and storage buffers (`Features.Compute == false`, `Features.StorageBuffers == false`, simulated in tests via `nsgotest.NoGPUCompute`).

Three NSGo-specific constraints shape the design:
- NSGo v0.3.0's software GPU VM (used by headless `nsgotest` and `gpu.NewSoftwareRenderer`) omits `shaderir.Log2`, `shaderir.Exp`, and `shaderir.Exp2`, so the textbook `floor(log2(z / near) * scale)` depth-slice formula in WGSL would fail in software GPU tests.
- WebGL 2 and OpenGL ES 3.0 disallow `UsageStorage` buffers and `BindingStorageRead` bind-group entries; any light system targeting all NSGo backends must automatically drop to a pure uniform-buffer path when `DeviceSupportsClusteredLighting(dev)` is false.
- Retained GPU resources survive device loss and restore by retargeting buffers to the restored device; holding a stale `*gpu.Device` inside the light system breaks updates after `RestoreGPUDevice`.

## Decision

`packages/game/render3d/clustered.go` implements a dual-path clustered forward lighting system (`ClusterGrid`, `ClusteredLightSystem`, `ClusterLight`, `ClusterConfig`) alongside pure-Go analytic evaluators (`EvaluateLightAttenuation`, `EvaluateDirectLights`, `EvaluateClusteredLights`):

- **Zero-allocation CPU frustum clustering (`ClusterGrid`)**: Partitions view space into `TilesX × TilesY × SlicesZ` (default `16 × 9 × 24 = 3,456` clusters) using logarithmic depth splits `z_i = near * (far/near)^(i/SlicesZ)` for perspective projections (and linear splits for orthographic). Cluster view-space AABBs are cached by `(proj, near, far)` and reused across camera translation/rotation. `AssignLights` culls off-frustum lights, computes conservative screen-tile and Z-slice bounds (clamping near-plane-straddling lights to the full tile span so there are zero false negatives), runs exact sphere-vs-AABB plus Bart Wronski tight bounding-sphere + cone-vs-AABB rejection for `ClusterLightSpot`, and packs contiguous `ClusterOffsetCount` + `LightIndices` slices with 0 heap allocations in steady state.
- **Portable binary-search depth slicing in WGSL**: `ClusteredBindingsWGSL` passes up to `MaxClusterSlicesZ + 1` (65) slice boundaries in a fixed 17 × `vec4<f32>` uniform array (`ClusterParams.slice_depths`) and computes `slice_z` with a fixed 6-step binary search (`cluster_find_slice`) over `view_z = -(camera.view * vec4(world_pos, 1.0)).z`. This avoids `log2`/`exp`, avoids dynamic vector indexing, and works identically across Metal, Direct3D 11, OpenGL 3.3, WebGPU, and NSGo's software GPU VM.
- **Automatic capability-gated fallback (`ClusteredLightSystem`)**: `NewClusteredLightSystem` inspects `DeviceSupportsClusteredLighting(dev)` (requiring `Features.StorageBuffers`, `Features.Compute`, and `MaxStorageBufferBindingSize >= 64 KiB`, unless `ForceFallback` is set). On capable devices it binds 1 uniform buffer (`ClusterParams`, 320 bytes) + 3 read-only storage buffers (`cluster_lights`, `cluster_cells`, `cluster_indices`) at `@group(1)` (`ClusteredLightGroup = 1`), all 16-byte aligned (`vec4<f32>` / `vec4<u32>`). On WebGL 2 / OpenGL ES 3.0 / `nsgotest.NoGPUCompute` devices it allocates a single 1,056-byte uniform buffer (`FallbackLightUniform`) holding up to `FallbackMaxLights` (default/max `16`) lights prioritized per frame by `SelectFallbackLights`.
- **Shared WGSL evaluation signature**: Both `ClusteredBindingsWGSL` and `ClusteredFallbackBindingsWGSL` expose `evaluate_clustered_lights(world_pos, normal, frag_pos) -> vec3<f32>` with glTF `KHR_lights_punctual` smooth range and spotlight attenuation, so both the built-in shaders (`ClusteredLitWGSL`, `ClusteredFallbackWGSL`) and user material shaders share one fragment body across desktop and mobile paths.
- **Explicit `*gpu.Device` on `Update`/`UpdateCamera`**: Matching `GPUTexture` and `GPUMorphMesh`, `ClusteredLightSystem` never caches a `*gpu.Device` pointer, allowing `ClusterConfig.Retained` buffers to recover and resume updating cleanly across GPU device loss and restore.

## Alternatives considered

### Compute-shader light assignment on the GPU

Assigning lights to clusters in a WGSL compute pass avoids uploading per-frame cell and index buffers from the CPU. However, NSGo's software GPU device runs compute workgroups sequentially on the CPU (making 3,456 × 256 invocations far slower in headless tests), requires atomic append counters in WGSL (`atomicAdd`) across all native backends, and still needs a CPU path for capability-limited targets. CPU `ClusterGrid.AssignLights` handles 256 point lights in well under 1 ms with 0 heap allocations, keeps buffer sizes deterministic (~884 KiB max index buffer), and lets headless tests and servers share the exact same culling logic.

### `log2(view_z / near)` slice indexing in WGSL

Computing `u32(log2(view_z / near) * inv_log_ratio)` saves 272 bytes in `ClusterParams`. It was rejected because NSGo v0.3.0's software GPU VM does not implement `shaderir.Log2`, which would break headless GPU parity tests, and because a uniform split-depth table also supports orthographic linear slicing and custom depth distributions with zero shader changes.

### Packing clustered light lists into a 2D RGBA32UI texture for WebGL 2

Emulating storage buffers via `textureLoad` on floating-point/integer textures could allow clustering on WebGL 2 without storage buffers, at the cost of requiring `Features.ExtendedFormats` (`FormatRGBA32Float` / integer texture formats) and much higher fragment-shader texture-fetch pressure on mobile tile GPUs. A prioritized 16-light uniform-buffer fallback (`SelectFallbackLights`) uses only core WebGL 2 / GLES 3.0 uniform buffers and stays comfortably within mobile fragment budgets.

## Consequences

- **Benefits**: Desktop GPU targets render 256+ local point and spot lights with zero steady-state CPU allocations, exact CPU-vs-GPU pixel parity (verified by `TestClustered256PointLightsParityAndBudget` and `TestClusteredMixedPointAndSpotLightsGPU`), and automatic seamless fallback on `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) with `Retained` device-loss recovery. Keeping all code in standalone `clustered.go` / `clustered_test.go` avoids merge conflicts with parallel `render3d` work.
- **Costs**: Cluster assignment runs on the CPU and uploads `cells` and `indices` buffers each frame via `Device.WriteBuffer` (scaling linearly with active cluster assignments, capped by `MaxLightsPerCluster` and `MaxStorageBufferBindingSize`). Depth slice count is capped at `MaxClusterSlicesZ = 64` to fit the fixed 17 × `vec4<f32>` uniform split table, and `FallbackMaxLights` is capped at `16` to stay compact inside WebGL 2 / GLES 3.0 uniform buffers.
