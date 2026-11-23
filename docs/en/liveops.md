# Live operations

```go
import "github.com/nsgo-dev/game/packages/game/liveops"
```

`liveops` holds provider-agnostic live operations primitives for NSGo games. It uses only Go's standard library, `packages/game` and NSGo's public `core` API, and links no third-party crash, analytics or ad SDKs.

## Scope decision ([#194](https://github.com/nsgo-dev/game/issues/194))

| Area | Status | Rationale |
| --- | --- | --- |
| **Crash reports** ([#195](https://github.com/nsgo-dev/game/issues/195)) | In scope | Provider-agnostic panic capture with a fixed-size step ring buffer, stack trace, metadata and optional state snapshot. |
| **Analytics** ([#198](https://github.com/nsgo-dev/game/issues/198)) | In scope | Provider-agnostic, strictly consent-gated event API backed by `core.Storage`. Zero events are sent or buffered before explicit opt-in consent. |
| **Cloud saves** ([#196](https://github.com/nsgo-dev/game/issues/196)) | In scope (interface) | Provider-agnostic `CloudSync` interface and `CloudSyncer` conflict resolution ([#539](https://github.com/nsgo-dev/game/issues/539)) today; built-in platform adapters (iCloud, Play Games, Steam Cloud) wait on platform cloud-save bindings. |
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

`CloudSync` defines the provider-agnostic slot interface (`Load`, `Save`, `Delete`, `List`, with sentinel errors `ErrSlotNotFound` and `ErrCloudUnavailable`). Games can implement it over their own backend or an in-memory test double today; platform adapters for iCloud, Google Play Games and Steam Cloud wait on platform cloud-save bindings ([#196](https://github.com/nsgo-dev/game/issues/196)).

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
		return showConflictDialog(c.Local, c.Cloud) // ChoiceLocal, ChoiceCloud, ChoiceBoth or ChoiceDefer
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
```

How a conflict is detected:

- For each slot the syncer remembers the base: the content hash (`sha256:…`) and cloud revision both sides held after the last sync, kept as JSON text in `State` under `StateKey` (`DefaultSyncStateKey` when empty).
- `Sync` visits every slot known locally, in the cloud or in the sync state, in name order. When only one side differs from the base, that side is copied to the other (`OutcomeUploaded`, `OutcomeDownloaded`, `OutcomeDeletedCloud`, `OutcomeDeletedLocal`). When both differ from the base and from each other, the slot is a conflict. A device that has never synced a slot and holds a different copy from the cloud is a conflict too, never an overwrite.
- Before every cloud write the syncer reads the cloud copy again; if another device uploaded in between, the slot fails with `ErrCloudChanged`, nothing is written, and the next sync sees the new copy.
- A slot that fails (a corrupt payload, an unsafe slot name, `Ask` returning an error) is skipped and its error joined into the result while the others still sync; `ErrCloudUnavailable` or a done context stops the sync.

Each `Conflict` carries a `SaveVersion` for both sides: whether it exists (a side may have deleted the slot), its revision, hash, modified time, writing device, the game's `Summarize` text and the raw data. The policies:

| Policy | Behaviour |
| --- | --- |
| `PolicyNewest` (default) | Keeps the copy with the later modified time and overwrites the other (`OutcomeKeptLocal` or `OutcomeKeptCloud`). When either time is unknown, such as a write that bypassed `Backend()`, or both are equal, it keeps both. When one side deleted the slot and the other changed it, it keeps the changed copy. |
| `PolicyKeepBoth` | Keeps the cloud copy in the slot and moves the local copy to `<slot>.conflict-<device>` (then `-2`, `-3`…) on both sides (`OutcomeKeptBoth`, `SlotResult.Fork`). The fork is written locally before anything is overwritten. A deletion against a change keeps the change. |
| `PolicyAsk` | Calls `Ask` with the conflict and applies its `Choice`: `ChoiceLocal`, `ChoiceCloud`, `ChoiceBoth` (as `PolicyKeepBoth`) or `ChoiceDefer`, which writes nothing and asks again on the next sync. An error from `Ask` leaves both copies untouched. |

Whenever a resolution replaces a copy, `SlotResult.Discarded` returns it so the game can show or archive it, and the local backend keeps the replaced local copy as its backup (`ReadBackup`).

Cloud payloads are JSON text with a fixed key order: `format` (`CloudSaveFormat`), `version` (`CloudSaveVersion`), `revision`, `hash`, `modified` (RFC 3339, UTC), `device` and `data`, a string holding the save as written (or `data_base64` for data that is not UTF-8). `DecodeCloudSave` refuses unknown fields, newer versions, trailing data and a hash that does not match the data with `ErrCorruptCloudSave`; decoding a payload `EncodeCloudSave` wrote and encoding it again gives back the same bytes. A cloud payload without the format marker, written by a game that used `CloudSync` directly, is read as revision 0 and upgraded on the next upload. Cloud slot names are checked with `save.ValidateSlot` before they reach the local backend.

A `CloudSyncer` is not safe for concurrent use; call it where the app's code runs, as `core.Storage` requires.
