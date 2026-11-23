### Added

- `game replay` checks every `*.replay.yaml` input recording under its directories and plays them headless through the tests with `Replay` in their names, failing when a recording cannot be played, a run ends in another state than its test expects or no test passed; `game bench` runs the benchmarks headless with `nsgo test -bench`, compares each one's median with a stored baseline and fails on a regression, or records the baseline with `-update` (#457).
- `game/tooling/bench` reads and writes benchmark baselines as versioned YAML text, parses `go test -bench` and `nsgo test -bench` output and reports regressions (#457).

### Removed

- The `benchcheck` command, replaced by `game bench`, and its JSON baseline: baselines are now YAML text files with a version field (`benchmarks/baseline.yaml` in this repository) (#457).
