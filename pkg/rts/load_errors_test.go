package rts_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/rts"
)

func TestParseFormationsLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nformations:\n  - name: line\n    shape: line\n    spacing: 24\n    angle: 45\n")
		_, err := rts.ParseFormations(data)
		loaderrtest.Expect(t, err, "", 6)
	})

	t.Run("duplicate formation name", func(t *testing.T) {
		data := []byte("version: 1\nformations:\n  - name: line\n    shape: line\n    spacing: 24\n  - name: line\n    shape: box\n    spacing: 24\n")
		_, err := rts.ParseFormations(data)
		loaderrtest.ExpectField(t, err, "", data, "formations[1].name")
	})

	t.Run("unknown shape", func(t *testing.T) {
		data := []byte("version: 1\nformations:\n  - name: ring\n    shape: circle\n    spacing: 24\n")
		_, err := rts.ParseFormations(data)
		loaderrtest.ExpectField(t, err, "", data, "formations[0].shape")
	})
}
