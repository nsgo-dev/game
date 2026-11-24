### Added

- `game/render3d`: map glTF 2.0 materials, textures, images, and samplers onto `render3d`'s metallic-roughness PBR model via `UploadGLTFMaterials`, `GLTFMaterialSet`, `GLTFMaterialInfo`, `GLTFTextureSlot`, `SamplerOptionsFromGLTF`, and `AlphaModeFromGLTF` with automatic sRGB/linear color-space separation, `(imageIndex, sRGB)` texture deduplication, `KHR_materials_variants` / `KHR_materials_emissive_strength` / `KHR_materials_unlit` / `KHR_texture_transform` support, `Retained` device-loss restore, and Khronos `DamagedHelmet` reference verification (#119).
