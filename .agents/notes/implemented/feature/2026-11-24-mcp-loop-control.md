# Agent Note: MCP loop control and deterministic seeding

Status: implemented

## Problem

With [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md) in place, coding agents can read live game state over `nsgo mcp`, but cannot control simulation time: they cannot pause the loop to inspect a frozen frame, advance an exact number of fixed updates synchronously, adjust time scale, or reset the deterministic random seed for reproducible runs. In addition, `game.Loop.Step(n)` queues updates for the next display frame only while paused (leaving immediate follow-up MCP reads stale), and accumulating `60 * (time.Second / 60)` in integer nanoseconds drops 40 ns per second (`0.99999996 s`) instead of advancing exactly `1.0` second.

## Decision

Add a loop-control tool group in `packages/game/mcp/loop.go` attached through `Bridge.Tools()`, backed by minimal additive helpers on `game.Loop` (`Steps`, `Elapsed`, `ElapsedSeconds`, `StepRate`, `StepDuration`, `EffectiveTimeScale`, `StepNow`) and `game/random` (`Stream.SetSeed`, `Streams.SetSeed`).

## Tool contract and loop invariants

- **Six `game_loop_*` tools**: `game_loop_status`, `game_loop_pause`, `game_loop_resume`, `game_loop_step`, `game_loop_time_scale`, and `game_loop_seed` follow the package's `snake_case` naming and strict `DisallowUnknownFields()` argument decoding, with pointer fields (`*int`, `*float64`, `*uint64`) distinguishing omitted optional arguments from explicit zero values.
- **Synchronous stepping (`Loop.StepNow`)**: `game_loop_step` executes `count` fixed updates synchronously on the engine goroutine (`1 <= count <= MaxLoopSteps`, default `1`, `MaxLoopSteps = 3600`) whether the loop is paused or running, and invokes `Draw` once afterward (unless `Update` stopped an active `Run`). Immediate follow-up `game_entity_inspect` or `game_component_get` calls observe the stepped state without waiting for the next display vsync.
- **Exact simulated game time**: `game.Loop` tracks executed steps per contiguous `Rate` segment (`prevElapsed + float64(segSteps)/float64(segRate)`). `Update` continues to receive the fixed `time.Second / time.Duration(rate)` step, while `ElapsedSeconds()` and `Elapsed()` avoid integer-nanosecond truncation drift so 60 updates at 60 Hz advance by exactly `1.0` second (`time.Second`).
- **Time scale semantics**: `game_loop_time_scale` rejects negative, `NaN`, and infinite values, updates `Loop.TimeScale` via `Loop.SetTimeScale`, and returns the effective `time_scale` (`1.0` when `Loop.TimeScale == 0`, matching `game.Loop`'s zero-value convention; freezing simulation uses `game_loop_pause`).
- **Deterministic seeding**: `Config` accepts `Random *random.Stream`, `RandomStreams *random.Streams`, and/or `SetSeed func(uint64)`. `Stream.SetSeed` and `Streams.SetSeed` re-seed existing stream pointers in place (including already-forked child streams on `Streams`) so existing references in game systems immediately draw from the new seed. Calling `game_loop_seed` when none of those fields is configured returns an actionable error.
- **Additive rebase boundary**: All loop-control constants, schemas, result structs, `Bridge.SetLoop`, and handlers live in `packages/game/mcp/loop.go`; `packages/game/mcp/mcp.go` only adds the `Config` fields and appends `b.loopTools()` in `Tools()` so sibling MCP tool PRs (#463–#469) rebase cleanly.

## Alternatives considered

### Queueing steps through existing `Loop.Step(n)` without adding `Loop.StepNow`

Strongest argument: `Loop.Step(n)` already exists on Stable `game.Loop`, so `game/mcp` could call it without adding any exported method to `packages/game`.

Rejected because `Loop.Step(n)` defers execution to the next display frame and is ignored whenever `Loop.Paused` is `false`. An MCP agent calling `game_loop_step` followed immediately by `game_entity_inspect` (or running under `nsgotest` without manually pumping `app.Advance`) would read pre-step state, and stepping while unpaused would silently do nothing.

### Accumulating game time as `steps * (time.Second / time.Duration(rate))`

Strongest argument: summing the exact `time.Duration` passed to `Update` needs only one `time.Duration` field on `Loop`.

Rejected because `time.Second / 60` truncates to `16_666_666 ns`, making 60 updates sum to `0.99999996 s` instead of `1.0 s`, failing exact one-second acceptance checks at 60 Hz and accumulating 40 ns of drift every second. Tracking contiguous `(segSteps, segRate)` segments preserves exact rational seconds (`60 / 60 == 1.0`) with zero allocations and handles mid-run `Rate` changes.

### Requiring only `Config.SetSeed func(uint64)` without `Random` / `RandomStreams`

Strongest argument: a single callback covers every custom RNG setup and avoids adding `SetSeed` to `packages/game/random`.

Rejected because most games in `packages/game` use `random.Stream` or `random.Streams` directly; supporting direct wiring of `Config.Random` and `Config.RandomStreams` alongside `Config.SetSeed` removes boilerplate while keeping custom re-seed hooks possible.

## Consequences

- **Benefits**: Agents can pause, resume, step exact simulation durations (with 60 updates at 60 Hz advancing exactly `1.0` second), tune time scale, and replay identical deterministic sequences via `game_loop_seed`, with immediate visibility to inspection tools and clean errors when unconfigured or given invalid arguments.
- **Costs**: `game.Loop` gains 40 bytes of unexported step/time tracking fields and seven additive methods, and `game/random` gains two additive `SetSeed` methods; both remain backward-compatible with Stable package guarantees and add zero heap allocations per step.

## Verification

Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./mcp`, `go tool nsgo test .`, `go tool nsgo test ./random`, `go run ./tools/changelog check`, and `bun run verify-notes`.
