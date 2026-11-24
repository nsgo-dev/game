### Added

- `game/gltf`: add `MaterialLightmap`, `PrimitiveLightmapUVs`, `Document.LightmapTexCoords`, and `LightmapRef` to extract baked lightmap bindings (`MOZ_lightmap`, `EXT_lightmap`, `extras.lightmap`, `KHR_texture_transform`, and `TEXCOORD_1` with `TEXCOORD_0` fallback) (#258).
- `game/render3d`: add baked lightmap rendering (`LightmapVertex`, `LightmapMesh`, `GPULightmapMesh`, `UploadLightmapMesh`, `LightmapMaterial`, `UVTransform`, `LightmapRenderer`, `UploadLightmapRGBA16F`, and RGBM/RGBD HDR encode/decode helpers) supporting dual UV sets, shared lightmap atlases, linear/sRGB/RGBM/RGBD encodings, ambient fallback, and additive direct lighting (#258).
