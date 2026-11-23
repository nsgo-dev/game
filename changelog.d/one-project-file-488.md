### Added

- `game/project`: `game.yaml`, the one project file holding a game's name, window and virtual resolution, input map, audio buses, autoloaded scenes and resources and per-platform export presets, kept in step with `nsgo.yaml`; the `gameproject` command (check, fmt, sync, targets), `editor.Editor.OpenProject`/`EditProject`/`SaveProject`, `sound.Mixer` default volumes, and the starter template and example games reading their settings from it (#488).
