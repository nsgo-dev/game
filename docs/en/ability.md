# ability

`github.com/nsgo-dev/game/pkg/ability` runs gameplay abilities: costs, cooldowns, charges, cast and channel times and conditions, activated by [input](input.md) actions and advanced one fixed [`game.Loop`](loop.md) step at a time.

```go
import "github.com/nsgo-dev/game/pkg/ability"
```

## Ability sets

Abilities are defined in a versioned YAML file:

```yaml
version: 1
abilities:
  - id: fireball
    name: ability.fireball
    action: cast
    costs:
      - resource: mana
        amount: 20
    cooldown: 2s
    castTime: 500ms
    conditions: [grounded, '!silenced']
  - id: dash
    action: dash
    cooldown: 250ms
    charges: 3
    recharge: 4s
  - id: drain
    action: drain
    trigger: hold
    costs:
      - resource: mana
        amount: 5
    cooldown: 6s
    channelTime: 3s
    tickInterval: 500ms
```

| Field | Meaning |
| --- | --- |
| `id` | Unique within the set: ASCII letters, digits, `_`, `-` and `.`, at most 64 bytes |
| `name` | Optional display name or localization key |
| `action` | The input action that activates it; empty means only code activates it with `TryActivate` |
| `trigger` | `press` (default) tries on the step the action is first pressed; `hold` tries on every step it is held and cancels the cast or channel when it is released |
| `costs` | Resources one use spends, each resource once, with a positive `amount` |
| `cooldown` | Shortest time between two uses, counted from the step a use takes effect; it applies even while charges remain |
| `charges` | Uses stored, 1 when omitted, at most 99; more than one needs a `recharge` |
| `recharge` | Time for one spent charge to come back; spent charges come back one after another |
| `castTime` | Time spent casting before the ability takes effect |
| `channelTime` | Time spent channeling after it takes effect |
| `tickInterval` | How often a channel ticks; needs a `channelTime` and must not exceed it |
| `conditions` | Names that must all hold to start and to keep casting or channeling; `!name` must not hold |

Durations are Go duration strings (`500ms`, `1m30s`) from 0 to 24h. `ability.Parse` refuses unknown fields, duplicate keys and IDs, other versions and out-of-range values; `ability.Encode` writes the fields in a fixed order and durations in their canonical form (`1500ms` becomes `1.5s`), and `Parse` reads its output back to an equal `Set`.

## Caster

A `Caster` runs one character's abilities. Make it with the set and the loop's rate, then call `Update` once per step after stepping the input:

```go
set, err := ability.Parse(data)
if err != nil {
	return err
}
hero, err := ability.NewCaster(set, 60) // the same Rate as game.Loop
if err != nil {
	return err
}
hero.Resources = ability.Pool{"mana": 100, "stamina": 50}
hero.Condition = func(name string) bool {
	switch name {
	case "grounded":
		return player.OnGround
	case "silenced":
		return player.Silenced
	}
	return false
}
hero.OnEvent = func(e ability.Event) {
	if e.Kind == ability.EventActivated && e.Ability == "fireball" {
		spawnFireball()
	}
}

loop := &game.Loop{Rate: 60, Update: func(dt time.Duration) {
	actions.Step()
	hero.Update(actions)
}}
```

`Update` takes anything with `Pressed` and `JustPressed`, such as `*input.Actions`, a replay or a network input; `nil` advances timers without reading input. Code activates abilities with `TryActivate(id)`, which returns `ReasonOK` or the reason it did not: `ReasonUnknown`, `ReasonBusy`, `ReasonCooldown`, `ReasonNoCharges`, `ReasonCondition` or `ReasonCost`, checked in that order. `CanActivate(id)` returns the same reason without changing anything, for greying out a button.

At most one ability casts or channels at a time. Its life is:

1. Activation checks the cooldown, charges, conditions and costs, and sends `EventStarted`.
2. The cast runs for `castTime`, nothing spent yet. A failing condition interrupts it (`EventInterrupted` with `ReasonCondition`), and `Cancel` stops it (`EventCanceled`), both spending nothing.
3. When the cast completes, the costs and a charge are spent, the cooldown starts and `EventActivated` is sent; this is where the game applies the effect. A cost that is no longer affordable interrupts it with `ReasonCost` instead. An ability with no cast time does this on the step it is activated.
4. A channel then runs for `channelTime`, sending `EventChannelTick` every `tickInterval` and `EventChannelEnded` at the end. Conditions, `Cancel` and releasing a `hold` action stop it early.

`EventCooldownEnded` and `EventChargeRestored` report timers finishing. `OnEvent` may call `TryActivate` or `Cancel`, for example to chain a follow-up when an ability takes effect; the events that causes follow the current one. Events that happened before a `Reset` or `Restore` called from `OnEvent` are still delivered, and when that call is made during `Update`, `Update` goes on to read input against the reset or restored state.

`Status(id)` reports charges, remaining cooldown and recharge, and the cast or channel progress in steps, and `Duration(steps)` converts them to game time for a cooldown bar. `Active()` reports the ability casting or channeling now.

### Costs and conditions

`Resources` is an interface with `Amount` and `Spend`, so costs can draw on the game's own stats. `ability.Pool` is a ready map-based one. Every cost of a use is checked before any is spent. With a nil `Resources`, abilities with costs cannot be activated; with a nil `Condition`, every condition is false.

## Determinism

`NewCaster` turns each duration into whole steps at its rate, rounding up, and every timer counts whole steps. At 60 Hz, `game.Loop`'s `dt` is 16,666,666 ns, so summing it would take 121 steps to reach 2 s; a `Caster` puts a 2 s cooldown at exactly 120 steps. Within a step, `Update` advances every cooldown and recharge in set order, then the active cast or channel, then reads input, so an ability whose cooldown ends on a step can be used again on that same step. When several abilities share an action, they are tried in set order and the first that activates wins, which lets conditions pick between them.

The same inputs on the same steps therefore give the same events on the same steps on every machine. `Snapshot` returns a `State` (step counts, JSON and YAML tags) for save slots, rollback and replays, and `Restore` puts it back, refusing a state from another set or rate. `Update` does not allocate.
