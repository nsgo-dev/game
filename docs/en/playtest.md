# Playtest

Define automated playtests in `playtest.yaml`, run bots headless across seeded runs with deterministic input recordings, and check measured metrics against the game's goals.

```go
import "github.com/nsgo-dev/game/pkg/tooling/playtest"
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
- **`bot`**: `profile` (such as `platformer-runner`, `topdown-explorer`, `shooter`, `puzzle-solver`, or `card-ai`), optional `strategy` text, `reactionMin` / `reactionMax` human reaction jitter durations, and optional scalar `params`.
- **`metrics`**: each metric has a unique `name`, optional `label` and `unit`, a `measure`, and an optional `goal`:
  - `measure.kind` is `event`, `counter`, `timer`, `outcome`, `ratio` (requiring `denomKey`), or `perf` (`frame_ms`, `update_ms`, `draw_ms`, `p50_frame_ms`, `p95_frame_ms`, `p99_frame_ms`, `max_frame_ms`, `allocs_per_frame`).
  - `measure.aggregate` is `count`, `sum`, `mean`, `median`, `min`, `max`, `p95`, `p99`, or `rate`.
  - `measure.heatMap: true` on an `event` metric emits all recorded `(x, y)` coordinates into the report's `heatMaps` section.
  - `goal` specifies `source` (such as `brief.yaml`, `game.yaml`, or `playtest.yaml`), an optional `summary`, and `min`, `max` (or both for a range), or `equal`.
- **`baselines`** (optional): the outcome policies the game must show besides the configured bot's metrics.
  - `idle.outcome` is `lose` or `stall`, the result a run with no input must reach, within `idle.within`, a duration no longer than `runs.maxDuration`.
  - `reference.winRate` is the minimum share of seeded runs, from `0` to `1`, the configured bot must win.
  - `degraded` weakens the same bot with `reactionScale` (a multiplier on `reactionMin` / `reactionMax`), `dropRate` (the share of its actions discarded, from `0` to `1`), or both, and limits its win rate with `maxWinRate`, `margin` (the minimum gap below the reference win rate), or both.

  `playtest.report.yaml` has a matching optional `baselines` list, one entry per policy with its `name` (`idle`, `reference` or `degraded`), `passed`, and the measured `outcome` or `winRate`. The runner does not run the policies yet: `playtest.Run` and `go tool game playtest` validate and keep the section but do not fill the report's `baselines` or fail on them.

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

For bar charts and matchup matrices in `playtest.report.yaml`, call `metrics.ConfigureSeries` / `metrics.AddSeries` / `metrics.SetSeries` / `metrics.FlagSeries` (emitting `report.Series`, such as hits per wave or shortest solution per level) and `metrics.ConfigureMatrix` / `metrics.RecordMatchup` (emitting `report.Matrices`, such as win rate by deck pair).

## Built-in bot profiles

Every built-in profile is deterministic for a given seed and registers itself via `playtest.RegisterBot`:

- **`platformer-runner`** (`playtest.ProfilePlatformerRunner`, template `platformer2d`): runs greedily toward `PlatformerObservation.ExitX` and jumps at ledges, gaps and hazards with deterministic human reaction jitter sampled from the per-run `random.Stream` in `[reactionMin, reactionMax]`. Use `playtest.ObservePlatformerGrid` to build a `PlatformerObservation` from a `physics.TileGrid` and hazard boxes.
- **`topdown-explorer`** (`playtest.ProfileTopDownExplorer`, template `rpg2d`): walks to the nearest unseen walkable tile via BFS, talks to every NPC once, opens every chest, and stops when finished or when `runs.maxDuration` is reached (configured at `8x` speed in `rpg2d`'s `playtest.yaml`). Supply `ExplorerObservation` and record `goal_time`, `tiles_seen`/`tiles_total`, and `tile_visit` events (`heatMap: true`) for the time-per-tile heat map.
- **`shooter`** (`playtest.ProfileShooter`, template `shmup2d`): fires constantly, collects power-ups within `powerUpRange` (default `80` px), and dodges predicted incoming bullets through a reaction-delayed perception buffer (fixed `180ms` when `reactionMin == reactionMax == 180ms`). Use `RecordShooterWave`, `RecordShooterHit` and `RecordShooterPowerUp` to report hits per wave (`hits_per_wave` series) and power-ups taken.
- **`puzzle-solver`** (`playtest.ProfilePuzzleSolver`, template `puzzle2d`): pairs `SolvePuzzleBFS` (exact breadth-first search for the shortest solution up to `stateLimit`, default 2,000,000 states) with a human-like player that undoes after `undoAfterBadMoves` (default `3`) consecutive bad moves. Use `EvaluatePuzzlePack` or `NewPuzzlePackSim` to report solvable levels, moves over par, difficulty order dips, and solutions shorter than intended.
- **`card-ai`** (`playtest.ProfileCardAI`, template `cards2d`): two greedy 1-turn-lookahead players (`CardAI.ChooseAction`) play every unordered deck pair `matchesPerPair` times, alternating who goes first. Use `RunCardTournament` or `NewCardTournamentSim` to report first-player win rate, median match length, most lopsided pair, deterministic think time, and the `deck_win_rates` matrix.

Register custom bot profiles with `playtest.RegisterBot(name, factory)`. The `platformer2d`, `rpg2d`, `shmup2d`, `puzzle2d`, and `cards2d` project templates each ship a canonical `playtest.yaml` configured for their matching profile alongside a `playtest_test.go` wired through `playtest.RunTest`, so running `go tool game playtest` inside a freshly created template writes a deterministic report right away:

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template platformer2d mygame
cd mygame
go tool game playtest
```

## Running headless and replaying runs

Call `playtest.Run(cfg, makeSim, opts)` directly or wire a test with `playtest.RunTest` so `go tool game playtest` can execute it:

```go
func TestLevel1Playtest(t *testing.T) {
	playtest.RunTest(t, "playtest.yaml", makeSim)
}
```

Then run from the project directory:

```sh
go tool game playtest
go tool game playtest -config playtest.yaml -out playtest.report.yaml -recordings playtest-recordings
go tool game playtest -check
```

Each seeded run wraps the bot in a `replay.Recorder` and writes `<recordings>/run_NNN_seed_<seed>.replay.yaml`, so any run can be replayed deterministically with `replay.NewPlayer` or turned into a `game replay` regression test. The YAML report (`playtest.report.yaml`, `version: 1`) records every run's outcome, metrics, goal statuses (`met`, `missed`, `info`), heat-map points and recording paths.

## Editor MCP tools

The editor's MCP server (`game/editor/editormcp`, reached via `go tool game mcp`) exposes two playtest tools:

- `game_playtest` (`run`): validates `playtest.yaml`, runs the playtest headless with replays, writes the YAML report, and returns the structured report and summary.
- `playtest_define` (`edit`): validates a proposed metric and returns a canonical `playtest.yaml` diff queued as a pending review (`Server.PlaytestReviews`, `Server.ApplyPlaytestReview`, `Server.DismissPlaytestReview`) without modifying `playtest.yaml` on disk until reviewed.

## Idle bot

The built-in `playtest.ProfileIdle` bot sends no actions or axes. Use it for an idle-loss baseline with a bounded `runs.maxDuration`; it never inspects the game observation.
