package undo_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/undo"
)

func TestDecodeLogLoadErrors(t *testing.T) {
	type move struct {
		Dir string `yaml:"dir"`
	}

	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 2\napplied: 0\nforgotten: 0\nmoves: []\nextra: 1\n")
		_, err := undo.DecodeLog[move](data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("applied out of range", func(t *testing.T) {
		data := []byte("version: 2\napplied: 3\nforgotten: 0\nmoves:\n  - dir: up\n")
		_, err := undo.DecodeLog[move](data)
		loaderrtest.ExpectField(t, err, "", data, "applied")
	})
}
