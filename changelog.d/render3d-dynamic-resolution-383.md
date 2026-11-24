### Added

- `game/render3d`: dynamic resolution scaling (`DynamicResolutionScaler`, `DynamicResolutionTarget`, `DynamicResolutionConfig`) that adapts 3D render scale from GPU frame time or pass timings with hysteresis and upscales a sub-viewport offscreen target onto the native surface while keeping HUD overlays at native resolution (#383).
