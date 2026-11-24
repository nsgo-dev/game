### Added

- `game/render3d`: custom WGSL and Kage 3D shader materials (`CustomMaterial`, `CustomShaderDesc`, `ShaderParamDesc`, `ComputeUniformLayout`, `CompileCheck`) with deterministic std140 uniform packing, typed parameter setters, fallback 1×1 textures, `Retained` device-loss restoration, and cross-backend compilation verification. `DrawLayout` and `ExtraLayout` give a custom material a per-draw model matrix and lights, and `LightWGSLForGroup` declares the scene lights in another bind group (#263).
