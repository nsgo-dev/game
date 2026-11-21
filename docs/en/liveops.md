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
| **Cloud saves** ([#196](https://github.com/nsgo-dev/game/issues/196)) | In scope (interface) | Provider-agnostic `CloudSync` interface today; built-in platform adapters (iCloud, Play Games, Steam Cloud) wait on platform cloud-save bindings. |
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
