### Added

- `game assets`: `fonts` targets bake a TrueType or OpenType font into an AngelCode BMFont (a `.fnt` file and its PNG pages, with the font's kerning) at a `size`, with the `chars` and `padding` they name, and `models` targets pack a glTF model, its buffers and the images it names into one GLB file, optionally `scale`d and without its `animations`. Both are declared in `game.yaml` (`project.FontTarget`, `project.ModelTarget`) (#487).
- `game/tooling/importsettings`: `font` and `model` sections, which a font's or a model's sidecar holds and which override its target's settings, so editing one rebuilds only that font or model; `File.Sections` lists the sections a sidecar holds (#487).
- `game/render3d`: `LoadKTX2` uploads a KTX2 texture as `game assets` and `game tex` write it, compressed where the device supports its format and decoded to RGBA8 elsewhere, and returns its sampler with the filters the image's sidecar set through the `game.filter` key; `KTX2Sampler` applies that filter to sampler options (#487).

### Changed

- `game tex` and `game audio` read the import settings sidecar beside their input, as `game assets` does, under the flags given, which win; `game tex` writes the sidecar's `filter` and `game audio` its loop points (#487).
- `game/tooling/importsettings`: a sidecar holds at most one of the `image`, `audio`, `font` and `model` sections, and the error for more than one names them (#487).
