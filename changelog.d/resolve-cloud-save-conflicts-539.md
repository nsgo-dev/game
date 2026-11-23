### Added

- `game/liveops`: `CloudSyncer` detects save slots changed both locally and in the cloud since the last sync and resolves them by policy (newest, keep both as a fork slot, or ask the player with a summary of each and apply the answer later with `Resolve`), with revisioned JSON cloud payloads, `save.ValidateSlot` for names from outside the game, and an `Unwrap` method that settings stores see through (#539).

### Fixed

- `game/save`: `StorageBackend.Slots` returns slot names that contain `.` or `%` as written, not as their escaped storage keys (#539).
