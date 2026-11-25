# Agent Note: The roguelike2d template

Status: implemented

## Problem

Issue #798, under #786, asks for a `roguelike2d` project template: procedurally generated floors, turn-based movement and combat, weighted loot, stats with modifiers, permadeath and a run summary, built mainly on `procgen`, `loot`, `stats`, `ability` and `save`, playable with the keyboard, a controller and touch, with an nsgotest playthrough that reaches a win and a loss. Those packages are built for real-time games stepped by `game.Loop`, and none of them defines a turn, a floor or a run.

## Decision

The template lives in `template/roguelike2d/template`, as `docs/en/templates.md` describes, with its copy under `packages/game/cmd/game/_templates/projects/roguelike2d`, one registry entry and one `go.work` line.

- **A run is a pure turn machine.** `run.go` holds the rules and knows nothing about input or drawing: `run.act` plays one player action and then every monster's turn, and the game turns input into at most one action per loop step. The playthrough tests, the balance bots and the rule tests all drive the same `act`, so the UI tests only add the device layer.
- **Every floor comes from its own stream.** A floor, its loot rolls and its combat rolls draw from `random.New(seed).Fork("<purpose>-<floor>")`, so a run saved on arrival needs no cells, monsters or items: resuming regenerates the floor exactly. The loot roller is rebuilt per floor on the floor's loot stream with the pity counters carried over through `Roller.State` and `SetState`.
- **The ability caster counts turns.** `ability.NewCaster(set, 1)` makes one step a second, so `cooldown: 8s` in `data/abilities.yaml` is eight turns, and the run calls `Caster.Update(nil)` once per turn after `TryActivate` from the action. Conditions (`hurt`, `foes-adjacent`) and the draught cost go through `Caster.Condition` and a `Resources` adapter over the run's draught count.
- **Stats carry gear and tonics; health is the run's.** `maxHealth`, `attack` and `defense` are a `stats.Sheet` from `data/stats.yaml`; weapon and armor are one source each, swapped with `Sheet.Refresh`, a tonic is a timed effect counted by `Sheet.Step` once a turn, and levels call `SetBase`. Current health is a plain integer clamped to `maxHealth`, because a stat is a value derived from modifiers, not a pool that is spent.
- **Permadeath is the run slot's lifetime.** `records.go` keeps two `save.Store` slots in `core.Storage`: `run`, written as the player arrives on each floor and deleted the moment the run is won or lost, and `records`, which counts every ended run and keeps the last summary. A slot from a newer version is left alone, as the tutorial's records are.
- **The template's own data format is `data/dungeon.yaml`**, version 1, decoded with known fields only and encoded canonically, holding the player's progression, the floors, the monsters and the items; `loadData` checks every name it uses from `stats.yaml`, `loot.yaml` and `abilities.yaml` and back.
- **Balance is a test.** `TestBotsWinAndLose` plays forty seeds with a bot that clears each floor (resting, quaffing and cleaving) and one that dives for the exits without healing, and requires the first to win at least half and the second to die in three quarters. At the time of writing they win and die in all forty. The UI playthroughs use fixed seeds from those runs.

## Gaps in the packages

These were worked around in the template rather than changed in shared packages:

- `procgen` has no distance-map or farthest-cell helper; `floor.go` writes a breadth-first `distances` and picks the exit from it. `ai` has A*, HPA* and JPS for one path, not a whole-grid distance map that many monsters walk down at once.
- `procgen` has no field-of-view; `floor.go` has a small Bresenham line-of-sight pass.
- `ability` and `stats` count fixed loop steps, which fits a turn only through the rate-1 caster and one `Sheet.Step` per turn; there is no turn-based clock to hand them.

## Alternatives considered

- **Activate abilities through `Caster.Update(actions)` reading the input map.** It is how a real-time game uses the package, but it would let an ability fire on any loop step rather than as the player's turn, and the monsters would not answer it. Calling `TryActivate` from the turn keeps every ability a turn.
- **Save the whole floor (cells, monsters, items) in the run slot.** It would let a run resume mid-floor, but it makes the slot large and ties it to the floor's internal shape; regenerating from the seed keeps the slot a dozen numbers. The cost is that quitting mid-floor resumes at the floor's start.
- **Draw a `tilemap.Tilemap` painted with `procgen.Grid.Paint`.** It is the path to real art, and the guide points to it, but the template's letters and shapes need no atlas or licensed images.
- **Rely on draughts as the only healing.** The first balance pass did, and the clearing bot won 2 of 40 seeds; health regeneration (`regenTurns`) with resting out of danger made the game winnable by careful play while diving stays fatal.

## Consequences

- A developer changes the game by editing YAML and sees the balance and floor-soundness tests report what broke.
- The run slot holds a seed and the player's numbers, so changing `data/dungeon.yaml` can make an old slot unresumable; such a slot is replaced by a new run rather than crashing.
- The template pins `packages/game` at a `main` pseudo-version that has `procgen`, `loot`, `stats`, `ability` and `save`, which CI's templates job tests with `GOWORK=off`.

## Verification

- `go tool nsgo test ./template/roguelike2d/template` from the workspace: the playthroughs, bots, rules, floors and data files.
- `go tool nsgo test ./cmd/game` from `packages/game`: the registry, the embedded copy and a project created from the template.
- CI's `templates` job tests a copy of the template against its pinned modules.
