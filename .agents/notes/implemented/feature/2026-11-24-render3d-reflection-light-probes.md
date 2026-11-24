# Agent Note: Reflection Probes and L2 Spherical-Harmonic Light Probes in render3d

Status: implemented

## Problem

Global environment lighting (`#253`) gives a scene one uniform sky, and lightmaps (`#258`) cover static geometry, but 3D scenes with distinct rooms and moving actors need two additional local lighting mechanisms (`#384`, part of `#251`):

1. **Localized reflections with parallax**: objects inside an interior room must reflect local walls rather than the distant sky, shift reflections accurately as they move off the probe center (box projection), and blend smoothly across overlapping probe volumes and back into the sky environment outside them.
2. **Baked indirect diffuse lighting for dynamic objects**: moving characters and props cannot sample static UV lightmaps and need compact, directional indirect lighting interpolated smoothly across the scene.
3. **Cross-backend GPU portability**: NSGo v0.3.0's software GPU VM omits `exp`, `exp2`, and `log2`, native backends do not support `Device.GenerateMipmaps`, WebGL 2 / OpenGL ES 3.0 lack compute shaders, and WebGPU/Metal/D3D11 cubemap faces follow a left-handed inside-cube convention whose Y origin is top-down while `math3d` cameras are right-handed Y-up.

## Decision

`packages/game/render3d/probes.go` implements reflection probes, L2 spherical-harmonic (`SH9`) light probes, on-request cubemap baking, and a portable GPU probe renderer in one self-contained file (avoiding symbol or file overlap with parallel `render3d` work):

- **Cubemap handle with CPU mipmaps and linear mirror (`ProbeCubemap`)**: builds on the same CPU area-weighted box mipmap strategy recorded in [3D GPU Textures and Samplers in render3d](../../implemented/feature/2026-11-24-render3d-textures-and-samplers.md), uploading all 6 faces (`gpu.DimCube`) and mip levels via `Device.WriteTextureMip` so mipmapped roughness sampling and `Retained` device-loss restoration work across every NSGo backend. A CPU linear RGB mirror (`[6][]float32`) enables deterministic `SampleCPU` and direct `ProbeCubemap.BakeSH9()`.
- **Parallax box projection (`BoxProjectDirection` and WGSL `box_project_dir`)**: computes world-space ray-AABB intersection using safe per-axis `slab_tmax` (returning `1e30` when `|dir_i| < 1e-6`, avoiding false zero-distance hits when a point sits on a box face and reflects parallel to it) and falls back to the unprojected reflection ray when outside the box pointing away.
- **Continuous top-2 reflection probe blending (`ReflectionProbeSet.SelectReflectionProbes`)**: ranks active probes by `(Priority desc, smoothstep Influence desc, BoxVolume asc, index asc)`. When `p0.Priority > p1.Priority` (nested rooms), `w0 = inf0`, `w1 = (1 - w0) * inf1`, `sky = 1 - w0 - w1`; when priorities tie (adjacent rooms), weights stay raw while `inf0 + inf1 <= 1` and normalize by `inf0 + inf1` above 1 (`sky = 0`), guaranteeing C0 continuity and `Weight0 + Weight1 + SkyWeight == 1` everywhere.
- **On-request baking with exact cubemap orientation (`BakeReflectionProbe`, `BakeReflectionProbeGPU`)**: CPU baking evaluates `CubeFaceDirection(face, u, v)` directly matching NSGo's `cubeDirToFaceUV`. GPU baking renders each face into a temporary offscreen `RenderTarget` using right-handed `ProbeFaceCamera` matrices (preserving CCW winding for backface culling), reads back the face via `Device.ReadTexture`, and flips rows vertically before uploading so WebGPU top-down render targets align identically with `texture_cube` sampling.
- **Exact solid-angle L2 SH9 baking and volume interpolation (`SH9`, `LightProbeGrid`, `LightProbeVolume`)**: `BakeSH9FromCubemap` and `BakeSH9FromFunc` weight each texel by Ignacio Castaño's analytical cube-face solid angle (summing to exact `4π` at any resolution), and `EvaluateSH9` applies normalized cosine-lobe weights `(1, 2/3, 1/4)` so a constant environment `C` evaluates to `C`. `LightProbeGrid` performs O(1) clamped trilinear interpolation; `LightProbeVolume` layers scattered finite-radius or unbounded inverse-distance-weighted `LightProbe` points over an optional grid or `FallbackSH`.
- **Portable GPU shading (`ProbeWGSL`, `ProbeRenderer`, `ProbeBinding`)**: reserves `CameraGroup = 0` (`CameraBuffer`) and `ProbeGroup = 1` (`ProbeData` uniform + `probe0_cube`, `probe1_cube`, `sky_cube`, `probe_samp`). `ProbeData` packs `SH9` as 9 explicit `vec4<f32>` fields (`sh0`..`sh8`) rather than a uniform array to avoid cross-backend std140 array stride discrepancies, uses pure algebraic Schlick Fresnel (no `exp`/`exp2`/`log2`), and allocates per-object `ProbeBinding` instances so multiple objects with distinct probe pairs and SH coefficients draw in a single render pass.

