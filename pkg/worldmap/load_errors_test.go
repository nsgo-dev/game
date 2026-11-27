package worldmap_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/worldmap"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nrooms:\n  - id: hall\n    x: 0\n    y: 0\n    width: 10\n    height: 10\n    extra: 1\n")
		_, err := worldmap.Parse(data)
		loaderrtest.Expect(t, err, "", 8)
	})

	t.Run("duplicate room id", func(t *testing.T) {
		data := []byte("version: 1\nrooms:\n  - id: hall\n    x: 0\n    y: 0\n    width: 10\n    height: 10\n  - id: hall\n    x: 10\n    y: 0\n    width: 10\n    height: 10\n")
		_, err := worldmap.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "rooms[1].id")
	})

	t.Run("unknown marker room", func(t *testing.T) {
		data := []byte("version: 1\nrooms:\n  - id: hall\n    x: 0\n    y: 0\n    width: 10\n    height: 10\nmarkers:\n  - id: save1\n    kind: save\n    room: crypt\n    x: 5\n    y: 5\n")
		_, err := worldmap.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "markers[0].room")
	})
}
