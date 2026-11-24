### Added

- `game/render3d`: directional light shadow mapping (`DirectionalShadowConfig`, `ShadowMapOptions`, `DirectionalShadowMap`, `ShadowUniforms`, `AppendShadowUniforms`, `ShadowDepthWGSL`, `ShadowWGSL`, `ShadowWGSLForGroup`, `ShadowLitWGSL`, `NewShadowLitPipeline`, and CPU reference `EvaluateDirectionalShadow`) with `FitBounds` orthographic framing, texel snapping, hard 1-tap and 3x3 PCF comparison filtering, dynamic multi-caster shadow passes, and `Retained` device-loss recovery (#117).
