# Agent Note: server.Runner recovers a panicking match instead of killing the process

Status: implemented

## Problem

`server.Runner.Run` started each match on its own goroutine with a deferred `OnStop`, but nothing called `recover`, so a panic in one match's `Update` unwound the goroutine and exited the process. Every other match on the runner stopped mid-tick without its `OnStop`, and their peers lost the game (#1314). The existing `liveops.CrashReporter` already recovers panics and builds a JSON `CrashReport`, but `liveops` imports the root `game` package and through it NSGo `core`, while `pkg/online/server` is documented and tested to import no NSGo package (see [`datatable` is NSGo-free](../architecture/2026-11-27-datatable-nsgo-free.md)), so the server could not reuse it.

## Decision

`Runner` gains an `OnPanic func(Panic) PanicAction` hook, and every match's `Update` and `OnStop` runs inside a `recover` (`guardUpdate`, `runOnStop` in `pkg/online/server/server.go`).

- `Panic` carries the match, the recovered value, the goroutine stack (`runtime/debug.Stack`), the match's 1-based tick (0 for an `OnStop` panic) and the recovery time. `Panic.Report()` builds a `crashreport.CrashReport` with the same fields a client report has, recording the match's `Name` as the `match` metadata key.
- `StopMatch` (the zero value and the default when `OnPanic` is nil) ends only the panicking match: `guardUpdate` reports failure, `runMatch` returns so the match never ticks again (its state may be half-updated), and its `OnStop` runs once through the existing defer. Sibling matches keep ticking.
- `Crash` stores the first recovered value in a `panicState`, cancels a `context.WithCancel` derived inside `Run`, and after every match's `OnStop` has run `Run` re-panics with that value.
- A panic inside `OnStop` goes through the same `handlePanic` path, so a failing shutdown hook can no longer kill the process either.

The crash report JSON shape (`CrashReport`, `StepRecord`, the custom `MarshalJSON`) moves to a new package `pkg/online/liveops/crashreport` that imports neither NSGo nor the root `game` package; `liveops` keeps type aliases so existing client code compiles unchanged. The root dependency-boundary test (`deps_test.go`, shared with the datatable work) gains `./pkg/online/liveops/crashreport` so the server and the schema package stay NSGo-free.

## Alternatives considered

- **Link `liveops.CrashReporter` from the server** — strongest argument: one recovery implementation and the ring buffer of recent steps comes along for free. Rejected because `liveops` transitively imports NSGo `core` (`crash.go` via the root `game` package, `analytics.go` and `cloudsync.go` directly), which would break the documented and now test-enforced headless property of `pkg/online/server`.
- **Re-panic immediately from the match goroutine on `Crash`** — simpler than carrying the value to `Run`. Rejected because Go only re-panics the whole process without running the other matches' defers; `Crash` is documented as running every `OnStop` first, which requires unwinding through `Run`.
- **Record per-match step rings server-side like `CrashReporter`** — would fill `LastSteps` in server reports. Rejected as out of scope for size/S: the server has no fixed simulation step recorder today, and `Panic` already carries the tick number; a reporter that wants step history can add `RecordStep` calls inside its own `Update`.

## Consequences

- **Benefits**: One buggy lobby ends with a report instead of taking every lobby on the machine down; server panic files and client crash files share one JSON schema; `OnPanic` nil keeps the old call sites working with the safe default; the NSGo-free boundary of the server and the schema package is pinned by `TestHeadlessPackagesDoNotDependOnNSGo`.
- **Costs and known limits**: After `StopMatch` the match's state may be half-updated and is never ticked again — peers must be told through `OnStop` or the `OnPanic` reporter. `OnPanic` itself is trusted code and is not recovered. A `Crash` re-panic loses the original stack's identity as a panic origin (the original stack survives in `Panic.Stack`).

## Testing

`pkg/online/server/panic_test.go`: `TestRunnerRecoversPanickingMatch` (panicking match stops at its tick, `OnStop` once, sibling keeps ticking, `Panic` fields populated), `TestRunnerRecoversPanickingMatchWithoutOnPanic` (default `StopMatch`), `TestRunnerCrashActionStopsAllAndRepanics` (every `OnStop` runs, `Run` re-panics with the value), `TestRunnerRecoversOnStopPanic` (reported with `Tick` 0, sibling unaffected) and `TestPanicReportMatchesCrashSchema` (JSON round-trip through `crashreport.CrashReport`). `TestHeadlessPackagesDoNotDependOnNSGo` at the repository root covers the new package. Verified with `go tool nsgo check` and `go tool nsgo test` on the touched packages.
