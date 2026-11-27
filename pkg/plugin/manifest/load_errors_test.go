package manifest_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/plugin/manifest"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("manifestVersion: 1\nextra: true\n")
		_, err := manifest.Parse(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("unsupported manifestVersion", func(t *testing.T) {
		data := []byte("manifestVersion: 2\n")
		_, err := manifest.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "manifestVersion")
	})
}
