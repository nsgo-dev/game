### Added

- `game/gltf`: parse and validate `KHR_materials_clearcoat` (`Material.Clearcoat`) and `KHR_materials_transmission` (`Material.Transmission`), including `texCoord`, normal `scale`, and `KHR_texture_transform` on all extension texture references, and accept both extensions in `extensionsRequired` (#264).
- `game/render3d`: extend `Material`, `PBRMaterial`, `EvaluateMaterial`, `EvaluatePBR`, and `ShadePoint` with clearcoat, transmission, and IOR support; add `ExtendedMaterialDefaults`, `GPUExtendedMaterial`, per-frame `TransmissionBackground` (with `DynamicResolutionTarget` UV scaling), `NewExtendedMaterialPipeline`, and `UploadGLTFExtendedMaterials` for glTF emissive strength, clearcoat, and transmission rendering (#264).
