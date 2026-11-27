# Determinism

A deterministic simulation produces the exact same state from the same starting seed and inputs on every run, across macOS, iOS, Windows, Linux, Android and the web. Determinism makes replays, seeded daily runs, save/load continuity and automated gameplay tests under `nsgotest` reliable instead of flaky.

This guide describes the rules that keep an NSGo game deterministic and how to verify them with `pkg/random`.

## 1. Advance time only with `game.Loop`'s fixed `dt`

Move simulation state exclusively inside `game.Loop.Update(dt)` using the fixed step `dt` (or an integer tick counter). Never read `time.Now()`, `time.Since`, or frame-to-frame display intervals inside simulation logic.

```go
loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		sim.Tick++
		sim.Step(dt) // dt is always 1/60s
	},
	Draw: func(alpha float64) {
		// Interpolate visuals only; never mutate simulation state here.
		render(sim.Prev, sim.Curr, alpha)
	},
}
```

- `Draw(alpha)` runs at the display's refresh rate and must be read-only with respect to gameplay state.
- Count cooldowns and timers in integer ticks or exact multiples of `dt` rather than wall-clock deadlines.

## 2. Use seeded `random.Stream` per purpose

Never call Go's global `math/rand` or `math/rand/v2` inside simulation code. Global generators share hidden mutable state across subsystems, and standard library helpers can change algorithms across Go releases.

Import `github.com/nsgo-dev/game/pkg/random` instead:

```go
import "github.com/nsgo-dev/game/pkg/random"

streams := random.NewStreams(masterSeed)
ai := streams.Stream("ai")
loot := streams.Stream("loot")
particles := streams.Stream("particles")
```

- Every `Stream` runs a self-contained SplitMix64 + Xoshiro256\*\* generator with fixed rejection sampling (`Intn`, `IntRange`, `Float64`, `Float32`, `Range`, `Range32`, `Bool`, `Chance`, `Pick`, `WeightedIndex`, `Shuffle`). The output sequence is bit-for-bit identical across Go versions, operating systems and CPU architectures (32-bit, 64-bit, ARM64, x64 and WebAssembly).
- Beyond uniform draws, `Normal(mean, stddev)` draws from a normal distribution as a fixed 12-draw Irwin-Hall sum, and `InCircle(r)`, `OnCircle()` and `InRect(x, y, w, h)` return uniformly distributed 2D points inside a circle, on the unit circle (a random direction) and inside a rectangle. They deliberately avoid `math.Sin`, `math.Cos`, `math.Log` and `math.Exp` (which round differently across architectures), so their results are bit-for-bit identical everywhere too.
- Forking by purpose (`root.Fork("loot")`, `root.Fork("level-1").Fork("spawns")`, or `streams.Stream("loot")`) derives independent child streams without advancing the parent or perturbing siblings. Adding particle effects or reordering stream creation will not change enemy AI or loot drops.
- Keep cosmetic randomness (particles, camera shake, sound pitch variation) on its own stream so visual tweaks never alter gameplay.

## 3. Never depend on Go map iteration order

Go intentionally randomizes `for k, v := range m` iteration order. Iterating a map directly in order-sensitive gameplay code—such as applying damage, resolving collisions, spawning waves or drawing from a `random.Stream`—causes runs to diverge.

- Prefer slices ordered by creation or stable entity ID for anything `Update` iterates over.
- When you must iterate a map, sort its keys first:

```go
for _, id := range slices.Sorted(maps.Keys(enemies)) {
	enemies[id].Update(dt, ai)
}
```

## 4. Keep simulation single-threaded and order-independent of goroutines

Run all simulation mutations sequentially on the engine's goroutine inside `Loop.Update`.

- Do not let multiple goroutines mutate game state or draw from the same `random.Stream`.
- Do not make gameplay decisions from `select` over multiple ready channels, because Go chooses among ready cases pseudo-randomly.
- If background work (such as pathfinding or procedural chunk generation) runs off-thread, deliver its result at a deterministic tick boundary and apply queued results sorted by a canonical key rather than arrival order.

## 5. Keep floating-point evaluation order consistent (and prevent arm64 FMA drift)

IEEE-754 floating-point addition and multiplication are not associative: `(a + b) + c` can differ in the last bit from `a + (b + c)`. On arm64, Go's compiler is also allowed by the language specification to contract `a*b + c` into a single fused multiply-add instruction (`FMADD`/`FMSUB`) with only one rounding instead of two, whereas amd64 executes separate multiply and add instructions. Over hundreds of ticks, even a 1-ULP difference between arm64 and amd64 can cross a collision or branch threshold and cascade.

