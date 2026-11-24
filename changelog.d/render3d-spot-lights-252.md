### Added

- `game/render3d`: spot lights with inner and outer cones in `LightRig` (`SpotLight`, `MaxSpotLights`), shaded by `shade_pbr` / `ShadePoint` together with the directional and point lights in the same pass and following glTF `KHR_lights_punctual` cone and range falloff (`SpotConeAttenuation`, `SpotLight.ConeScaleOffset`, `SpotLight.Attenuation`) (#252).

### Changed

- `game/render3d`: `LightUniformSize` grows from 672 to 1056 bytes to hold the spot lights, `LightUniforms` gains `SpotCount` and `Spot`, and the light block's `counts.z` now carries the spot light count (#252).
