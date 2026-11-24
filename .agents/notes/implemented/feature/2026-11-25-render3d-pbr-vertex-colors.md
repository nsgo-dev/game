# Agent Note: 3D PBR Vertex Colors in render3d

Status: implemented

## Problem

`render3d` already decodes and uploads glTF `COLOR_0` via `ColoredMesh`, `GPUColoredMesh`, `ColoredVertexLayout`, and `ColoredMeshFromGLTF` ([3D Unlit Materials and Vertex Colors in render3d](./2026-11-24-render3d-unlit-and-vertex-colors.md)), but only `NewUnlitPipeline` (`UnlitWGSL`) consumed `@location(3) color: vec4<f32>`. Both `NewMaterialPipeline` (`MaterialWGSL`, [3D Metallic-Roughness PBR Materials in render3d](./2026-11-24-render3d-metallic-roughness-materials.md)) and `NewLitPBRPipelineWithOptions` (`LitPBRWGSL` and its fog, IBL, shadow, and CSM variants) required 32-byte `MeshVertexLayout`, and `EvaluateMaterial` had no vertex-color input. Per glTF 2.0 core specification §3.7.2.1, `COLOR_0` multiplies the linear `baseColor` (both RGB and alpha) for every material, not only `KHR_materials_unlit`.

## Decision

`render3d` adds opt-in `VertexColors bool` pipeline options to both `MaterialPipelineOptions` and `LitPBRPipelineOptions`, keeping the default 32-byte `MeshVertexLayout` path unchanged for uncolored meshes while multiplying `base_color` by linear `@location(3) color` whenever `VertexColors` is true:

- **Metallic-roughness material shaders (`material.go`)**: `materialFragmentHeadWGSL` is split into shared `materialFragmentSampleWGSL` (uniform-control-flow texture reads and screen-space derivatives) and `materialFragmentShadeWGSL` (alpha-mask discard, normal perturbation, Cook-Torrance GGX, hemisphere ambient, and emissive). `MaterialColoredWGSL` (plus unexported `materialColoredLinearWGSL` and the fogged substitution in `NewMaterialPipeline`) uses `ColoredVertexLayout` and computes `let base_color = material.base_color_factor * base_sample * in.color;` before `AlphaMask` cutoff discard and PBR shading, while `MaterialWGSL` remains byte-identical.
- **Lit PBR shaders (`light.go`)**: `LitPBRColoredWGSL` extends `LitPBRWGSL` with `ColoredVertexLayout` (`@location(3) color`) and multiplies `pbr_draw.base_color.rgb` and `pbr_draw.base_color.a` by `in.color` in linear space. `NewLitPBRPipelineWithOptions` applies `litPBRWithVertexColors` across all non-skinned variants (`LitPBRWGSL`, fog, IBL, directional/spot/point shadows, CSM, and their `LinearOutput` forms) so IBL (`evaluate_ibl`) and direct lighting (`shade_pbr*`) both receive the vertex-colored linear base color without duplicating shader constants across six files.
- **Skinning exclusion**: `SkinnedVertexLayout` already assigns `@location(3)` to joint indices (`JOINTS_0`) and `SkinnedMesh` carries no vertex colors, so combining `VertexColors: true` with `SkinLayout != nil` fails fast with `gpu.ErrInvalidDescriptor` in both `NewMaterialPipeline` and `NewLitPBRPipelineWithOptions`.
- **CPU reference parity**: `MaterialEvalInput` gains `VertexColor [4]float32` and `HasVertexColor bool` (matching the existing `Has*Sample` pattern so zero-initialized callers default to white `[1, 1, 1, 1]` while explicit black `[0, 0, 0, 1]` or zero-alpha `[r, g, b, 0]` is distinguishable). `EvaluateMaterial` multiplies `baseColor` and `baseAlpha` by `VertexColor` before `AlphaMask` discard and PBR evaluation. `PBRMaterial.WithVertexColor(c math3d.Vec4)` returns a tinted `PBRMaterial` for CPU reference calls to `EvaluatePBR`, `ShadePoint`, `ShadePointShadowed`, `ShadePointVisibility`, and `EvaluateIBL`.

## Alternatives considered

### Switch `MaterialWGSL` and `LitPBRWGSL` unconditionally to `ColoredVertexLayout`

Using 48-byte `ColoredVertexLayout` for all metallic-roughness and lit PBR pipelines would eliminate the `VertexColors` option and let every mesh share one pipeline. It was rejected because it would break every existing caller passing a 32-byte `GPUMesh` to `NewMaterialPipeline` or `NewLitPBRPipeline` and inflate vertex buffer size by 50% for uncolored meshes.

### Add a `UseVertexColor` uniform flag to `MaterialUniforms` instead of a pipeline option

A uniform flag mirrors `UnlitMaterial.UseVertexColor` and allows toggling vertex colors dynamically per material. It was rejected because the vertex buffer layout (`MeshVertexLayout` vs `ColoredVertexLayout`) is fixed on the `gpu.Pipeline`, not in the material uniform block; `ColoredMesh` already fills omitted `COLOR_0` with linear white `(1, 1, 1, 1)`, so colored and uncolored `GPUColoredMesh` draws share one `VertexColors: true` pipeline without any uniform overhead or change to `MaterialUniformSize` (64 bytes).

### Duplicate `LitPBR*ColoredWGSL` constants in every group-3 feature file

Writing explicit colored and linear-colored constants in `fog.go`, `skybox.go`, `shadow.go`, `local_shadows.go`, and `csm.go` avoids runtime string composition. It was rejected because all seven non-skinned `LitPBR*` shaders share the exact `litPBRPreludeWGSL` prefix and `pbr_draw.base_color.{rgb,a}` references; rewriting them through `litPBRWithVertexColors` in `light.go` keeps a single source of truth for shading math and avoids cross-file drift.

## Consequences

- **Benefits**: Lit glTF 2.0 assets with `COLOR_0` (VEC3 or VEC4, opaque, alpha-mask, or alpha-blend) now render accurately with metallic-roughness materials and scene lighting, verified end-to-end against the Khronos `VertexColorTest` sample, synthetic VEC3/VEC4 glTF fixtures, CPU `EvaluateMaterial` / `ShadePoint` / `EvaluateIBL` parity, and headless `gpu.View` on both full and `nsgotest.NoGPUCompute` (WebGL 2 / GLES 3.0) profiles. Existing `MeshVertexLayout` callers remain byte-for-byte unchanged.
- **Costs**: Scenes mixing `GPUMesh` (`VertexColors: false`) and `GPUColoredMesh` (`VertexColors: true`) bind two pipelines unless they upload uncolored geometry as `ColoredMesh` (whose default white `(1, 1, 1, 1)` colors match uncolored rendering identically). Skinned meshes (`SkinnedVertexLayout`) still cannot carry `COLOR_0` without a combined skinned+colored vertex layout.
