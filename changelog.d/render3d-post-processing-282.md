### Added

- `game/render3d`: composable 3D post-processing via offscreen `gpu.RenderTarget` passes (`PostStack3D`, `BloomPass`, `SSAOPass`, `DepthOfFieldPass`) supporting HDR bright-extract + separable Gaussian Bloom, depth-reconstructed SSAO with bilateral blur, and depth-aware disc bokeh Depth of Field across full and compute-free (`WebGL 2` / `GLES 3.0`) NSGo GPU profiles (#282).
