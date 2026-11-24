### Added

- `game/render3d`: custom WGSL and Kage 3D shader materials (`CustomMaterial`, `CustomShaderDesc`, `ShaderParamDesc`, `ComputeUniformLayout`, `CompileCheck`) with deterministic std140 uniform packing, typed parameter setters, fallback 1×1 textures, `Retained` device-loss restoration, and cross-backend compilation verification (#263).
