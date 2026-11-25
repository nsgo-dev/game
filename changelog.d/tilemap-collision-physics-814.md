### Added

- `game/tilemap`: `*Tilemap` and `*WorldStream` now satisfy `physics.TileGrid` and `physics.TileGridOrigin` directly via `TileSize`, `GridSize`, `GridOrigin`, `TileFlagsAt`, and `TileFlags.PhysicsFlags` (#814).
- `game/tilemap`: preserve each Tiled tile's collision object group in `Tileset.TileCollisions` (`Tilemap.TileCollisionGroup`) and add deterministic `EncodeTiled` for `.tmj` round-tripping with per-tile collision data (#814).
- `game/tilemap`, `game/project`, `game/sceneformat`: add `LDtkOptions`, `LoadLDtkWith`, `LoadLDtkAllWith`, `LoadLDtkWorldWith`, LDtk `intGridValuesGroups` support, and `physics.ldtkIntGrid` rules in `game.yaml` (`File.LDtkOptions` and `project.Resolver`) to map LDtk IntGrid values to collision flags from project settings (#814).
- `game/tileshape`: merge adjacent partial rectangular tiles in `View.Boxes` and add `View.StaticShapes` and `View.AppendStaticShapesIn` to generate merged `Box`, convex `Polygon`, and closed `Chain` static physics shapes for partial tiles (#814).

### Changed

- `examples/caverun`: pass `*tilemap.Tilemap` directly to `physics.MoveAndSlide`, dropping its manual `tileGridAdapter` (#814).
