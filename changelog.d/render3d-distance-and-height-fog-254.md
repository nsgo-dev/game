### Added

- `game/render3d`: distance (`FogOff`, `FogLinear`, `FogExp`, `FogExp2`) and analytical exponential height fog (`Fog`, `Fog.Factor`, `Fog.Apply`, `FogCameraPos`), allocation-free `FogUniforms`/`AppendFogUniforms`, a scope-owned `GPUFogBuffer` that survives device loss when retained, fogged lit pipelines (`NewLitPBRFogPipeline` with the buffer at `FogGroup`, and `NewMaterialPipeline` with `MaterialPipelineOptions.FogLayout` at `MaterialFogGroup`), and the `FogWGSL`, `FogBindingWGSL`, `MaterialFogBindingWGSL` and `FogCameraWGSL` shader snippets (#254).
