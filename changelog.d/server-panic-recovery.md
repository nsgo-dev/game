### Added

- `pkg/online/server`: a panic in one match's `Update` or `OnStop` is recovered and reported through `Runner.OnPanic`; the default `StopMatch` action ends only that match while sibling matches keep running, and `Crash` stops every match, runs every `OnStop`, then re-panics from `Run`. `Panic.Report` builds a crash report with the client's JSON schema (#1314).
- `pkg/online/liveops/crashreport`: new NSGo-free package holding the `CrashReport` and `StepRecord` JSON schema, kept as aliases in `pkg/online/liveops` so existing code is unchanged (#1314).
