# Agent Note: RPG battle runner

Status: implemented

## Problem

The only turn-based battle in the repository is written into the rpg2d template: one player against one foe, `max(1, attack-defense)` damage, no MP, no scopes, no turn order and no deterministic replay (#1757). Nothing in `pkg/` runs a turn-based fight, so a card game, a tactics game or a `playtest` bot cannot share the rules.

## Decision

`pkg/battle` is a presentation-independent library over plain Go values. `battle.New(party, troop, rules, rng)` copies both sides into a `Runner`; `Runner.Resolve(commands)` is one turn and returns `[]Event` for the presentation layer to animate in its own time.

- **Commands from both sides**: a `Command` names its actor with a `Ref` (side and index), so the game feeds enemy choices through the same call as the party's; the runner has no enemy AI.
- **Turn order**: commands run by the actor's `Agility`, highest first; ties go to the party before the troop and then to the lower index. The order never depends on the order of the command slice, so two front ends that collect commands differently replay the same battle. Commands for missing actors are dropped, and an actor defeated earlier in the same turn is skipped.
- **Scopes relative to the actor's side**: `OneEnemy` and `AllEnemies` damage the opponents (`Power + Attack - Defense`, at least 1), `Self`, `OneAlly` and `AllAllies` heal allies by `Power`, capped at `MaxHP` when it is set. Damage and healing events report the HP actually lost or gained.
- **Costs**: `Action.Cost` spends MP before the action resolves; an actor without enough MP produces a `no_mp` event and nothing else.
- **Rolls**: only hostile actions roll, on the runner's `random.Stream`: a hit roll when `Accuracy` is in (0, 1), then a critical roll when `Rules.CriticalChance` is above 0, which doubles the damage. Accuracy 0 and 1 both mean a sure hit and draw nothing, so adding a sure-hit action does not shift later rolls.
- **Outcome**: the turn stops at the event that defeats the last fighter of a side (`won` or `lost`), and `Resolve` returns nil from then on. A single action defeats fighters on one side only, so a turn cannot end both won and lost.
- **Snapshots**: `Snapshot` copies fighters, the turn counter, the outcome and the stream's `random.State`; it marshals to JSON for save slots, and `Restore` replays the rest of a battle exactly.

## Scope of this slice

This package delivers the turn runner only. Not yet built from #1757: the `gauge` (time progress) turn mode, states and their YAML format, element rates and damage formulas, items, escape, enemy action patterns and troop events, rewards through `Progression` and `loot`, and moving the rpg2d template onto the runner. Those layer on top of `Command`, `Event` and `Snapshot` without changing how events are delivered.

## Alternatives considered

### Presentation callbacks inside the runner

Strongest argument: a callback per hit lets a UI play an animation at the moment the rule resolves, without buffering. Rejected because it couples rule resolution to a UI timing model and makes replays and headless tests drive a fake presentation; a returned event list serves animation, replay inspection and `playtest` assertions alike.

### Ordering ties by the position in the command slice

Strongest argument: a stable sort over the given slice is the least code. Rejected because the outcome then depends on how a front end happens to collect commands, which a replay recorded by one front end and played back by another would not reproduce.

## Consequences

Battles are deterministic from a seed and a command log, can be saved and rolled back mid-fight, and are testable without any rendering. The cost is that the game must choose enemy commands itself until enemy action patterns land, and the fixed formula (no element rates, no custom formulas) covers simple games only.
