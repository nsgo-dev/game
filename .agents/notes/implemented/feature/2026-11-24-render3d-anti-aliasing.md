# Agent Note: 3D rendering anti-aliasing (MSAA target & FXAA 3.11 pass)

Status: implemented

## Problem

3D scenes drawn on NSGo's GPU view (#243, #249) show jagged stair-stepping along diagonal edges at 1x sampling. Games need two complementary anti-aliasing paths that work across every NSGo backend (Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the headless software device) without compute shaders:

1. Hardware MSAA for forward rendering when the active device supports it (`Features.MSAA4x`), with automatic resolve, viewport resize, clean fallback to 1x when MSAA is unavailable, and device-loss retention.
2. A single-sampled FXAA 3.11 post-processing pass for deferred pipelines, post-FX chains, or devices without hardware MSAA, backed by an equivalent pure-Go CPU reference for deterministic headless verification.

## Decision

Add a self-contained anti-aliasing module in `packages/game/render3d/aa.go` with two cohesive layers:

- **Mode & sample-count clamping (`AAMode`, `ClampSampleCount`)**: `AAMode` exposes `AANone`, `AAFXAA`, `AAMSAA2x`, and `AAMSAA4x`. `ClampSampleCount(dev, requested)` returns `1` when `requested <= 1`, `dev == nil`, or `!dev.Features().MSAA4x`, and `4` when `requested >= 2` and `dev.Features().MSAA4x` is true. Every returned value is guaranteed valid for `gpu.TextureDesc.SampleCount`, `gpu.RenderTargetDesc.SampleCount`, and `gpu.PipelineDesc.SampleCount`.
- **Hardware MSAA target (`MSAATarget`)**: Wraps `*gpu.RenderTarget` with `ClampSampleCount`, optional depth, zero-allocation no-op `Resize(w, h)` when dimensions match (and atomic swap preserving the old target if reallocation fails), `Retained` device-loss support (plus `SetDevice` to rebind after restoration), and `OutputTexture()` returning `Resolve` when multisampled or `Color` when 1x. `ColorAttachment(clear)` automatically binds `ResolveTarget` with `StoreDiscard` on the transient multisample buffer when multisampled, and uses `StoreStore` without `ResolveTarget` when single-sampled.
- **FXAA 3.11 post-processing (`FXAAOptions`, `FXAAWGSL`, `FXAAPass`, `ApplyFXAA`)**:
  - `FXAAWGSL` draws a bufferless full-screen triangle via `@builtin(vertex_index)` (`Draw(3, 0)`), reads a 32-byte `FXAAParams` uniform (`two vec4<f32>` fields for identical alignment across WGSL, MSL, HLSL, and GLSL std140), and uses `textureSampleLevel(..., 0.0)` everywhere so non-uniform early exits and the bidirectional edge-search loop remain valid under uniform-control-flow validation on every backend.
  - Boolean XOR in the edge-variation check is written as `(endSmaller && !lumaCenterSmaller) || (!endSmaller && lumaCenterSmaller)` rather than `!=` on `bool`, because NSGo v0.3.0's software GPU VM (`cmpVal`) compares `.f[0]` on `shaderir.Bool` values.
  - `FXAAPass` caches the source texture's bind group (keyed by texture pointer and epoch) and last encoded uniform bytes, rejecting multisampled source textures synchronously with `gpu.ErrInvalidDescriptor`.
  - `ApplyFXAA(src, opts)` executes the exact same float32 FXAA 3.11 algorithm on CPU `*image.RGBA`, copying pixels verbatim on the contrast early-out so flat interiors stay untouched and alpha is preserved.

### Invariants

- `ClampSampleCount` must never return a sample count rejected by NSGo's `CreateTexture`, `CreateRenderTarget`, or `CreatePipeline` (only `1` or `4` when `Features.MSAA4x` is set).
- `FXAAPass` must never depend on compute shaders or storage buffers so it runs identically on `full` and `NoGPUCompute` (WebGL 2 / OpenGL ES 3.0) profiles.
- Flat interiors below `max(EdgeThresholdMin, lumaMax * EdgeThreshold)` must remain untouched and source alpha must be preserved by both GPU `FXAAPass` and CPU `ApplyFXAA`.

