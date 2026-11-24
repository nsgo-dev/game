# Agent Note: HDR environment cubemaps, textured glTF material IBL, and DamagedHelmet reference

Status: implemented

## Problem

[3D skybox and image-based lighting (IBL)](./2026-11-24-render3d-skybox-and-ibl.md) landed RGBA8 low dynamic range cubemaps and `LitPBRIBLWGSL` (flat per-draw scalar PBR materials), leaving two gaps for issue #253:

1. Radiance above 1.0 was clamped in RGBA8 cubemaps, flattening specular highlights from bright studio softboxes, suns, and `.hdr` environment maps.
2. Textured glTF 2.0 metallic-roughness materials (`GPUMaterial` / `GLTFMaterialSet`, carrying all five maps: base color, metallic-roughness, tangent-space normal, occlusion, and emissive) had no IBL pipeline, and the Khronos `DamagedHelmet` sample had not yet been rendered and verified under an HDR environment cubemap and skybox.

## Decision

`packages/game/render3d/ibl_hdr.go` extends the skybox and IBL subsystem without breaking existing LDR `Cubemap` or `MaterialPipeline` callers:

- **Radiance `.hdr` (RGBE) codec (`HDRImage`, `DecodeHDR`, `EncodeHDR`, `CubemapFromHDR`, `CubemapFromHDREquirectangular`)**: pure-Go decoder and encoder for Radiance `32-bit_rle_rgbe` images supporting adaptive RLE, old-style RLE, uncompressed scanlines, `EXPOSURE=` scaling, and `+/-Y +/-X` orientations, projecting equirectangular panoramas in linear float32 light with seamless horizontal wrapping.
- **Linear float32 HDR cubemaps (`HDRCubemap`, `GradientSkyHDRCubemap`, `HDRCubemapFromCubemap`)**: stores six square `[][3]float32` linear radiance faces and optional mip chains without clamping values > 1.0. `ComputeIrradianceSHHDR` and `PrefilterEnvironmentHDR` run exact solid-angle L2 SH projection and deterministic Hammersley GGX prefiltering directly on unclamped linear radiance.
- **GPU half-float cubemap upload (`UploadCubemapHDR`)**: uploads `*HDRCubemap` faces and mips as a standard `*GPUCubemap` in `gpu.FormatRGBA16Float` via `Device.WriteTextureMip`, packing finite IEEE 754 binary16 texels (clamping above `65504` so shaders never sample `Inf`/`NaN`), with automatic `FormatRGBA8Unorm` fallback only when a device lacks `FormatRGBA16Float`. Because it returns the existing `*GPUCubemap`, HDR cubemaps plug straight into `SkyboxPipeline.BindCubemap`, `NewGPUIBLBuffer`, `GPUCubemap.ByteSize`, and `Retained` device-loss restoration.
- **Tone-mapped skybox option (`SkyboxPipelineDesc.ToneMap`, `SkyboxToneMapWGSL`)**: default `SkyboxWGSL` writes linear `sampled.rgb * skybox.exposure` (ready for an `HDRTarget` resolved by `ToneMapPass`), while `ToneMap: true` applies Reinhard + gamma 2.2 matching `MaterialWGSL` and `MaterialIBLWGSL` for direct-to-LDR draws.
- **Textured glTF PBR + IBL (`MaterialIBLGroup = 2`, `MaterialIBLWGSL`, `MaterialIBLLinearWGSL`, `NewMaterialIBLPipeline`, `EvaluateMaterialIBL`)**: binds `CameraGroup` (0), `GPUMaterial` at `MaterialGroup` (1), and `GPUIBLBuffer` at `MaterialIBLGroup` (2), evaluating all five glTF maps, alpha modes, double-sided normals, split-sum IBL, and emissive, with `LinearOutput: true` targeting `HDRTarget` + `ToneMapPass`.
- **Isolated `DamagedHelmet` IBL reference (`packages/game/render3d/testdata/damagedhelmet/reference_ibl.png`)**: stored alongside `reference.png` inside the separate `testdata/damagedhelmet` module (`go.mod`), preserving the CC-BY-NC-4.0 exclusion from the published `packages/game` module (see [glTF materials and textures](./2026-11-24-render3d-gltf-materials-and-textures.md)).

### Invariants

- All HDR image and cubemap validation/decode errors wrap `ErrInvalidCubemap` and allocate zero GPU resources on failure.
- `UploadCubemapHDR` clamps finite float16 texels to `[0, 65504]` (`0x7BFF`) so no half-float `Inf` or `NaN` can enter GPU sampling.
- `MaterialIBLWGSL`, `MaterialIBLLinearWGSL`, and `SkyboxToneMapWGSL` never use `exp`, `exp2`, `log`, or `log2` (NSGo v0.3.0 software GPU VM omits them; `nsgo-dev/nsgo#2334`) and require zero compute stages so `full` and `nsgotest.NoGPUCompute` render identically.
- Any render derived from Khronos `DamagedHelmet` must stay inside `packages/game/render3d/testdata/damagedhelmet/` and skip when that directory is absent.

