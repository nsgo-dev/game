### Added

- `game check` subcommand and `game/tooling/assetcheck` package: check the scenes, Tiled and LDtk maps and sprite atlases under a directory and the references between them (tilemap sources, external tilesets, images, atlases and their frames, materials and their textures), reporting each problem as `file:line: message`; the Examples workflow runs it on the sample games (#456).
- `sceneformat.FieldError`: every error `sceneformat.Validate` returns names the field at fault by its path in the document, and `Validate` reports the entries of `tilemaps` in key order (#456).
