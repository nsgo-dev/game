package resource_test

import (
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/resource"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("type: material\nversion: 1\nshininess: 3\n")
		_, err := resource.Parse(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("invalid blend mode", func(t *testing.T) {
		data := []byte("type: material\nversion: 1\nblend: glow\n")
		_, err := resource.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "blend")
	})

	t.Run("Library.Load names file", func(t *testing.T) {
		data := []byte("type: material\nversion: 1\nopacity: 2\n")
		lib := resource.NewLibrary(fstest.MapFS{"materials/stone.material.yaml": &fstest.MapFile{Data: data}})
		_, err := lib.Load("materials/stone.material.yaml")
		loaderrtest.ExpectField(t, err, "materials/stone.material.yaml", data, "opacity")
	})
}
