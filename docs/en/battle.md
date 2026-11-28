# battle

`github.com/nsgo-dev/game/pkg/battle` resolves turn-based battles between a party and a troop. It holds no presentation: each turn returns a list of events for the game to animate in its own time, and every roll draws from a [`random.Stream`](determinism.md), so a seed and the same commands replay the same battle on every platform.

## A turn

```go
r := battle.New(
	[]battle.Fighter{{ID: "hero", HP: 30, MaxHP: 30, MP: 6, Attack: 5, Agility: 7}},
	[]battle.Fighter{{ID: "slime", HP: 12, Defense: 1, Attack: 3, Agility: 4}},
	battle.Rules{CriticalChance: 0.1},
	random.New(seed),
)
slash := battle.Action{ID: "slash", Scope: battle.OneEnemy, Power: 4, Accuracy: 0.9}
bite := battle.Action{ID: "bite", Scope: battle.OneEnemy, Power: 2}
events := r.Resolve([]battle.Command{
	{Actor: battle.Ref{Side: battle.PartySide, Index: 0}, Action: slash, Target: 0},
	{Actor: battle.Ref{Side: battle.TroopSide, Index: 0}, Action: bite, Target: 0},
})
```

The game chooses every fighter's command, the troop's included. `Resolve` runs them by `Agility`, highest first; ties go to the party before the troop and then to the lower index, whatever the order of the slice. A command for a fighter that does not exist is dropped, and a fighter defeated earlier in the turn does not act.

## Actions and scopes

| Scope | Targets | Effect |
| --- | --- | --- |
| `Self` | the actor | heals `Power` |
| `OneAlly` | the living ally at `Target` | heals `Power` |
| `AllAllies` | every living ally | heals `Power` |
| `OneEnemy` | the living opponent at `Target` | damages |
| `AllEnemies` | every living opponent | damages |

Damage is `Power + Attack - Defense`, at least 1. Each target of a hostile action rolls to hit against `Accuracy` (0 and 1 mean a sure hit and draw no roll), then for a critical against `Rules.CriticalChance`, which doubles the damage. Healing never raises HP above `MaxHP` when `MaxHP` is set. An action with a `Cost` spends that much MP first; an actor short of MP produces a `NoMP` event and nothing else.

## Events and outcome

`Resolve` returns `Missed`, `Damaged`, `Healed`, `Defeated` and `NoMP` events with the acting and the affected fighter, and `Amount` as the HP actually lost or gained. When the last troop member falls it appends `Won` and stops; when the last party member falls it appends `Lost` and stops. After that, `Resolve` returns nil.

## Saving and replay

`Runner.Snapshot` copies the fighters, the turn counter, the outcome and the stream's state, and marshals to JSON for a save slot. `Runner.Restore` puts a snapshot back, after which the same commands produce the same events as the original battle did.

## Not covered yet

The runner is the first part of #1757. It has no time-progress (gauge) mode, states, element rates or custom damage formulas, items, escape, enemy AI, troop events or rewards, and the rpg2d template does not use it yet.
