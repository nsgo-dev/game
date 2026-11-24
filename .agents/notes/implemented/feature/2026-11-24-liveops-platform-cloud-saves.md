# Agent Note: Platform cloud saves in `liveops`

Status: implemented

## Problem

`liveops` already defines the provider-agnostic `CloudSync` and `ConditionalCloudSync` interfaces, `CloudSyncer` conflict detection and resolution, and `CloudClient` for the self-hosted `cloudsave` service. Games targeting Apple platforms (iCloud), Android (Google Play Games Saved Games), and Steam (Steam Auto-Cloud) had no built-in `CloudSync` adapters and had to bridge NSGo's asynchronous `cloud` package or desktop synced directories themselves.

Bridging NSGo's `cloud` package into `CloudSync` also hits two non-obvious constraints:

1. **Engine-goroutine deadlock vs. reactive storage race.** On real Apple and Android targets (`cx.CloudStore() == nil`), `cloud.ReadFile` / `WriteFile` / `DeleteFile` / `ListFiles` run native I/O off-thread and deliver callbacks through `cx.Post` on the engine goroutine. Blocking `CloudSync` methods called on the engine goroutine would therefore deadlock waiting for `cx.Post`, while calling `CloudSyncer.Sync` blindly on a worker goroutine would touch `core.Storage` reactive signals off the engine goroutine.
2. **Cross-service naming rules.** Google Play Games `SnapshotsClient.open` rejects `/` in snapshot names, and Apple's `cloud_apple.go` `platformListFiles` ignores dotfiles (names beginning with `.`). A naive `"saves/<slot>.json"` path fails on Android, and a dot-prefixed name disappears from iCloud listings.

## Decision

`packages/game/liveops` provides two platform `CloudSync` adapters plus async sync entry points on `CloudSyncer`:

- **`PlatformCloud` (`cloud_platform.go`)** adapts `github.com/nsgo-dev/nsgo/cloud` behind `CloudSync` for iCloud (macOS/iOS/tvOS), Google Play Games Saved Games (Android), and `nsgotest`'s isolated per-app `core.CloudStore`.
  - Default file naming is flat: `DefaultCloudPrefix` (`"save."`) + `<slot>` + `".json"` (e.g. `save.main.json`). Prefixes starting with `.` or `/` or containing `..`, `\`, `//`, or control characters are rejected; callers targeting Android must keep prefixes free of `/`.
  - When `cx.CloudStore() != nil` (`nsgotest` or a custom `core.CloudStore`), `Load`, `Save`, `Delete`, and `List` call the thread-safe store directly so synchronous `CloudSyncer.Sync` calls in tests never deadlock waiting for `app.Settle()`.
  - When `cx.CloudStore() == nil` and `CloudProviderFor(os) == CloudProviderNone` (Windows, Linux, web, or a bare test root), operations immediately return an error wrapping `ErrCloudUnavailable` and `cloud.ErrUnavailable` without posting to the engine goroutine or silently dropping writes.
  - When `cx.CloudStore() == nil` on Apple or Android, operations invoke `cloud.ReadFile` / `WriteFile` / `DeleteFile` / `ListFiles` and wait on callback delivery, `ctx.Done()`, or `cx.Context().Done()` (captured at construction so scope disposal unblocks waiters cleanly without touching reactive signals off-thread).
  - Sentinel errors wrap both `liveops` and `cloud` sentinels: `ErrCloudUnavailable` (`cloud.ErrUnavailable`), `ErrSlotNotFound` (`cloud.ErrNotFound`), and `ErrCloudConflict` (`cloud.ErrConflict` / `*cloud.ConflictError`).
  - `WatchChanges` and `OnConflict` wrap `cloud.WatchChanges` and `cloud.OnConflict`, translating matching file names back into validated slot names and filtering out unrelated container files and KV-only changes (while preserving `cloud.ChangeAccount` and container-wide initial-sync notifications).
  - `Available()` reads `cx.Env().CanUseCloudStorage()` reactively; `Provider()` returns `CloudProviderFor(os)`.
