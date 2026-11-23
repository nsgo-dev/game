# datatable

`game/tooling/datatable` loads typed game data, such as enemies, items, levels and quests, into slices of your own Go struct, and reloads it while the game runs when a designer saves the file. Tables are YAML, so designers and AI tools edit them directly and every change reads as a line in review; CSV and JSON exports from spreadsheets and other tools load too.

```go
import "github.com/nsgo-dev/game/packages/game/tooling/datatable"
```

## A table and its type

A row type is a struct. Each field is a column named by its `table` tag, else its `json` tag, else its Go field name; a tag of `-` leaves the field out. Fields can be strings, bools, integers, unsigned integers, floats, `time.Duration`, pointers to these (absent or `null` leaves the pointer nil) and slices of them.

```go
type Enemy struct {
	ID       string        `table:"id"`
	Name     string        `table:"name"`
	HP       int           `table:"hp"`
	Speed    float64       `table:"speed"`
	Boss     bool          `table:"boss"`
	Tags     []string      `table:"tags"`
	Cooldown time.Duration `table:"cooldown"`
}
```

The YAML file is a mapping with a `version`, which is `datatable.FormatVersion` (1), and a `rows` list. Each row is a mapping from column names to values; a column a row leaves out keeps its zero value.

```yaml
version: 1
rows:
  - id: goblin
    name: Cave Goblin
    hp: 45
    speed: 3.5
    boss: false
    tags: [melee, scout]
    cooldown: 500ms
  - id: dragon
    name: Ancient Wyrm
    hp: 1200
    speed: 6.25
    boss: true
    tags: [flying, fire, boss]
    cooldown: 2s
```

`LoadFile[T](path)` picks the reader by extension (`.yaml`, `.yml`, `.csv` or `.json`); `LoadYAML[T](name, data)` reads bytes you already have, such as an embedded file.

```go
enemies, err := datatable.LoadFile[Enemy]("data/enemies.yaml")
```

## What a YAML table refuses

A YAML table is strict, so a typo is an error at load time rather than a silently missing value:

- a missing `version`, a version other than 1, or a top-level key other than `version` and `rows`;
- a bare list of rows without the `version`/`rows` mapping, or a second YAML document after `---`;
- a row key that names no column (`datatable.ErrUnknownField`), including a field tagged `-`;
- a column set twice in one row, for example as `hp` and `HP` (`datatable.ErrDuplicateField`);
- a merge key (`<<: *defaults`): anchors and aliases load, but each row spells out its own columns;
- a value the field's type cannot hold, such as `speed: fast` or an `int8` of 300.

Errors inside a row are a `*datatable.CellError` with the file, the 1-based row, the column and the value:

```go
var ce *datatable.CellError
if errors.As(err, &ce) {
	log.Printf("%s row %d, %s = %q: %v", ce.File, ce.Row, ce.Field, ce.Value, ce.Err)
}
```

Column names match exactly first and then without regard to case, so `Armor` and `armor` both fill a field named `Armor`. CSV and JSON stay as their tools export them: they have no version, and columns the type does not have are skipped.

## Saving a table

`MarshalYAML(rows)` and `SaveYAML(path, rows)` write the same format, for an editor or a script that generates data. The output is deterministic: `version` first, then every column of every row in struct declaration order, two-space indentation, lists inline, strings quoted only where YAML needs it, a nil pointer as `null` and a duration in its `String` form. A file saved by `SaveYAML`, loaded and saved again is byte for byte the same. A nil list and an empty list are both written as `[]`. Non-finite floats and field types the loader cannot read (maps, structs, interfaces) are an error.

Every column has one field: a row type in which two fields share a column name, such as two fields tagged `hp` or an embedded struct's `name` and the outer struct's own `name`, is refused by both `LoadYAML` and `MarshalYAML`, because only one of the two could be written or read back. Give each field its own tag, or tag one of them `-`.

```go
if err := datatable.SaveYAML("data/enemies.yaml", enemies); err != nil {
	return err
}
```

## Hot reload

`NewTable[T](path)` loads the file and holds its rows. `Watch` registers it with a `hotreload.Watcher` (from `game/tooling/hotreload`) and calls back on the engine goroutine with the new rows after each valid save:

```go
func Game(cx *core.Scope) core.Node {
	enemies, err := datatable.NewTable[Enemy]("data/enemies.yaml")
	if err != nil {
		panic(err)
	}
	w := hotreload.NewWatcher(cx, "data")
	enemies.WatchWithError(w, func(rows []Enemy, err error) {
		if err != nil {
			log.Printf("enemies.yaml: %v", err) // rows are still the last valid ones
			return
		}
		spawner.SetEnemies(rows)
	})
	// ...
}
```

An edit that does not load, such as a misspelt column, keeps the previous rows, leaves `Version` unchanged and records the error in `LastError`; the next valid save replaces the rows and increases `Version`. `Rows` returns a copy, so callers cannot change the table's rows. In release builds (`-tags release`) the watcher is a no-op and the table keeps the rows it loaded at start.
