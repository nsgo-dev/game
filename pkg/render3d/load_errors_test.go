package render3d_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/render3d"
)

func TestParseLightProbeGridLoadErrors(t *testing.T) {
	t.Run("unknown field preserves ErrInvalidProbe", func(t *testing.T) {
		data := []byte("version: 1\nboundsMin: [0, 0, 0]\nboundsMax: [1, 1, 1]\ncounts: [1, 1, 1]\nextra: true\nprobes: []\n")
		_, err := render3d.ParseLightProbeGrid(data)
		loaderrtest.Expect(t, err, "", 5)
		if !errors.Is(err, render3d.ErrInvalidProbe) {
			t.Errorf("errors.Is(err, ErrInvalidProbe) = false (err: %v)", err)
		}
	})

	t.Run("probe count mismatch preserves ErrInvalidProbe", func(t *testing.T) {
		data := []byte("version: 1\nboundsMin: [0, 0, 0]\nboundsMax: [1, 1, 1]\ncounts: [1, 1, 1]\nprobes: []\n")
		_, err := render3d.ParseLightProbeGrid(data)
		loaderrtest.ExpectField(t, err, "", data, "probes")
		if !errors.Is(err, render3d.ErrInvalidProbe) {
			t.Errorf("errors.Is(err, ErrInvalidProbe) = false (err: %v)", err)
		}
	})

	t.Run("LoadLightProbeGridFS names file", func(t *testing.T) {
		data := []byte("version: 2\nboundsMin: [0, 0, 0]\nboundsMax: [1, 1, 1]\ncounts: [1, 1, 1]\nprobes: []\n")
		fsys := fstest.MapFS{"baked/room.probes.yaml": &fstest.MapFile{Data: data}}
		_, err := render3d.LoadLightProbeGridFS(fsys, "baked/room.probes.yaml")
		loaderrtest.ExpectField(t, err, "baked/room.probes.yaml", data, "version")
	})
}
