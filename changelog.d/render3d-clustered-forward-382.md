### Added

- `game/render3d`: clustered forward shading (`ClusterGrid`, `ClusteredLightSystem`, `ClusterLight`, `ClusterConfig`, `SelectFallbackLights`, `ClusteredLitWGSL`, `ClusteredFallbackWGSL`) supporting hundreds of point and spot lights on desktop storage-buffer devices with automatic prioritized uniform-buffer fallback on WebGL 2 / OpenGL ES 3.0 targets without compute or storage buffers (#382).
