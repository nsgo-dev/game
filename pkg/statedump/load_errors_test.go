package statedump_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/statedump"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nseed: 1\nsteps: 0\nextra: true\nentities: []\n")
		_, err := statedump.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
	})

	t.Run("zero generation", func(t *testing.T) {
		data := []byte("version: 1\nseed: 1\nsteps: 0\nentities:\n  - index: 0\n    generation: 0\n")
		_, err := statedump.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "entities[0].generation")
	})

	t.Run("ReadFile missing file names path", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "dump.yaml")
		_, err := statedump.ReadFile(missing)
		loaderrtest.Expect(t, err, missing, 0)
	})
}
