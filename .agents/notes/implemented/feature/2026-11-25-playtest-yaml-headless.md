# Agent Note: Playtest configuration, headless runner, platformer-runner bot, CLI and MCP tools

Status: implemented

## Problem

The game layer had deterministic input recordings (`game/replay`) and headless state dumps (`game/statedump`), but no plain-text specification for defining playtest bots, seeded runs, metrics and sourced goals, no Go API for games to report positioned events/counters/timers, no built-in platformer bot with human reaction jitter, and no CLI or MCP tools to run playtests or propose new metrics as reviews.

## Decision

Add `packages/game/playtest` as the shared plain-text YAML format, metrics collector, bot registry, `platformer-runner` profile and headless runner, wired into `cmd/gameschema` (`playtest.v1.json`), `go tool game playtest`, and `editor/editormcp` (`game_playtest` and `playtest_define`).

### On-disk YAML contracts

- `playtest.yaml` (`playtest.CurrentVersion = 1`, published schema `https://game.nsgo.dev/schemas/playtest.v1.json`) declares `scope` (`project`, `scene`, `template`), `runs` (`count`, `seeds`, `seedStart`, `rate`, `speed`, `maxDuration`), `bot` (`profile`, `strategy`, `reactionMin`, `reactionMax`, scalar `params`), and `metrics` (`name`, `label`, `unit`, `measure`, optional `goal` with `source`, `summary`, and `min`/`max`/`equal`). `ParseConfig` refuses unknown fields, missing/unsupported versions, trailing documents, invalid goals and non-scalar params; `EncodeConfig` writes canonical 2-space YAML prefixed with `# yaml-language-server: $schema=...`.
- `playtest.report.yaml` (`playtest.ReportVersion = 1`) stores `scope`, `bot`, `rate`, `speed`, `seedCount`, `passed`, evaluated `metrics` (`met`, `missed`, `info`), `heatMaps` (world-space event points in deterministic run-then-step order), and per-seed `runs` (outcome, steps, duration, recording path, counters, timers, positioned events, optional perf summary). Both formats are covered by `internal/formattest.Run` and `docs/{en,zh}/formats.md`.

### Deterministic execution and replay contract

- Each seeded run wraps a `BotActionSource` inside `replay.NewRecorder(botSrc, seed, rate, actions, axes)` and passes that `replay.ActionSource` to the game's `SimFactory`. Every bot decision is derived from a dedicated `random.NewStreams(seed).Stream("playtest.bot")` stream so bot reaction jitter never perturbs the game's own random stream.
- `Run` requires `Sim.Step` to advance `ActionSource` once per fixed step, guaranteeing that every recorded `.replay.yaml` file replays via `replay.NewPlayer` (without the bot) to the exact same game states and death positions.
- Frame time comes from the perf readout (`debug.FrameStats` and `debug.GCMonitor`, matching `game/mcp`'s `game_perf_readout` averaging and percentiles). Wall-clock timing (`RunOptions.MeasurePerf`) is off by default so repeated headless runs produce byte-identical reports unless a sim explicitly supplies perf samples.

### CLI and Editor MCP integration

- Following `game replay` and `game bench`, `go tool game playtest` validates `playtest.yaml` first and executes the game's own tests matching `-run Playtest` via `go tool nsgo test`, passing `NSGO_PLAYTEST_*` environment variables that `playtest.RunTest` reads to write recordings and `playtest.report.yaml`. It prints the report summary and exits 1 when any goal is missed or no test matched.
- `game/editor/editormcp` adds `game_playtest` (`ActionRun`) and `playtest_define` (`ActionEdit`). `playtest_define` never overwrites `playtest.yaml` on disk directly; it validates the metric, produces canonical YAML and a unified diff, and stores a pending `PlaytestReview` on `Server` (`PlaytestReviews`, `ApplyPlaytestReview`, `DismissPlaytestReview`), verifying disk contents still match `Before` before applying.

## Alternatives considered

- **Running games inside `go tool game playtest` without `go tool nsgo test`**: A standalone CLI binary cannot import a user project's Go simulation packages without compiling them. Delegating execution to `go tool nsgo test` through `playtest.RunTest` matches `game replay` and `game bench` and keeps full fidelity with the game's build tags and Go code.
- **Letting `playtest_define` overwrite `playtest.yaml` directly**: Faster in a single tool call, but violates the editor review workflow in `Playtest.dc.html` where agent-proposed metrics arrive as a review banner with a diff before touching the project's files.
- **Measuring wall-clock frame time unconditionally inside `playtest.Run`**: Would populate `frame_ms` automatically for uninstrumented sims, but would break byte-identical report determinism across repeated runs of the same 20 seeds. Reading perf from `debug.FrameStats`/`debug.GCMonitor` (with opt-in `MeasurePerf`) preserves strict determinism while wiring directly into the perf readout.

## Consequences

- Benefits: Games and agents get a single versioned, schema-validated `playtest.yaml` contract, deterministic 20+ seed headless verification, replayable `.replay.yaml` artifacts for every seed, clear missed-goal diagnostics with goal provenance (`brief.yaml`, `game.yaml`, `playtest.yaml`), and a clean foundation for the editor Playtest UI (#1044) and additional bot profiles (#1045).
- Costs: Games that want `go tool game playtest` must add a small `Test*Playtest` wrapper calling `playtest.RunTest` and supply a `PlatformerObservation` (helped by `ObservePlatformerGrid`). Adding `game_playtest` and `playtest_define` increases the built-in tool list of `editor/editormcp` from five to seven.
