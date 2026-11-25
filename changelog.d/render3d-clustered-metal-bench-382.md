### Fixed

- `render3d`: reordered `LightWGSL` helper declarations so Apple Metal MSL compiles without forward declarations, removed the unused compute-shader check from `DeviceSupportsClusteredLighting` so Apple Metal uses the full clustered storage-buffer path, and recorded desktop Metal 4 frame-rate measurements for 256 clustered point lights (#382).
