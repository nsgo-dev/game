### Fixed

- `game/tilemap`: fix `EncodeTiled` to clear or update stale `solid`, `one-way`, and `flags` tile properties when flags are cleared via `SetFlags`, refuse non-finite tile sizes and overflowing layer dimensions, and match `Tileset.TileCollisions` (`map[uint32]*ObjectLayer`) in documentation (#814).
- `game/project`: clone `IntGridFlags` and `IntGridValueFlags` in `(*Resolver).LDtkOptions` so callers cannot mutate or race on cached project rules (#814).
- `game/tileshape`: pre-merge chunk `shapePolys`, reject non-overlapping polygon pairs by AABB in `mergeConvexPolys`, and wrap intermediate float products in `float64(...)` to avoid arm64 FMA drift (#814).
