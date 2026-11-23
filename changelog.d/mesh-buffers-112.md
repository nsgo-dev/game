### Added

- `game/render3d`: `Mesh` (positions, normals, UVs, indices) uploaded by `UploadMesh` into the NSGo GPU view's vertex and index buffers as a scope-owned `GPUMesh`, with one shared interleaved `MeshVertexLayout`, per-mesh uint16/uint32 indices that avoid the WebGL 2 restart index, validation, allocation-free encoding and retained buffers that survive device loss (#112).
