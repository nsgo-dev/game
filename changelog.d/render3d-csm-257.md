### Added

- `game/render3d`: cascaded shadow maps for the first directional light of a `LightRig` (`CascadedShadowMap`, `CSMConfig`, `ComputeCSMSplits`, `FitCascades`, `FitCascadesFromCamera`, `CSMWGSL`, `CSMWGSLForGroup`, `LitPBRCSMWGSL`, `NewLitPBRCSMPipeline`, `LitPBRPipelineOptions.CSMLayout`, and the CPU reference `EvaluateCSMShadow`), packing up to four texel-snapped cascades into one 2D depth atlas at bind group 3 with hard or 3x3 PCF filtering, cascade blending, a far fade and `Retained` device-loss support (#257).
