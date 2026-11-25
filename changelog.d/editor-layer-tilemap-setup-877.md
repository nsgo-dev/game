### Added

- `game/sceneformat`, `game/editor`: add undoable layer tilemap setup in the Layer Inspector (add an inline tilemap with tileset atlas, tile size and grid size; move between inline and top-level `tilemaps` via `ref`; resize from any edge while preserving painted tile world positions via version 3 `offsetX`/`offsetY`; change tileset atlas or tile size; remove tilemap), refusing invalid sizes and tilesets missing frames for painted IDs (#877).
