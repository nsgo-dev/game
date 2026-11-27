# GC and Frame Allocations

A 60 fps frame budget is 16.67 ms (8.33 ms at 120 fps). Go's garbage collector runs concurrently with low stop-the-world (STW) pauses, but steady per-frame heap allocations trigger frequent GC cycles, consume background CPU (mark assist), and can cause visible frame hitches. Keeping the update and draw hot paths at zero allocations per frame prevents GC work during active gameplay.

## Tuning `GOGC` and `GOMEMLIMIT`

Go exposes two primary GC knobs, configurable via environment variables or `runtime/debug`:

- **`GOGC`** (`debug.SetGCPercent`, default `100`): controls how much new heap may be allocated before the next GC cycle starts, as a percentage of live heap size.
  - Lower values (`50`–`100`) keep memory footprint tight at the cost of more frequent GC cycles.
  - Higher values (`200`–`400`, or `off` when paired with `GOMEMLIMIT`) reduce GC cycle frequency during gameplay, provided steady-state allocations stay near zero.
- **`GOMEMLIMIT`** (`debug.SetMemoryLimit`, default unlimited): sets a soft total Go runtime memory limit.
  - Pairing a soft `GOMEMLIMIT` with a higher `GOGC` lets the game avoid unnecessary collections while staying safely below platform memory ceilings.
  - Never set `GOMEMLIMIT` below the live asset + scene working set, or the GC will thrash trying to stay under the limit.

```go
import rdebug "runtime/debug"

func configureGC() {
	// Example: keep a 256 MiB soft ceiling and relax cycle frequency.
	rdebug.SetMemoryLimit(256 << 20)
	rdebug.SetGCPercent(200)
}
```

## Zero-Alloc Hot-Path Guidelines

Keep `Loop.Update`, `Loop.Draw`, and `Canvas.Draw` callbacks free of steady-state heap allocations:

1. **Pre-allocate and reuse slices and pools.** Keep slice backing arrays on structs and reset with `s = s[:0]` instead of allocating fresh slices each frame.
2. **Avoid `fmt.Sprintf` and string concatenation in hot loops.** Format HUD strings only when the underlying value changes, or inside reactive text callbacks gated on changed state.
3. **Avoid per-frame closure captures and interface boxing.** Prefer methods on pre-allocated structs and typed value tokens (such as `debug.ScopeToken`) over allocating closures or `any` wrappers per frame.
4. **Reuse `sprite.Batch` and fixed-size ring buffers.** Keep particle, bullet, and telemetry buffers fixed-capacity rather than growing/shrinking every tick.
5. **Load and decode assets between gameplay states.** Track per-group footprints with `debug.MemoryTracker` and call `ReleaseGroup` on level transitions; run an explicit `runtime.GC()` during loading screens rather than mid-frame.
6. **Build releases with `-tags release`.** All `pkg/debug` tooling compiles to zero-cost no-op stubs in release builds.

## Detecting GC Hitches and Testing in CI

### Runtime Overlay (`GCMonitor`)

Wire `debug.NewGCMonitor` into `debug.Options.GC` to sample `runtime/metrics` (`/gc/pauses:seconds`, `/gc/cycles/total:gc-cycles`, `/memory/classes/heap/objects:bytes`, `/gc/heap/allocs:objects`, `/gc/heap/allocs:bytes`) once per frame with zero steady-state allocations:

```go
gcMon := debug.NewGCMonitor(debug.FrameHistory)
overlay := debug.New(debug.Options{
	GC: gcMon,
})
```

Frames where a GC cycle or pause occurred are flagged (`GCFrameSample.Hitched == true`) and marked directly on the overlay's frame-time graph, alongside cycle count, last/max pause, live heap bytes, and recent hitch count.

Each `GCFrameSample` also carries the heap objects and bytes allocated during that frame (`AllocsDelta`, `AllocBytesDelta`). `gcMon.AllocsPerFrame()` and `gcMon.AllocBytesPerFrame()` average them over the retained history, and the overlay shows them under the GC summary as `allocs 12.0/f (1.5 KB/f)`. A value that stays above zero in steady gameplay means a hot path allocates every frame. Call `gcMon.Reset()` after a loading screen or level change to drop the history and measure from that point.

### CI Regression Checks (`AssertZeroAllocs`)

Use `debug.AssertZeroAllocs` (wrapping `testing.AllocsPerRun`) in unit tests to guard hot paths from accidental allocations in pull requests:

```go
func TestPhysicsStepZeroAllocs(t *testing.T) {
	world := newTestWorld()
	// Warm up one frame so any one-time caches are initialized.
	world.Step(16 * time.Millisecond)

	debug.AssertZeroAllocs(t, 100, func() {
		world.Step(16 * time.Millisecond)
	})
}
```

Use `debug.MeasureAllocs` when you want to assert an explicit upper bound or log allocation counts during optimization.

## Platform Notes

| Target | Memory & GC Considerations |
| --- | --- |
| **Desktop** (macOS, Windows, Linux) | Ample RAM and multi-core background marking; higher `GOGC` (e.g. `200`) with a conservative `GOMEMLIMIT` works well for smooth 60–120 fps pacing. |
| **iOS** | Strict jetsam memory limits per device class (especially older or lower-RAM devices). Always set `GOMEMLIMIT` well below the app's jetsam ceiling after accounting for native GPU textures and decoded audio buffers outside the Go heap. |
| **Android** | Wide variance in available RAM and thermal throttling across devices; background mark workers compete with rendering on low-core devices. Keep per-frame allocations at zero and size `GOMEMLIMIT` conservatively. |
| **WebAssembly** (`js/wasm`) | Single-threaded execution means GC work cannot hide on separate CPU cores and linear memory only grows. Minimizing per-frame allocations is especially critical to avoid frame drops and browser tab memory pressure. |

> **Hardware profiling status:** Physical-device GC pause and latency profiles across macOS, Windows, Linux, iOS, Android, and WebAssembly have not yet been recorded on target hardware. Numbers and platform defaults in this guide reflect Go runtime behavior and should be refined once hardware benchmarks are collected.
