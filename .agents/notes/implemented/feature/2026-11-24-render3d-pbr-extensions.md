# Agent Note: Extended 3D PBR materials (emissive strength, clearcoat, transmission)

Status: implemented

## Problem

`packages/game/gltf` only decoded base glTF 2.0 metallic-roughness properties and `KHR_materials_unlit`, leaving `KHR_materials_emissive_strength`, `KHR_materials_clearcoat`, and `KHR_materials_transmission` as unparsed raw JSON and rejecting assets that declare them in `extensionsRequired`. On the rendering side, `packages/game/render3d` had camera uniforms, mesh buffers, and texture upload helpers ([render3d textures and samplers](./2026-11-24-render3d-textures-and-samplers.md)), but no GPU material pipeline evaluating HDR emissive strength, dual-lobe clearcoat, or screen-space optical transmission across NSGo's backends (including the software GPU VM used by headless `nsgotest`).

## Decision

Support for all three Khronos material extensions spans `packages/game/gltf` and `packages/game/render3d` in dedicated additive files (`packages/game/gltf/material_ext.go`, `packages/game/render3d/pbr_ext.go`) to keep cross-package contracts explicit and avoid file conflicts with concurrent `render3d` work:

- **Typed glTF extension parsing and validation (`packages/game/gltf/material_ext.go`)**:
  - `Material.EmissiveStrength` defaults to `1.0` when the extension is absent or empty (`{}`), rejects non-finite or negative values, and exposes `Material.EffectiveEmissive()`.
  - `Material.Clearcoat` (`*Clearcoat`, `nil` when absent) parses `clearcoatFactor` (`[0, 1]`), `clearcoatRoughnessFactor` (`[0, 1]`), `clearcoatTexture` (R), `clearcoatRoughnessTexture` (G), and `clearcoatNormalTexture` (default `Scale: 1`), reusing `parseTextureRef` so `KHR_texture_transform` ([glTF texture transform and variants](./2026-11-24-gltf-texture-transform-and-variants.md)) works uniformly on all three clearcoat texture slots.
  - `Material.Transmission` (`*Transmission`, `nil` when absent) parses `transmissionFactor` (`[0, 1]`) and `transmissionTexture` (R, with `KHR_texture_transform`).
  - All three extension names are accepted in `extensionsRequired`, while malformed payloads return `*loaderr.Error` at `materials[i]`.
- **Extended PBR GPU renderer and analytic CPU reference (`packages/game/render3d/pbr_ext.go`)**:
  - `ExtendedPBRRenderer` uses `CameraGroup` (group 0), `PBRExtSceneGroup` (group 1, 176-byte model/normal/lighting uniform with per-object `ExtendedPBRScene` bindings), and `PBRExtMaterialGroup` (group 2, 80-byte material uniform + 2 samplers + 8 sampled textures with neutral 1×1 fallbacks), drawing any `GPUMesh` built with `MeshVertexLayout()`.
  - **HDR emissive**: `emissiveFactor * emissiveTex * emissiveStrength` is evaluated in linear HDR light. `PBRToneMapNone` preserves values `> 1.0` on `gpu.FormatRGBA16Float` targets; `PBRToneMapACES`, `PBRToneMapReinhard`, and `PBRToneMapLinearSRGB` map HDR cleanly to 8-bit SDR targets.
  - **Clearcoat**: evaluates a secondary GGX specular lobe (`IOR = 1.5`, `F0 = 0.04`) with its own roughness and optional tangent-space normal map, attenuating the base layer by `1 - clearcoat * F_coat` for Fresnel energy conservation.
  - **Transmission**: mixes the dielectric diffuse term toward a screen-space `TransmissionBackground` texture (`gpu.RenderTarget.Color` from an opaque pass) tinted by `baseColor`, filtered with a 5-tap roughness-controlled kernel while leaving base and clearcoat specular reflections intact.
  - **Portable WGSL contract**: `PBRExtWGSL` uses Schlick multiplication (`f2 * f2 * f`), Narkowicz rational ACES, `textureSampleLevel(..., 0.0)`, and an analytic normal-derived orthonormal tangent basis, avoiding `exp`/`exp2`/`log2` and `dpdx`/`dpdy` so the shader runs identically on WebGPU/Metal/D3D11/OpenGL/GLES3 hardware and NSGo v0.3.0's software GPU VM.
  - `EvaluatePBRMaterialExt` mirrors the WGSL equations in pure Go for analytic reference verification.

