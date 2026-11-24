# Agent Note: 3D render HDR targets, tone mapping, and sRGB/gamma pass

Status: implemented

## Problem

3D scenes lit in linear HDR space produce radiance values well above `1.0` on specular highlights and emissive surfaces. Rendering directly to an 8-bit UNORM swapchain clips everything above `1.0` to flat saturated bands and conflates linear lighting math with display gamma. `game/render3d` needs a portable HDR offscreen target, exposure control, standard filmic/PBR tone mapping operators, and correct linear-to-sRGB / power-law gamma handling (#250) that:

- preserves linear HDR radiance above `1.0` in `gpu.FormatRGBA16Float` while detecting devices that lack float target support and falling back cleanly to `gpu.FormatRGBA8Unorm`,
- runs identically across hardware GPU backends (Metal, D3D11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0) and NSGo v0.3.0's software GPU VM (including the `nsgotest.NoGPUCompute` profile),
- never double-applies gamma when targeting hardware sRGB framebuffers (`FormatRGBA8UnormSRGB` / `FormatBGRA8UnormSRGB`),
- survives GPU device loss when created with `Retained: true`, and
- incurs zero steady-state allocations per frame and keeps all new code isolated in `packages/game/render3d/tonemap.go` and `tonemap_test.go`.

## Decision

`packages/game/render3d/tonemap.go` adds a self-contained HDR and tone-mapping subsystem with matching CPU reference functions and a single full-screen WGSL pass:

- **`HDRTarget` (`NewHDRTarget`, `SupportsHDR`, `PreferredHDRFormat`)**: prefers `gpu.FormatRGBA16Float` when `dev.SupportsFormat(gpu.FormatRGBA16Float)` is true and falls back to `gpu.FormatRGBA8Unorm` otherwise (or if `CreateRenderTarget` reports `gpu.ErrUnsupported`), unless `HDRTargetDesc.DisableFallback` is set. MSAA `SampleCount > 1` falls back to `1` when `dev.Features().MSAA4x` is false. `HDRTarget.Resize` reallocates on size changes and closes the old attachments; `HDRTarget.ByteSize` reports exact color, resolve, and depth memory for budget accounting.
- **Exposure & transfer functions**: `ExposureFromEV(ev)` computes `2^ev` clamped to finite non-negative float32 values (`NaN -> 1.0`). `LinearToSRGBExact` and `SRGBToLinearExact` implement the exact IEC 61966-2-1 piecewise transfer functions matching NSGo's software renderer.
- **Tone mapping operators (`ToneMapOperator`, `ToneMapOptions`, `ApplyToneMap`)**: supports `ToneMapNone` (linear clamp), `ToneMapReinhard`, `ToneMapACES` (Narkowicz 2015 fit, the default in `DefaultToneMapOptions`), `ToneMapNeutral` (Khronos PBR Neutral), and `ToneMapAgX` (Troy Sobotka / Benjamin Wrensch AgX inset matrix, log2 sigmoid via 6th-order polynomial, outset matrix, and `2.2` linearization back to linear space so downstream display OETF applies uniformly across all operators). `ToneMapOptions.Normalized` canonicalizes non-positive/non-finite `Exposure` to `1.0`, non-positive/non-finite `Gamma` to `0` (exact sRGB), and unknown operators to `ToneMapNone`.
- **`ToneMapPass` & `ToneMapWGSL`**: draws a vertexless full-screen triangle (`@builtin(vertex_index)`, `pass.Draw(3, 0)`) binding the HDR texture, sampler, and a 16-byte uniform block (`ToneMapUniformSize = 16`, written with zero allocations by `AppendToneMapUniforms`) at `ToneMapGroup` (`0`). `ToneMapPass` caches the compiled `*gpu.ShaderModule` once via `sync.OnceValues` and caches the `*gpu.BindGroup` per source `*gpu.Texture` for zero steady-state allocations. When `TargetFormat.IsSRGB()` is true, `ToneMapPass` automatically sets `LinearOutput = true` (`inv_gamma = -1.0`) so hardware sRGB writes encode linear-to-sRGB once without double gamma.

### Shader VM & Backend Invariants

- `ToneMapWGSL` must never call `exp`, `exp2`, `log`, or `log2` builtins because NSGo v0.3.0's software GPU VM (`internal/driver/gpu_software.go`) omits those IR opcodes. `fast_log2_agx` evaluates `log2` over the AgX domain `[2^-12.47393, 2^4.026069]` via exact power-of-two range reduction into `[1/sqrt(2), sqrt(2)]` followed by a 4-term `atanh` Taylor polynomial (`< 5e-8` error, within float32 precision).
- `safe_pow_scalar` guards `x <= 0.0 -> 0.0` before calling `pow(x, p)`, avoiding undefined `pow(0, p)` `NaN` behavior on GLSL/HLSL/Metal drivers.
- All four uniform fields (`exposure`, `inv_gamma`, `operator_id`, `_pad`) are packed as `f32` (16 bytes, std140-aligned) to avoid integer-in-UBO quirks across WebGL 2 / OpenGL ES 3.0 drivers.
- `ToneMapPass.Write` and `ToneMapPass.Draw` accept an optional `*gpu.Device` (such as `frame.Device()`), updating the pass's device reference after a `Retained: true` device-loss restoration so subsequent uniform writes target the live restored device.

## Alternatives considered

### Why not call WGSL `log2()` in AgX?

Calling `log2()` directly is 1 line of WGSL and maps to native hardware instructions on Metal, D3D11, Vulkan, and WebGPU. However, NSGo v0.3.0's software GPU VM does not implement `shaderir.Log2` (returning zero silently), which breaks headless `nsgotest` verification and golden renders for AgX. Power-of-two range reduction plus a 4-term odd polynomial executes in pure arithmetic, matches `math.Log2` to float32 machine epsilon over AgX's 16.5-stop range, and runs identically on the software VM and every hardware backend.

### Why not bake one specialized WGSL pipeline per `ToneMapOperator`?

Compiling five separate pipelines avoids a uniform branch in `fs_main`. For a full-screen resolve pass where every pixel executes the same dynamically uniform `params.operator_id`, branch divergence is zero on all GPUs, while a single pipeline lets games switch operators live (for settings menus, photo mode, or A/B debugging) with a 16-byte uniform write and zero pipeline swaps or shader recompilations.

### Why not require a vertex buffer quad instead of `@builtin(vertex_index)`?

A 6-vertex buffer avoids depending on `vertex_index`. NSGo's WGSL compiler lowers `@builtin(vertex_index)` across all four shader targets (WGSL, GLSL, HLSL, MSL) as well as the software GPU VM, and a single full-screen triangle avoids the diagonal quad seam and saves one retained GPU buffer allocation per pass.

### Why not let callers opt out of automatic `LinearOutput` on sRGB targets?

Exposing a manual flag to force shader sRGB encoding onto a `FormatRGBA8UnormSRGB` target would allow intentional double-gamma testing. In practice, writing sRGB-encoded values into an sRGB render target is always a bug; automatically forcing `LinearOutput = true` when `TargetFormat.IsSRGB()` prevents silent double-gamma defects while callers who want custom non-sRGB gamma can target a linear UNORM format (`FormatRGBA8Unorm` / `FormatBGRA8Unorm`).

## Consequences

- **Benefit:** 3D scenes can render linear HDR radiance (`> 1.0`) into `FormatRGBA16Float` and tone-map cleanly with ACES, AgX, Khronos PBR Neutral, or Reinhard without clipping artifacts, verified pixel-for-pixel against CPU `ApplyToneMap` on both `full` and `NoGPUCompute` profiles.
- **Benefit:** Automatic hardware sRGB detection eliminates double-gamma mistakes across swapchain formats, and `Retained: true` lets both `HDRTarget` and `ToneMapPass` survive Android/WebGL context loss without manual re-upload.
- **Cost:** `fast_log2_agx` adds ~10 scalar comparisons per channel in the AgX fragment branch compared to a hardware `log2` instruction; if NSGo's software GPU VM later adds `shaderir.Log2`, `fast_log2_agx` can be simplified to `log2(clamp(v, ...))` without changing any public API or golden outputs.
- **Cost:** Custom power-law `Gamma` values (`Gamma > 0`) are ignored when targeting an sRGB hardware format (`TargetFormat.IsSRGB()`) because the hardware always applies exact sRGB encoding on write.

## Testing

- `packages/game/render3d/tonemap_test.go` verifies `ExposureFromEV`, `LinearToSRGBExact`/`SRGBToLinearExact` round-trip accuracy, `ApplyToneMap` monotonicity and HDR highlight compression across all 5 operators, 0-alloc `AppendToneMapUniforms`, `HDRTarget` lifecycle/resize/MSAA fallback, GPU-vs-CPU pixel agreement across 9 operator/exposure/gamma/linear configurations, hardware `FormatRGBA8UnormSRGB` vs shader-sRGB parity, `gpu.View` execution on both `full` and `nsgotest.NoGPUCompute`, `Retained: true` device-loss survival, scope disposal, fuzzing (`FuzzApplyToneMapFiniteAndBounded`), and golden reference renders (`testdata/golden/tonemap_aces.png`, `tonemap_agx.png`, `tonemap_neutral.png`).
- Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./render3d`, `go tool nsgo test -run TestStabilityTable .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
