### Added

- `game/editor`: watch the open scene, open animation clip, `game.yaml`, and project tree for external changes, automatically reloading clean files and refreshing the file browser while offering Compare (per-object Mine/Disk merge), Reload from disk, and Keep mine for dirty files (#906).
- `game/editor`: guard `Save`, `SaveFile`, `SaveProject`, and `SaveClipFile` against overwriting files modified on disk since open or last save (`ErrDiskConflict`, `ForceSave`, `ConfirmOverwriteSave`, and the overwrite confirmation dialog) (#906).

### Fixed

- `game/editor`: preserve open clip saved state and disk snapshots across undo/redo and `OnSaveClip` callbacks so clip dirty tracking clears accurately on save and undo (#906).
