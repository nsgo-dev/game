# Agent Note: Extended 3D PBR Materials (Emissive Strength, Clearcoat, and Transmission)

Status: implemented

## Problem

`game/gltf` already exposes `KHR_materials_emissive_strength` via `Material.EmissiveStrength()` and `game/render3d` folds it into `Material.EmissiveFactor`, but glTF 2.0 assets using `KHR_materials_clearcoat` (such as lacquered surfaces and car paint) or `KHR_materials_transmission` (such as glass lenses and crystal) cannot be loaded when listed in `extensionsRequired` and have no GPU or CPU shading path in `render3d` (#264).

Adding clearcoat and transmission must respect four architectural constraints established in [3D Metallic-Roughness PBR Materials in render3d](./2026-11-24-render3d-metallic-roughness-materials.md) and [glTF 2.0 Materials and Textures in render3d](./2026-11-24-render3d-gltf-materials-and-textures.md):

1. Plain `GPUMaterial` and `MaterialDefaults` must keep their lean 11-binding layout unchanged so ordinary metallic-roughness materials pay zero extra bindings or uniform bandwidth.
2. There must be no parallel fourth renderer duplicating base PBR features; extended materials must reuse `render3d.Material`, `LightRig`, alpha modes (`AlphaOpaque`, `AlphaMask`, `AlphaBlend`), `DoubleSided`, `HDRTarget`, and `MaterialWGSL`'s UV-aligned Cramer cotangent-frame normal perturbation (`dpdx`/`dpdy` + deterministic fallback).
3. The opaque scene color texture sampled by transmissive materials must not be baked into per-material bind groups, because swapchain resizes and `DynamicResolutionTarget` scale changes would otherwise force rebuilding every transmissive material's bind group.
4. Shaders must remain portable across WebGPU, Metal, Vulkan, D3D11, OpenGL, WebGL 2 / GLES 3.0 (`NoGPUCompute`), and NSGo v0.3.0's software GPU VM (which evaluates `exp`, `exp2`, `log`, and `log2` as 0).

## Decision

### glTF extension parsing (`packages/game/gltf/material_ext.go`)

`gltf.Material` gains optional additive fields `Clearcoat *Clearcoat` (`KHR_materials_clearcoat`) and `Transmission *Transmission` (`KHR_materials_transmission`), while keeping the existing `Material.EmissiveStrength()` method untouched to avoid duplicating emissive strength state. Both extensions are accepted in `extensionsRequired`, validate finite `[0, 1]` scalars and finite normal `scale`, and parse every extension texture reference through `Document.parseTextureRef` so `TexCoord` and `KHR_texture_transform` work identically to core PBR slots.

### Extended `Material` variant and per-frame `TransmissionBackground` (`packages/game/render3d/material_ext.go`)

- **`render3d.Material` and `PBRMaterial`**: `Material` adds `ClearcoatFactor`, `ClearcoatRoughnessFactor`, `ClearcoatTexture`/`Sampler`, `ClearcoatRoughnessTexture`/`Sampler`, `ClearcoatNormalTexture`/`Sampler`, `ClearcoatNormalScale`, `TransmissionFactor`, `TransmissionTexture`/`Sampler`, and `IOR` (zero defaults to `DefaultMaterialIOR = 1.5` via `EffectiveIOR()` for backwards compatibility with struct literals). `PBRMaterial`, `EvaluateMaterial`, `EvaluatePBR`, `ShadePoint`, and `ShadePointWithBackground` evaluate IOR-based dielectric F0, clearcoat dual-lobe specular with `1 - clearcoat * F_cc(Nc·V)` base-layer attenuation, and dielectric transmission `(1 - F_view) * (1 - metallic) * transmission * baseColor * bgLinear` with diffuse scaled by `1 - transmission`.
- **Lean vs. extended bind group layouts**: `MaterialBindGroupLayout` (bindings 0..10, 64-byte `MaterialUniforms`) stays unchanged for `GPUMaterial`. `ExtendedMaterialBindGroupLayout` extends `MaterialGroup` (group 1) with a 96-byte `ExtendedMaterialUniforms` (embedding `MaterialUniforms` in bytes 0..63) and four extra linear texture/sampler pairs at bindings 11..18 (clearcoat, clearcoat roughness, clearcoat normal, transmission). `ExtendedMaterialDefaults` wraps `MaterialDefaults` to share the same four 1×1 fallback textures (`WhiteSRGB`, `WhiteLinear`, `FlatNormal`, `Black`) and linear-repeat sampler without duplicating GPU texture memory.
- **UV-aligned clearcoat normal mapping**: `ExtendedMaterialBindingsWGSL` shares `ext_perturb_normal` (identical Cramer's-rule `dpdx`/`dpdy` Jacobian with deterministic orthonormal fallback from `MaterialWGSL`) for both base `NormalTexture` and `ClearcoatNormalTexture`. Per the glTF `KHR_materials_clearcoat` specification, `ClearcoatNormalTexture` perturbs the geometric normal `geom_n` independently of the base normal map.
- **Frame-level `TransmissionBackground` bind group**: `TransmissionBackground` owns a single pass-level bind group (binding 0 = 32-byte UV scale/clamp + sRGB decode flag uniform, binding 1 = opaque scene color `texture_2d<f32>`, binding 2 = linear clamp-to-edge sampler) bound once per pass at `TransmissionBackgroundGroup` (group 3 when paired with `LightLayout` at `ExtendedLightGroup = 2`, or `MaterialTransmissionBackgroundGroup = 2` without `LightLayout`). Screen UVs are derived from `frag_pos.xy / vec2<f32>(textureDimensions(transmission_bg_tex))`, scaled and half-texel clamped by `SetViewport` / `SetDynamicResolution` so swapchain resizes and `DynamicResolutionTarget` sub-viewport scale changes never touch per-material bind groups or bleed padding texels.
- **Pipeline variants & glTF upload**: `NewExtendedMaterialPipeline` builds the four `(LightLayout?, TransmissionLayout?)` combinations plus `LinearOutput` HDR variants for `HDRTarget` + `BloomPass` + `ToneMapPass`, keeping all base features (`AlphaOpaque`/`AlphaMask`/`AlphaBlend`, `DoubleSided`, `LightRig`, software-VM-safe WGSL with zero `exp`/`exp2`/`log`/`log2`). `UploadGLTFExtendedMaterials` shares the deduplicated upload pipeline of `UploadGLTFMaterials` (keeping plain `UploadGLTFMaterials` lean) and uploads the four extension maps as linear (`SRGB: false`), mapping `KHR_materials_emissive_strength`, `KHR_materials_clearcoat`, `KHR_materials_transmission`, and `KHR_materials_ior`.

## Alternatives considered

### Expand plain `MaterialBindGroupLayout` directly to 19 bindings for every material

Adding the four clearcoat/transmission texture-sampler pairs directly to `MaterialBindGroupLayout` would avoid introducing `ExtendedMaterialDefaults` and `GPUExtendedMaterial`. It was rejected because every ordinary metallic-roughness material would pay 32 extra uniform bytes and 8 extra texture/sampler bindings per draw even when neither clearcoat nor transmission is used; keeping plain `GPUMaterial` at 11 bindings and providing an extended layout variant preserves zero overhead on the common path while sharing one `Material` struct and fallback textures.

### Create a standalone fourth PBR renderer (`ExtendedPBRRenderer`) with per-material background bindings

Building a separate renderer owning its own scene-background texture inside each material bind group would decouple extended shading from `material.go` and `light.go`. It was rejected because it duplicates base PBR, normal mapping, alpha modes, double-sided culling, and light loops, breaks `Material` / `LightRig` composability, and forces recreating every transmissive material's bind group whenever the render target resizes or `DynamicResolutionTarget` changes scale.

### Compute clearcoat normal tangent frames with `cross(up, N)` instead of screen-space UV derivatives

Using a fixed world-up cross product avoids derivative math, but ignores triangle UV orientation and produces rotated or inverted anisotropic highlights whenever a mesh's UVs are rotated or mirrored. Reusing `MaterialWGSL`'s Cramer's-rule `dpdx`/`dpdy` cotangent frame (with its deterministic fallback for degenerate derivatives) aligns clearcoat normal maps accurately with UVs across every graphics API.

## Consequences

- **Benefits**: Games can load and render glTF 2.0 assets using `KHR_materials_emissive_strength`, `KHR_materials_clearcoat`, and `KHR_materials_transmission` through `render3d.Material`, `GPUExtendedMaterial`, `TransmissionBackground`, and `NewExtendedMaterialPipeline` with full `LightRig`, `HDRTarget`/`BloomPass`/`ToneMapPass`, `DynamicResolutionTarget`, `Retained` device-loss restore, and WebGL 2 / GLES 3.0 (`NoGPUCompute`) support, verified against Khronos sample models (`EmissiveStrengthTest`, `TextureTransformMultiTest`, `SunglassesKhronos`) and checked-in golden reference PNGs.
- **Costs**: Extended materials use 9 material textures + 1 background texture (10 total fragment textures, still well within WebGL 2 / GLES 3.0's 16-texture minimum) and require a two-pass draw (opaque pass into an offscreen target followed by `TransmissionBackground.SetTexture` / `SetDynamicResolution` before transmissive draws). Clearcoat roughness blur on transmitted backgrounds uses a 5-tap screen-space filter rather than a prefiltered mip chain on the opaque copy so WebGL 2 / software targets do not require render-target mipmap generation.
