# Agent Note: 3D Post-Processing (Bloom, SSAO, Depth of Field) in render3d

Status: implemented

## Problem

3D games built on `game/render3d` and NSGo's `gpu.View` need multi-pass offscreen 3D post-processing — HDR Bloom, depth-based Screen-Space Ambient Occlusion (SSAO), and Depth of Field (DoF) — running over `gpu.RenderTarget` color and depth attachments (#282, part of #279).

The existing 2D `game/effects` package fuses Kage fragment passes into a single draw without intermediate textures or a 3D depth buffer (see [CRT scanlines note](2026-11-24-effects-crt-scanlines.md)), so it cannot sample a 3D `texture_depth_2d` attachment, preserve HDR floating-point highlights (`FormatRGBA16Float`), or run multi-pass separable blurs. Building portable 3D post-processing on NSGo v0.3.0 also carries concrete cross-backend constraints:

- NSGo's software GPU VM omits `shaderir.Exp`, `shaderir.Exp2`, `shaderir.Log2`, and screen-space derivative builtins (`dpdx`/`dpdy`), so shaders that rely on `exp` Gaussian weights or `dpdx`/`dpdy` normal reconstruction fail or produce wrong output in headless tests.
- All `Device.WriteBuffer` calls queued within a frame execute before any render passes in that frame, so sharing one uniform buffer across horizontal and vertical blur passes overwrites the horizontal direction before it runs.
- WebGPU and GLES 3.0 require non-filtering (`FilterNearest`) samplers for non-comparison `texture_depth_2d` reads and require `textureSampleLevel` inside non-uniform control flow (loops or early-exit branches).
- WebGL 2 / OpenGL ES 3.0 (`nsgotest.NoGPUCompute`) lack compute shaders, so every pass must run as fragment render passes.

## Decision

`packages/game/render3d/post3d.go` adds standalone passes (`BloomPass`, `SSAOPass`, `DepthOfFieldPass`) and a composable `PostStack3D` manager with validated configs (`BloomConfig`, `SSAOConfig`, `DepthOfFieldConfig`, wrapping invalid inputs in `ErrInvalidPostProcess`):

- **Dedicated offscreen render targets and fixed stage order**: `PostStack3D` owns an offscreen color+depth scene target (`FormatRGBA16Float` + `FormatDepth32Float` by default, configurable to `FormatRGBA8Unorm` / `FormatDepth24Plus`) and two ping-pong color targets. Enabled passes run in order **SSAO -> Depth of Field -> Bloom** so ambient occlusion darkens crevices before optical blur and highlights bloom afterward. Each pass holds separate intermediate (`ColorFormat`) and output (`OutputFormat`) composite pipelines so the final enabled pass writes directly to the swapchain or destination texture without an extra copy pass.
- **Portable fragment-only WGSL**: All passes use a shared 3-vertex retained fullscreen triangle buffer (`z = 0.0`, avoiding far-plane `z = 1.0` clipping and empty-VAO WebGL 2 quirks), explicit `textureSampleLevel`, `FilterNearest` depth samplers, precomputed 9-tap separable Gaussian weights (Bloom), algebraic bilateral depth weights `1 / (1 + (dz / thresh)^2)` (SSAO blur), and edge-preserving 4-neighbor depth-difference normal reconstruction with a deterministic 4×4 stratified rotation pattern (SSAO). Zero `exp`/`exp2`/`log2`, `dpdx`/`dpdy`, or compute shaders are used.
- **Per-stage uniform buffers and bind-group caching**: Each sub-pass (`extract`, `horizBlur`, `vertBlur`, `composite`, `ssao`, `ssaoBlur`, `coc`, `bokeh`) owns its own uniform buffer so multi-pass submissions in one `gpu.Frame` or `gpu.Encoder` never clobber earlier passes' uniforms. External input bind groups are cached by texture handle and closed on change, `Resize`, or `Close` to avoid steady-state leaks.
- **Device-parameterized methods (`RenderPassRecorder`)**: Following [textures and samplers](2026-11-24-render3d-textures-and-samplers.md), passes never cache `*gpu.Device`; `Apply`, `ComputeAO`, and `Resize` accept `dev *gpu.Device` explicitly and the package's existing `RenderPassRecorder` interface (shared with `DynamicResolutionTarget`) satisfied by both `*gpu.Frame` and `*gpu.Encoder`, allowing `Retained: true` resources to survive device loss and restoration cleanly.

## Alternatives considered

### Compute-shader Bloom, SSAO, and DoF

Running separable blur, SSAO, and CoC gather in compute shaders with shared workgroup memory reduces texture bandwidth on desktop/modern mobile GPUs. Rejected because NSGo targets WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`), where compute pipelines and storage textures return `gpu.ErrUnsupported`. Fragment passes over `gpu.RenderTarget` work identically across Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the software device.

### Reconstructing SSAO normals with `dpdx`/`dpdy`

Using `normalize(cross(dpdx(viewPos), dpdy(viewPos)))` avoids four neighboring depth texture fetches per pixel. Rejected because NSGo's software GPU VM does not evaluate 2×2 fragment quads or implement `Dfdx`/`Dfdy`, and raw `dpdx`/`dpdy` produces silhouette-edge normal artifacts across depth discontinuities whereas minimum-delta 4-neighbor depth differencing is edge-preserving and deterministic everywhere.

### Extending `game/effects` with 3D post-processing

Placing 3D Bloom, SSAO, and DoF in `game/effects` would group all screen-space effects under one package. Rejected because `game/effects` is coupled to Kage single-pass fusion over 2D `core.ImageSource` layers without `gpu.Device`, `gpu.RenderTarget`, `texture_depth_2d`, or `scene3d.Camera` projection matrices; keeping 3D post-processing in `game/render3d` aligns with `CameraBuffer`, `GPUMesh`, and `GPUTexture`.

## Consequences

- **Benefits**: Games get composable or standalone HDR Bloom, depth-reconstructed SSAO (with inspectable `AOTexture()` and bilateral blur), and depth-aware disc bokeh Depth of Field running on every NSGo backend including `NoGPUCompute`, with `Resize`, `Retained` device-loss restoration, zero steady-state bind-group leaks, and deterministic golden reference tests (`post3d_bloom.png`, `post3d_ssao.png`, `post3d_dof.png`, `post3d_combined.png`).
- **Costs**: `PostStack3D` allocates offscreen scene, ping-pong, and per-pass render targets up front (even for passes initially disabled, so toggling via `SetSSAO`/`SetDepthOfField`/`SetBloom` allocates nothing at runtime); callers needing only one effect can instantiate `BloomPass`, `SSAOPass`, or `DepthOfFieldPass` standalone to avoid unused target memory. SSAO bilateral blur runs as a 2D neighborhood loop (capped at `BlurRadius <= 4`) rather than a separable pass, trading slightly more software-VM test time for stronger diagonal edge preservation.
