# Agent Note: Wire template playtest bots so game playtest runs out of the box

Status: implemented

## Problem

In [built-in playtest bot profiles](../feature/2026-11-25-playtest-bot-profiles.md), `rpg2d`, `shmup2d`, `puzzle2d`, and `cards2d` shipped canonical `playtest.yaml` files without Go tests calling `playtest.RunTest`, because `go.mod.tmpl` then pinned a pre-`playtest` version of `github.com/nsgo-dev/game`. `platformer2d` had no `playtest.yaml` at all. After `v0.1.0` published `pkg/playtest`, running `go tool game playtest` inside any scaffolded template still failed with no matching `Playtest` test (`nsgo-dev/game#1512`).

## Decision

Wire a `playtest_test.go` with `TestPlaytest` (calling `playtest.RunTest`) and a `TestBotReportCanonicalAndDeterministic` guard into `platformer2d`, `rpg2d`, `shmup2d`, `puzzle2d`, and `cards2d`, add a canonical `playtest.yaml` for `platformer2d`, sync the embedded copies under `internal/gamecli/_templates/projects/`, and verify `game playtest` determinism in the `templates` CI job.

### Per-template wiring and invariants

- **Single `Playtest`-matching test per template**: `game playtest` runs `-run Playtest`. Each template exposes exactly one matching test (`TestPlaytest`) and names auxiliary canonical/determinism checks without `Playtest` (`TestBotReportCanonicalAndDeterministic`) so `game playtest` never runs a second test that overwrites the report file.
- **`platformer2d` (`platformer-runner`)**: Adds canonical `playtest.yaml` (`scene: lantern_hills`, `moveAxis: move`, `jumpHoldMs: 260`, `reactionMin: 60ms`, `reactionMax: 140ms`) and adapts `world` via `playtest.ObservePlatformerGrid` over `w.grid` and spike boxes, reporting `clear_time`, `sparks_taken`/`sparks_total`, positioned `death` heat-map events, and deterministic `debug.FrameStats`.
- **`rpg2d` (`topdown-explorer`)**: Adapts `Game` across `town` and `field`, reporting `Moving: g.walk > simStep` so the explorer steers on the exact frame a tile step completes (`game.go`'s `updateField` finishes `g.walk` and immediately starts the next step on the same tick), blocking `thornhide` while unequipped, and emitting one `tile_visit` event per tile entered.
- **`shmup2d` (`shooter`)**: Adapts `world` with per-seed `stage.Seed = seed` and uses `RecordShooterWave`, `RecordShooterHit`, and `RecordShooterPowerUp`. Raises `powerUpRange` in `template/shmup2d/template/playtest.yaml` from `80` to `160` px so the bot crosses the 270 px playfield to collect drops from cleared waves.
- **`puzzle2d` (`puzzle-solver`)**: Wraps `*Board` and `slideMove` in `sparkPuzzleState` (`playtest.PuzzleState` + `playtest.PuzzleScorer`) and delegates directly to `playtest.NewPuzzlePackSim`.
- **`cards2d` (`card-ai`)**: Adapts `rules.yaml` and `board.yaml` into a shared-table two-player Suit Ladders duel (`suitLadderDuelState`) driven by `playtest.NewCardTournamentSim`. Consecutive even/odd matches per pair replay the same shuffled deck with seats and focus suits swapped (duplicate deals) so 40-match pair win rates remain free of pure binomial shuffle spikes while still catching genuine balance skew.
- **Deterministic perf telemetry**: Templates with `frame_time` metrics feed deterministic `debug.FrameStats` samples rather than wall-clock timings so repeated runs with the same seeds produce byte-identical `playtest.report.yaml` files.
- **CI drift prevention**: `.github/workflows/test.yml`'s `templates` job runs `game playtest` twice inside every copied standalone template that ships `playtest.yaml` (with `GOWORK=off` against its pinned `v0.1.0` `go.mod`) and diffs the two reports.

## Alternatives considered

- **Leaving `TestPlaytest` out of templates and keeping only `pkg/playtest/profiles_test.go`**: Rejected because `game new -template <t>` followed by `go tool game playtest` and the editor's playtest panel fail on every template out of the box, and the template `playtest.yaml` files drift from the real games.
- **Using independent shuffles with larger `matchesPerPair` in `cards2d`**: Even with perfectly balanced 50/50 decks, taking the maximum win rate across 5 runs and 6 pairs (30 trials of 40 independent matches) exceeds 60% over 99% of the time by binomial variance alone; raising `matchesPerPair` enough to suppress that noise would exceed `maxDuration` and slow down `nsgo test`. Duplicate deals cancel shuffle noise without changing `cards2d/playtest.yaml` or weakening its 60% threshold.
- **Measuring wall-clock `Step` duration via `RunOptions.MeasurePerf`**: Rejected because machine jitter breaks byte-identical report reproducibility on repeated runs for the same seeds.

## Consequences

- **Benefits**: Every playtest-configured template (`platformer2d`, `rpg2d`, `shmup2d`, `puzzle2d`, `cards2d`) runs `go tool game playtest` out of the box against its real game state, writes deterministic reports and `.replay.yaml` recordings, and is continuously verified in CI against its pinned module version.
- **Costs**: Adding or changing template gameplay or `playtest.yaml` goals now requires keeping the template's `playtest_test.go` adapter and embedded copy in `internal/gamecli/_templates/projects/` in sync, and template tests take a few extra seconds in CI to run the bot playtests.
