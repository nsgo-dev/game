# Agent Note: 3D Metallic-Roughness PBR Materials in render3d

Status: implemented

## Problem

`render3d` already provides scope-owned GPU meshes (`GPUMesh`), textures and samplers (`GPUTexture`, `GPUSampler` — see [3D GPU Textures and Samplers in render3d](./2026-11-24-render3d-textures-and-samplers.md)), and camera uniforms (`CameraBuffer` at `CameraGroup = 0`), but rendering physically based glTF 2.0 surfaces (`#113`) requires a unified material abstraction that combines base color, metallic-roughness, tangent-space normal, occlusion, and emissive maps with alpha modes, double-sided shading, and device-loss recovery without multiplying shader permutations or breaking standard 32-byte `MeshVertexLayout` meshes.

Several cross-backend constraints shape the implementation on NSGo v0.3.0:
- `MeshVertexLayout` carries position, normal, and UV (32 bytes) with no vertex tangent attribute, and screen-space Y polarity differs between OpenGL/WebGL and Metal/D3D11/WebGPU.
- NSGo's software GPU VM (`GPUSoftwareDevice`) does not evaluate `dpdx`/`dpdy` (returning zero) or `exp`/`exp2`, so naive cotangent-frame normal mapping collapses to zero vectors in headless `nsgotest` renders unless a deterministic fallback basis exists.
- WebGPU's uniformity analysis requires implicit-derivative `textureSample`, `dpdx`, and `dpdy` calls to execute in uniform control flow before any non-uniform `discard`.
- 8-bit UNORM flat-normal fallback texels `(128, 128, 255, 255)` decode to `(+0.0039, +0.0039, 1.0)` rather than exact `(0, 0, 1)`, and binding a black 1×1 fallback emissive texture would zero out scalar `EmissiveFactor` when `EmissiveTexture` is omitted unless texture presence is tracked in uniforms.

## Decision

`packages/game/render3d/material.go` implements glTF 2.0 metallic-roughness materials around one uniform `MaterialGroup = 1` layout, a 64-byte std140 uniform block, portable Cook-Torrance GGX shading, and an exact CPU reference evaluator:

- **One bind group layout via shared `MaterialDefaults`**: `MaterialDefaults` allocates one `MaterialBindGroupLayout` (binding 0 = 64-byte `MaterialUniforms`, bindings 1..10 = five `(texture_2d, sampler)` pairs for base color, metallic-roughness, normal, occlusion, and emissive), a default linear-repeat `GPUSampler`, and four 1×1 fallback textures (`WhiteSRGB`, `WhiteLinear`, `FlatNormal (128,128,255,255)`, `Black`). Every `GPUMaterial` fills omitted slots from `MaterialDefaults`, keeping zero shader/layout permutations across textured and untextured materials while staying well within WebGL 2 / GLES 3.0 texture-unit limits (5 textures, 5 samplers).
- **16-byte-aligned `MaterialUniforms` with texture-presence flags**: `MaterialUniforms` packs four `vec4<f32>` vectors (`MaterialUniformSize = 64`), encoding `AlphaMode`, `DoubleSided`, `HasNormalMap`, and `HasEmissiveMap` in `flags`. Keeping `HasNormalMap` and `HasEmissiveMap` in uniforms avoids 8-bit quantization tilt on untextured normals and lets `EmissiveFactor` emit light cleanly even while binding the 1×1 black fallback texture when `EmissiveTexture == nil`. `AppendUniformData` and `Encode` write into caller-owned buffers with zero allocations.
- **No cached `*gpu.Device` on `GPUMaterial`**: Matching `GPUTexture`, `GPUMaterial.Update(dev, m)` accepts `dev *gpu.Device` explicitly so retained materials remain updatable after `TriggerGPUDeviceLost` + `RestoreGPUDevice`. `Update` validates that texture/sampler handles have not changed (requiring a new `GPUMaterial` to rebind textures) and updates the uniform buffer in place via an internal 64-byte scratch array.
- **API-independent Cramer cotangent frame with deterministic fallback**: `MaterialWGSL` samples all five maps and evaluates `dpdx`/`dpdy` unconditionally at the top of `fs_main` (before `AlphaMask` `discard`) for WebGPU uniformity compliance. `material_perturb_normal` solves `T = dP/du` and `B = -dP/dv` via Cramer's rule on `(dpdx, dpdy)`, which cancels screen-Y handedness across Metal, D3D11, OpenGL, WebGPU, WebGL 2, and GLES 3.0; when `det` is zero (on `GPUSoftwareDevice` or meshes without UVs), it falls back to a deterministic orthonormal `(T, B)` frame from the geometric normal that matches glTF's `+U / -V` orientation on standard quads, cube faces, ground planes, and UV spheres.
- **Software-VM-safe Cook-Torrance GGX + CPU `EvaluateMaterial`**: `MaterialBindingsWGSL` implements Trowbridge-Reitz GGX `D`, height-correlated Smith-GGX `Vis`, 3-multiply Schlick Fresnel `pow5`, energy-conserving Lambertian diffuse, analytic hemisphere diffuse/specular ambient scaled by `OcclusionStrength`, and Reinhard + `pow(..., 1/2.2)` sRGB tone mapping without `exp`/`exp2`. `EvaluateMaterial` mirrors the exact same math in pure Go so tests verify GPU renders directly against CPU reference outputs and golden PNGs (`pbr_metallic_roughness_ramp.png`, `pbr_textured_normal_maps.png`).

