# perf/session

`github.com/nsgo-dev/game/pkg/perf/session` records release-safe frame pacing without the debug overlay. Feed completed frame durations to `Recorder.Observe`, then read `Summary` for p50/p95/p99, longest frame, frames at least twice the target interval, peak heap, runtime memory and GC cycles/pause observations. The recorder uses a fixed ring and can be sampled from a release build.
