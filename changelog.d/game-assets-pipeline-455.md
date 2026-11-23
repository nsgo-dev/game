### Added

- `game assets` builds every asset target `game.yaml` declares in one run: it packs atlases, encodes KTX2 textures, converts audio and regenerates the asset manifest, and keeps a YAML cache of each target's settings, inputs and outputs so a second run with nothing changed writes no file and changing one image repacks only its atlas; `-check` lists out-of-date targets for CI and `-force` rebuilds everything. `game/tooling/assetbuild` holds the versioned `game.yaml` schema and the builder, and `pack.ScanDir` lists the images `pack.PackDir` packs (#455).
