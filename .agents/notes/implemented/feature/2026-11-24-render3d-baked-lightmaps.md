# Agent Note: Baked lightmaps across game/gltf and game/render3d

Status: implemented

## Problem

External lightmap bakers (such as Blender, Unity, and Godot) export static indirect lighting onto a non-overlapping secondary UV chart (`TEXCOORD_1`) while tiled albedo maps stay on `TEXCOORD_0`. Previously, `game/gltf` decoded `Primitive.TexCoords1` but had no helper to extract lightmap bindings (`MOZ_lightmap`, `EXT_lightmap`, or `extras.lightmap`) or fall back cleanly to `TEXCOORD_0`, and `game/render3d` only supported a single UV channel in `MeshVertexLayout` (32 bytes) with no baked lightmap shader, encoding pipeline, or shared lightmap atlas UV transform.

## Decision

Baked lightmap support is split cleanly across `packages/game/gltf/lightmap.go` and `packages/game/render3d/lightmap.go`, preserving `render3d`'s format-agnostic package boundary established in [gltf-morph-targets](2026-11-24-gltf-morph-targets.md):

- **glTF metadata & UV extraction (`packages/game/gltf/lightmap.go`)**:
  - `Document.MaterialLightmap(materialIndex)` resolves `MOZ_lightmap`, `EXT_lightmap`, and `extras.lightmap` (in that order), defaulting `texCoord` to `1` (`TEXCOORD_1`), `intensity` to `1.0`, and `UVScale` to `[1, 1]`, and parsing nested `KHR_texture_transform`, `scaleOffset`, `uvScale`, and `uvOffset` with strict finiteness and bounds validation.
  - `PrimitiveLightmapUVs` and `Document.LightmapTexCoords` return `TEXCOORD_1` (or an explicit higher `texCoord`) and fall back cleanly to `TEXCOORD_0` when `TEXCOORD_1` is absent, reporting whether fallback occurred.
- **Dual-UV GPU mesh layout (`packages/game/render3d/lightmap.go`)**:
  - `LightmapVertex` and `LightmapMesh` use a dedicated 40-byte interleaved layout (`LightmapVertexLayout`: position at `@location(0)`, normal at `@location(1)`, `UV0` at `@location(2)`, `UV1` at `@location(3)`). When `UV1` is omitted, `UploadLightmapMesh` copies `UV0` into the `UV1` attribute slot and sets `GPULightmapMesh.UsedUV0Fallback()`.
  - `LightmapMeshFromAttributes` mirrors `gltf.Primitive` slice types directly so glTF primitives upload in one call without `render3d` importing `game/gltf`.
