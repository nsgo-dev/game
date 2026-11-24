### Added

- `game/render3d`: hardware MSAA offscreen render target management (`AAMode`, `ClampSampleCount`, `MSAATarget` with `Resize`, `ColorAttachment`, `DepthStencilAttachment`, `KeepDepth`, `OutputTexture`, and device-loss `Retained` support) and FXAA 3.11 full-screen post-processing (`FXAAOptions`, `FXAAPass`, `FXAAWGSL`, and CPU reference `ApplyFXAA`) compatible with both full and `NoGPUCompute` GPU profiles (#249).
