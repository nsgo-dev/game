### Fixed

- `pkg/physics3d`: `World.Save` and `World.Restore` now preserve shared `*PhysicsMaterial` identity via a deduplicated snapshot material table (`SnapshotVersion` 2, still reading version 1 snapshots) so editing a shared material after in-place or fresh-world restore still reaches every referencing body, and `Body` now exposes `UserData any` (excluded from snapshots and checksums, preserved across in-place restores) (#1594).
