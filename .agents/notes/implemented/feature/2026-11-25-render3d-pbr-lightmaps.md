# Agent Note: Baked lightmaps in the PBR material path

Status: implemented

## Problem

[Baked lightmaps across game/gltf and game/render3d](2026-11-24-render3d-baked-lightmaps.md) introduced `LightmapVertexLayout` (40 bytes with `UV0` and `UV1`), `LightmapEncoding` (Linear/RGBA16F, sRGB, RGBM, RGBD), and `LightmapRenderer`. However, `LightmapRenderer` is a standalone Lambertian path (albedo times baked irradiance plus one optional `DirectionalLight`) that does not use `GPUMaterial` (metallic-roughness, normal, occlusion, emissive, alpha modes) or `LightRig` (multiple directional, point, and spot lights with Cook-Torrance GGX). Baked glTF levels rendered with `LightmapRenderer` therefore lost their PBR surface maps and dynamic `LightRig` highlights.

## Decision

Baked lightmaps integrate into the lit PBR material path in dedicated files (`packages/game/render3d/pbr_lightmap.go` and `packages/game/render3d/pbr_lightmap_test.go`) within the four-bind-group limit guaranteed across WebGPU, WebGL 2, and OpenGL ES 3.0:

- **Four-group pipeline layout (`PBRLightmapWGSL` / `NewPBRLightmapPipeline`)**:
  - Group 0 (`CameraGroup`): `CameraBuffer` (`CameraWGSL`).
  - Group 1 (`LightGroup`): `GPULightBuffer` (`LightWGSL`), so `lightBuf.Bind(pass)` works unchanged and supplies all directional, point, and spot lights.
  - Group 2 (`PBRLightmapMaterialGroup`, matching `PBRDrawGroup` = 2): `GPUMaterial` via `MaterialBindingsWGSLForGroup(2)` — directly reuses `MaterialDefaults.Layout()` and any `*GPUMaterial` created by `NewGPUMaterial` or `UploadGLTFMaterials` with zero extra texture copies or material wrappers.
  - Group 3 (`PBRLightmapGroup`, parallel to `FogGroup` and `IBLGroup` = 3): `GPUPBRLightmap`, holding a 176-byte uniform buffer (`model`, derived `normal_matrix`, atlas `uv_transform`, `encoding` / `intensity` / `rgbm_max_range` / `has_lightmap`, and `ambient_fallback`) plus the 2D lightmap texture and sampler.
