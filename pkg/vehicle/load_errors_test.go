package vehicle_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/vehicle"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("config unknown field", func(t *testing.T) {
		data := []byte("version: 1\nengineForce: 1000\nturbo: true\n")
		_, err := vehicle.ParseConfig(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("config bad version", func(t *testing.T) {
		data := []byte("version: 2\n")
		_, err := vehicle.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("ghost bad version", func(t *testing.T) {
		data := []byte("version: 2\nrate: 60\nframes: []\n")
		_, err := vehicle.ParseGhost(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
