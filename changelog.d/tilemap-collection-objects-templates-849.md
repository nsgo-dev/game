### Added

- `game/tilemap`: support Tiled image-collection tilesets (`.tmj`/`.tsj` and `.tmx`/`.tsx`, including optional tile sub-rectangles), drawing tile objects (`DrawObjectLayer` and interleaved `Draw` order with flips, rotation, opacity, parallax, camera culling, `tileoffset`, and `objectalignment`), external `.tx`/`.tj` object templates via `TilesetLoader.LoadTemplate` with property merging and GID remapping, text objects (`ObjectText` / `TextObject`), and layer `tintcolor` (#849).
