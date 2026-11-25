### Added

- `game/ai`: add 2D `TiledNavMesh` and `NavTile` with independent tile baking/add/replace/remove, cross-border polygon linking, runtime dynamic obstacles (`NavBoxObstacle`, `NavOrientedBoxObstacle`, `NavCircleObstacle`, `NavPolygonObstacle` in carve or mark modes) rebuilding only touched tiles, two-level spatial indexing, and per-tile path invalidation (`IsPathValid`, `WatchPath`, `AddTileListener`) (#826).
- `game/world3d`: add 3D `TiledNavMesh`, `NavTile`, multi-level-aware 3D dynamic obstacles (`NavBoxObstacle`, `NavOrientedBoxObstacle`, `NavCylinderObstacle`, `NavPolygonObstacle`), two-level 3D spatial indexing, per-tile path invalidation, and `NavTileStreamer` following `ChunkStreamer` to load and unload navmesh tiles with world chunks (#826).
- `game/navbake`, `game/navbake3d`: add `Mesh.TiledNavMesh` helpers and `navbake3d.BakeTile` for independent chunk-aligned voxel tile baking (#826).