## Alternatives considered

### Separate shader/bind-group permutations per active texture combination

Compiling specialized bind group layouts and pipelines for each subset of present maps (base color only, metallic-roughness only, normal-mapped, etc.) saves sampling unused 1×1 fallback textures on simple materials. It was rejected because five optional maps create up to 32 layout/pipeline permutations, complicate draw batching and sorting in `render3d.Queue`, and add shader compilation latency on WebGL 2 / mobile backends; sharing one `MaterialGroup = 1` layout with shared 1×1 fallback textures from `MaterialDefaults` eliminates permutations while costing 16 bytes of shared texture memory per device.

### Extend `MeshVertexLayout` with a per-vertex `vec4<f32>` tangent attribute

Adding a 16-byte tangent attribute (expanding `MeshVertexStride` from 32 to 48 bytes) gives exact MikkTSpace tangents on every vertex without relying on screen-space derivatives. It was rejected because `MeshVertexLayout` (`#112`) is already an established 32-byte contract shared across `render3d` pipelines and tests, and computing tangents in the fragment shader via Cramer's rule with an orthonormal fallback gives normal mapping out of the box on every existing `GPUMesh` without changing `mesh.go`.

### Use Christian Schüler's `cotangent_frame` formula directly

Schüler's classic `cross(dp2, N) * duv1.x + cross(N, dp1) * duv2.x` avoids an explicit determinant division, but assumes `cross(dp1, dp2) · N > 0`, which flips tangent handedness between OpenGL (Y-up screen coordinates) and Metal/D3D11/WebGPU (Y-down screen coordinates). Solving the 2×2 Jacobian directly via Cramer's rule (`det = duv1.x*duv2.y - duv1.y*duv2.x`) cancels the screen-Y sign flip automatically across every NSGo backend.

## Consequences

- **Benefits**: Games can render full glTF 2.0 metallic-roughness surfaces (base color, packed metallic-roughness, normal, occlusion, emissive, `AlphaOpaque`/`AlphaMask`/`AlphaBlend`, `DoubleSided`) on any `GPUMesh` with one bind group layout, zero-allocation uniform encoding, in-place dynamic factor updates, `Retained` device-loss recovery, and exact pure-Go CPU reference evaluation verified on both full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles.
- **Costs**: Every fragment in `MaterialWGSL` samples all five texture slots (four 1×1 cache-hot fallback texels when maps are omitted) to preserve uniform control flow and a single bind group layout; `MaterialWGSL` embeds one built-in directional key light + hemisphere environment (custom multi-light scenes should compose `CameraWGSL` + `MaterialBindingsWGSL` with their own lighting bind group); and changing bound texture/sampler handles requires creating a new `GPUMaterial` rather than mutating an existing bind group via `Update`.
