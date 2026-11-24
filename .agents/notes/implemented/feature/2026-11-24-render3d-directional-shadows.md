# Agent Note: Directional Light Shadow Mapping in render3d

Status: implemented

## Problem

Issue `#117` (under parent `#19`, "3D: cameras and lights") requires portable directional-light shadow mapping for NSGo 3D scenes with verified reference renders.

Implementing directional shadows cleanly across all NSGo v0.3.0 GPU backends (Metal, D3D11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the software GPU renderer) imposes several non-obvious constraints:

- **Shader IR binding lookup**: NSGo's GLSL emitter (`internal/shaderir/glsl/pipeline.go`) and software GPU VM (`internal/driver/gpu_software.go`) resolve `textureSampleCompare` and `textureSampleCompareLevel` by indexing module-scope `@group/@binding` variables directly; passing `texture_depth_2d` or `sampler_comparison` as WGSL function parameters is not portable across NSGo's IR emitters.
- **Uniform control flow**: WebGPU rejects implicit-derivative `textureSampleCompare` inside non-uniform branches (such as frustum bounds checks or filter-mode selection), whereas `textureSampleCompareLevel` is legal anywhere and maps cleanly to `textureGrad`, `sample_compare(..., level(0))`, and `SampleCmpLevelZero`.
- **Multi-caster shadow passes and device loss**: Calling `Device.WriteBuffer` twice to offset 0 of one uniform buffer inside a single render pass overwrites the first caster's matrix before GPU execution, while `gpu.RingBuffer` is not restored across `Retained` device loss in NSGo v0.3.0.
- **Bind-group budget**: NSGo's cross-backend `Limits.MaxBindGroups` is 4 (`@group(0)`..`@group(3)`). The lit PBR pipeline already uses `CameraGroup` (0), `LightGroup` (1) and `PBRDrawGroup` (2), and fog, IBL and skinning each take group 3.

## Decision

`packages/game/render3d/shadow.go` implements single-directional-light shadow mapping in new self-contained files (`shadow.go` and `shadow_test.go`) using NSGo's native depth-only `gpu.RenderTarget` (`FormatDepth32Float`) and hardware comparison samplers (`texture_depth_2d` + `sampler_comparison`):

- **Module-scope `ShadowWGSL` at `ShadowGroup = 3` (plus `ShadowWGSLForGroup`)**: `ShadowWGSL` declares `@group(3)` bindings (`@binding(0)` 96-byte `ShadowUniforms`, `@binding(1)` `texture_depth_2d`, `@binding(2)` `sampler_comparison`) and exposes `sample_directional_shadow(world_pos, world_normal) -> f32` using `textureSampleCompareLevel`. Because the helper references module-scope binding variables, it compiles and executes identically across WebGPU, Metal, D3D11, GLSL/WebGL 2/GLES 3.0, and the software VM. Callers needing another bind-group slot can generate matching WGSL via `ShadowWGSLForGroup(group)`.
- **Retained dynamic-offset caster buffer (`DrawCaster`)**: `DirectionalShadowMap` allocates one persistent `gpu.Buffer` sized to `MaxCasters * align` (aligned to `Limits.MinUniformBufferAlignment`, default 256 slots) bound with `HasDynamicOffset: true` and `Retained: opts.Retained`. Each `DrawCaster(pass, mesh, model)` writes `lightViewProj * model` to its own aligned slot and binds `casterBG` with that dynamic offset. Because every owned resource (`RenderTarget`, `Buffer`, `Sampler`, `BindGroup`, `Pipeline`) uses `Retained` rather than `gpu.RingBuffer`, NSGo automatically restores the entire `DirectionalShadowMap` across device loss; `BeginShadowPass(dev, target)` and `Update(dev, ...)` take `*gpu.Device` explicitly so a restored shadow map never holds a stale device pointer.
- **Dual comparison samplers for zero-alloc Hard / PCF 3x3 switching**: `DirectionalShadowMap` creates both a `FilterNearest` (`CompareLessEqual`) and a `FilterLinear` (`CompareLessEqual`) comparison sampler and two bind groups (`hardBG` and `pcfBG`). `WriteUniforms` switches `BindGroup()` dynamically according to `ShadowUniforms.PCFKernel` with zero heap allocations.
- **Rotation-only light basis with texel snapping**: `DirectionalShadowConfig.LightViewProj` and `FitBounds` construct a translation-free light basis (`LookAt(0, dir, up)`) and compute the `[0, 1]` NDC orthographic projection (`OrthoZO`) around the light-space center, snapping XY to integer texel increments when `TexelSnap` is true. Snapping only stops shimmering if the texel size stays constant, so with `TexelSnap` `FitBounds` sizes the frustum from the bounds' bounding-sphere diameter (independent of the light direction) plus at least one texel per side, rounded up to a multiple of a sixteenth of the next power of two; the width then changes only in coarse steps as the bounds grow or shrink. `LightViewProj`'s depth is a `DepthRange` slab centred on `Target`.
- **Shadowed lit PBR pipeline and matching CPU reference**: `LitPBRShadowWGSL` (via `NewLitPBRShadowPipeline` or `LitPBRPipelineOptions.ShadowLayout`) reuses the lit PBR vertex stage, `LightRig` and `PBRDrawUniform`, and `LightWGSL`'s `shade_pbr_shadowed` multiplies directional light 0 by the shadow visibility (`shade_pbr` is the same function with visibility 1). `EvaluateDirectionalShadow` mirrors `sample_directional_shadow` in pure Go over a `ShadowDepthGrid` (including `ShadowDepthGridFromBytes` for GPU depth readback verification), and `ShadePointShadowed` mirrors the shadowed lighting; encoding and the CPU reference share one sanitizer so they agree on NaN values and unknown filters.