- **`DirCloud` (`cloud_dir.go`)** implements `ConditionalCloudSync` over a directory on disk (`<slot>.json`) using atomic temp-file writes, fsync, directory sync, stale `.cloud-*.tmp` sweeping, and Unicode NFC case-folding collision checks. Desktop games on Steam can either point Steam Auto-Cloud directly at `save.FileBackend(cx.DataDir())` or use `DirCloud` on a synced subdirectory (such as `filepath.Join(cx.DataDir(), "cloud")`) to share the exact `CloudSync` + `CloudSyncer` conflict pipeline across every storefront.
- **`CloudSyncer.SyncAsync`, `SyncSlotAsync`, and `ResolveAsync` (`cloudsync.go`)** run cloud I/O on a worker goroutine while marshaling all `Local`, `State`, `Summarize`, `Now`, and `Ask` operations onto `cx`'s engine goroutine through `cx.Post`. Concurrent async calls serialize through a 1-buffered semaphore respecting `ctx.Done()` and `cx.Context().Done()`. Before overwriting a local slot during an async sync, `applyCloud` re-checks the local content hash on the engine goroutine and returns `ErrConflictChanged` if the player saved a newer local copy while the cloud read was in flight.

## Platform and threading contract

- **Synchronous `CloudSyncer.Sync` / `SyncSlot` / `Resolve`**: safe on the engine goroutine with `DirCloud`, `nsgotest` `PlatformCloud` (`cx.CloudStore() != nil`), and in-memory test doubles; must never be called on the engine goroutine with `PlatformCloud` on real Apple or Android devices.
- **Asynchronous `CloudSyncer.SyncAsync` / `SyncSlotAsync` / `ResolveAsync`**: required for `PlatformCloud` on real Apple and Android devices and recommended for `CloudClient` over HTTPS so slow I/O never blocks frames while `core.Storage` and `Ask` stay strictly on the engine goroutine.
- **Unavailable targets**: `PlatformCloud` never falls back silently to local storage when `cloud.ErrUnavailable` is reported; local saves remain untouched in `save.Backend` until cloud storage becomes available.

## Alternatives considered

### Why not make `CloudSync` itself callback-based?

Changing `CloudSync` from `Load(ctx, slot) ([]byte, error)` to callbacks would break `CloudClient`, `CloudSyncer`, existing callers, and tests across `packages/game/liveops`. Keeping `CloudSync` synchronous while adding `SyncAsync` / `SyncSlotAsync` / `ResolveAsync` on `CloudSyncer` preserves full backwards compatibility and works identically for `PlatformCloud`, `DirCloud`, and `CloudClient`.

### Why not store slots in a `"saves/"` subdirectory by default in `PlatformCloud`?

A nested path works on Apple iCloud ubiquity containers and `nsgotest`, but NSGo's Android `CloudStorage.java` passes the file name verbatim to Google Play Games `SnapshotsClient.open(name, ...)`, which forbids `/`. A flat `"save.<slot>.json"` naming scheme works identically on both iCloud and Play Games while isolating save slots from other files in the container.

### Why not wrap Steamworks native `ISteamRemoteStorage` directly?

NSGo's architecture avoids third-party proprietary C/C++ SDKs (`AGENTS.md` and `nsgo/docs/architecture/cloud.md`). Steam Auto-Cloud synchronizes `core.Scope.DataDir()` (or a configured subdirectory) directly at the filesystem level with zero SDK linking; `DirCloud` brings that synced directory behind `ConditionalCloudSync` with atomic writes and conflict resolution.

## Consequences

- **Benefits**: One `CloudSync` + `CloudSyncer` stack now covers iCloud, Google Play Games Saved Games, Steam Auto-Cloud, and self-hosted `cloudsave`; `SyncAsync` eliminates both engine-goroutine deadlocks on `nsgo/cloud` and UI frame stalls on network cloud syncs while protecting concurrent local saves from being clobbered; `nsgotest` tests exercise device-to-device transfer, native conflicts, change notifications, and offline fallback headlessly and under `-race`.
- **Costs**: `CloudSyncer` carries an internal `syncOp` dispatch indirection and async semaphore; `PlatformCloud` has a subtle distinction between `cx.CloudStore() != nil` (direct synchronous calls in `nsgotest`) and `cx.CloudStore() == nil` (real Apple/Android callback waiting, which must run off the engine goroutine). Live end-to-end verification across two physical iCloud devices or Google Play Games test accounts still depends on signed device builds outside headless CI.

## Testing

Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./liveops/... -- -race`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`. `cloud_platform_test.go` covers iCloud and Play Games device-to-device sync, `WatchChanges` filtering and scope-disposal cleanup, `OnConflict` and `ErrCloudConflict` handling, `ErrCloudUnavailable` non-dropping behavior, prefix/slot validation, and `SyncAsync`/`SyncSlotAsync`/`ResolveAsync` including concurrent local-save protection. `cloud_dir_test.go` covers two-machine Steam Auto-Cloud sync, conditional writes (`SaveIf`/`DeleteIf`), atomic staging cleanup, and case-folding collision rejection.
