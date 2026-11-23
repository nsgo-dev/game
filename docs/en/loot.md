# loot

`github.com/nsgo-dev/game/packages/game/loot` rolls nested weighted drop tables with guaranteed drops and pity counters. Every roll draws from a [`random.Stream`](determinism.md), so a seed and a sequence of rolls give the same drops on every platform, in replays and on a dedicated server.

## Tables in YAML

A game's drop tables are a versioned YAML document:

```yaml
version: 1
tables:
  - name: boss
    rolls: {min: 1, max: 3}
    guaranteed:
      - item: gold
        count: {min: 50, max: 120}
    entries:
      - table: common
        weight: 80
      - table: rare
        weight: 19
        count: 2
      - nothing: true
        weight: 1
  - name: common
    entries:
      - item: potion
        weight: 3
      - item: arrow
        weight: 1
        count: 10
  - name: rare
    entries:
      - item: amulet
        weight: 1
```

- `rolls` is how many weighted picks one roll of the table makes (one when omitted). `guaranteed` entries drop every time the table is rolled, before its picks.
- An entry drops an `item`, rolls another `table`, or drops `nothing`; a `nothing` entry's weight sets the chance of a miss. Weights are relative and positive; the chance of an entry is its weight over the table's total.
- `count` is an item's quantity, or how many times a table entry rolls its table. `rolls` and `count` take an integer or a `{min, max}` range drawn uniformly.

`loot.Parse` refuses unknown fields, any version but 1, duplicate names, unknown or cyclic table references, and tables whose worst-case roll would exceed `loot.MaxRollSteps` (65536 picks, sub-table rolls and drops). `loot.Encode` writes a document back as deterministic YAML: the fields in a fixed order, a range written as an integer when its ends are equal, and text that parses back to an equal document.

## Rolling

```go
doc, err := loot.Parse(data)
if err != nil { ... }
lib, err := loot.New(doc)
if err != nil { ... }

streams := random.NewStreams(worldSeed)
roller := loot.NewRoller(lib, streams.Stream("loot"))

drops, err := roller.Roll("boss") // []loot.Drop{{Item: "gold", Count: 87}, {Item: "potion", Count: 1}, ...}
```

A `Library` is immutable and can be shared; a `Roller` belongs to one player or save and one goroutine. Drops come back in the order they were produced, with nested tables expanded in place. `AppendRoll(dst[:0], name)` reuses a slice and rolls without allocating. `Library.Chances(name)` returns each entry's chance on one pick, for drop-rate screens.

## Pity

A pity rule raises the chance of one of its table's weighted entries after a run of picks without it:

```yaml
  - name: banner
    entries:
      - table: five-star
        weight: 0.6
      - table: four-star
        weight: 5.1
      - item: three-star
        weight: 94.3
    pity:
      - name: five
        table: five-star
        softFrom: 74
        softStep: 6
        guaranteeAt: 90
      - name: four
        table: four-star
        guaranteeAt: 10
```

- `guaranteeAt: 90` forces the target on the 90th pick since it was last picked.
- `softFrom: 74` with `softStep: 6` adds `(n - 74 + 1) * 6` to the target's weight on the n-th pick since it was last picked, from the 74th on. `softFrom` must be below `guaranteeAt` when both are set.
- Each rule keeps a miss counter: the number of picks since its target was last picked, reset whenever the target is picked by chance or by force. `roller.Misses("banner", "five")` reads it for a pity meter.
- When several rules force the same pick, the first one in the list wins and the others keep counting, so the next pick goes to the next rule.

## Saving

The pity counters and the stream are saved together, so a loaded game continues with the same drops:

```go
type LootSave struct {
	Pity   loot.State   `json:"pity"`
	Stream random.State `json:"stream"`
}

save := LootSave{Pity: roller.State(), Stream: roller.Stream().State()}

// After loading:
stream := random.New(0)
stream.SetState(save.Stream)
roller = loot.NewRoller(lib, stream)
if err := roller.SetState(save.Pity); err != nil { ... }
```

`State` lists only non-zero counters, keyed by table and rule name, and marshals with sorted keys. `SetState` ignores counters for tables or rules the document no longer has, so a save survives edits to the loot file, and refuses negative counters.
