package datatable_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/tooling/datatable"
)

type weapon struct {
	ID       string        `table:"id"`
	Damage   int           `table:"damage"`
	Cooldown time.Duration `table:"cooldown"`
	Tags     []string      `table:"tags"`
}

// LoadYAML reads a data table, a mapping with version: 1 and a list of rows with one field per column, into a slice of structs.
func ExampleLoadYAML() {
	rows, err := datatable.LoadYAML[weapon]("weapons.yaml", []byte(`version: 1
rows:
  - id: sword
    damage: 12
    cooldown: 400ms
    tags: [melee]
  - id: bow
    damage: 8
    cooldown: 1s
    tags: [ranged, two-handed]
`))
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, w := range rows {
		fmt.Println(w.ID, w.Damage, w.Cooldown, w.Tags)
	}
	// Output:
	// sword 12 400ms [melee]
	// bow 8 1s [ranged two-handed]
}

// A bad cell is reported as a CellError naming the file, row and field, so a designer can find it.
func ExampleCellError() {
	_, err := datatable.LoadCSV[weapon]("weapons.csv", []byte("id,damage,cooldown,tags\nsword,twelve,400ms,melee\n"))
	var cell *datatable.CellError
	if errors.As(err, &cell) {
		fmt.Println(cell.File, cell.Row, cell.Field, cell.Value)
	}
	// Output:
	// weapons.csv 1 damage twelve
}
