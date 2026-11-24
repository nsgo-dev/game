# Agent Note: 3D Toon Shading with Banded Lighting and Inverted-Hull Outlines

Status: implemented

## Problem

`packages/game/render3d` provides GPU meshes (`GPUMesh`), textures and samplers (`GPUTexture`, `GPUSampler`), and camera uniforms (`CameraBuffer`), but had no stylized cel/toon material or outline rendering pipeline (`#262`, under `#259`).

Stylized 3D games targeting every NSGo backend (WebGPU, Metal, Vulkan, Direct3D 11, OpenGL 3.3, OpenGL ES 3.0, WebGL 2, and `gpu.NewSoftwareRenderer`) need:

- Discrete quantized diffuse light bands with optional smooth anti-aliased transitions, plus an optional 1D artist-authored cel-ramp texture override.
- Crisp stepped specular highlights and Fresnel silhouette rim lighting.
- Watertight silhouette outlines (including on hard-edged geometry like boxes) without compute shaders, deferred G-buffers, or depth/normal buffer readback that fail on the mobile WebGL 2 / GLES 3.0 forward path (`nsgotest.NoGPUCompute`).
- Zero-allocation uniform updates, `Retained` device-loss recovery, and a deterministic CPU reference for automated golden parity testing.

## Decision

`packages/game/render3d/toon.go` adds a self-contained forward cel/toon material and outline pipeline built entirely on standard vertex and fragment WGSL stages:

- **Three-group binding layout**:
  - `@group(0)` (`CameraGroup`): `CameraWGSL` (`CameraBuffer`), unchanged.
  - `@group(1)` (`ToonMaterialGroup`): `GPUToonMaterial` owns a 112-byte 16-byte-aligned `ToonMaterialUniform` (`7 × vec4<f32>`), plus base albedo and 1D cel-ramp `(texture_2d, sampler)` bindings with internal 1×1 opaque white fallback textures and default samplers when omitted.
  - `@group(2)` (`ToonSceneGroup`): `ToonSceneUniform` (`176` bytes: `model`, inverse-transpose `normal_mat`, `light_dir`, `light_color`, `ambient_color`). `ToonRenderer` uses `ToonSceneDynamicBindingLayout` (`HasDynamicOffset: true`) aligned to `dev.Limits().MinUniformBufferAlignment` so a single uniform buffer and single `WriteBuffer` upload drive arbitrary multi-object scenes in one render pass.
- **Closed-form band quantization without loops or `exp`/`exp2`/`log2`**: `ToonWGSL` and `ToonQuantizeBand` evaluate arbitrary discrete band counts (`2..8`, default `3`) in O(1) using `floor`, `clamp`, and an explicit arithmetic Hermite `toon_smoothstep`, and guard `pow(ndh, spec_power)` to strictly positive `ndh > 0` for GLSL ES 3.00 / WebGL 2 safety.
- **Shared-layout inverted-hull outline pass**: `ToonOutlineWGSL` + `NewToonOutlinePipeline` (`gpu.CullFront`, `DepthWrite: true`, `CompareLess`) shares the exact three bind group layouts of `ToonWGSL` so the same `GPUToonMaterial.Bind(pass)` drives both passes. Vertices extrude outward along world normals (or clip-space projected normals scaled by `clip.w` when `OutlineClipSpace` is true). `SmoothOutlineNormals` averages angle-weighted coincident-position normals into an outline mesh copy so hard-edged geometry does not crack at corners.
- **`ToonRenderer` pass order and `Retained` lifecycle**: `ToonRenderer.Draw` skips the outline pipeline when no submitted draw has an active outline (`OutlineWidth > 0 && OutlineColor[3] > 0`), otherwise renders outlines first and cel-shaded surfaces second. `GPUToonMaterial.Update(dev, mat)` and `ToonRenderer.Draw(dev, ...)` take `*gpu.Device` explicitly instead of caching a pointer that would go stale across `TriggerGPUDeviceLost` / `RestoreGPUDevice`, matching `GPUTexture` ([render3d textures and samplers](./2026-11-24-render3d-textures-and-samplers.md)).
- **CPU parity and golden coverage**: `EvaluateToon` mirrors `ToonWGSL` in pure Go; `packages/game/render3d/toon_test.go` verifies quantization, smoothness, specular/rim, validation, zero-alloc packing, `SmoothOutlineNormals`, `Retained` device-loss recovery, scope disposal, both `full` and `nsgotest.NoGPUCompute` `gpu.View` profiles, and a multi-object golden scene (`packages/game/render3d/testdata/golden/toon_scene.png`).

## Alternatives considered

### Screen-space Sobel / Roberts-Cross depth-normal post-process outlines

Post-process edge detection handles arbitrary concave silhouettes and interior crease edges with a single full-screen pass. It was rejected because NSGo's mobile forward rendering profile (WebGL 2 / OpenGL ES 3.0, tested via `nsgotest.NoGPUCompute`) has no deferred normal G-buffer and does not guarantee portable readable depth textures alongside an active render pass without an extra pre-pass and extra render targets. Inverted-hull `CullFront` outlines run inside the existing forward pass on every NSGo backend with zero extra render targets.

### Baking lighting and model transform into the material uniform (`@group(1)`)

Merging `model`, `normal_mat`, and directional light fields into `GPUToonMaterial`'s uniform would eliminate `@group(2)` and reduce bind groups from 3 to 2. It was rejected because multiple meshes in a scene routinely share one `GPUToonMaterial` at different transforms; putting per-draw transforms and scene lighting at `@group(2)` with dynamic offsets lets one `GPUToonMaterial` shade many objects in a single render pass without duplicating materials or clobbering transforms before `dev.Submit`.

### Separate bind group layout for `ToonOutlineWGSL`

Giving `ToonOutlineWGSL` a minimal `@group(1)` layout containing only the uniform buffer (omitting the fragment texture/sampler bindings) would avoid binding textures during the outline pass. It was rejected because it would force `GPUToonMaterial` to allocate and track a second bind group and layout; sharing `ToonMaterialBindingLayout` across both pipelines keeps `GPUToonMaterial.Bind(pass)` uniform and halves material bind group objects.

## Consequences

- **Benefits**:
  - Single-pass forward cel/toon shading and crisp silhouette outlines work identically across WebGPU, Metal, Vulkan, D3D11, GL 3.3, GLES 3.0, WebGL 2, and `gpu.NewSoftwareRenderer`.
  - Zero allocations in steady-state uniform packing (`AppendUniformData`, `GPUToonMaterial.Update`), full `Retained` device-loss recovery, and deterministic CPU/GPU parity verified against a golden PNG.
  - All new code lives in `packages/game/render3d/toon.go` and `toon_test.go` with no edits to other `render3d` source files.
- **Costs**:
  - Inverted-hull outlines require closed or smooth-normal meshes (hard-edged meshes need `SmoothOutlineNormals` to avoid corner gaps) and draw outlined meshes twice (once `CullFront`, once `CullBack`).
  - `ToonSceneUniform` models a single directional key light plus ambient fill; multi-light accumulation and shadow-map sampling remain separate lighting-pass concerns.
  - `GPUToonMaterial.Update` updates uniform parameters in place and rejects swapping bound texture/sampler pointers (callers construct a new `GPUToonMaterial` to rebind textures).
