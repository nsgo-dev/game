### Added

- `game/tilemap`: `LoadTiled` now loads Tiled XML maps (`.tmx`) and external XML tilesets (`.tsx`) alongside `.tmj`/`.tsj` (including cross-format references), decodes CSV, XML `<tile>`, and base64 tile data (uncompressed, `zlib`, and `gzip`) for both finite layers and infinite-map chunks with decompression-bomb protection, adds `TilesetLoader.LoadTileset` (falling back to `LoadJSON`), and refuses `zstd` compression with a structured `loaderr` fix hint (#848).