## Alternatives considered

### Encode shadow depth into a color `FormatRGBA8Unorm` or `FormatR32Float` render target

Packing depth into RGBA8 avoids depth-texture sampling altogether, and `FormatR32Float` avoids comparison samplers. Both were rejected because NSGo v0.3.0 already supports `FormatDepth32Float` depth-only `RenderTarget`s, `texture_depth_2d`, `sampler_comparison`, and hardware 2x2 PCF across all backends (including WebGL 2 / GLES 3.0 and the software renderer), whereas `FormatR32Float` requires `Features.ExtendedFormats` (false on native backends in v0.3.0) and RGBA8 packing sacrifices precision and hardware comparison filtering.

### Use `gpu.RingBuffer` for per-caster shadow matrices

`gpu.RingBuffer` simplifies multi-frame dynamic uniform allocation, but `gpu.Device.restoreRetainedFrom` in NSGo v0.3.0 does not recreate ring buffers on device loss, and `SoftwareRenderer` tests without `BeginFrame`/`EndFrame` do not rotate ring slots automatically. A single `Retained` dynamic-offset `gpu.Buffer` works identically in headless `SoftwareRenderer` passes, `gpu.View` frames, and `Retained` device-loss recovery.

### A separate Lambertian lit pipeline at group 2

The first version shipped its own `ShadowLitWGSL` with a per-object uniform at group 1 and the shadow at group 2. It was dropped because it collided with the lit PBR pipeline's `LightGroup` and `PBRDrawGroup`, duplicated `PBRDrawUniform` and `DirectionalLight`, and ignored the scene's `LightRig`.

### Pass `texture_depth_2d` and `sampler_comparison` as parameters to `sample_directional_shadow`

Parameterized WGSL helper functions would decouple the function body from `@group/@binding` indices. This was rejected because NSGo v0.3.0's GLSL emitter and software GPU VM read binding indices directly from the `BindingVariable` IR expression passed to `textureSampleCompareLevel`. Providing `ShadowWGSL` at `ShadowGroup = 3` alongside `ShadowWGSLForGroup(group)` achieves group flexibility while staying 100% compatible with every NSGo backend.

## Consequences

- **Benefits**: Portable directional shadows with hard 1-tap and smooth 3x3 PCF filtering, tight `FitBounds` orthographic framing, texel snapping that eliminates shadow swimming, zero-allocation uniform encoding (`AppendShadowUniforms`), dynamic multi-caster shadow passes, `Retained` device-loss restoration, and a matching CPU reference (`EvaluateDirectionalShadow`). Automated tests cover validation, texel snapping, zero allocations, CPU/GPU parity against depth readback, golden reference renders (`shadow_directional_hard.png`, `shadow_directional_pcf.png`), both `full` and `nsgotest.NoGPUCompute` `gpu.View` profiles, device loss, and scope disposal.
- **Costs**: A single shadow pass is bounded by `ShadowMapOptions.MaxCasters` dynamic uniform slots (default 256, configurable at creation time), the shadow takes group 3, so a lit PBR pipeline cannot combine it with fog, IBL or skinning, only directional light 0 is shadowed, and this implementation covers a single directional light frustum rather than cascaded shadow maps (CSM), point-light cubemap shadows, or spot-light shadows (tracked separately for future issues).
