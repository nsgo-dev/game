package voxel_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/voxel"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\n")
		_, err := voxel.Parse(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := []byte("version: 2\n")
		_, err := voxel.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
