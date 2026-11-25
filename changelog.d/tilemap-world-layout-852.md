### Added

- `game/tilemap`: add `Tilemap.Origin`, LDtk world positions and `Neighbours` (`LevelNeighbour`, `LoadLDtkWorld`), Tiled `.world` loader (`ParseTiledWorld`, `LoadTiledWorld`), and region-based multi-level streamer `WorldStream` (#852).
- `game/physics`: add optional `TileGridOrigin` interface and `Grid.OriginX`/`Grid.OriginY` honored by `MoveAndSlide`, `CharacterBody.Move`, `World` queries, `SoftWorld`, and debug drawing (#852).
- `game/fog`: add `Fog.Origin` copied by `NewFromTilemap` and honored by `CellAt`, `Draw`, and JSON snapshots (#852).
