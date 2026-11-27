# Agent Note: Schedule server.Runner ticks against a fixed deadline

Status: implemented

## Problem

`server.Runner.runMatch` (`pkg/online/server/server.go`) previously re-armed its timer with a full `step` (`timer.Reset(step)`) after every `m.Update(step)` returned. Every tick therefore took `step + Update` duration instead of `step`, so normal updates that stayed well under one step still slowed the match proportionally to CPU load (for example, a 100 Hz match with a 5 ms update ran near 57–67 Hz, and a 60 Hz match with an 8 ms update ran near 40 Hz) without ever triggering `OnOverrun`. Because clients predict and interpolate at the declared `Rate`, silent load-dependent tick drift desynchronizes authoritative server simulation from wall time (#1598).

## Decision

`Runner.runMatch` tracks a cumulative target deadline `next` initialized to `time.Now().Add(step)` and advances `next = next.Add(step)` after each `m.Update(step)` completes.

- When `now` is not past `next` (`!now.After(next)`), the timer resets to the remaining budget `next.Sub(now)`. Both `Update`'s runtime and minor OS timer wakeup jitter are absorbed out of the step budget rather than accumulating across ticks.
- When `now.After(next)`, the tick overran its deadline: `Runner` computes `lag := now.Sub(next)` and `missed := max(1, int(lag/step))`, invokes `OnOverrun` if set, refreshes `now` after `OnOverrun`, and resets `next = now.Add(step)` with `timer.Reset(step)`. Missed ticks are never caught up in a burst, and a slow `OnOverrun` callback does not eat into the next tick's budget.
- `Match.interval()` clamps non-positive computed steps to `time.Nanosecond` so extreme `Rate` values cannot cause a division by zero in `int(lag / step)`.
- Deterministic tick-rate and overrun regression tests use Go's standard `testing/synctest.Test` bubble instead of adding a fake clock hook to `Runner`'s public API.

## Alternatives considered

- **Per-tick deadline (`start.Add(step)` inside each iteration)**: simpler than maintaining `next` across iterations and removes `Update` duration from the sleep, so a 5 ms update at 100 Hz gets close to 100 Hz on an idle host. Rejected because OS timer wakeup slack (commonly 0.5–1 ms per wakeup) still accumulates permanently into every subsequent `start`, causing steady downward drift at higher tick rates.
- **Catch-up loop on overrun (advancing `next` without resetting to `now + step`)**: preserves exact long-term average tick count after a temporary stall. Rejected because `server.Runner` explicitly guarantees no catch-up bursts (`docs/en/server.md`, `docs/zh/server.md`): under sustained overload, catch-up bursts create a spiral of death and starve shutdown and network I/O.
- **Add an injectable `Now` / `After` clock interface to `server.Runner`**: matches `Matchmaker.Now` and `Guard.Now` and allows unit tests without Go 1.25+ `testing/synctest`. Rejected because `Runner` needs both clock reads and resettable timers, which would widen the public API solely for tests, while `go.mod` targets Go 1.27 and `testing/synctest` gives exact, race-free virtual time with zero API surface change.

## Consequences

- **Benefits**: Matches hold their configured `Rate` when `Update` fits within `1/Rate`, OS wakeup jitter does not drift the long-term cadence, overruns keep their documented no-burst reset behavior, and regression tests run deterministically in milliseconds under `testing/synctest`.
- **Costs and known limits**: A tick whose timer woke slightly late and whose `Update` finished past `next` (even if `Update` itself took slightly less than `step`) reports an overrun, because the schedule deadline was genuinely missed. After any overrun, wall-clock phase shifts to `now + step` rather than snapping back to the original phase grid.

## Testing

- `pkg/online/server/server_test.go`: `TestRunnerHoldsRateUnderHalfStepUpdate` (100 Hz / 5 ms and 60 Hz / 8 ms), `TestRunnerOverrunResetsScheduleWithoutBurst` (single- and multi-step overruns with resumption of deadline cadence), `TestRunnerExactStepUpdateDoesNotOverrunOrSkip`, and `TestRunnerCancelInsideOverrunStopsImmediately`, alongside all existing real-clock `Runner` tests.
- Verified with `go tool nsgo test ./pkg/online/server...` and `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/online/server`.
