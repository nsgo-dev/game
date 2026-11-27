package assetbuild

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
)

func TestParseCacheLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\ntargets: []\n")
		_, err := parseCache(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := []byte("version: 2\ntargets: []\n")
		_, err := parseCache(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
