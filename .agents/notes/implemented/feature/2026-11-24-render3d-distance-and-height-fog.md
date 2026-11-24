# Agent Note: Distance and height fog in render3d

Status: implemented

## Problem

`game/render3d` needs 3D atmospheric fog (#254, under #251) covering linear, exponential, and exponential-squared distance fog as well as analytical exponential height fog along the view ray. The implementation must satisfy four constraints simultaneously:

1. CPU reference evaluation (`Fog.Factor`, `Fog.Apply`) and GPU WGSL shaders (`fog_factor`, `apply_fog`) must match within 1–2 LSBs across all modes and altitudes, including grazing horizontal rays, vertical rays, and high cameras looking down into low valleys without `NaN` or `Inf`.
2. GPU uniform packing must be 16-byte aligned and allocation-free across all NSGo GPU backends (Metal, D3D11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and NSGo's software GPU renderer).
3. `GPUFogBuffer` must work with `core.Scope` disposal and `Retained: true` automatic device-loss recovery, including accepting `Update` calls after a restored device replaces the lost `*gpu.Device`.
4. Fog must reach the engine's real lit pipelines (`NewLitPBRPipeline` with `LightGroup` = 1 and `PBRDrawGroup` = 2, and `NewMaterialPipeline` with `MaterialGroup` = 1), not only a separate sample pipeline, and `FogWGSL` must still compose into custom shaders.

## Decision

`packages/game/render3d/fog.go` implements distance and analytical exponential height fog as CPU evaluators, a std140/WGSL uniform layout, a GPU buffer wrapper, portable WGSL snippets, and fogged variants of the two lit pipelines: `LitPBRFogWGSL` / `NewLitPBRFogPipeline` and `MaterialFogWGSL` (compiled by `NewMaterialPipeline` when `MaterialPipelineOptions.FogLayout` is set).

### Uniform contract and shader layout

- `FogUniforms` packs into three `vec4<f32>` fields (`FogUniformSize = 48` bytes) with zero implicit padding:
  - `color_opacity`: linear RGB fog color in `.xyz`, clamped `MaxOpacity` in `.w` (zero `Fog.MaxOpacity` defaults to `1.0` so idiomatic struct literals apply full fog).
  - `distance_params`: `f32(Mode)` in `.x` (`0=off`, `1=linear`, `2=exp`, `3=exp2`), `Start` in `.y`, `End` in `.z`, `Density` in `.w`.
  - `height_params`: `height_enabled` (`0` or `1`) in `.x`, `HeightBase` in `.y`, `HeightFalloff` in `.z`, `HeightDensity` in `.w`.
- `FogWGSL` declares `struct Fog`, `fog_exp`, `fog_factor(cam_pos, world_pos, fog)`, and `apply_fog(color, cam_pos, world_pos, fog)` with **no** `@group/@binding` declaration. `FogBindingWGSL` binds it at `FogGroup` (group 3, after camera 0, lights 1 and the per-draw PBR block 2) and `MaterialFogBindingWGSL` at `MaterialFogGroup` (group 2, after camera 0 and material 1). Custom shaders can bind `Fog` anywhere or embed it in a larger uniform block.
- `FogCameraWGSL`'s `fog_camera_pos(world_pos)` and the CPU `FogCameraPos(CameraUniform, worldPos)` give the ray's start: the eye under a perspective camera (`camera.eye.w > 0.5`), and under an orthographic camera the point on the camera plane the surface projects from (`world_pos + eye.xyz * view depth`).
- `LitPBRWGSL` and `MaterialWGSL` are split into shared prelude/fragment-head/tail constants so the fogged variants reuse the exact lighting code and only change the output line: lit PBR fogs the linear color before `linear_to_srgb`, and the material shader fogs the linear radiance before its Reinhard tone map.

### Analytical height fog and transmittance composition

Height fog models vertical extinction $\sigma(y) = \rho_h e^{-k(y - y_0)}$ integrated analytically along the ray segment from `camPos` to `worldPos` of length $d$:

- Let $h_0 = \text{camPos}.y - y_0$, $h_1 = \text{worldPos}.y - y_0$, $h_{\min} = \min(h_0, h_1)$, $\Delta = |h_1 - h_0|$, and $u = k \Delta \ge 0$.
- The optical depth is $\tau_h = \rho_h \cdot d \cdot e^{\text{clamp}(-k h_{\min}, -80, 40)} \cdot I(u)$, where $I(u) = (1 - e^{-\min(u, 80)})/u$ for $u \ge 0.05$ and the four-term Taylor polynomial $1 - \frac{u}{2} + \frac{u^2}{6} - \frac{u^3}{24}$ for $u < 0.05$.
- Distance and height fog combine by multiplying transmittances: `combined = 1 - (1 - f_dist) * (1 - f_height)`, then clamped to `[0, MaxOpacity]`.

### Device-loss recovery for `GPUFogBuffer`

`GPUFogBuffer.Update(dev *gpu.Device, fog Fog)` accepts the active `*gpu.Device` (falling back to the stored device when `dev == nil`). When `Retained: true`, NSGo's `restoreRetainedFrom` updates the underlying `*gpu.Buffer`'s internal device pointer, layout, and bind group with its shadow bytes automatically, and passing `frame.Device()` to `Update` refreshes `GPUFogBuffer`'s device reference so subsequent updates write to the restored device instead of no-oping against the lost one.

## Alternatives considered

### Why `pow(2.718281828, x)` (`fog_exp`) instead of WGSL `exp(x)` in `FogWGSL`?

Calling WGSL's built-in `exp(x)` directly is shorter and idiomatic. However, `github.com/nsgo-dev/nsgo@v0.3.0`'s software GPU VM (`internal/driver/gpu_software.go:evalCall`) implements `shaderir.Pow` (`math.Pow`) but does not handle `shaderir.Exp`, causing `exp(x)` to return `0.0` (and `1 - exp(-x)` to saturate to `1.0`) in headless software GPU tests. Evaluating `pow(2.718281828, x)` with a strictly positive constant base is valid for all finite exponents in WGSL, GLSL/WebGL 2, MSL, and HLSL, compiles to `exp2(x * log2(e))` on hardware GPUs, and executes accurately in NSGo's software renderer. Revisit switching `fog_exp` back to `exp(x)` once `nsgo` adds `shaderir.Exp` to its software VM.

### Why symmetric `h_min` + four-term Taylor expansion instead of naive `exp(-k*h0) * (1 - exp(-k*dy)) / (k*dy)`?

The textbook one-sided height-fog form `exp(-k*h0) * (1 - exp(-k*(h1-h0))) / (k*(h1-h0))` uses one fewer `min`/`abs`. However, when a high camera looks down into a valley (`k*h0 > 90` and `k*(h1-h0) < -90`), `exp(-k*h0)` underflows to `0.0` while `exp(-k*(h1-h0))` overflows to `+Inf` in `f32`, producing `0 * Inf = NaN`. Factoring out `exp(-k*h_min)` keeps the second exponent `-k*|h1-h0|` strictly non-positive (never overflowing), enforces Helmholtz reciprocity (`Factor(a, b) == Factor(b, a)`), and switching to the four-term Taylor series below `u = 0.05` avoids `f32` subtractive cancellation on grazing horizontal rays (keeping error below `3e-6`).

### Why multiply transmittances instead of `max(f_dist, f_height)`?

Taking `max(f_dist, f_height)` is simpler and guarantees the result stays in `[0, 1]`. However, it produces a visible slope discontinuity where distance fog overtakes height fog and under-attenuates rays passing through both media. Multiplying transmittances `1 - (1 - f_dist) * (1 - f_height)` is smooth everywhere, exact for additive exponential optical depths (`exp(-tau_d) * exp(-tau_h) = exp(-(tau_d + tau_h))`), and reduces identically to either component when the other is disabled.

### Why fog at group 3 for lit PBR but group 2 for materials?

One group index for both pipelines would be simpler for callers. Group 1 is taken by lights and materials and group 2 by the per-draw PBR block, so the lit PBR pipeline needs group 3. The material pipeline leaves group 2 empty, and NSGo v0.3.0 refuses a pipeline layout with a nil entry (`gpu/device.go` rejects nil `PipelineDesc.Layouts`), so putting its fog at group 3 would force callers to bind an empty placeholder group at 2. Using the first free group in each pipeline keeps both free of placeholders; `MaterialFogGroup` names the difference.

### Why drop the standalone `FogPipeline`?

The first version shipped a Lambert-only `FogPipeline` with its own `FogObject` per-draw block at group 2. It duplicated `PBRDrawBuffer` and left the engine's real lit pipelines without fog, so games using `NewLitPBRPipeline` or `NewMaterialPipeline` would have had to write their own shaders. Fogged variants of those pipelines replace it.

### Why omit `@group/@binding` from `FogWGSL`?

Declaring the binding inside `FogWGSL` would save callers one line. However, the two built-in pipelines already need different groups, and shaders that pack fog inside a shared environment buffer would be unable to reuse `FogWGSL` without string-rewriting it. Splitting `FogWGSL` (struct + pure functions) from `FogBindingWGSL` / `MaterialFogBindingWGSL` serves both.

## Consequences

- **Benefit:** CPU `Fog.Apply` over `ShadePoint` / `EvaluateMaterial` and the GPU fogged pipelines agree within 2–3 LSBs across all distance modes, height configurations, orthographic and perspective cameras, and WebGL 2 / GLES 3 capability profiles.
- **Benefit:** `AppendFogUniforms` allocates zero bytes per call (~29 ns/op), and `GPUFogBuffer` survives device loss cleanly when `Retained: true`.
- **Cost:** `FogUniforms` encodes `Mode` and `HeightEnabled` as `f32` flags inside `vec4<f32>` rather than `u32`, which avoids cross-backend std140/uint alignment pitfalls at the cost of floating-point range comparisons (`mode > 0.5 && mode < 1.5`) in WGSL.
- **Cost:** fog sits at different group indices in the two pipelines (`FogGroup` = 3, `MaterialFogGroup` = 2), so callers bind it with `GPUFogBuffer.Bind` for lit PBR and `SetBindGroup(MaterialFogGroup, ...)` for materials.

## Testing

- `packages/game/render3d/fog_test.go` verifies exact analytical values at set distances (`FogOff`, `FogLinear`, `FogExp`, `FogExp2`, `MaxOpacity`), height fog at/below/above `HeightBase`, vertical and grazing-ray continuity, high-camera-looking-down stability, validation/sanitization, zero-allocation uniform encoding (`BenchmarkAppendFogUniforms`, `FuzzFogUniformsAndFactor`), software GPU pixel parity against `ShadePoint` and `EvaluateMaterial` blended by `Fog.Apply` within 2–3 LSBs, `FogCameraPos` under orthographic and perspective cameras, golden renders (`fog_linear_distances.png`, `fog_height.png`), `nsgotest` `gpu.View` rendering under both full and `nsgotest.NoGPUCompute` profiles, dynamic `Update`, `Retained` device-loss restoration, and `core.Scope` disposal.
- Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./render3d/... .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
