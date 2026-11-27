### Fixed

- `pkg/online/leaderboard/selfhost`: `FileStore` compaction now checks write, short-write, `Sync`, `Close`, and `Rename` errors, removes the temporary file on failure, and never replaces a period file with a truncated or unsynced copy (#1545).
