### Added

- `pkg/online/leaderboard`: add `ProviderMiniGame` (`miniGame` catalog entries with `key` and `order`, enforcing WeChat's 128-key and 128-byte limits), best-score-only user cloud storage submission, open data context `Show` / `ShowIn` / `Hide`, and `MiniGameHost` / `WithMiniGameHost` for WeChat and Douyin mini games (#1737).

### Changed

- `pkg/online/leaderboard`, `pkg/online/liveops`: `ProviderFor` and `CloudProviderFor` take a `*core.Env` instead of a `core.OS`; pass `cx.Env()` where you passed `cx.Env().OS()` (#1737).

### Fixed

- `pkg/online/leaderboard`, `pkg/online/liveops`: `Service.Provider`, `PlatformCloud.Provider` and `PlatformCloud.Available` no longer report Game Center, Play Games or iCloud when `env.Simulated()` is true, such as inside a mini program on iOS or a web build run with `--nsgo-os ios` (#1737).
