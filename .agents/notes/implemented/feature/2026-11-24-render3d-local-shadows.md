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

- **3×2 2D `FormatDepth32Float` atlas for point lights (`PointShadowMap`)**: All six 90° cube faces (`gpu.CubeFacePosX`..`gpu.CubeFaceNegZ`) are packed into a single `(3 * FaceResolution) × (2 * FaceResolution)` depth-only `gpu.RenderTarget`. `BeginFacePass(rec, face)` clears the entire atlas on `face == 0` (`LoadClear`), preserves previous tiles on `face` 1..5 (`LoadLoad`), and locks both `SetViewport` and `SetScissor` to that face's tile so geometry cannot bleed into neighboring faces.
- **Analytic perspective depth without a depth fragment shader**: Both spot and point shadow passes use a vertex-only depth pipeline (`vs_local_shadow_depth`) writing standard `[0, 1]` perspective depth. In `PointShadowWGSL`, `sample_point_shadow` selects the dominant cube face from `worldPos + worldNormal * normalBias - lightPos`, projects `(xView, yView, axisDist)` directly into tile-local UV, computes the exact NDC depth `(far/(far-near)) * (1 - near/axisDist)` without uploading six `mat4x4`s to the fragment shader, and clamps every tap inside `[0.51/faceRes, 1 - 0.51/faceRes]` so linear comparison and 3×3 PCF taps never sample across tile boundaries.
- **Portable WGSL and `textureSampleCompareLevel`**: `SpotShadowWGSL` and `PointShadowWGSL` avoid `exp`/`exp2`/`log2` completely (using algebraic windowed inverse-square attenuation and smooth cubic cone falloff) and call `textureSampleCompareLevel` so bounds checks and filter branches remain valid under WebGPU's strict uniformity analysis as well as WebGL 2, GLES 3.0, and the software renderer.
- **256-byte dynamic-offset caster uniform slots**: Both shadow maps allocate a `casterBuf` sized to `MaxCasters * 256` bytes and bind it with `HasDynamicOffset: true`. Each `DrawCaster(dev, pass, mesh, model)` writes `(lightViewProj, model)` to the next aligned slot and binds its dynamic offset before `mesh.Draw(pass)`, allowing multiple casters with distinct transforms in a single pass (or across all six point-light faces in one encoder/frame).
- **Recorder-agnostic passes and explicit live `*gpu.Device`**: `BeginShadowPass` and `BeginFacePass` accept `LocalShadowPassRecorder` (satisfied by both `*gpu.Encoder` and `*gpu.Frame`), and `Update` / `DrawCaster` accept `dev *gpu.Device` explicitly so `Retained: true` shadow maps survive device loss and restore cleanly.
- **Pure-Go CPU reference evaluators**: `EvaluateSpotShadow`, `EvaluateSpotLighting`, `PointShadowSelectFace`, `EvaluatePointShadow`, and `EvaluatePointLighting` mirror the WGSL math (including nearest vs linear depth comparison and clamped 3×3 PCF) for deterministic GPU-vs-CPU verification in `local_shadows_test.go`.

## Alternatives considered

### Render point shadows into a `DimCube` depth texture with `texture_depth_cube`

A native cubemap depth texture lets hardware select faces and filter seamlessly across cube edges in a single `textureSampleCompare` call. It was rejected because NSGo v0.3.0's `gpu.DepthStencilAttachment` has no `ArrayLayer` field to attach individual cube faces, and cubemap depth attachments plus seamless cubemap PCF have backend-specific caveats on WebGL 2 and OpenGL ES 3.0. Packing the six faces into a `3×2` `FormatDepth32Float` 2D render target with per-tile scissor and UV clamping works identically across all six NSGo backends and the software GPU renderer.

### Write linear distance via `@builtin(frag_depth)` in a fragment shader for point shadows

Writing `length(worldPos - lightPos) / range` to `@builtin(frag_depth)` simplifies depth bias to a uniform world-space offset across all distances. It was rejected because writing `frag_depth` disables hardware early-Z optimization on every backend and requires fragment-shader execution during shadow passes, whereas analytic perspective NDC depth `(far/(far-near)) * (1 - near/axisDist)` matches the vertex-only hardware depth buffer exactly while keeping world-space `NormalBias` for grazing-angle acne suppression.

### Use one static caster uniform buffer overwritten per `DrawCaster`

A single 128-byte uniform buffer avoids sizing a `MaxCasters * 256` slot buffer. It was rejected because `Device.WriteBuffer` executes before the submitted command buffer runs, so drawing two or more casters in the same shadow pass (or across six faces in one command encoder) would silently overwrite earlier casters' transforms with the last caster's matrix.

## Consequences

- **Benefits**:
  - Perspective spot-light and omnidirectional 6-face point-light shadows work identically on full GPU profiles and `nsgotest.NoGPUCompute` (WebGL 2 / OpenGL ES 3.0), with zero-allocation uniform encoding (`SpotShadowUniform.Encode`, `PointShadowUniform.Encode`), multi-caster dynamic offsets, `Retained` device-loss restoration, and exact `ByteSize()` budget reporting.
  - Verified by `local_shadows_test.go` against both pure-Go CPU reference evaluators (`EvaluateSpotLighting`, `EvaluatePointLighting`) and golden reference PNGs (`local_shadow_spot.png`, `local_shadow_point.png`).
- **Costs**:
  - `PointShadowMap`'s `3×2` atlas clamps PCF taps inside each tile boundary rather than filtering across cube-face seams, which can leave a subtle 1-texel softness discontinuity right along a cube diagonal edge; increasing `FaceResolution` minimizes it.
  - `DrawCaster` caps draws per shadow submission at `MaxCasters` (default 256 for spot, 1024 for point) and silently skips excess draws rather than resizing GPU buffers mid-pass; callers with larger caster counts should raise `MaxCasters` in the config.
