package cards_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/cards"
)

func TestParseBoardDocLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\ncard_size: {w: 63, h: 88}\nslots:\n  - id: hand\n    rect: {x: 0, y: 0, w: 400, h: 120}\n    layout: {fan: {arc: 24}}\n    color: blue\n")
		_, err := cards.ParseBoardDoc(data)
		loaderrtest.Expect(t, err, "", 7)
	})

	t.Run("duplicate slot id", func(t *testing.T) {
		data := []byte("version: 1\ncard_size: {w: 63, h: 88}\nslots:\n  - id: hand\n    rect: {x: 0, y: 0, w: 400, h: 120}\n    layout: {fan: {arc: 24}}\n  - id: hand\n    rect: {x: 0, y: 200, w: 100, h: 140}\n    layout: {stack: {}}\n")
		_, err := cards.ParseBoardDoc(data)
		loaderrtest.ExpectField(t, err, "", data, "slots[1].id")
	})

	t.Run("missing layout", func(t *testing.T) {
		data := []byte("version: 1\ncard_size: {w: 63, h: 88}\nslots:\n  - id: hand\n    rect: {x: 0, y: 0, w: 400, h: 120}\n    layout: {}\n")
		_, err := cards.ParseBoardDoc(data)
		loaderrtest.ExpectField(t, err, "", data, "slots[0].layout")
	})
}
