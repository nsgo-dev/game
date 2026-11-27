package loot_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/loot"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\ntables:\n  - name: chest\n    color: gold\n    entries:\n      - item: coin\n        weight: 1\n")
		_, err := loot.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
	})

	t.Run("duplicate table name", func(t *testing.T) {
		data := []byte("version: 1\ntables:\n  - name: chest\n    entries:\n      - item: coin\n        weight: 1\n  - name: chest\n    entries:\n      - item: gem\n        weight: 1\n")
		_, err := loot.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "tables[1].name")
	})

	t.Run("unknown table reference", func(t *testing.T) {
		data := []byte("version: 1\ntables:\n  - name: chest\n    entries:\n      - table: missing\n        weight: 1\n")
		_, err := loot.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "tables[0].entries[0].table")
	})
}
