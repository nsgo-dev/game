### Fixed

- `game/rts`: split wall-clock step timing out of `TestCrowdThousandUnitsReachTarget` into `BenchmarkCrowdThousandUnits` tracked by the CI performance baselines so `go tool nsgo test ./rts` stays deterministic on busy machines (#954).
