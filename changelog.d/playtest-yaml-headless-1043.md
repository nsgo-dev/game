### Added

- `game/playtest`: add versioned `playtest.yaml` configuration and published JSON Schema (`playtest.v1.json`), deterministic YAML report format (`playtest.report.yaml`), per-run metrics `Collector` API (positioned events, counters, timers and perf readout), headless runner recording per-seed `.replay.yaml` files, and the `platformer-runner` bot profile with human reaction jitter (#1043).
- `game/cmd/game`: add `go tool game playtest` to validate `playtest.yaml`, run headless playtests via `playtest.RunTest`, save per-seed recordings, write the YAML report and fail on missed goals (#1043).
- `game/editor/editormcp`: add `game_playtest` and `playtest_define` MCP tools, with `playtest_define` queuing canonical `playtest.yaml` diffs as pending reviews (`PlaytestReviews`, `ApplyPlaytestReview`, `DismissPlaytestReview`) without modifying disk until reviewed (#1043).
