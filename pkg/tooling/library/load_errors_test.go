package library_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/tooling/library"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("index unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\npacks: []\n")
		_, err := library.ParseIndex(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("index unsupported version", func(t *testing.T) {
		data := []byte("version: 2\npacks: []\n")
		_, err := library.ParseIndex(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("manifest unknown field", func(t *testing.T) {
		data := []byte("version: 1\nname: tiles\nkind: art\npackVersion: 1.0.0\ndescription: Tiles\nlicense: CC0-1.0\nextra: 1\n")
		_, err := library.ParseManifest(data)
		loaderrtest.Expect(t, err, "", 7)
	})

	t.Run("manifest invalid kind", func(t *testing.T) {
		data := []byte("version: 1\nname: tiles\nkind: music\npackVersion: 1.0.0\ndescription: Tiles\nlicense: CC0-1.0\n")
		_, err := library.ParseManifest(data)
		loaderrtest.ExpectField(t, err, "", data, "kind")
	})
}
