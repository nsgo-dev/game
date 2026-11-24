### Added

- `game/render3d`: clustered forward shading for hundreds of point and spot lights. `ClusteredLightSystem` takes a `GPULightBuffer`'s place at `LightGroup`, uploads the same `LightRig` and adds `ClusterLight`s (`ClusterLightFromPoint`, `ClusterLightFromSpot`) binned by `ClusterGrid`, shaded with the same PBR BRDF through `NewClusteredLitPBRPipeline` / `ClusteredLitWGSL` or `shade_pbr_clustered` in custom shaders. On WebGL 2 and OpenGL ES 3.0 it falls back to the 16 most important visible lights in a uniform buffer (#382).