- Accumulate forces, velocities and scores in a fixed, deterministic entity order.
- In custom simulation code that must match bit-for-bit across amd64 and arm64, wrap multiply-add products in an explicit conversion (`float32(a*b) + c` or `float64(a*b) + c`), which the Go specification guarantees forbids FMA contraction, and avoid standard library transcendentals (`math.Sin`, `math.Cos`, `math.Atan2`, `math.Hypot`) whose arm64 implementations emit FMA, and `math.Pow`, `math.Exp` and `math.Log`, which take different code on each architecture (`math.Exp` has its own assembly on amd64 and on arm64, `math.Log` is assembly on amd64 only) and round differently.
- `pkg/physics`, `pkg/physics3d`, and `pkg/vehicle3d` already eliminate arm64 FMA instructions across their stepped simulation paths and verify cross-architecture golden trajectory hashes in CI on both Ubuntu amd64 and macOS arm64. In `pkg/physics` that covers the rigid world, the XPBD `SoftWorld`, the character controller and tile collision, and on arm64 its tests also disassemble the package with `go tool objdump` and fail on any `FMADD`, `FMSUB`, `FNMADD` or `FNMSUB` outside the debug drawing code.
- Avoid mixing `float32` and `float64` inconsistently across code paths.
- Use integer ticks or fixed-point values for exact counters, timers, grids and resource totals.

## 6. Save and restore PRNG and physics state for saves, replays, and rollback

A `Stream`'s full state (its seed and 256-bit generator words) can be snapshotted with `State()` and restored with `SetState(state)`. `State` marshals cleanly with `encoding/json`, so a saved game resumes the exact same future random sequence after reload:

```go
// Save
data, err := json.Marshal(streams.States())

// Load
var saved map[string]random.State
if err := json.Unmarshal(data, &saved); err == nil {
	streams.Restore(saved)
}
```

For a single stream, use `s.State()` and `s.SetState(state)`. Call `s.Reset()` or `streams.Reset()` to rewind to the start of a seed for replays or level restarts.

For physics worlds, `physics.World`, `physics.RigidWorld`, `physics3d.World`, and `vehicle3d.Vehicle` all provide `Save()`, `Restore(snap)`, and `Checksum()` capturing every piece of private solver state (warm-start impulse caches, sleep timers, previous poses, active contact/trigger pair sets, and joint state). Snapshots marshal cleanly to plain-text JSON and pair directly with `netplay.RollbackSession` (`netplay.SimState`):

```go
// Inside a netplay.SimState implementation:
func (s *MatchSim) Save() []byte {
	data, _ := json.Marshal(s.world.Save())
	return data
}
func (s *MatchSim) Load(b []byte) error {
	var snap physics3d.WorldSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	return s.world.Restore(snap)
}
func (s *MatchSim) Checksum() uint64 { return s.world.Checksum() }
```

## 7. Verify determinism in tests under `nsgotest`

The `random` package includes test helpers that execute a simulation multiple times from scratch and fail if any run diverges.

Use `AssertDeterministic` to check a full run, including a real `game.Loop` driven by `nsgotest`:

```go
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestSimulationIsDeterministic(t *testing.T) {
	random.AssertDeterministic(t, 4, func() WorldSnapshot {
		streams := random.NewStreams(42)
		world := NewWorld(streams)
		loop := &game.Loop{
			Rate:   60,
			Update: func(dt time.Duration) { world.Step(dt) },
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(2 * time.Second)
		return world.Snapshot()
	})
}
```

When a multi-step simulation diverges, use `CheckSteps` or `AssertDeterministicSteps` to pinpoint the exact first step where two runs differ and inspect both states:

```go
func TestStepByStepDeterminism(t *testing.T) {
	random.AssertDeterministicSteps(t, 300, func() func(step int) WorldSnapshot {
		world := NewWorld(random.NewStreams(42))
		dt := time.Second / 60
		return func(step int) WorldSnapshot {
			world.Step(dt)
			return world.Snapshot()
		}
	}, nil)
}
```

Run the suite with:

```sh
go tool nsgo test ./...
```

If `AssertDeterministicSteps` fails, it reports the first divergent step index along with both states (`run A` and `run B`), letting you narrow the cause directly to the tick that introduced nondeterminism.
