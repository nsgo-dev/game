# Agent Note: Keep unit tests deterministic and reproduce timing regressions before failing CI

Status: implemented

## Problem

Several unit tests failed when wall-clock measurements crossed a performance threshold, including the HPA* 10x speedup assertion on a 1024x1024 grid. Shared CI runners can exceed those thresholds without any code regression. The benchmark gate already compared medians from three runs, but a failing timing measurement was reported immediately instead of being reproduced, so runner noise could still turn an unrelated pull request red (#1901).

## Decision

Unit tests keep deterministic correctness and liveness assertions but do not assert elapsed-time performance. `TestHPA1024OptimalityBound` retains the path and cost-ratio checks, while `BenchmarkAStar1024` and `BenchmarkHPA1024` remain the performance measurements. The Performance baselines job checks those two benchmarks against the dedicated `benchmarks/hpa-baseline.yaml`, so it does not need to run the entire AI benchmark suite. The other remeshing, navmesh, permission-glob, hot-reload, and nonblocking-operation tests likewise retain their functional assertions or matching benchmarks without `time.Since` thresholds. Where the property under test is that a call does not block, the check stays but is tied to the behavior instead of a speed: `ReconnectAgent` and `DisconnectProfiler` run on a goroutine and fail only if they have not returned 5 s after a probe or RPC that never finishes on its own, and the WebSocket `Kick` test fails only if `Kick` returns after the `wsCloseFlushTimeout` flush deadline it arms, which is what the regression it guards did. This extends the existing [RTS crowd split between correctness and benchmarks](2026-11-25-rts-crowd-budget-benchmark.md).

`game bench` continues to compare each benchmark's median from three runs by default. When every failure is a ns/op regression and both B/op and allocs/op exactly match the baseline, the command runs the complete benchmark selection once more. It reports a warning and passes when the retry passes; it fails when the regression reproduces. Missing benchmarks, allocation changes, and mixed failures are not retried. `-in` cannot retry because it evaluates saved output rather than running benchmarks.

## Alternatives considered

- **Keep larger wall-clock thresholds in unit tests**: simple and preserves an explicit frame-budget assertion, but still makes correctness jobs depend on host load and only reduces rather than removes flakes.
- **Rely on the three-run median alone**: already removes an isolated outlier and remains the first line of defense, but does not meet the requirement to reproduce a failing timing regression before reporting it.
- **Retry every failed benchmark**: could smooth all timing noise without inspecting metrics, but wastes runs on missing benchmarks and can obscure allocation regressions that are deterministic and should fail immediately.
- **Add an expanded-node instrumentation API only for HPA***: provides a deterministic proxy, but changes the public API for a test when the existing A* and HPA* benchmarks already measure the desired scenario.

## Consequences

- **Benefits**: unit tests no longer fail solely because CPU scheduling changes a measured speed, unrelated changes no longer fail on the HPA* speedup ratio or the other elapsed-time thresholds, and a transient ns/op-only baseline failure gets one explicit reproduction attempt. Persistent timing and allocation regressions still fail.
- **Costs**: a ns/op-only failing benchmark selection takes a second complete run. The retry operates at selection granularity rather than rerunning only the failing benchmark, and saved `-in` results cannot benefit from it. The dedicated HPA* baseline adds a second `game bench` invocation to the Performance baselines job.
- **Verification**: regression tests first failed with one initial benchmark call and no retry, then passed after the implementation; one noisy run passes with a warning and a repeated +60% ns/op regression fails after exactly two calls. Package tests, `go tool nsgo check`, `go tool nsgo test ./...`, changelog validation, and note verification cover the final change.
