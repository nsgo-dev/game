### Fixed

- `game`, `pkg/input`: add `Loop.Idle` so unpaused `OnDemand` loops skip updates and draws while idle (and release display keep-awake after `PausedAwake`), have `MarkDirty` queue one wake step, add `Actions.Wake` / `TouchControls.OnWake` / `GestureRecognizer.Wake` input wake hooks, and document `OnDemand`, `Idle`, `MarkDirty`, `DrawRate`, `TimeScale`, and `Step` (#1499).
