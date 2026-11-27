### Fixed

- `game/ai`, `game/ai/navbake`: `ai.TiledNavMesh` now supports off-mesh links (`AddOffMeshLink`, `OffMeshLinks`, `ClearOffMeshLinks`, `TiledNavPathResult.Links`) that automatically re-attach across tile replacement and dynamic obstacle carving, and `navbake.Mesh.TiledNavMesh` registers all stored baked `Links` instead of dropping them (#1587).
