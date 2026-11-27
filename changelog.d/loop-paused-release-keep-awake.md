### Fixed

- `game`: release `Loop`'s display keep-awake hold when `Paused` or when an `OnDemand` loop is idle (after optional `Loop.PausedAwake` grace period, default immediate), re-acquiring it on resume, `Step`, or `MarkDirty` (#1658).
