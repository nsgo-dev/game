### Fixed

- `pkg/entities`: `World.Restore` (including smaller and empty snapshots) and `World.Reset` now preserve bumped slot generations and rollback generation floors so stale `Entity` handles never alias newly spawned entities (#1578).
