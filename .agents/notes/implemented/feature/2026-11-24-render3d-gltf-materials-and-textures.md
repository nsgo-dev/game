# Agent Note: glTF 2.0 Materials and Textures Mapping in render3d

Status: implemented

## Problem

`game/gltf` decodes glTF 2.0 PBR materials, textures, samplers, images, `KHR_texture_transform`, `KHR_materials_unlit`, and `KHR_materials_variants` (see [glTF KHR_texture_transform and KHR_materials_variants validation and helpers](./2026-11-24-gltf-texture-transform-and-variants.md)), and `game/render3d` provides GPU textures, samplers, and metallic-roughness PBR materials (`Material`, `MaterialDefaults`, `GPUMaterial`, `MaterialPipeline` — see [3D Metallic-Roughness PBR Materials in render3d](./2026-11-24-render3d-metallic-roughness-materials.md)), but games had to hand-wire every glTF material, decode images, track sRGB vs linear color spaces, translate glTF filter/wrap enums, deduplicate shared textures and samplers, and resolve variant/default fallbacks before drawing a glTF asset (`#119`).

Several subtle glTF 2.0 details break naive mappers:
- Color maps (`baseColorTexture`, `emissiveTexture`) must upload as sRGB (`FormatRGBA8UnormSRGB`) while data maps (`metallicRoughnessTexture`, `normalTexture`, `occlusionTexture`) must stay linear (`FormatRGBA8Unorm`); a single glTF image may be shared across multiple linear slots (packed ORM textures) or even across both a color slot and a data slot.
- Omitted glTF sampler properties (such as `samplers: [{}]` in Khronos `DamagedHelmet`) default to `REPEAT` wrapping and linear/mipmapped filtering per the glTF 2.0 specification; mapping omitted wrap modes to `ClampToEdge` corrupts assets whose UVs wrap outside `[0, 1]` (`DamagedHelmet` has `V ∈ [1.0, 2.0]`).
- WebGPU rejects `MaxAnisotropy > 1` on any sampler whose min, mag, or mipmap filter is `FilterNearest`.
- Unassigned primitives (`prim.Material == -1`) require a glTF 2.0 default material, and a failed upload partway through decoding images must not leak earlier GPU textures or samplers.

## Decision

`packages/game/render3d/gltf_material.go` implements the glTF 2.0 material and texture upload pipeline (`UploadGLTFMaterials`, `GLTFMaterialSet`, `GLTFMaterialUpload`, `GLTFMaterialInfo`, `GLTFTextureSlot`, `SamplerOptionsFromGLTF`, `AlphaModeFromGLTF`) inside `game/render3d`:

- **Package boundary (`render3d` imports `game/gltf`, `gltf` stays GPU-free)**: Following the same layering as `anim3d` (see [glTF 2.0 animation playback in anim3d](./2026-11-24-anim3d-gltf-playback.md)), `game/gltf` remains a pure-Go format loader depending only on the standard library, `math3d`, and `loaderr`, while `render3d`'s core `Mesh`, `MorphMesh`, and `Material` types stay format-agnostic (preserving the format-neutral design of [glTF 2.0 morph target evaluation and GPU morph meshes](./2026-11-24-gltf-morph-targets.md)) and `gltf_material.go` provides the dedicated glTF-to-GPU material bridge.
- **Four-pass atomic upload with zero leaks**:
  1. Validate every material scalar, alpha mode, `KHR_materials_emissive_strength`, sampler enum, and texture/image/sampler index up front before decoding any image or creating any GPU object. Collect only actually referenced samplers and `(imageIndex, sRGB)` texture keys in deterministic order.
  2. Create deduplicated `GPUSampler` handles via `SamplerOptionsFromGLTF` (omitted wrap -> `AddressRepeat`, omitted filters -> trilinear with mipmaps), applying `GLTFMaterialUpload.MaxAnisotropy` only to all-linear samplers.
  3. Decode each referenced glTF image once via `doc.DecodeImage` and upload each unique `(imageIndex, sRGB)` pair once via `UploadTexture` (color slots `SRGB: true`, data slots `SRGB: false`, mipmaps enabled when any referencing sampler wants mipmaps or `opts.Mipmaps` is set, unless `opts.DisableMipmaps` is set).
  4. Create the glTF 2.0 default material (`DefaultMaterial()` / `Material(-1)`) and each document `GPUMaterial`, preserving per-slot `GLTFTextureSlot` UV/transform metadata (`EffectiveTexCoord`, deep-copied `KHR_texture_transform`) and `Unlit` flag on `GLTFMaterialInfo`. Any error closes every GPU resource created so far before returning.
- **Variant and lifecycle integration**: `MaterialForPrimitive` / `MaterialForVariant` / `InfoForPrimitive` / `InfoForVariant` delegate to `gltf.Document` variant lookup without mutating `Primitive.Material`. `GLTFMaterialSet` owns `Close()`, `Valid()`, `ByteSize()`, `TextureCount()`, `SamplerCount()`, and `Retained` device-loss restoration.
- **Khronos `DamagedHelmet` reference fixture**: `packages/game/gltf/testdata/khronos/DamagedHelmet/` commits the unmodified Khronos `DamagedHelmet.gltf`, `DamagedHelmet.bin`, and `LICENSE.md` alongside its five JPEG maps downscaled 8×8 box-filtered (sRGB linear-light for albedo/emissive, linear for metallicRoughness/AO, unit-renormalized vectors for normal) from 2048×2048 to 256×256 (~84 KB total) so headless software renders stay fast while preserving full visual fidelity.

