package navbake_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/pathfind/navbake"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nradius: 8\nextra: 1\npolygons:\n  - [[0, 0], [10, 0], [10, 10]]\n")
		_, err := navbake.Parse(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("negative radius", func(t *testing.T) {
		data := []byte("version: 1\nradius: -2\npolygons:\n  - [[0, 0], [10, 0], [10, 10]]\n")
		_, err := navbake.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "radius")
	})

	t.Run("bad vertex count", func(t *testing.T) {
		data := []byte("version: 1\nradius: 8\npolygons:\n  - [[0, 0], [10, 0]]\n")
		_, err := navbake.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "polygons[0]")
	})
}
