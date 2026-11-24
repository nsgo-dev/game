# Agent Note: Dynamic Resolution Scaling in render3d

Status: implemented

## Problem

`game/quality` selects discrete low/medium/high presets and `game/render3d` tracks per-frame draw/memory budgets (`RenderStats`, `CheckBudget`), but neither adapts 3D render resolution continuously to hold a GPU frame-time budget while keeping 2D/UI HUD elements at crisp native resolution (#383, part of #243).

Naively reallocating an offscreen render target whenever resolution scale changes causes per-frame GPU texture allocation churn and stalls on mobile and WebGL 2 backends. Meanwhile, sampling a sub-rectangle of a larger offscreen texture without half-texel UV clamping bleeds unused padding texels along the right and bottom screen edges under bilinear filtering, and WGSL shaders in NSGo v0.3.0 must stay within the software GPU VM's instruction set (no `exp`/`exp2`/`log2`, single fragment entry point per compiled module).

## Decision

`packages/game/render3d/dynres.go` introduces two cooperating components:

- **Pure-Go proportional fill-rate controller (`DynamicResolutionScaler`)**: smooths incoming GPU frame times with an exponential moving average (`SmoothingAlpha`, default `0.25`), discards loading/resume spikes above `HitchLimit` (default `250ms`), and holds scale steady inside a symmetric `±Deadband` (default `±5%`) around `TargetFrameTime` (default `16.67ms`). Because fragment cost scales quadratically with linear resolution scale (`area = scale²`), overload and headroom both target `scale * sqrt(target / smoothed)` clamped to `[0.01, StepDown]` (fast response after `CooldownDown = 2` frames) and `[0.01, StepUp]` (smooth recovery after `CooldownUp = 12` consecutive headroom frames). `RecordPassTimings` bridges `gpu.Device.LastPassTimings` / `gpu.Frame.PassTimings` directly, and `SetScaleBounds` / `SetFixedScale` / `SetEnabled` let callers compose dynamic scaling with `quality.Selector` without importing `game/quality` (keeping `yaml/v3` out of `render3d`'s dependency graph).
- **Zero-reallocation offscreen scaled target and upscaler (`DynamicResolutionTarget`)**: allocates a single offscreen color (+ optional depth) `gpu.RenderTarget` sized to native capacity (`EnsureCapacity` grows on demand and only shrinks when native dimensions drop below half of allocated capacity). `Begin3D` targets that offscreen attachment and sets `SetViewport` and `SetScissor` to `[0, 0, scaledW, scaledH]` (aligned via `Alignment`, default even pixels). `Upscale` and `UpscaleIntoPass` accept `RenderPassRecorder` (satisfied by both `*gpu.Encoder` and `*gpu.Frame`) and sample the active sub-viewport with half-texel inset clamping (`clamp(in.uv * uv_scale, 0.5/alloc, (scaled-0.5)/alloc)`). Two separate single-entry WGSL modules (`dynResBlitWGSL` for `DynamicResolutionBilinear` / `DynamicResolutionNearest` and `dynResSharpenWGSL` for 5-tap min/max-clamped CAS sharpening `DynamicResolutionSharpen`) allow runtime filter switching via `SetFilter` with zero texture or pipeline reallocations.
- **Native-resolution HUD preservation**: because only the offscreen `Begin3D` pass runs at the scaled sub-viewport while `Upscale` presents to the full native surface, `scene.Overlay3D` (native `core.Canvas` stacked over `gpu.View`), a follow-up `gpu.LoadLoad` native GPU HUD pass, or HUD draws recorded right after `UpscaleIntoPass` all render at exact 1:1 native pixel resolution.

## Alternatives considered

### Reallocate the offscreen `gpu.RenderTarget` to exact `(scaledW, scaledH)` on every scale change

Allocating an exact-size offscreen target removes UV-scale uniform math and saves temporary VRAM when running below `1.0` scale. It was rejected because dynamic resolution changes scale frequently as camera framing shifts, and recreating color/depth attachments plus bind groups on every step causes frame hitches and memory fragmentation on mobile GPUs and WebGL 2. Sub-viewport rendering into a native-capacity target eliminates per-scale-change allocations completely while half-texel UV clamping eliminates border bleed.

### Import `game/quality` directly into `game/render3d`

Accepting `*quality.Selector` or `quality.Preset` directly on `DynamicResolutionScaler` would save callers one line when pairing quality presets with dynamic scaling. It was rejected because `game/quality` imports `go.yaml.in/yaml/v3`, which would bloat `render3d`'s dependency tree and WebAssembly binary budget; `SetScaleBounds(min, preset.ResolutionScale)` and `SetFixedScale` already compose cleanly without coupling the packages.

### Single WGSL module with multiple fragment entry points or uniform branching for sharpening

Putting bilinear and sharpen in one WGSL source with two `@fragment` functions or an `if (params.sharpness > 0.0)` branch around neighbor `textureSample` calls would avoid compiling a second small module. It was rejected because NSGo v0.3.0's `shaderir.Program` carries one fragment entry point per compiled module and strict WGSL uniformity analysis disallows `textureSample` inside branches on some WebGPU implementations.

## Consequences

- **Benefits**: 3D games smoothly hold GPU frame-time targets across fill-rate spikes with zero steady-state allocations and zero texture reallocations during scale adjustments, while keeping 2D/GPU HUD overlays at native 1:1 resolution. Unit and software-GPU tests (`dynres_test.go`) verify workload convergence, hitch rejection, full-viewport sub-rect UV accuracy, 1px native HUD sharpness, `scene.Overlay3D` integration on both full and `nsgotest.NoGPUCompute` profiles, and resize/lifecycle behavior.
- **Costs**: The backing offscreen `gpu.RenderTarget` reserves native-resolution color (+ depth) VRAM even while rendering at a reduced sub-viewport until native dimensions shrink below half capacity; games with strict VRAM ceilings can lower `MaxScale` or `Close` and recreate the target if needed. Hardware-backend timestamp accuracy on native GPUs remains covered by NSGo's own GPU test suite rather than headless software tests.
