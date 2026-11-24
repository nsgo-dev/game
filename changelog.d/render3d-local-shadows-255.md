### Added

- `game/render3d`: perspective 2D spot-light shadow maps (`SpotShadowMap`, `SpotShadowConfig`, `SpotShadowLight`, `SpotShadowWGSL`, `NewSpotShadowLitPipeline`) and omnidirectional 3×2 depth-atlas point-light shadow maps (`PointShadowMap`, `PointShadowConfig`, `PointShadowLight`, `PointShadowWGSL`, `NewPointShadowLitPipeline`) with hard and 3×3 PCF filtering, normal/depth biasing, dynamic-offset multi-caster drawing, `Retained` device-loss restoration, and pure-Go CPU reference evaluators (`EvaluateSpotShadow`, `EvaluatePointShadow`) (#255).
