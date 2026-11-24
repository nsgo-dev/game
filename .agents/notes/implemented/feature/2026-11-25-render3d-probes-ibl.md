# Agent Note: Reflection probes and light probes integrated into LitPBRWGSL IBL

Status: implemented

## Problem

3D scenes in `game/render3d` need spatially varying local reflections (with parallax box projection and smooth blending between nearby probes over the global sky) and spatially interpolated diffuse indirect lighting for dynamic objects moving between rooms or lighting zones (#384).

An earlier attempt introduced separate `ProbeCubemap`, `SH9`, and standalone `ProbeRenderer` types alongside `skybox.go`'s existing `Cubemap`/`GPUCubemap`/`IrradianceSH`/`IBLGroup` pipeline, assumed `gpu.Device.ReadTexture` completed synchronously after `WaitIdle`, leaked previous cubemaps on re-bake, lacked inverse-transpose normal transforms and sRGB encoding, and diverged between CPU and GPU on unbaked/dirty probes and nil skies.

## Decision

`packages/game/render3d/probes.go` builds reflection probes and light probes directly on `skybox.go`'s existing cubemap and spherical harmonic types and plugs them into `LitPBRWGSL` at `IBLGroup` (group 3):

- **Shared environment primitives (no duplicate cubemap or SH types)**:
  - `ReflectionProbe` bakes into `Cubemap`, prefilters roughness mips with `PrefilterEnvironment`, and uploads to `GPUCubemap` via `UploadCubemap` (see [3D skybox and image-based lighting (IBL)](2026-11-24-render3d-skybox-and-ibl.md)).
  - `LightProbe`, `LightProbeGrid`, and `LightProbeVolume` store and interpolate `IrradianceSH` (with `Add`, `Scale`, `Lerp`, `ProjectDirectionIrradianceSH`, and `ComputeIrradianceSHFromFunc` extending `IrradianceSH` directly).
- **Unified `LitPBRWGSL` integration at `IBLGroup` (group 3)**:
  - `ProbeIBLWGSL` (and `ProbeIBLWGSLForGroup`) binds a 256-byte `ProbeIBLUniform` (binding 0), two local prefiltered reflection cubemaps (bindings 1–2), the global sky cubemap (binding 3), and one trilinear sampler (binding 4). It exposes `box_project_dir`, `evaluate_probe_irradiance_sh`, `sample_probe_specular`, and `evaluate_probe_ibl` without using `exp`, `exp2`, `log`, or `log2` (matching NSGo v0.3.0's software GPU VM constraint).
  - `LitPBRProbeIBLWGSL` / `NewLitPBRProbeIBLPipeline` (and `NewLitPBRProbeIBLPipelineWithOptions` with `LinearOutput` for `HDRTarget`) composes `litPBRPreludeWGSL` + `ProbeIBLWGSL` + `litPBRFragmentHeadWGSL` + `evaluate_probe_ibl` + `linear_to_srgb`, inheriting `PBRDrawUniform`'s inverse-transpose normal matrix, `LightRig` direct lights, emissive term, and IEC 61966-2-1 sRGB output automatically.
  - `EvaluateProbeIBL` reproduces `evaluate_probe_ibl` on the CPU so `LinearToSRGB(ShadePoint + EvaluateProbeIBL)` matches `LitPBRProbeIBLWGSL` pixel-for-pixel.
- **Per-object `GPUProbeIBLBuffer` with black fallback parity**:
  - `GPUProbeIBLBuffer` owns a 1×1 linear black `(0, 0, 0, 255)` fallback `GPUCubemap` (never white). Any nil, unbaked, dirty, or disabled probe slot or nil sky binds that black cubemap and receives zero weight in `NewProbeIBLUniform`, guaranteeing exact CPU/GPU black parity.
  - `ReflectionProbeSet.SelectReflectionProbes` skips `nil`, `Disabled`, `!Baked`, and `Dirty` probes so objects fall back smoothly to ready probes or the global sky.
  - `GPUProbeIBLBuffer.Update` reuses its existing `gpu.BindGroup` whenever the three bound cubemap textures have not changed, and closes the previous bind group immediately when switching probes.
- **Asynchronous GPU probe baking and leak-free re-bakes**:
  - `BakeReflectionProbeGPU` allocates six temporary offscreen render targets (`FormatRGBA8Unorm` when `opts.SRGB` is false or `FormatRGBA8UnormSRGB` when `opts.SRGB` is true, paired with `FormatDepth24Plus`), submits one render pass per cubemap face using `ProbeFaceCamera`, queues `dev.ReadTexture` for all six faces without blocking or assuming `WaitIdle` fills readback bytes synchronously, closes all six render targets when the last callback fires on the engine goroutine, flips rows to canonical cubemap orientation, prefilters via `PrefilterEnvironment`, uploads via `UploadCubemap`, closes any previous `probe.GPUCubemap`, and invokes `onDone(err)`.
  - Both `BakeReflectionProbe` and `BakeReflectionProbeGPU` close the previous `probe.GPUCubemap` only after the replacement `UploadCubemap` succeeds, preventing both GPU texture leaks and partial-failure corruption.
- **Runtime-only bakes**:
  - Reflection probe cubemaps and light probe SH bakes are runtime-only data generated on load or on request rather than persisted in text scene files (consistent with the repository's text-only authoring format rule).

### Invariants

- Every validation error from probes, grids, or `ProbeIBLUniform` wraps `ErrInvalidProbe`.
- `ReflectionProbe.Influence` and `BoxProjectDirection` never return `NaN` for non-finite coordinates, inverted boxes, or degenerate directions.
- `LightProbeGrid.At`, `Set`, and `Sample` validate `Counts` (including overflow) against `len(Probes)` and never panic on mismatched struct literals.
- `ProbeIBLUniform.Encode` writes 256 bytes with zero heap allocations and panics if `len(dst) < ProbeIBLUniformSize`.
- Re-baking a `ReflectionProbe` always closes its previous `GPUCubemap` when replacement succeeds.

## Alternatives considered

### Separate `ProbeCubemap` / `SH9` types and standalone `ProbeRenderer`

Having probes own their own cubemap and SH structs and a separate unlit-direct-light shader keeps `probes.go` completely independent of `skybox.go` and `light.go`. Rejected because it duplicates cubemap upload, face direction math, and L2 SH evaluation with subtle convention drift (such as cosine-lobe normalization and missing normal matrix / sRGB encode), and prevents objects shaded by probes from receiving `LightRig` direct lighting in the same pass.

### Synchronous `BakeReflectionProbeGPU` waiting on `dev.WaitIdle()`

Returning `error` synchronously from `BakeReflectionProbeGPU` after `dev.WaitIdle()` makes call sites simpler in headless tests. Rejected because `gpu.Device.ReadTexture` is asynchronous inside a live `gpu.View` and completes on the engine goroutine on a later pump rather than during `WaitIdle()`, so a synchronous API fails on live hardware/view backends.

### Using a 1×1 white fallback cubemap for missing probe or sky slots

Binding a white fallback texture matches albedo fallback conventions in `MaterialDefaults`. Rejected because an unbaked probe or missing sky multiplied by a positive weight would inject bright white reflections on the GPU while CPU evaluation returns black; a 1×1 black cubemap paired with zero weights for non-ready probes guarantees exact CPU/GPU parity.

## Consequences

- **Benefit:** Local reflection probes (with parallax box projection and smooth two-probe + sky blending) and interpolated light-probe diffuse irradiance work in the same `LitPBRWGSL` pass alongside `LightRig` direct lighting, emissive, normal matrices, and sRGB output across all backends including WebGL 2 / GLES 3.0 (`NoGPUCompute`).
- **Benefit:** Zero duplicate cubemap or SH types — any `Cubemap`, `PrefilterEnvironment`, `GPUCubemap`, or `IrradianceSH` produced by `skybox.go` interops directly with probes.
- **Cost:** Binding probes at `IBLGroup` (group 3) shares that slot with fog, shadow maps, and skinning on `LitPBRWGSL` pipelines (the 4-group portability ceiling across all backends); custom shaders needing multiple group-3 features can compose `ProbeIBLWGSLForGroup` with `LightWGSLForGroup`.
- **Cost:** `BakeReflectionProbeGPU` temporarily allocates six face render targets for the duration of the async readback and performs GGX prefiltering on the CPU when `SkipPrefilter` is false; scenes with many large probes should bake lazily on load or spread dirty re-bakes across frames.

## Testing

- `packages/game/render3d/probes_test.go` verifies `BoxProjectDirection` geometry and non-finite safety, `ReflectionProbe.Influence` NaN/Inf safety and smoothstep selection skipping unbaked/dirty/disabled probes, `LightProbeGrid`/`LightProbeVolume` mismatched-struct-literal panic safety and trilinear/scattered interpolation, CPU==GPU exact black parity for unbaked/dirty probes and nil skies, leak-free `GPUCubemap` closure on CPU and GPU re-bakes, end-to-end `LitPBRProbeIBLPipeline` GPU pixel parity against `LinearToSRGB(ShadePoint + EvaluateProbeIBL)` for probe blending, parallax box projection, light-probe grid transitions, direct lights, and non-uniform scale normal matrices, live `gpu.View` async `BakeReflectionProbeGPU` (both `RGBA8Unorm` and `RGBA8UnormSRGB` targets with real 3D geometry via `ProbeFaceCamera`) on both `full` and `nsgotest.NoGPUCompute` profiles, and zero-allocation uniform encoding and resource lifecycles.
- Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./render3d`, root conformance/stability tests, `go run ./tools/changelog check`, and `bun run verify-notes`.
