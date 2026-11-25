# Agent Note: Split RTS crowd frame-budget timing from deterministic unit tests into CI benchmarks

Status: implemented

## Problem

`TestCrowdThousandUnitsReachTargetWithinFrameBudget` in `packages/game/rts/crowd_test.go` asserted that the 99th-percentile wall-clock step duration for 1,000 units crossing a 160×128 maze stayed under 16.67 ms (one 60 fps frame). On loaded build machines, OS preemption pushes individual step latencies past 16.67 ms even though the simulation itself is healthy, causing `go tool nsgo test ./...` to flake despite the repository's deterministic-testing requirement (#954).

## Decision

Separate correctness verification from performance regression gating:

- **Deterministic unit test (`TestCrowdThousandUnitsReachTarget`)**: keeps all three split-wall targets (`140,64`, `100,20`, `60,100`) and verifies that all 1,000 units arrive within the step cap, share flow fields (`Fields().Builds() <= 2`), stay out of blocked cells, and settle within `2 * Radius` of their assigned slots, with zero wall-clock assertions.
- **Tracked benchmarks (`BenchmarkCrowdThousandUnits` and `BenchmarkCrowdStep1000`)**: `BenchmarkCrowdThousandUnits` runs the same three full crossings as sub-benchmarks (`140,64`, `100,20`, `60,100`), measuring step-loop time and allocations (with crowd construction and `Move` excluded via `StopTimer`/`StartTimer`) and reporting `steps/op`, `avg-ms/step`, and `p99-ms/step`.
- **CI baseline enforcement**: `.github/workflows/quality.yml` adds `./rts` to the `go tool game bench -baseline ../../benchmarks/baseline.yaml` step, and `benchmarks/baseline.yaml` tracks `BenchmarkCrowdStep1000` (0 allocs/op, 4 ms baseline / 5 ms ceiling) and all three `BenchmarkCrowdThousandUnits` sub-benchmarks (ceilings well below the 60 fps budget, matching `BenchmarkFrame5000`'s budget-ceiling convention).

## Alternatives considered

- **Gate the `p99 > frameBudget` check behind an environment variable (e.g., `CI=true`) inside the unit test**: avoids adding `./rts` to `quality.yml`, but keeps a wall-clock flake risk inside `Test` jobs on busy CI runners (where `go tool nsgo test ./...` runs all packages in parallel) and leaves crowd performance outside the median-of-3 `game bench` pipeline.
- **Relax `frameBudget` in the unit test (e.g., to 100 ms)**: reduces flakes locally, but still couples unit-test pass/fail to machine load and lets real 5x step-time regressions slip past silently.
- **Track only `BenchmarkCrowdStep1000` in `benchmarks/baseline.yaml`**: fast and zero-allocation, but only covers mid-journey flow-field stepping (steps 300+) and never exercises the late-journey split-wall A* detour path that `TestCrowdThousandUnitsReachTargetWithinFrameBudget` originally timed.

## Consequences

- **Benefits**: `go tool nsgo test ./rts` is completely deterministic regardless of host CPU load, while the dedicated `Performance baselines` CI job catches regressions in both steady-state stepping (`BenchmarkCrowdStep1000`) and full-crossing detours (`BenchmarkCrowdThousandUnits/*`) using a 3-run median.
- **Costs**: The `Performance baselines` job in `.github/workflows/quality.yml` runs `./rts` benchmarks 3 times, adding roughly 15–20 seconds on `depot-ubuntu-24.04-4`.
- **Verification**: Ran `TestCrowdThousandUnitsReachTarget`, `go tool nsgo test ./rts/...`, `go tool nsgo test ./tooling/bench/...` (including `TestRepositoryBaselineRoundTrips`), `go tool game bench -baseline ../../benchmarks/baseline.yaml -bench='BenchmarkCrowd' ./rts`, `go tool nsgo check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
