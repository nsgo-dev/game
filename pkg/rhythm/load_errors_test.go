package rhythm_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/rhythm"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("beatmap unknown field", func(t *testing.T) {
		data := []byte("version: 1\ntitle: Song\ntempos:\n  - beat: 0\n    bpm: 120\nextra: 1\n")
		_, err := rhythm.ParseBeatMap(data)
		loaderrtest.Expect(t, err, "", 6)
	})

	t.Run("beatmap unsupported version", func(t *testing.T) {
		data := []byte("version: 2\ntitle: Song\ntempos:\n  - beat: 0\n    bpm: 120\n")
		_, err := rhythm.ParseBeatMap(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("calibrations unsupported version", func(t *testing.T) {
		data := []byte("version: 2\ndevices: {}\n")
		_, err := rhythm.ParseCalibrations(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
