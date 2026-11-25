# Playtest

Define automated playtests in `playtest.yaml`, run bots headless across seeded runs with deterministic input recordings, and check measured metrics against the game's goals.

```go
import "github.com/nsgo-dev/game/packages/game/playtest"
```

## `playtest.yaml`

`playtest.yaml` is a versioned plain-text YAML file with a published JSON Schema (`https://game.nsgo.dev/schemas/playtest.v1.json`). Unknown fields, missing or unsupported versions, duplicate metric names and invalid goals are refused; `EncodeConfig` writes canonical deterministic YAML that round-trips without changes.

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/playtest.v1.json
version: 1
scope:
  project: dungeon-run
  scene: level_01
  template: platformer2d
runs:
  count: 20
  seedStart: 1
  rate: 60
  speed: 1
  maxDuration: 1m30s
bot:
  profile: platformer-runner
  strategy: Greedy route to the door, jumps at ledges and gaps, 120-220 ms reaction jitter
  reactionMin: 120ms
  reactionMax: 220ms
  params:
    jumpHoldMs: 220
metrics:
  - name: finished
    label: Finished
    measure:
      kind: outcome
      key: finished
      aggregate: count
    goal:
      summary: most first-timers clear floor 1
      source: brief.yaml
      min: 16
  - name: median_time
    label: Median time
    unit: s
    measure:
      kind: timer
      key: clear_time
      aggregate: median
    goal:
      summary: under a minute
      source: brief.yaml
      min: 40
      max: 60
  - name: frame_time
    label: Frame time
    unit: ms
    measure:
      kind: perf
      key: frame_ms
      aggregate: median
    goal:
      summary: 16.7 ms at 60 Hz
      source: game.yaml
      max: 16.7
  - name: coins_taken
    label: Coins taken
    unit: '%'
    measure:
      kind: ratio
      key: coins_taken
      denomKey: coins_total
      aggregate: mean
    goal:
      summary: every coin on a route
      source: playtest.yaml
      min: 100
  - name: deaths
    label: Deaths
    measure:
      kind: event
      key: death
      aggregate: sum
      heatMap: true
```

### Sections

- **`scope`**: the `project`, required `scene`, and optional `template`.
- **`runs`**: `count`, optional explicit `seeds`, `seedStart` (defaults to `1`), fixed `rate` in steps per second (defaults to `60`), `speed` multiplier metadata (defaults to `1`), and `maxDuration` simulated time limit per run (defaults to `60s`).
- **`bot`**: `profile` (such as `platformer-runner`), optional `strategy` text, `reactionMin` / `reactionMax` human reaction jitter durations, and optional scalar `params`.
- **`metrics`**: each metric has a unique `name`, optional `label` and `unit`, a `measure`, and an optional `goal`:
  - `measure.kind` is `event`, `counter`, `timer`, `outcome`, `ratio` (requiring `denomKey`), or `perf` (`frame_ms`, `update_ms`, `draw_ms`, `p50_frame_ms`, `p95_frame_ms`, `p99_frame_ms`, `max_frame_ms`, `allocs_per_frame`).
  - `measure.aggregate` is `count`, `sum`, `mean`, `median`, `min`, `max`, `p95`, `p99`, or `rate`.
  - `measure.heatMap: true` on an `event` metric emits all recorded `(x, y)` coordinates into the report's `heatMaps` section.
  - `goal` specifies `source` (such as `brief.yaml`, `game.yaml`, or `playtest.yaml`), an optional `summary`, and `min`, `max` (or both for a range), or `equal`.

## Reporting metrics from Go

During a run, the game receives a `*playtest.Collector` and calls it to report events, counters, timers and outcome. Every method is nil-safe, so game code can call it unconditionally in both live play and playtest runs:

```go
metrics.StartTimer("clear_time")
metrics.SetCounter("coins_total", float64(len(coins)))

// When collecting a coin:
metrics.Event("pickup", float64(cx), float64(cy), coin.ID)
metrics.Inc("coins_taken")

// When dying:
metrics.Event("death", float64(px), float64(py), "slime_pit")

// On win or loss:
metrics.StopTimer("clear_time")
metrics.Finish() // or metrics.Fail("out of lives")
```

Frame time comes from the perf readout: pass `FrameStats: stats` and `GC: gcMon` on `playtest.Sim` (or call `metrics.RecordPerf(stats, gcMon)`), which uses the same averaging and percentile calculations as `game/mcp`'s `game_perf_readout`.

## Built-in bot profile: `platformer-runner`

`platformer-runner` (`playtest.ProfilePlatformerRunner`) runs greedily toward `PlatformerObservation.ExitX` and jumps at ledges, gaps and hazards with deterministic human reaction jitter sampled from the per-run `random.Stream` in `[reactionMin, reactionMax]`. Use `playtest.ObservePlatformerGrid` to build a `PlatformerObservation` from a `physics.TileGrid` and hazard boxes.

Register additional bot profiles with `playtest.RegisterBot(name, factory)`.

## Running headless and replaying runs

Call `playtest.Run(cfg, makeSim, opts)` directly or wire a test with `playtest.RunTest` so `nsgo game playtest` can execute it:

```go
func TestLevel1Playtest(t *testing.T) {
	playtest.RunTest(t, "playtest.yaml", makeSim)
}
```

Then run from the project directory:

```sh
nsgo game playtest
nsgo game playtest -config playtest.yaml -out playtest.report.yaml -recordings playtest-recordings
nsgo game playtest -check
```

Each seeded run wraps the bot in a `replay.Recorder` and writes `<recordings>/run_NNN_seed_<seed>.replay.yaml`, so any run can be replayed deterministically with `replay.NewPlayer` or turned into a `game replay` regression test. The YAML report (`playtest.report.yaml`, `version: 1`) records every run's outcome, metrics, goal statuses (`met`, `missed`, `info`), heat-map points and recording paths.

## Editor MCP tools

The editor's MCP server (`game/editor/editormcp`, reached via `nsgo game mcp`) exposes two playtest tools:

- `game_playtest` (`run`): validates `playtest.yaml`, runs the playtest headless with replays, writes the YAML report, and returns the structured report and summary.
- `playtest_define` (`edit`): validates a proposed metric and returns a canonical `playtest.yaml` diff queued as a pending review (`Server.PlaytestReviews`, `Server.ApplyPlaytestReview`, `Server.DismissPlaytestReview`) without modifying `playtest.yaml` on disk until reviewed.
