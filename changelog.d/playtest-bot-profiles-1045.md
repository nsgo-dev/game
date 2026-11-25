### Added

- `game/playtest`: built-in deterministic bot profiles for top-down exploration (`topdown-explorer`), shoot-'em-ups (`shooter`), puzzle solving and human-like undo play (`puzzle-solver` with `SolvePuzzleBFS`, `EvaluatePuzzlePack`, and `NewPuzzlePackSim`), and 1-turn-lookahead card duels (`card-ai` with `RunCardTournament` and `NewCardTournamentSim`), plus optional `Series` bar charts and `Matrices` matchup grids on `playtest.Report` (#1045).
- `template`: canonical `playtest.yaml` files in `rpg2d`, `shmup2d`, `puzzle2d`, and `cards2d` templates matching each template's bot profile (#1045).
