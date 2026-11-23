### Added

- `game/scene3d` and `game/render3d`: cameras on the GPU: `Camera.Eye`, `SetViewport` and `Lerp` (for drawing between `game.Loop` steps), and `render3d.CameraBuffer` with `CameraUniform` and `CameraWGSL`, a camera uniform block at bind group 0 for 3D pipelines, checked against perspective and orthographic reference renders made headless on NSGo's software GPU device (#115).
