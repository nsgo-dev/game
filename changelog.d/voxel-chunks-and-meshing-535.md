### Added

- `game/voxel`: chunked 3D voxel worlds with greedy meshing to `render3d.Mesh` and per-material `SubMesh` ranges, cross-chunk neighbor culling and ambient occlusion, minimal dirty-chunk remeshing on edit, 3D DDA voxel raycasting, and deterministic versioned YAML saves with published JSON Schema `voxel.v1.json` (#535).
