### Added

- `game/scene3d`: box-projected 3D decals (`Decal`, `NewDecal`, `NewDecalOnSurface`, `DecalReceiver`, `DecalReceiverMesh`, `ProjectDecal`, `ProjectDecalOntoReceiver`), a fixed-capacity zero-allocation `DecalPool` with deterministic priority/FIFO eviction and `FadeIn`/`FadeOut` lifetime aging, and a mobile forward-path `GPUDecalRenderer` (`DecalWGSL`, `DecalVertexLayout`) supporting normal-angle fade, depth fade, layer masks, and alpha-blended albedo textures across WebGPU, Metal, Vulkan, OpenGL ES 3.0, and WebGL 2 (#385).
