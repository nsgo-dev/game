# Agent Note: Distance and height fog in render3d

Status: implemented

## Problem

`game/render3d` needs 3D atmospheric fog (#254, under #251) covering linear, exponential, and exponential-squared distance fog as well as analytical exponential height fog along the view ray. The implementation must satisfy four constraints simultaneously:

1. CPU reference evaluation (`Fog.Factor`, `Fog.Apply`) and GPU WGSL shaders (`fog_factor`, `apply_fog`) must match within 1–2 LSBs across all modes and altitudes, including grazing horizontal rays, vertical rays, and high cameras looking down into low valleys without `NaN` or `Inf`.
2. GPU uniform packing must be 16-byte aligned and allocation-free across all NSGo GPU backends (Metal, D3D11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and NSGo's software GPU renderer).
3. `GPUFogBuffer` must work with `core.Scope` disposal and `Retained: true` automatic device-loss recovery, including accepting `Update` calls after a restored device replaces the lost `*gpu.Device`.
4. `FogWGSL` must compose cleanly into arbitrary custom shaders as well as a built-in lit/fogged pipeline without conflicting with parallel `render3d` additions (`texture.go`, `light.go`, `ktx2.go`, `morph.go`).

## Decision

`packages/game/render3d/fog.go` implements distance and analytical exponential height fog as a self-contained pair of CPU evaluators, std140/WGSL uniform layout, GPU buffer wrapper, portable WGSL snippets, and a lit/fogged `FogPipeline`.

### Uniform contract and shader layout

- `FogUniforms` packs into three `vec4<f32>` fields (`FogUniformSize = 48` bytes) with zero implicit padding:
  - `color_opacity`: linear RGB fog color in `.xyz`, clamped `MaxOpacity` in `.w` (zero `Fog.MaxOpacity` defaults to `1.0` so idiomatic struct literals apply full fog).
  - `distance_params`: `f32(Mode)` in `.x` (`0=off`, `1=linear`, `2=exp`, `3=exp2`), `Start` in `.y`, `End` in `.z`, `Density` in `.w`.
  - `height_params`: `height_enabled` (`0` or `1`) in `.x`, `HeightBase` in `.y`, `HeightFalloff` in `.z`, `HeightDensity` in `.w`.
- `FogWGSL` declares `struct Fog`, `fog_exp`, `fog_factor(cam_pos, world_pos, fog)`, and `apply_fog(color, cam_pos, world_pos, fog)` with **no** `@group/@binding` declaration. Shaders that bind `GPUFogBuffer` at `FogGroup` (group 1, alongside `CameraGroup` = 0) prepend `FogBindingWGSL`; custom shaders can bind `Fog` at any group/binding or embed it inside a larger scene uniform block.
- `NewFogPipeline` compiles `FogLitWGSL` using `MeshVertexLayout`, `CameraBuffer` at `CameraGroup` (0), `GPUFogBuffer` at `FogGroup` (1), and `GPUFogObject` at `FogObjectGroup` (2), handling both perspective (`camera.eye.w > 0.5`) and orthographic (`camera.eye.w <= 0.5`) view rays and zero-length normals safely.

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

### Why omit `@group/@binding` from `FogWGSL`?

Declaring `@group(1) @binding(0) var<uniform> fog: Fog;` inside `FogWGSL` would save callers one line when using `GPUFogBuffer` at `FogGroup`. However, engines or materials that already occupy group 1 (or pack fog inside a shared environment buffer) would be unable to reuse `FogWGSL` without string-rewriting it. Splitting `FogWGSL` (struct + pure functions) from `FogBindingWGSL` (`@group(1) @binding(0)`) serves both standalone pipelines and custom shader layouts.

## Consequences

- **Benefit:** CPU `Fog.Factor`/`Fog.Apply` and GPU `FogWGSL`/`FogLitWGSL` agree within 1–2 LSBs across all distance modes, height configurations, orthographic and perspective cameras, and WebGL 2 / GLES 3 capability profiles.
- **Benefit:** `AppendFogUniforms` allocates zero bytes per call (~29 ns/op), and `GPUFogBuffer` survives device loss cleanly when `Retained: true`.
- **Cost:** `FogUniforms` encodes `Mode` and `HeightEnabled` as `f32` flags inside `vec4<f32>` rather than `u32`, which avoids cross-backend std140/uint alignment pitfalls at the cost of floating-point range comparisons (`mode > 0.5 && mode < 1.5`) in WGSL.
- **Cost:** `FogLitWGSL` uses a single directional light plus ambient term on rigid/uniformly-scaled `Model` matrices; callers requiring PBR lighting or non-uniform scale normal matrices embed `FogWGSL` into their own shader.

## Testing

- `packages/game/render3d/fog_test.go` verifies exact analytical values at set distances (`FogOff`, `FogLinear`, `FogExp`, `FogExp2`, `MaxOpacity`), height fog at/below/above `HeightBase`, vertical and grazing-ray continuity, high-camera-looking-down stability, validation/sanitization, zero-allocation uniform encoding (`BenchmarkAppendFogUniforms`, `FuzzFogUniformsAndFactor`), software GPU pixel parity against CPU reference within 2 LSBs, golden renders (`fog_linear_distances.png`, `fog_height.png`), `nsgotest` `gpu.View` rendering under both full and `nsgotest.NoGPUCompute` profiles, dynamic `Update`, `Retained` device-loss restoration, and `core.Scope` disposal.
- Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./render3d/... .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
