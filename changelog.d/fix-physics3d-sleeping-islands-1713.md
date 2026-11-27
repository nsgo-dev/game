### Fixed

- `pkg/physics3d`: bodies now sleep and wake by contact/joint island instead of individually, and any contact from an awake dynamic body wakes the touched island before solving so slow pushes below 0.2 m/s conserve momentum instead of hitting an immovable sleeping body; added `World.DisableSleep`, `World.SleepLinearThreshold`, `World.SleepAngularThreshold`, `World.TimeToSleep`, default threshold constants, `Body.AllowSleep` / `SetAllowSleep`, and `SnapshotVersion` 3 (still reading v1 and v2 snapshots) (#1713).
