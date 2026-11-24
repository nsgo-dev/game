# Agent Note: 3D skybox and image-based lighting (IBL)

Status: implemented

## Problem

3D PBR materials in `game/render3d` need an infinitely distant environment skybox and physically based image-based lighting (diffuse irradiance and roughness-aware specular reflections) that work across every NSGo GPU backend — Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0 (`NoGPUCompute`), and NSGo's headless software GPU renderer — without requiring compute shaders, storage textures, or binary asset conversion. In addition, the WGSL shaders must match a deterministic CPU reference evaluator and survive GPU device loss when resources opt into `Retained`.

## Decision

`packages/game/render3d/skybox.go` adds a self-contained skybox and split-sum IBL subsystem built on standard `DimCube` RGBA8 textures and fragment-stage WGSL helpers:

- **CPU cubemaps (`Cubemap`)**: canonical 6-face square RGBA cubemaps (`CubeFacePosX`..`CubeFaceNegZ`) with optional precomputed mip chains (`Mips`) and explicit `SRGB` decoding. Constructors cover six-face images (`CubemapFromImages`), latitude-longitude panoramas with seamless horizontal wrapping (`CubemapFromEquirectangular`), and procedural three-band skies (`GradientSkyCubemap`).
- **GPU cubemaps (`GPUCubemap`, `UploadCubemap`)**: validates face dimensions and mip pyramids, normalizes sub-images whose `Bounds().Min != (0, 0)`, uploads all faces and mips with a trilinear clamp-to-edge sampler, and supports optional CPU 2x2 mip generation (`GenerateMips`) and device-loss recovery (`Retained`).
- **Fullscreen-triangle skybox (`SkyboxPipeline`, `SkyboxWGSL`)**: draws a single vertex-buffer-free triangle from `@builtin(vertex_index)` using the inverse of `Proj * ViewRotation` (translation stripped by `NewSkyboxUniform`). The vertex shader outputs `z = 0.999999` (while still unprojecting `ndc` at `z = 1.0`) paired with `CompareLessEqual` and `DepthWrite: false`, so the skybox draws behind all scene geometry without float32 barycentric depth interpolation overshooting `ClearDepth = 1.0`.
- **Diffuse irradiance (`IrradianceSH`, `ComputeIrradianceSH`)**: integrates the base cubemap using exact analytical per-texel solid angles (summing to `4*pi` across all six faces) into 9 pre-convolved L2 real spherical harmonic RGB coefficients (Ramamoorthi & Hanrahan 2001). `Evaluate` and `ToCubemap` mirror `IBLWGSL`'s `evaluate_irradiance_sh`.
- **Specular prefiltering & BRDF (`PrefilterEnvironment`, `GenerateBRDFLUT`, `EnvBRDFApprox`)**: builds a deterministic Hammersley GGX-prefiltered specular mip chain using solid-angle filtered source pyramid lookups to suppress fireflies. `IBLWGSL` combines L2 SH diffuse irradiance with `textureSampleLevel(ibl_prefilter, ibl_sampler, r, rough * ibl.max_lod)` and Karis (2014) `env_brdf_approx` using `pow(2.0, -9.28 * n_dot_v)` instead of `exp2(...)`. `EvaluateIBL` provides the exact CPU reference for `evaluate_ibl`.
- **IBL uniform & bind group (`IBLBuffer`, `IBLUniform`, `IBLGroup = 2`)**: binds the 160-byte SH + intensity + `max_lod` uniform buffer (binding 0), prefiltered cubemap (binding 1), and sampler (binding 2) at `@group(2)` by default (or any group via `IBLWGSLForGroup`).

### Invariants

