### Added

- `game/tilemap`, `game/scene`: add per-layer tile Y-sorting (`TileLayer.YSort`, `SetLayerYSort`, `PropSortOriginY` / `SetTileSortOriginY`, `AppendYSortTiles`, `SortYSortTiles`, `YSortDrawer`, `Layer.AddTileLayer`) so tall tiles interleave with sprites across orthogonal, isometric, staggered, and hexagonal maps with camera culling, animated tiles, and zero steady-state allocations (#851).
