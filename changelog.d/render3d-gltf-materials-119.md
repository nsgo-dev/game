### Added

- `game/render3d`: map glTF 2.0 materials, textures, images, and samplers onto `render3d`'s metallic-roughness PBR model via `UploadGLTFMaterials`, `GLTFMaterialSet`, `GLTFMaterialInfo`, `GLTFTextureSlot`, `SamplerOptionsFromGLTF`, and `AlphaModeFromGLTF` with automatic sRGB/linear color-space separation, `(imageIndex, sRGB)` texture deduplication, glTF default samplers, `KHR_materials_variants` and `KHR_materials_emissive_strength`, `Retained` device-loss restore, and a check of the Khronos `DamagedHelmet` render against its own textures (#119). `MaterialWGSL` does not apply `KHR_materials_unlit`, `KHR_texture_transform` or UV sets other than `TEXCOORD_0`; `GLTFMaterialInfo.Unsupported()` and `GLTFTextureSlot.NeedsUVBake()` report materials that use them.
- `game/gltf`: `Material.EmissiveStrength()` reads `KHR_materials_emissive_strength`, which a file may now list in `extensionsRequired` (#119).

### Changed

- `game/render3d`: `Material.Validate` accepts a negative `NormalScale`, which glTF 2.0 allows and which flips the normal map's X and Y (#119).
