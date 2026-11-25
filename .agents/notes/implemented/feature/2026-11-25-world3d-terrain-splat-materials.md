# Agent Note: Splat-map multi-layer PBR terrain materials in world3d and render3d

Status: implemented

## Problem

`game/world3d` heightmaps (`Heightmap` and [Terrain LOD](./2026-11-25-world3d-terrain-lod.md)) previously had no material layer representation: terrain rendered as a single uniform material, and gameplay systems could not query which surface material (grass, dirt, rock, snow) was underfoot at world `(x, z)` for footsteps, particles, or movement traction. Outdoor 3D terrain needs:

1. Up to 8 PBR material layers blended by 1–2 RGBA weight (splat) maps with per-layer UV tiling, optional height-based contrast sharpening, and optional triplanar projection on steep cliffs.
2. Full integration with `render3d`'s existing lighting (`LightGroup`), directional shadows (`ShadowGroup`), cascaded shadows (`CSMGroup`), fog (`FogGroup`), and HDR linear output across macOS, Windows, Linux, Android, iOS, and WebGL 2 / WebGPU.
3. A plain-text versioned YAML terrain definition with strict unknown-field refusal, deterministic canonical output, and a published JSON Schema.
4. Zero-allocation `LayerAt(x, z)` queries on the CPU that agree with GPU splat sampling and height-based weight blending without coupling `world3d` to GPU packages.

## Decision

Split the capability cleanly along the existing package boundary between pure-CPU world data (`packages/game/world3d`) and GPU rendering (`packages/game/render3d`):

- **`world3d` CPU splat maps and YAML format** (`terrain_splat.go`, `terrain_format.go`, `terrain.go`):
  - `TerrainSplatMap` stores 1–2 RGBA weight grids (`splat0` for layers 0..3, `splat1` for layers 4..7) plus 1–8 `TerrainLayerInfo` descriptors (name, UV scale, height bias, optional `TerrainHeightLayer`). `SampleUV`, `TerrainSplatMap.LayerAt`, and `Heightmap.LayerAt(x, z)` use texel-center bilinear filtering with edge clamping, matching GPU linear clamp-to-edge sampling, and return a stack-allocated `TerrainLayerSample` (dominant index, name, normalized `[MaxTerrainLayers]float32` weights, active count).
  - `BlendTerrainWeights` implements an algebraic height-based blending formula shared identically with the WGSL shader: normalize raw splat weights `nw_i`, compute `score_i = nw_i * (0.5 + clamp(h_i, 0, 1))`, threshold at `max(score) * clamp(contrast, 0, 0.98)`, clamp `max(score_i - threshold, 0)`, and renormalize. Zero-weight layers remain strictly zero and never bleed through even at height 1.0, and all-zero splats fall back to layer 0.
  - `TerrainDoc` (`TerrainFormatVersion = 1`, `TerrainSchemaURL = https://game.nsgo.dev/schemas/terrain.v1.json`) defines `ParseTerrain`, `ValidateTerrain`, and `EncodeTerrain` with strict `KnownFields(true)` decoding, `*loaderr.Error` diagnostics, `formattest.Run` conformance (`world3d#terrain`), and `cmd/gameschema` JSON Schema generation.
- **`render3d` GPU splat terrain material** (`terrain_material.go`):
  - `GPUTerrainMaterial` binds at `TerrainMaterialGroup = PBRDrawGroup` (group 2), preserving `CameraGroup` (0), `LightGroup` (1), and leaving group 3 free for `ShadowGroup`, `CSMGroup`, or `FogGroup`.
  - Per-layer maps are packed into three `gpu.Dim2DArray` textures (`albedo` sRGB, `normal` linear tangent-space, and `mrh` linear with R=height, G=roughness, B=metallic) plus two 2D splat textures and two samplers. This keeps fragment texture bindings at 5 (6 with shadows), well below WebGL 2 / GLES 3's 16-texture limit, with zero pipeline permutations across 1–8 layers.
  - All WGSL shaders (`TerrainMaterialWGSL`, `TerrainMaterialShadowWGSL`, `TerrainMaterialCSMWGSL`, `TerrainMaterialFogWGSL`, and their `LinearOutput` variants) declare every helper before its callers (WGSL -> MSL requirement), keep every `textureSample` call inside uniform control flow (gating height-blending and triplanar sampling only on uniform flags and constant-bound loops up to uniform `layer_count`), use Whiteout-style triplanar normal blending so flat normal maps preserve `geom_n` exactly, and avoid `exp`/`log` builtins.
  - `EvaluateTerrainMaterial` provides an exact pure-Go CPU reference matching the WGSL shading math.

