### Added

- `game/render3d`: add cel/toon shading (`ToonMaterial`, `GPUToonMaterial`, `ToonLight`, `ToonWGSL`, `ToonOutlineWGSL`, `NewToonPipeline`, `NewToonOutlinePipeline`, `ToonRenderer`, `SmoothOutlineNormals`, `EvaluateToon`) with discrete diffuse bands or 1D cel-ramp textures, stepped specular and Fresnel rim highlights, and `CullFront` inverted-hull outlines, lit by a `DirectionalLight` key light and `AmbientLight` fill (`ToonLightFromRig`) and sharing `MaterialDefaults` fallbacks (#262).
