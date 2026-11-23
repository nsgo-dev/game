# Live operations

```go
import "github.com/nsgo-dev/game/packages/game/liveops"
```

`liveops` holds provider-agnostic live operations primitives for NSGo games. It uses only Go's standard library, `packages/game` and NSGo's public `core` API, and links no third-party crash, analytics, ad or payment SDKs.

## Scope decision ([#194](https://github.com/nsgo-dev/game/issues/194))

| Area | Status | Rationale |
| --- | --- | --- |
| **Crash reports** ([#195](https://github.com/nsgo-dev/game/issues/195)) | In scope | Provider-agnostic panic capture with a fixed-size step ring buffer, stack trace, metadata and optional state snapshot. |
| **Analytics** ([#198](https://github.com/nsgo-dev/game/issues/198)) | In scope | Provider-agnostic, strictly consent-gated event API backed by `core.Storage`. Zero events are sent or buffered before explicit opt-in consent. |
| **Cloud saves** ([#196](https://github.com/nsgo-dev/game/issues/196)) | In scope (interface and self-hosted backend) | Provider-agnostic `CloudSync` interface, `CloudSyncer` conflict resolution ([#539](https://github.com/nsgo-dev/game/issues/539)) and `CloudClient` for the self-hosted [cloudsave](cloudsave.md) service ([#557](https://github.com/nsgo-dev/game/issues/557)) today; built-in platform adapters (iCloud, Play Games, Steam Cloud) wait on platform cloud-save bindings. |
| **In-app purchases** ([#369](https://github.com/nsgo-dev/game/issues/369)) | In scope | `Purchases` over NSGo's purchase services: consumables credited into the save exactly once, unlocks and subscriptions from the store's entitlements, restore, Ask to Buy and an optional server check. |
| **Ads** ([#199](https://github.com/nsgo-dev/game/issues/199)) | Out of scope | Ad networks (AdMob, AppLovin and similar) require proprietary native SDKs with heavy store-policy and privacy obligations. Keep them out of the core `game` module; games that need ads embed native SDK views directly via NSGo. |

## Crash reports

`CrashReporter` keeps the most recent simulation steps in a pre-allocated ring buffer (`DefaultHistorySize` = 120, zero allocations in steady state) and guards `game.Loop` callbacks or arbitrary functions against panics.

- `RecordStep(dt, summary)` and `RecordStepData(dt, summary, data)` append a `StepRecord` to the ring buffer and return its monotonic step number.
- `WrapLoop(l)` returns a guarded `*game.Loop`; `GuardLoop(l)` wraps `l.Update` and `l.Draw` in place. When `Update` does not call `RecordStep` itself during a step, the guard records an `"update"` step automatically, including for the step that panicked.
- `RunSafe(fn)` guards any callback and reports whether it panicked.
- On panic, the reporter builds a JSON-marshalable `CrashReport` holding `Timestamp`, `Panic`, `Message`, `Stack` (`runtime/debug.Stack`), `Step`, chronological `LastSteps` (oldest to newest), optional `State` from `CaptureState`, and a copy of `Metadata`, then passes it to `Reporter`.
- Set `Rethrow: true` when the process should still panic after `Reporter` finishes persisting or shipping the report.

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

`CloudSync` defines the provider-agnostic slot interface (`Load`, `Save`, `Delete`, `List`, with sentinel errors `ErrSlotNotFound` and `ErrCloudUnavailable`, and `ErrSlotTooLarge` and `ErrTooManySlots` for a backend's limits). `ConditionalCloudSync` adds `SaveIf` and `DeleteIf`, which write only while the slot still holds the payload the caller read, identified by `PayloadHash`.

`CloudClient` is the `ConditionalCloudSync` of the self-hosted [cloudsave](cloudsave.md) service ([#557](https://github.com/nsgo-dev/game/issues/557)): per-account slots over HTTPS, signed in through the [account service](accounts.md), with a size limit and the last few versions of each slot kept for `Versions`, `LoadVersion` and `Restore`. Games can also implement `CloudSync` over their own backend or an in-memory test double; platform adapters for iCloud, Google Play Games and Steam Cloud wait on platform cloud-save bindings ([#196](https://github.com/nsgo-dev/game/issues/196)).

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
- Before every cloud write the syncer reads the cloud copy again; if another device uploaded in between, the slot fails with `ErrCloudChanged`, nothing is written, and the next sync sees the new copy. When `Cloud` implements `ConditionalCloudSync`, as `CloudClient` does, the write itself is conditional on that copy, so an upload landing after the re-read fails the same way instead of being overwritten.
- A slot that fails (a corrupt payload, an unsafe slot name, `Ask` returning an error) is skipped and its error joined into the result while the others still sync; `ErrCloudUnavailable` or a done context stops the sync.

Each `Conflict` carries a `SaveVersion` for both sides: whether it exists (a side may have deleted the slot), its revision, hash, modified time, writing device, the game's `Summarize` text and the raw data. The policies:

| Policy | Behaviour |
| --- | --- |
| `PolicyNewest` (default) | Keeps the copy with the later modified time and overwrites the other (`OutcomeKeptLocal` or `OutcomeKeptCloud`). When either time is unknown, such as a local copy written without `Backend()` (a recorded time counts only for the exact copy it was recorded with), or both are equal, it keeps both. When one side deleted the slot and the other changed it, it keeps the changed copy. |
| `PolicyKeepBoth` | Keeps the cloud copy in the slot and moves the local copy to `<slot>.conflict-<device>` (then `-2`, `-3`…) on both sides (`OutcomeKeptBoth`, `SlotResult.Fork`). When that name would exceed the 200-byte slot limit, the slot part is shortened and followed by `~` and 8 hex digits of its SHA-256. The fork and its sync state are written locally before anything is overwritten, so if the upload fails the next sync uploads the same fork rather than creating another. A deletion against a change keeps the change. |
| `PolicyAsk` | Calls `Ask` with the conflict and applies its `Choice`: `ChoiceLocal`, `ChoiceCloud`, `ChoiceBoth` (as `PolicyKeepBoth`) or `ChoiceDefer`, which writes nothing and asks again on the next sync. An error from `Ask` leaves both copies untouched. |

`Ask` runs inside the sync, on the app's thread, so it must not wait for the player. To ask through a dialog, keep the `Conflict`, return `ChoiceDefer`, show both summaries, and apply the answer with `Resolve(ctx, conflict, choice)`, as in the example above. `Resolve` reads both sides again and applies the choice only while they still hold the copies the player saw (the same existence, hash and cloud revision); otherwise it writes nothing and returns `ErrConflictChanged`. It works under every policy.

Whenever a resolution replaces a copy, `SlotResult.Discarded` returns it so the game can show or archive it. When the replaced local copy is a valid save envelope and the cloud copy exists, the local backend also keeps it as its backup (`ReadBackup`); when the cloud copy is a deletion, or the local copy is not a save envelope, `Discarded` is the only copy left.

Cloud payloads are JSON text with a fixed key order: `format` (`CloudSaveFormat`), `version` (`CloudSaveVersion`), `revision`, `hash`, `modified` (RFC 3339, UTC), `device` and `data`, a string holding the save as written (or `data_base64` for data that is not UTF-8). `DecodeCloudSave` refuses unknown fields, newer versions, trailing data and a hash that does not match the data with `ErrCorruptCloudSave`; decoding a payload `EncodeCloudSave` wrote and encoding it again gives back the same bytes. A cloud payload without the format marker, written by a game that used `CloudSync` directly, is read as revision 0 and upgraded on the next upload. Cloud slot names are checked with `save.ValidateSlot` before they reach the local backend.

`Backend()` exposes the local backend through `Unwrap`, so a settings store built on it (`save.NewDefaultSettingsStore(syncer.Backend())`) keeps settings under the local backend's own settings key, and settings are never synced as a slot.

A `CloudSyncer` is not safe for concurrent use; call it where the app's code runs, as `core.Storage` requires. With a plain `CloudSync`, which has no conditional write, an upload by another device that lands between the syncer's re-read and its own write can still be overwritten; the window is a single `Load` and `Save` apart. A `ConditionalCloudSync` closes it.

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
