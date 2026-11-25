### Added

- `game/tooling/importsettings`, `game/tooling/pack`, `game/tooling/assetbuild`, `game/cmd/game`: add `image.pivot` (`default: [x, y]` and per-cell `cells` overrides) to `*.import.yaml` sidecars, regenerate `import.v1.json`, and write each frame's normalized pivot into packed TexturePacker atlas JSON in `game assets` and `game pack` for `sprite.LoadAtlas` (#880).
- `game/editor`: add an interactive sprite-sheet slice-grid preview canvas with draggable default and per-cell pivot markers, quick pivot presets, and undoable per-cell pivot editing to the image import panel (#880).
