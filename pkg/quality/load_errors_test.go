package quality_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/quality"
)

func TestParseConfigLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\npresets:\n  low: {resolution_scale: 0.5, particle_scale: 0.25}\n  medium: {resolution_scale: 0.75, particle_scale: 0.6}\n  high: {resolution_scale: 1, particle_scale: 1}\n")
		_, err := quality.ParseConfig(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("preset scale out of range", func(t *testing.T) {
		data := []byte("version: 1\npresets:\n  low:\n    resolution_scale: 0\n    particle_scale: 0.25\n  medium:\n    resolution_scale: 0.75\n    particle_scale: 0.6\n  high:\n    resolution_scale: 1\n    particle_scale: 1\n")
		_, err := quality.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "presets.low.resolution_scale")
	})

	t.Run("duplicate device rule", func(t *testing.T) {
		data := []byte("version: 1\npresets:\n  low: {resolution_scale: 0.5, particle_scale: 0.25}\n  medium: {resolution_scale: 0.75, particle_scale: 0.6}\n  high: {resolution_scale: 1, particle_scale: 1}\ndevices:\n  - model: Pixel\n    level: high\n  - model: pixel\n    level: low\n")
		_, err := quality.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "devices[1].model")
	})
}