- **Encodings and shared-atlas GPU renderer (`packages/game/render3d/lightmap.go`)**:
  - `LightmapEncoding` supports `LightmapLinear` (8-bit linear or float16 HDR via `UploadLightmapRGBA16F`), `LightmapSRGB` (automatically switching to hardware linear pass-through when `Lightmap.SRGB()` is true to prevent double-decoding), `LightmapRGBM` (`EncodeRGBM` / `DecodeRGBM` / `EncodeRGBMImage`), and `LightmapRGBD` (`EncodeRGBD` / `DecodeRGBD` / `EncodeRGBDImage`) so HDR baked lightmaps work in standard 8-bit PNGs on WebGL 2 and mobile.
  - `LightmapRenderer` uses three bind groups: `CameraGroup` (group 0, reusing `CameraWGSL`), `LightmapDrawGroup` (group 1, a 256-byte-aligned dynamic uniform ring buffer carrying `model`, `normal_matrix` (from `math3d.NormalMatrix`), `base_color`, `uv_transform`, encoding/intensity parameters, `ambient`, and an optional additive `DirectLight` that reuses `DirectionalLight` and its travel-direction convention), and `LightmapTextureGroup` (group 2, cached by `(baseTex, baseSamp, lmTex, lmSamp)`). Multiple meshes sharing one lightmap atlas with distinct `UVTransform` sub-rectangles draw in a single render pass without clobbering uniforms or allocating bind groups per frame.
  - `LightmapWGSL` avoids `exp`, `exp2`, and `log2` (which NSGo v0.3.0's software GPU VM omits) and uses `pow` and algebraic math so hardware backends and headless software GPU tests match accurately.

## Alternatives considered

### Expanding `MeshVertexLayout` from 32 to 40 bytes for all meshes

Adding `UV1` to `render3d.Mesh` and `MeshVertexLayout` would let one vertex layout draw both unlightmapped and lightmapped geometry. It was rejected because it would increase vertex memory by 25% for every dynamic, skinned, morphed, and unlit mesh in the engine and break existing pipelines expecting the 32-byte `MeshVertexLayout`. Keeping `LightmapVertexLayout` dedicated to `GPULightmapMesh` keeps existing pipelines untouched.

### Importing `game/gltf` directly inside `packages/game/render3d`

Adding a `LightmapMeshFromGLTF(gltf.Primitive)` helper directly in `render3d` would save passing five fields. It was rejected to stay consistent with [gltf-morph-targets](2026-11-24-gltf-morph-targets.md): keeping `render3d` free of a `game/gltf` dependency avoids pulling glTF, Draco, and meshopt decoders into procedural or non-glTF 3D applications.

### Per-draw uniform buffer allocation instead of dynamic uniform offsets

Allocating a separate `gpu.Buffer` and `gpu.BindGroup` per draw call would simplify `LightmapRenderer`. It was rejected because baking scenes routinely draw dozens of static meshes sharing one lightmap atlas per frame; a 256-byte-aligned dynamic uniform ring buffer reset on `BeginFrame()` (growing safely by retiring old buffers until the next `BeginFrame`) supports arbitrary draws per pass with zero steady-state GPU allocations.

## Consequences

- **Benefits**:
  - Baked glTF scenes with separate tiled `TEXCOORD_0` albedo UVs and unique `TEXCOORD_1` lightmap charts render out of the box across desktop, mobile, WebGPU, WebGL 2 (`NoGPUCompute`), and NSGo's software renderer.
  - HDR baked irradiance works both natively (`FormatRGBA16Float`) and inside 8-bit PNGs (`LightmapRGBM` / `LightmapRGBD`), and shared lightmap atlases work with per-object `UVScale` / `UVOffset` in one render pass.
  - Zero edits to existing `gltf` or `render3d` Go files keep the implementation isolated and low-conflict.
- **Costs and limitations**:
  - Callers drawing lightmapped meshes across frames should call `LightmapRenderer.BeginFrame()` once per frame; omitting it causes the dynamic ring buffer to grow until `Close()` is called.
  - `LightmapRenderer` implements Lambertian baked indirect + optional single directional light; its output is encoded with the same `linear_to_srgb` as `LitPBRWGSL` so both match in one pass. Feeding lightmaps into the PBR material path as the indirect term (reusing `LightmapVertexLayout`, `GPULightmapMesh`, `LightmapEncoding`, and the RGBM/RGBD/RGBA16F helpers) is follow-up #752.
  - `UploadLightmapRGBA16F` uploads mip level 0 only (no CPU float16 mip chain generation yet).

## Verification

- `packages/game/gltf/lightmap_test.go` verifies `MOZ_lightmap`, `EXT_lightmap`, `extras.lightmap`, `KHR_texture_transform`, `scaleOffset`, `TEXCOORD_1`/`TEXCOORD_2` extraction, `TEXCOORD_0` fallback, and malformed-input rejection.
- `packages/game/render3d/lightmap_test.go` renders a baked glTF scene with distinct tiled `TEXCOORD_0` and warm/cool `TEXCOORD_1` lightmap through `gpu.View` under both full and `nsgotest.NoGPUCompute` (WebGL 2 / OpenGL ES 3.0) profiles, verifies shared lightmap atlas `UVTransform` multi-draw rendering, verifies Linear, RGBA16F HDR, sRGB (software and hardware), RGBM HDR, and RGBD HDR encodings, and tests ambient fallback, additive direct lighting, missing-`TEXCOORD_1` fallback, dynamic ring growth, and validation errors.
