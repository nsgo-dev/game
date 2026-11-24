# Agent Note: Point and Spot Light Shadow Maps in render3d

Status: implemented

## Problem

Issue #255 (part of #251) requires real-time shadow maps for local 3D lights—perspective 2D shadows for spot lights and omnidirectional shadows for point lights—whose rendered shadows match analytic and golden references across every NSGo GPU backend (Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the software GPU renderer).

Implementing local shadows directly on top of NSGo v0.3.0's `gpu.Device` exposes four cross-backend constraints that callers should not have to rediscover:

1. **No layered depth attachments**: `gpu.DepthStencilAttachment` targets a 2D `*gpu.Texture` without an `ArrayLayer` selector, so rendering six cube faces directly into a `DimCube` depth texture is not expressible through the portable render-pass API.
2. **Uniform control flow and software VM limits**: WebGPU restricts `textureSampleCompare` to uniform control flow (rejecting calls inside `if` bounds checks), and NSGo v0.3.0's software GPU VM does not implement `exp`, `exp2`, or `log2`.
3. **Multi-caster uniform aliasing inside one pass**: Calling `Device.WriteBuffer` repeatedly to offset 0 of a single caster uniform buffer inside one `RenderPass` overwrites earlier casters' model matrices before `Submit` executes the pass.
4. **Device-loss restoration**: As recorded in [3D GPU Textures and Samplers](2026-11-24-render3d-textures-and-samplers.md), caching a `*gpu.Device` pointer on a retained resource wrapper becomes stale after `TriggerGPUDeviceLost` + `RestoreGPUDevice`.

## Decision

`packages/game/render3d/local_shadows.go` implements `SpotShadowMap` and `PointShadowMap` as dedicated, self-contained files (`local_shadows.go`, `local_shadows_test.go`, and golden PNGs under `testdata/golden/local_shadow_*.png`) to avoid symbol or file collisions with parallel directional/CSM/spotlight work:

- **Shadows for the `LightRig`'s own lights**: the maps take main's `SpotLight` and `PointLight` (`Update(dev, light)`), not a parallel light type. The far plane is the light's `Range`, or the config's `Far` when `Range` is 0 (unbounded inverse-square falloff); the config carries `Near`. The WGSL only computes visibility (`sample_spot_shadow`, `sample_point_shadow`); lighting stays in `LightWGSL`, whose new `shade_pbr_visibility` scales directional light 0, point light 0 and spot light 0 by a visibility each (`shade_pbr_shadowed` delegates to it). `LitPBRSpotShadowWGSL` / `LitPBRPointShadowWGSL` are `LitPBRWGSL` with the matching map bound, so the per-draw model and normal matrices and the material come from `PBRDrawUniform`, and `ShadePointVisibility` is the CPU reference.
- **Bind group 3**: `LocalShadowGroup` is `ShadowGroup` (3), the slot the directional shadow, fog, IBL and skinning share after camera (0), lights (1) and per-draw data (2). `SpotShadowWGSLForGroup` / `PointShadowWGSLForGroup` move the bindings, so a custom shader can bind a spot and a point shadow at once (tested at groups 3 and 2).
- **3×2 2D `FormatDepth32Float` atlas for point lights (`PointShadowMap`)**: All six 90° cube faces (`gpu.CubeFacePosX`..`gpu.CubeFaceNegZ`) are packed into a single `(3 * FaceResolution) × (2 * FaceResolution)` depth-only `gpu.RenderTarget`. `BeginFacePass(dev, target, face)` clears the entire atlas on `face == 0` (`LoadClear`), preserves previous tiles on `face` 1..5 (`LoadLoad`), and locks both `SetViewport` and `SetScissor` to that face's tile so geometry cannot bleed into neighboring faces.
- **Analytic perspective depth without a depth fragment shader**: Both spot and point shadow passes use a vertex-only depth pipeline (`vs_local_shadow_depth`) writing standard `[0, 1]` perspective depth. In `PointShadowWGSL`, `sample_point_shadow` selects the dominant cube face from `worldPos + worldNormal * normalBias - lightPos`, projects `(xView, yView, axisDist)` directly into tile-local UV, computes the exact NDC depth `(far/(far-near)) * (1 - near/axisDist)` without uploading six `mat4x4`s to the fragment shader, and clamps every tap inside `[0.51/faceRes, 1 - 0.51/faceRes]` so linear comparison and 3×3 PCF taps never sample across tile boundaries.
- **Portable WGSL and `textureSampleCompareLevel`**: `SpotShadowWGSL` and `PointShadowWGSL` avoid `exp`/`exp2`/`log2` and call `textureSampleCompareLevel` so bounds checks and filter branches remain valid under WebGPU's strict uniformity analysis as well as WebGL 2, GLES 3.0, and the software renderer.
- **256-byte dynamic-offset caster uniform slots**: the spot map allocates `MaxCasters` 256-byte slots and the point map `6 * MaxCasters`, one range per face, bound with `HasDynamicOffset: true`. `DrawCaster(pass, mesh, model)` writes `(lightViewProj, model)` to the next slot and returns false when the pass (or face) is full or `Update` has not run yet, as `DirectionalShadowMap.DrawCaster` does. `BeginFacePass` resets only its own face's slots, so refreshing a subset of faces, in any order, never exhausts the buffer. `MaxCasters` is capped at `MaxLocalShadowCasters` (4096) and the caster buffer is checked against the device's `MaxBufferSize`, both reported as `ErrInvalidLocalShadow`.
- **Same pass API as `DirectionalShadowMap`**: `BeginShadowPass(dev, target)` and `BeginFacePass(dev, target, face)` take the live `*gpu.Device` and a `ShadowPassTarget` (`*gpu.Encoder` or `*gpu.Frame`), so `Retained: true` maps survive device loss and restore.
- **Pure-Go CPU reference evaluators**: `EvaluateSpotShadow`, `PointShadowSelectFace` and `EvaluatePointShadow` mirror the WGSL math (including nearest vs linear depth comparison and clamped 3×3 PCF); with `ShadePointVisibility` they reproduce the GPU pixels in `local_shadows_test.go`.