## Alternatives considered

### Mutating `Cubemap` to hold union RGBA8/float32 fields vs adding `HDRCubemap`

Adding optional `FloatFaces [6][][3]float32` fields to `Cubemap` would keep one CPU type name, but every existing `Cubemap` caller and validator would have to disambiguate which representation is active and handle partially populated dual-representation structs. Keeping `Cubemap` strictly RGBA8 and introducing `HDRCubemap` as a dedicated linear float32 type that uploads into the exact same `*GPUCubemap` preserves full backwards compatibility and makes the LDR-vs-HDR distinction explicit in Go's type system.

### Extending `MaterialPipelineOptions` in `material.go` vs dedicated `NewMaterialIBLPipeline` in `ibl_hdr.go`

Adding an `IBLLayout` field to `MaterialPipelineOptions` would reuse `NewMaterialPipeline` just like `FogLayout` and `SkinLayout`. However, `MaterialWGSL` combines a hardcoded directional key light (`DefaultMaterialLight`) with a fake hemisphere ambient term, whereas an environment-lit glTF pipeline replaces both with `evaluate_ibl` (plus optional `SampleCount` and `Retained` pipeline options) and keeps all IBL material logic self-contained in `ibl_hdr.go` without expanding `MaterialPipelineOptions`'s mutual-exclusion matrix on bind group 2.

### Placing `DamagedHelmet` IBL golden PNG in `testdata/golden/` vs `testdata/damagedhelmet/`

Putting `damaged_helmet_ibl.png` under `packages/game/render3d/testdata/golden/` would follow other `render3d` goldens, but `DamagedHelmet` is licensed CC-BY-NC-4.0 and renders of it are derivatives under that license. Keeping `reference_ibl.png` inside `packages/game/render3d/testdata/damagedhelmet/` (which has its own `go.mod`) keeps all CC-BY-NC-4.0 files out of the Apache-2.0 `packages/game` Go module zip.

## Consequences

- **Benefit:** `render3d` now supports full end-to-end HDR IBL from Radiance `.hdr` equirectangular files through `FormatRGBA16Float` cubemaps, L2 SH diffuse irradiance, GGX prefiltered specular, textured 5-map glTF PBR materials, `HDRTarget`, and `ToneMapPass` (`ToneMapNeutral` / ACES / AgX / Reinhard) across all backends including WebGL 2 and OpenGL ES 3.0.
- **Benefit:** HDR environments produce measurably brighter metallic specular highlights than LDR-clamped environments while remaining bit-for-bit deterministic and recoverable across `Retained` GPU device loss.
- **Cost:** `HDRCubemap` uses 12 bytes/texel on CPU and `FormatRGBA16Float` uses 8 bytes/texel on GPU (double RGBA8), so very large environment cubemaps consume more memory; 32–128 px prefiltered faces keep footprint small.
- **Cost:** `MaterialIBLWGSL` shades purely from the environment map and emissive texture (no punctual `LightRig` group in the same 3-group pipeline); combining textured `GPUMaterial`, `GPUIBLBuffer`, and `GPULightBuffer` in one 4-group pipeline remains a future composition.

## Testing

- `packages/game/render3d/ibl_helmet_test.go` verifies Radiance `.hdr` RLE/uncompressed/EXPOSURE/orientation encode-decode round-trips and malformed-stream errors (`TestHDRImageCodecAndEquirectangular`), `HDRCubemap` validation and HDR SH/GGX prefilter accuracy (`TestHDRCubemapValidationAndBaking`), `UploadCubemapHDR` `FormatRGBA16Float` upload/byte-size/mipmaps and HDR-vs-LDR specular brightness (`TestUploadCubemapHDRAndSpecularBrightness`), pixel-for-pixel `MaterialIBLWGSL` vs `EvaluateMaterialIBL` and `SkyboxToneMapWGSL` parity (`TestMaterialIBLMatchesCPUReference`), Khronos `DamagedHelmet` rendered under an `.hdr`-decoded studio environment + skybox + `ToneMapNeutral` against `testdata/damagedhelmet/reference_ibl.png` with analytical skybox, visor, HDR-vs-LDR highlight, zero-IBL, and warm-environment checks (`TestDamagedHelmetHDRIBLReference`), and `gpu.View` parity across `full`, `nsgotest.NoGPUCompute`, and `Retained` device-loss restoration (`TestDamagedHelmetHDRIBLInGPUViewProfilesAndDeviceLoss`).
