### Fixed

- `online/liveops`, `save`: `CloudSyncer` checks save envelope versions via `CloudSyncOptions.SaveVersion` and `save.EnvelopeVersion`, keeping local saves intact with `OutcomeNeedsUpdate` when a cloud copy requires a newer build, never resolving mixed-version conflicts in favour of the older envelope version, and exposing both envelope versions in `Conflict` (#1538).
