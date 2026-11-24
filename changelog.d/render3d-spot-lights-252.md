### Added

- `game/render3d`: physically based spot lights with inner and outer cones (`SpotLight`, `SpotLightSet`, `GPUSpotLights`, `SpotLightsWGSL`, `SpotLitWGSL`, `SpotLitRenderer`) following glTF `KHR_lights_punctual` attenuation and Cook-Torrance metallic-roughness PBR shading, with CPU reference evaluators (`EvaluateSpotLight`, `EvaluateSpotPBR`, `SpotToneMapSRGB`) and golden reference scene verification (#252).
