package platformer_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/platformer"
)

func TestParseConfigLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nrun_speed: 200\nboost: 10\n")
		_, err := platformer.ParseConfig(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nrun_speed: 200\n")
		_, err := platformer.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("negative gravity", func(t *testing.T) {
		data := []byte("version: 1\ngravity: -10\n")
		_, err := platformer.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "gravity")
	})
}
