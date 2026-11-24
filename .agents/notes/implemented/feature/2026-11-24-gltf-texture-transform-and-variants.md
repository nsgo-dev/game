# Agent Note: glTF KHR_texture_transform and KHR_materials_variants validation and helpers

Status: implemented

## Problem

`packages/game/gltf` parsed basic `KHR_texture_transform` and `KHR_materials_variants` fields, but had three gaps relative to the Khronos specifications and runtime consumers:

1. **Silent acceptance of invalid extension payloads**: Unmarshaling `offset` and `scale` into fixed Go arrays (`*[2]float32`) silently accepted wrong-length JSON arrays (`[]`, `[0.5]`, `[1, 2, 3]`) and `null` elements; `KHR_materials_variants` accepted `null` extension payloads, empty `variants`/`mappings` arrays, variants missing required `name`, `null` variant indices (`[null]` decoding to `0`), and duplicate variant indices mapped multiple times on the same primitive. Top-level variant errors were also not wrapped as `*loaderr.Error`.
2. **Incomplete UV and variant runtime APIs**: `Primitive` pre-decoded only `TEXCOORD_0` and `TEXCOORD_1`, with no helper to evaluate transformed UVs for a `TextureRef` (including `KHR_texture_transform.texCoord` overrides targeting higher `TEXCOORD_n` sets) or to look up and resolve `KHR_materials_variants` by name and across meshes without mutating `Primitive.Material`.
3. **No Khronos reference sample verification**: Neither extension was verified against the official Khronos `glTF-Sample-Assets` test models (`TextureTransformTest`, `TextureTransformMultiTest`, `GlamVelvetSofa`).

## Decision

`packages/game/gltf` enforces strict Khronos JSON schema validation for both extensions at load time and adds non-mutating runtime evaluation helpers while preserving all existing exported types and methods:

- **Strict `KHR_texture_transform` parsing (`packages/game/gltf/gltf.go`)**: `parseTextureTransform` checks that the extension payload is a JSON object and decodes `offset`, `rotation`, `scale`, and `texCoord` from raw JSON fields (`[]*float32` for `offset`/`scale`) so `null` values, wrong array lengths, `null` elements, non-finite numbers, and negative or fractional `texCoord` values are rejected with `*loaderr.Error` pointing at `materials[i]`. `TextureTransform` remains a comparable value struct (no slice fields) with `DefaultTextureTransform()`, `IsIdentity()`, `Apply(math3d.Vec2)`, `ApplyUV(u, v)`, and `Matrix()` in exact Khronos `T * R * S` order.
- **On-demand UV evaluation (`packages/game/gltf/material.go`)**: `Document.PrimitiveTexCoords(prim, texCoord)` returns `TexCoords0`/`TexCoords1` directly and decodes `TEXCOORD_2+` on demand from `Primitive.Attributes` with finiteness checks. `Document.TransformTexCoords(prim, ref, out)` selects `ref.EffectiveTexCoord()` and evaluates transformed UVs into a caller-reusable slice (mirroring `EvaluateMorphPositions`).
- **Strict `KHR_materials_variants` parsing (`packages/game/gltf/gltf.go`)**: `loadVariants` wraps errors with `loaderr.Errorf("extensions.KHR_materials_variants", ...)` and requires a JSON object with a non-empty `variants` array whose entries are JSON objects carrying a `name` string. `parsePrimitiveVariants` requires a non-empty `mappings` array where every mapping has a valid `material` index, a non-empty `variants` array with no `null` or out-of-range entries, and at most one mapping per variant index on that primitive.
- **Non-mutating variant resolution (`packages/game/gltf/material.go`)**: `Document.VariantIndex(name)`, `Document.MaterialForVariant(prim, name)`, `Document.MeshMaterials(meshIndex, variantIndex)`, and `Document.ResolveVariantMaterials(variantIndex)` resolve active materials without overwriting `Primitive.Material`, keeping variant switching and fallback to default (`variantIndex < 0`) repeatable.
- **Khronos reference fixtures (`packages/game/gltf/testdata/khronos/`)**: Commits unmodified `TextureTransformTest` (including `UV.png` and `Arrow.png` for direct pixel sampling checks), `TextureTransformMultiTest` (verifying `extensionsRequired` and all 5 PBR texture slots plus `KHR_materials_unlit` across `TEXCOORD_0`/`TEXCOORD_1`), and `GlamVelvetSofa` (verifying 5 material variants across mapped and unmapped meshes combined with required normal-map `KHR_texture_transform`).

## Alternatives considered

### Mutating `Primitive.Material` in place when switching variants

An `ApplyVariant(variantIndex int)` method that overwrites `Primitive.Material` directly on `Document.Meshes` would simplify callers that only read `prim.Material`. It was rejected because overwriting `prim.Material` destroys the primitive's authored default material unless a hidden backup copy is stored on `Primitive`, breaking callers that inspect or reset `prim.Material` directly. Non-mutating lookup (`MaterialForPrimitive`, `MaterialForVariant`, `MeshMaterials`, `ResolveVariantMaterials`) keeps `Primitive` transparent and makes switching between variants and defaults lossless.

### Pre-decoding arbitrary `TEXCOORD_n` slices into a `[][]math3d.Vec2` field on `Primitive`

Replacing or augmenting `TexCoords0`/`TexCoords1` with a slice-of-slices loaded for every `TEXCOORD_n` attribute would avoid on-demand decoding in `PrimitiveTexCoords`. It was rejected because almost all glTF assets use at most two UV sets, `TexCoords0` and `TexCoords1` are already part of the public `Primitive` API referenced by `render3d`, and `Primitive.Attributes` already stores every attribute's accessor index for zero-overhead on-demand decoding of `TEXCOORD_2+`.

## Consequences

- **Benefits**:
  - Malformed `KHR_texture_transform` and `KHR_materials_variants` files fail fast at `gltf.Load` with actionable `*loaderr.Error` field locations instead of silently producing zeroed or overwritten state.
  - CPU renderers, asset validators, and tests can evaluate exact transformed UVs for any texture slot or UV set without duplicating `T * R * S` matrix math or `TEXCOORD_n` attribute decoding.
  - All three official Khronos samples (`TextureTransformTest`, `TextureTransformMultiTest`, `GlamVelvetSofa`) are checked in CI alongside unit, negative-validation, and fuzz tests.
- **Costs & limits**:
  - Stricter validation rejects non-compliant glTF files that previously loaded (such as empty `variants: []` / `mappings: []` arrays or duplicate variant mappings on one primitive), matching the Khronos `gltf_validator`.
  - `TextureTransform` does not store `extras` so that it remains a comparable Go value type.
  - Committing the three Khronos sample fixtures adds ~320 KB under `packages/game/gltf/testdata/khronos/`.

## Verification

- `env -u GOROOT go tool nsgo test ./gltf/... .` runs `TestTextureTransformKhronosSample` (pixel-verified against `UV.png` quadrants and `Arrow.png` marker positions), `TestTextureTransformMultiKhronosSample` (all PBR slots across `TEXCOORD_0` and `TEXCOORD_1`), `TestMaterialsVariantsKhronosSample` (`GlamVelvetSofa`), `TestTextureTransformTexCoordOverrideAndHighSet`, `TestMaterialsVariantsRequiredAndMultiPrimitiveReference`, `TestTextureTransformRejectsMalformed`, `TestMaterialsVariantsRejectsMalformed`, runnable examples, and `FuzzLoad` seeds.
- `env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web` validates cross-platform compilation and vetting.
- `bun run verify-notes` validates this note's tree placement and format.
