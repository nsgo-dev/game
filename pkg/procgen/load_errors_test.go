package procgen_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/procgen"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("rules unknown field", func(t *testing.T) {
		data := []byte("version: 1\ntiles:\n  - name: floor\n    weight: 1\n    bogus: 1\n")
		_, err := procgen.ParseRules(data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("rules unsupported version", func(t *testing.T) {
		data := []byte("version: 2\ntiles:\n  - name: floor\n    weight: 1\n")
		_, err := procgen.ParseRules(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("room templates unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nrooms: []\n")
		_, err := procgen.ParseRoomTemplates(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
