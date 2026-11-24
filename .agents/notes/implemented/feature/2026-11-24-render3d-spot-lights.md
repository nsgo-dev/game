# Agent Note: Physically based 3D spot lights in render3d

Status: implemented

## Problem

`game/render3d` needs punctual spot lights with inner and outer cones (`#252`, part of `#251`) that:

1. Follow glTF `KHR_lights_punctual` spot light semantics (world position, emission direction, linear color, intensity, optional smooth range windowing, and inner/outer cone half-angles in `[0, pi/2]`) in a Cook-Torrance metallic-roughness PBR shading model.
2. Run identically on every backend NSGo's `gpu.View` supports—including WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`), where storage buffers and compute shaders are unavailable—as well as NSGo v0.3.0's software GPU VM, which omits `shaderir.Exp`, `shaderir.Exp2`, and `shaderir.Log2`.
3. Provide deterministic CPU reference evaluation matching the GPU shader output and clean `Retained` device-loss recovery, while staying in dedicated files (`spotlight.go` / `spotlight_test.go`) to avoid merge conflicts with parallel lighting work (`#116` `light.go`, `#255` `local_shadows.go`).

## Decision

`packages/game/render3d/spotlight.go` introduces `SpotLight`, `SpotLightSet`, `GPUSpotLights`, `SpotMaterial`, `GPUSpotMaterial`, `SpotLightsWGSL`, `SpotLitWGSL`, `NewSpotLitPipeline`, `SpotLitRenderer`, and CPU evaluators (`EvaluateSpotLight`, `EvaluateSpotPBR`, `SpotToneMapSRGB`) with five key structural choices:

- **Fixed 8-light `vec4<f32>` uniform block (`SpotLightUniformSize = 400`)**: `AppendSpotLightUniforms` packs a header `vec4` (`ambient.rgb`, `f32(count)`) followed by `MaxSpotLights = 8` lights of three `vec4<f32>` fields each (`pos_range`, `dir_scale`, `color_offset`). Precomputing `invRange`, normalized `Direction`, glTF `angleScale = 1 / max(cosInner - cosOuter, 0.0001)`, `angleOffset = -cosOuter * angleScale`, and `Color * Intensity` on the CPU keeps per-fragment work minimal, avoids `u32`/struct padding discrepancies across WGSL/MSL/HLSL/GLSL std140/software VM, and requires zero storage buffers.
- **Algebraic + `sqrt`/`pow` portable PBR WGSL**: `SpotLightsWGSL` implements glTF range windowing `clamp(1 - (d/Range)^4, 0, 1)^2 / max(d^2, 0.0001)`, glTF quadratic cone falloff `clamp(dot(dir, -L) * angleScale + angleOffset, 0, 1)^2`, GGX NDF, height-correlated Smith-GGX visibility, algebraic Schlick Fresnel `(1 - VdotH)^5`, energy-conserving Lambertian diffuse, and Reinhard + 2.2 gamma output (`pow`). Avoiding `exp`/`exp2`/`log2` ensures exact parity with NSGo v0.3.0's software GPU VM.
- **Per-draw `GPUSpotMaterial` uniform buffers**: Because `gpu.Device.WriteBuffer` operations in a frame execute before render passes, each draw with distinct model matrix or PBR parameters owns its own 144-byte `GPUSpotMaterial` uniform buffer (storing `model`, inverse-transpose normal matrix columns packed with `metallic`/`roughness`, `base_color`, and `emissive`) bound at `SpotMaterialGroup` (`@group(2)`).
- **Explicit `*gpu.Device` on retained `Update` methods**: Following [3D GPU Textures and Samplers in render3d](2026-11-24-render3d-textures-and-samplers.md), `GPUSpotLights.Update(dev, set)` and `GPUSpotMaterial.Update(dev, mat)` do not cache `*gpu.Device`, so `Retained` uniform buffers remain updatable on the new device after GPU device loss and restore.
- **CPU/GPU parity and golden reference testing**: `EvaluateSpotLight`, `EvaluateSpotPBR`, and `SpotToneMapSRGB` mirror the WGSL equations directly so tests verify both pixel-level analytic parity against the GPU rasterizer and a multi-spotlight 3D scene golden image (`testdata/golden/spot_lights_scene.png`).

## Alternatives considered

### Variable-length storage buffer (`var<storage, read>`) for unlimited spot lights

Using a read-only storage buffer would allow arbitrary numbers of spot lights without recompiling shaders. It was rejected because `gpu.View` targets WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`), which do not support storage buffers in fragment shaders. A fixed 8-light uniform block works everywhere, and forward-clustered / tiled lighting can extend this later on capable backends.

### Dynamic uniform buffer offsets for per-draw materials inside `SpotLitRenderer`

Packing multiple materials into one ring buffer with `HasDynamicOffset` reduces the number of GPU buffer objects when drawing many meshes per pass. It was rejected here because WebGPU mandates 256-byte dynamic uniform alignment (almost doubling the 144-byte material block) and requires manual frame-slot capacity management; standalone `GPUSpotMaterial` buffers mirror `CameraBuffer` and `GPUTexture`, compose directly with `Retained` device-loss restoration, and keep the API minimal.

### Combining spot, directional, and point lights in a single `light.go` uber-block

A single unified light array could handle all punctual light types in one loop. It was rejected to respect package boundaries and parallel issue ownership (`#116` owns `light.go`, `#255` owns `local_shadows.go`), while `SpotLightsWGSL` and `SpotLight` remain cleanly composable with directional/point light shaders.

## Consequences

- **Benefits**:
  - Physically based spot lights with smooth inner/outer cone falloff and smooth range windowing work identically on full WebGPU/Metal/Vulkan/D3D11/OpenGL 3.3 and `NoGPUCompute` (WebGL 2 / GLES 3.0) profiles as well as the software GPU renderer.
  - Zero heap allocations per frame for `AppendSpotLightUniforms`, `AppendSpotMaterialUniforms`, `GPUSpotLights.Update`, and `GPUSpotMaterial.Update`.
  - Exact CPU-to-GPU mathematical parity enables analytic shading assertions alongside golden visual regression testing.
  - `Retained` light and material buffers survive GPU device loss and remain updatable after restoration.
- **Costs & limits**:
  - A single `GPUSpotLights` uniform block holds at most `MaxSpotLights` (8) active spot lights per pass; scenes with more lights must cull/select the nearest 8 per draw or use multi-pass lighting.
  - Spot lights do not cast shadows yet; shadow-mapped local lights are scoped separately to `#255`.

## Verification

- `packages/game/render3d/spotlight_test.go` verifies `SpotLight`/`SpotLightSet`/`SpotMaterial` validation, inner/mid/outer/behind cone attenuation and range windowing, exact 400-byte/144-byte uniform packing with 0 allocs/op, non-uniform scale inverse-transpose normal matrices, analytic GPU-vs-CPU PBR pixel parity (`<= 2/255` per channel), the multi-spotlight golden reference scene (`testdata/golden/spot_lights_scene.png`), identical `gpu.View` rendering across `full` and `nsgotest.NoGPUCompute()` profiles, `Retained` device-loss restoration including post-restore `Update`, scope disposal, and double-`Close` cleanup.