## Alternatives considered

### Compute-shader GGX specular prefiltering and SH9 reduction on the GPU

Computing prefiltered roughness mips and SH9 reductions via WGSL compute shaders would keep cubemap baking entirely on the GPU without readback. It was rejected because WebGL 2 and OpenGL ES 3.0 targets (`nsgotest.NoGPUCompute`) do not support compute shaders at all, and NSGo v0.3.0 native backends do not implement GPU mipmap generation. Performing CPU box-mip generation and analytical solid-angle SH9 integration works identically across all six NSGo backends and makes CPU and GPU evaluation bit-consistent in headless tests.

### Rendering cubemap faces directly into `DimCube` array layers with a Y-flipped projection

Setting `ColorAttachment{Texture: cube, ArrayLayer: face}` with a Y-negated projection matrix would avoid temporary 2D render target readback and vertical row flips. It was rejected because negating Y in the projection matrix reverses matrix determinant and triangle winding (breaking `CullBack` pipelines in scene render callbacks), and still would not populate CPU mipmap chains or linear CPU face data for `BakeSH9()` and `Retained` device-loss restoration.

### Storing `SH9` as `array<vec4<f32>, 9>` in `ProbeData`

Using a fixed-size WGSL array in the uniform struct would allow loop indexing in `eval_sh9_diffuse`. It was rejected because uniform array indexing and stride translation are common failure points across WebGL 2 / GLSL / MSL backends, whereas 9 explicit `vec4<f32>` fields (`sh0`..`sh8`) unroll cleanly into 9 vector multiply-adds with identical byte layout across every backend and NSGo's software VM.

### Tetrahedral (Delaunay) interpolation for scattered `LightProbe` clouds

Building a 3D Delaunay tetrahedralization for arbitrary scattered light probes gives piecewise-linear barycentric interpolation inside the convex hull. It was rejected for the initial implementation because 3D Delaunay construction and degeneracy handling add substantial geometric complexity, while most scenes use regular `LightProbeGrid` volumes (O(1) trilinear) optionally augmented by localized scattered probes where smooth radial falloff + inverse-distance weighting is simple, branch-free, and handles arbitrary point counts (including 1–3 probes).

## Consequences

- **Benefits**:
  - Dynamic 3D objects seamlessly blend up to two local parallax-corrected reflection probes over the global sky cubemap and pick up smoothly interpolated baked L2 SH9 diffuse lighting.
  - Both CPU and on-request GPU cubemap baking work deterministically across full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles without requiring compute shaders or `exp`/`exp2`/`log2`.
  - All new symbols and helpers live solely in `packages/game/render3d/probes.go` and `packages/game/render3d/probes_test.go`, keeping zero merge conflicts with parallel `render3d` work.
- **Costs**:
  - `BakeReflectionProbeGPU` performs synchronous per-face readbacks through `Device.ReadTexture` and CPU mipmap generation, so it is intended for load-time or explicit on-request bakes rather than per-frame dynamic cubemap updates.
  - Roughness mipmaps use area-weighted box filtering rather than importance-sampled GGX split-sum convolution; high-roughness specular highlights are softer approximations until a dedicated IBL prefilter pass is added.
  - `ProbeRenderer` uses `CameraGroup = 0` and `ProbeGroup = 1`; callers integrating probes into custom PBR material shaders (`#113`) can reuse `ProbeUniform`, `ProbeBlendResult`, `BoxProjectDirection`, `SH9`, and `ProbeWGSL`'s helper functions at their own bind group index.
