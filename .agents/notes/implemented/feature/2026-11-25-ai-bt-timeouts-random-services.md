# Agent Note: Behavior tree timeouts, retries, random choices, and services in game/ai

Status: implemented

## Problem

`game/ai`'s initial behavior tree (`packages/game/ai/bt.go`) provides core composites (`Sequence`, `Selector`, `Parallel`) and basic decorators (`Guard`, `Cooldown`, `Inverter`, `Succeeder`, `Failer`, `Repeater`, `UntilFail`), leaving games to reimplement four recurring patterns: aborting stuck running actions after a game-time budget, retrying transient failures, deterministic randomized/weighted branch choices for replayable combat and idle variety, and periodic perception/target refresh scoped to an active branch.

These additions live in `game/ai` (a **Stable** package) and must stay deterministic on fixed `game.Loop` steps, zero-allocation per `Tick`, safe against nil inputs and extreme durations/weights, and ready for a future YAML behavior tree format.

## Decision

Add six behavior tree nodes (plus `WeightedNode`) in dedicated files `packages/game/ai/bt_ext.go` and `packages/game/ai/bt_ext_test.go`, keeping `bt.go` untouched to isolate changes from concurrent `game/ai` work:

### 1. `TimeLimit(d, child)` evaluates child before checking timeout

`TimeLimit` ticks `child` first and only accumulates positive `dt` and checks `t.limit <= 0 || t.elapsed >= t.limit` if `child` returned `StatusRunning`. Checking after `child.Tick(dt)` ensures a child such as `Wait(100ms)` under `TimeLimit(100ms, ...)` stepped at `50ms` receives its full `100ms` budget and succeeds on the boundary tick instead of failing one tick early. When the limit expires while still running, `TimeLimit` clears `elapsed`, calls `child.Reset()`, and returns `StatusFailure`.

### 2. `Retry(n, child)` mirrors `Repeater` with one attempt per tick

`Retry(n, child)` allows up to `n` retries after an initial failure (`1+n` total attempts; `n <= 0` performs zero retries and fails immediately). Like `Repeater` and `UntilFail` in `bt.go`, it ticks `child` once per `Tick(dt)` and returns `StatusRunning` between failed attempts so fixed-step loops never spin synchronously inside a single frame. `StatusRunning` ticks from a multi-tick child do not consume retries.

### 3. Deterministic, zero-allocation random composites (`RandomSelector`, `RandomSequence`, `WeightedSelector`)

- `RandomSelector` and `RandomSequence` preallocate an `order []int` index slice at construction. At the start of each run, `order` is reset to `0..len-1` and shuffled in place via `random.Shuffle(stream, order)` (or left in declaration order when `stream == nil`). Resetting `order` to canonical indices before every shuffle ensures that resetting both `stream` and the node reproduces the exact same permutation as a freshly constructed node.
- Both nodes keep the chosen order and cursor across `StatusRunning` ticks until completion or `Reset()`. Unlike priority `Selector`, `RandomSelector` does not re-tick earlier failed children in the shuffled order while a chosen child is running.
- `WeightedSelector(stream, options...)` pairs `Node` and `Weight` in `WeightedNode`, sanitizes weights at construction (treating `<= 0`, `NaN`, `Inf`, or `nil` nodes as ineligible without panicking), and draws lazily without replacement from a preallocated `remWeights []float64` buffer. Each draw normalizes remaining weights by `maxW` so extreme finite weights (such as `1e308`) never overflow `float64` sums and there is zero subtraction drift. When `stream == nil`, eligible options are tried in descending `Weight` order with declaration-order tie-breaking.

### 4. `Service(interval, fn, child)` preserves phase and conserves elapsed time

`Service` runs `fn(dt)` immediately on entry before `child.Tick(dt)` so leaves on the branch observe refreshed state on the very first tick. While `child` remains `StatusRunning`, `Service` tracks both a phase accumulator (`phase %= interval` on fire, capped at one fire per tick) and `sinceLast` (exact game time accumulated since `fn` last ran, passed to `fn`). When `interval <= 0`, `fn` runs every tick while active. Completion (`StatusSuccess` or `StatusFailure`) or `Reset()` deactivates the service so the next activation runs `fn` on entry again.

## Alternatives considered

### Checking `TimeLimit` elapsed time before ticking `child`

Accumulating `dt` and failing before calling `child.Tick(dt)` would reject `TimeLimit(100ms, Wait(100ms))` at `dt = 50ms` after only 50ms of child progress. Checking after `child.Tick(dt)` aligns the budget boundary with `Wait` and `Cooldown`.

### Retrying synchronously inside one `Tick` loop in `Retry`

Looping up to `1+n` times within a single `Tick` would handle instantaneous stateless failures in one frame, but breaks symmetry with `Repeater` and `UntilFail` in `bt.go` and would cause unexpected burst side effects on fixed-step `game.Loop` ticks. Ticking once per `Tick` matches `game/ai`'s existing decorator contract.

### Using `stream.WeightedIndex` directly in `WeightedSelector`

`random.Stream.WeightedIndex` panics on negative, `NaN`, or infinite weights and on `float64` sum overflow. Normalizing by `maxW` on sanitized preallocated weights avoids panics on malformed or extreme weights while keeping identical proportional draws and zero allocations.

### Passing frame `dt` instead of elapsed interval time to `Service`

Passing raw frame `dt` on every periodic service tick would under-report elapsed time for services running slower than frame rate (e.g. reporting `16ms` for a `250ms` service). Passing `sinceLast` (and `dt` on entry) conserves total simulated game time while keeping non-divisor step timing drift-free via `phase %= interval`.

## Consequences

- **Benefits**: Adds timeouts, retries, deterministic random/weighted selection, and branch-scoped services to `game/ai` with `0 allocs/op` on `Tick`, full same-seed replayability via `*random.Stream`, no experimental dependencies, and clean constructor signatures ready for future YAML behavior trees.
- **Costs**: `WeightedSelector` adds one small exported struct (`WeightedNode`) to `game/ai`'s stable API surface; `Retry` advances one failed attempt per tick rather than retrying synchronously in one tick.

## Verification

- `packages/game/ai/bt_ext_test.go` exercises every node with table-driven fixed-`dt` tests, nil/zero/negative/NaN/Inf edge cases, mid-run `Reset()`, same-seed replayability across independent runs and node+stream resets, weighted distribution sanity checks, nested composition inside `game.Loop`, and `testing.AllocsPerRun` (`0 allocs/op`).
- Verified with `gofmt -l packages/game/ai`, `go tool nsgo test ./ai/...`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
