### Added

- `game/tilemap`: added optional `default` local tile ID on `TerrainDoc` (regenerated `tileset.v1.json`), `TilesetDoc.Terrain`, `TilesetDoc.Clone`, `Tilemap.ApplyTerrain`, `Tilemap.SetTerrainTile`, `AutotileSet.ComputeMask`, and mask helpers (`ValidTerrainMask`, `ToggleTerrainMask`, `TerrainMaskNeighbors`, `TerrainNeighborsMask`) (#879).
- `game/editor`: added the `ToolTerrain` (`t`) autotile brush (`TileStrokeTerrain`, `TileStrokeEraseTerrain`, `PaintTerrain`, `EraseTerrain`) that places or erases terrain and updates neighboring cells through the same rules as `tilemap.Tilemap.SetAutotile` in one undoable step per stroke (#879).
- `game/editor`: added the docked Tileset Editor (`TilesetEditorPanelView`, `OpenActiveLayerTileset`, `OpenTilesetFile`, `AddTilesetTerrain`, `ToggleTilesetEdgeOrCorner`) to define 4-bit cardinal and 8-bit blob terrain sets, set defaults, and assign tiles to neighbor masks by clicking their edges and corners (#879).
