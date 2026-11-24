### Added

- `game/loaderr`: a shared `Error` for files a loader cannot use, naming the file, line, column, field, what was expected and a fix, with `Locate` to find a field's line in YAML or JSON and to turn decode errors into it; a guide in `docs/en/errors.md` (#503).
- `game/save`: `Store.LoadChecked` checks a decoded value while the slot loads, falling back to the backup copy when the check refuses it (decoded into the value the destination held before the call, not over the refused copy) and reporting the check's field at its line; load errors name a `FileBackend` copy by its file path and a `StorageBackend` copy by its storage key (#503).

### Changed

- Every loader (`sceneformat`, `particles`, `skeletal`, `anim3d`, `sprite`, `tilemap`, `l10n`, `input`, `modding`, `replay`, `save`, `entities`, `assets`, `world3d`, `gltf`, `text`, `flow`, `editor`, `debug`, `tooling/datatable`, `tooling/devstate`, `tooling/bench`, `cmd/sizecheck`) returns `*loaderr.Error` values that name the file, line and field and give a fix; messages now start with `file:line:column:` instead of a package prefix. Sentinels such as `save.ErrCorrupt` and `modding.ErrInvalidManifest` and `datatable.CellError` still match with `errors.Is` and `errors.As` (#503).
- `game/entities`: `LoadWorld` checks the snapshot while the slot decodes, so a snapshot it cannot restore falls back to the backup copy (#503).