## Alternatives considered

### Screen-space derivatives (`dpdx`/`dpdy`) for clearcoat tangent frames and mip-level transmission blur

Using `dpdx`/`dpdy` cotangent frames (MikkTSpace-free screen-space tangents) and hardware mip-biased `textureSampleBias` for frosted transmission is common in desktop PBR shaders. It was rejected because NSGo v0.3.0's software GPU VM omits `dpdx`/`dpdy` and `exp`/`exp2`/`log2`, and hardware WebGPU forbids `textureSample` inside non-uniform branches. An analytic orthonormal tangent frame plus an unbranched 5-tap `textureSampleLevel(..., 0.0)` kernel works identically across every backend without changing `MeshVertexLayout()`.

### Keeping extension properties in `Material.Extensions` raw JSON instead of typed structs

Leaving `KHR_materials_emissive_strength`, `KHR_materials_clearcoat`, and `KHR_materials_transmission` as raw JSON maps on `gltf.Material` would avoid adding fields to `gltf.Material`. It was rejected because `gltf.Load` would still reject assets listing them in `extensionsRequired`, invalid factors or out-of-range texture indices would go undetected at load time, and `KHR_texture_transform` on extension textures would not be decoded into `TextureRef.Transform`. Keeping `Material.Extensions` populated alongside the typed fields preserves backward compatibility.

### Single-pass alpha blending for `KHR_materials_transmission`

Approximating transmission with `BlendAlpha` and opacity `1 - transmissionFactor` would avoid a two-pass background target. It was rejected because alpha blending dims specular highlights and clearcoat reflections along with the diffuse layer, violating the Khronos `KHR_materials_transmission` specification, and cannot support roughness-dependent background blur or baseColor tinting of transmitted light.

## Consequences

- **Benefits**:
  - Khronos sample models using `KHR_materials_clearcoat` (`TextureTransformMultiTest`) and `KHR_materials_transmission` (`SunglassesKhronos`) parse and validate directly, and glTF materials flow into GPU rendering via `PBRMaterialExtFromGLTF`.
  - GPU rendering tests on NSGo's software renderer verify HDR `> 1.0` float16 readback, ACES tone-mapped progression, dual-lobe clearcoat highlights and grazing Fresnel base darkening, and two-pass `RenderTarget` transmission with preserved specular reflections against exact analytic references.
  - Zero-allocation uniform encoders (`EncodeUniform`, `EncodeSceneUniform`) and per-object `ExtendedPBRScene` bindings support multi-object single-pass scenes cleanly.
- **Costs & limits**:
  - `PBRExtWGSL` derives its clearcoat tangent basis from the world normal rather than per-vertex `TANGENT` attributes so it stays compatible with the 32-byte `MeshVertexLayout`; meshes requiring custom UV-aligned anisotropic normal seams will need a future tangent-bearing vertex layout.
  - Transmission uses thin-surface screen-space background sampling without `KHR_materials_volume` refraction thickness/absorption (Beer's law) or IBL prefiltered environment maps, which belong to follow-up issues under `#259`.
  - `ExtendedPBRMaterial` fixes texture bindings at creation time (`Update` updates scalar/vector factors only); rebinding a new `TransmissionBackground` after resize requires creating a new `ExtendedPBRMaterial`.

## Verification

- `env -u GOROOT go tool nsgo test ./gltf` runs `TestClearcoatKhronosSample` (`TextureTransformMultiTest`), `TestTransmissionKhronosSample` (`SunglassesKhronos`), `TestMaterialExtensionsSpecAndRequired`, and `TestMaterialExtensionsRejectMalformed`.
- `env -u GOROOT go tool nsgo test ./render3d` runs `TestPBRExtEmissiveStrengthHDRAndToneMap`, `TestPBRExtClearcoatDualLobeAttenuationAndTextures`, `TestPBRExtTransmissionOpaqueTransmissiveAndSpecular`, `TestPBRExtGLTFEndToEndAndMultiPassScene`, and `TestPBRExtValidationAndUniformEncoding`.
- `env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes` validate cross-platform builds, schemas, changelog, and note structure.
