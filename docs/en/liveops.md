# Live operations

```go
import "github.com/nsgo-dev/game/pkg/online/liveops"
```

`liveops` holds provider-agnostic live operations primitives for NSGo games. It uses only Go's standard library, `github.com/nsgo-dev/game` and NSGo's public `core` and `cloud` APIs, and links no third-party crash, analytics, ad or payment SDKs. For mainland China releases—first-launch privacy consent gating, self-hosted fallbacks on Android devices without Google Play Services, and planned anti-addiction hooks—see [Releasing in Mainland China](shipping-china.md).

## Scope decision ([#194](https://github.com/nsgo-dev/game/issues/194))

| Area | Status | Rationale |
| --- | --- | --- |
| **Crash reports** ([#195](https://github.com/nsgo-dev/game/issues/195)) | In scope | Provider-agnostic panic capture with a fixed-size step ring buffer, stack trace, metadata and optional state snapshot. |
| **Analytics** ([#198](https://github.com/nsgo-dev/game/issues/198)) | In scope | Provider-agnostic, strictly consent-gated event API backed by `core.Storage`. Zero events are sent or buffered before explicit opt-in consent. |
| **Cloud saves** ([#196](https://github.com/nsgo-dev/game/issues/196)) | In scope | Provider-agnostic `CloudSync` interface, `PlatformCloud` over NSGo's `cloud` package (iCloud on macOS/iOS, Google Play Games Saved Games on Android, in-memory store in `nsgotest`), `DirCloud` for Steam Auto-Cloud and other synced directories, `CloudClient` for the self-hosted [cloudsave](cloudsave.md) service ([#557](https://github.com/nsgo-dev/game/issues/557)), and `CloudSyncer` conflict resolution ([#539](https://github.com/nsgo-dev/game/issues/539)). |
| **In-app purchases** ([#369](https://github.com/nsgo-dev/game/issues/369)) | In scope | `Purchases` over NSGo's purchase services: consumables credited into the save exactly once, unlocks and subscriptions from the store's entitlements, restore, Ask to Buy and an optional server check. |
| **Local reminders** ([#1310](https://github.com/nsgo-dev/game/issues/1310)) | In scope | `Reminders` over NSGo's `core.Scope.ShowNotification`: versioned YAML definitions (`RemindersConfig`), permission prompted only on first `Schedule` or explicit `RequestPermission`, persisted player toggle, local-time `QuietHours`, and foreground cancel/reschedule/keep rules. |
| **Review prompts** ([#1767](https://github.com/nsgo-dev/game/issues/1767)) | In scope | `ReviewPrompt` over `core.Storage` and `core.Scope.RequestReview`: gates store rating requests on sessions, elapsed days, positive moments, cooldown and once-per-version rules, suppresses prompts after negative moments, and fails closed on corrupt state or unsupported platforms. |
| **Share cards and invites** ([#1738](https://github.com/nsgo-dev/game/issues/1738)) | In scope | `Invites` over `ShareHost`: bounded canonical `key=value&…` payloads with optional HMAC-SHA256 signing, at-most-once cold-launch and warm-resume delivery, native/web `PlatformShareHost`, mini-game `MiniGameShareHost` (ready for [nsgo#2672](https://github.com/nsgo-dev/nsgo/issues/2672)), and 5:4 frame snapshot helpers. |
| **Remote push** ([#1310](https://github.com/nsgo-dev/game/issues/1310)) | Pending upstream | Waiting on NSGo remote push ([nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)) and its design decisions ([nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)). Remote push is currently unavailable everywhere. |
| **Mini-game ads** ([#1739](https://github.com/nsgo-dev/game/issues/1739)) | In scope | `Ads` over a host `AdHost`: rewards only for a video watched to the end and at most once per show, no-fill and failure results, one ad at a time, interstitial cap, interval and `Disabled` predicate, and pause and input-reset hooks. The mini-game host waits on [nsgo#2672](https://github.com/nsgo-dev/nsgo/issues/2672). |
| **Native ad SDKs** ([#199](https://github.com/nsgo-dev/game/issues/199)) | Out of scope | Ad networks (AdMob, AppLovin and similar) require proprietary native SDKs with heavy store-policy and privacy obligations. Keep them out of the core `game` module; games that need them embed native SDK views directly via NSGo. |

## Crash reports

`CrashReporter` keeps the most recent simulation steps in a pre-allocated ring buffer (`DefaultHistorySize` = 120, zero allocations in steady state) and guards `game.Loop` callbacks or arbitrary functions against panics.

- `RecordStep(dt, summary)` and `RecordStepData(dt, summary, data)` append a `StepRecord` to the ring buffer and return its monotonic step number.
- `WrapLoop(l)` returns a guarded `*game.Loop`; `GuardLoop(l)` wraps `l.Update` and `l.Draw` in place. When `Update` does not call `RecordStep` itself during a step, the guard records an `"update"` step automatically, including for the step that panicked.
- `RunSafe(fn)` guards any callback and reports whether it panicked.
- On panic, the reporter builds a JSON-marshalable `CrashReport` holding `Timestamp`, `Panic`, `Message`, `Stack` (`runtime/debug.Stack`), `Step`, chronological `LastSteps` (oldest to newest), optional `State` from `CaptureState`, and a copy of `Metadata`, then passes it to `Reporter`.
- Set `Rethrow: true` when the process should still panic after `Reporter` finishes persisting or shipping the report.
- `CrashReport` and `StepRecord` are aliases for the types in `pkg/online/liveops/crashreport`, a package that imports no NSGo or game package, so a headless dedicated server builds the same report schema (`gameserver.Panic.Report`, see [the server guide](gameserver.md#stopping)). Existing code keeps working unchanged.

```go
reporter := liveops.NewCrashReporter(liveops.CrashOptions{
	HistorySize:  120,
	Reporter:     func(r liveops.CrashReport) { uploadOrSave(r) },
	CaptureState: func() any { return world.Snapshot() },
	Metadata:     map[string]string{"version": appVersion, "scene": "level-1"},
	Now:          cx.Now,
})

loop := reporter.WrapLoop(&game.Loop{
	Update: func(dt time.Duration) {
		reporter.RecordStep(dt, "world.step")
		world.Step(dt)
	},
	Draw: func(alpha float64) { world.Draw(alpha) },
})
loop.Run(cx)
```

## Analytics

`Analytics` gates every event on explicit opt-in consent (`ConsentGranted`) and ships no built-in telemetry backend.

- Consent states are `ConsentUnknown` (default), `ConsentDenied` and `ConsentGranted`.
- `NewAnalytics(store, key, provider)` persists the choice in `core.Storage` under `key` (`DefaultConsentKey` when empty) and fails closed to `ConsentUnknown` if the stored value is missing or unrecognized. `NewMemoryAnalytics(provider)` keeps consent in memory only.
- `Track(name, props)` returns `false` and drops the event immediately whenever consent is `ConsentUnknown` or `ConsentDenied` — events are never sent and never buffered for retroactive replay.
- When consent is `ConsentGranted`, `Track` defensively copies `props` and dispatches an `Event` to `AnalyticsProvider.Send`.
- Calling `SetConsent(ConsentDenied)` immediately stops all subsequent `Track` and `Flush` calls.

```go
analytics := liveops.NewAnalytics(cx.Storage(), "liveops.consent", myProvider)
analytics.SetClock(cx.Now)

// Dropped before explicit opt-in:
analytics.Track("app_open", nil) // returns false, 0 events sent

// After player opts in:
_ = analytics.SetConsent(liveops.ConsentGranted)
analytics.Track("level_complete", map[string]any{"level": 1})
```

## Cloud saves

`CloudSync` defines the provider-agnostic slot interface (`Load`, `Save`, `Delete`, `List`, with sentinel errors `ErrSlotNotFound`, `ErrCloudUnavailable`, `ErrCloudConflict`, `ErrSlotTooLarge` and `ErrTooManySlots`). `ConditionalCloudSync` adds `SaveIf` and `DeleteIf`, which write only while the slot still holds the payload the caller read, identified by `PayloadHash`.

Three built-in adapters implement `CloudSync` behind the same interface:

| Adapter | Services / targets | Notes |
| --- | --- | --- |
| `PlatformCloud` | **iCloud** (macOS, iOS, iPadOS, tvOS), **Google Play Games Saved Games** (Android), isolated in-memory store (`nsgotest`) | Built on `github.com/nsgo-dev/nsgo/cloud`. Returns `ErrCloudUnavailable` on Windows, Linux, the web or when signed out / missing entitlements, and never silently drops writes. |
| `DirCloud` | **Steam Auto-Cloud** and other directory-synced desktop stores (Windows, Linux, macOS) | `ConditionalCloudSync` keeping each slot as an atomically written `<slot>.json` file in a synced directory (such as `filepath.Join(cx.DataDir(), "cloud")`). |
| `CloudClient` | Self-hosted [cloudsave](cloudsave.md) service ([#557](https://github.com/nsgo-dev/game/issues/557)) (every target, including the web) | `ConditionalCloudSync` over HTTPS signed in through the [account service](accounts.md), with size limits and version history (`Versions`, `LoadVersion`, `Restore`, `Purge`). |

Games can also implement `CloudSync` over their own backend or an in-memory test double.

### Platform cloud saves: iCloud, Play Games and Steam Cloud ([#196](https://github.com/nsgo-dev/game/issues/196))

`PlatformCloud` stores each save slot as `<Prefix><slot>.json` (default `DefaultCloudPrefix` = `"save."`, e.g. `save.main.json`) using NSGo's `cloud` file/snapshot API. The flat dot-separated name avoids path separators so both Apple iCloud ubiquity containers and Google Play Games `SnapshotsClient` accept it, while keeping save slots separate from other files in the container.

```go
pc, err := liveops.NewPlatformCloud(cx, liveops.PlatformCloudOptions{})
if err != nil {
	return err
}
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:  save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:  pc,
	State:  cx.Storage(),
	Device: deviceID,
	Policy: liveops.PolicyKeepBoth,
})
if err != nil {
	return err
}

// Re-sync when another device updates a save slot:
pc.WatchChanges(func(ev liveops.CloudChange) {
	syncer.SyncAsync(cx, ctx, func(res liveops.SyncResult, err error) { /* update UI */ })
})

// Resolve native iCloud / Play Games file conflicts when the platform reports one:
pc.OnConflict(func(c liveops.PlatformConflict) {
	chosen := c.Remote
	if c.LocalTime.After(c.RemoteTime) {
		chosen = c.Local
	}
	_ = c.Resolve(chosen)
})
```

- `CloudProviderFor(env)` and `pc.Provider()` report the target's built-in service (`CloudProviderICloud`, `CloudProviderPlayGames`, `CloudProviderNone`; `CloudProviderSteam` identifies Steam Auto-Cloud), returning `CloudProviderNone` when `env` is nil or `env.Simulated()` is true (such as inside a mini program on iOS/Android or a web build run with `--nsgo-os`). `pc.Available()` reads `cx.Env().CanUseCloudStorage()` reactively (and is false when `Simulated()` is true) so views update when iCloud or Play Games availability changes.
- On real Apple and Android targets, NSGo's `cloud` delivers operation callbacks on `cx`'s engine goroutine via `cx.Post`, so run `PlatformCloud` syncs through `CloudSyncer.SyncAsync` (or call `PlatformCloud` off the engine goroutine) rather than blocking the engine goroutine with synchronous `Sync`. `SyncAsync`, `SyncSlotAsync` and `ResolveAsync` run cloud I/O on a worker goroutine while marshaling `Local`, `State`, `Summarize`, `Now` and `Ask` onto `cx`'s engine goroutine, serialize concurrent calls, and refuse to overwrite a local save made while a cloud read was in flight (`ErrConflictChanged`).
- On macOS and iOS, declare iCloud entitlements in `nsgo.yaml` under `cloud:` (`kv_store` / `containers: [iCloud.com.example.myapp]`); Mac App Store builds require a provisioning profile carrying those entitlements, otherwise iCloud returns `ErrCloudUnavailable`.
- If iCloud (`NSFileVersion`) or Google Play Games (`SnapshotsClient`) holds an unresolved native conflict for a slot, `PlatformCloud.Load` fails with an error wrapping `ErrCloudConflict`, `cloud.ErrConflict` and `*cloud.ConflictError`; `CloudSyncer` skips that slot while syncing other slots until `PlatformConflict.Resolve` (or a write) clears it.
- On **Steam** (Windows, Linux, macOS), configure Steam Auto-Cloud to sync a directory under `cx.DataDir()`. Games that do not need `CloudSyncer`'s conflict envelopes can point Steam Auto-Cloud directly at `save.FileBackend(cx.DataDir())` (`*.json`); games that want the same `CloudSync` + `CloudSyncer` conflict-resolution flow across every platform pass `liveops.NewDirCloud(filepath.Join(cx.DataDir(), "cloud"))` as `Cloud`. `DirCloud` implements `ConditionalCloudSync` with atomic temp-file writes, directory sync and case-folding collision checks. See [Steam Cloud](steam.md#steam-cloud) for the exact Steamworks Auto-Cloud roots and Root Overrides on Windows, macOS and Linux / SteamOS.
- Under `nsgotest`, start apps with `nsgotest.CloudAvailable(true)` and drive cross-device updates, native conflicts and offline fallback with `app.SimulateCloudChange`, `app.SimulateCloudConflict` and `app.SetCloudAvailable`; `liveops/cloud_platform_test.go` and `liveops/cloud_dir_test.go` cover each.

### Resolving conflicts ([#539](https://github.com/nsgo-dev/game/issues/539))

`CloudSyncer` reconciles a device's `save.Backend` with a `CloudSync` and never silently loses progress when the same slot changed on two devices.

```go
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:  save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:  myCloud,
	State:  cx.Storage(),
	Device: deviceID, // 1-64 ASCII letters, digits, '-' or '_'
	Policy: liveops.PolicyAsk,
	Ask: func(ctx context.Context, c liveops.Conflict) (liveops.Choice, error) {
		pending = append(pending, c) // shown by the game's conflict dialog
		return liveops.ChoiceDefer, nil
	},
	Summarize: func(slot string, data []byte) string { return describeSave(data) }, // "Level 12, 3h 40m"
})
if err != nil {
	return err
}
store := save.NewStore(syncer.Backend()) // stamps each local write with its time

res, err := syncer.Sync(ctx)
for _, r := range res.Conflicts() {
	log.Printf("%s: %s", r.Slot, r.Outcome)
}

// Later, when the player picks a copy in the dialog:
r, err := syncer.Resolve(ctx, c, liveops.ChoiceLocal) // or ChoiceCloud, ChoiceBoth
if errors.Is(err, liveops.ErrConflictChanged) {
	// A save or sync changed the slot while the dialog was open: sync again and ask about the new conflict.
}
```

How a conflict is detected:

- For each slot the syncer remembers the base: the content hash (`sha256:…`) and cloud revision both sides held after the last sync, kept as JSON text in `State` under `StateKey` (`DefaultSyncStateKey` when empty).
- `Sync` visits every slot known locally, in the cloud or in the sync state, in name order. When only one side differs from the base, that side is copied to the other (`OutcomeUploaded`, `OutcomeDownloaded`, `OutcomeDeletedCloud`, `OutcomeDeletedLocal`). When both differ from the base and from each other, the slot is a conflict. A device that has never synced a slot and holds a different copy from the cloud is a conflict too, never an overwrite.
- Before every cloud write the syncer reads the cloud copy again; if another device uploaded in between, the slot fails with `ErrCloudChanged`, nothing is written, and the next sync sees the new copy. When `Cloud` implements `ConditionalCloudSync`, as `CloudClient` and `DirCloud` do, the write itself is conditional on that copy, so an upload landing after the re-read fails the same way instead of being overwritten.
- A slot that fails (a corrupt payload, an unsafe slot name, `Ask` returning an error) is skipped and its error joined into the result while the others still sync; `ErrCloudUnavailable` or a done context stops the sync.

Each `Conflict` carries a `SaveVersion` for both sides: whether it exists (a side may have deleted the slot), its revision, hash, modified time, writing device, save `EnvelopeVersion`, the game's `Summarize` text and the raw data. Set `CloudSyncOptions.SaveVersion` to the `save.Store.Version` this build writes so a cloud copy from a newer game version is never written locally (`OutcomeNeedsUpdate`, with `SlotResult.LocalVersion`, `SlotResult.CloudVersion` and `SyncResult.NeedsUpdate()`) and never overwritten by an older build. The policies:

| Policy | Behaviour |
| --- | --- |
| `PolicyNewest` (default) | Keeps the copy with the later modified time and overwrites the other (`OutcomeKeptLocal` or `OutcomeKeptCloud`). When either time is unknown, such as a local copy written without `Backend()` (a recorded time counts only for the exact copy it was recorded with), or both are equal, it keeps both. When one side deleted the slot and the other changed it, it keeps the changed copy. When both copies carry save envelopes of different versions, it never resolves in favour of the older envelope version and falls back to keeping both. |
| `PolicyKeepBoth` | Keeps the cloud copy in the slot and moves the local copy to `<slot>.conflict-<device>` (then `-2`, `-3`…) on both sides (`OutcomeKeptBoth`, `SlotResult.Fork`). When the cloud copy exceeds `SaveVersion`, the local copy is forked to both sides while both the local and cloud slots stay untouched (`OutcomeNeedsUpdate`). When that name would exceed the 200-byte slot limit, the slot part is shortened and followed by `~` and 8 hex digits of its SHA-256. The fork and its sync state are written locally before anything is overwritten, so if the upload fails the next sync uploads the same fork rather than creating another. A deletion against a change keeps the change. |
| `PolicyAsk` | Calls `Ask` with the conflict (including both copies' `EnvelopeVersion`) and applies its `Choice`: `ChoiceLocal`, `ChoiceCloud`, `ChoiceBoth` (as `PolicyKeepBoth`) or `ChoiceDefer`, which writes nothing and asks again on the next sync. An error from `Ask` leaves both copies untouched. Resolving in favour of an older envelope version falls back to keeping both, and applying a cloud copy newer than `SaveVersion` reports `OutcomeNeedsUpdate` without overwriting the local slot. |

`Ask` runs inside the sync, on the app's thread, so it must not wait for the player. To ask through a dialog, keep the `Conflict`, return `ChoiceDefer`, show both summaries, and apply the answer with `Resolve(ctx, conflict, choice)`, as in the example above. `Resolve` reads both sides again and applies the choice only while they still hold the copies the player saw (the same existence, hash and cloud revision); otherwise it writes nothing and returns `ErrConflictChanged`. It works under every policy.

Whenever a resolution replaces a copy, `SlotResult.Discarded` returns it so the game can show or archive it. When the replaced local copy is a valid save envelope and the cloud copy exists, the local backend also keeps it as its backup (`ReadBackup`); when the cloud copy is a deletion, or the local copy is not a save envelope, `Discarded` is the only copy left.

Cloud payloads are JSON text with a fixed key order: `format` (`CloudSaveFormat`), `version` (`CloudSaveVersion`), `revision`, `hash`, `modified` (RFC 3339, UTC), `device` and `data`, a string holding the save as written (or `data_base64` for data that is not UTF-8). `DecodeCloudSave` refuses unknown fields, newer versions, trailing data and a hash that does not match the data with `ErrCorruptCloudSave`; decoding a payload `EncodeCloudSave` wrote and encoding it again gives back the same bytes. A cloud payload without the format marker, written by a game that used `CloudSync` directly, is read as revision 0 and upgraded on the next upload. Cloud slot names are checked with `save.ValidateSlot` before they reach the local backend.

`Backend()` exposes the local backend through `Unwrap`, so a settings store built on it (`save.NewDefaultSettingsStore(syncer.Backend())`) keeps settings under the local backend's own settings key, and settings are never synced as a slot.

A `CloudSyncer` is not safe for concurrent synchronous use; call `Sync`, `SyncSlot` and `Resolve` where the app's code runs, as `core.Storage` requires, or use `SyncAsync`, `SyncSlotAsync` and `ResolveAsync` to run cloud I/O off the engine goroutine while marshaling local storage and `Ask` back onto `cx` (required with `PlatformCloud` on Apple and Android devices, and recommended with `CloudClient`). With a plain `CloudSync`, which has no conditional write, an upload by another device that lands between the syncer's re-read and its own write can still be overwritten; the window is a single `Load` and `Save` apart. A `ConditionalCloudSync` closes it.

## In-app purchases

`Purchases` sells a game's products over NSGo's purchase services (`cx.Purchase`, `cx.OnPurchaseUpdate`, `cx.CurrentEntitlements`, `cx.SyncPurchases`). It sells where `cx.Env().CanPurchase()` is true, which in NSGo v0.3.0 is the App Store on macOS, iOS and tvOS in a build whose `nsgo.yaml` sets `apple.purchases: true`; elsewhere `Buy` fails with `core.PurchasesUnavailable` and `Available` reports false.

- A consumable is credited exactly once. Keep a `Ledger` inside the save, beside the state `Credit` changes: `Purchases` calls `Credit`, records the transaction ID in the ledger, calls `Save` and only then finishes the transaction. A kill before the save loses both the credit and its record, and the store hands the transaction to the app again at the next launch; a kill after the save finds the ID in the ledger and only finishes it. A failed `Save` leaves the transaction unfinished.
- Non-consumables and subscriptions are owned, not credited: `Owned(product)` reads the store's entitlements, which `NewPurchases` loads at start and `Refresh` reloads, and checks a subscription's expiration date against `cx.WallTime()`. Read in a reactive computation, `Owned` and `Pending` update the view on purchases, refunds and restores; a subscription that expires while the app runs is noticed at the next read or `Refresh`, so call `Refresh` as the app returns to the foreground. An older transaction for a product, such as an expired renewal handed back unfinished at launch, never replaces a later one.
- `Restore` runs the store's sync (call it only from a Restore Purchases button, as it may ask the user to sign in) and reloads the entitlements, which brings unlocks back after a reinstall that wiped the save.
- `Buy` answers `Bought`, `BuyPending` (Ask to Buy and other approvals; `Pending(product)` stays true until the approved transaction arrives through the store's updates, which deliver it as `Buy` would have; a declined request produces no transaction, so `Pending` stays true until a later purchase of the product arrives or the app restarts), `BuyCancelled`, or `BuyFailed` with the error.
- A refund or revocation removes an unlock, and runs `Revoke` once for a credited consumable when the game sets it. The ledger keeps the newest `LedgerLimit` (1024) IDs, so a refund of a consumable credited before that many newer ones is finished without `Revoke`.
- `Verify` hands each transaction, whose `Signed` is the store's JWS, to the game's server before delivery and may answer from any goroutine; a refused transaction is neither delivered nor finished. Without `Verify`, only transactions the device verified are delivered.
- `OnError` hears failures no `Buy` or `Restore` answers, such as an unfinished transaction at launch whose save failed.

```go
type Progress struct {
	Coins     int            `json:"coins"`
	Purchases liveops.Ledger `json:"purchases"`
}

saves := save.NewStorageStore(cx.Storage(), "game")
var p Progress
_ = saves.Load("main", &p)

shop := liveops.NewPurchases(cx, liveops.PurchasesOptions{
	Ledger: &p.Purchases,
	Credit: func(t core.Transaction) { p.Coins += 100 * t.Quantity },
	Revoke: func(t core.Transaction) { p.Coins -= 100 * t.Quantity },
	Save:   func() error { return saves.Save("main", &p) },
	Verify: func(t core.Transaction, done func(error)) { go done(server.Verify(t.Signed)) },
})

shop.Buy("com.example.coins.100", 1, func(r liveops.BuyResult, err error) { /* bought, pending, cancelled or failed */ })
showNoAds := !shop.Owned("com.example.noads")
```

Tests drive purchases through `nsgotest`: start the app with `nsgotest.Environment(core.Environment{Purchases: true})`, set `app.Products`, and use `app.PurchaseAnswer` for a cancel or Ask to Buy, `app.UpdatePurchase` for an approval or a refund, `nsgotest.UnfinishedTransactions` together with `nsgotest.Storage` for a run killed before it finished a transaction, and `app.Entitlements` with `Restore` for a reinstall. `liveops/purchases_test.go` covers each.

To check a restore by hand under StoreKit Testing, add a StoreKit configuration file with a non-consumable to the iOS app's Xcode scheme, build with `apple.purchases: true`, buy the product in the simulator, delete and reinstall the app, then press the game's Restore Purchases button: `Owned` reports the product again. Android has no store in NSGo v0.3.0 (Google Play Billing waits on [nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369)).

### WeChat mini game payments

WeChat mini games sell items through virtual payments (`wx.requestMidasPaymentGameItem`), which `Purchases` does not drive yet: the platform needs an order signed with the game's AppKey, which must never reach a client, and it delivers goods by calling the game's server, not the client. `github.com/nsgo-dev/game/pkg/online/storeverify/wechatpay` is that server side, with no NSGo dependency:

- `SignOrder(appKey, sessionKey, Order)` returns the `signData` JSON with its `paySig` (HMAC-SHA256 with the AppKey over `requestMidasPaymentGameItem&` + `signData`) and the user-state `signature` (HMAC-SHA256 with the player's session key from the login exchange). The client passes all three to the platform unchanged. `GoodsPrice` is in fen; `OutTradeNo` is at most 32 letters, digits or `_-|*@` and does not start with `_`.
- `Handler.Handle(ctx, event, payload, payEventSig)` checks a `minigame_game_pay_goods_deliver_notify` push: the event, the `PayEventSig` (HMAC-SHA256 with the AppKey over event + `&` + `Payload`), and that the payload's `Env` is the handler's `Env`, so sandbox purchases never grant goods in production. It then calls `Deliver` once per `OutTradeNo`, recording the order in a `DeliveryStore`; the platform's retries, including ones that arrive while the first delivery is still running in the same process, return nil without delivering again, and a `Deliver` error is not recorded, so the next retry delivers. Answer `{"ErrCode":0,"ErrMsg":"Success"}` when `Handle` returns nil and a non-zero `ErrCode` otherwise.

`MemoryStore` forgets deliveries on restart; a server without a durable store, and several processes behind one store must make `Deliver` itself idempotent on `OutTradeNo`, for example by granting in the database transaction that records the order. The HTTP endpoint stays with the game's server: the push URL's token check and the safe-mode AES decryption are not in the package yet, nor are SQLite and Postgres stores, an order query for reconciliation, a client-side `Purchases` mode or the mapping of the minor-protection error `-15009` ([#1740](https://github.com/nsgo-dev/game/issues/1740)).

## Local reminders

`Reminders` schedules, cancels and reschedules local re-engagement notifications over NSGo's `core.Scope.ShowNotification`. It works natively (including after the app closes) on macOS, iOS, watchOS, Android and Windows, and while the app or page runs on Linux and the web — anywhere `cx.Env().CanScheduleNotifications()` is true.

- **Versioned YAML definitions**: declare reminders in a `reminders.yaml` file (schema `https://game.nsgo.dev/schemas/reminders.v1.json`) loaded with `ParseReminders` or `LoadRemindersFS` and written deterministically with `EncodeReminders`. Each `ReminderDef` carries a unique `id`, localization keys `title` and optional `body`, either a positive `delay` or a 24-hour local wall-clock `at` (optionally paired with `weekday` for weekly schedules), optional `repeat` (`daily` or `weekly`), optional `onOpen` (`cancel`, `reschedule`, or `keep`; defaults to `cancel`), and optional `silent`. Unknown keys, duplicate keys, anchors/aliases and unsupported versions are refused.
- **Permission timing**: `NewReminders` never queries or prompts for notification permission at launch. Permission is requested only when the game first calls `Schedule` / `ScheduleWith` (while reminders are enabled and supported) or an explicit `RequestPermission`. Concurrent calls coalesce into one prompt, and `Permission()` exposes the last observed `core.NotificationPermission` reactively.
- **Player setting & quiet hours**: `Enabled()` / `SetEnabled(bool)` persists the player's toggle in `core.Storage`; turning reminders off immediately cancels every active reminder and stops future `Schedule` calls from prompting. `QuietHours` defines a daily local-time window `[Start, End)` (including overnight ranges such as `"22:00"` to `"08:00"`); any reminder landing inside quiet hours is deferred to `End`. `SetQuietHours` persists player overrides (`nil` saves `"off"` so config defaults do not return on restart) and defers any already-scheduled reminder that now falls inside the new window without prompting.
- **Foreground reconciliation**: `Reminders` observes `cx.Env().Lifecycle()` and reconciles active reminders on launch and whenever the app returns to the front — cancelling `onOpen: cancel`, rescheduling `onOpen: reschedule` from the current `WallTime()` without prompting, and leaving `onOpen: keep` untouched.
- **Platform capability & presses**: `CanScheduleNotifications()` reactively forwards `cx.Env().CanScheduleNotifications()` so a game can hide its reminders toggle where scheduled notifications do nothing (such as tvOS). `RemindersOptions.OnPress` receives a `ReminderPress{ID}` when the player taps a delivered reminder notification, using a dedicated ID prefix (`DefaultReminderPrefix`) so `CancelAll` and press routing never interfere with the app's other notifications.

```go
cfg, err := liveops.LoadRemindersFS(assetsFS, "reminders.yaml")
if err != nil {
	return err
}
reminders, err := liveops.NewReminders(cx, liveops.RemindersOptions{
	Config:   cfg,
	Localize: func(key string) string { return loc.T(key) },
	OnPress: func(ev liveops.ReminderPress) {
		openReminderDestination(ev.ID)
	},
})
if err != nil {
	return err
}

// Hide the toggle on platforms that cannot schedule notifications:
showReminderToggle := reminders.CanScheduleNotifications()

// Ask permission and schedule only at a natural moment (never at launch):
_ = reminders.Schedule("daily_reward", func(scheduled bool) { /* update UI */ })
```

Tests drive reminders headlessly through `nsgotest`: start the app with `nsgotest.Environment(core.Environment{ScheduledNotifications: true})` and `nsgotest.WallTime(...)`, advance the clock with `app.FastForward(...)`, test background/foreground transitions with `app.SetEnvironment(...)`, simulate notification taps with `app.PressNotification(...)`, and check persistence across restarts with `nsgotest.Storage(...)`. `liveops/reminders_test.go` covers each path.

### Remote push (pending upstream)

Remote push notifications (`liveops.Push`) are currently unavailable everywhere and are intentionally not shipped yet. The game-layer wrapper — registering device tokens with a game server, routing push payloads to live-event handlers and delivering remote notification presses alongside `ReminderPress` — waits on NSGo implementing remote push ([nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)) and resolving its open API design decisions ([nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)).

## Review prompts

`ReviewPrompt` decides when to call NSGo's `core.Scope.RequestReview` so a game asks for a store rating only after positive moments, at most once per app version, and never at a frustrating time.

### Store guidelines

- **Apple App Store (StoreKit)**: shows the system rating prompt at most three times in any 365-day period per app, regardless of version updates. Never trigger `RequestReview` from a button or direct user tap, and never interrupt gameplay or active modals; ask at a natural pause after a completed task or positive milestone.
- **Google Play (In-App Review API)**: enforces its own opaque quota without reporting whether the dialog appeared. Wait until the player has used the game enough to give meaningful feedback, and never show a leading pre-prompt ("Are you enjoying the game?") before calling the store API.

### Policy and persistence

- **Thresholds and defaults**: `NewReviewPrompt(store, opts)` increments the session count on launch and gates `MaybeAsk(cx)` on `MinSessions` (`DefaultReviewMinSessions` = 3, never prompting in the first session), `MinDays` since first launch (`DefaultReviewMinDays` = 3), `MinPositiveMoments` in the current `AppVersion` (`DefaultReviewMinPositiveMoments` = 3), `Cooldown` between requests across versions (`DefaultReviewCooldown` = 122 days, keeping requests within Apple's 3-per-365-day cap), and once per `AppVersion`.
- **Moments and suppression**: call `Moment(MomentPositive)` after rewarding milestones (such as clearing a level or setting a record) and `Moment(MomentNegative)` after losses or errors. Any `MomentNegative` suppresses prompts for the rest of the current session; a new launch or `RecordSession()` starts a fresh session. Set `Suppress` to block prompts while a purchase sheet, ad flow or modal dialog is open.
- **Versioned JSON state and fail-closed safety**: state is stored as deterministic versioned JSON text (`ReviewPromptStateVersion` = 1) in `core.Storage` under `StorageKey` (`DefaultReviewPromptKey` when empty). Any unrecognised field, trailing token, invalid timestamp, negative counter or unknown/future schema version fails closed (`Eligible()` and `MaybeAsk(cx)` return `false` without overwriting stored data). When `AppVersion` changes, per-version `positiveMoments` resets to 0 while `firstLaunch`, `sessions`, and `lastPromptedAt` stay intact so cooldown still protects store quotas across hotfixes. `MaybeAsk` persists the prompt record before calling `cx.RequestReview()`, failing closed if storage cannot be written.
- **Platform no-op**: on targets where `cx.Env().CanRequestReview()` is false (Windows, Linux, the web), `MaybeAsk(cx)` is a no-op returning `false` without marking the version as prompted, so games call it unconditionally.

```go
review := liveops.NewReviewPrompt(cx.Storage(), liveops.ReviewPromptOptions{
	AppVersion: Project.AppVersion,
	Now:        cx.WallTime,
	Suppress:   func() bool { return purchaseOpen },
})

// After clearing a level, at a natural pause (for example when the result card appears):
review.Moment(liveops.MomentPositive)
review.MaybeAsk(cx)

// After a defeat or frustrating failure:
review.Moment(liveops.MomentNegative)
```

Tests drive review prompts headlessly through `nsgotest` with `nsgotest.Environment(core.Environment{OS: core.OSIOS, CanRequestReview: true})`, `nsgotest.WallTime(...)`, `app.FastForward(...)`, `nsgotest.Storage(...)`, and `app.ReviewRequests`; `liveops/review_test.go` covers every rule.

## Share cards and invites

`Invites` shares payload-carrying cards and invite links over a small `ShareHost` interface and reads incoming invite payloads back when another player opens or resumes the game from one.

### Payload format and HMAC verification

- **Canonical wire encoding**: payloads are encoded as sorted `key=value&…` query strings (`EncodeInvitePayload` / `DecodeInvitePayload`) using URL query escaping. Keys must be 1–64 bytes of ASCII `[A-Za-z0-9._-]`, values must be valid UTF-8 free of ASCII control characters, and payloads carry at most `MaxInvitePayloadKeys` (32) keys within the host's byte cap (`DefaultMaxInvitePayloadBytes` = 1024). Unknown keys that satisfy validation are kept.
- **Pre-share enforcement and safe rejection**: `Invites.Share` validates and encodes the card before calling `ShareHost.Share`; oversized (`ErrInvitePayloadTooLarge`) or malformed (`ErrInvalidInvitePayload`, `ErrInvalidCard`) cards are refused immediately without opening the platform share UI. Malformed, duplicate-key, oversized or tampered incoming payloads are rejected safely and reported to `InvitesOptions.OnError` without delivering an invite.
- **Optional HMAC-SHA256 signing**: when `HMACKey` is configured, `EncodeInvitePayload` computes a base64url raw HMAC-SHA256 tag over the canonical sorted unsigned query and appends it under `InviteSigKey` (`"sig"`). Because decoding canonicalizes parameter order and space escaping (`+` vs `%20`) before verifying with `hmac.Equal`, signatures survive URL re-encoding by chat apps and browsers. Set `RequireSignature: true` to reject unsigned invites.
- **Server-side verification and reward idempotency**: keep production reward-grant signing secrets on the game server when possible — mint signatures on the server with `SignInvitePayload` (passed to clients via `Card.Signature`), and have the recipient client send `Invite.RawQuery` to the server to verify with `VerifyInvitePayload`. Use `Invite.ID` (carried under `InviteIDKey` = `"id"`, set via `Card.InviteID`) as the server's idempotency key for invite reward grants ([#1643](https://github.com/nsgo-dev/game/issues/1643)), and pass room or party parameters from `Invite.Payload` to your party/room join flow ([#1688](https://github.com/nsgo-dev/game/issues/1688)).

### Launch, resume and menu cards

- **Cold launch (`Opened`)**: `Opened() (Invite, bool)` returns the invite that cold-started the game and consumes it so subsequent calls return `false`.
- **Warm resume (`Consume`, `Incoming`, `OnInvite`)**: invites that arrive while the game is already running (mobile/desktop deep links, web URL/hash edits, or mini-game `onShow`) are delivered at most once through `Consume() (Invite, bool)` (reactive inside `core.Effect`) and to `InvitesOptions.OnInvite` / `OnInvite(...)` listeners. `Incoming()` and `PendingCount()` reactively inspect the unconsumed queue.
- **Menu share card (`SetMenuCard`)**: registers a dynamic `func() Card` callback for platforms with a built-in share menu (such as WeChat mini games' Forward button); every returned card passes through the same size, validation and signing checks before reaching the host. Passing `nil` clears and disables the menu card.

### Platform hosts and 5:4 card snapshots

| Host | Targets | Mechanism |
| --- | --- | --- |
| `PlatformShareHost` (default when `InvitesOptions.Host` is nil) | Mobile (iOS, Android), desktop (macOS, Windows, Linux) | Builds a link from `BaseURL` (such as `mygame://invite`), shares text and attached image bytes through `core.Scope.Share`, and receives launch/resume links through `core.Scope.AppURL` and `core.Scope.OnOpenLinks`. |
| `PlatformShareHost` on `core.OSWeb` | Web | Auto-derives `BaseURL` from `cx.AppURL()` when omitted, supports query (`InviteEncodeQuery`) or fragment (`InviteEncodeFragment`) payloads, shares via `core.Scope.Share` (with optional `FallbackToClipboard` when `navigator.share` is unavailable), and reads launch/navigation payloads via `cx.AppURL()` and `cx.OnAppURLChange`. |
| `MiniGameShareHost` | WeChat and other mini games | Wraps a `MiniGameShareDriver` (`ShareAppMessage`, `ShowShareMenu` with `WithShareTicket`, `OnShareAppMessage`, `LaunchOptions`, `OnShow`), preserving `Invite.Scene` and `Invite.ShareTicket`. Passing a `nil` driver yields a safe unavailable host (`ErrShareUnavailable`) until NSGo ships native mini-game share-card APIs ([nsgo#2672](https://github.com/nsgo-dev/nsgo/issues/2672)). |

Use `CaptureCardSnapshot(cx, target, opts, done)` (or `CropCardSnapshot` / `EncodeCardSnapshotPNG` on any `image.Image`) to center-crop and scale the current game frame into a 5:4 PNG (`DefaultCardSnapshotWidth` = 500, `DefaultCardSnapshotHeight` = 400) returned as a `core.ImageSource` ready for `Card.Image`.

```go
invites, err := liveops.NewInvites(cx, liveops.InvitesOptions{
	Platform: liveops.PlatformShareHostOptions{
		BaseURL:             "mygame://invite",
		FallbackToClipboard: true,
	},
})
if err != nil {
	return err
}

// Cold-start invite (consumed once):
if inv, ok := invites.Opened(); ok {
	handleInvite(inv) // verify inv.RawQuery on server; use inv.ID as grant idempotency key
}

// Warm resume invites (reactive, consumed once each):
cx.Effect(func() {
	for {
		inv, ok := invites.Consume()
		if !ok {
			break
		}
		handleInvite(inv)
	}
})

// Capture a 5:4 frame snapshot and share an invite card:
liveops.CaptureCardSnapshot(cx, gameCanvasRef, liveops.CardSnapshotOptions{}, func(img core.ImageSource, err error) {
	invites.Share(liveops.Card{
		Title:    "Join my squad!",
		Image:    img,
		InviteID: inviteID,
		Payload:  map[string]string{"room": roomCode},
	}, func(shown bool, err error) { /* handle result */ })
})
```

Tests cover fake `ShareHost` round-trips, size boundaries, malformed-input rejection, HMAC verification, at-most-once launch/resume delivery, `MiniGameShareHost`, `PlatformShareHost` (native & web via `nsgotest`), and 5:4 snapshots in `liveops/invites_test.go`.

## Mini-game ads

Mini games (WeChat, Douyin) show ads through the host's own API rather than an embedded SDK. `Ads` holds the rules every game otherwise writes for itself, over an `AdHost` that asks the platform to show one ad and calls `done(ended, err)` when it closes.

```go
ads := &liveops.Ads{
	Host:                 host,
	InterstitialCap:      3,
	InterstitialInterval: 2 * time.Minute,
	Disabled:             func() bool { return shop.Owned("noads") },
	Pause:                func(p bool) { loop.Paused = p },
	ResetInput:           resetHeldInput,
}
ads.ShowRewarded("revive", func(r liveops.AdResult) {
	if r == liveops.AdRewarded {
		revive()
	}
})
```

- `ShowRewarded` reports `AdRewarded` only when the host says the video played to the end, `AdSkipped` when it closed early, `AdNoFill` when the host's error wraps `ErrAdNoFill`, and `AdFailed` for any other error. The callback runs once per show even if the host calls `done` twice.
- `ShowInterstitial` reports `AdCapped` without asking the host when `Disabled` returns true, `InterstitialCap` shows have already been made this session, or less than `InterstitialInterval` has passed since the last one, and `AdShown` when the interstitial closes. `Disabled` leaves rewarded ads alone, since the player chooses to watch them.
- Only one ad shows at a time: a show requested while another is active reports `AdFailed` and returns `ErrAdBusy`.
- `Pause(true)` and `ResetInput` run before the ad shows and `Pause(false)` before the result is reported, so the loop and the mixer stay paused and no key stays held while the ad covers the game.

Not yet available: an `ads.yaml` declaring unit IDs and placements, banner ads, and the mini-game `AdHost`, which waits on NSGo's mini-game ad API ([nsgo#2672](https://github.com/nsgo-dev/nsgo/issues/2672)). Grant currency rewards on the server, keyed by your own show ID, so a reward is not granted twice.

### Crash fingerprints

`liveops.CrashReport.Fingerprint()` returns a stable SHA-256 grouping key derived from the top application stack frames. It omits file paths, line numbers, goroutine IDs and runtime frames, so moving a line or recovering the same panic on another goroutine keeps the same grouping key. Include the fingerprint with an uploaded report, while retaining the full stack for diagnosis.
