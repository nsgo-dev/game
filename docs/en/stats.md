# stats

`game/stats` keeps an entity's gameplay stats: named base values changed by additive, multiplicative and override modifiers that come from sources such as equipment and buffs. Values are recomputed the moment anything changes, and timed modifiers count fixed steps, so a buff that lasts 300 steps ends on exactly the 300th.

```go
import "github.com/nsgo-dev/game/pkg/stats"
```

## A sheet of stats

A `Sheet` holds one entity's stats in the order they were defined:

```go
hero, err := stats.NewSheet(
	stats.Def{Name: "health", Base: 100, Min: stats.Float(0)},
	stats.Def{Name: "attack", Base: 12},
	stats.Def{Name: "speed", Base: 5, Min: stats.Float(0), Max: stats.Float(20)},
)

hero.Value("attack") // 12
hero.SetBase("attack", 14)
```

`Value` is a cached read; it never recomputes. `Lookup` also reports whether the stat exists, `Base` returns the value before modifiers, and `Stats` lists the names. The zero `Sheet` is ready to use with `Define`.

## Modifiers and how they stack

A `Modifier` names a stat, an `Op` and a value. Each one is added from a source, a free-form tag such as `"equip:weapon"` or `"buff:haste"`:

| Op | YAML | Effect |
| --- | --- | --- |
| `OpAdd` | `add` | Adds its value to the base |
| `OpMul` | `mul` | Multiplies by its value after every addition |
| `OpOverride` | `override` | Replaces the result, ignoring adds and muls |

A stat's value is:

```
clamp((base + sum of add values) × product of mul values)
```

While any override is on the stat, the value is instead `clamp(override value)`, where the override with the highest `Priority` wins and, among equal priorities, the one added last. `clamp` applies the stat's `Min` and `Max` when they are set, to overrides too. Apart from breaking a tie between overrides, the order modifiers are added in matters only for floating-point rounding: adds and muls are applied in that order, so `0.1 + 0.2 + 0.3` and `0.3 + 0.1 + 0.2` can differ in the last digit. Compare values with a tolerance when that matters. A sum or product that overflows `float64` saturates at the largest finite value of its sign, so a value is always finite and within its bounds.

| Base | Modifiers | Value |
| --- | --- | --- |
| 10 | add 5, add 3, add −2 | 16 |
| 10 | mul 2, mul 1.5 | 30 |
| 10 | mul 2, add 5, mul 1.5, add 3 | (10 + 8) × 3 = 54 |
| 10 | add 5, override 7, mul 3 | 7 |
| 10 | override 1 (priority 5), override 2 (priority 9) | 2 |
| 10, max 25 | mul 3 | 25 |

```go
sword, _ := hero.Add("equip:weapon", stats.Modifier{Stat: "attack", Op: stats.OpAdd, Value: 8})
hero.Add("equip:ring", stats.Modifier{Stat: "attack", Op: stats.OpMul, Value: 1.5})
hero.Value("attack") // (14 + 8) × 1.5 = 33

hero.Remove(sword)               // one modifier by its ID
hero.RemoveSource("equip:ring")  // every modifier from a source
hero.Clear()                     // all of them
```

`Add` refuses an unknown stat (`ErrUnknownStat`), an unknown op (`ErrInvalidOp`) and a NaN or infinite value (`ErrInvalidValue`). `Modifiers` lists what is on the sheet in the order it was added, with each modifier's ID, source and remaining steps.

## Timed modifiers on the fixed step

`AddTimed` adds a modifier that lasts a number of calls of `Step`. Call `Step` once from each `game.Loop` update; the modifier is removed by exactly the n-th `Step` after it was added, and the stat is recomputed in that same step:

```go
hero.AddTimed("buff:haste", stats.Modifier{Stat: "speed", Op: stats.OpMul, Value: 2}, 180)

loop := &game.Loop{Rate: 60, Update: func(dt time.Duration) {
	hero.Step() // haste ends on the 180th update: 3 seconds at 60 Hz
}}
```

Because time is counted in steps rather than durations, a paused loop pauses every buff, and a replay ends them on the same step. `Remaining(id)` returns how many steps a modifier has left (0 for one without a limit). `Steps` returns how many times `Step` has run.

## Effects

Effects are deterministic fixed-step state. `Apply` is atomic, `Refresh` replaces one source as a single net change, and `Snapshot`/`Restore` preserve active modifiers and remaining steps for replay.

An `Effect` is a named group of modifiers applied together, such as an item's bonuses or a buff that touches several stats. Its `Steps` (0 for no limit) applies to all of its modifiers:

```go
berserk := stats.Effect{Name: "berserk", Steps: 300, Modifiers: []stats.Modifier{
	{Stat: "attack", Op: stats.OpMul, Value: 1.5},
	{Stat: "speed", Op: stats.OpAdd, Value: 2},
}}
ids, err := hero.Apply("buff:berserk", berserk)
```

`Apply` adds all of the effect's modifiers or none of them. Applying an effect again stacks a second copy; to refresh a buff's duration instead, call `Refresh` with its source:

```go
hero.Refresh("buff:berserk", berserk) // a full 300 steps again
```

`Refresh` removes every modifier from the source and applies the effect in their place as one change, so `OnChange` sees only the net difference, never the buff ending and starting again. Calling `RemoveSource` and then `Apply` does the same in two changes, and listeners see the value drop back to the unbuffed one in between.

## Change notifications

`OnChange` is called once for each stat whose value changed, after the change that caused it, in definition order:

```go
hero.OnChange = func(c stats.Change) {
	if c.Stat == "health" && c.New == 0 {
		die()
	}
}
```

A stat whose modifiers changed but whose value did not is not reported. `OnChange` may change the sheet itself, for example to keep one stat in step with another; those changes are recomputed and reported before the call that started them returns.

## Stats files

Stat definitions and effects are written in a versioned YAML file, so designers can tune numbers in review:

```yaml
version: 1
stats:
  - name: health
    base: 100
    min: 0
  - name: attack
    base: 12
  - name: speed
    base: 5
    min: 0
    max: 20
effects:
  - name: iron-sword
    modifiers:
      - stat: attack
        op: add
        value: 8
  - name: berserk
    steps: 300
    modifiers:
      - stat: attack
        op: mul
        value: 1.5
      - stat: speed
        op: add
        value: 2
  - name: frozen
    steps: 90
    modifiers:
      - stat: speed
        op: override
        value: 0
        priority: 10
```

```go
file, err := stats.Parse(data)
hero, err := file.NewSheet()
sword, _ := file.Effect("iron-sword")
hero.Apply("equip:weapon", sword)
```

`Parse` refuses unknown fields, a missing or different `version`, trailing documents, duplicate or empty names, non-finite numbers, `min` above `max`, negative `steps`, effects without modifiers, unknown ops and modifiers on undefined stats. `Encode` writes the fields in a fixed order with 2-space indentation and keeps lists in their given order, so parsing and encoding an encoded file reproduces it byte for byte.

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | int | Schema version, `1` |
| `stats[].name` | string | Unique stat name |
| `stats[].base` | number | Value before modifiers |
| `stats[].min`, `stats[].max` | number, optional | Bounds applied after modifiers |
| `effects[].name` | string | Unique effect name |
| `effects[].steps` | int, optional | Fixed steps the effect lasts, 0 or absent for no limit |
| `effects[].modifiers[].stat` | string | A stat defined above |
| `effects[].modifiers[].op` | `add`, `mul` or `override` | How it changes the stat |
| `effects[].modifiers[].value` | number | The amount, factor or override value |
| `effects[].modifiers[].priority` | int, optional | Override priority, highest wins |
