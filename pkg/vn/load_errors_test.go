package vn_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/vn"
)

func TestParseCastLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\ncharacters:\n  - id: aria\n    name: Aria\n    extra: 1\n")
		_, err := vn.ParseCast(data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("duplicate character id", func(t *testing.T) {
		data := []byte("version: 1\ncharacters:\n  - id: aria\n    name: Aria\n    expressions:\n      - name: neutral\n        frame: aria_neutral\n  - id: aria\n    name: Aria 2\n    expressions:\n      - name: neutral\n        frame: aria_neutral\n")
		_, err := vn.ParseCast(data)
		loaderrtest.ExpectField(t, err, "", data, "characters[1].id")
	})
}
