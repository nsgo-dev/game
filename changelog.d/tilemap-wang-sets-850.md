### Added

- `game/tilemap`: parse Tiled Wang sets (`wangsets` of type `corner`, `edge`, and `mixed`) and per-tile `probability` from embedded `.tmj` and external `.tsj` tilesets into `Tileset.WangSets` and `Tileset.TileProbabilities`, and add multi-terrain autotiling (`Tilemap.SetTerrain` and `Tilemap.SetTerrainRect`) with Tiled terrain-brush neighbor propagation, orthogonal and isometric support, dense and sparse chunked layer support, and deterministic probability-weighted variant selection via `*random.Stream` (#850).
