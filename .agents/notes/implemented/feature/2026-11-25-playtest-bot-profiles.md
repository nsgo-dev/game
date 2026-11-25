# Agent Note: Built-in playtest bot profiles for explorer, shooter, puzzle and card templates

Status: implemented

## Problem

After [playtest configuration and headless runner](2026-11-25-playtest-yaml-headless.md) landed `platformer-runner`, the remaining four genres designed in `docs/design/editor/Playtest.dc.html` (`rpg2d`, `shmup2d`, `puzzle2d`, and `cards2d`) had no built-in deterministic bot profiles, no report structures for per-wave/per-level bar series or deck-pair win-rate matrices, and no starter `playtest.yaml` in those four project templates.

## Decision

Add four deterministic bot profiles to `packages/game/playtest` (`topdown-explorer`, `shooter`, `puzzle-solver`, and `card-ai`), extend `playtest.Report` and `playtest.Collector` with optional `Series` and `Matrices` charts, and ship a canonical `playtest.yaml` in `rpg2d`, `shmup2d`, `puzzle2d`, and `cards2d`.

### Bot profiles and domain adapters

- **`topdown-explorer`** (`ProfileTopDownExplorer`, `topdown_explorer.go`): runs BFS on walkable tiles to visit the nearest unseen tile, talks to every NPC once, opens every chest once, advances dialogue/battle prompts, and stops on goal completion or `runs.maxDuration` (configured at `8x` speed in `rpg2d/template/playtest.yaml`). Time-per-tile heat maps are emitted as positioned `tile_visit` events (`heatMap: true`).
- **`shooter`** (`ProfileShooter`, `shooter.go`): maintains a fixed-delay perception snapshot buffer sized from `reactionMin`/`reactionMax` (default fixed `180ms`), fires constantly, collects power-ups within `powerUpRange` (default `80` px), and dodges predicted bullet trajectories. Helpers (`RecordShooterWave`, `RecordShooterHit`, `RecordShooterPowerUp`) emit `hits_per_wave` series entries and power-up ratios.
- **`puzzle-solver`** (`ProfilePuzzleSolver`, `puzzle_solver.go`): combines `SolvePuzzleBFS` (parent-indexed breadth-first search up to `stateLimit`, default `2_000_000` states) with a human-like player that undoes after `undoAfterBadMoves` (default `3`) consecutive bad moves, remembering failed branches so it explores alternatives. `EvaluatePuzzlePack` and `NewPuzzlePackSim` report solvable levels, difficulty order dips, median moves over par, and solutions shorter than intended.
- **`card-ai`** (`ProfileCardAI`, `card_ai.go`): implements greedy 1-turn-lookahead (`ChooseAction`: completes the active player's turn greedily, then simulates the opponent's best greedy reply turn) with deterministic node-count think-time accounting (`NodesEvaluated * msPerNode`). `RunCardTournament` and `NewCardTournamentSim` play every unordered deck pair `matchesPerPair` times, alternating who goes first, and populate first-player win rate, median match length, most lopsided pair, think time, and the `deck_win_rates` matrix.

### Report extensions and template contract

- `playtest.Report` (`ReportVersion = 1`) gains optional `omitempty` `Series []SeriesChart` and `Matrices []MatrixChart` fields, validated strictly by `ValidateReport` and aggregated deterministically in `Run` from `Collector` (`ConfigureSeries`/`AddSeries`/`SetSeries`/`FlagSeries` and `ConfigureMatrix`/`RecordMatchup`). Existing version 1 reports without these fields remain unchanged.
- `template/{rpg2d,shmup2d,puzzle2d,cards2d}/template/playtest.yaml` and their embedded copies under `packages/game/cmd/game/_templates/projects/` contain only canonical YAML (`EncodeConfig` byte-for-byte identical) without adding Go imports of `packages/game/playtest` into template Go sources, because standalone `go.mod.tmpl` pins an earlier `packages/game` pseudo-version until release.

## Alternatives considered

- **Measuring wall-clock think time for `card-ai`**: Real wall-clock timing would show machine-specific latency, but breaks the acceptance requirement that repeated runs for the same seeds produce identical YAML reports. Accounting think time deterministically from evaluated lookahead nodes (`NodesEvaluated * msPerNode`) keeps reports byte-identical across runs and machines while still scaling with decision branching complexity.
- **Adding `Test*Playtest` Go files directly inside `template/*/template/`**: Would let `go tool game playtest` run inside a freshly scaffolded template immediately, but fails when `game new` generates a standalone module whose `go.mod.tmpl` pins a pre-`playtest` commit of `packages/game`. Keeping template additions to canonical `playtest.yaml` files and testing every profile against realistic genre fixtures in `packages/game/playtest/profiles_test.go` avoids breaking standalone template builds.
- **Encoding bar series and matchup matrices only as ad-hoc counters**: Works without touching `Report`, but forces the editor Playtest UI (#1044) to hardcode naming conventions to reconstruct wave bars, level curves, and 2D win-rate matrices. Typed, strictly validated `Series` and `Matrices` slices on `Report` give both CLI reports and the editor UI a single deterministic contract.

## Consequences

- Benefits: All five genres from `Playtest.dc.html` now have registered, deterministic, seed-reproducible bot profiles wired into `playtest.Run` and `replay.Recorder`, with typed heat-map, bar-series, and matrix outputs ready for the editor UI (#1044) and canonical `playtest.yaml` configs in all four matching templates.
- Costs: `playtest.Report` adds two optional chart sections (`series` and `matrices`) that must remain backward-compatible and validated in `ValidateReport`. Games using `puzzle-solver` or `card-ai` must adapt their state to the small `PuzzleState` or `CardGameState` interfaces.