## Alternatives considered

### Place the glTF-to-GPU material uploader inside `packages/game/gltf`

Putting `UploadMaterials` in `game/gltf` would keep `render3d` free of a `game/gltf` import. It was rejected because `game/gltf` is documented and used as a pure-Go format loader (standard library + `math3d` + `loaderr`), imported by `anim3d` and headless tools; making `gltf` import `render3d` would pull `nsgo/core`, `nsgo/gpu`, `scene3d`, and `particles` into every pure-Go glTF consumer.

### Deduplicate uploaded GPU textures solely by `imageIndex`

Caching one `*GPUTexture` per glTF image index is simpler, but if a glTF asset references the same image in both an sRGB slot (`baseColorTexture` or `emissiveTexture`) and a linear data slot (`occlusionTexture`, `metallicRoughnessTexture`, or `normalTexture`), one slot would sample with the wrong transfer function. Keying GPU uploads by `(imageIndex, sRGB)` while caching decoded Go `image.Image`s by `imageIndex` avoids redundant PNG/JPEG decoding, still uploads shared ORM linear maps once, and guarantees exact color-space decoding on every slot.

### Commit full 2048×2048 `DamagedHelmet` textures (~3.8 MB) to `testdata/khronos/`

Keeping the raw 2K JPEGs makes the fixture byte-for-byte identical to Khronos's release, but adds ~3.2 MB of JPEG blobs to git and allocates ~80 MB of RGBA8 memory plus CPU mipmap chains on every `go test` run in `GPUSoftwareDevice`. Downscaling the five maps to 256×256 with physically accurate color-space/normal filtering keeps `DamagedHelmet.gltf` and `DamagedHelmet.bin` 100% unmodified while cutting total image size to 84 KB and keeping test runtime under two seconds at 192×192 reference resolution.

## Consequences

- **Benefits**:
  - Loading and rendering a real PBR glTF 2.0 model (`DamagedHelmet` and multi-variant / multi-material scenes) takes one `UploadGLTFMaterials` call with exact sRGB/linear handling, automatic ORM/shared-image deduplication, compliant glTF 2.0 sampler defaults, `KHR_materials_variants` and `KHR_materials_emissive_strength` support, and `Retained` device-loss survival across all NSGo GPU backends including WebGL 2 and OpenGL ES 3.0.
  - `game/gltf` remains completely decoupled from `nsgo/gpu` and `render3d`.
- **Costs & limits**:
  - `game/render3d` now depends on `game/gltf` (pure Go; no impact on the 2D `cmd/wasmprobe` size budget).
  - `MaterialWGSL` uses `MeshVertexLayout`'s single UV attribute, so per-slot `KHR_texture_transform` matrices and `TEXCOORD_1+` overrides are exposed via `GLTFTextureSlot` for callers that bake transformed UVs (`doc.TransformTexCoords`) or write custom multi-UV shaders rather than applied automatically inside `MaterialWGSL`.

## Verification

- `packages/game/render3d/gltf_material_test.go` verifies:
  - All glTF sampler filter/wrap modes, empty-sampler defaults, and alpha modes (`TestSamplerOptionsFromGLTFAndAlphaMode`).
  - Shared ORM deduplication, dual-color-space `(imageIndex, sRGB)` separation, mipmap counts, WebGPU-safe `MaxAnisotropy`, `KHR_materials_emissive_strength`, `KHR_materials_unlit`, `KHR_texture_transform` metadata, `Source == -1` fallback, `Material(-1)` default material, `ByteSize`, and `Close` (`TestUploadGLTFMaterialsDeduplicationAndColorSpaces`).
  - `KHR_materials_variants` lookup and GPU rendering across `AlphaOpaque`, `AlphaMask` + `DoubleSided`, `AlphaBlend`, and sRGB-vs-linear base color verification (`TestUploadGLTFMaterialsVariantsAndRendering`).
  - Validation and error wrapping for malformed documents, indices, samplers, factors, and corrupt image data (`TestUploadGLTFMaterialsErrors`).
  - Khronos `DamagedHelmet` rendered against `packages/game/render3d/testdata/golden/damaged_helmet_reference.png`, verifying emissive HUD contribution and textured vs untextured diff (`TestDamagedHelmetMatchesReference`), plus headless `gpu.View` parity across full and `nsgotest.NoGPUCompute` profiles (`TestDamagedHelmetDrawsInGPUView`) and `Retained` device-loss restoration (`TestGLTFMaterialsRetainedSurvivesDeviceLoss`).
- Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./gltf ./render3d`, root conformance/stability tests, `go run ./tools/changelog check`, and `bun run verify-notes`.
