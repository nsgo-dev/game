### Added

- `game/tilemap`, `game/world3d`: add sparse power-of-two chunked tile layers with negative coordinates, per-chunk static caches and dirty tracking, deterministic YAML/JSON chunk persistence, bounded 2D streaming (`tilemap.ChunkStream` and `world3d.TilemapStreamer` on `world3d.ChunkStreamer`), and direct sparse loading of infinite Tiled `.tmj` maps (#853).
