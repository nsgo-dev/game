### Changed

- `pkg/pathfind3d/navbake3d`: `BakeTile` visits only the triangles near the tile through a deterministic XZ grid index on `Geometry`, so rebaking one tile no longer scans the whole level (#1719).
