### Added

- `game/render3d`: feed baked lightmaps (`LightmapVertexLayout`, `LightmapEncoding`, atlas `UVTransform`, `MOZ_lightmap` / `EXT_lightmap` / `extras.lightmap`) into the lit metallic-roughness PBR material path via `PBRLightmapWGSL`, `NewPBRLightmapPipeline`, `GPUPBRLightmap`, `EvaluatePBRLightmap`, `LightmapMeshFromGLTF`, and `UploadGLTFPBRLightmaps` (#752).