## Alternatives considered

### Why promote `AAMSAA2x` to `4` in `ClampSampleCount` instead of clamping down to `1`?

Clamping `2` down to `1` would strictly honor "never exceed requested samples" and save memory bandwidth on mobile devices if 2x existed. However, both WebGPU and NSGo v0.3.0 only accept sample counts `1` and `4` (`CreateTexture` and `CreatePipeline` reject `SampleCount == 2` with `ErrInvalidDescriptor`). Clamping `2` down to `1` would silently disable anti-aliasing whenever a caller selects `AAMSAA2x` on every device, including desktop Metal and WebGPU. Promoting `requested >= 2` to `4` when `Features.MSAA4x` is available (and falling back to `1` when unsupported) ensures `AAMSAA2x` delivers real hardware MSAA on capable GPUs while never failing descriptor validation.

### Why use a fragment render pass with `textureSampleLevel` instead of a compute shader for FXAA?

A compute shader writing to a storage texture can share luma values in workgroup shared memory to reduce redundant texture fetches. However, NSGo targets WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`), where `Features.Compute` and `Features.StorageTextures` are false, and rendering directly into the swapchain drawable requires a fragment pass anyway. A bufferless full-screen fragment triangle with `textureSampleLevel(..., 0.0)` works identically across every backend and writes directly to the swapchain or another offscreen texture with zero extra blit passes.

### Why keep `FXAA` contrast search threshold at canonical `0.25 * max(grad1, grad2)` despite 1-ULP UV ties?

Replacing `0.25` with a biased constant (e.g. `0.251`) could avoid exact ties at half-texel bilinear corners (`0.25*A + 0.75*B - 0.5*(A+B) = -0.25*(A-B)`), making GPU and CPU bit-identical on synthetic step edges. Doing so would deviate from Timothy Lottes' canonical FXAA 3.11 behavior and still would not eliminate subtexel precision differences across real hardware texture units (which use 8-bit fixed-point bilinear weights). Keeping canonical `0.25` preserves standard FXAA 3.11 quality while parity tests enforce `>= 99.5%` pixels within 2 channel levels and mean absolute error `< 0.1/255` (matching `camera_test.go`'s golden tolerance).

## Consequences

- **Benefit:** Games get drop-in MSAA (`MSAATarget`) and post-process FXAA 3.11 (`FXAAPass`) with automatic capability fallback, zero-allocation no-op `Resize`, device-loss retention, and deterministic CPU/GPU golden tests across `full` and `NoGPUCompute` profiles.
- **Benefit:** Placing all code in new files (`aa.go`, `aa_test.go`) keeps the feature isolated from concurrent `render3d` pipeline work.
- **Cost:** `AAMSAA2x` currently consumes 4x MSAA memory on MSAA4x devices until NSGo exposes native 2x sample counts; if NSGo adds `Features.MSAA2x`, update `ClampSampleCount` and this note in place.
- **Cost:** Retained `MSAATarget` and `FXAAPass` instances require callers to call `SetDevice(dev)` in `OnReady` / `OnDeviceRestored` after a device loss if they perform subsequent `Resize` or uniform/bind-group updates.

## Testing

- `packages/game/render3d/aa_test.go` verifies `AAMode` and `ClampSampleCount` (with/without `MSAA4x`, nil device), `MSAATarget` lifecycle/resize/fallback/nil-safety, hardware 4x MSAA diagonal smoothing and flat-interior preservation (`aa_baseline.png`, `aa_msaa4x.png`), `FXAAWGSL` compilation, `FXAAOptions.Sanitize`, `FXAAPass` lifecycle/validation, GPU `FXAAPass` vs CPU `ApplyFXAA` parity (`aa_fxaa.png`), `ApplyFXAA` edge cases (nil, tiny, sub-image, `DisableSubpixel`), end-to-end `gpu.View` pixel readback on both `full` and `NoGPUCompute` profiles, retained device-loss restoration, and benchmarks (`BenchmarkApplyFXAA`, `BenchmarkMSAATargetResizeNoop`, `BenchmarkFXAAPassRecordSteadyState`).
- Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./render3d`, `go tool nsgo test -run TestStabilityTable .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