## Alternatives considered

- **Binding individual 2D textures per layer (up to 8 × 3 = 24 textures)**: Simpler upload without resampling mixed-size layer textures to a common array resolution. Rejected because 24+ fragment texture bindings exceed WebGL 2 and GLES 3's guaranteed 16 `MAX_TEXTURE_IMAGE_UNITS` minimum and would force multi-pass terrain rendering or drop layers on mobile and web. Packing into three `gpu.Dim2DArray` textures (with automatic bilinear resampling when layer image sizes differ) supports all 8 layers in a single pass on every backend.
- **Additive `score_i = h_i + nw_i` height blending**: Common in tutorials, but a tiny non-zero splat weight (e.g., `nw_1 = 0.001`) paired with `h_1 = 1.0` produces a higher score than `nw_0 = 0.999` with `h_0 = 0.0`, causing distant layers to pop in at weight edges. Multiplying by `nw_i` (`nw_i * (0.5 + h_i)`) with a relative threshold (`maxScore * contrast`) is continuous at `nw_i -> 0+`, keeps zero-weight layers strictly zero, and never divides by zero.
- **Putting GPU rendering inside `world3d` or having `world3d` import `render3d`**: Would give one-stop terrain construction, but would pull `nsgo/gpu` and shaders into headless server and gameplay-only packages that only need `Heightmap`, `LayerAt`, and `TerrainDoc`. Keeping `world3d` CPU-only and matching `TerrainMesh` fields to `render3d.Mesh` preserves clean layering.

## Consequences

- **Benefits**:
  - Single-pass rendering of up to 8 PBR terrain layers with height blending, triplanar cliff projection, normal mapping, directional/cascaded shadows, fog, and HDR output across all 6 target platforms (macOS, Windows, Linux, Android, iOS, Web) and the software renderer.
  - `Heightmap.LayerAt(x, z)` is zero-allocation and matches GPU splat interpolation and height-based weight sharpening.
  - `*.terrain.yaml` files are human- and AI-editable with published JSON Schema validation (`apps/website/public/schemas/terrain.v1.json`) and automated `formattest` conformance.
- **Costs**:
  - All layers within one `GPUTerrainMaterial` share a single 2D array resolution (`LayerTextureSize`, inferred from the first non-nil layer image when 0); mixed-resolution source images are resampled on upload.
  - When `Triplanar.Enabled` is true, the shader samples all three projection axes in uniform control flow for every active layer (even on flat ground) to satisfy WebGPU/WGSL uniform control-flow rules; callers that do not need cliff projection leave `Triplanar.Enabled` false for minimal texture bandwidth.
- **Verification**:
  - `packages/game/world3d/terrain_splat_test.go` and `conformance_test.go` verify `LayerAt(x, z)` across corners, bilinear midpoints, out-of-bounds clamping, 4- and 8-layer splat maps, height-weighted blending, and YAML round-trip / unknown-field / schema conformance.
  - `packages/game/render3d/terrain_material_test.go` verifies WGSL compilation and no-forward-call ordering across all shader variants, CPU-vs-GPU pixel parity across all 8 layers, height-blend sharpening, triplanar cliff projection, directional shadow darkening, CSM/Fog/LinearOutput pipelines, `UpdateUniforms`/`UpdateSplatMaps`, `gpu.View` rendering, and the 4-layer shadowed golden image (`testdata/golden/terrain_four_layer.png`).
