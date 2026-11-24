### Added

- `game/render3d`: directional, point, and ambient lights in glTF 2.0 metallic-roughness PBR shading (`DirectionalLight`, `PointLight` with `KHR_lights_punctual` smooth range windowing, `AmbientLight`, `LightRig`, `LightUniforms`, `GPULightBuffer` at bind group 1 with `Retained` device-loss recovery, `LightWGSL`, `LitPBRWGSL`, `PBRMaterial`, `PBRDrawBuffer`, `NewLitPBRPipeline`, and matching CPU reference evaluators `EvaluatePBR`, `ShadePoint`, `PointLightAttenuation`, and `LinearToSRGB`), verified pixel-for-pixel against CPU PBR math and checked-in reference renders (#116).
