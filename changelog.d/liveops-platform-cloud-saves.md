### Added

- `game/liveops`: `PlatformCloud` (adapting NSGo's `cloud` package for iCloud on macOS/iOS, Google Play Games Saved Games on Android, and `nsgotest`'s in-memory store, with `CloudProviderFor`, `Available`, `WatchChanges`, `OnConflict`, and `ErrCloudConflict`), `DirCloud` (a `ConditionalCloudSync` for Steam Auto-Cloud and other directory-synced stores), and `CloudSyncer.SyncAsync` / `SyncSlotAsync` / `ResolveAsync` for non-blocking sync with engine-goroutine marshaling (#196).
