package rooms_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/rooms"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nrooms:\n  - id: a\n    x: 0\n    y: 0\n    width: 100\n    height: 100\n    bogus: 1\n")
		_, err := rooms.Parse(data)
		loaderrtest.Expect(t, err, "", 8)
		if !errors.Is(err, rooms.ErrInvalidMap) {
			t.Errorf("errors.Is(err, ErrInvalidMap) = false (err: %v)", err)
		}
	})

	t.Run("duplicate room id", func(t *testing.T) {
		data := []byte("version: 1\nrooms:\n  - id: hall\n    x: 0\n    y: 0\n    width: 100\n    height: 100\n  - id: hall\n    x: 100\n    y: 0\n    width: 100\n    height: 100\n")
		_, err := rooms.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "rooms[1].id")
		if !errors.Is(err, rooms.ErrInvalidMap) {
			t.Errorf("errors.Is(err, ErrInvalidMap) = false (err: %v)", err)
		}
	})

	t.Run("bad transition duration", func(t *testing.T) {
		data := []byte("version: 1\ntransition:\n  kind: slide\n  duration: nope\nrooms:\n  - id: hall\n    x: 0\n    y: 0\n    width: 100\n    height: 100\n")
		_, err := rooms.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
		if !errors.Is(err, rooms.ErrInvalidMap) {
			t.Errorf("errors.Is(err, ErrInvalidMap) = false (err: %v)", err)
		}
	})

	t.Run("LoadFS names file", func(t *testing.T) {
		data := []byte("version: 1\nrooms:\n  - id: hall\n    x: 0\n    y: 0\n    width: 0\n    height: 100\n")
		fsys := fstest.MapFS{"maps/castle.rooms.yaml": &fstest.MapFile{Data: data}}
		_, err := rooms.LoadFS(fsys, "maps/castle.rooms.yaml")
		loaderrtest.ExpectField(t, err, "maps/castle.rooms.yaml", data, "rooms[0].width")
	})
}
