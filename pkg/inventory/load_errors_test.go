package inventory_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/inventory"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nitems:\n  - id: sword\n    rarity: rare\n")
		_, err := inventory.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
	})

	t.Run("duplicate item id", func(t *testing.T) {
		data := []byte("version: 1\nitems:\n  - id: potion\n  - id: potion\n")
		_, err := inventory.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "items[1].id")
	})

	t.Run("explicit zero stack", func(t *testing.T) {
		data := []byte("version: 1\nitems:\n  - id: potion\n    stack: 0\n")
		_, err := inventory.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "items[0].stack")
	})
}
