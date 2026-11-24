### Added

- `game/gltf`: parse and validate `KHR_materials_emissive_strength` (`Material.EmissiveStrength`, `Material.EffectiveEmissive`), `KHR_materials_clearcoat` (`Material.Clearcoat`), and `KHR_materials_transmission` (`Material.Transmission`), including `KHR_texture_transform` on extension textures and `extensionsRequired` support (#264).
- `game/render3d`: add `ExtendedPBRRenderer`, `PBRMaterialExt`, `PBRMaterialExtFromGLTF`, and `EvaluatePBRMaterialExt` implementing Khronos HDR emissive strength, dual-lobe Fresnel-attenuated clearcoat, and screen-space roughness-filtered transmission (#264).
