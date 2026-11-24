### Added

- `game/tooling/importsettings`: an import settings sidecar may hold a `credit` section, beside an `image` or `audio` section or on its own beside any asset, such as a font or a model, recording the asset's `author`, its `license` as an SPDX license expression (checked by `ValidateLicense`), its `source` and a `notice` its license asks to be shipped. The published `import.v1.json` schema describes it (#513).
- `game/tooling/credits`: collects the credit of every asset `game.yaml`'s `assets` section reaches (the images each atlas packs, each texture and audio input, and every file the manifest lists, leaving out what the targets write) and writes them as a deterministic Markdown credits file, `CREDITS.md`, with a table of authors, licenses and sources, a count per license and every notice (#513).
- `game release` writes `CREDITS.md` beside the packages when `game.yaml` has an `assets` section, and refuses to build while an asset has no credit; `game credits` writes or prints the same file without building, failing on an uncredited asset (#513).
- `game/project`: `Assets.IsOutput` reports whether a target writes a file (#513).

### Changed

- `game assets` accepts a sidecar that holds only a credit, beside an image or a sound a target reads or beside any file in a folder a target reads from (#513).