- **Vertex layout**: reuses the 40-byte `LightmapVertexLayout` (`Position`, `Normal`, `UV0`, `UV1`) and `GPULightmapMesh`, keeping 32-byte `MeshVertexLayout` untouched for unlightmapped geometry.
- **Shading equation and exact parity**:
  - Fragment shading samples all five `GPUMaterial` textures at `UV0`, samples the lightmap at `UV1 * uvScale + uvOffset`, and computes `dpdx`/`dpdy` in uniform control flow before any `AlphaMask` discard.
  - Decoded lightmap irradiance uses the exact same linear/sRGB/RGBM/RGBD math as `LightmapWGSL` (avoiding `exp`, `exp2`, `log`, and `log2` for NSGo v0.3.0's software GPU VM, nsgo-dev/nsgo#2334), skipping shader sRGB decode when `Lightmap.SRGB()` is true.
  - When a lightmap is bound, it **replaces** `LightRig.Ambient` as the indirect diffuse term: `(1 - metallic) * baseColor * decodedLightmap * intensity * occlusion`. When no lightmap is bound, indirect lighting falls back to `AmbientFallback` (if set) or `LightRig.Ambient`.
  - Dynamic directional, point, and spot lights evaluate through `shade_pbr_direct` calling `LightWGSL`'s `pbr_brdf`, `point_light_attenuation`, and `spot_cone_attenuation` directly (no `lights.ambient` addition/subtraction cancellation error), plus emissive.
  - Output encodes with `linear_to_srgb` (matching both `LightmapWGSL` and `LitPBRWGSL`, rather than `MaterialWGSL`'s Reinhard tone map), switching automatically to unclamped linear output (`PBRLightmapLinearWGSL`) when `LinearOutput` is set or `ColorFormat.IsSRGB()` is true.
  - `EvaluatePBRLightmap` and `DecodeLightmapSample` provide a pure-Go CPU reference matching the shader.
- **glTF loader bridge**: `UploadGLTFPBRLightmaps` produces a `GLTFPBRLightmapSet` alongside `GLTFMaterialSet`, reading `Document.MaterialLightmap` (`MOZ_lightmap`, `EXT_lightmap`, `extras.lightmap` with `KHR_texture_transform`, `scaleOffset`, `uvScale`, `uvOffset`, `intensity`, `encoding`, `rgbmMaxRange`), deduplicating lightmap textures/samplers, skipping CPU box mipmaps on RGBM/RGBD textures (where linear averaging corrupts shared-exponent HDR channels), and offering `NewInstance` for multiple scene nodes sharing one material at different world transforms. `LightmapMeshFromGLTF` builds the dual-UV `LightmapMesh` directly from a glTF primitive.

## Alternatives considered

### Expanding `MaterialBindGroupLayout` (`GPUMaterial`) with lightmap bindings

Adding the lightmap uniform, texture, and sampler directly to `MaterialGroup` (group 1) would keep a single bind group per material. It was rejected because it would expand every unlightmapped `GPUMaterial` uniform buffer and bind group layout, conflict with `MaterialWGSL` / `MaterialFogWGSL` / `MaterialSkinnedWGSL` pipelines, and prevent sharing one `GPUMaterial` across multiple instances with distinct atlas UV transforms or model matrices.

### Using `MaterialWGSL`'s hardcoded `DefaultMaterialLight` and Reinhard tone mapping

Building a UV1 variant of `MaterialWGSL` instead of combining `GPUMaterial` with `LightWGSL` would reuse `MaterialGroup` at group 1 directly. It was rejected because `MaterialWGSL` has no `LightRig` support, no per-object model matrix, and uses Reinhard tone mapping + `pow(1/2.2)` instead of `linear_to_srgb`, which would break exact parity with both `LightmapRenderer` and `LitPBRWGSL`.

### Adding a fifth bind group for per-draw `PBRDrawUniform` alongside `GPUMaterial` and `GPUPBRLightmap`

Keeping `PBRDrawBuffer` at group 2, `GPUMaterial` at group 3, and `GPUPBRLightmap` at group 4 would separate transforms from lightmaps. It was rejected because WebGPU minimum limits and WebGL 2 / OpenGL ES 3.0 guarantee only four bind groups (0..3). Placing `model` and `normal_matrix` inside `GPUPBRLightmap` at group 3 keeps the full camera + lights + textured PBR material + lightmap pipeline within four groups.

## Consequences

- **Benefits**:
  - Baked glTF scenes with `MOZ_lightmap`, `EXT_lightmap`, or `extras.lightmap` render with full metallic-roughness, normal, occlusion, and emissive maps plus dynamic `LightRig` directional/point/spot lights in a single portable pipeline across desktop, mobile, WebGPU, WebGL 2 (`NoGPUCompute`), and NSGo's software renderer.
  - Zero edits to `lightmap.go`, `material.go`, `light.go`, or `gltf_material.go`; `GPUMaterial` instances from `UploadGLTFMaterials` bind directly at `PBRLightmapMaterialGroup` (2).
  - Exact pixel parity with `LightmapRenderer` (dielectric, rough, untextured, no dynamic lights) and with `LitPBRWGSL`'s direct term when adding `LightRig` lights.
- **Costs and limitations**:
  - Because group 3 holds `GPUPBRLightmap`, a single pass cannot simultaneously bind `FogGroup`, `IBLGroup`, `ShadowGroup`, `CSMGroup`, or `LitPBRSkinGroup` at group 3 without a custom shader using `PBRLightmapBindingsWGSLForGroup`.
  - Specular indirect lighting from probes/cubemaps is not included in the diffuse lightmap path; only baked diffuse irradiance + dynamic `LightRig` specular/diffuse + emissive are evaluated.
  - Because `GPUMaterial.Bind(pass)` binds at `MaterialGroup` (1), callers using `NewPBRLightmapPipeline` bind `mat.BindGroup()` explicitly at `PBRLightmapMaterialGroup` (2).

## Verification

- `packages/game/render3d/pbr_lightmap_test.go` verifies exact pixel parity with `LightmapRenderer` (dielectric, rough, untextured, no dynamic lights), replacement of `LightRig.Ambient`, exact direct-term parity with `LitPBRWGSL` when adding a `LightRig` directional light, end-to-end glTF `MOZ_lightmap` + metallic-roughness/normal/occlusion/emissive rendering in `gpu.View` on both `full` and `nsgotest.NoGPUCompute` profiles, Linear/RGBA16F/sRGB/RGBM/RGBD encodings, shared atlas `UVTransform` + `SetModel`, point/spot lights against `EvaluatePBRLightmap`, `LinearOutput`, `Retained` device-loss recovery, validation errors, `EXT_lightmap` / `extras.lightmap` / `KHR_texture_transform`, and idempotent `Close()`.
