# Steamworks stats and leaderboards

`game/online/steam` provides the engine-facing boundary for Steam achievements, stats, leaderboards and rich presence. Inject a native `Driver` from the Steamworks adapter, call `Init`, pump `Tick` from the fixed loop, and use `Unlock`, `SetStatInt`, `SetStatFloat`, `StoreStats`, `UploadScore`, `Scores` and `SetPresence`. Every completion callback is posted to the engine scope. Tests can inject a fake driver and unsupported builds can return `ErrUnavailable`.
