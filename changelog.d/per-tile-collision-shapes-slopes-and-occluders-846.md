### Added

- `game/tilemap`: `LoadTiled` reads the collision shapes Tiled's collision editor draws on tiles (rectangles, polygons, polylines, and ellipses approximated by `EllipseShapeSegments` points) into `Tileset.Shapes`; `TileShapes` returns a placed tile's shapes flipped and rotated by its GID's flip bits (`TileShape.Flipped`), `TileRevision` and `TileChangesSince` now also track `SetCellFlags`, `ClearCellFlags` and `SetFlags` (which asks for a full rebuild), and `Flags`, `CellFlags` and `TileBox` expose a tile ID's flags, a cell's override and a tile's size (#846).
- `game/tileshape`: a `View` of a tilemap's chosen layers that is a `physics.TileGrid` for `MoveAndSlide`, `CharacterBody`, the platformer controller and `World`, derives `physics.Slope` segments from 45 degree, 2:1, 22.5 degree and half tiles, merges solid cells and tile shapes into an `Outline`, static `Boxes` and `lights2d` `Occluders`, and rebuilds only the 16 by 16 chunks around each tile change it reads from `Tilemap.TileChangesSince` (#846).

### Fixed

- `game/physics`: `CharacterBody.Move` lifts a character onto a walkable slope before testing tile walls, so walking up a slope whose top meets a solid tile's top no longer stops at the tile's side for a step, and it snaps onto a tile top within `SnapDist` when the character walks off the lower end of a slope instead of leaving the ground for a step (#846).