- All cubemap validation errors wrap `ErrInvalidCubemap` and allocate zero GPU resources on failure.
- `SkyboxUniform.Encode` (80 bytes) and `IBLUniform.Encode` (160 bytes) never allocate and panic when `len(dst)` is too short.
- `IBLWGSL` and `SkyboxWGSL` never use compute stages, storage buffers, `exp`, or `exp2` (which NSGo v0.3.0's software GPU VM omits from its builtin dispatch, silently returning zero); `pow(2.0, x)` is used instead so software and hardware backends evaluate identically.
- Skybox depth is emitted at `0.999999` (never `1.0`) so barycentric rounding on a fullscreen triangle cannot fail `CompareLessEqual` against `ClearDepth = 1.0`.

## Alternatives considered

### Unit cube vertex buffer vs fullscreen triangle for `SkyboxPipeline`

Drawing an inverted unit cube with 8 vertices and 36 indices is traditional and easy to visualize. However, it requires allocating and retaining a vertex/index buffer on the device, requires careful winding/culling setup, and still needs a per-pixel direction normalization. A single vertex-buffer-free fullscreen triangle from `@builtin(vertex_index)` allocates zero geometry buffers, renders in one 3-vertex draw call, and reconstructs exact world-space rays via `inv(Proj * ViewRotation)`.

### Emitting `z = 1.0` vs `z = 0.999999` in `SkyboxWGSL`

Emitting `z = 1.0` (`pos.xyww`) is the textbook GLSL/WGSL trick for skyboxes with `CompareLessEqual`. On float32 depth buffers (such as NSGo's software rasterizer), barycentric interpolation `w0*1 + w1*1 + w2*1` across a giant `[-1, 3]` fullscreen triangle rounds to `1.0000001` at specific pixel coordinates and fails `CompareLessEqual` against `ClearDepth = 1.0`, leaving regular clear-color dot artifacts in the background. Emitting `z = 0.999999` stays strictly behind all scene geometry up to `0.999999` clip depth while eliminating depth-rounding dropouts everywhere.

### Requiring a 2D BRDF LUT texture binding in `IBLBuffer` vs analytical `env_brdf_approx`

Binding a precomputed 2D RG16F/RGBA8 BRDF LUT texture alongside the prefiltered cubemap gives slightly higher accuracy at extreme grazing metallic angles. However, it requires an extra texture + sampler binding slot per pipeline (tight on WebGL 2 / GLES 3 fragment sampler limits) and an additional GPU upload during startup. Using Karis's analytical `env_brdf_approx` inside `IBLWGSL` keeps `IBLBuffer` to three bindings (1 uniform, 1 cubemap, 1 sampler) with zero extra texture fetches while still shipping `GenerateBRDFLUT` / `GenerateBRDFLUTWithSamples` for custom PBR shaders that opt into a 2D LUT.

### GPU compute shader prefiltering vs CPU Hammersley prefiltering

Computing irradiance and GGX prefiltered mips on the GPU via compute shaders is faster for dynamic runtime environment probes. However, WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`) do not support compute shaders at all, and static/startup environment baking at 32–128 px face sizes finishes in milliseconds on CPU with bit-for-bit determinism across platforms.

## Consequences

- **Benefit:** Skybox and PBR IBL (`IrradianceSH` + GGX prefiltered cubemap + `IBLWGSL`) work identically on all NSGo targets including WebGL 2, OpenGL ES 3.0, and the headless software GPU renderer, with full `Retained` device-loss recovery.
- **Benefit:** CPU `EvaluateIBL` matches `IBLWGSL` pixel-for-pixel in headless tests, enabling deterministic golden-image regression testing without hardware GPU flakiness.
- **Cost:** CPU prefiltering is intended for startup or asset-bake time rather than per-frame dynamic reflection probes; dynamic real-time probes should add a compute-capable fast path gated behind `gpu.Features` when needed.
- **Cost:** `IBLWGSL`'s default `env_brdf_approx` trades minor grazing-angle accuracy on rough metals (~0.15 at extreme angles) for fewer texture bindings; shaders needing exact Monte Carlo BRDF integration can bind `GenerateBRDFLUT`.

## Testing

- `packages/game/render3d/skybox_test.go` verifies cubemap face round-trips, validation errors, equirectangular projection, gradient sky generation, analytical L2 SH uniform/hemisphere/sRGB integration, deterministic GGX prefiltering variance reduction, BRDF LUT & `EnvBRDFApprox` agreement, uniform byte layout and zero-allocation `Encode`, cardinal face alignment & translation invariance, skybox-behind-scene rendering (`testdata/golden/skybox_scene.png`), 2x2 metallic/roughness reflective spheres matching CPU `EvaluateIBL` (`testdata/golden/ibl_spheres.png`), `gpu.View` execution on both `full` and `nsgotest.NoGPUCompute` profiles, retained device-loss recovery, and resource lifecycles.
- Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./render3d`, `go tool nsgo test -run TestStabilityTable .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
