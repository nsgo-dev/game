### Added

- `game/render3d`: cascaded shadow maps (`CascadedShadowMap`, `CSMConfig`, `ComputeCSMSplits`, `FitCascades`, `FitCascadesFromCamera`, `CSMDepthWGSL`, `CSMWGSL`, `CSMLitWGSL`, `NewCSMLitPipeline`, and pure-Go CPU reference `EvaluateCSM` / `EvaluateCSMShadow`) packing up to 4 texel-snapped directional shadow cascades into a portable 2D depth atlas with 3x3 PCF, smooth cascade blending, far fade, and `Retained` device-loss support (#257).