## Alternatives considered

### Render point shadows into a `DimCube` depth texture with `texture_depth_cube`

A native cubemap depth texture lets hardware select faces and filter seamlessly across cube edges in a single `textureSampleCompare` call. It was rejected because NSGo v0.3.0's `gpu.DepthStencilAttachment` has no `ArrayLayer` field to attach individual cube faces, and cubemap depth attachments plus seamless cubemap PCF have backend-specific caveats on WebGL 2 and OpenGL ES 3.0. Packing the six faces into a `3×2` `FormatDepth32Float` 2D render target with per-tile scissor and UV clamping works identically across all six NSGo backends and the software GPU renderer.

### Write linear distance via `@builtin(frag_depth)` in a fragment shader for point shadows

Writing `length(worldPos - lightPos) / range` to `@builtin(frag_depth)` simplifies depth bias to a uniform world-space offset across all distances. It was rejected because writing `frag_depth` disables hardware early-Z optimization on every backend and requires fragment-shader execution during shadow passes, whereas analytic perspective NDC depth `(far/(far-near)) * (1 - near/axisDist)` matches the vertex-only hardware depth buffer exactly while keeping world-space `NormalBias` for grazing-angle acne suppression.

### Keep a separate light model (`SpotShadowLight`, `PointShadowLight`) with its own lighting helpers

The first version shipped its own light structs and `evaluate_*_lighting` WGSL. It was rejected in review: it disagreed with the `LightRig` (no 1/π in the diffuse term, a different near-field clamp, `Range` required where the rig allows 0), its ready-made pipelines had no per-draw model matrix and a constant albedo, and it hard-coded `@group(1)`, which collides with `LightGroup`. Deriving the shadow from the rig's lights and multiplying visibility into the existing PBR loop keeps one lighting model.

### Use one static caster uniform buffer overwritten per `DrawCaster`

A single 128-byte uniform buffer avoids sizing a `MaxCasters * 256` slot buffer. It was rejected because `Device.WriteBuffer` executes before the submitted command buffer runs, so drawing two or more casters in the same shadow pass (or across six faces in one command encoder) would silently overwrite earlier casters' transforms with the last caster's matrix.

## Consequences

- **Benefits**:
  - Perspective spot-light and omnidirectional 6-face point-light shadows work identically on full GPU profiles and `nsgotest.NoGPUCompute` (WebGL 2 / OpenGL ES 3.0), with zero-allocation uniform encoding (`SpotShadowUniform.Encode`, `PointShadowUniform.Encode`), multi-caster dynamic offsets, `Retained` device-loss restoration, and exact `ByteSize()` budget reporting.
  - Verified by `local_shadows_test.go` against the pure-Go references (`EvaluateSpotShadow`, `EvaluatePointShadow`, `ShadePointVisibility`) and golden reference PNGs (`local_shadow_spot.png`, `local_shadow_point.png`).
- **Costs**:
  - `PointShadowMap`'s `3×2` atlas clamps PCF taps inside each tile boundary rather than filtering across cube-face seams, which can leave a subtle 1-texel softness discontinuity right along a cube diagonal edge; increasing `FaceResolution` minimizes it.
  - Only the first spot light and the first point light of a rig can be shadowed by the ready-made pipelines, and both cannot be bound there at once because they share group 3; a custom shader binds both through the `ForGroup` variants.
  - Face 0 clears the whole point atlas, so a partial refresh must either include face 0 with every other face or skip it.
  - `DrawCaster` refuses draws past `MaxCasters` (default 256 per spot pass or per point face) and reports it by returning false rather than resizing GPU buffers mid-pass.
